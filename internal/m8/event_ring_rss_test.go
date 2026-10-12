package m8

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

func retainedRingResourceLoad(h *Hub, pids []int) map[string]any {
	before := muxResourceSample(h, pids)
	peak := procRSS(os.Getpid())
	for a := 0; a < 9; a++ {
		for i := 0; i < 20; i++ {
			prefix := fmt.Sprintf(`{"kind":"assistant_text","text":"ring-resource-%d-%d-`, a, i)
			data := prefix + strings.Repeat("x", (1<<20)-len(prefix)-2) + `"}`
			h.publishSessionEvent("ring-resource", fmt.Sprint(a), data)
			if rss := procRSS(os.Getpid()); rss > peak {
				peak = rss
			}
		}
	}
	loaded := muxResourceSample(h, pids)
	h.mu.Lock()
	bytes, entries := h.retainedRingBytes, h.retainedRingFIFO.Len()
	h.mu.Unlock()
	time.Sleep(time.Second)
	stopped := muxResourceSample(h, pids)
	h.mu.Lock()
	for a := 0; a < 9; a++ {
		h.closeTerminalLocked("ring-resource", fmt.Sprint(a))
	}
	afterBytes := h.retainedRingBytes
	h.mu.Unlock()
	deleted := muxResourceSample(h, pids)
	runtime.GC()
	debug.FreeOSMemory()
	trimmed := muxResourceSample(h, pids)
	return map[string]any{"before": before, "loaded": loaded, "observed_hub_rss_peak": peak, "retained_global_bytes": bytes, "retained_global_entries": entries, "stopped_publish_1s": stopped, "terminal_deleted": deleted, "retained_after_test_terminal_delete": afterBytes, "after_explicit_test_gc_and_os_trim": trimmed, "scope": "9 independent test terminal histories x20 unique serialized 1MiB events; real Hub ring code; ongoing managed stream and two VS unchanged; forced GC/OS trim is diagnostic, not automatic product recovery"}
}
