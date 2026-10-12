package m8

import (
	"context"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"strings"
	"sync"
	"testing"
)

func ringSnapshot(t *testing.T, h *Hub, name string) sessionSeqSnapshot {
	t.Helper()
	s, ok := h.subscribeExistingSessionEventsSeqSnapshot("ring", name, "check")
	if !ok {
		t.Fatal("snapshot missing")
	}
	s.Unsub()
	return s
}
func TestRingPerAgentSuffixOversizedAndLiveBody(t *testing.T) {
	h, _, _ := newTestHub(t)
	body := strings.Repeat("界", (1<<20)/3)
	for i := 0; i < 20; i++ {
		h.publishSessionEvent("ring", "one", body)
	}
	s := ringSnapshot(t, h, "one")
	if len(s.Replay) != 16 || s.Floor != 5 || s.H != 20 || s.LostThrough != 4 || !s.HistoryTruncated {
		t.Fatalf("suffix %+v", s)
	}
	if _, gap, e := selectMuxReplay(s, s.Epoch+"-3", true); e != nil || gap != "overflow" {
		t.Fatal("managed overflow", gap, e)
	}
	if _, gap, e := selectMuxReplay(s, s.Epoch+"-4", true); e != nil || gap != "" {
		t.Fatal("floor-1 rejected", gap, e)
	}
	if _, gap := selectLegacySnapshotReplay(s, s.Epoch+"-3", true); gap != "overflow" {
		t.Fatal("legacy overflow", gap)
	}
	if _, gap := selectLegacySnapshotReplay(s, "", false); gap != "overflow" {
		t.Fatal("fresh independent view must disclose truncated history")
	}
	live, ok := h.subscribeExistingSessionEventsSeqSnapshot("ring", "one", "live")
	if !ok {
		t.Fatal("live")
	}
	defer live.Unsub()
	huge := strings.Repeat("z", terminalEventRingBytes+1)
	h.publishSessionEvent("ring", "one", huge)
	got := <-live.Live
	if got.Seq != 21 || got.Data != huge {
		t.Fatal("live body/seq changed")
	}
	empty := ringSnapshot(t, h, "one")
	if !empty.Empty || empty.H != 21 || empty.Floor != 0 || empty.Epoch != s.Epoch || empty.LostThrough != 21 {
		t.Fatal("oversized not an explicit empty history cut")
	}
	if _, gap, _ := selectMuxReplay(empty, empty.Epoch+"-20", true); gap != "history_unavailable" {
		t.Fatal("empty hole hidden", gap)
	}
	if _, gap, _ := selectMuxReplay(empty, "", false); gap != "history_unavailable" {
		t.Fatal("empty fresh hole hidden", gap)
	}
	if _, gap := selectLegacySnapshotReplay(empty, empty.Epoch+"-20", true); gap != "overflow" {
		t.Fatal("legacy empty hole hidden", gap)
	}
	h.publishSessionEvent("ring", "one", "new-suffix")
	next := ringSnapshot(t, h, "one")
	if next.Floor != 22 || next.H != 22 || next.Replay[0].Data != "new-suffix" {
		t.Fatal("suffix resumed with internal hole")
	}
}
func TestRingGlobalFIFODeletionAndConcurrentCuts(t *testing.T) {
	h, _, _ := newTestHub(t)
	body := strings.Repeat("x", 1<<20)
	for a := 0; a < 9; a++ {
		for i := 0; i < 16; i++ {
			h.publishSessionEvent("ring", fmt.Sprint(a), body)
		}
	}
	h.mu.Lock()
	if h.retainedRingBytes != hubEventRingBytes || h.retainedRingFIFO.Len() != 128 {
		t.Fatal("global charge", h.retainedRingBytes, h.retainedRingFIFO.Len())
	}
	h.mu.Unlock()
	old := ringSnapshot(t, h, "0")
	if !old.Empty || old.H != 16 || old.LostThrough != 16 {
		t.Fatal("global FIFO did not cut oldest terminal")
	}
	var wg sync.WaitGroup
	for a := 0; a < 9; a++ {
		wg.Add(1)
		go func(a int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				h.publishSessionEvent("ring", fmt.Sprint(a), body)
				s := ringSnapshot(t, h, fmt.Sprint(a))
				for j := 1; j < len(s.Replay); j++ {
					if s.Replay[j].Seq != s.Replay[j-1].Seq+1 {
						t.Error("noncontiguous retained suffix")
					}
				}
			}
		}(a)
	}
	wg.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	sum, count := 0, 0
	for _, st := range h.terminals {
		if st.eventRingBytes > terminalEventRingBytes || len(st.eventRing) > 1000 {
			t.Error("per-terminal cap")
		}
		for _, s := range st.eventRing {
			sum += len(s)
		}
		count += len(st.eventRing)
	}
	if sum != h.retainedRingBytes || sum > hubEventRingBytes || count != h.retainedRingFIFO.Len() {
		t.Fatal("atomic global accounting")
	}
	for a := 0; a < 9; a++ {
		h.closeTerminalLocked("ring", fmt.Sprint(a))
	}
	if h.retainedRingBytes != 0 || h.retainedRingFIFO.Len() != 0 {
		t.Fatal("deletion retained payload")
	}
	t.Log("same-lock global FIFO <=128MiB, per-Agent<=16MiB, contiguous suffix, concurrent snapshots, terminal cleanup=0")
}
func TestRingExistingCountAndMetadataCap(t *testing.T) {
	h, _, _ := newTestHub(t)
	for i := 0; i < 1001; i++ {
		h.publishSessionEvent("ring", "count", "x")
	}
	s := ringSnapshot(t, h, "count")
	if len(s.Replay) != 1000 || s.Floor != 2 {
		t.Fatal("original count cap changed")
	}
	h.mu.Lock()
	h.closeTerminalLocked("ring", "count")
	h.mu.Unlock()
	for a := 0; a < 129; a++ {
		for i := 0; i < 1000; i++ {
			h.publishSessionEvent("ring", fmt.Sprint(a), "")
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.retainedRingFIFO.Len() != hubEventRingEntries || h.retainedRingBytes != 0 {
		t.Fatal("zero-length metadata bound")
	}
	if len(h.terminals[terminalKey("ring", "0")].eventRing) != 0 {
		t.Fatal("metadata FIFO not oldest first")
	}
}

func TestRingTerminalDeleteStopsOldMuxEpochAndFreesRawPayload(t *testing.T) {
	h, _, _ := newTestHub(t)
	a := consolemux.Agent{Owner: "owner", Session: "one", ID: "one", FullName: "one.owner"}
	key := AgentKeyID(a.Owner)
	h.publishSessionEvent(key, a.Session, "old")
	old, reader, unsub, ok := h.subscribeConsoleMuxSource(a, "same")
	if !ok {
		t.Fatal("subscribe")
	}
	defer unsub()
	h.publishSessionEvent(key, a.Session, "pending")
	h.mu.Lock()
	h.closeTerminalLocked(key, a.Session)
	h.mu.Unlock()
	reader.mu.Lock()
	if reader.bytes != 0 || len(reader.events) != 0 || !reader.closed {
		t.Fatal("raw payload remains after epoch deletion")
	}
	reader.mu.Unlock()
	_, gap, available := reader.next(context.Background())
	if !available || gap != "epoch_changed" {
		t.Fatal("old source did not get visible epoch cut", gap)
	}
	h.publishSessionEvent(key, a.Session, "new")
	next, r2, u2, ok := h.subscribeConsoleMuxSource(a, "same")
	if !ok {
		t.Fatal("replacement")
	}
	defer u2()
	if next.Epoch == old.Epoch || next.H != 1 {
		t.Fatal("replacement epoch/seq changed incorrectly")
	}
	unsub()
	h.mu.Lock()
	current := h.terminals[terminalKey(key, a.Session)].muxSeqSubs["same"]
	h.mu.Unlock()
	if current != r2 {
		t.Fatal("old cleanup erased new source")
	}
}
