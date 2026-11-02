package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"strings"
	"testing"
	"time"
)

func TestMuxWireLimitAndChargedReaders(t *testing.T) {
	meta := consoleMuxWire{Type: "event", AgentID: "a", Epoch: "deadbeef", Seq: "9007199254740993"}
	head, _ := json.Marshal(meta)
	body := []byte(`{"kind":"assistant_text","text":"` + strings.Repeat("x", muxFrameMax-len(head)-19-len(`{"kind":"assistant_text","text":""}`)) + `"}`)
	p, err := muxPacket(meta, body)
	if err != nil || len(p.Wire) != muxFrameMax {
		t.Fatal("F boundary", len(body), err)
	}
	if _, err := muxPacket(meta, append(body[:len(body)-2], []byte(`x"}`)...)); err == nil {
		t.Fatal("F+1 accepted")
	}
	permits := make(chan struct{}, 1)
	count := 0
	if err := muxReadCharged(context.Background(), bytes.NewReader(p.Wire), permits, func(got *consoleMuxPacket) error {
		count++
		if got.Meta.Seq != "9007199254740993" || len(got.Wire) != muxFrameMax {
			t.Fatal("precision/size lost")
		}
		if len(permits) != 1 {
			t.Fatal("scratch not charged")
		}
		return nil
	}); err == nil {
		t.Fatal("expected EOF")
	}
	if count != 1 || len(permits) != 0 {
		t.Fatal("scratch leaked")
	}
}
func TestMuxQueueCreditsAndReplayBarrier(t *testing.T) {
	q := newMuxStream()
	scope := &consoleMuxScope{Agent: consolemux.Agent{ID: "a", FullName: "a.test"}, Generation: "g", Round: 1}
	for i := uint64(1); i <= 20; i++ {
		m := muxMeta(scope, "event")
		m.Epoch = "deadbeef"
		m.Seq = muxSeq(i)
		p, _ := muxPacket(m, []byte(`{"kind":"test"}`))
		if !q.offer(p) {
			t.Fatal("admission")
		}
	}
	begin := muxMeta(scope, "replay_begin")
	begin.Epoch = "deadbeef"
	begin.H = "20"
	b, _ := muxPacket(begin, nil)
	q.offer(b)
	if q.take("a", true) != nil {
		t.Fatal("data overtook begin")
	}
	q.controlWritten(q.take("", false))
	end := muxMeta(scope, "replay_end")
	end.Epoch = "deadbeef"
	end.H = "20"
	p, _ := muxPacket(end, nil)
	q.offer(p)
	if q.take("", false) != nil {
		t.Fatal("end overtook replay")
	}
	for i := 0; i < 16; i++ {
		if q.take("a", true) == nil {
			t.Fatal("missing data")
		}
	}
	if q.take("a", true) != nil {
		t.Fatal("unapplied window exceeded")
	}
	if err := q.ack([]consoleMuxApplied{{FullName: "a.test", Generation: "g", After: "deadbeef-16"}}, map[string]*consoleMuxScope{"a.test": scope}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if q.take("a", true) == nil {
			t.Fatal("credit failed")
		}
	}
	if q.take("", false) == nil {
		t.Fatal("barrier never released")
	}
	if q.count != 4 {
		t.Fatal("pending+inflight charge", q.count)
	}
	if err := q.ack([]consoleMuxApplied{{FullName: "a.test", Generation: "g", After: "deadbeef-21"}}, map[string]*consoleMuxScope{"a.test": scope}); err == nil {
		t.Fatal("future ACK accepted")
	}
	q.resetAgent("a")
	if q.count != 0 || q.bytes != 0 {
		t.Fatal("unsub retained payload")
	}
}
func TestMuxRawByteBackpressure(t *testing.T) {
	r := &consoleMuxReader{epoch: "deadbeef", ready: make(chan struct{}, 1)}
	data := strings.Repeat("x", muxFrameMax)
	r.offer("deadbeef", 1, data)
	r.offer("deadbeef", 2, data)
	r.offer("deadbeef", 3, "x")
	if r.bytes != 2<<20 || len(r.events) != 2 || r.gap != "backpressure" {
		t.Fatal("raw quota", r.bytes, r.gap)
	}
	r.offer("deadbeef", 4, data)
	if r.bytes != 2<<20 {
		t.Fatal("gap kept accepting")
	}
}
func TestMuxRecoveryMetadataExpiresWithoutAnotherRequest(t *testing.T) {
	h, _, _ := newTestHub(t)
	f := &consoleMuxFeed{ID: "f", RecoverUntil: time.Now().Add(20 * time.Millisecond), Scopes: map[string]*consoleMuxScope{"a": {Generation: "g"}}, Requests: map[string]consoleMuxResult{"r": {Status: 200}}}
	m := &consoleMuxRegistry{h: h, feeds: map[string]*consoleMuxFeed{"f": f}}
	m.expireLater(f)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		expired := m.feeds["f"] == nil
		m.mu.Unlock()
		if expired {
			f.mu.Lock()
			empty := f.Scopes == nil && f.Requests == nil
			f.mu.Unlock()
			if !empty {
				t.Fatal("expiry retained metadata")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("registry exceeded retention deadline")
}

// Both queued and physically sent, not-yet-applied events retain the same byte charge.
func TestMuxMaximumFramesPendingAndSentLedger(t *testing.T) {
	q := newMuxStream()
	scopes := map[string]*consoleMuxScope{}
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		s := &consoleMuxScope{Agent: consolemux.Agent{ID: id, FullName: id + ".test"}, Generation: "g", Round: 1}
		scopes[s.Agent.FullName] = s
		begin := muxMeta(s, "replay_begin")
		begin.Epoch = "deadbeef"
		begin.H = "2"
		p, _ := muxPacket(begin, nil)
		if !q.offer(p) {
			t.Fatal("begin")
		}
		q.controlWritten(q.take("", false))
		for seq := uint64(1); seq <= 2; seq++ {
			m := muxMeta(s, "event")
			m.Epoch = "deadbeef"
			m.Seq = muxSeq(seq)
			head, _ := json.Marshal(m)
			body := []byte(`{"kind":"assistant_text","text":"` + strings.Repeat("x", muxFrameMax-len(head)-19-len(`{"kind":"assistant_text","text":""}`)) + `"}`)
			p, e := muxPacket(m, body)
			if e != nil || len(p.Wire) != muxFrameMax || !q.offer(p) {
				t.Fatal("legal F", e)
			}
		}
	}
	if q.bytes != 16<<20 || q.count != 16 {
		t.Fatal("full aggregate charge", q.bytes, q.count)
	}
	rejected, _ := muxPacket(consoleMuxWire{Type: "event", AgentID: "extra", Epoch: "deadbeef", Seq: "1"}, []byte(`{}`))
	if q.offer(rejected) {
		t.Fatal("aggregate byte overrun admitted")
	}
	for _, s := range scopes {
		if q.take(s.Agent.ID, true) == nil || q.take(s.Agent.ID, true) == nil {
			t.Fatal("sent selection")
		}
	}
	if q.bytes != 16<<20 || q.count != 16 {
		t.Fatal("physical send incorrectly released credit")
	}
	acks := []consoleMuxApplied{}
	for name := range scopes {
		acks = append(acks, consoleMuxApplied{FullName: name, Generation: "g", After: "deadbeef-2"})
	}
	if e := q.ack(acks, scopes); e != nil {
		t.Fatal(e)
	}
	if q.bytes != 0 || q.count != 0 {
		t.Fatal("applied did not release charge")
	}
	t.Log("8 seats x 2 exact F: pending=16MiB sent/unapplied=16MiB rejected-overrun=true applied=0")
}

// The callback still runs after the deadline, but sealed lifecycle admission
// must reject its metadata-cleanup job without launching untracked work.
func TestMuxRecoveryExpiryAfterHubShutdownRejectsBackgroundWork(t *testing.T) {
	h, _, _ := newTestHub(t)
	h.Close()
	f := &consoleMuxFeed{ID: "sealed", RecoverUntil: time.Now().Add(20 * time.Millisecond), Scopes: map[string]*consoleMuxScope{"a": {Generation: "g"}}, Requests: map[string]consoleMuxResult{"r": {Status: 200}}}
	m := &consoleMuxRegistry{h: h, feeds: map[string]*consoleMuxFeed{f.ID: f}}
	m.expireLater(f)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		rejected := f.expiryRejected
		retained := f.Scopes != nil && f.Requests != nil
		f.mu.Unlock()
		if rejected {
			m.mu.Lock()
			unchanged := m.feeds[f.ID] == f
			m.mu.Unlock()
			if !retained || !unchanged || h.LifecycleCounts()["background_mux_timer"] != 0 {
				t.Fatal("MUX_TIMER_AFTER_SEAL: rejected timer started metadata work")
			}
			t.Log("MUX_TIMER_AFTER_SEAL: timer_rejected=true background_mux_timer=0 metadata_unchanged=true")
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("MUX_TIMER_AFTER_SEAL: expiry callback did not record admission refusal")
}
