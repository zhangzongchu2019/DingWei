package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func heldReleaseFixture(t *testing.T) (*softQuota, *dockerOps, string) {
	t.Helper()
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_release")
	q.mu.Lock()
	s := q.state.Seats["ag_release"]
	s.Usage = diskUsage{1024, 1024}
	s.MeasuredAt = q.now()
	q.holdLocked("ag_release", "disk_limit")
	s.StopState = "stopped"
	id := s.HoldID
	if len(id) != 32 {
		t.Fatal("hold generation absent")
	}
	if err := q.persistLocked(); err != nil {
		t.Fatal(err)
	}
	q.mu.Unlock()
	return q, &dockerOps{soft: q}, id
}

func TestSoftHoldReleaseGenerationPersistentStoppedOnly(t *testing.T) {
	q, d, id := heldReleaseFixture(t)
	params, _ := json.Marshal(map[string]string{"seat_id": "ag_release", "hold_id": id})
	raw, err := d.releaseHold(context.Background(), params)
	if err != nil || !strings.Contains(string(raw), `"state":"stopped"`) {
		t.Fatalf("release %s %v", raw, err)
	}
	data, err := os.ReadFile(q.path)
	if err != nil {
		t.Fatal(err)
	}
	var stored softState
	if json.Unmarshal(data, &stored) != nil || stored.Seats["ag_release"].Hold != "" {
		t.Fatal("release not persisted")
	}
	q.mu.Lock()
	q.holdLocked("ag_release", "node_space_emergency")
	q.state.Seats["ag_release"].StopState = "stopped"
	next := q.state.Seats["ag_release"].HoldID
	q.mu.Unlock()
	if next == id {
		t.Fatal("new hazard reused stale owner approval")
	}
	if _, err = d.releaseHold(context.Background(), params); err == nil {
		t.Fatal("stale approval cleared new hold")
	}
	seats, _, _ := q.report()
	if seats["ag_release"].HoldID != next || len(seats["ag_release"].CIDs) != 0 {
		t.Fatal("hold/report changed")
	}
}

func TestSoftHoldReleaseLedgerReplayCannotClearLaterHold(t *testing.T) {
	q, d, id := heldReleaseFixture(t)
	a, err := New(Config{HubURL: "wss://fixture.invalid/node", NodeID: "nd_fixture", SecretPath: "/unused-synthetic-fixture", LedgerPath: filepath.Join(t.TempDir(), "ledger.json")})
	if err != nil {
		t.Fatal(err)
	}
	a.commandGate = q.commandGate
	a.Register(CmdReleaseHold, d.releaseHold)
	params, _ := json.Marshal(map[string]string{"seat_id": "ag_release", "hold_id": id})
	cmd := Command{CommandID: "fixture-release-once", Type: CmdReleaseHold, Params: params}
	if r := a.execute(context.Background(), cmd); !r.OK {
		t.Fatal(r.Error)
	}
	q.mu.Lock()
	q.holdLocked("ag_release", "node_inode_emergency")
	next := q.state.Seats["ag_release"].HoldID
	q.mu.Unlock()
	if r := a.execute(context.Background(), cmd); !r.OK {
		t.Fatal("ledger replay did not return original receipt")
	}
	seats, _, _ := q.report()
	if seats["ag_release"].HoldID != next || seats["ag_release"].Hold == "" {
		t.Fatal("ledger replay cleared later hold")
	}
}

func TestSoftHoldReleaseEverySafetyGate(t *testing.T) {
	for name, change := range map[string]func(*softQuota){
		"pending_stop":         func(q *softQuota) { q.state.Seats["ag_release"].StopState = "pending" },
		"failed_stop":          func(q *softQuota) { q.state.Seats["ag_release"].StopState = "failed" },
		"usage_above_recovery": func(q *softQuota) { s := q.state.Seats["ag_release"]; s.Usage.Apparent = s.Limit },
		"usage_unknown":        func(q *softQuota) { q.state.Seats["ag_release"].UsageUnknown = true },
		"usage_stale":          func(q *softQuota) { q.state.Seats["ag_release"].MeasuredAt = q.now().Add(-2 * q.policy.MaxAge) },
		"root_unknown":         func(q *softQuota) { q.spaceErr = errors.New("private fixture failure") },
		"root_stale":           func(q *softQuota) { q.spaceAt = q.now().Add(-2 * q.policy.MaxAge) },
		"byte_reserve":         func(q *softQuota) { q.space.Free = 1 },
		"inode_reserve":        func(q *softQuota) { q.space.FreeFiles = 1 },
		"broken_state":         func(q *softQuota) { q.broken = true },
		"retiring":             func(q *softQuota) { q.state.Seats["ag_release"].Retiring = true },
	} {
		t.Run(name, func(t *testing.T) {
			q, _, id := heldReleaseFixture(t)
			q.mu.Lock()
			change(q)
			q.mu.Unlock()
			if err := q.releaseHold("ag_release", id); err == nil {
				t.Fatal("unsafe release accepted")
			}
			q.mu.Lock()
			defer q.mu.Unlock()
			if q.state.Seats["ag_release"].Hold == "" {
				t.Fatal("hold silently cleared")
			}
		})
	}
}

func TestSoftHoldNeverAutoReleasesAndPersistenceFailureClosed(t *testing.T) {
	q, _, id := heldReleaseFixture(t)
	q.measure(context.Background())
	q.checkReserve()
	seats, _, _ := q.report()
	if seats["ag_release"].HoldID != id {
		t.Fatal("monitor auto released hold")
	}
	q.writeState = func(string, []byte) error { return errors.New("secret fixture path") }
	if err := q.releaseHold("ag_release", id); err != errHoldRelease {
		t.Fatalf("unsafe/raw error %v", err)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.broken || q.state.Seats["ag_release"].HoldID != id || q.admitLocked(0) == nil {
		t.Fatal("failed persistence opened gate")
	}
}

func TestSoftHoldReleaseCommandWhitelistAndMalformed(t *testing.T) {
	if !commandWhitelist[CmdReleaseHold] {
		t.Fatal("release command not wired")
	}
	q, d, id := heldReleaseFixture(t)
	for _, raw := range []string{`{} {}`, `{}`, `{"seat_id":"../../secret","hold_id":"` + id + `"}`, `{"seat_id":"ag_release","hold_id":"bad"}`, `{"seat_id":"ag_release","hold_id":"` + id + `","path":"secret"}`} {
		if _, err := d.releaseHold(context.Background(), json.RawMessage(raw)); err != errHoldRelease {
			t.Fatalf("malformed %q: %v", raw, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	params, _ := json.Marshal(map[string]string{"seat_id": "ag_release", "hold_id": id})
	if _, err := d.releaseHold(ctx, params); err == nil {
		t.Fatal("cancelled command executed")
	}
	if _, err := (&dockerOps{}).releaseHold(context.Background(), params); err == nil {
		t.Fatal("hard mode release accepted")
	}
	q.mu.Lock()
	q.state.Seats["ag_release"].MeasuredAt = q.now().Add(time.Second)
	q.mu.Unlock()
	if err := q.releaseHold("ag_release", id); err == nil {
		t.Fatal("future sample accepted")
	}
}
