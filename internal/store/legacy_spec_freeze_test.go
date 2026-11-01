package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func r11Snapshot(t *testing.T, db *SQLite) map[string][]string {
	t.Helper()
	rows, err := db.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND (name='console_spec' OR name LIKE 'console_spec_%' OR name LIKE 'pilot_template_%' OR name='pilot_spec_source') ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	out := map[string][]string{}
	for _, name := range names {
		rows, err := db.db.Query(`SELECT * FROM "` + name + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		out[name] = nil
		for rows.Next() {
			v := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range v {
				ptr[i] = &v[i]
			}
			if err = rows.Scan(ptr...); err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(v)
			out[name] = append(out[name], string(b))
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(out[name])
	}
	return out
}

func TestR11FreezeStoreRefusalsPreserveHistory(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Now()
	createSpec(t, db, "r11", "baseline", now)
	// Before cutover the normal write contract is unchanged.
	if _, e := db.AppendConsoleSpecRevision(ctx, "owner-a", "r11", 1, specRevision("r11", 2, "before"), now); e != nil {
		t.Fatal(e)
	}
	before := r11Snapshot(t, db)
	if e := db.EnableLegacySpecFreeze(ctx); e != nil {
		t.Fatal(e)
	}
	if e := db.EnableLegacySpecFreeze(ctx); e != nil {
		t.Fatal("latch is not idempotent", e)
	}
	paths := map[string]func() error{
		"LCS-S001_append": func() error {
			_, e := db.AppendConsoleSpecRevision(ctx, "owner-a", "r11", 2, specRevision("r11", 3, "after"), now)
			return e
		},
		"LCS-S005_shared": func() error {
			_, e := db.AppendConsoleSpecRevisionWithCapability(ctx, "r11", 2, "hash", specRevision("r11", 3, "after"), now)
			return e
		},
		"LCS-S004_submit": func() error { return db.SubmitConsoleSpecReview(ctx, "r11", "owner-a", now) },
		"archive":         func() error { return db.ArchiveConsoleSpec(ctx, "r11", "owner-a", now) },
		"close":           func() error { return db.CloseConsoleSpecEditing(ctx, "owner-a", "r11", now) },
	}
	for name, fn := range paths {
		t.Run(name, func(t *testing.T) {
			if e := fn(); e == nil {
				t.Fatal("frozen write succeeded")
			}
		})
	}
	for _, q := range []string{
		`UPDATE console_spec SET title='changed' WHERE id='r11'`,
		`DELETE FROM console_spec WHERE id='r11'`,
		`UPDATE console_spec_revision SET content_markdown='changed' WHERE spec_id='r11'`,
		`INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_at,created_by_kind,supersedes_revision) SELECT spec_id,3,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_at,created_by_kind,supersedes_revision FROM console_spec_revision WHERE spec_id='r11' AND revision=2`,
	} {
		if _, e := db.db.Exec(q); e == nil || !strings.Contains(e.Error(), "legacy_spec_frozen") {
			t.Fatalf("raw freeze bypass: %s: %v", q, e)
		}
	}
	if after := r11Snapshot(t, db); !reflect.DeepEqual(before, after) {
		t.Fatal("LCS-S008 historical objects changed")
	}
	if _, _, e := db.BeginLegacySpecWrite(ctx); !errors.Is(e, ErrLegacySpecFrozen) {
		t.Fatal(e)
	}
	if _, e := db.db.Exec(`UPDATE legacy_spec_freeze SET frozen=0 WHERE id=1`); e == nil {
		t.Fatal("latch reopened")
	}
}

func TestR11LateResultsAndTaskProgress(t *testing.T) {
	t.Run("LCS-S005_late_review", func(t *testing.T) {
		db, now, q, a := r8Fixture(t, "review")
		before := r11Snapshot(t, db)
		if e := db.EnableLegacySpecFreeze(context.Background()); e != nil {
			t.Fatal(e)
		}
		if _, e := db.CompleteSpecAgentRequest(context.Background(), q.ID, a, r8Result(q), nil, now); e == nil {
			t.Fatal("late review committed")
		}
		if e := db.ReconcileSpecAgentRequests(context.Background(), "new-boot", now.Add(time.Hour)); e == nil {
			t.Fatal("background rewrote frozen request")
		}
		if !reflect.DeepEqual(before, r11Snapshot(t, db)) {
			t.Fatal("late callback changed history")
		}
	})
	t.Run("LCS-S010_inflight", func(t *testing.T) {
		db, now, s := taskFlowFixture(t)
		// Snapshot only the actual spec revision and root; task tables are expected to change.
		var original string
		if e := db.db.QueryRow(`SELECT content_markdown FROM console_spec_revision WHERE spec_id='r32' AND revision=1`).Scan(&original); e != nil {
			t.Fatal(e)
		}
		if e := db.EnableLegacySpecFreeze(context.Background()); e != nil {
			t.Fatal(e)
		}
		var e error
		for _, step := range []struct{ actor, op string }{{"dev", "start"}, {"dev", "mark_case"}, {"review", "confirm_case"}, {"dev", "complete"}} {
			s, e = flowApply(t, db, now, s, step.actor, step.op)
			if e != nil {
				t.Fatalf("inflight %s: %v", step.op, e)
			}
		}
		var after string
		if e = db.db.QueryRow(`SELECT content_markdown FROM console_spec_revision WHERE spec_id='r32' AND revision=1`).Scan(&after); e != nil || after != original {
			t.Fatal("inflight changed original", e)
		}
	})
}

func TestR11FreezeProcessLease(t *testing.T) {
	if dbPath := os.Getenv("R11_TEST_DB"); dbPath != "" {
		db, e := OpenSQLite(dbPath)
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		if e = os.WriteFile(os.Getenv("R11_TEST_READY"), []byte("ready"), 0600); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e = db.EnableLegacySpecFreeze(ctx); e != nil {
			t.Fatal(e)
		}
		return
	}
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	leased, release, e := db.BeginLegacySpecWrite(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestR11FreezeProcessLease$", "-test.count=1")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "R11_TEST_DB=" + strings.TrimSuffix(db.legacySpecLockPath, ".legacy-spec.lock"), "R11_TEST_READY=" + ready}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e = os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child not ready")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case e := <-done:
		t.Fatalf("freeze crossed live lease: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	createSpec(t, db, "before-cutover", "before", time.Now())
	release()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("freeze did not finish")
	}
	if frozen, e := db.LegacySpecsFrozen(ctx); e != nil || !frozen {
		t.Fatal(frozen, e)
	}
	// A context from a completed operation cannot act as a perpetual bypass.
	if _, _, e = db.BeginLegacySpecWrite(leased); !errors.Is(e, ErrLegacySpecFrozen) {
		t.Fatal("expired lease reused", e)
	}
	if _, e = db.AppendConsoleSpecRevision(ctx, "owner-a", "before-cutover", 1, specRevision("before-cutover", 2, "after"), time.Now()); e == nil {
		t.Fatal("post-cutover commit")
	}
}

func TestR11FreezeLeaseCancellation(t *testing.T) {
	db := newConsoleSpecStore(t)
	_, release, e := db.BeginLegacySpecWrite(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if e = db.EnableLegacySpecFreeze(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if frozen, e := db.LegacySpecsFrozen(context.Background()); e != nil || frozen {
		t.Fatal("cancelled cutover changed state", e)
	}
}

// New legacy-spec tables must not accidentally escape the freeze migration.
func TestR11FreezeProtectedTableInventory(t *testing.T) {
	db := newConsoleSpecStore(t)
	rows, e := db.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND (name='console_spec' OR name LIKE 'console_spec_%' OR name LIKE 'pilot_template_%' OR name='pilot_spec_source')`)
	if e != nil {
		t.Fatal(e)
	}
	var tables []string
	for rows.Next() {
		var table string
		if e = rows.Scan(&table); e != nil {
			t.Fatal(e)
		}
		tables = append(tables, table)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	inflight := map[string]bool{"console_spec_task": true, "console_spec_task_acceptance": true, "console_spec_task_case_result": true}
	protected := 0
	for _, table := range tables {
		if inflight[table] {
			continue
		}
		protected++
		for _, op := range []string{"insert", "update", "delete"} {
			var sql string
			name := "r11_freeze_" + table + "_" + op
			if e = db.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name=? AND tbl_name=?`, name, table).Scan(&sql); e != nil || !strings.Contains(sql, "legacy_spec_frozen") {
				t.Fatalf("unguarded %s/%s: %v", table, op, e)
			}
		}
	}
	if protected != 26 {
		t.Fatalf("protected-table inventory changed: %d; review migration and inflight exclusions", protected)
	}
	if e = db.EnableLegacySpecFreeze(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = db.Migrate(context.Background()); e != nil {
		t.Fatal("frozen restart migration", e)
	}
	if frozen, e := db.LegacySpecsFrozen(context.Background()); e != nil || !frozen {
		t.Fatal("migration reopened latch", e)
	}
}

func TestR11FreezeSQLTransactionAtomicity(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	createSpec(t, db, "atomic", "original", time.Now())
	second, e := OpenSQLite(strings.TrimSuffix(db.legacySpecLockPath, ".legacy-spec.lock"))
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	tx, e := db.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE console_spec SET title='complete-before-freeze' WHERE id='atomic'`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		freezeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		done <- second.EnableLegacySpecFreeze(freezeCtx)
	}()
	select {
	case e := <-done:
		t.Fatalf("cutover crossed uncommitted SQL writer: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	var title string
	if e = db.db.QueryRow(`SELECT title FROM console_spec WHERE id='atomic'`).Scan(&title); e != nil || title != "complete-before-freeze" {
		t.Fatal("partial pre-freeze commit", title, e)
	}
	if _, e = db.db.Exec(`UPDATE console_spec SET title='after-freeze' WHERE id='atomic'`); e == nil {
		t.Fatal("post-freeze SQL writer succeeded")
	}
}
