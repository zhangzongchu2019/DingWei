package m8

import "testing"

func TestMuxSourceReplayWindowRequiresActualConfirmation(t *testing.T) {
	var w muxSourceWindow
	for seq := uint64(1); seq <= 16; seq++ {
		if !w.ready(64) {
			t.Fatal("early stall")
		}
		w.sent(seq, 64)
	}
	if w.ready(64) {
		t.Fatal("source outran its window")
	}
	w.confirm(0)
	if w.ready(64) {
		t.Fatal("no confirmed progress")
	}
	w.confirm(8)
	for seq := uint64(17); seq <= 24; seq++ {
		if !w.ready(64) {
			t.Fatal("confirmation did not release")
		}
		w.sent(seq, 64)
	}
	w.confirm(24)
	if w.bytes != 0 || len(w.entries) != 0 {
		t.Fatal("credit leak")
	}
	w.sent(25, muxFrameMax)
	w.sent(26, muxFrameMax)
	if w.ready(1) {
		t.Fatal("maximum legal frames exceeded byte window")
	}
	w.confirm(25)
	if !w.ready(muxFrameMax) {
		t.Fatal("big frame did not release")
	}
}
