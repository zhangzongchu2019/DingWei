package main

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/store"
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

func TestSessionHistoryDisabledDoesNotCreateHistoryDatabase(t *testing.T) {
	dir := t.TempDir()
	history, err := openSessionHistoryForRun(dir, false, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), time.Now())
	if err != nil || history != nil {
		t.Fatalf("history=%v err=%v", history, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "workpulse-history.db")); !os.IsNotExist(err) {
		t.Fatalf("disabled switch created history database: stat err=%v", err)
	}
}

func TestSessionHistoryDisabledExistingUnwritableStoreWarnsOnceAndHubRemainsDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpulse-history.db")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	history, err := openSessionHistoryForRun(dir, false, slog.New(slog.NewTextHandler(&logs, nil)), time.Now())
	if err != nil || history != nil {
		t.Fatalf("history=%v err=%v", history, err)
	}
	if strings.Count(logs.String(), "WARN") != 1 || !strings.Contains(logs.String(), path) || !strings.Contains(logs.String(), "error=") {
		t.Fatalf("expected one warning with path and cause, got %q", logs.String())
	}
	hub := m8.New(nil, m8.ConsoleAuthMode{})
	hub.SessionHistory, hub.SessionHistoryEnabled = history, false
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/view/work/history", nil)
	r.SetPathValue("sessionName", "work")
	hub.HandleSessionHistory(w, r)
	if w.Code != 503 || w.Body.String() != `{"code":"history_feature_disabled","message":"未启用"}` {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSessionHistoryEnabledUnwritableStoreRejectsStartupWithPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpulse-history.db")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	history, err := openSessionHistoryForRun(dir, true, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), time.Now())
	if history != nil || err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("history=%v err=%v", history, err)
	}
}

func TestSessionHistoryDisabledStateWriteFailureWarnsOnceAndHubRemainsAvailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpulse-history.db")
	history, err := store.OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.SetEnabled(context.Background(), true, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_disable BEFORE UPDATE OF enabled ON session_history_runtime_state WHEN NEW.enabled=0 BEGIN SELECT RAISE(ABORT,'simulated readonly state'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	history, err = openSessionHistoryForRun(dir, false, slog.New(slog.NewTextHandler(&logs, nil)), time.Unix(200, 0))
	if err != nil || history != nil {
		t.Fatalf("history=%v err=%v", history, err)
	}
	if strings.Count(logs.String(), "WARN") != 1 || !strings.Contains(logs.String(), path) || !strings.Contains(logs.String(), "simulated readonly state") {
		t.Fatalf("expected one warning with path and failure cause, got %q", logs.String())
	}
	hub := m8.New(nil, m8.ConsoleAuthMode{})
	hub.SessionHistory, hub.SessionHistoryEnabled = history, false
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/view/work/history", nil)
	r.SetPathValue("sessionName", "work")
	hub.HandleSessionHistory(w, r)
	if w.Code != 503 || w.Body.String() != `{"code":"history_feature_disabled","message":"未启用"}` {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSessionHistoryOffThenOnStartupPersistsDisabledGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpulse-history.db")
	history, err := store.OpenSessionHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(300, 0)
	if err := history.SetEnabled(context.Background(), true, start); err != nil {
		t.Fatal(err)
	}
	if _, err := history.Append(context.Background(), "owner", "agent", "work", "before", "t", []byte(`{"n":1}`), start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	history, err = openSessionHistoryForRun(dir, false, logger, start.Add(10*time.Second))
	if err != nil || history == nil {
		t.Fatalf("disabled startup history=%v err=%v", history, err)
	}
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}
	history, err = openSessionHistoryForRun(dir, true, logger, start.Add(20*time.Second))
	if err != nil || history == nil {
		t.Fatalf("enabled startup history=%v err=%v", history, err)
	}
	defer history.Close()
	page, err := history.Page(context.Background(), "owner", "agent", "work", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || len(page.Gaps) != 1 || page.Gaps[0].Reason != "feature_disabled" || !page.Gaps[0].StartedAt.Equal(start.Add(10*time.Second)) {
		t.Fatalf("open-off-on did not preserve history and gap: %+v", page)
	}
}
