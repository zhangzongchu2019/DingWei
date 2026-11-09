package m8

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type consoleMuxBridge struct {
	Active         bool
	ID, TargetName string
	Target         ViewTarget
	Scopes         map[string]*consoleMuxScope
}

func muxScopes(f *consoleMuxFeed) (map[string]*consoleMuxScope, consolemux.Owner) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]*consoleMuxScope{}
	for k, s := range f.Scopes {
		c := *s
		out[k] = &c
	}
	return out, f.Owner
}
func muxNames(scopes map[string]*consoleMuxScope) []string {
	n := make([]string, 0, len(scopes))
	for k := range scopes {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// One cancellation tree owns the public writer, target bridges, readers and payloads.
func (h *Hub) HandleConsoleMuxEvents(w http.ResponseWriter, r *http.Request) {
	r, finish, admitted := h.streamRequest(w, r, "public_mux")
	if !admitted {
		return
	}
	defer finish()
	m := h.consoleMux
	if m == nil {
		http.NotFound(w, r)
		return
	}
	a, hash, _, ok := m.actor(w, r, nil)
	if !ok {
		return
	}
	f := m.feed(w, r, hash, a.Principal)
	if f == nil {
		return
	}
	f.mu.Lock()
	if f.Closed || f.Cancel != nil {
		f.mu.Unlock()
		writeAgentWSError(w, 409, "console_in_use")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	done := make(chan struct{})
	if f.expiryTimer != nil {
		f.expiryTimer.Stop()
		f.expiryTimer = nil
	}
	f.Cancel = cancel
	f.Done = done
	owner := f.Owner
	f.mu.Unlock()
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
		f.mu.Lock()
		if f.Gate != nil {
			f.Gate.Revoke("")
		}
		if f.Stream != nil {
			f.Stream.clear()
		}
		f.mu.Unlock()
		// Release before making this registry available for a new transport. Otherwise an
		// old close could CAS-release the very owner reused by a newly admitted writer.
		releaseCtx, stop := context.WithTimeout(context.Background(), time.Second)
		_ = m.slots.Release(releaseCtx, owner)
		stop()
		f.mu.Lock()
		f.Stream = nil
		f.Bridges = nil
		f.Gate = nil
		f.Owner = consolemux.Owner{}
		f.Scratch = nil
		f.Cancel = nil
		f.Done = nil
		f.RecoverUntil = time.Now().Add(time.Minute)
		f.mu.Unlock()
		m.expireLater(f)
		close(done)
	}()
	var err error
	owner, err = m.slots.Check(ctx, owner)
	if err != nil { // A disconnected feed reacquires; it cannot renew an expired incarnation.
		owner, err = consolemux.NewOwner(f.Principal, m.origin, f.Session, f.Instance)
		if err == nil {
			owner, err = m.slots.Acquire(ctx, owner)
		}
	}
	if err != nil {
		writeAgentWSError(w, 409, "console_in_use")
		return
	}
	scopes, _ := muxScopes(f)
	auth, start, err := m.authorize(ctx, f.Session, muxNames(scopes))
	if err != nil {
		writeAgentWSError(w, 401, "authorization_unavailable")
		return
	}
	gate := consolemux.NewGate(owner)
	grants := map[string]string{}
	for name, s := range scopes {
		if actual, ok := auth.Agents[name]; ok && actual.ID == s.Agent.ID {
			grants[s.Agent.ID] = s.Generation
		}
	}
	if !gate.CommitCheck(gate.Version(), start, auth.Expiry, owner, grants) {
		writeAgentWSError(w, 503, "authorization_expired")
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(consolemux.WriteLimit)); err != nil {
		writeAgentWSError(w, 503, "write_deadline_unavailable")
		return
	}
	f.mu.Lock()
	if f.Closed {
		f.mu.Unlock()
		return
	}
	f.Owner = owner
	f.Gate = gate
	f.Stream = newMuxStream()
	f.Scratch = make(chan struct{}, 1)
	f.Bridges = map[string]*consoleMuxBridge{}
	stream := f.Stream
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	if gate.Write(w, "", "", []byte(": console-mux/v1\n\n")) != nil {
		return
	}
	workers.Add(3)
	go func() { defer workers.Done(); m.keepFeed(ctx, cancel, f) }()
	go func() { defer workers.Done(); m.renewFeed(ctx, cancel, f) }()
	go func() { defer workers.Done(); m.manageBridges(ctx, cancel, f) }()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		// Do not dequeue data while a slow fresh check has temporarily expired the lease.
		if gate.Ready("", "") {
			if p := stream.take("", false); p != nil {
				current, _ := muxScopes(f)
				sc := current[p.Meta.FullName]
				if p.Meta.Type == "agent_revoked" || p.Meta.Type == "gap" {
					if gate.Write(w, "", "", p.Wire) != nil {
						return
					}
				} else if sc != nil && sc.Generation == p.Meta.Generation && muxSeq(sc.Round) == p.Meta.Round && gate.Ready(sc.Agent.ID, sc.Generation) {
					if gate.Write(w, sc.Agent.ID, sc.Generation, p.Wire) != nil {
						return
					}
					stream.controlWritten(p)
				}
			}
			current, _ := muxScopes(f)
			for _, name := range muxNames(current) {
				s := current[name]
				n, b := 0, 0
				for n < 16 && b < 64<<10 && gate.Ready(s.Agent.ID, s.Generation) {
					p := stream.take(s.Agent.ID, true)
					if p == nil {
						break
					}
					if p.Meta.Generation != s.Generation || p.Meta.Round != muxSeq(s.Round) {
						stream.dropSent(p)
						continue
					}
					if gate.Write(w, s.Agent.ID, s.Generation, p.Wire) != nil {
						return
					}
					n++
					b += len(p.Wire)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-stream.wake:
		case <-heartbeat.C:
			if gate.Ready("", "") {
				if gate.Write(w, "", "", []byte(": heartbeat\n\n")) != nil {
					return
				}
			}
		}
	}
}
func (m *consoleMuxRegistry) keepFeed(ctx context.Context, cancel context.CancelFunc, f *consoleMuxFeed) {
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		scopes, owner := muxScopes(f)
		f.mu.Lock()
		gate := f.Gate
		f.mu.Unlock()
		if gate == nil {
			return
		}
		version := gate.Version()
		round, c := context.WithTimeout(ctx, time.Second)
		auth, start, err := m.authorize(round, f.Session, muxNames(scopes))

		c()
		if err != nil {
			gate.FailCheck()
			cancel()
			return
		}
		_, owner = muxScopes(f)
		grants := map[string]string{}
		for name, s := range scopes {
			if actual, ok := auth.Agents[name]; ok && actual.ID == s.Agent.ID {
				grants[s.Agent.ID] = s.Generation
			} else {
				m.h.revokeConsoleMuxAgent(s.Agent.ID)
			}
		}
		if !gate.CommitCheck(version, start, auth.Expiry, owner, grants) { // A control update invalidated this result; no stale grant is installed.
			if ctx.Err() != nil {
				return
			}
		}

	}
}
func (m *consoleMuxRegistry) renewFeed(ctx context.Context, cancel context.CancelFunc, f *consoleMuxFeed) {
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		_, owner := muxScopes(f)
		round, stop := context.WithTimeout(ctx, 400*time.Millisecond)
		next, err := m.slots.Renew(round, owner)
		stop()
		f.mu.Lock()
		gate := f.Gate
		f.mu.Unlock()
		if err != nil || gate == nil || !gate.UpdateSlot(next) {
			if gate != nil {
				gate.Revoke("")
			}
			cancel()
			return
		}
		f.mu.Lock()
		f.Owner = next
		f.mu.Unlock()
	}
}
func (m *consoleMuxRegistry) manageBridges(ctx context.Context, cancel context.CancelFunc, f *consoleMuxFeed) {
	type run struct {
		key    string
		cancel context.CancelFunc
		done   chan struct{}
	}
	running := map[string]run{}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	defer func() {
		for _, r := range running {
			r.cancel()
		}
		for _, r := range running {
			<-r.done
		}
	}()
	for {
		scopes, _ := muxScopes(f)
		groups := map[string]map[string]*consoleMuxScope{}
		for name, s := range scopes {
			if groups[s.TargetName] == nil {
				groups[s.TargetName] = map[string]*consoleMuxScope{}
			}
			groups[s.TargetName][name] = s
		}
		for target, r := range running {
			g := groups[target]
			key := muxGroupKey(g)
			if key != r.key {
				r.cancel()
				<-r.done
				delete(running, target)
			}
		}
		for target, g := range groups {
			if _, exists := running[target]; exists {
				continue
			}
			id, err := muxID()
			if err != nil {
				cancel()
				return
			}
			var sample *consoleMuxScope
			for _, s := range g {
				sample = s
				break
			}
			bridge := &consoleMuxBridge{ID: id, TargetName: target, Target: sample.Target, Scopes: g}
			f.mu.Lock()
			f.Bridges[id] = bridge
			stream := f.Stream
			f.mu.Unlock()
			bc, stop := context.WithCancel(ctx)
			done := make(chan struct{})
			running[target] = run{muxGroupKey(g), stop, done}
			go func() {
				defer close(done)
				defer func() { f.mu.Lock(); delete(f.Bridges, id); f.mu.Unlock() }()
				var bridgeErr error
				for attempt := 0; attempt < 3; attempt++ {
					if attempt > 0 {
						select {
						case <-bc.Done():
							return
						case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
						}
						current, _ := muxScopes(f)
						f.mu.Lock()
						for name, sc := range bridge.Scopes {
							if next := current[name]; next != nil && next.Generation == sc.Generation && next.Round == sc.Round {
								sc.Desired.After = next.Desired.After
							}
						}
						f.mu.Unlock()
					}
					bridgeErr = m.runBridge(bc, f, bridge, stream)
					if bc.Err() != nil {
						return
					}
				}
				if bridgeErr != nil && bc.Err() == nil {
					for _, sc := range bridge.Scopes {
						f.mu.Lock()
						current := f.Scopes[sc.Agent.FullName]
						valid := current != nil && current.Generation == sc.Generation && current.Round == sc.Round
						if valid {
							f.retireScopeLocked(sc.Agent.FullName, current)
							delete(f.Scopes, sc.Agent.FullName)
							f.Revision++
							if f.Gate != nil {
								f.Gate.Revoke(sc.Agent.ID)
							}
						}
						f.mu.Unlock()
						if valid {
							stream.resetAgent(sc.Agent.ID)
							meta := muxMeta(sc, "gap")
							meta.Reason = "target_unavailable"
							p, _ := muxPacket(meta, nil)
							if !stream.offer(p) {
								cancel()
								return
							}
						}
					}
					select {
					case f.Changed <- struct{}{}:
					default:
					}
				}
			}()
		}
		select {
		case <-ctx.Done():
			return
		case <-f.Changed:
		case <-tick.C:
		}
	}
}
func muxGroupKey(g map[string]*consoleMuxScope) string {
	var b strings.Builder
	for _, n := range muxNames(g) {
		s := g[n]
		b.WriteString(n + "/" + s.Generation + "/" + muxSeq(s.Round) + ";")
	}
	return b.String()
}
func muxSignedRequest(ctx context.Context, base, path, direction, target, file string, body []byte) (*http.Request, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("invalid bridge endpoint")
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = ""
	u.Fragment = ""
	key, err := os.ReadFile(file)
	if err != nil || len(bytes.TrimSpace(key)) < 32 {
		return nil, errors.New("bridge secret unavailable")
	}
	nonce, err := muxID()
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DW-VS-Target", target)
	req.Header.Set("X-DW-Timestamp", ts)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(bytes.TrimSpace(key), direction, "POST", u.Path, ts, nonce, body))
	return req, nil
}
func (m *consoleMuxRegistry) runBridge(ctx context.Context, f *consoleMuxFeed, b *consoleMuxBridge, s *consoleMuxStream) error {
	body, _ := json.Marshal(map[string]string{"feed_id": f.ID, "bridge_id": b.ID, "target": b.TargetName})
	req, err := muxSignedRequest(ctx, b.Target.URL, "/internal/console-mux/events", hubToVSDirection, b.TargetName, b.Target.HubToVSSecretFile, body)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: time.Second}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("bridge rejected")
	}
	return muxReadCharged(ctx, resp.Body, f.Scratch, func(p *consoleMuxPacket) error {
		meta := p.Meta
		sc := b.Scopes[meta.FullName]
		if sc == nil || sc.Agent.RootID != meta.RootID || sc.Agent.NodeID != meta.NodeID || sc.Agent.LaneGeneration != meta.LaneGeneration || sc.Agent.ID != meta.AgentID || sc.Generation != meta.Generation || sc.Desired.Instance != meta.Instance || muxSeq(sc.Round) != meta.Round {
			return errors.New("bridge scope mismatch")
		}
		if meta.Type == "event" {
			ep, n, ok := parseSSEAfter(meta.Epoch + "-" + meta.Seq)
			if !ok || ep != meta.Epoch || muxSeq(n) != meta.Seq {
				return errors.New("invalid bridge cursor")
			}
		}
		current, _ := muxScopes(f)
		active := current[meta.FullName]
		if active == nil || active.Generation != meta.Generation || active.Round != sc.Round {
			return nil
		}
		if s.offer(p) {
			return nil
		}
		if meta.Type != "event" {
			return errors.New("control_backpressure")
		}
		// Do not let a full seat hold a shared encoder or stall another target.
		f.mu.Lock()
		currentScope := f.Scopes[meta.FullName]
		if currentScope == nil || currentScope.Generation != sc.Generation || currentScope.Round != sc.Round {
			f.mu.Unlock()
			return nil
		}
		f.retireScopeLocked(meta.FullName, currentScope)
		delete(f.Scopes, meta.FullName)
		f.Revision++
		if f.Gate != nil {
			f.Gate.Revoke(meta.AgentID)
		}
		f.mu.Unlock()
		s.resetAgent(meta.AgentID)
		gap := muxMeta(sc, "gap")
		gap.Reason = "backpressure"
		control, err := muxPacket(gap, nil)
		if err != nil || !s.offer(control) {
			return errors.New("control_backpressure")
		}
		select {
		case f.Changed <- struct{}{}:
		default:
		}
		return nil
	})
}

// A small prefix buffer detects an active target before reserving one frame-sized
// scratch permit. Idle targets cannot monopolize the two-per-feed permits.
func muxReadCharged(ctx context.Context, r io.Reader, permits chan struct{}, consume func(*consoleMuxPacket) error) error {
	reader := bufio.NewReaderSize(r, 512)
	for {
		prefix, err := reader.ReadSlice('\n')
		if err != nil && err != bufio.ErrBufferFull {
			return err
		}
		if len(prefix) == 1 && prefix[0] == '\n' {
			continue
		}
		if !bytes.HasPrefix(prefix, []byte("data: ")) {
			if err == bufio.ErrBufferFull {
				return errors.New("invalid control line")
			}
			continue
		}
		select {
		case permits <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		frame := make([]byte, 0, muxFrameMax)
		frame = append(frame, prefix...)
		for err == bufio.ErrBufferFull {
			var fragment []byte
			fragment, err = reader.ReadSlice('\n')
			if len(frame)+len(fragment) > muxFrameMax-1 {
				<-permits
				return errors.New("event_too_large")
			}
			frame = append(frame, fragment...)
		}
		if err != nil {
			<-permits
			return err
		}
		blank, e := reader.ReadSlice('\n')
		if e != nil || !bytes.Equal(blank, []byte("\n")) || len(frame)+1 > muxFrameMax {
			<-permits
			return errors.New("invalid complete frame")
		}
		frame = append(frame, '\n')
		var meta consoleMuxWire
		e = json.Unmarshal(frame[6:len(frame)-2], &meta)
		if e == nil {
			owned := append([]byte(nil), frame...)
			e = consume(&consoleMuxPacket{Meta: meta, Wire: owned})
		}
		<-permits
		if e != nil {
			return e
		}
	}
}

// Reads a complete frame with a fixed cap; no ReadAll or unbounded readline on a stream.
func muxReadPackets(r io.Reader, consume func([]byte) error) error {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), muxFrameMax)
	var data []byte
	size := 0
	for scan.Scan() {
		line := scan.Bytes()
		size += len(line) + 1
		if size > muxFrameMax {
			return errors.New("event_too_large")
		}
		if len(line) == 0 {
			if len(data) > 0 {
				if err := consume(data); err != nil {
					return err
				}
			}
			data = nil
			size = 0
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			if len(data) != 0 {
				return errors.New("multiline mux frame")
			}
			data = append([]byte(nil), line[6:]...)
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	return io.EOF
}

// VS is the only consumer permitted to open this feed's raw source bridge.
func (h *Hub) HandleInternalConsoleMuxEvents(w http.ResponseWriter, r *http.Request) {
	r, finish, admitted := h.streamRequest(w, r, "internal_bridge")
	if !admitted {
		return
	}
	defer finish()
	m := h.consoleMux
	if m == nil {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	f := m.feeds[r.PathValue("feed_id")]
	m.mu.Unlock()
	if f == nil {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	b := f.Bridges[r.PathValue("bridge_id")]
	gate := f.Gate
	f.mu.Unlock()
	if b == nil || gate == nil {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	key, err := os.ReadFile(b.Target.VSToHubSecretFile)
	ts, nonce := r.Header.Get("X-DW-Timestamp"), r.Header.Get("X-DW-Nonce")
	n, e := strconv.ParseInt(ts, 10, 64)
	want := signViewServiceRequest(bytes.TrimSpace(key), vsToHubDirection, r.Method, r.URL.Path, ts, nonce, body)
	if err != nil || len(bytes.TrimSpace(key)) < 32 || e != nil || time.Since(time.Unix(n, 0)).Abs() > time.Minute || !muxValidID(nonce) || r.Header.Get("X-DW-VS-Target") != b.TargetName || !hmac.Equal([]byte(want), []byte(r.Header.Get("X-DW-Signature"))) {
		http.Error(w, "unauthorized", 401)
		return
	}
	if !h.claimViewV2Nonce(b.TargetName, nonce, time.Now()) {
		http.Error(w, "replayed", 409)
		return
	}
	var claim struct {
		Target string `json:"target"`
	}
	if json.Unmarshal(body, &claim) != nil || claim.Target != b.TargetName {
		http.Error(w, "target mismatch", 400)
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(consolemux.WriteLimit)); err != nil {
		http.Error(w, "deadline unavailable", 503)
		return
	}
	f.mu.Lock()
	if f.Bridges[b.ID] != b || b.Active {
		f.mu.Unlock()
		http.Error(w, "bridge already active", 409)
		return
	}
	b.Active = true
	f.mu.Unlock()
	defer func() { f.mu.Lock(); b.Active = false; f.mu.Unlock() }()
	f.mu.Lock()
	copyBridge := *b
	copyBridge.Scopes = map[string]*consoleMuxScope{}
	for name, sc := range b.Scopes {
		copy := *sc
		copyBridge.Scopes[name] = &copy
	}
	f.mu.Unlock()
	h.serveMuxSources(w, r, f, &copyBridge, gate)
}

// All eight sources share one encoder and writer; raw payloads retain their per-seat charge until selected.
func (h *Hub) serveMuxSources(w http.ResponseWriter, r *http.Request, f *consoleMuxFeed, b *consoleMuxBridge, gate *consolemux.Gate) {
	type src struct {
		s       *consoleMuxScope
		snap    sessionSeqSnapshot
		reader  *consoleMuxReader
		unsub   func()
		replay  []sessionSeqEvent
		phase   int
		stopped bool
		gap     string
		window  muxSourceWindow
	}
	sources := []*src{}
	defer func() {
		for _, v := range sources {
			v.unsub()
		}
	}()
	for _, name := range muxNames(b.Scopes) {
		s := b.Scopes[name]
		snap, reader, unsub, ok := h.subscribeConsoleMuxSource(s.Agent, b.ID+"/"+s.Agent.ID)
		if !ok {
			return
		}
		replay, gap, err := selectMuxReplay(snap, s.Desired.After, s.Desired.After != "")
		// Keep only the selected replay ownership, not an additional complete
		// snapshot reference after its cut metadata has been extracted.
		snap.Replay = nil
		v := &src{s: s, snap: snap, reader: reader, unsub: unsub, replay: replay}
		sources = append(sources, v)
		if err != nil || gap != "" {
			v.phase = -1
			v.gap = gap
			if err != nil {
				v.gap = err.Error()
			}
			v.replay = nil
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(s *consoleMuxScope, meta consoleMuxWire, payload string) bool {
		head, err := json.Marshal(meta)
		if err != nil {
			return false
		}
		if meta.Type != "event" {
			p, err := muxPacket(meta, nil)
			return err == nil && gate.Write(w, s.Agent.ID, s.Generation, p.Wire) == nil
		}
		prefix := append([]byte("data: "), head[:len(head)-1]...)
		prefix = append(prefix, []byte(",\"payload\":")...)
		if len(prefix)+len(payload)+3 > muxFrameMax {
			return false
		}
		return gate.WriteParts(w, s.Agent.ID, s.Generation, prefix, payload, []byte("}\n\n")) == nil
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	wm := time.NewTicker(15 * time.Second)
	defer wm.Stop()
	for {
		if gate.Terminated() {
			return
		}
		f.mu.Lock()
		registered := f.Bridges[b.ID] != nil
		f.mu.Unlock()
		if !registered {
			return
		}
		for _, v := range sources {
			if v.stopped || !gate.Ready(v.s.Agent.ID, v.s.Generation) {
				continue
			}
			meta := muxMeta(v.s, "replay_begin")
			meta.Epoch = v.snap.Epoch
			meta.Floor = muxSeq(v.snap.Floor)
			meta.H = muxSeq(v.snap.H)
			meta.Empty = v.snap.Empty
			if v.phase == -1 {
				meta.Type = "gap"
				meta.Reason = "resync_required"
				meta.Cause = v.gap
				if !write(v.s, meta, "") {
					return
				}
				v.stopped = true
				continue
			}
			if v.phase == 0 {
				if !write(v.s, meta, "") {
					return
				}
				v.phase = 1
			}
			count, total := 0, 0
			for count < 16 && total < 64<<10 {
				var ev sessionSeqEvent
				var gap string
				has := false
				live := false
				if len(v.replay) > 0 {
					ev = v.replay[0]
					has = true
				} else if v.phase == 1 {
					meta.Type = "replay_end"
					if !write(v.s, meta, "") {
						return
					}
					v.phase = 2
				} else {
					v.reader.mu.Lock()
					if len(v.reader.events) > 0 {
						ev = v.reader.events[0]
						live = true
						has = true
					} else if v.reader.gap != "" {
						gap = v.reader.gap
						v.reader.gap = ""
					}
					v.reader.mu.Unlock()
				}
				if gap != "" {
					meta.Type = "gap"
					meta.Reason = "resync_required"
					meta.Cause = gap
					if !write(v.s, meta, "") {
						return
					}
					v.stopped = true
					break
				}
				if !has {
					break
				}
				if v.phase == 2 && ev.Seq <= v.snap.H {
					if live {
						v.reader.mu.Lock()
						if len(v.reader.events) > 0 {
							v.reader.events[0] = sessionSeqEvent{}
							v.reader.events = v.reader.events[1:]
							v.reader.bytes -= len(ev.Data)
						}
						v.reader.mu.Unlock()
					}
					continue
				}
				if ev.Seq == 0 {
					meta.Type = "gap"
					meta.Reason = "legacy_unsequenced"
					if !write(v.s, meta, "") {
						return
					}
					v.stopped = true
					break
				}
				// Event metadata must not inherit replay floor/H/empty. Those
				// control-only fields would reduce the legal maximum payload.
				eventMeta := muxMeta(v.s, "event")
				eventMeta.Epoch = v.snap.Epoch
				eventMeta.Seq = muxSeq(ev.Seq)
				header, _ := json.Marshal(eventMeta)
				if len(header)+len(ev.Data)+19 > muxFrameMax {
					meta.Type = "gap"
					meta.Reason = "event_too_large"
					if !write(v.s, meta, "") {
						return
					}
					v.stopped = true
					v.unsub()
					break
				}
				// Pace at the raw source before encoding into a shared VS
				// bridge. A large valid retained replay must not flood the
				// public queue and be mistaken for a live producer overrun.
				f.mu.Lock()
				current := f.Scopes[v.s.Agent.FullName]
				applied := ""
				if current != nil && current.Generation == v.s.Generation && current.Round == v.s.Round {
					applied = current.Desired.After
				}
				f.mu.Unlock()
				ep, confirmed, valid := parseSSEAfter(applied)
				if valid && ep == v.snap.Epoch {
					v.window.confirm(confirmed)
				}
				wireBytes := len(header) + len(ev.Data) + 19
				if !v.window.ready(wireBytes) {
					break
				}
				if !write(v.s, eventMeta, ev.Data) {
					return
				}
				v.window.sent(ev.Seq, wireBytes)
				if !live {
					v.replay[0] = sessionSeqEvent{}
					v.replay = v.replay[1:]
				}
				if live {
					v.reader.mu.Lock()
					if len(v.reader.events) > 0 && v.reader.events[0].Seq == ev.Seq {
						v.reader.events[0] = sessionSeqEvent{}
						v.reader.events = v.reader.events[1:]
						v.reader.bytes -= len(ev.Data)
					}
					v.reader.mu.Unlock()
				}
				count++
				total += len(ev.Data)
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		case <-wm.C:
			for _, v := range sources {
				if !v.stopped && gate.Ready(v.s.Agent.ID, v.s.Generation) {
					snap, ok := h.consoleMuxSnapshot(v.s.Agent)
					if !ok {
						return
					}
					meta := muxMeta(v.s, "watermark")
					meta.Epoch = snap.Epoch
					meta.H = muxSeq(snap.H)
					if !write(v.s, meta, "") {
						return
					}
				}
			}
		}
	}
}
func (s *consoleMuxStream) dropSent(p *consoleMuxPacket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := p.Meta.Epoch + "-" + p.Meta.Seq
	if s.sent[p.Meta.AgentID][key] == p {
		delete(s.sent[p.Meta.AgentID], key)
		s.release(p)
	}
}
