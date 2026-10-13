package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type taskBoardOwnerPayload struct {
	Specs  []model.ConsoleSpec         `json:"specs"`
	Tasks  []model.ConsoleTaskProposal `json:"tasks"`
	Total  int                         `json:"task_total"`
	Viewer string                      `json:"viewer_key"`
}

func boardOwnedFixtureSpec(t *testing.T, h *Hub, owner, title string, final bool) model.ConsoleSpec {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(validSpecBody), &body); err != nil {
		t.Fatal(err)
	}
	body["title"] = title
	raw, _ := json.Marshal(body)
	rr := callConsoleSpec(t, h, owner, consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", string(raw), h.HandleConsoleSpecs)
	if rr.Code != 201 {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
	var spec model.ConsoleSpec
	json.Unmarshal(rr.Body.Bytes(), &spec)
	if !final {
		return spec
	}
	db := h.Repo.(*store.SQLite)
	if err := db.TransferConsoleSpec(context.Background(), spec.ID, owner, "丙", "review", h.now()); err != nil {
		t.Fatal(err)
	}
	completeSpecGateForTest(t, db, spec.ID, "manager", h.now())
	if err := db.FinalizeConsoleSpec(context.Background(), spec.ID, "manager", h.now()); err != nil {
		t.Fatal(err)
	}
	spec.State = "final"
	spec.PrincipalID = owner
	task := model.ConsoleTaskProposal{ID: "owned-task-" + spec.ID, SpecID: spec.ID, Title: title, TaskContent: "synthetic work", Status: "planned", DependsOnJSON: "[]", AcceptanceJSON: "[]", ReviewedBy: owner, ReviewedAt: h.now(), CreatedAt: h.now()}
	if err := db.CreateConsoleTaskProposal(context.Background(), task, owner, false); err != nil {
		t.Fatal(err)
	}
	return spec
}
func boardOwnedRequest(t *testing.T, h *Hub, principal, query string) (int, taskBoardOwnerPayload) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/console/task-board?"+query, nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principal))
	rr := httptest.NewRecorder()
	h.HandleConsoleTaskBoard(rr, req)
	var p taskBoardOwnerPayload
	if rr.Code == 200 {
		if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
	}
	return rr.Code, p
}

// Model the documented ListConsoleSpecs(owner OR current_editor) projection,
// retaining real SQLite task/progress queries and the production owner filter.
type taskBoardEditorProjection struct {
	*store.SQLite
	extra model.ConsoleSpec
}

func (r taskBoardEditorProjection) ListConsoleSpecs(ctx context.Context, principal string, management bool) ([]model.ConsoleSpec, error) {
	items, err := r.SQLite.ListConsoleSpecs(ctx, principal, management)
	if principal == "owner-a" {
		items = append(items, r.extra)
	}
	return items, err
}
func TestConsoleTaskBoardOwnedOverviewAllSpecsAndAuthenticatedPrincipal(t *testing.T) {
	h := newConsoleSpecTestHub(t)
	ids := map[string]bool{}
	for i := 0; i < 9; i++ {
		s := boardOwnedFixtureSpec(t, h, "owner-a", fmt.Sprintf("owned-%d", i), true)
		ids[s.ID] = true
	}
	foreign := boardOwnedFixtureSpec(t, h, "owner-b", "foreign", true)
	draft := boardOwnedFixtureSpec(t, h, "owner-a", "draft", false)
	code, p := boardOwnedRequest(t, h, "owner-a", "principal_id=owner-b&owner=owner-b")
	if code != 200 || len(p.Specs) != 8 || len(p.Tasks) != 9 || p.Total != 9 || p.Viewer == "" {
		t.Fatalf("owned aggregate code=%d specs=%d tasks=%d total=%d", code, len(p.Specs), len(p.Tasks), p.Total)
	}
	for _, task := range p.Tasks {
		if !ids[task.SpecID] || task.Status != "planned" || task.TotalCases != 1 || task.PassedCases != 0 {
			t.Fatalf("not authentic owned task/progress: %+v", task)
		}
	}
	if code, _ = boardOwnedRequest(t, h, "owner-a", "spec_id="+foreign.ID); code != 404 {
		t.Fatalf("foreign selected=%d", code)
	}
	if code, _ = boardOwnedRequest(t, h, "owner-a", "spec_id="+draft.ID); code != 409 {
		t.Fatalf("draft selected=%d", code)
	}
	if code, _ = boardOwnedRequest(t, h, "", "principal_id=owner-a"); code != 401 {
		t.Fatalf("missing authenticated principal=%d", code)
	}
	code, other := boardOwnedRequest(t, h, "owner-b", "")
	if code != 200 || len(other.Tasks) != 1 || other.Tasks[0].SpecID != foreign.ID || other.Viewer == p.Viewer {
		t.Fatal("other principal scope/identity mixed")
	}
	db := h.Repo.(*store.SQLite)
	for _, agent := range []model.ConsoleAgent{{ID: "board-dev", KeyID: "bd", SessionName: "bd", OwnerPrincipal: "owner-a", Active: true}, {ID: "board-review", KeyID: "br", SessionName: "br", OwnerPrincipal: "owner-a", Active: true}} {
		if err := db.UpsertConsoleAgent(context.Background(), agent); err != nil {
			t.Fatal(err)
		}
	}
	var firstSpec string
	for id := range ids {
		firstSpec = id
		break
	}
	firstTask := "owned-task-" + firstSpec
	if err := db.UpdateConsoleSpecTaskAssignees(context.Background(), firstTask, "owner-a", "board-dev", "board-review"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConsoleSpecTaskAcceptances(context.Background(), firstTask, "owner-a", []string{"A1"}); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkConsoleSpecTaskCase(context.Background(), firstTask, "A1", "case-1", "board-dev", h.now()); err != nil {
		t.Fatal(err)
	}
	_, marked := boardOwnedRequest(t, h, "owner-a", "")
	for _, task := range marked.Tasks {
		if task.ID == firstTask && task.PassedCases != 0 {
			t.Fatal("unconfirmed mark counted as passed")
		}
	}
	if err := db.ConfirmConsoleSpecTaskCase(context.Background(), firstTask, "A1", "case-1", "board-review", h.now()); err != nil {
		t.Fatal(err)
	}
	_, confirmed := boardOwnedRequest(t, h, "owner-a", "")
	found := false
	for _, task := range confirmed.Tasks {
		if task.ID == firstTask {
			found = true
			if task.PassedCases != 1 || task.TotalCases != 1 || task.Status != "planned" {
				t.Fatal("authoritative case progress/state altered")
			}
		}
	}
	if !found {
		t.Fatal("case task missing")
	}
	if denied := callConsoleSpec(t, h, "no-platform-access", consolePlatformActionRead, http.MethodGet, "/api/console/task-board", "", h.HandleConsoleTaskBoard); denied.Code != 403 {
		t.Fatalf("read guard=%d", denied.Code)
	}
	foreign.CurrentEditorID = "owner-a"
	h.Repo = taskBoardEditorProjection{SQLite: db, extra: foreign}
	code, p = boardOwnedRequest(t, h, "owner-a", "")
	if code != 200 || len(p.Tasks) != 9 {
		t.Fatal("current-editor-only spec leaked into owned overview")
	}
	code, p = boardOwnedRequest(t, h, "owner-a", "spec_id="+foreign.ID)
	if code != 200 || len(p.Tasks) != 1 || p.Tasks[0].SpecID != foreign.ID {
		t.Fatal("explicit existing editor read scope lost")
	}
	taskID := "owned-task-" + foreign.ID
	assign := httptest.NewRequest(http.MethodPatch, "/api/console/task-board/tasks/"+taskID, strings.NewReader(`{"developer_agent_id":"","reviewer_agent_id":""}`))
	assign.SetPathValue("taskID", taskID)
	assign = assign.WithContext(context.WithValue(assign.Context(), consolePrincipalContextKey{}, "owner-a"))
	out := httptest.NewRecorder()
	h.HandleConsoleTaskBoardAssignment(out, assign)
	if out.Code != 403 {
		t.Fatalf("editor became owner-writer: %d %s", out.Code, out.Body.String())
	}
	t.Log("9 owned final specs aggregated before eight-spec pagination; other-user/editor/default/query spoof isolated; original final/write restrictions retained")
}
