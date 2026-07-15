package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type LockTimingStats struct {
	Count uint64        `json:"count"`
	Min   time.Duration `json:"min_ns"`
	P50   time.Duration `json:"p50_ns"`
	P95   time.Duration `json:"p95_ns"`
	P99   time.Duration `json:"p99_ns"`
	P999  time.Duration `json:"p999_ns"`
	Max   time.Duration `json:"max_ns"`
}

const (
	defaultLockProbeInterval = 50 * time.Millisecond
	maxDebugHoldLock         = 2 * time.Minute
)

// withHubLock guarantees that the Hub mutex is released even if work panics.
// Lock-site hardening can use this helper as each audited critical section is
// converted; keep blocking or otherwise fallible I/O outside work.
func (h *Hub) withHubLock(work func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	work()
}

// ProbeLock reports whether the Hub mutex can be acquired within timeout.
// It uses TryLock polling so an unsuccessful probe never leaves a goroutine
// blocked behind a permanently stuck mutex.
func (h *Hub) ProbeLock(ctx context.Context, timeout, interval time.Duration) bool {
	if h == nil {
		return false
	}
	if h.mu.TryLock() {
		h.mu.Unlock()
		return true
	}
	if timeout <= 0 {
		return false
	}
	if interval <= 0 {
		interval = defaultLockProbeInterval
	}
	if interval > timeout {
		interval = timeout
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-ticker.C:
			if h.mu.TryLock() {
				h.mu.Unlock()
				return true
			}
		}
	}
}

// HandleDebugHoldLock deliberately holds the Hub mutex for watchdog testing.
// Mount this handler only on the internal listener and only when explicitly
// enabled by configuration.
func (h *Hub) HandleDebugHoldLock(w http.ResponseWriter, r *http.Request) {
	seconds := 30
	if raw := r.URL.Query().Get("seconds"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			http.Error(w, "seconds must be a positive integer", http.StatusBadRequest)
			return
		}
		seconds = parsed
	}
	duration := time.Duration(seconds) * time.Second
	if duration > maxDebugHoldLock {
		http.Error(w, fmt.Sprintf("seconds must not exceed %d", int(maxDebugHoldLock/time.Second)), http.StatusBadRequest)
		return
	}
	if !h.mu.TryLock() {
		http.Error(w, "hub lock is already held", http.StatusConflict)
		return
	}
	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		<-timer.C
		h.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, "holding hub lock for %s\n", duration)
}

// HandleDebugLockStats exposes h.mu hold-time percentiles in instrmutex builds.
// Production builds return 204 even if the debug route is accidentally enabled.
func (h *Hub) HandleDebugLockStats(w http.ResponseWriter, _ *http.Request) {
	stats, enabled := h.mu.lockStats()
	if !enabled {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}
