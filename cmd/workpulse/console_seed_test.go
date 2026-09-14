package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestConsoleSeedIsExplicitAndSeparatesSharedKey(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.json")
	raw := `{"principals":[{"id":"a","provider":"feishu","provider_issuer":"app","provider_subject":"ua","active":true},{"id":"b","provider":"feishu","provider_issuer":"app","provider_subject":"ub","active":true}],"agents":[{"id":"aa","key_id":"shared","session_name":"sa","owner_principal":"a","active":true,"grants":[{"principal_id":"a","permission":"interact"}]},{"id":"ab","key_id":"shared","session_name":"sb","owner_principal":"b","active":true,"grants":[{"principal_id":"b","permission":"interact"}]}]}`
	if err = os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err = seedConsoleACL(ctx, db, path); err != nil {
		t.Fatal(err)
	}
	a, err := db.ListConsoleAgents(ctx, "a")
	if err != nil || len(a) != 1 || a[0].SessionName != "sa" {
		t.Fatalf("a=%+v err=%v", a, err)
	}
	if ok, err := db.ConsoleAgentAllowed(ctx, "a", "shared", "sb", "view"); err != nil || ok {
		t.Fatalf("cross access ok=%v err=%v", ok, err)
	}
}

func TestConsoleSeedRequiresExplicitActive(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.json")
	_ = os.WriteFile(path, []byte(`{"principals":[{"id":"a","provider":"feishu","provider_issuer":"app","provider_subject":"ua"}],"agents":[{"id":"aa","key_id":"k","session_name":"s","owner_principal":"a","active":true,"grants":[{"principal_id":"a","permission":"view"}]}]}`), 0600)
	if err = seedConsoleACL(ctx, db, path); err == nil || !strings.Contains(err.Error(), "active is required") {
		t.Fatalf("err=%v", err)
	}
}

func TestConsoleSeedFailsClosedWithoutGrant(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.json")
	_ = os.WriteFile(path, []byte(`{"principals":[{"id":"a","provider":"feishu","provider_issuer":"app","provider_subject":"ua","active":true}],"agents":[{"id":"aa","key_id":"k","session_name":"s","owner_principal":"a","active":true,"grants":[]}]}`), 0600)
	if err = seedConsoleACL(ctx, db, path); err == nil {
		t.Fatal("agent without explicit grant accepted")
	}
}
