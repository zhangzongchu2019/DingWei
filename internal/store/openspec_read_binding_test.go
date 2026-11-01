package store

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"path/filepath"
	"testing"
)

func TestR5BindingPersistenceIsolationAuditAndRevocation(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t, filepath.Join(t.TempDir(), "binding.db"))
	defer db.Close()
	var n int
	if e := db.db.QueryRow(`SELECT COUNT(*) FROM openspec_read_binding`).Scan(&n); e != nil || n != 0 {
		t.Fatal("migration seeded bindings", n, e)
	}
	for _, id := range []string{"alice", "bob"} {
		if e := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: id, Provider: "feishu", ProviderIssuer: "test", ProviderSubject: id, DisplayName: id, Active: true}); e != nil {
			t.Fatal(e)
		}
	}
	b := specread.Binding{Principal: "alice", Provider: "github", Instance: "github.com", AccountID: "123", Login: "alice"}
	if e := db.SetSpecReadBinding(ctx, "admin", b); e != nil {
		t.Fatal(e)
	}
	got, e := db.SpecReadBinding(ctx, "alice", "github", "github.com")
	if e != nil || got == nil || *got != b {
		t.Fatal(got, e)
	}
	for _, c := range [][3]string{{"bob", "github", "github.com"}, {"alice", "github", "evil.invalid"}, {"alice", "gongfeng", "github.com"}} {
		if got, e := db.SpecReadBinding(ctx, c[0], c[1], c[2]); e != nil || got != nil {
			t.Fatal("binding crossed identity", got, e)
		}
	}
	b.AccountID = "456"
	if e := db.SetSpecReadBinding(ctx, "admin2", b); e != nil {
		t.Fatal(e)
	}
	if e := db.DeleteSpecReadBinding(ctx, "admin3", "alice", "github", "github.com"); e != nil {
		t.Fatal(e)
	}
	if got, e := db.SpecReadBinding(ctx, "alice", "github", "github.com"); e != nil || got != nil {
		t.Fatal("binding removal ignored")
	}
	if e := db.db.QueryRow(`SELECT COUNT(*) FROM openspec_read_binding_audit`).Scan(&n); e != nil || n != 3 {
		t.Fatal("missing audit", n, e)
	}
	if e := db.SetSpecReadBinding(ctx, "admin", b); e != nil {
		t.Fatal(e)
	}
	if _, e := db.db.Exec(`UPDATE console_principal SET active=0 WHERE id='alice'`); e != nil {
		t.Fatal(e)
	}
	if got, e := db.SpecReadBinding(ctx, "alice", "github", "github.com"); e != nil || got != nil {
		t.Fatal("inactive principal accepted")
	}
}

func TestR5BindingMigrationLedgerLossAndDrift(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t, filepath.Join(t.TempDir(), "ledger.db"))
	defer db.Close()
	if _, e := db.db.Exec(`DELETE FROM schema_migrations WHERE version='0070_openspec_read_binding.sql'`); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrate(ctx); e != nil {
		t.Fatal("cannot backfill exact binding schema", e)
	}
	if _, e := db.db.Exec(`DELETE FROM schema_migrations WHERE version='0070_openspec_read_binding.sql'; ALTER TABLE openspec_read_binding ADD COLUMN unexpected TEXT`); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrate(ctx); e == nil {
		t.Fatal("schema drift silently accepted")
	}
}
