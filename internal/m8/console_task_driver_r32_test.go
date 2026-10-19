package m8

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type r32LegacyDriverRepository struct {
	*store.SQLite
	afterList func()
	stateRead func(int, store.TaskFlowState, error) (store.TaskFlowState, error)
	reads     int
}

func (r *r32LegacyDriverRepository) ListConsoleTasksForAgent(ctx context.Context, id string) ([]model.ConsoleTaskProposal, error) {
	tasks, err := r.SQLite.ListConsoleTasksForAgent(ctx, id)
	if err == nil && r.afterList != nil {
		r.afterList()
	}
	return tasks, err
}
func (r *r32LegacyDriverRepository) ConsoleTaskFlowState(ctx context.Context, id string) (store.TaskFlowState, error) {
	state, err := r.SQLite.ConsoleTaskFlowState(ctx, id)
	r.reads++
	if r.stateRead != nil {
		return r.stateRead(r.reads, state, err)
	}
	return state, err
}
func r32LegacyDriverFixture(t *testing.T) (*shimEnv, store.TaskFlowState, string, string) {
	t.Helper()
	e, _, state := r32FlowEnv(t)
	tasks, err := e.db.ListConsoleTasksForAgent(e.ctx, state.Developer)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("fixture tasks=%v err=%v", tasks, err)
	}
	task := tasks[0]
	rev, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", task.SpecID, task.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.db.StartConsoleTaskDriver(e.ctx, model.ConsoleTaskDriverStart{SpecID: task.SpecID, Revision: task.Revision, ContentSHA256: rev.ContentSHA256, RepositoryName: "dingwei", BranchName: "r32", Schedule: "test", TaskEndpoint: "/api/agent/console/task-driver", TokenInstructions: "Console issues capability", AnnouncedBy: "zzc", AnnouncedAt: e.hub.now()}); err != nil {
		t.Fatal(err)
	}
	e.srv.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agents/{sessionName}/capability", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleCapability))
	mux.HandleFunc("GET /api/agent/console/task-driver/specs/{specID}/{revision}/tasks", e.hub.RequireConsoleSpecAgentCapability(func(*http.Request) string { return ConsoleAgentCapabilityTasks }, e.hub.HandleAgentConsoleTasks))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	status, capability := e.issue(e.tokZZC, "Kimi.zzc", ConsoleAgentCapabilityTasks)
	if status != 200 {
		t.Fatalf("issue=%d", status)
	}
	return e, state, "/api/agent/console/task-driver/specs/" + task.SpecID + "/1/tasks", capability
}
func TestR32LegacyTaskDriverStateReadFailureCloses(t *testing.T) {
	for _, scenario := range []struct {
		name string
		at   int
		err  error
	}{{"initial-storage", 1, errors.New("fixture state read unavailable")}, {"locked-storage", 2, errors.New("fixture locked read unavailable")}, {"missing-row", 1, store.ErrTaskFlowPermission}} {
		t.Run(scenario.name, func(t *testing.T) {
			e, _, path, capability := r32LegacyDriverFixture(t)
			wrapper := &r32LegacyDriverRepository{SQLite: e.db}
			wrapper.stateRead = func(n int, s store.TaskFlowState, err error) (store.TaskFlowState, error) {
				if n == scenario.at {
					return store.TaskFlowState{}, scenario.err
				}
				return s, err
			}
			e.hub.Repo = wrapper
			status, body := e.do("GET", path, "", e.zKimiCred, capability, "")
			if status == 200 || strings.Contains(body, `"tasks"`) || strings.Contains(body, "implement") || wrapper.reads < scenario.at {
				t.Fatalf("LEGACY_DRIVER_STATE_READ_FAILED_OPEN: status=%d reads=%d body=%s", status, wrapper.reads, body)
			}
		})
	}
}

// HTTP credentials/capability sessions remain the original live fixture;
// task authorization alone uses real PostgreSQL group/Agent locks.
type r32LegacyPGTaskStore struct {
	identity.Store
	identity.TaskAuthorizationStore
}

func TestR32LegacyTaskDriverReassignmentBetweenReads(t *testing.T) {
	for _, phase := range []string{"after-list", "after-state-before-lock"} {
		t.Run(phase, func(t *testing.T) {
			e, state, path, capability := r32LegacyDriverFixture(t)
			peer, err := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
			if err != nil {
				t.Fatal(err)
			}
			if err = e.svc.JoinGroup(e.ctx, "lbl", state.GroupCode, peer.ID); err != nil {
				t.Fatal(err)
			}
			if err = e.hub.syncNewPlatformUser(e.ctx, "lbl", nil); err != nil {
				t.Fatal(err)
			}
			originalIdentity := e.svc.Store
			pg := r32UsePGIdentity(t, e)
			e.svc.Store = r32LegacyPGTaskStore{originalIdentity, pg}
			ready, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			resume := func() { releaseOnce.Do(func() { close(release) }) }
			defer resume()
			pause := func() { close(ready); <-release }
			wrapper := &r32LegacyDriverRepository{SQLite: e.db}
			if phase == "after-list" {
				wrapper.afterList = pause
			} else {
				wrapper.stateRead = func(n int, s store.TaskFlowState, err error) (store.TaskFlowState, error) {
					if n == 1 {
						pause()
					}
					return s, err
				}
			}
			e.hub.Repo = wrapper
			type response struct {
				status int
				body   string
			}
			result := make(chan response, 1)
			go func() {
				status, body := e.do("GET", path, "", e.zKimiCred, capability, "")
				result <- response{status, body}
			}()
			select {
			case <-ready:
			case got := <-result:
				t.Fatalf("request bypassed race barrier: %d %s", got.status, got.body)
			case <-time.After(5 * time.Second):
				t.Fatal("request never reached race barrier")
			}
			changed, err := e.db.AssignConsoleTaskFlow(e.ctx, state.ID, "zzc", state.GroupCode, peer.ID, state.Reviewer, state.Version, func() error { return nil }, e.hub.now())
			if err != nil {
				t.Fatal(err)
			}
			if changed.Developer == state.Developer {
				t.Fatal("fixture did not revoke actor assignment")
			}
			resume()
			select {
			case got := <-result:
				if got.status == 200 || strings.Contains(got.body, `"tasks"`) || strings.Contains(got.body, "implement") {
					t.Fatalf("LEGACY_DRIVER_REASSIGNMENT_LEAKED: phase=%s status=%d body=%s", phase, got.status, got.body)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request stuck after reassignment")
			}
		})
	}
}
func TestR32LegacyTaskDriverUnchangedAndGrouplessControls(t *testing.T) {
	e, _, path, capability := r32LegacyDriverFixture(t)
	if status, body := e.do("GET", path, "", e.zKimiCred, capability, ""); status != 200 || !strings.Contains(body, `"proposal_id":"r32-task"`) {
		t.Fatalf("valid R32 task rejected: %d %s", status, body)
	}
	hub, spec, agents := taskDriverHTTPFixture(t)
	rr := callAgentTasks(t, hub, spec, agents["a"], "")
	if rr.Code != 200 || len(taskIDsFromResponse(t, rr)) != 1 {
		t.Fatalf("LEGACY_GROUPLESS_TASK_CHANGED: %d %s", rr.Code, rr.Body.String())
	}
}

// ListConsoleTaskFlow performs separate task-selection and state reads. Model
// their legal interleaving: selection sees the old actor, state sees new roles.
type r32ReassignedListRepository struct {
	*store.SQLite
	selected chan struct{}
	resume   chan struct{}
}

func (r *r32ReassignedListRepository) ListConsoleTaskFlow(ctx context.Context, actor string) ([]store.TaskFlowItem, error) {
	items, err := r.SQLite.ListConsoleTaskFlow(ctx, actor)
	if err != nil {
		return nil, err
	}
	close(r.selected)
	select {
	case <-r.resume:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	for i := range items {
		state, e := r.SQLite.ConsoleTaskFlowState(ctx, items[i].FlowState.ID)
		if e != nil {
			return nil, e
		}
		items[i].FlowState = state
		items[i].Version = state.Version
	}
	return items, nil
}
func TestR32MainTaskListRechecksCurrentActorAfterReassignment(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	peer, err := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.JoinGroup(e.ctx, "lbl", state.GroupCode, peer.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.hub.syncNewPlatformUser(e.ctx, "lbl", nil); err != nil {
		t.Fatal(err)
	}
	selected, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	defer unblock()
	e.hub.Repo = &r32ReassignedListRepository{e.db, selected, resume}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- r32HTTP(e, "GET", "", e.zKimiCred, "") }()
	select {
	case <-selected:
	case <-time.After(5 * time.Second):
		t.Fatal("list never reached selection barrier")
	}
	if _, err = e.db.AssignConsoleTaskFlow(e.ctx, state.ID, "zzc", state.GroupCode, peer.ID, state.Reviewer, state.Version, func() error { return nil }, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case rr := <-result:
		if rr.Code != 200 || strings.Contains(rr.Body.String(), "implement") || strings.Contains(rr.Body.String(), `"task_id":"r32-task"`) {
			t.Fatalf("MAIN_LIST_CURRENT_ACTOR_NOT_RECHECKED: %d %s", rr.Code, rr.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("list stuck after reassignment")
	}
}

type r32ListStateFailureRepository struct{ *store.SQLite }

func (r r32ListStateFailureRepository) ListConsoleTaskFlow(context.Context, string) ([]store.TaskFlowItem, error) {
	return nil, errors.New("fixture projection state read unavailable")
}
func TestR32MainTaskListStateReadFailureCloses(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	e.hub.Repo = r32ListStateFailureRepository{e.db}
	rr := r32HTTP(e, "GET", "", e.zKimiCred, "")
	if rr.Code != 503 || strings.Contains(rr.Body.String(), `"tasks"`) || strings.Contains(rr.Body.String(), "implement") {
		t.Fatalf("MAIN_LIST_STATE_READ_FAILED_OPEN: %d %s", rr.Code, rr.Body.String())
	}
}
