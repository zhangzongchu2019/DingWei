package hosted

import (
	"context"
	"errors"
	"testing"
)

// R-G4-DISK-TIER（57d9a01a3ad6）：磁盘档位定 2c4g=8192MB、4c8g=16384MB。
// 反向：把任一档 DiskMB 改回 0（去掉赋值），本格即红。

func TestPlanDiskTiersPinned(t *testing.T) {
	for id, want := range map[string]int64{"2c4g": 8192, "4c8g": 16384} {
		p, ok := PlanByID(id)
		if !ok {
			t.Fatalf("套餐 %s 不在", id)
		}
		if p.DiskMB != want {
			t.Errorf("%s DiskMB=%d, want %d（档位不得为 0、不得漂移）", id, p.DiskMB, want)
		}
	}
}

// 磁盘闸在 CheckCreate 独立起作用：vCPU／内存／Agent 数都宽裕，只有磁盘不够 4c8g 档 ⇒ plan_exceeds_quota，
// 剩余 JSON 的 disk_gb 随档位显形（8G 剩 8G，装不下 16G 档）。
func TestCheckCreateDiskGate(t *testing.T) {
	ctx := context.Background()
	big, _ := PlanByID("4c8g")
	qs, _, _ := mkQuotaSvc(t)
	if err := qs.Put(ctx, "admin", true, Quota{UserLC: "zzc", AgentsLimit: 3, VCPULimit: 64, MemMBLimit: 1 << 20, DiskMBLimit: 8192}); err != nil {
		t.Fatal(err)
	}
	err := qs.CheckCreate(ctx, "zzc", big)
	if !errors.Is(err, ErrPlanExceedsQuota) {
		t.Fatalf("disk short: %v, want plan_exceeds_quota", err)
	}
	var pe *PlanExceedsQuotaError
	if !errors.As(err, &pe) {
		t.Fatalf("错误类型: %T", err)
	}
	if got := string(pe.RemainingJSON()); got != `{"agents":3,"disk_gb":8,"mem_gb":1024,"vcpu":64}` {
		t.Fatalf("remaining: %s", got)
	}
}
