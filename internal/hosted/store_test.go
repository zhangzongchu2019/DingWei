package hosted

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// 集成测试取库三级顺序与 G2 R0 的 pgtest 同一纪律：① HUB_TEST_PG_DSN（独立测试库、永不指生产）
// ② 内嵌 PG（随起随清）③ 都不通则 t.Skip 并说明原因，不静默降级。
// pgtest 自 G2 R0 分支（c7f4ecda）整目录引入，R0 合入主线后去重（merge 时以主线为准）。
func dsnpool(t *testing.T) *sql.DB {
	t.Helper()
	dsn := pgtest.DSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPlanTable(t *testing.T) {
	p, ok := PlanByID("2c4g")
	if !ok || p.VCPU != 2 || p.MemMB != 4096 {
		t.Fatalf("2c4g 读数错: %+v ok=%v", p, ok)
	}
	if _, ok := PlanByID("3c16g"); ok {
		t.Fatal("未知套餐不应存在")
	}
	if got := PlanIDs(); len(got) != 2 || got[0] != "2c4g" || got[1] != "4c8g" {
		t.Fatalf("PlanIDs 顺序错: %v", got)
	}
}

func TestSchemaApplyRollbackRepeatable(t *testing.T) {
	db := dsnpool(t)
	ctx := context.Background()
	s := NewStore(db)
	for i := 0; i < 2; i++ { // apply 可重复
		if err := s.ApplySchema(ctx); err != nil {
			t.Fatalf("apply #%d: %v", i, err)
		}
	}
	if err := s.DropSchema(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := s.DropSchema(ctx); err != nil { // rollback 可重复
		t.Fatalf("drop 重复: %v", err)
	}
	if err := s.ApplySchema(ctx); err != nil { // 拆后可重建
		t.Fatalf("重建: %v", err)
	}
}

func TestNodeSeatPoolQuotaFlow(t *testing.T) {
	db := dsnpool(t)
	ctx := context.Background()
	s := NewStore(db)
	if err := s.DropSchema(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// 节点：登记→离线；心跳→在线＋容量；自动落位取余量最大者
	if err := s.UpsertNode(ctx, Node{ID: "nd_a", Name: "node-a", Labels: json.RawMessage(`{"pool":"p1"}`), DockerVersion: "29.1.3", ProtocolVersion: 1}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := s.UpsertNode(ctx, Node{ID: "nd_b", Name: "node-b", Labels: json.RawMessage(`{"pool":"p1"}`), DockerVersion: "29.1.3", ProtocolVersion: 1}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}
	if _, err := s.PickNodeAuto(ctx); err != ErrNotFound {
		t.Fatalf("无 online 节点应 ErrNotFound, 得 %v", err)
	}
	if err := s.SetNodeOnline(ctx, "nd_a", json.RawMessage(`{"mem_free_mb":4096}`)); err != nil {
		t.Fatalf("online a: %v", err)
	}
	if err := s.SetNodeOnline(ctx, "nd_b", json.RawMessage(`{"mem_free_mb":8192}`)); err != nil {
		t.Fatalf("online b: %v", err)
	}
	picked, err := s.PickNodeAuto(ctx)
	if err != nil || picked.ID != "nd_b" {
		t.Fatalf("自动落位应取余量最大 nd_b, 得 %+v err=%v", picked, err)
	}

	// 席位：建→状态机迁移＋事件
	st := Seat{AgentID: "ag_1", NodeID: picked.ID, CLI: "codex", LLM: "gpt", CredKind: "token_file",
		Plan: "2c4g", ImageRef: "hub/base@sha256:x", CreatedBy: "tester"}
	if err := s.CreateSeat(ctx, st); err != nil {
		t.Fatalf("create seat: %v", err)
	}
	if err := s.SetSeatState(ctx, "ag_1", "online", "", "online", nil); err != nil {
		t.Fatalf("seat online: %v", err)
	}
	if err := s.SetSeatState(ctx, "ag_1", "bogus", "", "fail", nil); err != ErrInvalidState {
		t.Fatalf("非法状态应 ErrInvalidState, 得 %v", err)
	}

	// 账号池：一席一账号（并发安全的反向锚）
	acct := Account{ID: "acct_1", Name: "team-codex", Kind: "token_file", CLI: "codex", LLM: "gpt", KeyVersion: 1}
	if err := s.CreateAccount(ctx, acct, []byte("enc"), "tester"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := s.GrantAccount(ctx, "acct_1", "zzc", "tester"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := s.BindAccount(ctx, "acct_1", "ag_1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := s.BindAccount(ctx, "acct_1", "ag_2"); err != ErrAccountBound {
		t.Fatalf("一席一账号反向应 ErrAccountBound, 得 %v", err)
	}
	usable, err := s.ListUsableAccounts(ctx, "zzc", "codex", "gpt")
	if err != nil {
		t.Fatalf("list usable: %v", err)
	}
	if len(usable) != 0 { // 已绑定账号不再可选（V6 表单事实源）
		t.Fatalf("绑定后不应再出现在下拉, 得 %+v", usable)
	}
	if err := s.UnbindAccount(ctx, "acct_1"); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	usable, err = s.ListUsableAccounts(ctx, "zzc", "codex", "gpt")
	if err != nil || len(usable) != 1 {
		t.Fatalf("回池后应重新可选, 得 %+v err=%v", usable, err)
	}

	// 配额：默认零→设上限→已用聚合（disabled 计入）
	q, err := s.GetQuota(ctx, "zzc")
	if err != nil || q.AgentsLimit != 0 {
		t.Fatalf("未设置应默认零, 得 %+v err=%v", q, err)
	}
	if err := s.PutQuota(ctx, Quota{UserLC: "zzc", AgentsLimit: 2, VCPULimit: 4, MemMBLimit: 8192}, "tester"); err != nil {
		t.Fatalf("put quota: %v", err)
	}
	u, err := s.UsageFor(ctx, []string{"ag_1"})
	if err != nil || u.Agents != 1 || u.VCPU != 2 || u.MemMB != 4096 {
		t.Fatalf("usage 聚合错: %+v err=%v", u, err)
	}
	if err := s.SetSeatState(ctx, "ag_1", "disabled", "", "disable", nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	u, err = s.UsageFor(ctx, []string{"ag_1"})
	if err != nil || u.Agents != 1 {
		t.Fatalf("disabled 应仍计入, 得 %+v err=%v", u, err)
	}
	if err := s.SetSeatState(ctx, "ag_1", "destroyed", "", "destroy", nil); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	u, err = s.UsageFor(ctx, []string{"ag_1"})
	if err != nil || u.Agents != 0 {
		t.Fatalf("destroyed 应不计入, 得 %+v err=%v", u, err)
	}

	// 审计
	if err := s.AddAudit(ctx, "tester", "account.bind", "acct_1", json.RawMessage(`{"agent":"ag_1"}`)); err != nil {
		t.Fatalf("audit: %v", err)
	}
}
