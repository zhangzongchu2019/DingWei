package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestConsoleTaskBoardUsesIndependentEightAndTwentyItemPages(t *testing.T) {
	specs := make([]model.ConsoleSpec, 9)
	for i := range specs {
		specs[i].ID = "spec"
	}
	tasks := make([]model.ConsoleTaskProposal, 21)
	for i := range tasks {
		tasks[i].ID = "task"
	}
	firstSpecs, specPages := pageConsoleSpecs(specs, 1, consoleTaskBoardSpecPageSize)
	secondSpecs, _ := pageConsoleSpecs(specs, 2, consoleTaskBoardSpecPageSize)
	firstTasks, taskPages := pageConsoleTasks(tasks, 1, consoleTaskBoardTaskPageSize)
	secondTasks, _ := pageConsoleTasks(tasks, 2, consoleTaskBoardTaskPageSize)
	if len(firstSpecs) != 8 || len(secondSpecs) != 1 || specPages != 2 {
		t.Fatalf("spec pagination first=%d second=%d pages=%d", len(firstSpecs), len(secondSpecs), specPages)
	}
	if len(firstTasks) != 20 || len(secondTasks) != 1 || taskPages != 2 {
		t.Fatalf("task pagination first=%d second=%d pages=%d", len(firstTasks), len(secondTasks), taskPages)
	}
}

func TestConsoleTaskBoardFiveColumnsAndNoInventedPrincipalPlaceholder(t *testing.T) {
	taskTable := consoleTaskBoardPageHTML[strings.Index(consoleTaskBoardPageHTML, `<table id=task-table>`):]
	taskTable = taskTable[:strings.Index(taskTable, `</table>`)]
	wantHeadings := "<th>任务文本</th><th>研发</th><th>验收</th><th>状态</th><th>验收标准</th>"
	if !strings.Contains(taskTable, wantHeadings) {
		t.Fatalf("task headings are not in sealed order: %s", taskTable)
	}
	for _, heading := range []string{"任务文本", "研发", "验收", "状态", "验收标准"} {
		if strings.Count(taskTable, "<th>"+heading+"</th>") != 1 {
			t.Fatalf("heading %q count=%d", heading, strings.Count(taskTable, "<th>"+heading+"</th>"))
		}
	}
	if strings.Contains(consoleTaskBoardPageHTML, "未知") {
		t.Fatal("task board must leave unavailable values empty")
	}
}

func TestConsoleTaskBoardStatusNeverShowsZeroOverZero(t *testing.T) {
	if !strings.Contains(consoleTaskBoardPageHTML, "String(task.passed_cases||0)+'/'+String(task.total_cases||1)") {
		t.Fatal("K24 status must use the 0/1 fallback instead of rendering 0/0")
	}
}

func TestConsoleTaskBoardListsAndOpensOnlyFinalSpecs(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	create := func(title string) model.ConsoleSpec {
		var body map[string]any
		if err := json.Unmarshal([]byte(validSpecBody), &body); err != nil {
			t.Fatal(err)
		}
		body["title"] = title
		raw, _ := json.Marshal(body)
		rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", string(raw), hub.HandleConsoleSpecs)
		if rr.Code != http.StatusCreated {
			t.Fatalf("create %s code=%d body=%s", title, rr.Code, rr.Body.String())
		}
		var spec model.ConsoleSpec
		if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
			t.Fatal(err)
		}
		return spec
	}
	draft := create("尚未审结")
	final := create("已经审结")
	db := hub.Repo.(*store.SQLite)
	if err := db.TransferConsoleSpec(context.Background(), final.ID, "owner-a", "丙", "review", hub.now()); err != nil {
		t.Fatal(err)
	}
	completeSpecGateForTest(t, db, final.ID, "manager", hub.now())
	if err := db.FinalizeConsoleSpec(context.Background(), final.ID, "manager", hub.now()); err != nil {
		t.Fatal(err)
	}

	request := func(rawQuery string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/console/task-board?"+rawQuery, nil)
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
		rr := httptest.NewRecorder()
		hub.HandleConsoleTaskBoard(rr, req)
		return rr
	}
	listed := request("spec_page=1&task_page=1")
	if listed.Code != http.StatusOK {
		t.Fatalf("list code=%d body=%s", listed.Code, listed.Body.String())
	}
	var payload struct {
		Specs []model.ConsoleSpec `json:"specs"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Specs) != 1 || payload.Specs[0].ID != final.ID {
		t.Fatalf("任务管理只应列已审结 spec: got=%+v draft=%s final=%s", payload.Specs, draft.ID, final.ID)
	}
	opened := request("spec_page=1&task_page=1&spec_id=" + final.ID)
	if opened.Code != http.StatusOK {
		t.Fatalf("final spec cannot enter task board: code=%d body=%s", opened.Code, opened.Body.String())
	}
	blocked := request("spec_page=1&task_page=1&spec_id=" + draft.ID)
	want := "该 spec 尚未审结(当前状态:editing),审结后才能进入任务管理"
	if blocked.Code != http.StatusConflict || strings.TrimSpace(blocked.Body.String()) != want {
		t.Fatalf("unfinalized direct access code=%d body=%q want 409 %q", blocked.Code, strings.TrimSpace(blocked.Body.String()), want)
	}
	t.Logf("final listed/opened; draft hidden; direct draft: %d %s", blocked.Code, want)
}

func TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel(t *testing.T) {
	t.Skip("K13 deferred: task case record model does not exist, so started state cannot be derived mechanically")
}
