package nodeagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemInfoMB(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "meminfo")
	os.WriteFile(p, []byte("MemTotal:       16384000 kB\nMemFree:         1024000 kB\nMemAvailable:    8192000 kB\n"), 0o644)
	total, avail, err := memInfoMB(p)
	if err != nil || total != 16000 || avail != 8000 {
		t.Fatalf("meminfo 解析: total=%d avail=%d err=%v", total, avail, err)
	}
}

func TestDiskMB(t *testing.T) {
	total, free, err := diskMB(t.TempDir())
	if err != nil || total <= 0 || free <= 0 || free > total {
		t.Fatalf("statfs: total=%d free=%d err=%v", total, free, err)
	}
}

func TestCollectCapacitySeatsAndImages(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	c, err := d.collectCapacity(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(c.Seats) != 1 || c.Seats[0].Name != "seat-a" {
		t.Fatalf("只应留 seat- 前缀且非 -new 的容器: %+v", c.Seats)
	}
	if c.ImageCount != 2 || c.ImageSizeMB != 150 {
		t.Fatalf("镜像缓存: count=%d size=%d", c.ImageCount, c.ImageSizeMB)
	}
}

func TestChangedBeyond(t *testing.T) {
	base := Capacity{MemFreeMB: 8192, DiskFreeMB: 102400, Seats: []seatInfo{{Name: "seat-a", State: "running"}}}
	if changedBeyond(base, base) {
		t.Fatal("无变化不应触发")
	}
	if !changedBeyond(base, Capacity{MemFreeMB: 8192 - 2048, DiskFreeMB: 102400, Seats: base.Seats}) {
		t.Fatal("内存余量变化超 1GiB 应触发")
	}
	if !changedBeyond(base, Capacity{MemFreeMB: 8192, DiskFreeMB: 102400, Seats: nil}) {
		t.Fatal("席位清单变化应触发")
	}
	if changedBeyond(base, Capacity{MemFreeMB: 8192 - 512, DiskFreeMB: 102400, Seats: base.Seats}) {
		t.Fatal("小变化不应触发")
	}
}

// TestHeartbeatSendsOnConnectAndInterval 对账即发＋周期心跳。
func TestHeartbeatSendsOnConnectAndInterval(t *testing.T) {
	secret := "node-secret-t1"
	hub := newStubHub(t, map[string]string{"nd_t1": secret})
	defer hub.close()

	cfg := mkConfig(t, hub.url(), secret)
	cfg.HubCertSHA256 = hub.certSHA256()
	ag, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var calls int32
	ag.capacity = func(ctx context.Context) (Capacity, error) {
		calls++
		return Capacity{CPUTotal: 4, MemTotalMB: 16384, MemFreeMB: 8192}, nil
	}
	ag.SetHeartbeatInterval(20 * time.Second) // 周期>测试时长：首轮后只能靠对账帧

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ag.Run(ctx)

	// 对账帧：连上即到（type=capacity_report）
	select {
	case raw := <-hub.frames():
		var f capacityFrame
		if json.Unmarshal(raw, &f) != nil || f.Type != "capacity_report" {
			t.Fatalf("首帧应为 capacity_report: %s", raw)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("3 秒内未收到对账容量帧")
	}
}

// TestReportCapacityHandler Hub 主动查：report_capacity 指令返回真采集。
func TestReportCapacityHandler(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	out, err := d.reportCapacity(context.Background(), nil)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	var c Capacity
	if json.Unmarshal(out, &c) != nil || c.CPUTotal <= 0 || c.DiskTotalMB <= 0 {
		t.Fatalf("采集内容: %s", out)
	}
}

