package store

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
)

func TestSubAgentRollbackCheckAllRoots(t *testing.T) {
	db, ctx := newTestSQLite(t)
	check := func(want SubAgentRollbackCheck) {
		t.Helper()
		var before, after int
		if err := db.db.QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		got, err := db.CheckSubAgentRollback(ctx)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("AA009_GLOBAL_ROLLBACK_PENDING got=%+v want=%+v err=%v", got, want, err)
		}
		if err := db.db.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil || before != after {
			t.Fatal("AA009_CHECK_WROTE_DATABASE", before, after, err)
		}
	}
	check(SubAgentRollbackCheck{Roots: []SubAgentRollbackPendingRoot{}})
	for _, root := range []string{"root-b", "root-a"} {
		if err := db.BeginSubAgentConnection(ctx, root, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ReportSubAgentTree(ctx, root, subTree()); err != nil {
			t.Fatal(err)
		}
		scope, err := db.ResolveSubAgent(ctx, root, 1, []string{"aa"})
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{root + "-1"}
		if root == "root-a" {
			ids = append(ids, root+"-2")
		}
		for _, id := range ids {
			if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: id, KeyID: "k", SessionName: "parent", Envelope: `{}`, SubRoute: &subagent.DeliveryRoute{To: scope}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Terminal deliveries and unknown deliveries still require their own audit.
	if _, err := db.db.Exec(`UPDATE session_delivery SET state='processed',ack_generation=7 WHERE id='root-a-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE session_delivery SET state='sent',attempts=1 WHERE id='root-a-2'`); err != nil {
		t.Fatal(err)
	}
	check(SubAgentRollbackCheck{Pending: 3, Roots: []SubAgentRollbackPendingRoot{{"root-a", 2, []string{"root-a-1", "root-a-2"}}, {"root-b", 1, []string{"root-b-1"}}}})
	if _, err := db.PrepareSubAgentRollback(ctx, "root-a"); err != nil {
		t.Fatal(err)
	}
	check(SubAgentRollbackCheck{Pending: 1, Roots: []SubAgentRollbackPendingRoot{{"root-b", 1, []string{"root-b-1"}}}})
	if _, err := db.PrepareSubAgentRollback(ctx, "root-b"); err != nil {
		t.Fatal(err)
	}
	check(SubAgentRollbackCheck{Roots: []SubAgentRollbackPendingRoot{}})
	ack, err := db.GetSessionDelivery(ctx, "root-a-1")
	if err != nil || ack.AckGeneration != 7 || ack.State != "processed" {
		t.Fatal("AA009_CHECK_ACK_PRESERVED", ack, err)
	}
}

func TestSubAgentRollbackCheckSourceOnlyAndInvalidRoute(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "source-only", KeyID: "k", SessionName: "root", Envelope: `{}`}); err != nil {
		t.Fatal(err)
	}
	// Retained routes are inventoried even when the registry no longer has a root.
	if _, err := db.db.Exec(`INSERT INTO session_delivery_sub_route VALUES('source-only','{"from":{"root_agent_id":"retained-source"}}')`); err != nil {
		t.Fatal(err)
	}
	got, err := db.CheckSubAgentRollback(ctx)
	if err != nil || got.Pending != 1 || len(got.Roots) != 1 || got.Roots[0].RootAgentID != "retained-source" {
		t.Fatal("AA009_SOURCE_ONLY_MISSING", got, err)
	}
	for _, raw := range []string{"{", `{}`, `{"to":{}}`} {
		if _, err := db.db.Exec(`UPDATE session_delivery_sub_route SET route_json=?`, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CheckSubAgentRollback(ctx); err == nil {
			t.Fatal("AA009_INVALID_ROUTE_FAIL_CLOSED", raw)
		}
	}
}

func TestSubAgentRollbackReadOnlyOpen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "literal?mode=rw#snapshot.sqlite")
	db, err := OpenSQLiteReadOnly(path)
	if err == nil {
		db.Close()
		t.Fatal("AA009_CHECK_CREATED_MISSING_DATABASE")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read-only open created a file", err)
	}
	// Rename a closed checkpointed database to a literal filename containing URI
	// delimiters, so the reader must escape them rather than accept mode options.
	plain := filepath.Join(t.TempDir(), "existing.sqlite")
	db, err = OpenSQLite(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(plain, path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ro.db.Exec(`CREATE TABLE forbidden_write(x)`); err == nil {
		t.Fatal("AA009_READONLY_WRITE_ACCEPTED")
	}
	got, err := ro.CheckSubAgentRollback(ctx)
	if err != nil || got.Pending != 0 {
		t.Fatal(got, err)
	}
	if err = ro.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("AA009_READONLY_DATABASE_CHANGED", err)
	}
}

func TestSubAgentRollbackReadOnlyIncludesCommittedWAL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "wal.sqlite")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if err = db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "wal-only", KeyID: "k", SessionName: "root", Envelope: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(`INSERT INTO session_delivery_sub_route VALUES('wal-only','{"to":{"root_agent_id":"wal-root"}}')`); err != nil {
		t.Fatal(err)
	}
	beforeDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeWAL, err := os.ReadFile(path + "-wal")
	if err != nil || len(beforeWAL) == 0 {
		t.Fatal("missing WAL fixture", err)
	}
	ro, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ro.CheckSubAgentRollback(ctx)
	if err != nil || got.Pending != 1 || len(got.Roots) != 1 || got.Roots[0].RootAgentID != "wal-root" {
		t.Fatal("AA009_COMMITTED_WAL_IGNORED", got, err)
	}
	if err = ro.Close(); err != nil {
		t.Fatal(err)
	}
	afterDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterWAL, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(beforeDB) != sha256.Sum256(afterDB) || sha256.Sum256(beforeWAL) != sha256.Sum256(afterWAL) {
		t.Fatal("AA009_READONLY_CHECKPOINT_OR_WRITE")
	}
}
