package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func mkQuotaSvc(t *testing.T) (*QuotaService, *Store, *fakeIdentity) {
	t.Helper()
	_, s := mkPoolSvc(t)
	idp := &fakeIdentity{}
	return NewQuotaService(s, idp), s, idp
}

func TestQuotaCheckBoundaries(t *testing.T) {
	qs, s, idp := mkQuotaSvc(t)
	ctx := context.Background()
	plan, _ := PlanByID("2c4g")

	// 未设置＝默认 4 席 ⇒ 仍放行；超额响应必须带各维度剩余（先造满再验）
	if err := qs.CheckCreate(ctx, "zzc", plan); err != nil {
		t.Fatalf("默认 4 席内应放行: %v", err)
	}
	if err := qs.Put(ctx, "admin", true, Quota{UserLC: "zzc", AgentsLimit: 0, VCPULimit: 0, MemMBLimit: 0, DiskMBLimit: 0}); err != nil {
		t.Fatal(err)
	}
	err := qs.CheckCreate(ctx, "zzc", plan)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("零上限应拒: %v", err)
	}
	var qe *QuotaExceededError
	if errors.As(err, &qe) {
		if len(qe.RemainingJSON()) == 0 {
			t.Fatal("超额响应应带各维度剩余")
		}
	}

	// 设上限后放行
	if err := qs.Put(ctx, "admin", true, Quota{UserLC: "zzc", AgentsLimit: 2, VCPULimit: 4, MemMBLimit: 8192, DiskMBLimit: 102400}); err != nil {
		t.Fatal(err)
	}
	if err := qs.CheckCreate(ctx, "zzc", plan); err != nil {
		t.Fatalf("额度内应放行: %v", err)
	}

	// 造一席已用（含 disabled 计入）：idp 名下塞两个 agent，席位落库一 disabled
	idp.agentIDs = []string{"ag_u1"}
	if err := s.UpsertNode(ctx, Node{ID: "nd_q", Name: "q", Labels: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSeat(ctx, Seat{AgentID: "ag_u1", NodeID: "nd_q", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "2c4g", CreatedBy: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeatState(ctx, "ag_u1", "disabled", "", "disable", nil); err != nil {
		t.Fatal(err)
	}
	// 已用 1/2、2048/4096MB ⇒ 再建一席仍放行
	if err := qs.CheckCreate(ctx, "zzc", plan); err != nil {
		t.Fatalf("第二席应放行: %v", err)
	}
	// Agent 数满（2/2 形状：把已用做成两席）
	idp.agentIDs = []string{"ag_u1", "ag_u2"}
	if err := s.CreateSeat(ctx, Seat{AgentID: "ag_u2", NodeID: "nd_q", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "2c4g", CreatedBy: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := qs.CheckCreate(ctx, "zzc", plan); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("Agent 数满应拒（C7）: %v", err)
	}

	// G6：新上限低于已用量允许保存；此后新建被拒直到回落
	if err := qs.Put(ctx, "admin", true, Quota{UserLC: "zzc", AgentsLimit: 1, VCPULimit: 4, MemMBLimit: 8192}); err != nil {
		t.Fatalf("低于已用应允许保存（G6）: %v", err)
	}
	if err := qs.CheckCreate(ctx, "zzc", plan); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("低于已用后新建应拒: %v", err)
	}

	// 普通用户 Put 必拒（C6 反向）
	if err := qs.Put(ctx, "user1", false, Quota{UserLC: "zzc", AgentsLimit: 99}); err != ErrForbiddenRole {
		t.Fatalf("普通用户设配额应 ErrForbiddenRole, 得 %v", err)
	}
	// 负数上限拒
	if err := qs.Put(ctx, "admin", true, Quota{UserLC: "zzc", AgentsLimit: -1}); err == nil {
		t.Fatal("负上限应拒")
	}
}

func err0(ctx context.Context, qs *QuotaService, p Plan) error { return qs.CheckCreate(ctx, "zzc", p) }

func TestQuotaListWithUsage(t *testing.T) {
	qs, s, idp := mkQuotaSvc(t)
	ctx := context.Background()
	if err := qs.Put(ctx, "admin", true, Quota{UserLC: "zzc", AgentsLimit: 4, VCPULimit: 8, MemMBLimit: 16384}); err != nil {
		t.Fatal(err)
	}
	idp.agentIDs = []string{"ag_v1"}
	if err := s.UpsertNode(ctx, Node{ID: "nd_v", Name: "v", Labels: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSeat(ctx, Seat{AgentID: "ag_v1", NodeID: "nd_v", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "4c8g", CreatedBy: "t"}); err != nil {
		t.Fatal(err)
	}
	rows, err := qs.ListWithUsage(ctx, []string{"zzc"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v", err)
	}
	r := rows[0]
	if r.Usage.Agents != 1 || r.Usage.VCPU != 4 || r.Usage.MemMB != 8192 || r.Quota.AgentsLimit != 4 {
		t.Fatalf("已用/上限并排: %+v", r)
	}
}

// 组长裁定 lead-g4-joint-run-1 第 6 条：Agent 数剩 0 ⇒ quota_exceeded；Agent 数还有、套餐超 vCPU／内存剩余 ⇒ plan_exceeds_quota。
// 两种都带剩余；两者同时成立时答 quota_exceeded。
func TestCheckCreateCodeSplit(t *testing.T) {
	ctx := context.Background()
	big, _ := PlanByID("4c8g")
	for _, c := range []struct {
		name      string
		q         Quota
		want      error
		remaining string
	}{
		{"vcpu short", Quota{UserLC: "zzc", AgentsLimit: 3, VCPULimit: 3, MemMBLimit: 1 << 20, DiskMBLimit: 1 << 20}, ErrPlanExceedsQuota,
			`{"agents":3,"disk_gb":1024,"mem_gb":1024,"vcpu":3}`},
		{"memory short", Quota{UserLC: "zzc", AgentsLimit: 3, VCPULimit: 64, MemMBLimit: 6144, DiskMBLimit: 1 << 20}, ErrPlanExceedsQuota,
			`{"agents":3,"disk_gb":1024,"mem_gb":6,"vcpu":64}`},
		{"no agent slot", Quota{UserLC: "zzc", AgentsLimit: 0, VCPULimit: 64, MemMBLimit: 1 << 20, DiskMBLimit: 1 << 20}, ErrQuotaExceeded,
			`{"agents":0,"disk_gb":1024,"mem_gb":1024,"vcpu":64}`},
		{"both: agents wins", Quota{UserLC: "zzc", AgentsLimit: 0, VCPULimit: 1, MemMBLimit: 1, DiskMBLimit: 1 << 20}, ErrQuotaExceeded,
			`{"agents":0,"disk_gb":1024,"mem_gb":0,"vcpu":1}`},
	} {
		qs, _, _ := mkQuotaSvc(t)
		if err := qs.Put(ctx, "admin", true, c.q); err != nil {
			t.Fatal(err)
		}
		err := qs.CheckCreate(ctx, "zzc", big)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
			continue
		}
		if c.want == ErrQuotaExceeded && errors.Is(err, ErrPlanExceedsQuota) || c.want == ErrPlanExceedsQuota && errors.Is(err, ErrQuotaExceeded) {
			t.Errorf("%s: %v matches both codes", c.name, err)
		}
		var raw json.RawMessage
		var qe *QuotaExceededError
		var pe *PlanExceedsQuotaError
		switch {
		case errors.As(err, &qe):
			raw = qe.RemainingJSON()
		case errors.As(err, &pe):
			raw = pe.RemainingJSON()
		}
		if string(raw) != c.remaining {
			t.Errorf("%s: remaining %s, want %s", c.name, raw, c.remaining)
		}
	}
}
