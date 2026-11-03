package main

import (
	"os"
	"path/filepath"
	"testing"
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
	history, err := openSessionHistoryIfEnabled(dir, false)
	if err != nil || history != nil {
		t.Fatalf("history=%v err=%v", history, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "workpulse-history.db")); !os.IsNotExist(err) {
		t.Fatalf("disabled switch created history database: stat err=%v", err)
	}
}
