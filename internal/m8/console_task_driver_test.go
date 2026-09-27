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

func taskDriverHTTPFixture(t *testing.T) (*Hub, string, map[string]string) {
	t.Helper()
	hub := newConsoleSpecTestHub(t)
	ctx := context.Background()
	db := hub.Repo.(*store.SQLite)
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var spec model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if err := db.TransferConsoleSpec(ctx, spec.ID, "owner-a", "丙", "review", hub.now()); err != nil {
		t.Fatal(err)
	}
	completeSpecGateForTest(t, db, spec.ID, "manager", hub.now())
	if err := db.FinalizeConsoleSpec(ctx, spec.ID, "manager", hub.now()); err != nil {
		t.Fatal(err)
	}
	rev, err := db.ConsoleSpecRevision(ctx, "owner-a", spec.ID, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	agents := map[string]string{"a": "driver-a", "b": "driver-b", "c": "driver-c", "d": "driver-d", "empty": "driver-empty"}
	for k, id := range agents {
		key, session := "key-"+k, "session-"+k
		if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: id, KeyID: key, SessionName: session, OwnerPrincipal: "owner-a", Active: true}); err != nil {
			t.Fatalf("upsert agent %s: %v", id, err)
		}
		hub.sessionClients[key] = map[string]*sessionClient{session: {keyID: key, sessionName: session}}
	}
	for i := 1; i <= 2; i++ {
		id := fmt.Sprintf("driver-http-task-%d", i)
		p := model.ConsoleTaskProposal{ID: id, SpecID: spec.ID, Revision: 1, ContentSHA256: rev.ContentSHA256, Title: id, TaskContent: "work", Status: "planned", DependsOnJSON: "[]", AcceptanceJSON: "[]", InputAnchorsJSON: "[]", DefinitionOfDone: "done", ReviewedBy: "owner-a", ReviewedAt: hub.now(), CreatedAt: hub.now()}
		if err := db.CreateConsoleTaskProposal(ctx, p, "owner-a", false); err != nil {
			t.Fatalf("create task %s: %v", id, err)
		}
		dev, review := agents["a"], agents["c"]
		if i == 2 {
			dev, review = agents["b"], agents["d"]
		}
		if err := db.UpdateConsoleSpecTaskAssignees(ctx, id, "owner-a", dev, review); err != nil {
			t.Fatalf("assign task %s: %v", id, err)
		}
	}
	if err := db.StartConsoleTaskDriver(ctx, model.ConsoleTaskDriverStart{SpecID: spec.ID, Revision: 1, ContentSHA256: rev.ContentSHA256, RepositoryName: "dingwei", BranchName: "feature", Schedule: "2026-09-30", TaskEndpoint: "/api/agent/console/task-driver", TokenInstructions: "由控制台签发 capability", AnnouncedBy: "owner-a", AnnouncedAt: hub.now()}); err != nil {
		t.Fatalf("start task driver: %v", err)
	}
	return hub, spec.ID, agents
}

func callAgentTasks(t *testing.T, hub *Hub, specID, actor, claim string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/agent/console/task-driver/specs/"+specID+"/1/tasks?agent_id="+claim, nil)
	req.SetPathValue("specID", specID)
	req.SetPathValue("revision", "1")
	req = req.WithContext(context.WithValue(req.Context(), consoleSpecActorContextKey{}, consoleSpecActor{ID: actor, Kind: "agent"}))
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr := httptest.NewRecorder()
	hub.HandleAgentConsoleTasks(rr, req)
	return rr
}

func taskIDsFromResponse(t *testing.T, rr *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Tasks   []model.ConsoleTaskProposal `json:"tasks"`
		Status  string                      `json:"status"`
		Message string                      `json:"message"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(body.Tasks))
	for i := range body.Tasks {
		ids[i] = body.Tasks[i].ID
	}
	return ids
}

func TestAgentTaskDriverUsesTokenActorAndPersistsPauseReport(t *testing.T) {
	hub, specID, agents := taskDriverHTTPFixture(t)
	a := callAgentTasks(t, hub, specID, agents["a"], agents["b"])
	aWithoutClaim := callAgentTasks(t, hub, specID, agents["a"], "")
	b := callAgentTasks(t, hub, specID, agents["b"], "")
	if a.Code != 200 || aWithoutClaim.Code != 200 || b.Code != 200 {
		t.Fatalf("C3 task calls a=%d %s b=%d %s", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
	aIDs, aUnclaimedIDs, bIDs := taskIDsFromResponse(t, a), taskIDsFromResponse(t, aWithoutClaim), taskIDsFromResponse(t, b)
	if len(aUnclaimedIDs) != 1 || len(bIDs) != 1 || aUnclaimedIDs[0] == bIDs[0] {
		t.Fatalf("C3 token change did not change sets: a=%v b=%v", aUnclaimedIDs, bIDs)
	}
	if len(aUnclaimedIDs) != 1 || aIDs[0] != aUnclaimedIDs[0] {
		t.Fatalf("C4 request claim changed token-selected set: claimed=%v unclaimed=%v", aIDs, aUnclaimedIDs)
	}
	empty := callAgentTasks(t, hub, specID, agents["empty"], agents["a"])
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), `"status":"paused"`) || !strings.Contains(empty.Body.String(), "本席暂停") {
		t.Fatalf("C6 normal empty did not report pause: code=%d body=%s", empty.Code, empty.Body.String())
	}
	reported, err := hub.Repo.(*store.SQLite).ConsoleTaskPauseReported(context.Background(), specID, 1, agents["empty"])
	if err != nil || !reported {
		t.Fatalf("C6 pause did not reach durable owner-visible record: reported=%v err=%v", reported, err)
	}
	reports, err := hub.Repo.(*store.SQLite).ListConsoleTaskPauseReports(context.Background(), specID, 1)
	if err != nil || len(reports) != 1 || reports[0].AgentID != agents["empty"] {
		t.Fatalf("C6 owner report view=%+v err=%v", reports, err)
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/api/console/task-driver/specs/"+specID+"/1", nil)
	statusReq.SetPathValue("specID", specID)
	statusReq.SetPathValue("revision", "1")
	statusReq = statusReq.WithContext(context.WithValue(statusReq.Context(), consolePrincipalContextKey{}, "owner-a"))
	statusRR := httptest.NewRecorder()
	hub.HandleConsoleTaskDriverStatus(statusRR, statusReq)
	if statusRR.Code != 200 || !strings.Contains(statusRR.Body.String(), agents["empty"]) {
		t.Fatalf("C6 pause did not reach owner status endpoint: code=%d body=%s", statusRR.Code, statusRR.Body.String())
	}
}

func TestAgentTaskDriverFailureIsNotPause(t *testing.T) {
	hub, specID, agents := taskDriverHTTPFixture(t)
	if err := hub.Repo.(*store.SQLite).Close(); err != nil {
		t.Fatal(err)
	}
	rr := callAgentTasks(t, hub, specID, agents["a"], agents["b"])
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "取不到任务清单") || strings.Contains(rr.Body.String(), "本席暂停") {
		t.Fatalf("C5 failure collapsed into empty: code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestTaskStartRejectsSecretsAndIncompleteSevenPartMessage(t *testing.T) {
	if !taskStartContainsSecret(consoleTaskStartRequest{TokenInstructions: "Authorization: Bearer abcdefghijklmnop"}) {
		t.Fatal("C2 bearer token was not detected")
	}
	if taskStartContainsSecret(consoleTaskStartRequest{TokenInstructions: "从控制台签发短期 capability，不在正文放令牌"}) {
		t.Fatal("C2 safe token instructions rejected")
	}
}

func TestConsoleTaskBoardExplainsMissingReviewerInsteadOfShowingOnlyProgress(t *testing.T) {
	if !strings.Contains(consoleTaskBoardPageHTML, "缺少验收 Agent，任务无法完成") || !strings.Contains(consoleTaskBoardPageHTML, "assignment-problem") {
		t.Fatal("P2 missing reviewer is indistinguishable from ordinary 0/N progress")
	}
}

func TestConsoleTaskBoardSubmitsAssigneePairTogether(t *testing.T) {
	if strings.Contains(consoleTaskBoardPageHTML, "select.onchange=function(){task[idKey]=select.value;saveAssignment(task)") || !strings.Contains(consoleTaskBoardPageHTML, "task.assignment_dirty=true") || !strings.Contains(consoleTaskBoardPageHTML, "await saveAssignments()") {
		t.Fatal("P1 UI must stage both roles and submit the pair together at 编辑完成")
	}
}

func TestConsoleTaskBoardAssignmentExplainsPairAndDistinctViolations(t *testing.T) {
	hub, _, agents := taskDriverHTTPFixture(t)
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/console/task-board/tasks/driver-http-task-1", strings.NewReader(body))
		req.SetPathValue("taskID", "driver-http-task-1")
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
		rr := httptest.NewRecorder()
		hub.HandleConsoleTaskBoardAssignment(rr, req)
		return rr
	}
	paired := call(`{"developer_agent_id":"` + agents["a"] + `","reviewer_agent_id":""}`)
	if paired.Code != http.StatusConflict || !strings.Contains(paired.Body.String(), "成对指定") {
		t.Fatalf("P1 missing reviewer response code=%d body=%s", paired.Code, paired.Body.String())
	}
	same := call(`{"developer_agent_id":"` + agents["a"] + `","reviewer_agent_id":"` + agents["a"] + `"}`)
	if same.Code != http.StatusConflict || !strings.Contains(same.Body.String(), "不同 Agent") {
		t.Fatalf("P1 same Agent response code=%d body=%s", same.Code, same.Body.String())
	}
}
