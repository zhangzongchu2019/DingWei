package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

type fakeLivezDB struct{ err error }

func (f fakeLivezDB) Ping(context.Context) error { return f.err }

func TestLivezChecksLockDBAndAcceptHeartbeats(t *testing.T) {
	t.Setenv("WP_HUB_LIVEZ_LOCK_TIMEOUT", "20ms")
	hub := m8.New(nil)
	publicHB, internalHB := &acceptHeartbeat{}, &acceptHeartbeat{}
	publicHB.mark()
	internalHB.mark()
	handler := livezHandler(hub, fakeLivezDB{}, publicHB, internalHB)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("healthy livez: status=%d body=%s", rec.Code, rec.Body.String())
	}

	hold := httptest.NewRecorder()
	hub.HandleDebugHoldLock(hold, httptest.NewRequest(http.MethodPost, "/debug/hold-lock?seconds=1", nil))
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"hub_lock":false`) {
		t.Fatalf("stuck-lock livez: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSDNotifyWritesUnixDatagram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notify.sock")
	addr := &net.UnixAddr{Name: path, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	t.Setenv("NOTIFY_SOCKET", path)

	if err := sdNotify("READY=1"); err != nil {
		t.Fatalf("sdNotify: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Fatalf("notification = %q", got)
	}
}

func TestWatchdogInterval(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "60000000")
	got, ok := watchdogInterval()
	if !ok || got != time.Minute {
		t.Fatalf("watchdogInterval = %s, %v", got, ok)
	}
}

func TestWatchdogFastConfirmRecoversOnAnySuccessfulProbe(t *testing.T) {
	attempts := 0
	recovered := watchdogFastConfirm(context.Background(), 3, time.Nanosecond, func() bool {
		attempts++
		return attempts == 2
	})
	if !recovered || attempts != 2 {
		t.Fatalf("recovered=%v attempts=%d", recovered, attempts)
	}
}

func TestWatchdogFastConfirmRequiresAllConfiguredFailures(t *testing.T) {
	attempts := 0
	recovered := watchdogFastConfirm(context.Background(), 3, time.Nanosecond, func() bool {
		attempts++
		return false
	})
	if recovered || attempts != 3 {
		t.Fatalf("recovered=%v attempts=%d", recovered, attempts)
	}
}
