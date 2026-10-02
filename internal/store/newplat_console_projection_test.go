package store

import (
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-SPEC-TASK-SHIM-IMPL H15（存储侧）：一个用户的投影一个事务写完——中途失败整体回滚，不留半投影。
func TestNewPlatformProjectionIsAtomic(t *testing.T) {
	s, ctx := newTestSQLite(t)
	now := time.Now()
	bad := model.NewPlatformConsoleProjection{PrincipalID: "alice", Issuer: "dingwei", DisplayName: "al", OwnerKey: "u_alice", Active: true, Management: true, Now: now,
		Agents: []model.NewPlatformConsoleAgent{{ID: "ag_1", SessionName: "kimi", Active: true}, {ID: "ag_2", SessionName: "", Active: true}}} // 第二个 Agent 非法 ⇒ 在写完 principal、角色、第一个 Agent 之后失败
	if _, err := s.SyncNewPlatformConsoleIdentity(ctx, bad); err == nil {
		t.Fatal("invalid projection accepted")
	}
	if p, err := s.GetConsolePrincipal(ctx, "alice"); err != nil || p != nil {
		t.Fatalf("principal left behind by a failed projection: %+v %v", p, err)
	}
	if agents, _ := s.ListOwnedConsoleAgents(ctx, "alice"); len(agents) != 0 {
		t.Fatalf("agents left behind: %+v", agents)
	}
	if ids, _ := s.ListNewPlatformConsolePrincipalIDs(ctx); len(ids) != 0 {
		t.Fatalf("projected principals after failure: %v", ids)
	}
	// 同一用户改成合法后照常写入。
	bad.Agents = bad.Agents[:1]
	if _, err := s.SyncNewPlatformConsoleIdentity(ctx, bad); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.ResolveConsolePlatformRole(ctx, "alice"); role != "management" {
		t.Fatalf("role=%q, want management", role)
	}
}

// 不覆盖旧世界的人：id 已被非 newplat 主体占用 ⇒ 拒，旧主体一字不动。
func TestNewPlatformProjectionRefusesLegacyPrincipalID(t *testing.T) {
	s, ctx := newTestSQLite(t)
	if err := s.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "bob", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ou_b", DisplayName: "老鲍", Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncNewPlatformConsoleIdentity(ctx, model.NewPlatformConsoleProjection{PrincipalID: "bob", Issuer: "dingwei", DisplayName: "bo", OwnerKey: "u_bob", Active: true, Now: time.Now()}); err == nil {
		t.Fatal("projection overwrote a legacy principal id")
	}
	if p, _ := s.GetConsolePrincipal(ctx, "bob"); p == nil || p.Provider != "feishu" || p.DisplayName != "老鲍" {
		t.Fatalf("legacy principal changed: %+v", p)
	}
}
