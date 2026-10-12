package m8

// Metadata only: the payload remains in the existing replay/live reader.
// Source credit is released only by the public consumer's confirmed cursor.
// Per scope at most 16 descriptors (256 bytes) and <=2MiB encoded in flight;
// limits stay below the existing per-seat pending+sent budget (256/2MiB).
type muxSourceWindow struct {
	entries []muxSourceFlight
	bytes   int
}
type muxSourceFlight struct {
	seq   uint64
	bytes int
}

func (w *muxSourceWindow) confirm(after uint64) {
	n := 0
	for n < len(w.entries) && w.entries[n].seq <= after {
		w.bytes -= w.entries[n].bytes
		n++
	}
	if n > 0 {
		copy(w.entries, w.entries[n:])
		w.entries = w.entries[:len(w.entries)-n]
	}
}
func (w *muxSourceWindow) ready(size int) bool { return len(w.entries) < 16 && w.bytes+size <= 2<<20 }
func (w *muxSourceWindow) sent(seq uint64, size int) {
	w.entries = append(w.entries, muxSourceFlight{seq, size})
	w.bytes += size
}
