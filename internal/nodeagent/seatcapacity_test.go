package nodeagent

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

func TestSeatCapacityMinimumConfiguration(t *testing.T) {
	p, err := ParseNodePolicy(func(string) string { return "" })
	if err != nil || p.Mode != "hard" || p.MinimumSeatCPUMillicores != 1000 || p.MinimumSeatMemoryBytes != 1536<<20 || p.MinimumSeatDiskBytes != 40<<30 {
		t.Fatalf("defaults %+v: %v", p, err)
	}
	if p.MaxSeats != 4 {
		t.Fatal("legacy hard default changed")
	}
	soft := map[string]string{"NODEAGENT_QUOTA_MODE": "soft", "NODEAGENT_SOFT_APPROVAL_REF": "fixture", "NODEAGENT_HOST_ROOT_TRUST_REF": "fixture", "NODEAGENT_NETWORK_APPROVAL_REF": "fixture", "NODEAGENT_ENGINE_ID": "fixture", "NODEAGENT_ENGINE_ROOT": "/isolated-fixture"}
	for _, limit := range []string{"", "5"} {
		soft["NODEAGENT_MAX_SEATS"] = limit
		configured, err := ParseNodePolicy(func(k string) string { return soft[k] })
		want := 100
		if limit != "" {
			want = 5
		}
		if err != nil || configured.MaxSeats != want {
			t.Fatalf("soft default/operator cap: %+v %v", configured, err)
		}
	}
	for key, value := range map[string]string{"CPU_MILLICORES": "2000", "MEMORY_BYTES": "2147483648", "DISK_BYTES": "85899345920"} {
		name := "NODEAGENT_MINIMUM_SEAT_" + key
		changed, err := ParseNodePolicy(func(k string) string {
			if k == name {
				return value
			}
			return ""
		})
		if err != nil || changed.Revision() == p.Revision() {
			t.Fatalf("%s not bound to policy revision: %+v %v", name, changed, err)
		}
		for _, bad := range []string{"0", "-1", "1.5", "9223372036854775808"} {
			if _, err := ParseNodePolicy(func(k string) string {
				if k == name {
					return bad
				}
				return ""
			}); err == nil || strings.Contains(err.Error(), bad) {
				t.Fatalf("%s invalid value did not fail closed with bounded error: %v", name, err)
			}
		}
	}
}

func installCapacityFixture(q *softQuota) {
	q.policy.MaxSeats = 100
	q.capacity = &seatCapacityState{snapshot: seatcapacity.Snapshot{Known: true, At: q.now(), Total: seatcapacity.Resources{CPU: 8000, Memory: 16 << 30, Disk: 320 << 30}}}
}

func TestSoftCapacityRealReserveEightNinthAndRestart(t *testing.T) {
	q, _ := softFixture(t)
	installCapacityFixture(q)
	r := seatResources{MemoryMB: 1536, CPUShares: 1024, CPULimitMillicores: 1000, DiskMB: 40960}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("ag_capacity%d", i)
		if err := q.reserve(id, r); err != nil {
			t.Fatalf("reserve %d: %v", i+1, err)
		}
		if err := os.Mkdir(seatDir(q.root, id), 0700); err != nil {
			t.Fatal(err)
		}
		q.sampleNode()
	}
	if err := q.reserve("ag_ninth", r); err == nil || !strings.Contains(err.Error(), "seat_capacity_cpu+disk") {
		t.Fatalf("ninth: %v", err)
	}
	if len(q.state.Seats) != 8 {
		t.Fatal("rejected reservation persisted")
	}
	starts := 0
	if err := q.start(context.Background(), "ag_capacity0", func(context.Context) error { starts++; return nil }); err != nil || starts != 1 {
		t.Fatalf("restart charged twice: %v/%d", err, starts)
	}
	v := q.capacityReport()
	if !v.Known || v.TotalSlots != 8 || v.Occupied != 8 || v.AvailableSlots != 0 {
		t.Fatalf("report: %+v", v)
	}
	// Shrinking B capacity blocks another start but never creates holds/stops.
	q.capacity.snapshot.External.CPU = 1
	if err := q.start(context.Background(), "ag_capacity0", func(context.Context) error { starts++; return nil }); err == nil || starts != 1 {
		t.Fatalf("lost capacity allowed start: %v/%d", err, starts)
	}
	for _, s := range q.state.Seats {
		if s.Hold != "" || s.StopState != "" {
			t.Fatal("B changed running state")
		}
	}
}

func TestSoftCapacityUnknownStaleSlowAndEachDimension(t *testing.T) {
	q, _ := softFixture(t)
	installCapacityFixture(q)
	r := seatResources{MemoryMB: 1536, CPUShares: 1024, CPULimitMillicores: 1000, DiskMB: 40960}
	base := q.capacity.snapshot
	for _, tc := range []struct {
		name   string
		change func()
	}{
		{"unknown", func() { q.capacity.snapshot.Known = false }},
		{"stale", func() { q.capacity.snapshot.At = q.now().Add(-q.policy.MaxAge - time.Nanosecond) }},
		{"slow", func() {
			q.capacity.sampling = true
			q.capacity.started = q.now().Add(-q.policy.ScanBudget - time.Nanosecond)
		}},
		{"cpu", func() { q.capacity.snapshot.External.CPU = 7001 }},
		{"memory", func() { q.capacity.snapshot.Reserved.Memory = 16<<30 - (1536 << 20) + 1 }},
		{"disk", func() { q.capacity.snapshot.External.Disk = 280<<30 + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q.capacity.snapshot = base
			q.capacity.sampling = false
			tc.change()
			if err := q.reserve("ag_refused", r); err == nil {
				t.Fatal("gate bypassed")
			}
			if len(q.state.Seats) != 0 {
				t.Fatal("failed reserve changed durable state")
			}
		})
	}
	q.capacity.snapshot = base
	q.capacity.sampling = false
	if err := q.reserve("ag_restored", r); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityCPUParsingAndPhysicalHardlinkUnion(t *testing.T) {
	a, err := parseCapacityCPU([]byte("cpu 100 20 30 400 50 10 20 0 90 0\ncpu0 0"))
	if err != nil || a.total != 630 || a.busy != 180 {
		t.Fatalf("CPU %+v %v", a, err)
	}
	for _, bad := range []string{"cpu 1 2", "cpu -1 0 0 0 0 0 0 0", "cpu 18446744073709551615 1 0 0 0 0 0 0"} {
		if _, err := parseCapacityCPU([]byte(bad)); err == nil {
			t.Fatal("bad CPU accepted")
		}
	}
	root := t.TempDir()
	for _, id := range []string{"ag_a", "ag_b"} {
		if err := os.Mkdir(seatDir(root, id), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := seatDir(root, "ag_a") + "/data"
	if err := os.WriteFile(path, make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, seatDir(root, "ag_b")+"/data"); err != nil {
		t.Fatal(err)
	}
	seen := map[[2]uint64]bool{}
	unionA, err := measureUsageSeen(context.Background(), root, "ag_a", seen)
	if err != nil {
		t.Fatal(err)
	}
	unionB, err := measureUsageSeen(context.Background(), root, "ag_b", seen)
	if err != nil {
		t.Fatal(err)
	}
	separate, err := measureUsage(context.Background(), root, "ag_b")
	if err != nil {
		t.Fatal(err)
	}
	if unionA.Allocated <= 0 || unionB.Allocated >= separate.Allocated || reflect.DeepEqual(unionB, separate) {
		t.Fatalf("physical links double-counted: %+v %+v", unionB, separate)
	}
}
