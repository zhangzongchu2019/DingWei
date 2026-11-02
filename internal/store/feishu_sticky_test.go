package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFeishuStickyIsolationCASExpiryRestartAndDedup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sticky.db")
	db := openMigrated(t, path)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	k := FeishuStickyKey{"alice", "bot1"}
	op, fresh, err := db.BeginFeishuSticky(ctx, k, "m1", "explicit", now)
	if err != nil || !fresh {
		t.Fatal(err)
	}
	old, _, err := db.BeginFeishuSticky(ctx, k, "m2", "explicit", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishFeishuSticky(ctx, op, "accepted", "A", "explicit", now); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishFeishuSticky(ctx, old, "accepted", "B", "late", now); !errors.Is(err, ErrStickyGeneration) {
		t.Fatal("S008_STALE_OVERWRITE", err)
	}
	state, _ := db.GetFeishuSticky(ctx, k)
	if state.AgentID != "A" || state.Generation != 1 {
		t.Fatal("S008_NEWER_TARGET", state)
	}
	for _, other := range []FeishuStickyKey{{"alice", "bot2"}, {"bob", "bot1"}} {
		s, e := db.GetFeishuSticky(ctx, other)
		if e != nil || s != nil {
			t.Fatal("S001_KEY_LEAK", s, e)
		}
	}
	dup, fresh, err := db.BeginFeishuSticky(ctx, FeishuStickyKey{"remapped", "bot1"}, "m1", "explicit", now.Add(time.Minute))
	if err != nil || fresh || dup.Seq != op.Seq {
		t.Fatal("S016_SOURCE_DUPLICATE", dup, fresh, err)
	}
	if !state.Valid(now.Add(30*time.Minute-time.Nanosecond)) || state.Valid(now.Add(30*time.Minute)) || state.Valid(now.Add(-time.Nanosecond)) {
		t.Fatal("S007_TTL_BOUNDARY_OR_ROLLBACK")
	}
	db.Close()
	db, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state, err = db.GetFeishuSticky(ctx, k)
	if err != nil || !state.ExpiresAt.Equal(now.Add(30*time.Minute)) {
		t.Fatal("S015_RESTART_EXTENDED", state, err)
	}
}
func TestFeishuStickyRejectedPendingUnknownRetentionAndBatch(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t, filepath.Join(t.TempDir(), "sticky.db"))
	defer db.Close()
	now := time.Now().UTC()
	k := FeishuStickyKey{"alice", "bot"}
	op, _, _ := db.BeginFeishuSticky(ctx, k, "reject", "explicit", now)
	if err := db.FinishFeishuSticky(ctx, op, "rejected", "A", "route_rejected", now); err != nil {
		t.Fatal(err)
	}
	if s, _ := db.GetFeishuSticky(ctx, k); s != nil {
		t.Fatal("S005_REJECTED_BINDING", s)
	}
	pending, _, _ := db.BeginFeishuSticky(ctx, k, "crash", "explicit", now)
	unresolved, err := db.ListFeishuStickyUnresolved(ctx, k, now.Add(10*time.Minute))
	if err != nil || len(unresolved) != 1 || unresolved[0].Status != "unknown" {
		t.Fatal("S019_PENDING_NOT_UNKNOWN", unresolved, err)
	}
	for i := 0; i < 5; i++ {
		o, _, err := db.BeginFeishuSticky(ctx, FeishuStickyKey{"old", string(rune('a' + i))}, "old", "explicit", now)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.FinishFeishuSticky(ctx, o, "accepted", "A", "explicit", now); err != nil {
			t.Fatal(err)
		}
	}
	n, err := db.CleanupFeishuSticky(ctx, now.Add(31*24*time.Hour), 3)
	if err != nil || n != 3 {
		t.Fatal("CLEANUP_BATCH_UNBOUNDED", n, err)
	}
	unresolved, err = db.ListFeishuStickyUnresolved(ctx, k, now.Add(31*24*time.Hour))
	if err != nil || len(unresolved) != 1 || unresolved[0].Seq != pending.Seq {
		t.Fatal("UNKNOWN_PURGED", unresolved, err)
	}
	if err = db.ResolveFeishuSticky(ctx, pending.Seq, now.Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	unresolved, err = db.ListFeishuStickyUnresolved(ctx, k, now.Add(31*24*time.Hour))
	if err != nil || len(unresolved) != 0 {
		t.Fatal("RECONCILIATION_BLOCKED", err)
	}
}
