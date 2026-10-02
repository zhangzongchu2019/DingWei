package hosted

// 资源套餐（组长裁定 da622016：平台管理侧预设，本版两档）。
// disk_mb 待 G1 配额档位确认（契约第五节待办 1）；配额记账在磁盘档未定前按 0 预记账——
// 与 V7-C8"实测前 Hub 只预记账、不声称磁盘硬限生效"同一条纪律。

type Plan struct {
	ID     string
	VCPU   int
	MemMB  int64
	DiskMB int64
}

var plans = map[string]Plan{
	"2c4g": {ID: "2c4g", VCPU: 2, MemMB: 4096, DiskMB: 0}, // DiskMB 待 G1 定档
	"4c8g": {ID: "4c8g", VCPU: 4, MemMB: 8192, DiskMB: 0},
}

// PlanByID 取套餐；未知 id 返回 ok=false（调用方判 plan_exceeds_quota/name_invalid 层）。
func PlanByID(id string) (Plan, bool) { p, ok := plans[id]; return p, ok }

// PlanIDs 表单数据源用（顺序固定，便于契约测试）。
func PlanIDs() []string { return []string{"2c4g", "4c8g"} }
