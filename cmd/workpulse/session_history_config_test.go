package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

func TestSessionHistorySwitchRequiresExactOne(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", false}, {"0", false}, {"true", false}, {" 1", false}, {"1 ", false}, {"01", false}, {"1", true}} {
		if got := sessionHistoryEnabledFromEnv(tc.value); got != tc.want {
			t.Errorf("value %q enabled=%t want %t", tc.value, got, tc.want)
		}
	}
}

func TestSessionHistoryDisabledOpenFailureContinuesAndHistoryEndpointIsDisabled(t *testing.T) {
	dir := t.TempDir()
	obstacle := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(obstacle, []byte("file blocks history directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(obstacle, "data")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	history, err := openSessionHistoryWithPolicy(dataDir, false, logger)
	if err != nil || history != nil {
		t.Fatalf("disabled open failure must continue without history: history=%v err=%v", history, err)
	}
	wantPath := filepath.Join(dataDir, "workpulse-history.db")
	if !strings.Contains(logs.String(), wantPath) || !strings.Contains(logs.String(), "error=\"") || strings.Contains(logs.String(), "error=\"\"") || strings.Count(logs.String(), "\n") != 1 {
		t.Fatalf("warning must include path and cause: %q", logs.String())
	}
	hub := m8.New(nil, m8.ConsoleAuthMode{})
	hub.SessionHistory = history
	hub.SessionHistoryEnabled = false
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/view/work/history", nil)
	r.SetPathValue("sessionName", "work")
	hub.HandleSessionHistory(w, r)
	if w.Code != 503 || w.Body.String() != `{"code":"history_feature_disabled","message":"未启用"}` {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSessionHistoryEnabledOpenFailureRejectsStartWithPath(t *testing.T) {
	dir := t.TempDir()
	obstacle := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(obstacle, []byte("file blocks history directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(obstacle, "data")
	history, err := openSessionHistoryWithPolicy(dataDir, true, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if history != nil || err == nil {
		t.Fatalf("enabled open failure must reject start: history=%v err=%v", history, err)
	}
	if !strings.Contains(err.Error(), filepath.Join(dataDir, "workpulse-history.db")) {
		t.Fatalf("startup error omits history path: %v", err)
	}
}

func TestSessionHistoryDisabledStillMigratesStoreWhenPathIsWritable(t *testing.T) {
	dir := t.TempDir()
	history, err := openSessionHistoryWithPolicy(dir, false, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil || history == nil {
		t.Fatalf("disabled startup should open/migrate the store: history=%v err=%v", history, err)
	}
	defer history.Close()
	if err := history.SetEnabled(context.Background(), false, time.Now()); err != nil {
		t.Fatalf("set disabled runtime state: %v", err)
	}
	path := filepath.Join(dir, "workpulse-history.db")
	for _, artifact := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(artifact); err != nil {
			t.Fatalf("disabled startup did not create SQLite runtime artifact %s: %v", artifact, err)
		}
	}
}
