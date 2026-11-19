package migrate

import (
	"context"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestExternalGroupMigration0113AddsOptionalPairAndStableRoute(t *testing.T) {
	ctx := context.Background()
	pool := testSchema(t, pgtest.DSN(t))
	entries, e := LedgerInDomains(HubDomains())
	if e != nil {
		t.Fatal(e)
	}
	var previous []Entry
	for _, entry := range entries {
		if entry.Name != "0113_extseat_groups.sql" {
			previous = append(previous, entry)
		}
	}
	if _, e = MigrateEntries(ctx, pool, previous, nil); e != nil {
		t.Fatal(e)
	}
	before := ledgerRows(t, pool)
	report, e := Migrate(ctx, pool, HubDomains())
	if e != nil || len(report.Applied) != 1 || report.Applied[0] != "0113_extseat_groups.sql" {
		t.Fatalf("0113 must be the sole pending migration: %+v %v", report, e)
	}
	after := ledgerRows(t, pool)
	for name, old := range before {
		if after[name].digest != old.digest || !after[name].appliedAt.Equal(old.appliedAt) {
			t.Fatalf("0113 rewrote old ledger entry %s", name)
		}
	}
	// 不以改总数代替形状检查：可选配对必须成对可空，路由有自己的稳定引用。
	for _, column := range []string{"ag_code", "ag_generation"} {
		var nullable string
		row, e := pool.QueryRow(ctx, testCtx(t), `SELECT is_nullable FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='np_extseat' AND column_name=$1`, column)
		if e != nil {
			t.Fatal(e)
		}
		if e = row.Scan(&nullable); e != nil || nullable != "YES" {
			t.Fatalf("optional pair column %s is not nullable: %s %v", column, nullable, e)
		}
	}
	for _, item := range []struct{ table, column string }{{"np_extseat", "routing_enabled"}, {"np_extseat_inbox", "route_kind"}, {"np_extseat_inbox", "route_ref"}, {"np_extseat_inbox", "route_generation"}, {"np_extseat_outbox", "route_ref"}, {"np_extseat_send_key", "target_agent_id"}} {
		var n int
		row, e := pool.QueryRow(ctx, testCtx(t), `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2`, item.table, item.column)
		if e != nil {
			t.Fatal(e)
		}
		if e = row.Scan(&n); e != nil || n != 1 {
			t.Fatalf("missing group binding %s.%s: %v", item.table, item.column, e)
		}
	}
	for _, constraint := range []string{"np_extseat_optional_pair", "np_extseat_inbox_ordinary_binding", "np_extseat_outbox_ordinary_binding", "np_extseat_send_key_ordinary_binding"} {
		var n int
		row, e := pool.QueryRow(ctx, testCtx(t), `SELECT count(*) FROM pg_constraint WHERE connamespace=current_schema()::regnamespace AND conname=$1`, constraint)
		if e != nil {
			t.Fatal(e)
		}
		if e = row.Scan(&n); e != nil || n != 1 {
			t.Fatalf("missing binding constraint %s: %v", constraint, e)
		}
	}
	if repeated, e := Migrate(ctx, pool, HubDomains()); e != nil || len(repeated.Applied) != 0 {
		t.Fatalf("0113 reapplied: %+v %v", repeated, e)
	}
}
