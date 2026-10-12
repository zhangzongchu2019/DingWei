package m8

// Retained serialized UTF-8 payload budgets. Metadata has an independent,
// conservative global entry cap; neither number is a total process RSS bound.
const (
	terminalEventRingBytes = 16 << 20
	hubEventRingBytes      = 128 << 20
	hubEventRingEntries    = 128000
)

// All methods below require h.mu. The global list contains no payload copy and
// its oldest node always identifies the oldest event of that terminal suffix.
func (h *Hub) dropRingHeadLocked(st *terminalState) {
	if len(st.eventRing) == 0 {
		return
	}
	n := len(st.eventRing[0])
	h.retainedRingBytes -= n
	st.eventRingBytes -= n
	if len(st.eventRingSeq) > 0 {
		if st.eventRingSeq[0] > st.eventRingLostThrough {
			st.eventRingLostThrough = st.eventRingSeq[0]
		}
		st.eventRingSeq[0] = 0
		st.eventRingSeq = st.eventRingSeq[1:]
	}
	st.eventRingTruncated = true
	st.eventRing[0] = ""
	st.eventRing = st.eventRing[1:]
	if len(st.eventRingNodes) > 0 {
		h.retainedRingFIFO.Remove(st.eventRingNodes[0])
		st.eventRingNodes[0] = nil
		st.eventRingNodes = st.eventRingNodes[1:]
	}
	if len(st.eventRing) == 0 {
		st.eventRing = nil
		st.eventRingSeq = nil
		st.eventRingNodes = nil
	} else if len(st.eventRing)*4 < cap(st.eventRing) {
		st.eventRing = append([]string(nil), st.eventRing...)
		st.eventRingSeq = append([]uint64(nil), st.eventRingSeq...)
		st.eventRingNodes = append(st.eventRingNodes[:0:0], st.eventRingNodes...)
	}
}
func (h *Hub) clearRingLocked(st *terminalState) {
	for len(st.eventRing) > 0 {
		h.dropRingHeadLocked(st)
	}
}
func (h *Hub) enforceRingBudgetsLocked(st *terminalState) {
	for len(st.eventRing) > terminalEventRingCap || st.eventRingBytes > terminalEventRingBytes {
		h.dropRingHeadLocked(st)
	}
	for h.retainedRingBytes > hubEventRingBytes || h.retainedRingFIFO.Len() > hubEventRingEntries {
		oldest := h.retainedRingFIFO.Front().Value.(*terminalState)
		h.dropRingHeadLocked(oldest)
	}
}

// Only legacy tests can inject an unaccounted ring; adopt once without changing
// their Seq=0 semantics. Production admission always goes through this method.
func (h *Hub) adoptRingLocked(st *terminalState) {
	if len(st.eventRingNodes) == len(st.eventRing) {
		return
	}
	if len(st.eventRingNodes) != 0 {
		panic("event ring accounting mismatch")
	}
	if len(st.eventRingSeq) < len(st.eventRing) {
		st.eventRingSeq = append(make([]uint64, len(st.eventRing)-len(st.eventRingSeq)), st.eventRingSeq...)
	}
	for _, data := range st.eventRing {
		st.eventRingNodes = append(st.eventRingNodes, h.retainedRingFIFO.PushBack(st))
		st.eventRingBytes += len(data)
		h.retainedRingBytes += len(data)
	}
	h.enforceRingBudgetsLocked(st)
}
func (h *Hub) retainEventLocked(st *terminalState, seq uint64, data string) {
	h.adoptRingLocked(st)
	if len(data) > terminalEventRingBytes {
		h.clearRingLocked(st)
		st.eventRingTruncated = true
		st.eventRingLostThrough = seq
		return
	}
	st.eventRing = append(st.eventRing, data)
	st.eventRingSeq = append(st.eventRingSeq, seq)
	st.eventRingNodes = append(st.eventRingNodes, h.retainedRingFIFO.PushBack(st))
	st.eventRingBytes += len(data)
	h.retainedRingBytes += len(data)
	h.enforceRingBudgetsLocked(st)
}
func selectLegacySnapshotReplay(s sessionSeqSnapshot, after string, hasAfter bool) ([]sessionSeqEvent, string) {
	replay, gap := selectSSEReplay(s.Epoch, s.Replay, after, hasAfter)
	if gap != "" || !s.HistoryTruncated {
		return replay, gap
	}
	if !hasAfter {
		return replay, "overflow"
	}
	epoch, n, ok := parseSSEAfter(after)
	if ok && epoch == s.Epoch && n < s.LostThrough {
		return replay, "overflow"
	}
	return replay, ""
}
