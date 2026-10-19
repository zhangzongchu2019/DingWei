package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

func TestMinimumPlanDefinitionAndNetCapacity(t *testing.T) {
	p, ok := PlanByID("1c1.5g40")
	if !ok || p != (Plan{ID: "1c1.5g40", VCPU: 1, MemMB: 1536, DiskMB: 40960}) {
		t.Fatalf("minimum plan: %+v %v", p, ok)
	}
	for id, want := range map[string]Plan{
		"2c4g": {ID: "2c4g", VCPU: 2, MemMB: 4096, DiskMB: 8192},
		"4c8g": {ID: "4c8g", VCPU: 4, MemMB: 8192, DiskMB: 16384},
	} {
		if got, ok := PlanByID(id); !ok || got != want {
			t.Fatalf("existing entitlement changed: %+v", got)
		}
	}
	// This binds the actual plan to B's calculator, not a duplicate resource
	// literal. Admission-path coverage is added with B's later integration.
	r := seatcapacity.Resources{CPU: int64(p.VCPU) * 1000, Memory: p.MemMB << 20, Disk: p.DiskMB << 20}
	policy := seatcapacity.DefaultPolicy()
	now := time.Unix(100, 0)
	snapshot := seatcapacity.Snapshot{Known: true, At: now, Total: seatcapacity.Resources{CPU: 8000, Memory: 16 << 30, Disk: 320 << 30}}
	var managed []seatcapacity.Resources
	for i := 0; i < 8; i++ {
		v := seatcapacity.Evaluate(policy, snapshot, managed, now, time.Minute)
		if reasons := v.CanAdd(policy, r); len(reasons) != 0 {
			t.Fatalf("seat %d refused: %v", i+1, reasons)
		}
		managed = append(managed, r)
	}
	v := seatcapacity.Evaluate(policy, snapshot, managed, now, time.Minute)
	if got := v.CanAdd(policy, r); !reflect.DeepEqual(got, []string{"cpu", "disk"}) || v.TotalSlots != 8 || v.AvailableSlots != 0 {
		t.Fatalf("ninth minimum plan: %+v reasons=%v", v, got)
	}
}

func TestFrozenR30MinimumPlanSchemaUpgradesOldConstraint(t *testing.T) {
	ctx := context.Background()
	_, s := mkPoolSvc(t)
	if err := s.upsertR31LegacyNode(ctx, Node{ID: "nd_min", Name: "min", Labels: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	// Reproduce the deployed constraint, retaining real old seats and quotas.
	if _, err := s.exec(ctx, `ALTER TABLE g4_hosted_seat DROP CONSTRAINT g4_hosted_seat_plan_check;
ALTER TABLE g4_hosted_seat ADD CONSTRAINT g4_hosted_seat_plan_check CHECK (plan IN ('2c4g','4c8g'));`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"2c4g", "4c8g"} {
		if err := s.CreateSeat(ctx, Seat{AgentID: "ag_" + id, NodeID: "nd_min", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: id, CreatedBy: "zzc"}); err != nil {
			t.Fatal(err)
		}
	}
	quota := Quota{UserLC: "zzc", AgentsLimit: 7, VCPULimit: 12, MemMBLimit: 20000, DiskMBLimit: 70000}
	if err := s.PutQuota(ctx, quota, "admin"); err != nil {
		t.Fatal(err)
	}
	minimum := Seat{AgentID: "ag_min", NodeID: "nd_min", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "1c1.5g40", CreatedBy: "zzc"}
	if err := s.CreateSeat(ctx, minimum); err == nil {
		t.Fatal("old CHECK unexpectedly accepts new plan")
	}
	for i := 0; i < 2; i++ {
		if err := func() error { _, err := s.exec(ctx, R30SchemaDDL); return err }(); err != nil {
			t.Fatalf("upgrade %d: %v", i, err)
		}
	}
	if err := s.CreateSeat(ctx, minimum); err != nil {
		t.Fatalf("new plan rejected after upgrade: %v", err)
	}
	for _, id := range []string{"2c4g", "4c8g"} {
		got, err := s.GetSeat(ctx, "ag_"+id)
		if err != nil || got.Plan != id || got.State != "queued" || got.CreatedBy != "zzc" {
			t.Fatalf("old seat changed: %+v %v", got, err)
		}
	}
	got, err := s.GetQuota(ctx, "zzc")
	if err != nil || got.AgentsLimit != quota.AgentsLimit || got.VCPULimit != quota.VCPULimit || got.MemMBLimit != quota.MemMBLimit || got.DiskMBLimit != quota.DiskMBLimit {
		t.Fatalf("quota rewritten: %+v %v", got, err)
	}
	minimum.AgentID, minimum.Plan = "ag_invalid", "unlisted"
	if err := s.CreateSeat(ctx, minimum); err == nil {
		t.Fatal("unknown plan passed database validation")
	}
}

func TestMinimumPlanQuotaDebitsAndEveryDimension(t *testing.T) {
	ctx := context.Background()
	qs, s, idp := mkQuotaSvc(t)
	plan, ok := PlanByID("1c1.5g40")
	if !ok {
		t.Fatal("minimum plan missing")
	}
	if err := s.upsertR31LegacyNode(ctx, Node{ID: "nd_min", Name: "min", Labels: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []Quota{
		{UserLC: "zzc", AgentsLimit: 1, VCPULimit: 1, MemMBLimit: 1536, DiskMBLimit: 40960},
		{UserLC: "zzc", AgentsLimit: 1, VCPULimit: 0, MemMBLimit: 1536, DiskMBLimit: 40960},
		{UserLC: "zzc", AgentsLimit: 1, VCPULimit: 1, MemMBLimit: 1535, DiskMBLimit: 40960},
		{UserLC: "zzc", AgentsLimit: 1, VCPULimit: 1, MemMBLimit: 1536, DiskMBLimit: 40959},
	} {
		if err := qs.Put(ctx, "admin", true, limit); err != nil {
			t.Fatal(err)
		}
		err := qs.CheckCreate(ctx, "zzc", plan)
		fits := limit.VCPULimit == 1 && limit.MemMBLimit == 1536 && limit.DiskMBLimit == 40960
		if (fits && err != nil) || (!fits && !errors.Is(err, ErrPlanExceedsQuota)) {
			t.Fatalf("quota %+v: %v", limit, err)
		}
	}
	for i, state := range []string{"online", "disabled", "destroyed"} {
		id := fmt.Sprintf("ag_min%d", i)
		if err := s.CreateSeat(ctx, Seat{AgentID: id, NodeID: "nd_min", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: plan.ID, CreatedBy: "zzc"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSeatState(ctx, id, state, "", "fixture", nil); err != nil {
			t.Fatal(err)
		}
		idp.agentIDs = append(idp.agentIDs, id)
	}
	usage, err := s.UsageFor(ctx, idp.agentIDs)
	if err != nil || usage.Agents != 2 || usage.VCPU != 2 || usage.MemMB != 3072 || usage.DiskMB != 81920 {
		t.Fatalf("minimum plan debit (disabled counts; destroyed does not): %+v %v", usage, err)
	}
}

func TestMinimumPlanOptionsAndRealCreateResources(t *testing.T) {
	ctx := context.Background()
	o, _, gw, s := mkOrch(t)
	if err := s.setR31LegacyOnline(ctx, "nd_free", []byte(`{"resource_limits_version":1,"mem_free_mb":16384}`)); err != nil {
		t.Fatal(err)
	}
	v, err := o.Options(ctx, "zzc")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Plans) != 3 || v.Plans[0].ID != "1c1.5g40" || v.Plans[0].Name != "1C/1.5G/40G" || v.Plans[0].MemGB != 1.5 || !v.Plans[0].Fits {
		t.Fatalf("options: %+v", v.Plans)
	}
	raw, _ := json.Marshal(v.Plans[0])
	if !strings.Contains(string(raw), `"mem_gb":1.5`) {
		t.Fatalf("fractional memory lost in JSON: %s", raw)
	}
	// Options and actual creation must agree on the new 40-GiB entitlement.
	quota, err := s.GetQuota(ctx, "zzc")
	if err != nil {
		t.Fatal(err)
	}
	short := quota
	short.DiskMBLimit = 40959
	if err := s.PutQuota(ctx, short, "admin"); err != nil {
		t.Fatal(err)
	}
	v, err = o.Options(ctx, "zzc")
	if err != nil || len(v.Plans) == 0 || v.Plans[0].Fits {
		t.Fatalf("short disk quota remains selectable: %+v %v", v.Plans, err)
	}
	if err := s.PutQuota(ctx, quota, "admin"); err != nil {
		t.Fatal(err)
	}
	out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "minimum", NodeID: "nd_free", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "1c1.5g40"})
	if err != nil {
		t.Fatalf("minimum create: %v", err)
	}
	o.RunJob(ctx, out.AgentID)
	var resources struct {
		Resources struct {
			Memory int64 `json:"memory_mb"`
			CPU    int64 `json:"cpu_limit_millicores"`
			Shares int64 `json:"cpu_shares"`
			Disk   int64 `json:"disk_mb"`
		} `json:"resources"`
	}
	if len(gw.sent) != 2 || gw.sent[1].Type != "create_seat" || json.Unmarshal(gw.sent[1].Params, &resources) != nil {
		t.Fatalf("actual create chain: %+v", gw.sent)
	}
	r := resources.Resources
	if r.Memory != 1536 || r.CPU != 1000 || r.Shares != 1024 || r.Disk != 40960 {
		t.Fatalf("minimum runtime resources: %+v", r)
	}
	if _, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "invalidplan", NodeID: "nd_free", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "unlisted"}); !errors.Is(err, ErrPlanExceedsQuota) && !errors.Is(err, ErrParams) {
		t.Fatalf("unknown plan accepted: %v", err)
	}
}
