package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func reserveMonitorFixture(t *testing.T, q *softQuota, stop func(context.Context)) *dockerOps {
	t.Helper()
	var mu sync.Mutex
	running := map[string]bool{"one": true, "two": true, "cid": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" || r.URL.Path == "/images/json" {
			w.Write([]byte(`[]`))
			return
		}
		var cid string
		if _, err := fmt.Sscanf(r.URL.Path, "/containers/%s", &cid); err != nil {
			t.Errorf("bad fixture request")
			return
		}
		for i, c := range cid {
			if c == '/' {
				cid = cid[:i]
				break
			}
		}
		id := "ag_one"
		if cid == "two" {
			id = "ag_two"
		}
		if r.Method == "POST" {
			if stop != nil {
				stop(r.Context())
			}
			mu.Lock()
			running[cid] = false
			mu.Unlock()
			w.Write([]byte(`{}`))
			return
		}
		mu.Lock()
		live := running[cid]
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"Id": cid, "State": map[string]bool{"Running": live}, "Config": map[string]any{"Labels": map[string]string{managedSeatLabel: q.nodeID, managedSeatIDLabel: id}}, "Mounts": []map[string]string{{"Source": seatDir(q.root, id), "Destination": "/work"}}})
	}))
	t.Cleanup(srv.Close)
	return &dockerOps{eng: newFakeEngine(srv.Client(), srv.URL), soft: q, policy: q.policy, seatRoot: q.root}
}
func waitReserve(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal(what)
	}
}
func TestSoftReserveRunsWhileMetadataWalkIsBlocked(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	q.policy.ReserveInterval = 5 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	q.usage = func(context.Context, string, string) (diskUsage, error) {
		once.Do(func() { close(entered) })
		<-release
		return diskUsage{1024, 1024}, nil
	}
	var low atomic.Bool
	q.disk = func(string) (fsSpace, error) {
		s := fsSpace{1 << 40, 1 << 40, 10000000, 10000000}
		if low.Load() {
			s.Free = 1
		}
		return s, nil
	}
	q.sampleNode()
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
	defer func() { cancel(); close(release); <-done }()
	waitReserve(t, entered, "metadata walk not entered")
	low.Store(true)
	waitReserve(t, stopped, "blocked scan suppressed emergency reserve stop")
	q.mu.Lock()
	hold := q.state.Seats["ag_one"].Hold
	q.mu.Unlock()
	if hold != "node_space_emergency" {
		t.Fatalf("missing emergency hold: %s", hold)
	}
	// release remains blocked here: passing requires genuinely independent reserve.
}
func TestSoftReserveRunsWhileEngineStopIsBlocked(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	reserveFixture(t, q, "ag_two")
	q.track("ag_one", "one")
	q.track("ag_two", "two")
	q.policy.ReserveInterval = 5 * time.Millisecond
	q.usage = func(_ context.Context, _ string, id string) (diskUsage, error) {
		if id == "ag_one" {
			return diskUsage{9 << 20, 9 << 20}, nil
		}
		return diskUsage{1024, 1024}, nil
	}
	var low atomic.Bool
	var ticks atomic.Int32
	q.disk = func(string) (fsSpace, error) {
		s := fsSpace{1 << 40, 1 << 40, 10000000, 10000000}
		if low.Load() {
			ticks.Add(1)
			s.Free = 1
		}
		return s, nil
	}
	q.sampleNode()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d := reserveMonitorFixture(t, q, func(context.Context) { once.Do(func() { close(entered) }); <-release })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.runSoft(ctx) }()
	defer func() { cancel(); close(release); <-done }()
	waitReserve(t, entered, "Engine stop not entered")
	low.Store(true)
	deadline := time.Now().Add(time.Second)
	for {
		q.mu.Lock()
		hold := q.state.Seats["ag_two"].Hold
		pending := q.state.Seats["ag_one"].StopState
		q.mu.Unlock()
		if hold == "node_space_emergency" && ticks.Load() >= 3 {
			if pending == "stopped" {
				t.Fatal("uncompleted Engine request falsely reported stopped")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("blocked Engine serialized reserve or state lock")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestSoftEmergencyThresholdBoundariesPersistentHoldAndNoDeletion(t *testing.T) {
	for _, dimension := range []string{"bytes"} {
		for _, offset := range []int64{-1, 0, 1} {
			t.Run(fmt.Sprintf("%s/%d", dimension, offset), func(t *testing.T) {
				q, p := softFixture(t)
				reserveFixture(t, q, "ag_one")
				q.track("ag_one", "cid")
				business := filepath.Join(seatDir(q.root, "ag_one"), "business")
				os.WriteFile(business, []byte("keep"), 0600)
				space := fsSpace{1 << 40, 1 << 40, 10000000, 10000000}
				bytes := q.emergencyThreshold(space)
				space.Free = bytes + offset
				q.disk = func(string) (fsSpace, error) { return space, nil }
				q.sampleNode()
				q.checkReserve()
				stops := 0
				q.enforce(context.Background(), func(context.Context, string, string) error {
					raw, err := os.ReadFile(q.path)
					if err != nil {
						t.Fatal(err)
					}
					var state softState
					if json.Unmarshal(raw, &state) != nil || state.Seats["ag_one"].Hold == "" {
						t.Fatal("stop before durable intent")
					}
					stops++
					return nil
				})
				held := offset < 0
				if (q.state.Seats["ag_one"].Hold != "") != held || (stops > 0) != held {
					t.Fatalf("emergency equality boundary hold=%s stops=%d", q.state.Seats["ag_one"].Hold, stops)
				}
				if q.commandGate(Command{Type: CmdStartSeat, Params: json.RawMessage(`{"seat_id":"ag_one"}`)}) == nil {
					t.Fatal("below ordinary R admitted writer")
				}
				if b, err := os.ReadFile(business); err != nil || string(b) != "keep" {
					t.Fatal("business removed/changed")
				}
				if held {
					q.close()
					restored, err := newSoftQuota(p, q.root, q.path, q.nodeID)
					if err != nil {
						t.Fatal(err)
					}
					defer restored.close()
					restored.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1 << 40, 10000000, 10000000}, nil }
					restored.sampleNode()
					restored.checkReserve()
					if restored.state.Seats["ag_one"].Hold == "" {
						t.Fatal("recovery/restart auto-cleared hold")
					}
				}
			})
		}
	}
}
func TestSoftEmergencyConfigAndUnknownSpaceFailClosed(t *testing.T) {
	p := DefaultNodePolicy()
	if p.EmergencyReservePercent != 50 {
		t.Fatal("R/2 default missing")
	}
	before := p.Revision()
	p.EmergencyReservePercent = 40
	if p.Validate() != nil || p.Revision() == before {
		t.Fatal("emergency config not revision-bound")
	}
	for _, value := range []string{"0", "100", "-1", "not-a-number"} {
		if _, err := ParseNodePolicy(func(k string) string {
			if k == "NODEAGENT_EMERGENCY_RESERVE_PERCENT" {
				return value
			}
			return ""
		}); err == nil {
			t.Fatal("invalid emergency accepted", value)
		}
	}
	got, err := ParseNodePolicy(func(k string) string {
		if k == "NODEAGENT_EMERGENCY_RESERVE_PERCENT" {
			return "40"
		}
		return ""
	})
	if err != nil || got.EmergencyReservePercent != 40 {
		t.Fatal("explicit emergency config ignored")
	}
}

func TestSoftReserveCanHoldDuringWriterWalkBeforeAnyStart(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	entered, release := make(chan struct{}), make(chan struct{})
	q.usage = func(context.Context, string, string) (diskUsage, error) {
		close(entered)
		<-release
		return diskUsage{1024, 1024}, nil
	}
	called := false
	result := make(chan error, 1)
	go func() {
		result <- q.start(context.Background(), "ag_one", func(context.Context) error { called = true; return nil })
	}()
	waitReserve(t, entered, "writer walk not entered")
	q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1, 10000000, 10000000}, nil }
	q.sampleNode()
	checked := make(chan struct{})
	go func() { q.checkReserve(); close(checked) }()
	select {
	case <-checked:
	case <-time.After(time.Second):
		close(release)
		<-result
		t.Fatal("writer walk retained state lock")
	}
	stopped := make(chan struct{}, 1)
	d := reserveMonitorFixture(t, q, func(context.Context) { stopped <- struct{}{} })
	enforced := make(chan struct{})
	go func() { q.enforce(context.Background(), d.stopOwned); close(enforced) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		close(release)
		<-result
		<-enforced
		t.Fatal("writer walk gate prevented stopping existing writer")
	}
	<-enforced
	close(release)
	if err := <-result; err == nil || called {
		t.Fatal("emergency hold bypassed before start")
	}
}

func TestSoftEmergencyCannotStopBeforeLateWriterStart(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	q.track("ag_one", "cid")
	q.policy.ReserveInterval = 5 * time.Millisecond
	var low, running atomic.Bool
	q.disk = func(string) (fsSpace, error) {
		s := fsSpace{1 << 40, 1 << 40, 10000000, 10000000}
		if low.Load() {
			s.Free = 1
		}
		return s, nil
	}
	q.sampleNode()
	var requests atomic.Int32
	stopped := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == "POST" {
			running.Store(false)
			select {
			case stopped <- struct{}{}:
			default:
			}
			w.Write([]byte(`{}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"Id": "cid", "State": map[string]bool{"Running": running.Load()}, "Config": map[string]any{"Labels": map[string]string{managedSeatLabel: q.nodeID, managedSeatIDLabel: "ag_one"}}, "Mounts": []map[string]string{{"Source": seatDir(q.root, "ag_one"), "Destination": "/work"}}})
	}))
	defer srv.Close()
	d := &dockerOps{eng: newFakeEngine(srv.Client(), srv.URL), soft: q, policy: q.policy, seatRoot: q.root}
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- q.start(context.Background(), "ag_one", func(context.Context) error { close(entered); <-release; running.Store(true); return nil })
	}()
	waitReserve(t, entered, "writer Engine call not entered")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.runSoft(ctx) }()
	released := false
	defer func() {
		cancel()
		if !released {
			close(release)
			<-result
		}
		<-done
	}()
	low.Store(true)
	deadline := time.Now().Add(time.Second)
	for {
		q.mu.Lock()
		held := q.state.Seats["ag_one"].Hold != ""
		q.mu.Unlock()
		if held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("in-flight Engine start retained reserve state lock")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	if requests.Load() != 0 {
		t.Fatal("stop inspected before late start could finish")
	}
	close(release)
	released = true
	if err := <-result; !errors.Is(err, ErrSoftQuota) {
		t.Fatal("late start falsely succeeded", err)
	}
	waitReserve(t, stopped, "held late start was not subsequently stopped")
	if running.Load() {
		t.Fatal("held writer left running")
	}
}

func TestSoftBlockedWriterDoesNotSerializeOtherSeatStops(t *testing.T) {
	q, _ := softFixture(t)
	reserveFixture(t, q, "ag_one")
	reserveFixture(t, q, "ag_two")
	q.track("ag_one", "one")
	q.track("ag_two", "two")
	q.policy.StopTimeout = 100 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- q.start(context.Background(), "ag_one", func(context.Context) error { close(entered); <-release; return nil })
	}()
	waitReserve(t, entered, "first writer did not enter")
	defer func() { close(release); <-result }()
	q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1, 10000000, 10000000}, nil }
	q.sampleNode()
	q.checkReserve()
	d := reserveMonitorFixture(t, q, nil)
	q.enforce(context.Background(), d.stopOwned)
	q.mu.Lock()
	first, second := q.state.Seats["ag_one"].StopState, q.state.Seats["ag_two"].StopState
	q.mu.Unlock()
	if first != "unknown" || second != "stopped" {
		t.Fatalf("one blocked writer serialized unrelated stop: first=%s second=%s", first, second)
	}
}
