package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// ---------- 注入假件 ----------

type fakeIdentity struct {
	issued   []string
	retired  []string
	agentIDs []string
	issueErr error
	nextID   int
}

func (f *fakeIdentity) IssueHostedAgent(ctx context.Context, ownerLC, name, groupCode, by string) (IssuedAgent, EnrollToken, error) {
	if f.issueErr != nil {
		return IssuedAgent{}, EnrollToken{}, f.issueErr
	}
	f.nextID++
	id := "ag_fake" + string(rune('a'+f.nextID))
	f.issued = append(f.issued, id)
	f.agentIDs = append(f.agentIDs, id)
	return IssuedAgent{ID: id, FullName: name + ".zzc"}, EnrollToken{ApplicationID: "app1", PollToken: "tok", ExpiresAt: time.Now().Add(2 * time.Hour)}, nil
}

func (f *fakeIdentity) RetireHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	f.retired = append(f.retired, agentID)
	return false, nil
}

func (f *fakeIdentity) ListHostedAgentIDsByOwner(ctx context.Context, ownerLC string) ([]string, error) {
	return f.agentIDs, nil
}

type fakeGateway struct {
	sent   []NodeCommand
	failOn string // 命中该 Type 即返回失败（反向）
}

func (f *fakeGateway) Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
	f.sent = append(f.sent, cmd)
	if cmd.Type == f.failOn {
		return NodeResult{OK: false, Error: "injected"}, nil
	}
	return NodeResult{OK: true}, nil
}

type fakeCombos struct{ m map[string]bool } // key: cli|llm|kind → verified；不在表＝不兼容

func (c fakeCombos) Lookup(cli, llm, kind string) (bool, bool) {
	v, ok := c.m[cli+"|"+llm+"|"+kind]
	return v, ok
}

func (c fakeCombos) List() []Combo {
	var out []Combo
	for k, v := range c.m {
		parts := strings.SplitN(k, "|", 3)
		if len(parts) != 3 {
			continue
		}
		out = append(out, Combo{CLI: parts[0], LLM: parts[1], CredKind: parts[2], Verified: v})
	}
	return out
}

type fakeCatalog struct{}

func (fakeCatalog) Latest(cli string) (string, bool) {
	return "hub.local/dingwei/base-" + cli + "@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true
}

func mkOrch(t *testing.T) (*Orchestrator, *fakeIdentity, *fakeGateway, *Store) {
	t.Helper()
	svc, s := mkPoolSvc(t)
	idp := &fakeIdentity{}
	gw := &fakeGateway{}
	combos := fakeCombos{m: map[string]bool{"codex|gpt|token_file": true}}
	o := NewOrchestrator(s, svc, idp, gw, combos, fakeCatalog{})
	// 节点两枚：一满一空
	if err := s.UpsertNode(context.Background(), Node{ID: "nd_full", Name: "full", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(context.Background(), Node{ID: "nd_free", Name: "free", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(context.Background(), "nd_full", json.RawMessage(`{"mem_free_mb":1024}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(context.Background(), "nd_free", json.RawMessage(`{"mem_free_mb":16384}`)); err != nil {
		t.Fatal(err)
	}
	// 配额与账号
	if err := s.PutQuota(context.Background(), Quota{UserLC: "zzc", AgentsLimit: 2, VCPULimit: 4, MemMBLimit: 8192}, "admin"); err != nil {
		t.Fatal(err)
	}
	a, err := svc.Register(context.Background(), "admin", true, "team-codex", "token_file", "codex", "gpt", "sk-x")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Grant(context.Background(), "admin", true, a.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a })
	return o, idp, gw, s
}

func acctOf(t *testing.T, svc *PoolService) string {
	t.Helper()
	lst, err := svc.ListAdmin(context.Background(), "admin", true)
	if err != nil || len(lst) == 0 {
		t.Fatalf("无账号: %v", err)
	}
	return lst[0].ID
}

func TestCreateHappyPathCodexFirst(t *testing.T) {
	o, idp, gw, s := mkOrch(t)
	var acctID string
	{
		svc := o.pool
		acctID = acctOf(t, svc)
	}
	out, err := o.Create(context.Background(), CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "worker1", GroupCode: "g1",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctID, Plan: "2c4g",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.State != "queued" {
		t.Fatalf("应 queued: %+v", out)
	}
	// 自动落位＝余量最大者
	seat, err := s.GetSeat(context.Background(), out.AgentID)
	if err != nil || seat.NodeID != "nd_free" {
		t.Fatalf("落位: %+v err=%v", seat, err)
	}
	if seat.ImageRef == "" {
		t.Fatal("镜像 ref 应来自版本登记")
	}
	// 执行链：pull→create→starting；Agent 以专属凭据连入（MarkOnline）才 online（板 ac33b1cb）
	o.RunJob(context.Background(), out.AgentID)
	seat, _ = s.GetSeat(context.Background(), out.AgentID)
	if seat.State != "starting" {
		t.Fatalf("容器起来后、Agent 连入前应 starting: %+v", seat)
	}
	if err := o.MarkOnline(context.Background(), out.AgentID); err != nil {
		t.Fatal(err)
	}
	seat, _ = s.GetSeat(context.Background(), out.AgentID)
	if seat.State != "online" {
		t.Fatalf("Agent 连入后应 online: %+v", seat)
	}
	if len(gw.sent) != 2 || gw.sent[0].Type != "pull_image" || gw.sent[1].Type != "create_seat" {
		t.Fatalf("指令序列: %+v", gw.sent)
	}
	// T-G4-32：create_seat 参数带齐——resources（套餐换算）、token_file（一次性入网令牌）、
	// credential_file（解密一次的账号凭据）
	var cp struct {
		SeatID    string `json:"seat_id"`
		Image     string `json:"image"`
		Resources struct {
			MemoryMB  int64 `json:"memory_mb"`
			CPUShares int   `json:"cpu_shares"`
			DiskMB    int64 `json:"disk_mb"`
		} `json:"resources"`
		Token struct {
			ApplicationID string `json:"application_id"`
			PollToken     string `json:"poll_token"`
			ExpiresAt     string `json:"expires_at"`
		} `json:"token_file"`
		Cred struct {
			Content string `json:"content"`
		} `json:"credential_file"`
	}
	if err := json.Unmarshal(gw.sent[1].Params, &cp); err != nil {
		t.Fatalf("create_seat 参数: %v", err)
	}
	if cp.Resources.MemoryMB != 4096 || cp.Resources.CPUShares != 2048 { // 2c4g ⇒ clamp(1024×2)
		t.Fatalf("resources 换算: %+v", cp.Resources)
	}
	if cp.Token.ApplicationID != "app1" || cp.Token.PollToken != "tok" || cp.Token.ExpiresAt == "" {
		t.Fatalf("token_file: %+v", cp.Token)
	}
	if cp.Cred.Content != "sk-x" {
		t.Fatalf("credential_file 应是解密明文一次下发: %q", cp.Cred.Content)
	}
	// 解密审计恰好一条
	row, err := s.qrow(context.Background(), `SELECT COUNT(*) FROM g4_audit WHERE action='account.decrypt_issue' AND target=$1`, acctID)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := row.Scan(&n); err != nil || n != 1 {
		t.Fatalf("解密审计应恰好一条: n=%d err=%v", n, err)
	}
	if len(idp.retired) != 0 {
		t.Fatalf("成功路径不应 Retire: %v", idp.retired)
	}
}

func TestCreateRejections(t *testing.T) {
	o, idp, _, s := mkOrch(t)
	acctID := acctOf(t, o.pool)
	base := CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w", GroupCode: "g1",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctID, Plan: "2c4g"}

	// 不兼容组合（不在表）
	r := base
	r.LLM = "claude"
	if _, err := o.Create(context.Background(), r); err != ErrComboIncompatible {
		t.Fatalf("应 combo_incompatible, 得 %v", err)
	}
	// 未验通组合
	o.combos = fakeCombos{m: map[string]bool{"codex|gpt|token_file": false}}
	if _, err := o.Create(context.Background(), base); err != ErrComboNotVerified {
		t.Fatalf("应 combo_not_verified, 得 %v", err)
	}
	o.combos = fakeCombos{m: map[string]bool{"codex|gpt|token_file": true}}
	// 超配额（Agent 数：先发两席占满，再发第三席）
	for i := 0; i < 2; i++ {
		r := base
		r.Name = "w" + string(rune('a'+i))
		r.AccountID = acctID
		if i == 0 {
			if _, err := o.Create(context.Background(), r); err != nil {
				t.Fatalf("第 %d 席应成: %v", i, err)
			}
		}
	}
	// 第二席账号已被绑定 ⇒ account_not_allowed；且在账号预检（步骤④）就拒——
	// 根本不签发、不占名字（比"签了再 Retire"更好；Retire 兜底留给签发后的真竞态/落库失败）
	r2 := base
	r2.Name = "w1"
	issuedBefore := len(idp.issued)
	if _, err := o.Create(context.Background(), r2); err != ErrAccountNotAllowed {
		t.Fatalf("账号已绑应 account_not_allowed, 得 %v", err)
	}
	if len(idp.issued) != issuedBefore {
		t.Fatal("账号预检拒绝不应签发（不占名字）")
	}
	// 指定离线节点 ⇒ node_no_capacity（需一个能过账号预检的新账号，才能走到节点格）
	a2, err := o.pool.Register(context.Background(), "admin", true, "team-codex-2", "token_file", "codex", "gpt", "sk-y")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.pool.Grant(context.Background(), "admin", true, a2.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	r3 := base
	r3.NodeID = "nd_offline"
	r3.AccountID = a2.ID
	if _, err := o.Create(context.Background(), r3); err != ErrNoNodeCapacity {
		t.Fatalf("离线节点应 node_no_capacity, 得 %v", err)
	}
	// 配额满员：agents_limit=2 已发 1 席，再设 limit=1 ⇒ 新上限低于已用量允许保存、只限新建
	if err := s.PutQuota(context.Background(), Quota{UserLC: "zzc", AgentsLimit: 1, VCPULimit: 4, MemMBLimit: 8192}, "admin"); err != nil {
		t.Fatal(err)
	}
	r4 := base
	r4.AccountID = "acct_any"
	if _, err := o.Create(context.Background(), r4); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("满额新建应 quota_exceeded, 得 %v", err)
	}
}

func TestRunJobFailureCleanup(t *testing.T) {
	o, idp, gw, _ := mkOrch(t)
	gw.failOn = "pull_image"
	out, err := o.Create(context.Background(), CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "worker9", GroupCode: "g1",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	o.RunJob(context.Background(), out.AgentID)
	seat, _ := o.st.GetSeat(context.Background(), out.AgentID)
	if seat.State != "error" || seat.StateDetail != "image_pull_failed" {
		t.Fatalf("失败应 error/image_pull_failed: %+v", seat)
	}
	if len(idp.retired) != 1 || idp.retired[0] != out.AgentID {
		t.Fatalf("失败必须 Retire: %v", idp.retired)
	}
	// 账号解绑回池
	acct, _ := o.st.GetAccount(context.Background(), acctOf(t, o.pool))
	if acct.State != "available" {
		t.Fatalf("失败必须解绑回池: %+v", acct)
	}
}

func TestSweepTimeouts(t *testing.T) {
	o, idp, _, _ := mkOrch(t)
	out, err := o.Create(context.Background(), CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "worker7", GroupCode: "g1",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// queued 态 11 分钟 ⇒ queue_timeout
	o.SweepTimeouts(context.Background(), time.Now().Add(11*time.Minute))
	seat, _ := o.st.GetSeat(context.Background(), out.AgentID)
	if seat.State != "error" || seat.StateDetail != "queue_timeout" {
		t.Fatalf("应 queue_timeout: %+v", seat)
	}
	if len(idp.retired) != 1 {
		t.Fatalf("超时必须 Retire: %v", idp.retired)
	}
}

var _ = errors.New

func TestPoolScheduling(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	// 三个节点：pool-a 大余量、pool-b 小余量（标签落位不得被全池最闲抢走——D1 反向）
	if err := s.UpsertNode(ctx, Node{ID: "nd_pa", Name: "pa", Labels: json.RawMessage(`{"pool":"a"}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(ctx, "nd_pa", json.RawMessage(`{"mem_free_mb":32768}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(ctx, Node{ID: "nd_pb", Name: "pb", Labels: json.RawMessage(`{"pool":"b"}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(ctx, "nd_pb", json.RawMessage(`{"mem_free_mb":8192}`)); err != nil {
		t.Fatal(err)
	}
	// 指定 pool=b ⇒ 必须落在 b（尽管 a 更闲）
	n, err := o.pickNode(ctx, "auto", "b", Plan{ID: "2c4g", VCPU: 2, MemMB: 4096})
	if err != nil || n.ID != "nd_pb" {
		t.Fatalf("指定池应落 nd_pb: %+v err=%v", n, err)
	}
	// 指定不存在的池 ⇒ node_no_capacity
	if _, err := o.pickNode(ctx, "auto", "no-such-pool", Plan{ID: "2c4g"}); err != ErrNoNodeCapacity {
		t.Fatalf("空池应 node_no_capacity, 得 %v", err)
	}
	// 不指定 ⇒ 全池最闲（nd_pa 32768）
	n, err = o.pickNode(ctx, "auto", "", Plan{ID: "2c4g"})
	if err != nil || n.ID != "nd_pa" {
		t.Fatalf("不指定应全池最闲 nd_pa: %+v err=%v", n, err)
	}
}

// 业主令 8582acc4：编组可不选——不给编组的创建必成功且不建组（反向：强制编组必红）。
func TestCreateWithoutGroupSucceeds(t *testing.T) {
	o, _, _, _ := mkOrch(t)
	out, err := o.Create(context.Background(), CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "nogroup1", GroupCode: "", // 不选编组
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file",
		AccountID: acctOf(t, o.pool), Plan: "2c4g",
	})
	if err != nil {
		t.Fatalf("不给编组的创建必须成功: %v", err)
	}
	if out.State != "queued" {
		t.Fatalf("应 queued: %+v", out)
	}
}

// 解冻批①（T-G4-50 解冻后对齐 G3 终版 800f24d8）：编组错误码映射——签发侧报
// ErrGroupNotFound/ErrGroupClosed 时 Create 原样透传哨兵（契约机器码），且不留任何副作用
// （未签发成功 ⇒ 无席位、无绑定、无 Retire）。
func TestCreateGroupErrorsPropagate(t *testing.T) {
	for _, want := range []error{ErrGroupNotFound, ErrGroupClosed} {
		o, idp, _, s := mkOrch(t)
		acctID := acctOf(t, o.pool)
		idp.issueErr = want
		_, err := o.Create(context.Background(), CreateRequest{
			OwnerLC: "zzc", By: "zzc", Name: "w", GroupCode: "g-dead",
			NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctID, Plan: "2c4g",
		})
		if !errors.Is(err, want) {
			t.Fatalf("应透传 %v, 得 %v", want, err)
		}
		if len(idp.issued) != 0 || len(idp.retired) != 0 {
			t.Fatalf("签发失败不应有签发/吊销副作用: %+v", idp)
		}
		_ = s
	}
}

// 解冻批②（T-G4-51）：hosted 列表注入——G3 注入点形状 HostedItem(ctx, agentID)
// (map[string]any, bool)；契约字段 node_name/state/upgradable/image_version/plan/account_last4，
// token_file 类出 account_kind＋account_registered_at。状态映射逐格＋缺席语义＋账号两形态。
func TestHostedOverlay(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	mkSeat := func(agentID, nodeID, state, imageRef string) {
		if err := s.CreateSeat(ctx, Seat{AgentID: agentID, NodeID: nodeID, CLI: "codex", LLM: "gpt",
			CredKind: "token_file", Plan: "2c4g", ImageRef: imageRef, CreatedBy: "zzc"}); err != nil {
			t.Fatal(err)
		}
		if state != "queued" {
			if err := s.SetSeatState(ctx, agentID, state, "", "", nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	mkSeat("ag_run", "nd_free", "online", "old-ref")     // running＋upgradable
	mkSeat("ag_dis", "nd_free", "disabled", "old-ref")   // disabled（不可升级）
	mkSeat("ag_busy", "nd_free", "busy", "old-ref")      // busy
	mkSeat("ag_off", "nd_full", "online", "old-ref")     // 节点离线 ⇒ abnormal
	mkSeat("ag_err", "nd_free", "error", "old-ref")      // error ⇒ abnormal
	mkSeat("ag_dead", "nd_free", "destroyed", "old-ref") // 已销毁 ⇒ 缺席
	mkSeat("ag_fresh", "nd_free", "online", "hub.local/dingwei/base-codex@sha256:"+
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") // 已最新
	// 账号两形态：token_file（池里那枚）绑 ag_run；另登记一枚 api_key 绑 ag_dis
	acctID := acctOf(t, o.pool)
	if err := s.BindAccount(ctx, acctID, "ag_run"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeatState(ctx, "ag_run", "online", "", "", nil); err != nil {
		t.Fatal(err)
	}
	// 直接补 account_ref（CreateSeat 后绑定的列不同步——席位列是快照）
	if _, err := s.pool.Exec(ctx, htx(), `UPDATE g4_hosted_seat SET account_ref=$1 WHERE agent_id='ag_run'`, acctID); err != nil {
		t.Fatal(err)
	}
	apiAcct, err := o.pool.Register(ctx, "admin", true, "team-deepseek", "api_key", "codex", "gpt", "sk-1234567890abcd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, htx(), `UPDATE g4_hosted_seat SET account_ref=$1 WHERE agent_id='ag_dis'`, apiAcct.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeState(ctx, "nd_full", "offline"); err != nil {
		t.Fatal(err)
	}

	ov, err := o.HostedOverlay(ctx, []string{"ag_run", "ag_dis", "ag_busy", "ag_off", "ag_err", "ag_dead", "ag_fresh", "ag_ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ov) != 6 { // destroyed 与未落席位缺席
		t.Fatalf("缺席语义: %v", ov)
	}
	run := ov["ag_run"]
	if run["state"] != "running" || run["upgradable"] != true || run["node_name"] != "free" ||
		run["plan"] != "2c4g" || run["image_version"] != "old-ref" {
		t.Fatalf("running 格: %v", run)
	}
	// token_file 账号 ⇒ 类别＋登记时间，无 account_last4
	if run["account_kind"] != "token_file" || run["account_registered_at"] == "" {
		t.Fatalf("token_file 显示格: %v", run)
	}
	if _, has := run["account_last4"]; has {
		t.Fatalf("token_file 不得出后四位: %v", run)
	}
	dis := ov["ag_dis"]
	if dis["state"] != "disabled" || dis["upgradable"] != false {
		t.Fatalf("disabled 格: %v", dis)
	}
	if dis["account_last4"] != "abcd" { // api_key ⇒ 后四位
		t.Fatalf("api_key 显示格: %v", dis)
	}
	if ov["ag_busy"]["state"] != "busy" || ov["ag_off"]["state"] != "abnormal" || ov["ag_err"]["state"] != "abnormal" {
		t.Fatalf("busy/offline/error 格: %v", ov)
	}
	if ov["ag_fresh"]["state"] != "running" || ov["ag_fresh"]["upgradable"] != false {
		t.Fatalf("已最新格: %v", ov["ag_fresh"])
	}
	// G3 注入点形状：缺席/错误收敛 false
	if m, ok := o.HostedItem(ctx, "ag_ghost"); ok || m != nil {
		t.Fatalf("未落席位应 false: %v %v", m, ok)
	}
	if m, ok := o.HostedItem(ctx, "ag_dead"); ok || m != nil {
		t.Fatalf("已销毁应 false: %v %v", m, ok)
	}
	if m, ok := o.HostedItem(ctx, "ag_run"); !ok || m["state"] != "running" {
		t.Fatalf("HostedItem 正常格: %v %v", m, ok)
	}
}

// T-G4-32 反向：Hub 重启丢了内存暂存令牌（直接对已有席位 RunJob、未经 Create 暂存）⇒
// 启动阶段 credential_inject_failed 失败清理：Retire＋解绑＋状态 error，不硬撑不带凭据起容器。
func TestRunJobMissingTokenFailsClosed(t *testing.T) {
	o, idp, gw, s := mkOrch(t)
	ctx := context.Background()
	acctID := acctOf(t, o.pool)
	out, err := o.Create(ctx, CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "w1", GroupCode: "",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctID, Plan: "2c4g",
	})
	if err != nil {
		t.Fatal(err)
	}
	o.dropToken(out.AgentID) // 模拟 Hub 重启丢失暂存
	o.RunJob(ctx, out.AgentID)
	seat, err := s.GetSeat(ctx, out.AgentID)
	if err != nil || seat.State != "error" || seat.StateDetail != "credential_inject_failed" {
		t.Fatalf("应 credential_inject_failed: %+v err=%v", seat, err)
	}
	if len(idp.retired) != 1 || idp.retired[0] != out.AgentID {
		t.Fatalf("必须 Retire 释放名字: %v", idp.retired)
	}
	// 未起容器：只发了 pull_image，create_seat 未发
	for _, c := range gw.sent {
		if c.Type == "create_seat" {
			t.Fatalf("无令牌不得发 create_seat: %+v", gw.sent)
		}
	}
	// 账号已解绑（回池可再绑）
	a, err := s.GetAccount(ctx, acctID)
	if err != nil || a.State != "available" || a.BoundAgentID.Valid {
		t.Fatalf("账号应回池: %+v err=%v", a, err)
	}
}

// T-G4-35：options 读模型＋jobs 进度（契约第一节字段逐字）。
func TestOptionsViewAndJobProgress(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	// 容量读数（模拟心跳上报）：nd_free 16C 总、15G 余、100G 盘余
	if err := s.SetNodeOnline(ctx, "nd_free", json.RawMessage(`{"cpu_total":16,"mem_free_mb":15360,"disk_free_mb":102400}`)); err != nil {
		t.Fatal(err)
	}
	// 先建一席占资源：2c4g 落 nd_free
	acctID := acctOf(t, o.pool)
	out, err := o.Create(ctx, CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "w1", GroupCode: "",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctID, Plan: "2c4g",
	})
	if err != nil {
		t.Fatal(err)
	}

	v, err := o.Options(ctx, "zzc")
	if err != nil {
		t.Fatal(err)
	}
	// 节点两枚：free_vcpu＝16−2＝14（一席占 2C）、online 两态
	var free *NodeOption
	for i := range v.Nodes {
		if v.Nodes[i].ID == "nd_free" {
			free = &v.Nodes[i]
		}
	}
	if free == nil || !free.Online || free.FreeVCPU != 14 || free.FreeMemGB != 15 || free.FreeDiskGB != 100 {
		t.Fatalf("节点格: %+v", free)
	}
	// 账号：唯一一枚已绑定 ⇒ 下拉开不出（一席一账号）
	if len(v.Accounts) != 0 {
		t.Fatalf("已绑账号不得再出现: %+v", v.Accounts)
	}
	// 配额剩余：agents 2−1＝1、vcpu 4−2＝2、mem 8−4＝4G（mkOrch 配额 2/4/8192）
	if r := v.Quota.Remaining; r.Agents != 1 || r.VCPU != 2 || r.MemGB != 4 {
		t.Fatalf("剩余额度: %+v", v.Quota.Remaining)
	}
	// fits：2c4g 还剩 1 席 2C/4G ⇒ 可；4c8g 超剩余 ⇒ 置灰
	var fits map[string]bool = map[string]bool{}
	for _, p := range v.Plans {
		fits[p.ID] = p.Fits
		if p.ID == "2c4g" && (p.Name != "2C/4G" || p.VCPU != 2 || p.MemGB != 4) {
			t.Fatalf("套餐格: %+v", p)
		}
	}
	if !fits["2c4g"] || fits["4c8g"] {
		t.Fatalf("fits: %v", fits)
	}
	// combos：验通表原样下发
	if len(v.Combos) != 1 || !v.Combos[0].Verified || v.Combos[0].CLI != "codex" {
		t.Fatalf("combos: %+v", v.Combos)
	}

	// jobs 进度：queued→pulling→starting→online→failed 逐格（状态机真实读面，不写死）
	jv, err := o.JobProgress(ctx, out.AgentID)
	if err != nil || jv.Stage != "queued" || jv.AgentID != "" {
		t.Fatalf("queued 格: %+v %v", jv, err)
	}
	o.RunJob(ctx, out.AgentID) // 假网关全成 ⇒ starting（Agent 未连入）
	if jv, _ = o.JobProgress(ctx, out.AgentID); jv.Stage != "starting" || jv.AgentID != "" {
		t.Fatalf("starting 格: %+v", jv)
	}
	_ = o.MarkOnline(ctx, out.AgentID) // Agent 连入 ⇒ online
	jv, _ = o.JobProgress(ctx, out.AgentID)
	if jv.Stage != "online" || jv.AgentID != out.AgentID {
		t.Fatalf("online 格: %+v", jv)
	}
	// failed 格：另一席制造失败
	acct2, err := o.pool.Register(ctx, "admin", true, "team-codex-2", "token_file", "codex", "gpt", "sk-y")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.pool.Grant(ctx, "admin", true, acct2.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	out2, err := o.Create(ctx, CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "w2", GroupCode: "",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acct2.ID, Plan: "2c4g",
	})
	if err != nil { // 配额 agents=2 已满（w1 online 占一席）
		t.Fatalf("第二席在额度内: %v", err)
	}
	o.dropToken(out2.AgentID) // 丢令牌 ⇒ credential_inject_failed
	o.RunJob(ctx, out2.AgentID)
	jv, _ = o.JobProgress(ctx, out2.AgentID)
	if jv.Stage != "failed" || jv.Reason != "credential_inject_failed" || jv.AgentID != "" {
		t.Fatalf("failed 格: %+v", jv)
	}
	// 无 cancelled：合同无此态（排队不可取消）——JobView 结构无该值即判据
}

// 组长 18:56:12Z：HostedItem 收敛 false 必须留痕——计数＋1 且返回 false（日志同路径）。
func TestHostedItemErrorCounted(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	if err := s.CreateSeat(ctx, Seat{AgentID: "ag_x", NodeID: "nd_free", CLI: "codex", LLM: "gpt",
		CredKind: "token_file", Plan: "2c4g", ImageRef: "r", CreatedBy: "zzc"}); err != nil {
		t.Fatal(err)
	}
	before := HostedItemErrorCount()
	if err := s.DropSchema(ctx); err != nil { // 库面破坏 ⇒ GetSeat 必错
		t.Fatal(err)
	}
	if m, ok := o.HostedItem(ctx, "ag_x"); ok || m != nil {
		t.Fatalf("库坏应降级 false: %v %v", m, ok)
	}
	if HostedItemErrorCount() != before+1 {
		t.Fatalf("错误应计数: before=%d after=%d", before, HostedItemErrorCount())
	}
}
