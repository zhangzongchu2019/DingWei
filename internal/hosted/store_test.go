package hosted

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// 集成测试取库三级顺序与 G2 R0 的 pgtest 同一纪律：① HUB_TEST_PG_DSN（独立测试库、永不指生产）
// ② 内嵌 PG（随起随清）③ 都不通则 t.Skip 并说明原因，不静默降级。
// pgtest 自 G2 R0 分支（c7f4ecda）整目录引入，R0 合入主线后去重（merge 时以主线为准）。
// testpool 取库一律 pgtest.Pool——边界闸判据一「仓储包外 sql.Open 恒零」（判据二同守）。
func testpool(t *testing.T) *repo.Pool {
	t.Helper()
	return pgtest.Pool(t)
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
	pool := testpool(t)
	ctx := context.Background()
	s := NewStore(pool)
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

// TestSchemaDropLeavesNoG4Tables：DropSchema 必须把 g4_ 十一表全拆净
// （R-V2-DEPLOY-ARTIFACTS 甲案：DropDDL 曾漏拆 g4_backup——反向：从 DropDDL 去掉
// g4_backup ⇒ 本格红）。apply→drop→apply 全链后核两端：建后十一表齐、拆后零残留。
func TestSchemaDropLeavesNoG4Tables(t *testing.T) {
	pool := testpool(t)
	ctx := context.Background()
	s := NewStore(pool)
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	countG4 := func() int {
		var n int
		// 走租户化入口 s.qrow（不新增 htx() 直调点——边界闸按效果计债，R-BOUNDARY-GATE-REGRESS 还债）
		row, err := s.qrow(ctx,
			"SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'g4\\_%'")
		if err != nil {
			t.Fatalf("数 g4_ 表: %v", err)
		}
		if err := row.Scan(&n); err != nil {
			t.Fatalf("数 g4_ 表: %v", err)
		}
		return n
	}
	if n := countG4(); n != 11 {
		t.Fatalf("建后应有十一张 g4_ 表，实得 %d", n)
	}
	if err := s.DropSchema(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if n := countG4(); n != 0 {
		t.Fatalf("拆后残留 %d 张 g4_ 表（DropDDL 漏拆）", n)
	}
	if err := s.ApplySchema(ctx); err != nil { // 拆净后重建须绿
		t.Fatalf("重建: %v", err)
	}
	if n := countG4(); n != 11 {
		t.Fatalf("重建后应有十一张 g4_ 表，实得 %d", n)
	}
}

func TestApplySchemaUpgradesOldHostedShapeWithoutLosingSeats(t *testing.T) {
	pool := testpool(t)
	ctx := context.Background()
	s := NewStore(pool)
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `INSERT INTO g4_node(id,name) VALUES('nd_old','old') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,plan,created_by)
VALUES('ag_old','nd_old','codex','gpt','token_file','2c4g','zzc') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	// This is the deployed pre-A1 shape: all existing hosted data remains, but
	// the lifecycle-job table/index do not exist yet.
	if _, err := s.exec(ctx, `DROP TABLE g4_lifecycle_job`); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("upgrade old hosted shape: %v", err)
	}
	if seat, err := s.GetSeat(ctx, "ag_old"); err != nil || seat.CreatedBy != "zzc" {
		t.Fatalf("old seat was not preserved: %+v err=%v", seat, err)
	}
	var exists bool
	row, err := s.qrow(ctx, `SELECT to_regclass('g4_lifecycle_job') IS NOT NULL`)
	if err != nil || row.Scan(&exists) != nil || !exists {
		t.Fatalf("lifecycle job table missing after upgrade: exists=%t err=%v", exists, err)
	}
}

// 包 B2（R-V2-SEAT-B2-SCHEMA 裁定）：旧形状表（无 model 列、有存量行）上 ApplySchema ⇒
// 幂等 ALTER 加列、存量行可读（model=”＝节点默认）、新建写入新列。
// 反向：去掉 SchemaDDL 里那行 ALTER ⇒ 本格红。
func TestSchemaModelColumnMigration(t *testing.T) {
	pool := testpool(t)
	ctx := context.Background()
	s := NewStore(pool)
	// 旧形状（生产现状）：g4_hosted_seat 无 model 列
	if _, err := s.exec(ctx, `CREATE TABLE g4_hosted_seat (
  agent_id    TEXT PRIMARY KEY,
  node_id     TEXT NOT NULL,
  cli         TEXT NOT NULL,
  llm         TEXT NOT NULL,
  cred_kind   TEXT NOT NULL,
  account_ref TEXT,
  plan        TEXT NOT NULL,
  state       TEXT NOT NULL DEFAULT 'queued',
  state_detail TEXT NOT NULL DEFAULT '',
  image_ref   TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatalf("旧形状建表: %v", err)
	}
	// 存量行（无 model 列时的形态）
	if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat (agent_id, node_id, cli, llm, cred_kind, plan, created_by)
		VALUES ('ag_old', 'nd_a', 'codex', 'gpt', 'token_file', '2c4g', 'zzc')`); err != nil {
		t.Fatalf("存量行: %v", err)
	}
	// 其余九表＋ALTER（新 Hub 启动路径）
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	old2, err := s.GetSeat(ctx, "ag_old")
	if err != nil {
		t.Fatalf("存量行须可读: %v", err)
	}
	if old2.Model != "" {
		t.Fatalf("存量行 model 应空（＝节点默认）: %q", old2.Model)
	}
	// 新建写入新列
	if err := s.CreateSeat(ctx, Seat{AgentID: "ag_new", NodeID: "nd_a", CLI: "codex", LLM: "gpt",
		CredKind: "token_file", Plan: "2c4g", Model: "gpt-6.1-sol", CreatedBy: "zzc"}); err != nil {
		t.Fatalf("新建写入新列: %v", err)
	}
	newSeat, _ := s.GetSeat(ctx, "ag_new")
	if newSeat.Model != "gpt-6.1-sol" {
		t.Fatalf("新列须写入: %q", newSeat.Model)
	}
	// 列存在直断（信息模式）
	var col string
	row, err := s.qrow(ctx, "SELECT column_name FROM information_schema.columns WHERE table_name='g4_hosted_seat' AND column_name='model'")
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&col); err != nil || col != "model" {
		t.Fatalf("model 列须存在: %v %q", err, col)
	}
	// 回滚兼容（裁定 3②）：旧 Hub 的 INSERT 形状（不含 model 列）在新表上仍成功（靠 DEFAULT ''）
	if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat (agent_id, node_id, cli, llm, cred_kind, plan, created_by)
		VALUES ('ag_legacy', 'nd_a', 'codex', 'gpt', 'token_file', '2c4g', 'zzc')`); err != nil {
		t.Fatalf("旧形状 INSERT 须仍成功: %v", err)
	}
	// 幂等：再 apply 不出错
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("重复 apply: %v", err)
	}
}

func TestNodeSeatPoolQuotaFlow(t *testing.T) {
	pool := testpool(t)
	ctx := context.Background()
	s := NewStore(pool)
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

	// 配额：业主裁定默认值→设上限→已用聚合（disabled 计入）
	q, err := s.GetQuota(ctx, "zzc")
	if err != nil || q.AgentsLimit != 4 || q.MemMBLimit != 16384 || q.DiskMBLimit != 65536 || q.VCPULimit != 4 {
		t.Fatalf("未设置应取默认配额（8971bddf）, 得 %+v err=%v", q, err)
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

// TestAccessCtxValid htx 占位必须恒可用；且 qrow 的错误必须当场可见（M1 返修：不丢 QueryRow error）。
func TestAccessCtxValid(t *testing.T) {
	pool := testpool(t)
	s := NewStore(pool)
	var n int
	row, err := s.qrow(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("qrow 错误必须当场可见（M1 返修）: %v", err)
	}
	if err := row.Scan(&n); err != nil || n != 1 {
		t.Fatalf("htx 占位不可用: n=%d err=%v", n, err)
	}
}
