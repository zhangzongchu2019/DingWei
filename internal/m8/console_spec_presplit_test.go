package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestConsoleSpecPresplitCallbackUsesAuthenticatedSessionIngress(t *testing.T) {
	source, err := os.ReadFile("hub.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	authority := strings.Index(text, `env.Meta[hubIngressMetaKey] = hubIngressAgent`)
	callback := strings.Index(text, `h.handleConsoleSpecPresplitResult(r.Context(), c, env)`)
	route := strings.Index(text, `h.RouteEnvelope(r.Context(), env)`)
	if authority < 0 || callback < authority || route < callback {
		t.Fatalf("presplit callback must consume the authenticated session client before generic routing: authority=%d callback=%d route=%d", authority, callback, route)
	}
}

func finalSpecForPresplit(t *testing.T, hub *Hub) model.ConsoleSpec {
	t.Helper()
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var spec model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	db := hub.Repo.(*store.SQLite)
	if err := db.TransferConsoleSpec(context.Background(), spec.ID, "owner-a", "丙", "review", hub.now()); err != nil {
		t.Fatal(err)
	}
	completeSpecGateForTest(t, db, spec.ID, "manager", hub.now())
	if err := db.FinalizeConsoleSpec(context.Background(), spec.ID, "manager", hub.now()); err != nil {
		t.Fatal(err)
	}
	return spec
}

func addPresplitAgent(t *testing.T, hub *Hub, id, key, session, owner string) model.ConsoleAgent {
	t.Helper()
	agent := model.ConsoleAgent{ID: id, KeyID: key, SessionName: session, OwnerPrincipal: owner, Active: true}
	if err := hub.Repo.(*store.SQLite).UpsertConsoleAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	if hub.sessionClients[key] == nil {
		hub.sessionClients[key] = map[string]*sessionClient{}
	}
	hub.sessionClients[key][session] = &sessionClient{keyID: key, sessionName: session}
	hub.mu.Unlock()
	return agent
}

func startPresplit(t *testing.T, hub *Hub, specID, agentID string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+specID+"/presplit", strings.NewReader(`{"agent_id":"`+agentID+`"}`))
	req.SetPathValue("specID", specID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpecPresplit)(rr, req)
	var body struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	return body.RequestID, rr
}

func presplitState(t *testing.T, hub *Hub, specID string) *model.ConsoleSpecPresplitRequest {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/console/specs/"+specID+"/presplit", nil)
	req.SetPathValue("specID", specID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionRead, hub.HandleConsoleSpecPresplit)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Request *model.ConsoleSpecPresplitRequest `json:"request"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Request
}

func TestConsoleSpecPresplitAgentIdentityTasksAndFirstResult(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	agent := addPresplitAgent(t, hub, "agent-a", "ka", "agent-a-2642", "owner-a")
	forged := addPresplitAgent(t, hub, "agent-b", "kb", "agent-b-2642", "owner-b")
	spec := finalSpecForPresplit(t, hub)
	var dispatched model.Envelope
	hub.specPresplitDispatch = func(_ context.Context, got model.ConsoleAgent, env model.Envelope) error {
		if got.ID != agent.ID {
			t.Fatalf("dispatch agent=%s", got.ID)
		}
		dispatched = env
		return nil
	}
	requestID, rr := startPresplit(t, hub, spec.ID, agent.ID)
	if rr.Code != http.StatusAccepted || requestID == "" || metaString(dispatched.Meta, "request_id") != requestID || !strings.Contains(dispatched.Body, "content_markdown") {
		t.Fatalf("start code=%d body=%s dispatched=%+v", rr.Code, rr.Body.String(), dispatched)
	}
	result := `{"request_id":"` + requestID + `","key_id":"spoof","session_name":"spoof","notes":"ignored","tasks":[{"title":"DB","content":"schema","definition_of_done":"migrated","estimated_hours":2,"depends_on_indexes":[]},{"title":"API","content":"handler","definition_of_done":"tested","estimated_hours":3,"depends_on_indexes":[0]}]}`
	if !hub.handleConsoleSpecPresplitResult(context.Background(), &sessionClient{keyID: forged.KeyID, sessionName: forged.SessionName}, model.Envelope{Meta: map[string]any{"type": specPresplitResultType}, Body: result}) {
		t.Fatal("forged callback not consumed")
	}
	if got := presplitState(t, hub, spec.ID); got == nil || got.Status != "pending" {
		t.Fatalf("forged callback changed request: %+v", got)
	}
	hub.handleConsoleSpecPresplitResult(context.Background(), &sessionClient{keyID: agent.KeyID, sessionName: agent.SessionName}, model.Envelope{Meta: map[string]any{"type": specPresplitResultType}, Body: result})
	staged, e := hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), spec.ID)
	if e != nil || len(staged) != 0 {
		t.Fatalf("R32 unconfirmed tasks=%v err=%v", staged, e)
	}
	c, e := hub.Repo.(*store.SQLite).ConsoleTaskCandidates(context.Background(), spec.ID, "owner-a")
	if e != nil || c == nil {
		t.Fatalf("candidate=%v err=%v", c, e)
	}
	if e = hub.Repo.(*store.SQLite).ConfirmConsoleTaskCandidates(context.Background(), requestID, "owner-a", c.Revision, c.Version, c.Tasks, func() error { return nil }, hub.now()); e != nil {
		t.Fatal(e)
	}
	tasks, err := hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), spec.ID)
	if err != nil || len(tasks) != 2 || tasks[0].ReviewedBy != "owner-a" || tasks[1].ReviewedBy != "owner-a" {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	// Same request_id is first-result-only and must not duplicate tasks.
	hub.handleConsoleSpecPresplitResult(context.Background(), &sessionClient{keyID: agent.KeyID, sessionName: agent.SessionName}, model.Envelope{Meta: map[string]any{"type": specPresplitResultType}, Body: result})
	tasks, _ = hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), spec.ID)
	if len(tasks) != 2 {
		t.Fatalf("duplicate callback tasks=%d", len(tasks))
	}
	var dependent, remaining model.ConsoleTaskProposal
	for _, task := range tasks {
		if task.Title == "API" {
			dependent = task
		} else {
			remaining = task
		}
	}
	db := hub.Repo.(*store.SQLite)
	if err := db.UpdateConsoleTaskProposalPlan(context.Background(), remaining.ID, "owner-a", false, "edited", "planned", 4, nil, ""); err != nil {
		t.Fatalf("creator edits presplit task: %v", err)
	}
	updated, err := db.ListConsoleTaskProposals(context.Background(), spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundEdited := false
	for _, task := range updated {
		if task.ID == remaining.ID && task.TaskContent == "edited" {
			foundEdited = true
		}
	}
	if !foundEdited {
		t.Fatalf("presplit task edit was a blind success: %+v", updated)
	}
	if err := db.DeleteConsoleSpecTask(context.Background(), dependent.ID, "owner-a"); err != nil {
		t.Fatalf("creator deletes presplit task: %v", err)
	}
	if err := db.CloseConsoleSpecTaskSplit(context.Background(), spec.ID, "owner-a", hub.now()); err != nil {
		t.Fatalf("close task split: %v", err)
	}
	if err := db.UpdateConsoleTaskProposalPlan(context.Background(), remaining.ID, "owner-a", false, "after close", "planned", 4, nil, `[]`); !errors.Is(err, store.ErrConsoleSpecPermission) {
		t.Fatalf("closed task edit err=%v", err)
	}
	if err := db.DeleteConsoleSpecTask(context.Background(), remaining.ID, "owner-a"); !errors.Is(err, store.ErrConsoleSpecPermission) {
		t.Fatalf("closed task delete err=%v", err)
	}
	manual := model.ConsoleTaskProposal{ID: "after-close", SpecID: spec.ID, Title: "late", TaskContent: "late", Status: "planned", EstimatedHours: 1, DependsOnJSON: `[]`, ReviewedBy: "owner-a", CreatedAt: hub.now()}
	if err := db.CreateConsoleTaskProposal(context.Background(), manual, "owner-a", false); !errors.Is(err, store.ErrConsoleSpecPermission) {
		t.Fatalf("closed task add err=%v", err)
	}
}

func TestConsoleSpecPresplitFailuresArePersistentAndNeverEmptySuccess(t *testing.T) {
	t.Run("dispatch no reply", func(t *testing.T) {
		hub := newConsoleSpecTestHub(t)
		agent := addPresplitAgent(t, hub, "agent-a", "ka", "agent-a-2642", "owner-a")
		spec := finalSpecForPresplit(t, hub)
		hub.specPresplitDispatch = func(context.Context, model.ConsoleAgent, model.Envelope) error { return errors.New("offline") }
		_, rr := startPresplit(t, hub, spec.ID, agent.ID)
		if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "预拆失败，可手工添加") {
			t.Fatalf("code=%d body=%q", rr.Code, rr.Body.String())
		}
		if state := presplitState(t, hub, spec.ID); state == nil || state.Status != "failed" {
			t.Fatalf("state=%+v", state)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		hub := newConsoleSpecTestHub(t)
		now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
		hub.NowFunc = func() time.Time { return now }
		hub.startedAt = now.Add(-time.Minute)
		hub.SpecPresplitTimeout = 10 * time.Millisecond
		agent := addPresplitAgent(t, hub, "agent-a", "ka", "agent-a-2642", "owner-a")
		spec := finalSpecForPresplit(t, hub)
		hub.specPresplitDispatch = func(context.Context, model.ConsoleAgent, model.Envelope) error { return nil }
		requestID, rr := startPresplit(t, hub, spec.ID, agent.ID)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("start=%d", rr.Code)
		}
		now = now.Add(time.Second)
		late := `{"request_id":"` + requestID + `","tasks":[{"title":"late","content":"must not persist","estimated_hours":1,"depends_on_indexes":[]}]}`
		hub.handleConsoleSpecPresplitResult(context.Background(), &sessionClient{keyID: agent.KeyID, sessionName: agent.SessionName}, model.Envelope{Meta: map[string]any{"type": specPresplitResultType}, Body: late})
		if state := presplitState(t, hub, spec.ID); state == nil || state.Status != "failed" || state.FailureReason != "timeout" {
			t.Fatalf("state=%+v", state)
		}
		if tasks, _ := hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), spec.ID); len(tasks) != 0 {
			t.Fatalf("late callback persisted tasks=%+v", tasks)
		}
	})
	t.Run("restart", func(t *testing.T) {
		hub := newConsoleSpecTestHub(t)
		now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
		hub.NowFunc = func() time.Time { return now }
		hub.startedAt = now.Add(-time.Minute)
		agent := addPresplitAgent(t, hub, "agent-a", "ka", "agent-a-2642", "owner-a")
		spec := finalSpecForPresplit(t, hub)
		hub.specPresplitDispatch = func(context.Context, model.ConsoleAgent, model.Envelope) error { return nil }
		_, _ = startPresplit(t, hub, spec.ID, agent.ID)
		now = now.Add(time.Second)
		hub.startedAt = now // simulate a restarted process after the pending row
		if state := presplitState(t, hub, spec.ID); state == nil || state.Status != "failed" || state.FailureReason != "restart" {
			t.Fatalf("state=%+v", state)
		}
	})
	t.Run("invalid format", func(t *testing.T) {
		hub := newConsoleSpecTestHub(t)
		agent := addPresplitAgent(t, hub, "agent-a", "ka", "agent-a-2642", "owner-a")
		spec := finalSpecForPresplit(t, hub)
		hub.specPresplitDispatch = func(context.Context, model.ConsoleAgent, model.Envelope) error { return nil }
		requestID, _ := startPresplit(t, hub, spec.ID, agent.ID)
		body := `{"request_id":"` + requestID + `","tasks":[]}`
		hub.handleConsoleSpecPresplitResult(context.Background(), &sessionClient{keyID: agent.KeyID, sessionName: agent.SessionName}, model.Envelope{Meta: map[string]any{"type": specPresplitResultType}, Body: body})
		state := presplitState(t, hub, spec.ID)
		tasks, _ := hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), spec.ID)
		if state == nil || state.Status != "failed" || len(tasks) != 0 {
			t.Fatalf("state=%+v tasks=%+v", state, tasks)
		}
	})
	t.Run("agent disabled after dispatch", func(t *testing.T) {
		hub := newConsoleSpecTestHub(t)
		agent := addPresplitAgent(t, hub, "agent-a", "ka", "agent-a-2642", "owner-a")
		spec := finalSpecForPresplit(t, hub)
		hub.specPresplitDispatch = func(context.Context, model.ConsoleAgent, model.Envelope) error { return nil }
		requestID, _ := startPresplit(t, hub, spec.ID, agent.ID)
		agent.Active = false
		if err := hub.Repo.(*store.SQLite).UpsertConsoleAgent(context.Background(), agent); err != nil {
			t.Fatal(err)
		}
		body := `{"request_id":"` + requestID + `","tasks":[{"title":"x","content":"x","estimated_hours":1,"depends_on_indexes":[]}]}`
		hub.handleConsoleSpecPresplitResult(context.Background(), &sessionClient{keyID: agent.KeyID, sessionName: agent.SessionName}, model.Envelope{Meta: map[string]any{"type": specPresplitResultType}, Body: body})
		if state := presplitState(t, hub, spec.ID); state == nil || state.Status != "failed" || state.FailureReason != "agent authorization changed" {
			t.Fatalf("state=%+v", state)
		}
		if tasks, _ := hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), spec.ID); len(tasks) != 0 {
			t.Fatalf("disabled callback tasks=%+v", tasks)
		}
	})
}

func TestValidatePresplitTasksRejectsMalformedGraph(t *testing.T) {
	valid := []presplitResultTask{{Title: "A", Content: "a"}, {Title: "B", Content: "b", DependsOnIndexes: []int{0}}}
	if tasks, ok := validatePresplitTasks(valid); !ok || len(tasks) != 2 || len(tasks[1].DependsOn) != 1 {
		t.Fatalf("valid tasks=%+v ok=%t", tasks, ok)
	}
	for name, input := range map[string][]presplitResultTask{
		"empty":        {},
		"negative":     {{Title: "A", Content: "a", EstimatedHours: -1}},
		"out of range": {{Title: "A", Content: "a", DependsOnIndexes: []int{1}}},
		"self":         {{Title: "A", Content: "a", DependsOnIndexes: []int{0}}},
		"cycle":        {{Title: "A", Content: "a", DependsOnIndexes: []int{1}}, {Title: "B", Content: "b", DependsOnIndexes: []int{0}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := validatePresplitTasks(input); ok {
				t.Fatal("malformed tasks accepted")
			}
		})
	}
}
