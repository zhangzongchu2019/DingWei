package m8

import (
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
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

func TestConsoleTaskBoardStatusStaysEmptyWithoutTaskCaseRecords(t *testing.T) {
	if !strings.Contains(consoleTaskBoardPageHTML, "assigneeCell(task,'reviewer'),taskCell(''),taskCell(acceptance.label") {
		t.Fatal("status column is no longer mechanically empty before task-case records exist")
	}
}

func TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel(t *testing.T) {
	t.Skip("K13 deferred: task case record model does not exist, so started state cannot be derived mechanically")
}
