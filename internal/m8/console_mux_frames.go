package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const muxFrameMax = 1 << 20

type consoleMuxWire struct {
	Type       string `json:"type"`
	AgentID    string `json:"agent_id,omitempty"`
	FullName   string `json:"full_name,omitempty"`
	Instance   string `json:"iframe_instance,omitempty"`
	Generation string `json:"generation,omitempty"`
	Round      string `json:"replay_round,omitempty"`
	Epoch      string `json:"epoch,omitempty"`
	Seq        string `json:"seq,omitempty"`
	Floor      string `json:"floor,omitempty"`
	H          string `json:"H,omitempty"`
	Empty      bool   `json:"empty,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Reason     string `json:"reason,omitempty"`
}
type consoleMuxPacket struct {
	Meta consoleMuxWire
	Body []byte
	Wire []byte
}

func muxPacket(meta consoleMuxWire, payload []byte) (*consoleMuxPacket, error) {
	if len(payload) > muxFrameMax {
		return nil, errors.New("event_too_large")
	}
	head, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	cap := 4096
	if meta.Type == "event" {
		cap = muxFrameMax
	}
	size := len(head) + 8
	if len(payload) > 0 {
		if !json.Valid(payload) {
			return nil, errors.New("invalid_event_body")
		}
		size += len(payload) + 11
	}
	if size > cap {
		return nil, errors.New("event_too_large")
	}
	out := make([]byte, 0, size)
	out = append(out, []byte("data: ")...)
	if len(payload) > 0 {
		out = append(out, head[:len(head)-1]...)
		out = append(out, []byte(",\"payload\":")...)
		out = append(out, payload...)
		out = append(out, '}')
	} else {
		out = append(out, head...)
	}
	out = append(out, '\n', '\n')
	return &consoleMuxPacket{Meta: meta, Wire: out}, nil
}
func muxMeta(s *consoleMuxScope, kind string) consoleMuxWire {
	return consoleMuxWire{Type: kind, AgentID: s.Agent.ID, FullName: s.Agent.FullName, Instance: s.Desired.Instance, Generation: s.Generation, Round: muxSeq(s.Round)}
}

// Reader bytes are checked at publisher fanout, not after an unbounded channel.
type consoleMuxReader struct {
	mu     sync.Mutex
	events []sessionSeqEvent
	bytes  int
	epoch  string
	gap    string
	closed bool
	ready  chan struct{}
}

func (r *consoleMuxReader) signal() {
	select {
	case r.ready <- struct{}{}:
	default:
	}
}
func (r *consoleMuxReader) offer(epoch string, seq uint64, data string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.gap != "" {
		return
	}
	if epoch != r.epoch || seq >= math.MaxUint64-1 {
		r.gap = "seq_exhausted"
		r.signal()
		return
	}
	if len(data) > muxFrameMax {
		r.gap = "event_too_large"
		r.signal()
		return
	}
	if len(r.events) >= 256 || r.bytes+len(data) > 2<<20 {
		r.gap = "backpressure"
		r.signal()
		return
	}
	r.events = append(r.events, sessionSeqEvent{Seq: seq, Data: data})
	r.bytes += len(data)
	r.signal()
}
func (r *consoleMuxReader) next(ctx context.Context) (sessionSeqEvent, string, bool) {
	for {
		r.mu.Lock()
		if len(r.events) > 0 {
			v := r.events[0]
			r.events[0] = sessionSeqEvent{}
			r.events = r.events[1:]
			r.bytes -= len(v.Data)
			r.mu.Unlock()
			return v, "", true
		}
		if r.gap != "" {
			gap := r.gap
			r.gap = ""
			r.closed = true
			r.mu.Unlock()
			return sessionSeqEvent{}, gap, true
		}
		closed := r.closed
		r.mu.Unlock()
		if closed {
			return sessionSeqEvent{}, "", false
		}
		select {
		case <-ctx.Done():
			return sessionSeqEvent{}, "", false
		case <-r.ready:
		}
	}
}
func (h *Hub) subscribeConsoleMuxSource(a consolemux.Agent, id string) (sessionSeqSnapshot, *consoleMuxReader, func(), bool) {
	h.mu.Lock()
	st := h.terminals[terminalKey(AgentKeyID(a.Owner), a.Session)]
	if st == nil {
		h.mu.Unlock()
		return sessionSeqSnapshot{}, nil, nil, false
	}
	st.ensureEventEpochLocked()
	r := &consoleMuxReader{epoch: st.eventEpoch, ready: make(chan struct{}, 1)}
	if st.muxSeqSubs == nil {
		st.muxSeqSubs = map[string]*consoleMuxReader{}
	}
	st.muxSeqSubs[id] = r
	snap := sessionSeqSnapshot{Epoch: st.eventEpoch, H: st.eventSeq, Replay: st.eventRingSnapshotLocked(), Empty: len(st.eventRing) == 0}
	for _, e := range snap.Replay {
		if e.Seq > 0 {
			snap.Floor = e.Seq
			break
		}
	}
	h.mu.Unlock()
	return snap, r, func() {
		h.mu.Lock()
		if cur := h.terminals[terminalKey(AgentKeyID(a.Owner), a.Session)]; cur == st && cur.muxSeqSubs[id] == r {
			delete(cur.muxSeqSubs, id)
		}
		h.mu.Unlock()
		r.mu.Lock()
		r.closed = true
		r.events = nil
		r.bytes = 0
		r.signal()
		r.mu.Unlock()
	}, true
}

// Encoded pending and sent-but-not-applied packets share the SAME 16MiB charge.
type consoleMuxStream struct {
	mu                 sync.Mutex
	pending            map[string][]*consoleMuxPacket
	sent               map[string]map[string]*consoleMuxPacket
	bytes, count       int
	perBytes, perCount map[string]int
	controls           []*consoleMuxPacket
	controlBytes       int
	begun              map[string]bool
	ended              map[string]bool
	high               map[string]uint64
	wake               chan struct{}
}

func newMuxStream() *consoleMuxStream {
	return &consoleMuxStream{pending: map[string][]*consoleMuxPacket{}, sent: map[string]map[string]*consoleMuxPacket{}, perBytes: map[string]int{}, perCount: map[string]int{}, wake: make(chan struct{}, 1), begun: map[string]bool{}, ended: map[string]bool{}, high: map[string]uint64{}}
}
func (s *consoleMuxStream) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *consoleMuxStream) offer(p *consoleMuxPacket) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := p.Meta.AgentID
	if p.Meta.Type != "event" {
		if len(s.controls) >= 64 || s.controlBytes+len(p.Wire) > 256<<10 {
			return false
		}
		s.controls = append(s.controls, p)
		s.controlBytes += len(p.Wire)
		s.signal()
		return true
	}
	if s.count >= 2048 || s.bytes+len(p.Wire) > 16<<20 || s.perBytes[id]+len(p.Wire) > 2<<20 || s.perCount[id] >= 256 {
		return false
	}
	s.pending[id] = append(s.pending[id], p)
	s.bytes += len(p.Wire)
	s.count++
	s.perBytes[id] += len(p.Wire)
	s.perCount[id]++
	s.signal()
	return true
}
func (s *consoleMuxStream) take(id string, credits bool) *consoleMuxPacket {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		if len(s.controls) == 0 {
			return nil
		}
		index := -1
		for i, p := range s.controls {
			blocked := false
			if p.Meta.Type == "replay_end" {
				for _, data := range s.pending[p.Meta.AgentID] {
					n, _ := strconv.ParseUint(data.Meta.Seq, 10, 64)
					high, _ := strconv.ParseUint(p.Meta.H, 10, 64)
					if data.Meta.Generation == p.Meta.Generation && data.Meta.Round == p.Meta.Round && data.Meta.Epoch == p.Meta.Epoch && n <= high {
						blocked = true
						break
					}
				}
			}
			if !blocked {
				index = i
				break
			}
		}
		if index < 0 {
			return nil
		}
		p := s.controls[index]
		s.controls = append(s.controls[:index], s.controls[index+1:]...)
		s.controlBytes -= len(p.Wire)
		return p
	}
	if credits && len(s.pending[id]) > 0 {
		p := s.pending[id][0]
		key := muxPhaseKey(p.Meta)
		if !s.begun[key] {
			return nil
		}
		n, _ := strconv.ParseUint(p.Meta.Seq, 10, 64)
		if !s.ended[key] && n > s.high[key] {
			return nil
		}
	}
	if credits && len(s.sent[id]) >= 16 {
		return nil
	}
	if len(s.pending[id]) == 0 {
		return nil
	}
	p := s.pending[id][0]
	s.pending[id][0] = nil
	s.pending[id] = s.pending[id][1:]
	if credits {
		if s.sent[id] == nil {
			s.sent[id] = map[string]*consoleMuxPacket{}
		}
		key := p.Meta.Epoch + "-" + p.Meta.Seq
		if old, duplicate := s.sent[id][key]; duplicate {
			s.release(old)
		}
		s.sent[id][key] = p
	} else {
		s.release(p)
	}
	return p
}
func (s *consoleMuxStream) release(p *consoleMuxPacket) {
	id := p.Meta.AgentID
	s.bytes -= len(p.Wire)
	s.count--
	s.perBytes[id] -= len(p.Wire)
	s.perCount[id]--
}
func (s *consoleMuxStream) ack(acks []consoleMuxApplied, scopes map[string]*consoleMuxScope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Validate all acknowledgements before releasing any credit.
	for _, a := range acks {
		scope := scopes[a.FullName]
		ep, seq, ok := parseSSEAfter(a.After)
		if scope == nil || scope.Generation != a.Generation || !ok {
			return errors.New("invalid_ack")
		}
		found := false
		for _, p := range s.sent[scope.Agent.ID] {
			nEp, nSeq, _ := parseSSEAfter(p.Meta.Epoch + "-" + p.Meta.Seq)
			if nEp == ep && nSeq == seq {
				found = true
			}
		}
		if !found {
			oldEp, oldSeq, valid := parseSSEAfter(scope.Desired.After)
			if !valid || oldEp != ep || seq > oldSeq {
				return errors.New("future_ack")
			}
		}
	}
	for _, a := range acks {
		scope := scopes[a.FullName]
		ep, seq, _ := parseSSEAfter(a.After)
		for key, p := range s.sent[scope.Agent.ID] {
			nEp, nSeq, _ := parseSSEAfter(key)
			if nEp == ep && nSeq <= seq {
				s.release(p)
				delete(s.sent[scope.Agent.ID], key)
			}
		}
		oldEp, oldSeq, valid := parseSSEAfter(scope.Desired.After)
		if !valid || oldEp != ep || seq > oldSeq {
			scope.Desired.After = a.After
		}
	}
	s.signal()
	return nil
}
func (s *consoleMuxStream) removeAgent(scope *consoleMuxScope) {
	s.mu.Lock()
	for _, p := range s.pending[scope.Agent.ID] {
		s.release(p)
	}
	for _, p := range s.sent[scope.Agent.ID] {
		s.release(p)
	}
	delete(s.pending, scope.Agent.ID)
	delete(s.sent, scope.Agent.ID)
	s.mu.Unlock()
	meta := muxMeta(scope, "agent_revoked")
	p, _ := muxPacket(meta, nil)
	s.offer(p)
}
func (s *consoleMuxStream) clear() {
	s.mu.Lock()
	s.pending = map[string][]*consoleMuxPacket{}
	s.sent = map[string]map[string]*consoleMuxPacket{}
	s.perBytes = map[string]int{}
	s.perCount = map[string]int{}
	s.controls = nil
	s.count = 0
	s.bytes = 0
	s.controlBytes = 0
	s.mu.Unlock()
}

// blocked producers wait, retaining at most one encoder scratch frame each.
func muxEnqueue(ctx context.Context, s *consoleMuxStream, p *consoleMuxPacket) bool {
	for {
		if s.offer(p) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-s.wake:
		case <-time.After(25 * time.Millisecond):
		}
	}
}
func (s *consoleMuxStream) resetAgent(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.pending[id] {
		s.release(p)
	}
	for _, p := range s.sent[id] {
		s.release(p)
	}
	delete(s.pending, id)
	delete(s.sent, id)
	for key := range s.begun {
		if strings.HasPrefix(key, id+"/") {
			delete(s.begun, key)
			delete(s.ended, key)
			delete(s.high, key)
		}
	}
	s.signal()
}
func muxPhaseKey(m consoleMuxWire) string { return m.AgentID + "/" + m.Generation + "/" + m.Round }
func (s *consoleMuxStream) controlWritten(p *consoleMuxPacket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := muxPhaseKey(p.Meta)
	if p.Meta.Type == "replay_begin" {
		s.begun[key] = true
		s.ended[key] = false
		s.high[key], _ = strconv.ParseUint(p.Meta.H, 10, 64)
	}
	if p.Meta.Type == "replay_end" {
		s.ended[key] = true
	}
	s.signal()
}
