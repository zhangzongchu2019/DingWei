package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// ---------- 注入假件 ----------

type fakeIdentity struct {
	issued      []string
	retired     []string
	agentIDs    []string
	issueErr    error
	nextID      int
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
	sent    []NodeCommand
	failOn  string // 命中该 Type 即返回失败（反向）
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
	// 执行链：pull→create→online
	o.RunJob(context.Background(), out.AgentID)
	seat, _ = s.GetSeat(context.Background(), out.AgentID)
	if seat.State != "online" {
		t.Fatalf("执行链后应 online: %+v", seat)
	}
	if len(gw.sent) != 2 || gw.sent[0].Type != "pull_image" || gw.sent[1].Type != "create_seat" {
		t.Fatalf("指令序列: %+v", gw.sent)
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
	if _, err := o.Create(context.Background(), r4); err != ErrQuotaExceeded {
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
