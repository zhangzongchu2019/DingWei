package main

import (
	"context"
	"os"
	"path/filepath"
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
	raw := `{"principals":[{"ID":"a","Provider":"feishu","ProviderIssuer":"app","ProviderSubject":"ua","Active":true},{"ID":"b","Provider":"feishu","ProviderIssuer":"app","ProviderSubject":"ub","Active":true}],"agents":[{"ID":"aa","KeyID":"shared","SessionName":"sa","OwnerPrincipal":"a","Active":true,"grants":[{"principal_id":"a","permission":"interact"}]},{"ID":"ab","KeyID":"shared","SessionName":"sb","OwnerPrincipal":"b","Active":true,"grants":[{"principal_id":"b","permission":"interact"}]}]}`
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
	_ = os.WriteFile(path, []byte(`{"principals":[{"ID":"a","Provider":"feishu","ProviderIssuer":"app","ProviderSubject":"ua","Active":true}],"agents":[{"ID":"aa","KeyID":"k","SessionName":"s","OwnerPrincipal":"a","Active":true,"grants":[]}]}`), 0600)
	if err = seedConsoleACL(ctx, db, path); err == nil {
		t.Fatal("agent without explicit grant accepted")
	}
}
