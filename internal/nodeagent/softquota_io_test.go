package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSoftInodeAdmissionRemainsAboveEmergencyLine(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	space := fsSpace{1 << 40, 1 << 40, 10000000, 10000000}
	space.FreeFiles = q.inodeEmergencyThreshold(space)
	q.disk = func(string) (fsSpace, error) { return space, nil }
	q.sampleNode()
	q.checkReserve()
	if q.state.Seats["ag_one"].Hold != "" {
		t.Fatal("equality at inode emergency line incorrectly held seat")
	}
	if err := q.commandGate(Command{Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}); err == nil {
		t.Fatal("original I admission gate weakened")
	}
	q.enforce(context.Background(), func(context.Context, string, string) error {
		t.Fatal("inode I unexpectedly stopped running seat")
		return nil
	})
}
func TestSoftStatfsErrorWarnsBlocksAndExpiresFromLastGood(t *testing.T) {
	for _, kind := range []string{"error", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			q, _ := softFixture(t)
			reserveFixture(t, q, "ag_one")
			q.track("ag_one", "cid")
			now := time.Now()
			q.now = func() time.Time { return now }
			q.sampleNode()
			last := q.state.LastSpaceAt
			q.disk = func(string) (fsSpace, error) {
				if kind == "error" {
					return fsSpace{}, errors.New("fixture")
				}
				return fsSpace{1, 2, 1, 2}, nil
			}
			q.sampleNode()
			q.checkReserve()
			if q.state.Seats["ag_one"].Hold != "" || !q.state.Seats["ag_one"].SpaceWarning {
				t.Fatal("first statfs error became F=0 hold or lost warning")
			}
			if err := q.commandGate(Command{Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}); err == nil {
				t.Fatal("old good statfs admitted writer")
			}
			if _, _, ready := q.report(); ready {
				t.Fatal("unknown snapshot reported ready")
			}
			if len(q.state.Events) != 1 || q.state.Events[0].Reason != "usage_warning" {
				t.Fatal("initial unknown did not notify once")
			}
			q.enforce(context.Background(), func(context.Context, string, string) error { t.Fatal("first error prematurely stopped"); return nil })
			now = last.Add(q.policy.MaxAge)
			q.state.Seats["ag_one"].MeasuredAt = now
			q.checkReserve()
			if q.state.Seats["ag_one"].Hold != "" {
				t.Fatal("exactly60s is not beyond60s")
			}
			now = now.Add(time.Nanosecond)
			q.state.Seats["ag_one"].MeasuredAt = now
			q.checkReserve()
			if q.state.Seats["ag_one"].Hold != "usage_unknown" {
				t.Fatal("old good value allowed indefinite unknown operation")
			}
			stops := 0
			q.enforce(context.Background(), func(context.Context, string, string) error {
				stops++
				raw, _ := os.ReadFile(q.path)
				var state softState
				if json.Unmarshal(raw, &state) != nil || state.Seats["ag_one"].Hold != "usage_unknown" {
					t.Fatal("expired hold not durable before stop")
				}
				return nil
			})
			if stops != 1 {
				t.Fatal("expired unknown did not stop")
			}
			q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1 << 40, 10000000, 10000000}, nil }
			q.sampleNode()
			q.checkReserve()
			if q.state.Seats["ag_one"].Hold != "usage_unknown" {
				t.Fatal("recovery auto-released hold")
			}
		})
	}
}
func TestSoftStatfsRecoveryWithinGraceAndRestartDoesNotResetAnchor(t *testing.T) {
	q, p := softFixture(t)
	reserveFixture(t, q, "ag_one")
	now := time.Now()
	q.now = func() time.Time { return now }
	q.sampleNode()
	first := now
	high := q.disk
	q.disk = func(string) (fsSpace, error) { return fsSpace{}, errors.New("fixture") }
	q.sampleNode()
	q.checkReserve()
	now = first.Add(30 * time.Second)
	q.disk = high
	q.sampleNode()
	q.checkReserve()
	if q.state.Seats["ag_one"].Hold != "" || q.state.Seats["ag_one"].SpaceWarning {
		t.Fatal("recovered within grace remained unknown")
	}
	if q.commandGate(Command{Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}) != nil {
		t.Fatal("recovered good snapshot unusable")
	}
	good := now
	q.disk = func(string) (fsSpace, error) { return fsSpace{}, errors.New("fixture") }
	q.sampleNode()
	q.checkReserve()
	q.close()
	restored, err := newSoftQuota(p, q.root, q.path, q.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	now = good.Add(p.MaxAge + time.Nanosecond)
	restored.now = func() time.Time { return now }
	restored.disk = q.disk
	restored.resources = q.resources
	restored.sampleNode()
	restored.mu.Lock()
	restored.state.Seats["ag_one"].MeasuredAt = now
	restored.mu.Unlock()
	restored.checkReserve()
	if !restored.state.LastSpaceAt.Equal(good) || restored.state.Seats["ag_one"].Hold != "usage_unknown" {
		t.Fatal("restart reset persisted last-good age")
	}
}
func TestSoftBlockedNodeProbesCannotBlockReserveAdmissionOrCapacity(t *testing.T) {
	for _, stage := range []string{"statfs", "resources", "inventory"} {
		t.Run(stage, func(t *testing.T) {
			q, _ := softFixture(t)
			reserveFixture(t, q, "ag_one")
			q.track("ag_one", "cid")
			q.policy.ScanBudget = 10 * time.Millisecond
			q.policy.ReserveInterval = 5 * time.Millisecond
			base := time.Now()
			var clock atomic.Int64
			clock.Store(base.UnixNano())
			q.now = func() time.Time { return time.Unix(0, clock.Load()) }
			q.sampleNode()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var reads atomic.Int32
			block := func() { reads.Add(1); once.Do(func() { close(entered) }); <-release }
			switch stage {
			case "statfs":
				q.disk = func(string) (fsSpace, error) { block(); return fsSpace{1 << 40, 1 << 40, 10000000, 10000000}, nil }
			case "resources":
				q.resources = func() (int64, int64, int64, error) { block(); return 64000, 1 << 40, 1 << 40, nil }
			case "inventory":
				q.readInventory = func(root string) ([]os.DirEntry, error) { block(); return os.ReadDir(root) }
			}

			stopped := make(chan struct{}, 1)
			d := reserveMonitorFixture(t, q, func(context.Context) {
				select {
				case stopped <- struct{}{}:
				default:
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); d.runSoft(ctx) }()
			released := false
			defer func() {
				cancel()
				if !released {
					close(release)
				}
				<-done
			}()
			waitReserve(t, entered, "statfs worker did not enter")
			clock.Store(base.Add(q.policy.ScanBudget + time.Millisecond).UnixNano())
			quick := make(chan struct{})
			go func() {
				q.checkReserve()
				if q.commandGate(Command{Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}) == nil {
					t.Error("hung snapshot admitted writer")
				}
				for i := 0; i < 20; i++ {
					if _, _, ready := q.report(); ready {
						t.Error("hung snapshot reported ready")
					}
				}
				cap, err := d.collectCapacity(context.Background())
				if err != nil {
					t.Error(err)
				} else if cap.DiskSnapshotValid == nil || *cap.DiskSnapshotValid || cap.SoftReady {
					t.Error("blocked probe did not mark report snapshot unknown")
				}
				close(quick)
			}()
			select {
			case <-quick:
			case <-time.After(time.Second):
				close(release)
				released = true
				cancel()
				<-quick
				t.Fatal("blocked statfs held reserve/admit/report")
			}
			q.mu.Lock()
			held := q.state.Seats["ag_one"].Hold
			q.mu.Unlock()
			if held != "" {
				t.Fatal("budget timeout equated first unknown to F=0")
			}
			clock.Store(base.Add(q.policy.MaxAge + time.Second).UnixNano())
			q.mu.Lock()
			q.state.Seats["ag_one"].MeasuredAt = q.now()
			q.mu.Unlock()
			waitReserve(t, stopped, "blocked statfs prevented expiry stop")
			q.mu.Lock()
			held = q.state.Seats["ag_one"].Hold
			q.mu.Unlock()
			if held != "usage_unknown" || reads.Load() != 1 {
				t.Fatalf("expiry or bounded worker violated hold=%s reads=%d", held, reads.Load())
			}
		})
	}
}

func TestSoftBlockedStateWriteDoesNotHoldMutexOrAccumulateWorkers(t *testing.T) {
	q, p := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	q.policy.StateWriteBudget = 20 * time.Millisecond
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var enterOnce, finishOnce sync.Once
	q.writeState = func(path string, raw []byte) error {
		calls.Add(1)
		enterOnce.Do(func() { close(entered) })
		<-release
		defer finishOnce.Do(func() { close(finished) })
		return writeSoftState(path, raw)
	}
	q.mu.Lock()
	q.holdLocked("ag_one", "disk_limit")
	q.mu.Unlock()
	stopped := make(chan struct{}, 1)
	d := reserveMonitorFixture(t, q, func(context.Context) { stopped <- struct{}{} })
	enforced := make(chan struct{})
	go func() { defer close(enforced); q.enforce(context.Background(), d.stopOwned) }()
	released := false
	defer func() {
		if !released {
			close(release)
		}
		<-finished
		<-enforced
	}()
	waitReserve(t, entered, "state writer not entered")
	quick := make(chan struct{})
	go func() {
		q.checkReserve()
		q.report()
		q.commandGate(Command{Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)})
		close(quick)
	}()
	select {
	case <-quick:
	case <-time.After(time.Second):
		close(release)
		released = true
		<-quick
		t.Fatal("state fsync held quota mutex")
	}
	waitReserve(t, stopped, "state write timeout prevented fail-closed stop")
	<-enforced
	if _, _, ready := q.report(); ready || !q.broken {
		t.Fatal("private-write failure did not block admission/report")
	}
	for i := 0; i < 20; i++ {
		q.mu.Lock()
		err := q.persistLocked()
		q.mu.Unlock()
		if err == nil {
			t.Fatal("broken persistence admitted another request")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("blocked persistence spawned replacement workers")
	}
	// A failed private write must not flip Active=false on close; before release
	// the last durable state still has the old active marker and container identity.
	raw, err := os.ReadFile(q.path)
	if err != nil {
		t.Fatal(err)
	}
	var state softState
	if json.Unmarshal(raw, &state) != nil || !state.Active || len(state.Seats["ag_one"].CIDs) != 1 {
		t.Fatal("failed-write crash safety marker lost")
	}
	// Close is bounded but retains flock in the sole in-flight worker. A
	// replacement controller must not race a late old rename.
	q.close()
	for i := 0; i < 10; i++ {
		other, err := newSoftQuota(p, q.root, q.path, q.nodeID)
		if err == nil {
			other.close()
			t.Fatal("released lifetime lock before old IO completed")
		}
	}
	close(release)
	released = true
	<-finished
	<-q.persistDone
	restored, err := newSoftQuota(p, q.root, q.path, q.nodeID)
	if err != nil {
		t.Fatal("completed worker did not release lifetime lock", err)
	}
	defer restored.close()
	if restored.state.Seats["ag_one"].Hold == "" {
		t.Fatal("late state write lost hold")
	}
}

func TestSoftMissingAgeAnchorIsRejectedWithoutReset(t *testing.T) {
	q, p := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.close()
	raw, err := os.ReadFile(q.path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("fixture")
	}
	delete(fields, "last_space_at")
	delete(fields, "unknown_started_at")
	without, _ := json.Marshal(fields)
	if err := os.WriteFile(q.path, without, 0600); err != nil {
		t.Fatal(err)
	}
	other, err := newSoftQuota(p, q.root, q.path, q.nodeID)
	if err == nil {
		other.close()
		t.Fatal("missing age anchor silently reset grace")
	}
	after, _ := os.ReadFile(q.path)
	if string(after) != string(without) {
		t.Fatal("unsupported private state rewritten")
	}
}
func TestSoftStateWriteBudgetConfiguration(t *testing.T) {
	p, err := ParseNodePolicy(func(string) string { return "" })
	if err != nil || p.StateWriteBudget != 5*time.Second {
		t.Fatal("missing bounded write default")
	}
	for _, value := range []string{"0s", "-1s", "31s", "bad"} {
		if _, err := ParseNodePolicy(func(k string) string {
			if k == "NODEAGENT_STATE_WRITE_BUDGET" {
				return value
			}
			return ""
		}); err == nil {
			t.Fatal("invalid write budget admitted", value)
		}
	}
	changed, err := ParseNodePolicy(func(k string) string {
		if k == "NODEAGENT_STATE_WRITE_BUDGET" {
			return "2s"
		}
		return ""
	})
	if err != nil || changed.StateWriteBudget != 2*time.Second || changed.Revision() == p.Revision() {
		t.Fatal("write budget not configuration/revision bound")
	}
}

func TestSoftReservesUseActualFilesystemTotalWithoutEarlyTruncation(t *testing.T) {
	q, _ := softFixture(t)
	q.policy.ReserveBytes = 1
	q.policy.ReservePercent = 10
	q.policy.ReserveInodes = 1
	q.policy.ReserveInodePercent = 10
	space := fsSpace{199, 199, 199, 199}
	bytes, inodes := q.admissionReserves(space)
	if bytes != 20 || inodes != 20 || q.emergencyThreshold(space) != 10 {
		t.Fatal("percentage calculated after truncating filesystem total", bytes, inodes)
	}
}
