package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func softFixture(t *testing.T) (*softQuota, NodePolicy) {
	t.Helper()
	root := t.TempDir()
	private := t.TempDir()
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	p := DefaultNodePolicy()
	p.Mode = "soft"
	p.SoftApprovalRef = "fixture-risk"
	p.HostRootTrustRef = "fixture-trust"
	p.NetworkApprovalRef = "fixture-network"
	p.EngineID = "fixture-engine"
	p.EngineRoot = "/isolated-fixture"
	q, err := newSoftQuota(p, root, filepath.Join(private, "quota.json"), "nd_fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.close)
	q.resources = func() (int64, int64, int64, error) { return 64000, 1 << 40, 1 << 40, nil }
	q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1 << 40, 10000000, 10000000}, nil }
	q.sampleNode()
	q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{1024, 1024}, nil }
	return q, p
}
func reserveFixture(t *testing.T, q *softQuota, id string) {
	t.Helper()
	if err := q.reserve(id, seatResources{MemoryMB: 4096, CPUShares: 2048, DiskMB: 8}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(seatDir(q.root, id), 0700); err != nil {
		t.Fatal(err)
	}
	q.sampleNode()
}

func TestNodePolicyExplicitDefaultsAndInvalidValues(t *testing.T) {
	p, err := ParseNodePolicy(func(string) string { return "" })
	if err != nil || p.Mode != "hard" || p.ScanInterval != 30*time.Second || p.ScanBudget != 5*time.Second || p.PidsLimit != 512 {
		t.Fatalf("defaults %+v %v", p, err)
	}
	for _, kv := range [][2]string{{"QUOTA_MODE", "Soft"}, {"QUOTA_MODE", "auto"}, {"QUOTA_MODE", "soft"}, {"PIDS_LIMIT", "0"}, {"WARN_PERCENT", "100"}, {"RECOVERY_PERCENT", "81"}, {"MAX_AGE", "1s"}, {"SCAN_BUDGET", "31s"}, {"RESERVE_INODES", "0"}, {"RESERVE_PERCENT", "-1"}, {"MAX_SEATS", "0"}, {"LOG_MAX_FILES", "0"}, {"SCAN_INTERVAL", "invalid"}} {
		t.Run(kv[0]+kv[1], func(t *testing.T) {
			_, err := ParseNodePolicy(func(k string) string {
				if k == "NODEAGENT_"+kv[0] {
					return kv[1]
				}
				return ""
			})
			if err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	soft := map[string]string{"NODEAGENT_SOFT_APPROVAL_REF": "fixture", "NODEAGENT_HOST_ROOT_TRUST_REF": "fixture", "NODEAGENT_NETWORK_APPROVAL_REF": "fixture", "NODEAGENT_QUOTA_MODE": "soft", "NODEAGENT_ENGINE_ID": "fixture", "NODEAGENT_ENGINE_ROOT": "/isolated", "NODEAGENT_WARN_PERCENT": "85", "NODEAGENT_PIDS_LIMIT": "300"}
	p, err = ParseNodePolicy(func(k string) string { return soft[k] })
	if err != nil || p.Mode != "soft" || p.WarnPercent != 85 || p.PidsLimit != 300 || len(p.Revision()) != 64 {
		t.Fatalf("explicit %+v %v", p, err)
	}
	a := p.Revision()
	p.WarnPercent = 86
	if p.Revision() == a {
		t.Fatal("threshold changes must change registration revision")
	}
}

func TestDualUsageSparseHardlinksSymlinksAndCancellation(t *testing.T) {
	root := t.TempDir()
	id := "ag_usage"
	dir := seatDir(root, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	sparse, err := os.Create(filepath.Join(dir, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err = sparse.Truncate(32 << 20); err != nil {
		t.Fatal(err)
	}
	sparse.Close()
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), make([]byte, 1<<20), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	a, err := measureUsage(context.Background(), root, id)
	if err != nil || a.Apparent < 32<<20 || a.Apparent > (32<<20)+(64<<10) || a.Allocated >= a.Apparent {
		t.Fatalf("dual usage %+v %v", a, err)
	}
	if err := os.Link(filepath.Join(dir, "sparse"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	b, err := measureUsage(context.Background(), root, id)
	if err != nil || b.Apparent-a.Apparent > 4096 {
		t.Fatalf("hardlink charged twice %+v %+v %v", a, b, err)
	}
	dir2 := seatDir(root, "ag_other")
	os.Mkdir(dir2, 0700)
	if err := os.Link(filepath.Join(dir, "sparse"), filepath.Join(dir2, "alias")); err != nil {
		t.Fatal(err)
	}
	c, err := measureUsage(context.Background(), root, "ag_other")
	if err != nil || c.Apparent < 32<<20 {
		t.Fatalf("cross-seat alias not charged %+v %v", c, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = measureUsage(ctx, root, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}
	if err := os.Symlink(outside, seatDir(root, "ag_symlink")); err != nil {
		t.Fatal(err)
	}
	if _, err = measureUsage(context.Background(), root, "ag_symlink"); err == nil {
		t.Fatal("seat symlink followed")
	}
	before, _ := os.ReadDir("/proc/self/fd")
	for i := 0; i < 200; i++ {
		if _, err := measureUsage(context.Background(), root, id); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadDir("/proc/self/fd")
	if len(before) != len(after) {
		t.Fatalf("FD growth %d→%d", len(before), len(after))
	}
}

func TestSoftHoldStopsWithoutDeletionPersistsAndGatesEveryEntry(t *testing.T) {
	q, p := softFixture(t)
	reserveFixture(t, q, "ag_one")
	marker := filepath.Join(seatDir(q.root, "ag_one"), "keep")
	os.WriteFile(marker, []byte("business-data"), 0600)
	q.track("ag_one", "fixture-cid")
	stopCalls := 0
	stop := func(ctx context.Context, id, cid string) error {
		stopCalls++
		if id != "ag_one" || cid != "fixture-cid" {
			t.Fatal("wrong target")
		}
		return nil
	}
	q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{1, 9 << 20}, nil }
	q.scan(context.Background(), stop)
	if q.state.Seats["ag_one"].Hold != "disk_limit" || q.state.Seats["ag_one"].StopState != "stopped" || stopCalls != 1 {
		t.Fatalf("missing hold/stop %+v calls%d", q.state.Seats["ag_one"], stopCalls)
	}
	for _, kind := range []string{CmdCreateSeat, CmdStartSeat, CmdRestartSeat, CmdUpgradeSeat, CmdRefreshCredSeat} {
		if err := q.commandGate(Command{Type: kind, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}); !errors.Is(err, ErrSoftQuota) {
			t.Fatalf("%s bypass %v", kind, err)
		}
	}
	if err := q.commandGate(Command{Type: CmdStopSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}); err != nil {
		t.Fatal("stop forbidden")
	}
	if b, _ := os.ReadFile(marker); string(b) != "business-data" {
		t.Fatal("hold deleted data")
	}
	q.close()
	restored, err := newSoftQuota(p, q.root, q.path, q.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	restored.disk = q.disk
	restored.resources = q.resources
	restored.sampleNode()
	restored.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{1024, 1024}, nil }
	if restored.state.Seats["ag_one"].Hold != "disk_limit" {
		t.Fatal("restart lost hold")
	}
	called := false
	if err := restored.start(context.Background(), "ag_one", func(context.Context) error { called = true; return nil }); err == nil || called {
		t.Fatal("final held start boundary bypass")
	}
	if len(restored.state.Events) != 2 {
		t.Fatalf("warning+hold outbox %+v", restored.state.Events)
	}
	restored.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{}, nil }
	restored.scan(context.Background(), stop)
	if restored.state.Seats["ag_one"].Hold == "" {
		t.Fatal("auto-resume cleared hold")
	}
}

func TestSoftWarningHysteresisUnknownBudgetAndStopFailure(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	now := time.Now()
	q.now = func() time.Time { return now }
	size := int64(7 << 20)
	q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{size, size}, nil }
	q.scan(context.Background(), func(context.Context, string, string) error { t.Fatal("warning stopped seat"); return nil })
	if !q.state.Seats["ag_one"].Warn || q.state.Seats["ag_one"].Hold != "" || len(q.state.Events) != 1 {
		t.Fatal("warning not emitted")
	}
	q.scan(context.Background(), nil)
	if len(q.state.Events) != 1 {
		t.Fatal("warning flood")
	}
	size = 5 << 20
	q.scan(context.Background(), nil)
	size = 7 << 20
	q.scan(context.Background(), nil)
	if len(q.state.Events) != 2 {
		t.Fatal("new warning episode missing")
	}
	q.usage = func(context.Context, string, string) (diskUsage, error) {
		return diskUsage{}, fmt.Errorf("unavailable")
	}
	now = now.Add(q.policy.MaxAge + time.Second)
	q.scan(context.Background(), func(context.Context, string, string) error { return fmt.Errorf("stop failed") })
	if q.state.Seats["ag_one"].Hold != "usage_unknown" || q.state.Seats["ag_one"].StopState != "unknown" {
		t.Fatal("unknown usage/stop reported safe")
	}
}

func TestSoftReservationBytesInodesInventoryConcurrencyAndReplay(t *testing.T) {
	q, _ := softFixture(t)
	base := fsSpace{1 << 40, 1 << 40, 10000000, 10000000}
	q.disk = func(string) (fsSpace, error) { v := base; v.FreeFiles = 1; return v, nil }
	q.sampleNode()
	if err := q.reserve("ag_low_inode", seatResources{4096, 2048, 8, 0}); err == nil {
		t.Fatal("inode reserve bypass")
	}
	q.disk = func(string) (fsSpace, error) { v := base; v.Free = 1; return v, nil }
	q.sampleNode()
	if err := q.reserve("ag_low_bytes", seatResources{4096, 2048, 8, 0}); err == nil {
		t.Fatal("byte reserve bypass")
	}
	q.disk = func(string) (fsSpace, error) { return base, nil }
	q.sampleNode()
	os.Mkdir(filepath.Join(q.root, "unmanaged"), 0700)
	q.sampleNode()
	if err := q.reserve("ag_unknown", seatResources{4096, 2048, 8, 0}); err == nil {
		t.Fatal("unmanaged inventory adopted")
	}
	os.Remove(filepath.Join(q.root, "unmanaged"))
	q.sampleNode()
	q.policy.MaxSeats = 1
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for _, id := range []string{"ag_a", "ag_b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if q.reserve(id, seatResources{4096, 2048, 8, 0}) == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	if wins != 1 || len(q.state.Seats) != 1 {
		t.Fatal("concurrent over-admission")
	}
	var id string
	for k := range q.state.Seats {
		id = k
	}
	os.Mkdir(seatDir(q.root, id), 0700)
	q.mu.Lock()
	q.holdLocked(id, "disk_limit")
	q.persistLocked()
	q.mu.Unlock()
	a := &Agent{commandGate: q.commandGate, handlers: map[string]Handler{CmdStartSeat: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("held handler ran")
		return nil, nil
	}}, ledger: &ledger{path: filepath.Join(t.TempDir(), "ledger"), m: map[string]Result{"old": {CommandID: "old", OK: true}}}}
	r := a.execute(context.Background(), Command{CommandID: "old", Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"` + id + `"}`)})
	if r.OK || !strings.Contains(r.Error, "soft_quota_blocked") {
		t.Fatalf("cached success bypass %+v", r)
	}
}

func TestSoftStartFreshUsageReserveAndNoCredentialSideEffects(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	if err := q.start(context.Background(), "ag_one", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	q.mu.Lock()
	q.holdLocked("ag_one", "disk_limit")
	q.persistLocked()
	q.mu.Unlock()
	f := newFakeDocker(t)
	d := f.ops(q.root)
	d.soft = q
	d.policy = q.policy
	marker := filepath.Join(seatDir(q.root, "ag_one"), "account-credential")
	os.WriteFile(marker, []byte("old-fixture"), 0600)
	for _, kind := range []string{"start", "restart", "upgrade", "refresh"} {
		var err error
		switch kind {
		case "start":
			_, err = d.start(context.Background(), json.RawMessage(`{"seat_id":"ag_one"}`))
		case "restart":
			_, err = d.restart(context.Background(), json.RawMessage(`{"seat_id":"ag_one"}`))
		case "upgrade":
			_, err = d.upgrade(context.Background(), json.RawMessage(`{"seat_id":"ag_one","image":"`+goodImage+`","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8},"cli":"codex","cred_kind":"token_file"}`))
		case "refresh":
			_, err = d.refreshCred(context.Background(), json.RawMessage(`{"seat_id":"ag_one","credential_file":{"content":"new-fixture"}}`))
		}
		if !errors.Is(err, ErrSoftQuota) {
			t.Fatalf("%s hold bypass %v", kind, err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("held operation reached Engine %+v", f.calls)
	}
	if b, _ := os.ReadFile(marker); string(b) != "old-fixture" {
		t.Fatal("held refresh changed credential")
	}
}

func TestSoftQuotaStateMetadataCorruptionAndUncleanRecovery(t *testing.T) {
	q, p := softFixture(t)
	reserveFixture(t, q, "ag_one")
	path, root := q.path, q.root
	// Simulate abrupt process loss without the clean-shutdown marker.
	q.lock.Close()
	q.lock = nil
	restored, err := newSoftQuota(p, root, path, q.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.state.Seats["ag_one"].Hold != "unclean_restart" {
		t.Fatal("unclean restart silently enabled writes")
	}
	restored.close()
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := newSoftQuota(p, root, path, q.nodeID); err == nil {
		got.close()
		t.Fatal("permissive state accepted")
	}
	os.Chmod(path, 0600)
	os.WriteFile(path, []byte(`{broken`), 0600)
	if got, err := newSoftQuota(p, root, path, q.nodeID); err == nil {
		got.close()
		t.Fatal("corrupt state reset")
	}
	os.Remove(path)
	os.Symlink(filepath.Join(t.TempDir(), "missing"), path)
	if got, err := newSoftQuota(p, root, path, q.nodeID); err == nil {
		got.close()
		t.Fatal("state symlink followed")
	}
}

func TestSoftEngineCapabilityAndIdentityPreflight(t *testing.T) {
	root := t.TempDir()
	p := DefaultNodePolicy()
	p.Mode = "soft"
	p.SoftApprovalRef = "fixture-risk"
	p.HostRootTrustRef = "fixture-trust"
	p.NetworkApprovalRef = "fixture-network"
	p.EngineID = "approved"
	p.EngineRoot = root
	good := map[string]any{"ID": "approved", "DockerRootDir": root, "CgroupDriver": "systemd", "CgroupVersion": "2", "MemoryLimit": true, "SwapLimit": true, "CpuCfsPeriod": true, "CpuCfsQuota": true, "PidsLimit": true}
	for _, key := range []string{"", "ID", "DockerRootDir", "MemoryLimit", "SwapLimit", "CpuCfsPeriod", "CpuCfsQuota", "PidsLimit", "CgroupDriver", "CgroupVersion"} {
		t.Run(key, func(t *testing.T) {
			info := map[string]any{}
			for k, v := range good {
				info[k] = v
			}
			if key != "" {
				delete(info, key)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/info" {
					t.Error("preflight mutated Engine")
				}
				json.NewEncoder(w).Encode(info)
			}))
			defer srv.Close()
			err := checkSoftEngine(context.Background(), newFakeEngine(srv.Client(), srv.URL), root, p)
			if (err == nil) != (key == "") {
				t.Fatalf("%s %v", key, err)
			}
		})
	}
	file := filepath.Join(t.TempDir(), "regular")
	os.WriteFile(file, nil, 0600)
	if ValidateIsolatedSocket(file) == nil {
		t.Fatal("regular socket accepted")
	}
	short, err := os.MkdirTemp("/tmp", "sq-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(short)
	socket := filepath.Join(short, "engine.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err = ValidateIsolatedSocket(socket); err != nil {
		t.Fatal(err)
	}
}

func TestSeatHardLimitsAndLoginNoLogPreserved(t *testing.T) {
	d := &dockerOps{}
	s := d.buildSpec(goodImage, "ag_limits", "net", seatResources{MemoryMB: 4096, CPUShares: 2048, DiskMB: 8, CPULimitMillicores: 3000}, "token_file", "codex", "")
	h := s["HostConfig"].(map[string]any)
	if h["CpuQuota"] != int64(300000) || h["CpuPeriod"] != int64(100000) || h["PidsLimit"] != int64(512) || h["ReadonlyRootfs"] != true || h["MemorySwap"] != int64(4096<<20) {
		t.Fatalf("hard limits %+v", h)
	}
	logs, ok := h["LogConfig"].(map[string]any)
	if !ok {
		t.Fatal("missing log bounds")
	}
	if logs["Type"] != "local" || logs["Config"].(map[string]string)["max-file"] != "3" {
		t.Fatalf("unbounded logs %+v", logs)
	}
	l := &loginOps{d: d}
	login := l.spec("fixture", goodImage, "/fixture", "net")["HostConfig"].(map[string]any)
	if login["PidsLimit"] != int64(128) || login["CpuQuota"] != int64(25000) || login["LogConfig"].(map[string]any)["Type"] != "none" {
		t.Fatalf("login safety %+v", login)
	}
}

func TestSoftCreatePositiveAndFailureRetainsData(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			q, _ := softFixture(t)
			f := newFakeDocker(t)
			d := f.ops(q.root)
			d.soft = q
			d.policy = q.policy
			d.quota = softDirectory{}
			if fail {
				f.failAt = "/start"
			}
			raw := json.RawMessage(`{"seat_id":"ag_create","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8},"cred_kind":"token_file","cli":"codex"}`)
			result, err := d.create(context.Background(), raw)
			if fail && err == nil || !fail && err != nil {
				t.Fatalf("create %s %v", result, err)
			}
			if _, err := os.Stat(seatDir(q.root, "ag_create")); err != nil {
				t.Fatal("failed soft create erased data directory")
			}
			if len(q.state.Seats["ag_create"].CIDs) != 1 {
				t.Fatal("container not recorded before start")
			}
			spec := f.specs[0]
			labels := spec["Labels"].(map[string]any)
			if labels[managedSeatLabel] != q.nodeID || labels[managedSeatIDLabel] != "ag_create" {
				t.Fatal("ownership labels missing")
			}
			for _, call := range f.calls {
				if strings.HasPrefix(call, "DELETE") {
					t.Fatalf("soft failure deleted data/object: %s", call)
				}
			}
		})
	}
}

func TestSoftMonitorWithoutHubStopsOnlyOwnedContainers(t *testing.T) {
	for _, bad := range []string{"", "label", "mount", "id"} {
		t.Run(bad, func(t *testing.T) {
			q, _ := softFixture(t)
			reserveFixture(t, q, "ag_one")
			q.track("ag_one", "cid")
			q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{9 << 20, 9 << 20}, nil }
			var mu sync.Mutex
			running := true
			stops := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == "POST" {
					if r.URL.Path != "/containers/cid/stop" {
						t.Errorf("wrong stop %s", r.URL.Path)
					}
					stops++
					running = false
					w.Write([]byte(`{}`))
					return
				}
				id, node, source := "cid", q.nodeID, seatDir(q.root, "ag_one")
				if bad == "id" {
					id = "other"
				}
				if bad == "label" {
					node = "foreign"
				}
				if bad == "mount" {
					source = "/foreign"
				}
				json.NewEncoder(w).Encode(map[string]any{"Id": id, "State": map[string]bool{"Running": running}, "Config": map[string]any{"Labels": map[string]string{managedSeatLabel: node, managedSeatIDLabel: "ag_one"}}, "Mounts": []map[string]string{{"Source": source, "Destination": "/work"}}})
			}))
			defer srv.Close()
			d := &dockerOps{eng: newFakeEngine(srv.Client(), srv.URL), soft: q, policy: q.policy, seatRoot: q.root}
			// No Agent.Run, websocket or Hub is involved: the monitor has its own lifetime.
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); d.runSoft(ctx) }()
			deadline := time.Now().Add(2 * time.Second)
			for {
				q.mu.Lock()
				state := q.state.Seats["ag_one"].StopState
				q.mu.Unlock()
				if state == "stopped" || state == "unknown" {
					break
				}
				if time.Now().After(deadline) {
					cancel()
					<-done
					t.Fatal("offline monitor did not enforce hold")
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			<-done
			mu.Lock()
			n := stops
			mu.Unlock()
			if bad == "" && n != 1 || bad != "" && n != 0 {
				t.Fatalf("ownership %s stops%d", bad, n)
			}
			if _, err := os.Stat(seatDir(q.root, "ag_one")); err != nil {
				t.Fatal("monitor deleted data")
			}
		})
	}
}

func TestSoftScanBudgetUnknownAndOutboxAcknowledgement(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	q.policy.ScanBudget = 10 * time.Millisecond
	q.state.Seats["ag_one"].CreatedAt = time.Now().Add(-2 * q.policy.MaxAge)
	q.usage = func(ctx context.Context, _, _ string) (diskUsage, error) { <-ctx.Done(); return diskUsage{}, ctx.Err() }
	calls := 0
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	q.scan(parent, func(context.Context, string, string) error { calls++; return nil })
	if time.Since(start) > time.Second || calls != 1 || q.state.Seats["ag_one"].Hold != "usage_unknown" {
		t.Fatal("scan budget ignored")
	}
	seats, events, ready := q.report()
	if ready || seats["ag_one"].CIDs != nil || len(events) != 2 || events[0].Reason != "usage_warning" || events[1].Reason != "usage_unknown" {
		t.Fatal("unsafe capacity metadata")
	}
	if err := q.acknowledge([]string{events[0].ID, events[1].ID}); err != nil {
		t.Fatal(err)
	}
	q.close()
	restored, err := newSoftQuota(q.policy, q.root, q.path, q.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	if len(restored.state.Events) != 0 || restored.state.Seats["ag_one"].Hold == "" {
		t.Fatal("ack removed hold or failed to persist")
	}
}

func TestSoftResourceAndCommittedDiskReserve(t *testing.T) {
	q, _ := softFixture(t)
	q.resources = func() (int64, int64, int64, error) { return 2000, 1 << 40, 1 << 40, nil }
	q.sampleNode()
	if q.reserve("ag_cpu", seatResources{4096, 2048, 8, 0}) == nil {
		t.Fatal("host CPU reserve bypass")
	}
	q.resources = func() (int64, int64, int64, error) { return 64000, 4 << 30, 4 << 30, nil }
	q.sampleNode()
	if q.reserve("ag_memory", seatResources{4096, 2048, 8, 0}) == nil {
		t.Fatal("host memory reserve bypass")
	}
	q.resources = func() (int64, int64, int64, error) { return 64000, 1 << 40, 1 << 40, nil }
	q.sampleNode()
	reserveFixture(t, q, "ag_one")
	q.state.Seats["ag_one"].Usage = diskUsage{Allocated: 4 << 20, Apparent: 4 << 20}
	// Reserve uses unallocated commitment (8-4), not the entire limit twice.
	q.disk = func(string) (fsSpace, error) {
		return fsSpace{100 << 30, q.policy.ReserveBytes + q.policy.ImageReserveBytes + (12 << 20), 10000000, 10000000}, nil
	}
	q.sampleNode()
	if err := q.reserve("ag_two", seatResources{4096, 2048, 8, 0}); err != nil {
		t.Fatalf("allocated usage double counted %v", err)
	}
	if err := q.reserve("ag_three", seatResources{4096, 2048, 8, 0}); err == nil {
		t.Fatal("commitment over-admission")
	}
}

func TestUsageRejectsSameDeviceMountTable(t *testing.T) {
	raw := []byte("10 2 0:1 / /work/seat/sub rw - ext4 /dev/fake rw\n")
	if rejectNestedMount(raw, "/work", "seat") == nil {
		t.Fatal("same-device mount ignored")
	}
	if rejectNestedMount(raw, "/work", "other") != nil {
		t.Fatal("unrelated mount rejected")
	}
}

func TestSoftPrivateWriteFailureStillStopsAndCannotResumeAfterCrash(t *testing.T) {
	q, p := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	original := q.path
	q.path = filepath.Dir(original) // rename over a directory fails, even as root.
	q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{9 << 20, 9 << 20}, nil }
	stops := 0
	q.scan(context.Background(), func(context.Context, string, string) error { stops++; return nil })
	if !q.broken || stops != 1 {
		t.Fatal("failed private write bypassed stop")
	}
	if q.commandGate(Command{Type: CmdLoginStart, Params: json.RawMessage(`{}`)}) == nil {
		t.Fatal("failed persistence enabled writer")
	}
	q.lock.Close()
	q.lock = nil
	restored, err := newSoftQuota(p, q.root, original, q.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	if restored.state.Seats["ag_one"].Hold != "unclean_restart" {
		t.Fatal("unpersisted hold silently resumed after crash")
	}
}

func TestSoftEnforcementBatchDeadlineNotMultipliedBySeatCount(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	reserveFixture(t, q, "ag_two")
	q.track("ag_one", "one")
	q.track("ag_two", "two")
	q.policy.StopTimeout = 20 * time.Millisecond
	q.mu.Lock()
	q.holdLocked("ag_one", "disk_limit")
	q.holdLocked("ag_two", "disk_limit")
	q.mu.Unlock()
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	q.enforce(parent, func(ctx context.Context, _, _ string) error { <-ctx.Done(); return ctx.Err() })
	if time.Since(start) > time.Second || q.state.Seats["ag_one"].StopState != "unknown" || q.state.Seats["ag_two"].StopState != "unknown" {
		t.Fatal("stop batch unbounded or false success")
	}
}

func TestSoftUnknownAndAgeGateRecovery(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	now := time.Now()
	q.now = func() time.Time { return now }
	q.scan(context.Background(), nil)
	q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{}, errors.New("read error") }
	q.scan(context.Background(), nil)
	if q.state.Seats["ag_one"].Hold != "" {
		t.Fatal("early unknown prematurely held")
	}
	if q.commandGate(Command{Type: CmdLoginStart, Params: json.RawMessage(`{}`)}) == nil {
		t.Fatal("unknown read admitted writer")
	}
	q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{1024, 1024}, nil }
	q.scan(context.Background(), nil)
	if err := q.commandGate(Command{Type: CmdLoginStart, Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatal("fresh recovered reading blocked", err)
	}
	now = now.Add(q.policy.MaxAge + time.Second)
	if q.commandGate(Command{Type: CmdLoginStart, Params: json.RawMessage(`{}`)}) == nil {
		t.Fatal("stale metadata admitted writer")
	}
}

func TestSoftMemoryCommitAndAvailableReserveSeparate(t *testing.T) {
	q, _ := softFixture(t)
	q.resources = func() (int64, int64, int64, error) { return 64000, 1 << 40, 4 << 30, nil }
	q.sampleNode()
	if q.reserve("ag_avail", seatResources{4096, 2048, 8, 0}) == nil {
		t.Fatal("available memory reserve ignored")
	}
	q.resources = func() (int64, int64, int64, error) { return 64000, 12 << 30, 12 << 30, nil }
	q.sampleNode()
	reserveFixture(t, q, "ag_one")
	reserveFixture(t, q, "ag_two")
	if q.reserve("ag_total", seatResources{4096, 2048, 8, 0}) == nil {
		t.Fatal("memory commitments exceeded total")
	}
}

func TestSoftUnrecordedContainerIsNeverAdopted(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "recorded")
	f := newFakeDocker(t)
	d := f.ops(q.root)
	d.soft = q
	if _, err := d.seatContainerID(context.Background(), "ag_one"); err == nil {
		t.Fatal("unrecorded name target adopted")
	}
	if d.startWriter(context.Background(), "ag_one", "unrecorded", false) == nil {
		t.Fatal("final writer accepted unrecorded object")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "POST") {
			t.Fatal("unrecorded object modified", c)
		}
	}
}

func TestSoftApprovalReferencesAndDefaultSocketAliases(t *testing.T) {
	p := DefaultNodePolicy()
	p.Mode = "soft"
	p.EngineID = "fixture"
	p.EngineRoot = "/isolated"
	p.SoftApprovalRef = "owner"
	p.HostRootTrustRef = "owner-trust"
	p.NetworkApprovalRef = "approved-network"
	for _, key := range []string{"risk", "root", "network"} {
		v := p
		switch key {
		case "risk":
			v.SoftApprovalRef = ""
		case "root":
			v.HostRootTrustRef = ""
		case "network":
			v.NetworkApprovalRef = ""
		}
		if v.Validate() == nil {
			t.Fatal("missing approval accepted", key)
		}
	}
	dir, err := os.MkdirTemp("/tmp", "sq-sock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "default.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	alias := filepath.Join(dir, "alias.sock")
	if err := os.Symlink(socket, alias); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{socket, alias, dir + "/sub/../default.sock"} {
		if err := validateIsolatedSocket(candidate, socket); err == nil {
			t.Fatal("default socket alias accepted", candidate)
		}
	}
	other := filepath.Join(dir, "isolated.sock")
	l2, err := net.Listen("unix", other)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	if err := validateIsolatedSocket(other, socket); err != nil {
		t.Fatal("approved distinct fixture socket rejected", err)
	}
}

func TestNodeResourceCapabilityAndCPUMinimum(t *testing.T) {
	f := newFakeDocker(t)
	d := f.ops(t.TempDir())
	c, err := d.collectCapacity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.QuotaMode != "hard" || c.ResourceLimitsVersion != 1 || len(c.QuotaPolicyRevision) != 64 {
		t.Fatalf("capability %+v", c)
	}
	for _, cpu := range []int64{-1, 1, 9, 256001} {
		r := seatResources{MemoryMB: 4096, CPUShares: 2048, DiskMB: 8, CPULimitMillicores: cpu}
		if r.validate() == nil {
			t.Fatal("invalid CPU limit accepted", cpu)
		}
	}
	for _, cpu := range []int64{0, 10, 2000, 256000} {
		r := seatResources{MemoryMB: 4096, CPUShares: 2048, DiskMB: 8, CPULimitMillicores: cpu}
		if err := r.validate(); err != nil {
			t.Fatal("valid CPU limit refused", cpu, err)
		}
	}
}

func TestSoftHoldDurableBeforeStop(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	q.usage = func(context.Context, string, string) (diskUsage, error) { return diskUsage{9 << 20, 9 << 20}, nil }
	durable := false
	q.scan(context.Background(), func(context.Context, string, string) error {
		raw, err := os.ReadFile(q.path)
		if err != nil {
			return err
		}
		var state softState
		if err = json.Unmarshal(raw, &state); err != nil {
			return err
		}
		seat := state.Seats["ag_one"]
		durable = seat != nil && seat.Hold == "disk_limit"
		if !durable {
			return errors.New("stop preceded durable hold")
		}
		return nil
	})
	if !durable {
		t.Fatal("hold was not durable before stopping writer")
	}
}

func TestUsageFDStability(t *testing.T) {
	if os.Getenv("DW_SOFT_QUOTA_FD_PROBE") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run", "^TestUsageFDStability$", "-test.v")
		cmd.Env = append(os.Environ(), "DW_SOFT_QUOTA_FD_PROBE=1")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "FD_PROBE_OK") {
			t.Fatalf("isolated FD probe %v: %s", err, out)
		}
		t.Log(string(out))
		return
	}
	// Only this child test process changes GC/RLIMIT; no parent/host limits change.
	debug.SetGCPercent(-1)
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	limit.Cur = 128
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	os.Mkdir(seatDir(root, "ag_fd"), 0700)
	os.WriteFile(filepath.Join(seatDir(root, "ag_fd"), "file"), []byte("fixture"), 0600)
	before, _ := os.ReadDir("/proc/self/fd")
	for i := 0; i < 1000; i++ {
		if _, err := measureUsage(context.Background(), root, "ag_fd"); err != nil {
			t.Fatal(i, err)
		}
	}
	after, _ := os.ReadDir("/proc/self/fd")
	if len(before) != len(after) {
		t.Fatalf("FD growth %d→%d", len(before), len(after))
	}
	t.Logf("FD_PROBE_OK before=%d after=%d iterations=1000 GC=off RLIMIT=128", len(before), len(after))
}

type rejectedHardQuota struct{}

func (rejectedHardQuota) Setup(string, string, int64) error {
	return errors.New("hard-backend-sentinel")
}
func (rejectedHardQuota) Clear(string, string) error { return errors.New("hard-cleanup-sentinel") }

func TestSoftRegistrationUsesDirectoryBackendAndReplayGate(t *testing.T) {
	q, p := softFixture(t)
	f := newFakeDocker(t)
	private := t.TempDir()
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{HubURL: "wss://fixture.invalid/api/node/connect", NodeID: "nd_fixture", SecretPath: filepath.Join(t.TempDir(), "never-read"), LedgerPath: filepath.Join(private, "ledger")})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterDockerHandlers(newFakeEngine(f.srv.Client(), f.srv.URL), rejectedHardQuota{}, q.root, "hub.local/dingwei", "fixture.slice", "http://fixture.invalid:11080", "wss://fixture.invalid", "", false, "", "", nil, p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.policyClose)
	a.docker.soft.disk = q.disk
	a.docker.soft.resources = q.resources
	a.docker.soft.sampleNode()
	a.docker.soft.usage = q.usage
	if a.docker.soft.capacity == nil {
		t.Fatal("production soft registration omitted B")
	}
	// New B wire-up is fail-closed until its independent sampler publishes data.
	if v := a.docker.soft.capacityReport(); v == nil || v.Known {
		t.Fatal("unsampled B must be unknown")
	}
	installCapacityFixture(a.docker.soft)
	create := Command{CommandID: "create", Type: CmdCreateSeat, Params: json.RawMessage(`{"seat_id":"ag_registered","image":"` + goodImage + `","resources":{"memory_mb":4096,"cpu_shares":2048,"disk_mb":8},"cred_kind":"token_file","cli":"codex"}`)}
	if r := a.execute(context.Background(), create); !r.OK {
		t.Fatalf("soft still used hard backend: %+v", r)
	}
	start := Command{CommandID: "start", Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_registered"}`)}
	if r := a.execute(context.Background(), start); !r.OK {
		t.Fatalf("fresh start %+v", r)
	}
	a.docker.soft.mu.Lock()
	a.docker.soft.holdLocked("ag_registered", "disk_limit")
	a.docker.soft.persistLocked()
	a.docker.soft.mu.Unlock()
	if r := a.execute(context.Background(), start); r.OK || !strings.Contains(r.Error, "soft_quota_blocked") {
		t.Fatalf("registered replay bypass: %+v", r)
	}
	if a.background == nil {
		t.Fatal("soft monitor not attached independently of Hub")
	}
}

func TestSoftMonitorMeasuresAgainOnPeriodicTick(t *testing.T) {
	q, p := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	p.ScanInterval = 30 * time.Millisecond
	p.ScanBudget = 10 * time.Millisecond
	p.MaxAge = 200 * time.Millisecond
	p.ReserveInterval = 5 * time.Millisecond
	q.policy = p
	samples := 0
	q.usage = func(context.Context, string, string) (diskUsage, error) {
		samples++
		if samples == 1 {
			return diskUsage{1024, 1024}, nil
		}
		return diskUsage{9 << 20, 9 << 20}, nil
	}
	var mu sync.Mutex
	running := true
	stopped := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "POST" {
			running = false
			select {
			case stopped <- struct{}{}:
			default:
			}
			w.Write([]byte(`{}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"Id": "cid", "State": map[string]bool{"Running": running}, "Config": map[string]any{"Labels": map[string]string{managedSeatLabel: q.nodeID, managedSeatIDLabel: "ag_one"}}, "Mounts": []map[string]string{{"Source": seatDir(q.root, "ag_one"), "Destination": "/work"}}})
	}))
	defer srv.Close()
	d := &dockerOps{eng: newFakeEngine(srv.Client(), srv.URL), soft: q, policy: p, seatRoot: q.root}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.runSoft(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("later usage never sampled/stopped")
	}
	q.mu.Lock()
	hold := q.state.Seats["ag_one"].Hold
	q.mu.Unlock()
	if hold != "disk_limit" {
		t.Fatalf("periodic measurement missing, hold=%s", hold)
	}
}
