package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSQLiteUsesWALAndNormalSynchronous(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "pragma.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want WAL", journalMode)
	}

	var synchronous int
	if err := store.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	if synchronous != 1 {
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", synchronous)
	}
}
