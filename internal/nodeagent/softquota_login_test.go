package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Extend, rather than replace, the existing real login-flow Engine/Hub doubles.
// Inspect/stop use the exact specification sent by l.start to lfEngine.
func softLoginFixture(t *testing.T) (*loginOps, *softQuota, *lfEngine, *lfHub) {
	t.Helper()
	q, p := softFixture(t)
	f, h := newLFEngine(t, lfOutput), newLFHub(t)
	l := mkLoginOps(t, f, h)
	l.d.soft, l.d.policy, l.d.seatRoot = q, p, q.root
	var mu sync.Mutex
	running, removed := map[string]bool{}, map[string]bool{}
	base := f.srv.Config.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		cid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/json")
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/json") && r.URL.Path != "/containers/json" {
			f.mu.Lock()
			f.calls = append(f.calls, key)
			var spec map[string]any
			for _, v := range f.specs {
				labels := v["Labels"].(map[string]any)
				if "cid-login-"+labels[loginLabel].(string) == cid {
					spec = v
					break
				}
			}
			f.mu.Unlock()
			mu.Lock()
			gone, isRunning := removed[cid], running[cid]
			mu.Unlock()
			if spec == nil || gone {
				http.Error(w, "absent", 404)
				return
			}
			labels := spec["Labels"].(map[string]any)
			binds := spec["HostConfig"].(map[string]any)["Binds"].([]any)
			host, dest, _ := strings.Cut(binds[0].(string), ":")
			json.NewEncoder(w).Encode(map[string]any{"Id": cid, "Config": map[string]any{"Labels": labels}, "Mounts": []map[string]string{{"Source": host, "Destination": dest}}, "State": map[string]bool{"Running": isRunning}})
			return
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/stop") {
			f.mu.Lock()
			f.calls = append(f.calls, key)
			f.mu.Unlock()
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/stop")
			mu.Lock()
			running[id] = false
			mu.Unlock()
			f.letExit()
			w.Write([]byte(`{}`))
			return
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/start") {
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/start")
			mu.Lock()
			running[id] = true
			mu.Unlock()
		}
		if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/containers/") {
			id := strings.TrimPrefix(r.URL.Path, "/containers/")
			mu.Lock()
			removed[id] = true
			running[id] = false
			mu.Unlock()
		}
		base.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(f.letExit)
	l.d.eng = newFakeEngine(srv.Client(), srv.URL)
	t.Cleanup(func() {
		l.mu.Lock()
		sessions := make([]*loginSession, 0, len(l.sessions))
		for _, s := range l.sessions {
			sessions = append(sessions, s)
		}
		l.mu.Unlock()
		for _, s := range sessions {
			if s.cancel != nil {
				s.cancel()
			}
		}
		f.letExit()
		for _, s := range sessions {
			select {
			case <-s.done:
			case <-time.After(3 * time.Second):
				t.Error("login finish did not stop")
			}
		}
	})
	return l, q, f, h
}

func TestSoftLoginRealStartEmergencyRetainsDataAndBudget(t *testing.T) {
	for _, kind := range []string{"bytes", "inodes", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			l, q, f, h := softLoginFixture(t)
			if _, err := l.start(context.Background(), lfParams(l, lfID)); err != nil {
				t.Fatal(err)
			}
			key, cid := "login-"+lfID, "cid-login-"+lfID
			q.mu.Lock()
			s := q.state.Seats[key]
			q.mu.Unlock()
			if s == nil || s.WriterKind != "login" || s.Resources.cpuMillicores() != 250 || s.Resources.MemoryMB != 256 || s.Limit != 256<<20 || len(s.CIDs) != 1 || s.CIDs[0] != cid {
				t.Fatalf("unbudgeted/untracked login: %+v", s)
			}
			marker := filepath.Join(l.dir(lfID), "preserved-business")
			if err := os.WriteFile(marker, []byte("synthetic retained data"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(l.dir(lfID), ".codex", "auth.json"), []byte(lfCred), 0600); err != nil {
				t.Fatal(err)
			}
			l.mu.Lock()
			sess := l.sessions[lfID]
			l.mu.Unlock()
			switch kind {
			case "bytes":
				q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1, 10000000, 10000000}, nil }
				q.sampleNode()
			case "inodes":
				q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1 << 40, 10000000, 1}, nil }
				q.sampleNode()
			case "unknown":
				q.disk = func(string) (fsSpace, error) { return fsSpace{}, errors.New("fixture unknown") }
				q.sampleNode()
				q.mu.Lock()
				q.state.LastSpaceAt = q.now().Add(-q.policy.MaxAge - time.Second)
				q.mu.Unlock()
			}
			q.checkReserve()
			if _, err := l.start(context.Background(), lfParams(l, lfID)); err == nil {
				t.Fatal("held login replay accepted")
			}
			q.enforce(context.Background(), l.d.stopOwned)
			if f.count("POST /containers/"+cid+"/stop") != 1 {
				t.Fatalf("real login not stopped: %v", f.calls)
			}
			select {
			case <-sess.done:
			case <-time.After(3 * time.Second):
				t.Fatal("finish pending")
			}
			if len(h.uploads()) != 0 {
				t.Fatal("emergency login uploaded credential")
			}
			l.cleanupLeftovers(context.Background())
			if _, err := l.cancel(context.Background(), json.RawMessage(`{"login_id":"`+lfID+`"}`)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("held login data deleted", err)
			}
			if f.count("DELETE /containers/") != 0 || f.count("DELETE /networks/") != 0 {
				t.Fatal("held writer cleanup")
			}
			records, events, _ := q.report()
			if records[key].Hold == "" || records[key].StopState != "stopped" || records[key].WriterRoot != "" {
				t.Fatalf("report: %+v", records[key])
			}
			if len(events) == 0 || events[len(events)-1].WriterKind != "login" {
				t.Fatal("missing typed login audit")
			}
			q.mu.Lock()
			kept := q.state.Seats[key]
			q.mu.Unlock()
			if kept == nil || kept.Resources.MemoryMB != 256 {
				t.Fatal("held budget released")
			}
			raw, err := os.ReadFile(q.path)
			if err != nil {
				t.Fatal(err)
			}
			var state softState
			if json.Unmarshal(raw, &state) != nil || state.Seats[key].Hold == "" {
				t.Fatal("hold not persisted")
			}
		})
	}
}

func TestSoftLoginNormalFinishReleasesBudget(t *testing.T) {
	l, q, f, h := softLoginFixture(t)
	if _, err := l.start(context.Background(), lfParams(l, lfID)); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	sess := l.sessions[lfID]
	l.mu.Unlock()
	if err := os.WriteFile(filepath.Join(l.dir(lfID), ".codex", "auth.json"), []byte(lfCred), 0600); err != nil {
		t.Fatal(err)
	}
	f.letExit()
	select {
	case <-sess.done:
	case <-time.After(3 * time.Second):
		t.Fatal("finish pending")
	}
	if len(h.uploads()) != 1 || h.uploads()[0]["status"] != "ok" {
		t.Fatal("normal login flow lost")
	}
	if _, err := os.Stat(l.dir(lfID)); !os.IsNotExist(err) {
		t.Fatal("normal credential cleanup missing")
	}
	q.mu.Lock()
	remaining := len(q.state.Seats)
	q.mu.Unlock()
	if remaining != 0 {
		t.Fatal("normal budget leaked")
	}
}

func TestSoftLoginBudgetAdmissionBeforeDocker(t *testing.T) {
	for _, resource := range []string{"cpu", "memory", "disk", "inode"} {
		t.Run(resource, func(t *testing.T) {
			l, q, f, _ := softLoginFixture(t)
			switch resource {
			case "cpu":
				q.resources = func() (int64, int64, int64, error) { return q.policy.ReserveCPUMillicores + 249, 1 << 40, 1 << 40, nil }
			case "memory":
				q.resources = func() (int64, int64, int64, error) {
					return 64000, q.policy.ReserveMemoryBytes + (255 << 20), 1 << 40, nil
				}
			case "disk":
				q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1, 10000000, 10000000}, nil }
			case "inode":
				q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1 << 40, 10000000, 1}, nil }
			}
			q.sampleNode()
			if _, err := l.start(context.Background(), lfParams(l, lfID)); err == nil {
				t.Fatal("login admitted without budget")
			}
			if f.count("POST /containers/") != 0 || f.count("POST /networks/") != 0 {
				t.Fatal("side effect before budget")
			}
		})
	}
}

func TestSoftLoginConsumesSharedCPUAndMemory(t *testing.T) {
	l, q, f, _ := softLoginFixture(t)
	if _, err := l.start(context.Background(), lfParams(l, lfID)); err != nil {
		t.Fatal(err)
	}
	q.mu.Lock()
	q.sampledCPU = q.policy.ReserveCPUMillicores + 499
	q.mu.Unlock()
	if _, err := l.start(context.Background(), lfParams(l, lfID2)); err == nil {
		t.Fatal("second login ignored first CPU")
	}
	q.mu.Lock()
	q.sampledCPU = 64000
	q.sampledTotal = q.policy.ReserveMemoryBytes + (511 << 20)
	q.mu.Unlock()
	if err := q.reserve("ag_budget", seatResources{MemoryMB: 256, CPUShares: 256, DiskMB: 8}); err == nil {
		t.Fatal("seat ignored login memory")
	}
	if f.count("POST /containers/create") != 1 {
		t.Fatal("unexpected second create")
	}
}

func TestSoftLoginMeasurementOwnershipAndRestart(t *testing.T) {
	l, q, f, _ := softLoginFixture(t)
	if _, err := l.start(context.Background(), lfParams(l, lfID)); err != nil {
		t.Fatal(err)
	}
	key := "login-" + lfID
	q.measure(context.Background())
	q.mu.Lock()
	first := q.state.Seats[key].Usage
	q.mu.Unlock()
	if first.Allocated <= 0 {
		t.Fatal("login metadata measurement absent")
	}
	// A wrong-node object must never be stopped, even with a tracked CID.
	f.mu.Lock()
	labels := f.specs[0]["Labels"].(map[string]any)
	labels[managedSeatLabel] = "other-node"
	f.mu.Unlock()
	q.mu.Lock()
	q.holdLocked(key, "node_space_emergency")
	q.mu.Unlock()
	q.enforce(context.Background(), l.d.stopOwned)
	if f.count("POST /containers/cid-login-"+lfID+"/stop") != 0 {
		t.Fatal("wrong owner stopped")
	}
	f.mu.Lock()
	labels[managedSeatLabel] = q.nodeID
	f.mu.Unlock()
	q.enforce(context.Background(), l.d.stopOwned)
	waitFor(t, "held login finish", func() bool { l.mu.Lock(); defer l.mu.Unlock(); return len(l.sessions) == 0 })
	path, root, p, node := q.path, q.root, q.policy, q.nodeID
	q.close()
	restored, err := newSoftQuota(p, root, path, node)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	l.d.soft = restored
	l.cleanupLeftovers(context.Background())
	if _, err := os.Stat(l.dir(lfID)); err != nil {
		t.Fatal("restart deleted held data", err)
	}
	if _, err := l.start(context.Background(), lfParams(l, lfID)); err == nil {
		t.Fatal("restart resumed held login")
	}
	restored.mu.Lock()
	s := restored.state.Seats[key]
	restored.mu.Unlock()
	if s == nil || s.Hold == "" || s.WriterRoot != l.root {
		t.Fatal("private login state lost")
	}
}

func TestSoftLoginUnknownLeftoverFailsClosedAndPreserves(t *testing.T) {
	l, q, f, _ := softLoginFixture(t)
	dir := l.dir(lfID)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := chownForSeat(dir); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "synthetic-data")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	f.labeled = []containerListItem{{Labels: map[string]string{loginLabel: lfID}}}
	l.cleanupLeftovers(context.Background())
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("unrecorded data deleted")
	}
	if _, err := l.start(context.Background(), lfParams(l, lfID2)); err == nil {
		t.Fatal("unknown writer did not fence node")
	}
	if f.count("DELETE ") != 0 || f.count("POST /containers/") != 0 {
		t.Fatal("unknown writer mutated")
	}
	q.mu.Lock()
	broken := q.broken
	q.mu.Unlock()
	if !broken {
		t.Fatal("node not fenced")
	}
}

func TestSoftLoginStartAndEmergencyAreSerialized(t *testing.T) {
	l, q, f, _ := softLoginFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.onStart = func() { once.Do(func() { close(entered) }); <-release }
	startDone := make(chan error, 1)
	go func() { _, err := l.start(context.Background(), lfParams(l, lfID)); startDone <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("start not reached")
	}
	q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1, 10000000, 10000000}, nil }
	q.sampleNode()
	q.checkReserve()
	enforceDone := make(chan struct{})
	go func() { q.enforce(context.Background(), l.d.stopOwned); close(enforceDone) }()
	// Stop cannot race ahead of an already in-flight Engine start.
	select {
	case <-enforceDone:
		t.Error("stop did not wait for writer gate")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-startDone:
		if err == nil {
			t.Error("start during hold reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("start hung")
	}
	select {
	case <-enforceDone:
	case <-time.After(3 * time.Second):
		t.Fatal("stop hung")
	}
	if f.count("POST /containers/cid-login-"+lfID+"/stop") != 1 {
		t.Fatal("late-start writer not stopped")
	}
	if _, err := os.Stat(l.dir(lfID)); err != nil {
		t.Fatal("late-start held data removed")
	}
}

func TestSoftLoginEmergencyMustStopActualLoginCID(t *testing.T) {
	l, q, f, _ := softLoginFixture(t)
	if _, err := l.start(context.Background(), lfParams(l, lfID)); err != nil {
		t.Fatal(err)
	}
	q.disk = func(string) (fsSpace, error) { return fsSpace{1 << 40, 1, 10000000, 10000000}, nil }
	q.sampleNode()
	q.checkReserve()
	q.enforce(context.Background(), l.d.stopOwned)
	if f.count("POST /containers/cid-login-"+lfID+"/stop") != 1 {
		t.Fatal("Free=1 did not stop the actual loginCID")
	}
}

func TestSoftLoginPersistentPoolAndStateValidation(t *testing.T) {
	q, _ := softFixture(t)
	root := t.TempDir()
	if err := q.reserveLogin(lfID, root); err != nil {
		t.Fatal(err)
	}
	if err := q.reserveLogin(lfID2, root); err != nil {
		t.Fatal(err)
	}
	q.mu.Lock()
	q.holdLocked("login-"+lfID, "usage_unknown")
	q.mu.Unlock()
	if err := q.reserveLogin(strings.Repeat("a", 32), root); !errors.Is(err, ErrLoginBusy) {
		t.Fatalf("persistent pool cap: %v", err)
	}
	for _, root := range []string{q.root, filepath.Dir(q.path), filepath.Join(root, ".."), "relative"} {
		if err := q.reserveLogin(strings.Repeat("b", 32), root); err == nil {
			t.Fatal("unsafe root accepted")
		}
	}
	if validSoftWriter("ag_bad", &softSeat{WriterKind: "login", WriterRoot: root}) || validSoftWriter("ag_bad", &softSeat{WriterRoot: root}) || validSoftWriter("login-"+lfID, &softSeat{}) {
		t.Fatal("typed state validation bypass")
	}
	if q.reserve("login-"+strings.Repeat("c", 32), seatResources{MemoryMB: 256, CPUShares: 256, DiskMB: 256}) == nil {
		t.Fatal("seat stole login namespace")
	}
}

func TestSoftLoginBudgetCannotBeForgottenBySeatTeardown(t *testing.T) {
	l, q, _, _ := softLoginFixture(t)
	if _, err := l.start(context.Background(), lfParams(l, lfID)); err != nil {
		t.Fatal(err)
	}
	key := "login-" + lfID
	if err := q.forget(key); err == nil {
		t.Fatal("seat teardown erased a running login writer")
	}
	q.mu.Lock()
	writer := q.state.Seats[key]
	q.mu.Unlock()
	if writer == nil {
		t.Fatal("login budget lost")
	}
	if err := q.finishLogin(key, writer); err == nil {
		t.Fatal("running login retired before Engine removal")
	}
	q.mu.Lock()
	q.holdLocked(key, "usage_unknown")
	q.mu.Unlock()
	if err := q.forget(key); err == nil {
		t.Fatal("seat teardown erased a held login")
	}
}
