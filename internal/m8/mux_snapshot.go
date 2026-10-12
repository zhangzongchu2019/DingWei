package m8

import (
	"errors"
	"math"
	"strconv"
)

var errMuxCursorAhead = errors.New("cursor_ahead")
var errMuxInvalidCursor = errors.New("invalid_cursor")

// Legacy selectSSEReplay remains unchanged. Mux admission rejects an ahead
// cursor rather than turning missing history into a successful empty replay.
func selectMuxReplay(s sessionSeqSnapshot, after string, hasAfter bool) ([]sessionSeqEvent, string, error) {
	if s.H >= math.MaxUint64-1 {
		return nil, "seq_exhausted", nil
	}
	if !hasAfter {
		if s.HistoryTruncated && s.Empty {
			return nil, "history_unavailable", nil
		}
		for _, e := range s.Replay {
			if e.Seq == 0 {
				return nil, "legacy_unsequenced", nil
			}
		}
		return s.Replay, "", nil
	}
	epoch, seq, ok := parseSSEAfter(after)
	if !ok || epoch+"-"+strconv.FormatUint(seq, 10) != after {
		return nil, "", errMuxInvalidCursor
	}
	if epoch != s.Epoch {
		return s.Replay, "epoch_changed", nil
	}
	if seq > s.H {
		return nil, "", errMuxCursorAhead
	}
	if s.Empty && seq < s.H {
		return nil, "history_unavailable", nil
	}
	var out []sessionSeqEvent
	gap := ""
	if s.Floor > 0 && seq < s.Floor-1 {
		gap = "overflow"
	}
	for _, e := range s.Replay {
		if e.Seq == 0 {
			return nil, "legacy_unsequenced", nil
		}
		if e.Seq > seq && e.Seq <= s.H {
			out = append(out, e)
		}
	}
	return out, gap, nil
}
