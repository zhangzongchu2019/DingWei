package nodeagent

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestR31HardWiringUnknownCapacityAndNoSoftEnforcer(t *testing.T) {
	f := newFakeDocker(t)
	private := t.TempDir()
	root := filepath.Join(private, "seats")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{HubURL: "wss://fixture.invalid/api/node/connect", NodeID: "nd_hard", SecretPath: filepath.Join(private, "unused-secret"), LedgerPath: filepath.Join(private, "private", "ledger")})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.RegisterDockerHandlers(newFakeEngine(f.srv.Client(), f.srv.URL), rejectedHardQuota{}, root, "hub.local/dingwei", "fixture.slice", "http://fixture.invalid:11080", "wss://fixture.invalid", "", false, "", "", nil, DefaultNodePolicy()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.policyClose)
	if a.docker.soft != nil || a.docker.capacityQuota == nil || a.docker.capacityQuota.policy.Mode != "hard" {
		t.Fatal("hard capacity missing or soft enforcer enabled")
	}
	q := a.docker.capacityQuota
	cmd := Command{Type: CmdCreateSeat, Params: json.RawMessage(`{"seat_id":"ag_first"}`)}
	if err = a.commandGate(cmd); err == nil {
		t.Fatal("unknown hard measurement admitted")
	}
	q.capacity.snapshot = seatcapacity.Snapshot{Known: true, At: time.Now(), Total: seatcapacity.Resources{CPU: 16000, Memory: 30 << 30, Disk: 560 << 30}}
	q.policy.MaxSeats = 4
	r := seatResources{MemoryMB: 1536, CPUShares: 1024, CPULimitMillicores: 1000, DiskMB: 40 * 1024}
	for _, id := range []string{"ag_first", "ag_second", "ag_third", "ag_fourth"} {
		if err = q.reserve(id, r); err != nil {
			t.Fatal(err)
		}
	}
	if err = q.reserve("ag_fifth", r); err == nil {
		t.Fatal("hard MAX_SEATS bypass")
	}
	called := false
	if err = q.start(context.Background(), "ag_first", func(context.Context) error { called = true; return nil }); err != nil || !called {
		t.Fatalf("existing admitted hard seat cannot start: %v", err)
	}
	q.capacity.snapshot.Known = false
	called = false
	if err = q.start(context.Background(), "ag_first", func(context.Context) error { called = true; return nil }); err == nil || called {
		t.Fatal("unknown snapshot started hard writer")
	}
	q.mu.Lock()
	if len(q.state.Events) != 0 {
		t.Fatal("hard started soft events")
	}
	q.mu.Unlock()
}
