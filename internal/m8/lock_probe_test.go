package m8

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHubLockReleasedAfterHandlerPanic(t *testing.T) {
	hub := New(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /panic-under-lock", func(http.ResponseWriter, *http.Request) {
		hub.withHubLock(func() {
			panic("injected critical-section panic")
		})
	})
	mux.HandleFunc("GET /after-panic", func(w http.ResponseWriter, r *http.Request) {
		if !hub.ProbeLock(r.Context(), 100*time.Millisecond, time.Millisecond) {
			http.Error(w, "hub lock remained held", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// net/http recovers the injected panic and closes this request's stream.
	resp, err := srv.Client().Get(srv.URL + "/panic-under-lock")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("panic request unexpectedly completed")
	}

	resp, err = srv.Client().Get(srv.URL + "/after-panic")
	if err != nil {
		t.Fatalf("request after panic: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response after panic: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("response after panic: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestProbeLock(t *testing.T) {
	hub := New(nil)
	if !hub.ProbeLock(context.Background(), 20*time.Millisecond, time.Millisecond) {
		t.Fatal("uncontended mutex should be healthy")
	}

	hub.mu.Lock()
	started := time.Now()
	if hub.ProbeLock(context.Background(), 25*time.Millisecond, time.Millisecond) {
		hub.mu.Unlock()
		t.Fatal("held mutex should fail the bounded probe")
	}
	hub.mu.Unlock()
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("probe duration = %s, want a bounded wait near timeout", elapsed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	hub.mu.Lock()
	cancel()
	if hub.ProbeLock(ctx, time.Second, time.Millisecond) {
		hub.mu.Unlock()
		t.Fatal("cancelled probe should fail")
	}
	hub.mu.Unlock()
}

func TestHandleDebugHoldLock(t *testing.T) {
	hub := New(nil)
	req := httptest.NewRequest(http.MethodPost, "/debug/hold-lock?seconds=1", nil)
	rec := httptest.NewRecorder()
	hub.HandleDebugHoldLock(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "1s") {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if hub.ProbeLock(context.Background(), 20*time.Millisecond, time.Millisecond) {
		t.Fatal("debug endpoint did not hold the mutex")
	}
	time.Sleep(1100 * time.Millisecond)
	if !hub.ProbeLock(context.Background(), 20*time.Millisecond, time.Millisecond) {
		t.Fatal("debug endpoint did not release the mutex")
	}
}

func TestHandleDebugHoldLockRejectsInvalidDuration(t *testing.T) {
	hub := New(nil)
	for _, target := range []string{
		"/debug/hold-lock?seconds=0",
		"/debug/hold-lock?seconds=invalid",
		"/debug/hold-lock?seconds=121",
	} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		rec := httptest.NewRecorder()
		hub.HandleDebugHoldLock(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", target, rec.Code, http.StatusBadRequest)
		}
	}
}
