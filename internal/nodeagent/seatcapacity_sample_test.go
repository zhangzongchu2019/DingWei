package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

func TestCapacitySamplerForeignLoadOwnershipAndUnknown(t *testing.T) {
	q, p := softFixture(t)
	now := time.Now()
	q.now = func() time.Time { return now }
	if err := q.reserve("ag_sample", seatResources{MemoryMB: 1536, CPUShares: 1024, CPULimitMillicores: 1000, DiskMB: 40960}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(seatDir(q.root, "ag_sample"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seatDir(q.root, "ag_sample")+"/business", make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	if err := q.track("ag_sample", "cid"); err != nil {
		t.Fatal(err)
	}
	q.resources = func() (int64, int64, int64, error) { return 8000, 16 << 30, 12 << 30, nil }
	q.disk = func(string) (fsSpace, error) { return fsSpace{320 << 30, 312 << 30, 10000000, 10000000}, nil }
	var mu sync.Mutex
	mode := ""
	statsCount := uint64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if mode == "http_error" {
			w.WriteHeader(503)
			return
		}
		switch {
		case r.URL.Path == "/containers/cid/json":
			node := q.nodeID
			source := seatDir(q.root, "ag_sample")
			if mode == "wrong_owner" {
				node = "another-node"
			}
			if mode == "wrong_mount" {
				source = "/another/private/path"
			}
			json.NewEncoder(w).Encode(map[string]any{"Id": "cid", "Config": map[string]any{"Labels": map[string]string{managedSeatLabel: node, managedSeatIDLabel: "ag_sample"}}, "Mounts": []map[string]string{{"Source": source, "Destination": "/work"}}, "State": map[string]bool{"Running": true}})
		case r.URL.Path == "/containers/cid/stats":
			if r.URL.Query().Get("stream") != "false" || r.URL.Query().Get("one-shot") != "true" {
				t.Error("unbounded stats query")
			}
			statsCount++
			memory := map[string]uint64{"anon": 128 << 20}
			if mode == "missing_memory" {
				memory = map[string]uint64{}
			}
			json.NewEncoder(w).Encode(map[string]any{"read": now, "cpu_stats": map[string]any{"cpu_usage": map[string]uint64{"total_usage": statsCount * 100}, "system_cpu_usage": statsCount * 1000}, "memory_stats": map[string]any{"stats": memory}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	d := &dockerOps{eng: newFakeEngine(srv.Client(), srv.URL), soft: q, policy: p, seatRoot: q.root}
	r := newSeatCapacityReader(d)
	tick := uint64(0)
	r.readCPU = func() (cpuCounters, error) { tick++; return cpuCounters{total: tick * 100, busy: tick * 50}, nil }
	if _, err := r.sample(context.Background()); err == nil {
		t.Fatal("first CPU sample treated as zero")
	}
	if _, err := r.sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := r.sample(context.Background())
	if err != nil || !s.Known {
		t.Fatalf("sample: %+v %v", s, err)
	}
	u, err := measureUsage(context.Background(), q.root, "ag_sample")
	if err != nil {
		t.Fatal(err)
	}
	if s.External.CPU != 3200 || s.External.Memory != (4<<30)-(128<<20) || s.External.Disk != (8<<30)-u.Allocated || s.Reserved.CPU != 2000 || s.Reserved.Memory != 4<<30 || s.Reserved.Disk != 40<<30 {
		t.Fatalf("non-managed/reserve accounting: %+v", s)
	}
	for _, bad := range []string{"wrong_owner", "wrong_mount", "missing_memory", "http_error"} {
		mu.Lock()
		mode = ""
		mu.Unlock()
		for attempts := 0; ; attempts++ {
			_, err := r.sample(context.Background())
			if err == nil {
				break
			}
			if !errors.Is(err, errCapacityWarming) || attempts >= 2 {
				t.Fatalf("could not warm fixture: %v", err)
			}
		}
		mu.Lock()
		mode = bad
		mu.Unlock()
		if s, err := r.sample(context.Background()); err == nil || errors.Is(err, errCapacityWarming) || s.Known {
			t.Fatalf("%s became known zero: %+v %v", bad, s, err)
		}
	}
	if q.state.Seats["ag_sample"].Hold != "" {
		t.Fatal("B sample failure held running seat")
	}
	mu.Lock()
	mode = ""
	mu.Unlock()
	if _, err := r.sample(context.Background()); !errors.Is(err, errCapacityWarming) {
		t.Fatalf("failed sample reused old CPU window: %v", err)
	}
	if recovered, err := r.sample(context.Background()); err != nil || !recovered.Known || recovered.External.CPU != 4000 {
		t.Fatalf("fresh baseline recovery: %+v %v", recovered, err)
	}
}

func TestCapacityBlockedSamplerIsSingleAndNeverLocksReserveOrReport(t *testing.T) {
	q, p := softFixture(t)
	installCapacityFixture(q)
	r := seatResources{MemoryMB: 1536, CPUShares: 1024, DiskMB: 40960, CPULimitMillicores: 1000}
	if err := q.reserve("ag_live", r); err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	q.now = func() time.Time { return time.Unix(0, clock.Load()) }
	q.policy.ScanBudget = 10 * time.Millisecond
	q.policy.ReserveInterval = time.Millisecond
	q.capacity.snapshot.At = q.now()
	var calls atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	q.capacity.read = func(context.Context) (seatcapacity.Snapshot, error) {
		calls.Add(1)
		close(entered)
		<-release
		return seatcapacity.Snapshot{}, errors.New("blocked kernel fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	d := &dockerOps{soft: q, policy: p}
	go func() { d.runSeatCapacity(ctx); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("sampler not started")
	}
	clock.Add(int64(20 * time.Millisecond))
	finished := make(chan error, 1)
	go func() {
		v := q.capacityReport()
		if v.Known {
			finished <- errors.New("overbudget sampler still known")
			return
		}
		_, _, _ = q.report() // Old reserve/hold monitor remains independently callable.
		finished <- q.reserve("ag_blocked", r)
	}()
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "seat_capacity_load_unknown") {
			t.Fatalf("blocked sample admission: %v", err)
		}
	case <-time.After(time.Second):
		unblock()
		t.Fatal("sampler held state lock")
	}
	if calls.Load() != 1 || q.state.Seats["ag_live"].Hold != "" {
		t.Fatal("extra worker or unsolicited hold")
	}
	cancel()
	unblock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after IO returned")
	}
}

func TestCapacityLoginBudgetIsNotBusinessSeatOrFortyGiBFloor(t *testing.T) {
	q, _ := softFixture(t)
	installCapacityFixture(q)
	q.state.Seats["login-"+strings.Repeat("a", 32)] = &softSeat{WriterKind: "login", WriterRoot: t.TempDir(), Resources: seatResources{MemoryMB: 128, CPUShares: 256, CPULimitMillicores: 250, DiskMB: 128}}
	v := q.capacityReport()
	if v.Occupied != 0 || v.CPU.NodeReserve != 250 || v.Memory.NodeReserve != 128<<20 || v.Disk.NodeReserve != 128<<20 || v.TotalSlots != 7 {
		t.Fatalf("login charged as minimum business seat: %+v", v)
	}
	if q.state.Seats["login-"+strings.Repeat("a", 32)].Hold != "" {
		t.Fatal("B held login")
	}
	reported, _, _ := q.report()
	if reported["login-"+strings.Repeat("a", 32)].WriterRoot != "" {
		t.Fatal("private writer path leaked in capacity")
	}
}

func TestCapacityMemoryMissingIsUnknownNotZero(t *testing.T) {
	for _, raw := range []string{"MemTotal: 1024 kB\n", "MemTotal: 1024 kB\nMemAvailable: nope kB\n", "MemTotal: 1024 kB\nMemAvailable: 1025 kB\n", "MemTotal: 1024 kB\nMemAvailable: 1 kB\nMemAvailable: 2 kB\n"} {
		if _, _, err := parseCapacityMemory([]byte(raw)); err == nil {
			t.Fatalf("invalid memory accepted: %q", raw)
		}
	}
	for _, available := range []int64{0, 512} {
		total, free, err := parseCapacityMemory([]byte(fmt.Sprintf("MemTotal: 1024 kB\nMemAvailable: %d kB\n", available)))
		if err != nil || total != 1<<20 || free != available<<10 {
			t.Fatalf("valid zero/positive availability: %d %d %v", total, free, err)
		}
	}
}
