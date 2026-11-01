package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

func r7Fact() specconfirm.Confirmation {
	actor, op := "committee-user", "operation-00000001"
	return specconfirm.Confirmation{ID: specconfirm.OperationKey(actor, op), OperationID: op, Coordinate: specconfirm.Coordinate{Instance: "git.code.tencent.com", Repository: "wego-ai/dingwei", ChangeDir: "openspec/changes/r7-confirm-immutable-version", Commit: strings.Repeat("a", 40)}, ConfirmedBy: actor, ConfirmedAt: time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC), Meaning: "同意此版本需求与范围"}
}
func TestR7SpecConfirmationPersistenceImmutableAndRetry(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "r7.db")
	db := openMigrated(t, file)
	fact := r7Fact()
	got, err := db.SaveSpecConfirmation(ctx, fact)
	if err != nil || !got.SameFact(fact) {
		t.Fatalf("save %+v %v", got, err)
	}
	retry := fact
	retry.ConfirmedAt = retry.ConfirmedAt.Add(time.Hour)
	got, err = db.SaveSpecConfirmation(ctx, retry)
	if err != nil || !got.ConfirmedAt.Equal(fact.ConfirmedAt) {
		t.Fatalf("retry moved original time: %+v %v", got, err)
	}
	for _, mutation := range []func(*specconfirm.Confirmation){func(c *specconfirm.Confirmation) { c.Commit = strings.Repeat("b", 40) }, func(c *specconfirm.Confirmation) { c.Meaning = "different" }, func(c *specconfirm.Confirmation) { c.Repository = "other/repo" }} {
		changed := fact
		mutation(&changed)
		if _, e := db.SaveSpecConfirmation(ctx, changed); !errors.Is(e, specconfirm.ErrConflict) {
			t.Fatalf("rebound: %v", e)
		}
	}
	for _, stmt := range []string{`UPDATE openspec_commit_confirmation SET commit_sha='` + strings.Repeat("b", 40) + `'`, `DELETE FROM openspec_commit_confirmation`} {
		if _, e := db.db.ExecContext(ctx, stmt); e == nil || !strings.Contains(e.Error(), "immutable") {
			t.Fatalf("DDL protection: %v", e)
		}
	}
	rows, err := db.db.QueryContext(ctx, `PRAGMA foreign_key_list(openspec_commit_confirmation)`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("confirmation has cascade-capable external FK")
	}
	rows.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := OpenSQLite(file)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err = db2.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Even if a branch moves, querying that change never relabels its facts.
	moved := fact.Coordinate
	moved.Commit = strings.Repeat("b", 40)
	history, err := db2.ListSpecConfirmations(ctx, moved)
	if err != nil || len(history) != 1 || history[0].Commit != fact.Commit {
		t.Fatalf("reopen/force-push: %+v %v", history, err)
	}
	moved.Repository = "other/repo"
	history, err = db2.ListSpecConfirmations(ctx, moved)
	if err != nil || len(history) != 0 {
		t.Fatalf("cross repo leak %+v %v", history, err)
	}
}
func TestR7SpecConfirmationRejectsInvalidAndKeepsNewHistory(t *testing.T) {
	ctx := context.Background()
	db := newConsoleSpecStore(t)
	fact := r7Fact()
	for _, mutate := range []func(*specconfirm.Confirmation){func(c *specconfirm.Confirmation) { c.Commit = "main" }, func(c *specconfirm.Confirmation) { c.ChangeDir = "openspec/changes/../escape" }, func(c *specconfirm.Confirmation) { c.Meaning = " " }, func(c *specconfirm.Confirmation) { c.ConfirmedBy = "" }, func(c *specconfirm.Confirmation) { c.ID = "forged" }} {
		c := fact
		mutate(&c)
		if _, e := db.SaveSpecConfirmation(ctx, c); !errors.Is(e, specconfirm.ErrInvalid) {
			t.Fatalf("invalid accepted: %v", e)
		}
	}
	if _, e := db.SaveSpecConfirmation(ctx, fact); e != nil {
		t.Fatal(e)
	}
	next := fact
	next.Commit = strings.Repeat("b", 40)
	next.OperationID = "operation-00000002"
	next.ID = specconfirm.OperationKey(next.ConfirmedBy, next.OperationID)
	if _, e := db.SaveSpecConfirmation(ctx, next); e != nil {
		t.Fatal(e)
	}
	all, e := db.ListSpecConfirmations(ctx, next.Coordinate)
	if e != nil || len(all) != 2 {
		t.Fatalf("history lost %+v %v", all, e)
	}
}

func TestR7SpecConfirmationConcurrentWritersAreIdempotent(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "concurrent.db")
	a := openMigrated(t, file)
	b, err := OpenSQLite(file)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	done := make(chan error, 12)
	for i := 0; i < 12; i++ {
		db := a
		if i%2 == 0 {
			db = b
		}
		go func(db *SQLite, i int) {
			c := r7Fact()
			c.ConfirmedAt = c.ConfirmedAt.Add(time.Duration(i) * time.Second)
			_, e := db.SaveSpecConfirmation(ctx, c)
			done <- e
		}(db, i)
	}
	for i := 0; i < 12; i++ {
		if e := <-done; e != nil {
			t.Fatalf("concurrent retry %v", e)
		}
	}
	facts, e := a.ListSpecConfirmations(ctx, r7Fact().Coordinate)
	if e != nil || len(facts) != 1 {
		t.Fatalf("duplicate atomic confirmation %+v %v", facts, e)
	}
}

func TestR7SpecConfirmationMigrationRejectsPartialOrAlteredProtection(t *testing.T) {
	for _, damage := range []string{`DROP TRIGGER openspec_commit_confirmation_no_update`, `DROP INDEX idx_openspec_commit_confirmation_change`, `DROP TRIGGER openspec_commit_confirmation_no_delete; CREATE TRIGGER openspec_commit_confirmation_no_delete BEFORE DELETE ON openspec_commit_confirmation BEGIN SELECT 1; END;`} {
		t.Run(damage, func(t *testing.T) {
			ctx := context.Background()
			db := newConsoleSpecStore(t)
			if _, e := db.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version='0068_openspec_commit_confirmation.sql'`); e != nil {
				t.Fatal(e)
			}
			if _, e := db.db.ExecContext(ctx, damage); e != nil {
				t.Fatal(e)
			}
			err := db.Migrate(ctx)
			if err == nil || (!strings.Contains(err.Error(), "partially present") && !strings.Contains(err.Error(), "schema mismatch")) {
				t.Fatalf("damaged schema silently accepted: %v", err)
			}
			var n int
			if e := db.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version='0068_openspec_commit_confirmation.sql'`).Scan(&n); e != nil || n != 0 {
				t.Fatalf("damaged schema booked %d %v", n, e)
			}
		})
	}
}
