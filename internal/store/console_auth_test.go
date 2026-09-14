package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestConsoleACLSeparatesPrincipalsDespiteSharedKey(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "console.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.ConsolePrincipal{
		{ID: "principal-a", Provider: "feishu", ProviderIssuer: "app-1", ProviderSubject: "user-a", Active: true},
		{ID: "principal-b", Provider: "feishu", ProviderIssuer: "app-1", ProviderSubject: "user-b", Active: true},
	} {
		if err := db.UpsertConsolePrincipal(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []model.ConsoleAgent{
		{ID: "agent-a", KeyID: "shared-key", SessionName: "session-a", OwnerPrincipal: "principal-a", Active: true},
		{ID: "agent-b", KeyID: "shared-key", SessionName: "session-b", OwnerPrincipal: "principal-b", Active: true},
	} {
		if err := db.UpsertConsoleAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertConsoleAgentGrant(ctx, model.ConsoleAgentGrant{PrincipalID: "principal-a", AgentID: "agent-a", Permission: "interact", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsoleAgentGrant(ctx, model.ConsoleAgentGrant{PrincipalID: "principal-b", AgentID: "agent-b", Permission: "interact", Active: true}); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ConsoleAgentAllowed(ctx, "principal-a", "shared-key", "session-a", "interact"); err != nil || !ok {
		t.Fatalf("own grant ok=%v err=%v", ok, err)
	}
	if ok, err := db.ConsoleAgentAllowed(ctx, "principal-a", "shared-key", "session-b", "view"); err != nil || ok {
		t.Fatalf("cross-principal grant ok=%v err=%v", ok, err)
	}
	agents, err := db.ListConsoleAgents(ctx, "principal-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].ID != "agent-a" {
		t.Fatalf("agents=%+v", agents)
	}
}

func TestConsolePrincipalIssuerIsPartOfIdentity(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "console.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "p", Provider: "feishu", ProviderIssuer: "app-1", ProviderSubject: "same", Active: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := db.ResolveConsolePrincipal(ctx, "feishu", "app-2", "same"); err != nil || got != nil {
		t.Fatalf("cross issuer got=%+v err=%v", got, err)
	}
}
