//go:build instrmutex

package m8

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInstrMutexMeasuresHoldTimeNotWaitTime(t *testing.T) {
	var mu hubMutex
	mu.Mutex.Lock() // uninstrumented blocker: creates wait time without a sample
	done := make(chan struct{})
	go func() {
		mu.Lock()
		time.Sleep(2 * time.Millisecond)
		mu.Unlock()
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	mu.Mutex.Unlock()
	<-done

	stats, enabled := mu.lockStats()
	if !enabled || stats.Count != 1 {
		t.Fatalf("stats enabled=%v count=%d", enabled, stats.Count)
	}
	if stats.Max >= 20*time.Millisecond {
		t.Fatalf("hold duration includes lock wait: max=%s", stats.Max)
	}
}

func TestDebugLockStatsEndpointInInstrBuild(t *testing.T) {
	hub := New(nil)
	hub.mu.Lock()
	hub.mu.Unlock()
	rec := httptest.NewRecorder()
	hub.HandleDebugLockStats(rec, httptest.NewRequest(http.MethodGet, "/debug/lock-stats", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"count":1`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
