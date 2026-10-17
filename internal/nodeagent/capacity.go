package nodeagent

// T-G4-13 · 容量上报真采集：CPU／内存／磁盘（席位根所在挂载）／容器清单／镜像缓存。
// 三个触发面：周期心跳（默认 60s）· 变更触发（余量变化超阈值）· Hub 主动查（report_capacity 指令）；
// 重连对账：每次连上 Hub 立刻发一次（Hub 侧据此把 offline 节点翻回 online 并对账容器实况）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type Capacity struct {
	ResourceLimitsVersion int                 `json:"resource_limits_version,omitempty"`
	QuotaMode             string              `json:"quota_mode"`
	QuotaPolicyRevision   string              `json:"quota_policy_revision"`
	SoftReady             bool                `json:"soft_ready"`
	SoftSeats             map[string]softSeat `json:"soft_seats,omitempty"`
	QuotaEvents           []quotaEvent        `json:"quota_events,omitempty"`
	NodeID                string              `json:"node_id"`
	Labels                map[string]string   `json:"labels,omitempty"`
	At                    time.Time           `json:"at"`
	CPUTotal              int                 `json:"cpu_total"`
	MemTotalMB            int64               `json:"mem_total_mb"`
	MemFreeMB             int64               `json:"mem_free_mb"`
	DiskTotalMB           int64               `json:"disk_total_mb"`
	DiskFreeMB            int64               `json:"disk_free_mb"`
	Seats                 []seatInfo          `json:"seats"`
	Models                []string            `json:"models,omitempty"` // 包 B2：本节点可选模型清单（NODEAGENT_SEAT_CODEX_MODELS 上报；空＝不支持自选）
	ImageCount            int                 `json:"image_count"`
	ImageSizeMB           int64               `json:"image_size_mb"`
}

type seatInfo struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// collectCapacity 采集（meminfo 与 statfs 可注入——单测用 fixture）。
func (d *dockerOps) collectCapacity(ctx context.Context) (Capacity, error) {
	c := Capacity{NodeID: "", Labels: nil, At: time.Now().UTC()}
	c.CPUTotal = runtime.NumCPU()
	c.ResourceLimitsVersion = 1
	c.QuotaMode = d.policyValue().Mode
	c.QuotaPolicyRevision = d.policyValue().Revision()
	if d.soft != nil {
		c.SoftSeats, c.QuotaEvents, c.SoftReady = d.soft.report()
	}
	total, free, err := memInfoMB("/proc/meminfo")
	if err != nil {
		return c, err
	}
	c.MemTotalMB, c.MemFreeMB = total, free
	dt, df, err := diskMB(d.seatRoot)
	if err != nil {
		return c, err
	}
	c.DiskTotalMB, c.DiskFreeMB = dt, df
	c.Models = d.seatModels

	// 容器清单（seat- 前缀）
	var containers []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
		State string   `json:"State"`
	}
	if err := d.eng.call(ctx, "GET", "/containers/json?all=1", nil, &containers); err != nil {
		return c, err
	}
	for _, ct := range containers {
		for _, n := range ct.Names {
			name := strings.TrimPrefix(n, "/")
			if strings.HasPrefix(name, "seat-") && !strings.HasSuffix(name, "-new") {
				c.Seats = append(c.Seats, seatInfo{Name: name, State: ct.State})
			}
		}
	}
	// 镜像缓存
	var images []struct {
		Size int64 `json:"Size"`
	}
	if err := d.eng.call(ctx, "GET", "/images/json", nil, &images); err != nil {
		return c, err
	}
	c.ImageCount = len(images)
	for _, im := range images {
		c.ImageSizeMB += im.Size / 1024 / 1024
	}
	return c, nil
}

// memInfoMB 解析 /proc/meminfo 的 MemTotal／MemAvailable（MB）。
func memInfoMB(path string) (int64, int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var total, avail int64
	for _, ln := range strings.Split(string(raw), "\n") {
		var kb int64
		if strings.HasPrefix(ln, "MemTotal:") {
			fmt.Sscanf(ln, "MemTotal: %d kB", &kb)
			total = kb / 1024
		} else if strings.HasPrefix(ln, "MemAvailable:") {
			fmt.Sscanf(ln, "MemAvailable: %d kB", &kb)
			avail = kb / 1024
		}
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("meminfo 解析失败: %s", path)
	}
	return total, avail, nil
}

// diskMB 席位根所在文件系统的总量/余量（MB）。
func diskMB(path string) (int64, int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := int64(st.Bsize)
	return int64(st.Blocks) * bs / 1024 / 1024, int64(st.Bavail) * bs / 1024 / 1024, nil
}

// capacityReport 指令响应（Hub 主动查）。
func (d *dockerOps) reportCapacity(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
	c, err := d.collectCapacity(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// changedBeyond 变更触发阈值：内存或磁盘余量变化超过 1 GiB，或席位清单变化。
func changedBeyond(a, b Capacity) bool {
	if a.QuotaMode != b.QuotaMode || a.SoftReady != b.SoftReady || len(a.QuotaEvents) != len(b.QuotaEvents) {
		return true
	}
	if abs64(a.MemFreeMB-b.MemFreeMB) > 1024 || abs64(a.DiskFreeMB-b.DiskFreeMB) > 1024 {
		return true
	}
	if len(a.Seats) != len(b.Seats) {
		return true
	}
	seen := map[string]string{}
	for _, s := range a.Seats {
		seen[s.Name] = s.State
	}
	for _, s := range b.Seats {
		if st, ok := seen[s.Name]; !ok || st != s.State {
			return true
		}
	}
	return false
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
