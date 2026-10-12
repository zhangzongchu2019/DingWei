package m8

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test instrumentation observes the actual public native net/http Write+Flush.
// It does not replace the gate, its clock, primary authorization, or socket.
type muxOutputEvidence struct {
	mu                          sync.Mutex
	commit, lastFlush           time.Time
	events, afterCommit, probes int
	maxFrame                    int
	probeDone                   chan struct{}
}
type muxOutputTap struct {
	http.ResponseWriter
	e       *muxOutputEvidence
	pending bool
}

func (w *muxOutputTap) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *muxOutputTap) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"type":"event"`)) {
		w.pending = true
		w.e.mu.Lock()
		if len(p) > w.e.maxFrame {
			w.e.maxFrame = len(p)
		}
		w.e.mu.Unlock()
	}
	return w.ResponseWriter.Write(p)
}
func (w *muxOutputTap) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err == nil && w.pending {
		w.e.mu.Lock()
		w.e.events++
		w.e.lastFlush = time.Now()
		if !w.e.commit.IsZero() && w.e.lastFlush.After(w.e.commit) {
			w.e.afterCommit++
		}
		w.e.mu.Unlock()
	}
	w.pending = false
	return err
}
func (w *muxOutputTap) WriteString(p string) (int, error) { return w.Write([]byte(p)) }
func (e *muxOutputEvidence) check(t *testing.T) {
	t.Helper()
	e.mu.Lock()
	done := e.probeDone
	e.mu.Unlock()
	if done == nil {
		t.Fatal("primary invalidation not exercised")
	}
	<-done
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.maxFrame != muxFrameMax {
		t.Fatalf("legal maximum did not physically cross public socket: %d", e.maxFrame)
	}
	if e.commit.IsZero() || e.probes < 100 {
		t.Fatal("insufficient post-commit source traffic")
	}
	tail := e.lastFlush.Sub(e.commit)
	if tail > 2*time.Second {
		t.Fatalf("sensitive output exceeded commit+2s: %s", tail)
	}
	t.Logf("PRIMARY_OUTPUT_CUTOFF authoritative_commit_lower_bound=monotonic_call_start_conservative last_successful_sensitive_flush_relative=%s after_commit_flushes=%d continuous_postcommit_publish_attempts=%d maximum_physical_frame=%d observation=2500ms", tail, e.afterCommit, e.probes, e.maxFrame)
}
func procRSS(pid int) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			n, _ := strconv.ParseUint(f[1], 10, 64)
			return n * 1024
		}
	}
	return 0
}
func muxResourceSample(h *Hub, pids []int) map[string]any {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	h.consoleMux.mu.Lock()
	feeds := make([]*consoleMuxFeed, 0, len(h.consoleMux.feeds))
	for _, f := range h.consoleMux.feeds {
		feeds = append(feeds, f)
	}
	h.consoleMux.mu.Unlock()
	queueBytes, queueCount := 0, 0
	activeTransports, activeSubscriptions := 0, 0
	for _, f := range feeds {
		f.mu.Lock()
		q := f.Stream
		if q != nil && f.Cancel != nil && !f.Closed {
			activeTransports++
			activeSubscriptions += len(f.Scopes)
		}
		f.mu.Unlock()
		if q != nil {
			q.mu.Lock()
			queueBytes += q.bytes
			queueCount += q.count
			q.mu.Unlock()
		}
	}
	h.mu.Lock()
	var readers []*consoleMuxReader
	ringBytes := 0
	for _, st := range h.terminals {
		for _, s := range st.eventRing {
			ringBytes += len(s)
		}
		for _, r := range st.muxSeqSubs {
			readers = append(readers, r)
		}
	}
	h.mu.Unlock()
	rawBytes := 0
	for _, r := range readers {
		r.mu.Lock()
		rawBytes += r.bytes
		r.mu.Unlock()
	}
	vs := []uint64{}
	for _, pid := range pids {
		vs = append(vs, procRSS(pid))
	}
	return map[string]any{"hub_process_rss": procRSS(os.Getpid()), "hub_heap_alloc": mem.HeapAlloc, "hub_heap_sys": mem.HeapSys, "hub_queue_wire_bytes": queueBytes, "hub_queue_events": queueCount, "active_mux_transports": activeTransports, "active_subscription_count": activeSubscriptions, "raw_reader_payload_bytes": rawBytes, "legacy_ring_payload_bytes_excluded_from_mux_budget": ringBytes, "vs_process_rss": vs, "scope": "one actual Hub test process/two VS; absolute RSS includes all process overhead; snapshot not a universal upper bound"}
}
func handleMuxMaximumFrame(h *Hub, w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name") + ".owner"
	h.consoleMux.mu.Lock()
	var feed *consoleMuxFeed
	for _, f := range h.consoleMux.feeds {
		f.mu.Lock()
		active := f.Cancel != nil
		f.mu.Unlock()
		if active {
			feed = f
			break
		}
	}
	h.consoleMux.mu.Unlock()
	if feed == nil {
		http.Error(w, "no feed", 409)
		return
	}
	scopes, _ := muxScopes(feed)
	s := scopes[name]
	if s == nil {
		http.Error(w, "no scope", 409)
		return
	}
	h.mu.Lock()
	st := h.terminals[terminalKey(AgentKeyID("owner"), r.PathValue("name"))]
	epoch, seq := st.eventEpoch, st.eventSeq+1
	h.mu.Unlock()
	meta := muxMeta(s, "event")
	meta.Epoch = epoch
	meta.Seq = muxSeq(seq)
	head, _ := json.Marshal(meta)
	marker := "maximum-frame-" + r.PathValue("name")
	body := []byte(`{"kind":"assistant_text","text":"` + marker + strings.Repeat("x", muxFrameMax-len(head)-19-len(`{"kind":"assistant_text","text":""}`)-len(marker)) + `"}`)
	over := r.URL.Query().Get("over") == "1"
	if over {
		body = append(body[:len(body)-2], []byte(`x"}`)...)
	}
	p, e := muxPacket(meta, body)
	if over && e != nil {
		h.publishSessionEvent(AgentKeyID("owner"), r.PathValue("name"), string(body))
		json.NewEncoder(w).Encode(map[string]any{"wire_bytes": muxFrameMax + 1, "expected": "seat_gap_event_too_large"})
		return
	}
	if e != nil || len(p.Wire) != muxFrameMax {
		http.Error(w, "max construction failed", 500)
		return
	}
	h.publishSessionEvent(AgentKeyID("owner"), r.PathValue("name"), string(body))
	json.NewEncoder(w).Encode(map[string]any{"wire_bytes": len(p.Wire), "raw_payload_bytes": len(body), "after": epoch + "-" + meta.Seq, "marker": marker})
}
