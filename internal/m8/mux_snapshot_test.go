package m8

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestMuxSnapshotCutAndCleanupIsolation(t *testing.T) {
	h, _, _ := newTestHub(t)
	h.publishSessionEvent("k", "s", `{"kind":"test"}`)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			h.publishSessionEvent("k", "s", `{"kind":"test"}`)
		}
	}()
	snap, ok := h.subscribeExistingSessionEventsSeqSnapshot("k", "s", "reader")
	if !ok {
		t.Fatal("missing")
	}
	wg.Wait()
	seen := map[uint64]bool{}
	for _, e := range snap.Replay {
		if e.Seq > snap.H {
			t.Fatal("snapshot passed its cut")
		}
		seen[e.Seq] = true
	}
	for {
		select {
		case e := <-snap.Live:
			seen[e.Seq] = true
		default:
			goto drained
		}
	}
drained:
	for i := uint64(1); i <= 101; i++ {
		if !seen[i] {
			t.Fatalf("cut lost event %d", i)
		}
	}
	newer, ok := h.subscribeExistingSessionEventsSeqSnapshot("k", "s", "reader")
	if !ok {
		t.Fatal("missing newer")
	}
	snap.Unsub()
	h.publishSessionEvent("k", "s", `{"kind":"test"}`)
	select {
	case <-newer.Live:
	default:
		t.Fatal("old cleanup removed new reader")
	}
	newer.Unsub()
}
func TestMuxReplayAdmissionBoundaries(t *testing.T) {
	s := sessionSeqSnapshot{Epoch: "deadbeef", H: 0, Empty: true}
	if _, gap, err := selectMuxReplay(s, "deadbeef-0", true); gap != "" || err != nil {
		t.Fatal(gap, err)
	}
	if _, _, err := selectMuxReplay(s, "deadbeef-1", true); !errors.Is(err, errMuxCursorAhead) {
		t.Fatal("ahead accepted")
	}
	s.H = 8
	if _, gap, err := selectMuxReplay(s, "deadbeef-0", true); gap != "history_unavailable" || err != nil {
		t.Fatal(gap, err)
	}
	s.Empty = false
	s.Floor = 6
	s.Replay = []sessionSeqEvent{{Seq: 6}, {Seq: 7}, {Seq: 8}}
	if out, gap, err := selectMuxReplay(s, "deadbeef-5", true); len(out) != 3 || gap != "" || err != nil {
		t.Fatal("floor-1", gap, err)
	}
	if _, gap, _ := selectMuxReplay(s, "deadbeef-4", true); gap != "overflow" {
		t.Fatal("overflow hidden")
	}
	if _, _, err := selectMuxReplay(s, "deadbeef-18446744073709551616", true); !errors.Is(err, errMuxInvalidCursor) {
		t.Fatal("uint64 overflow accepted")
	}
}

func TestMuxRolloverStopsOnlyManagedAdmission(t *testing.T) {
	s := sessionSeqSnapshot{Epoch: "deadbeef", H: math.MaxUint64 - 1}
	if _, gap, err := selectMuxReplay(s, "", false); gap != "seq_exhausted" || err != nil {
		t.Fatal("rollover did not stop managed seat", gap, err)
	}
}
