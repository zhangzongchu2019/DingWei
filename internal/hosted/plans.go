package hosted

// 资源套餐（组长裁定 da622016：平台管理侧预设，本版两档）。
// 磁盘档位已定（R-G4-DISK-TIER 57d9a01a3ad6：2c4g=8192MB、4c8g=16384MB；业主可调）。
// 定档前 DiskMB=0 的口径（只预记账、不设硬限）作废；旧席位按 0 记的差额见交件说明。

type Plan struct {
	ID     string
	VCPU   int
	MemMB  int64
	DiskMB int64
}

var plans = map[string]Plan{
	"2c4g": {ID: "2c4g", VCPU: 2, MemMB: 4096, DiskMB: 8192},
	"4c8g": {ID: "4c8g", VCPU: 4, MemMB: 8192, DiskMB: 16384},
}

// PlanByID 取套餐；未知 id 返回 ok=false（调用方判 plan_exceeds_quota/name_invalid 层）。
func PlanByID(id string) (Plan, bool) { p, ok := plans[id]; return p, ok }

// PlanIDs 表单数据源用（顺序固定，便于契约测试）。
func PlanIDs() []string { return []string{"2c4g", "4c8g"} }
