package store

import (
	"context"
	"path/filepath"
	"testing"

	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

func TestR10MigrationCreatesSeparateTablesAndEmptyReaders(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "r10.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"r10_task_lock", "r10_task_lock_operation", "r10_task_lock_event", "r10_task_lock_outbox"} {
		var n int
		if err = db.db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("R10_NEW_TABLE_NOT_EMPTY table=%s n=%d err=%v", table, n, err)
		}
	}
	k := td.Key{Provider: "github", Instance: "github.com", Repository: "1", ChangeDir: "openspec/changes/example", TaskID: "T-01"}
	if got, e := db.TaskDispatchLock(ctx, k); e != nil || got != nil {
		t.Fatalf("empty lock: %+v %v", got, e)
	}
	if got, e := db.TaskDispatchActive(ctx); e != nil || len(got) != 0 {
		t.Fatalf("empty active: %+v %v", got, e)
	}
	if got, e := db.TaskDispatchEvents(ctx, k, 0); e != nil || len(got) != 0 {
		t.Fatalf("empty events: %+v %v", got, e)
	}
	if got, e := db.TaskDispatchNotices(ctx, "pending", 100); e != nil || len(got) != 0 {
		t.Fatalf("empty notices: %+v %v", got, e)
	}
	if got, e := db.TaskDispatchOperation(ctx, k, "operation_00000001"); e != nil || got != nil {
		t.Fatalf("empty operation: %+v %v", got, e)
	}
}

func TestR10MigrationBackfillRejectsDamagedSchema(t *testing.T) {
	for _, damage := range []string{"missing_unique_index", "changed_table"} {
		t.Run(damage, func(t *testing.T) {
			db, err := OpenSQLite(filepath.Join(t.TempDir(), "damaged.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx := context.Background()
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err = db.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version='0073_task_dispatch_lock.sql'`); err != nil {
				t.Fatal(err)
			}
			sql := `DROP INDEX r10_task_lock_outbox_unique`
			if damage == "changed_table" {
				sql = `ALTER TABLE r10_task_lock ADD COLUMN unexpected TEXT`
			}
			if _, err = db.db.ExecContext(ctx, sql); err != nil {
				t.Fatal(err)
			}
			if err = db.Migrate(ctx); err == nil {
				t.Fatal("R10_DAMAGED_SCHEMA_BACKFILLED")
			}
			var n int
			if err = db.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version='0073_task_dispatch_lock.sql'`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("R10_FAILED_SCHEMA_LEDGER_ADDED n=%d err=%v", n, err)
			}
		})
	}
}
