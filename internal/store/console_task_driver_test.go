package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func taskDriverFixture(t *testing.T, specID string) (*SQLite, model.ConsoleTaskDriverStart, string, string) {
	t.Helper()
	ctx := context.Background()
	db, now := lineageFixture(t, specID)
	for _, a := range []model.ConsoleAgent{{ID: "driver-dev", KeyID: "dev-key", SessionName: "dev", OwnerPrincipal: "owner-a", Active: true}, {ID: "driver-review", KeyID: "review-key", SessionName: "review", OwnerPrincipal: "owner-a", Active: true}} {
		if err := db.UpsertConsoleAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	createLineageTask(t, db, now, specID, "driver-task", "done")
	if err := db.SetConsoleSpecTaskAcceptances(ctx, "driver-task", "owner-a", []string{"A1"}); err != nil {
		t.Fatal(err)
	}
	var sha string
	if err := db.db.QueryRowContext(ctx, `SELECT content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=1`, specID).Scan(&sha); err != nil {
		t.Fatal(err)
	}
	start := model.ConsoleTaskDriverStart{SpecID: specID, Revision: 1, ContentSHA256: sha, RepositoryName: "dingwei", BranchName: "feature", Schedule: "2026-09-30", TaskEndpoint: "/api/agent/console/task-driver", TokenInstructions: "由业主在控制台签发五分钟 capability", AnnouncedBy: "owner-a", AnnouncedAt: now}
	return db, start, "driver-dev", "driver-review"
}

func TestConsoleTaskAssignmentsMustBePairedAndDifferent(t *testing.T) {
	db, _, dev, review := taskDriverFixture(t, "driver-pair")
	ctx := context.Background()
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "driver-task", "owner-a", dev, ""); !errors.Is(err, ErrConsoleTaskAssigneePair) {
		t.Fatalf("P1 only developer err=%v want paired rejection", err)
	}
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "driver-task", "owner-a", dev, dev); !errors.Is(err, ErrConsoleTaskAssigneeSame) {
		t.Fatalf("P1 same Agent err=%v want distinct rejection", err)
	}
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "driver-task", "owner-a", dev, review); err != nil {
		t.Fatalf("P1 valid pair rejected: %v", err)
	}
}

func TestConsoleTaskDriverStartGreenReviewAndDeploySequence(t *testing.T) {
	db, start, dev, review := taskDriverFixture(t, "driver-flow")
	ctx := context.Background()
	incomplete := start
	incomplete.Schedule = ""
	if err := db.StartConsoleTaskDriver(ctx, incomplete); !errors.Is(err, ErrConsoleTaskStartIncomplete) {
		t.Fatalf("C1 missing schedule err=%v", err)
	}
	if err := db.StartConsoleTaskDriver(ctx, start); !errors.Is(err, ErrConsoleTaskStartUnassigned) {
		t.Fatalf("C1 unassigned task err=%v", err)
	}
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "driver-task", "owner-a", dev, review); err != nil {
		t.Fatal(err)
	}
	if err := db.StartConsoleTaskDriver(ctx, start); err != nil {
		t.Fatalf("C1 complete start rejected: %v", err)
	}
	if err := db.RequestConsoleTaskDeployment(ctx, start.SpecID, 1, "owner-a", start.AnnouncedAt); !errors.Is(err, ErrConsoleTaskOwnerReview) {
		t.Fatalf("C9 deploy before review err=%v", err)
	}
	if err := db.ReviewConsoleTaskDriver(ctx, start.SpecID, 1, "owner-a", start.AnnouncedAt); !errors.Is(err, ErrConsoleTaskNotGreen) {
		t.Fatalf("C8 incomplete task treated green err=%v", err)
	}
	if err := db.MarkConsoleSpecTaskCase(ctx, "driver-task", "A1", "", dev, start.AnnouncedAt); err == nil {
		t.Fatal("fixture unexpectedly accepted wrong case")
	}
	// A1 has no explicit cases in this fixture, so the mechanically defined fallback is the only case.
	if err := db.SetConsoleSpecTaskAcceptances(ctx, "driver-task", "owner-a", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkConsoleSpecTaskCase(ctx, "driver-task", "", consoleTaskFallbackCase, dev, start.AnnouncedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmConsoleSpecTaskCase(ctx, "driver-task", "", consoleTaskFallbackCase, review, start.AnnouncedAt); err != nil {
		t.Fatal(err)
	}
	if err := db.ReviewConsoleTaskDriver(ctx, start.SpecID, 1, "owner-a", start.AnnouncedAt); err != nil {
		t.Fatalf("C8 all-green review rejected: %v", err)
	}
	if err := db.RequestConsoleTaskDeployment(ctx, start.SpecID, 1, "owner-a", start.AnnouncedAt); err != nil {
		t.Fatalf("C9 reviewed deployment rejected: %v", err)
	}
}

func TestConsoleTaskDriverStartRequiresEveryOneOfSevenParts(t *testing.T) {
	_, start, _, _ := taskDriverFixture(t, "driver-seven")
	cases := []struct {
		name    string
		breakIt func(*model.ConsoleTaskDriverStart)
	}{
		{"spec", func(v *model.ConsoleTaskDriverStart) { v.SpecID = "" }}, {"revision", func(v *model.ConsoleTaskDriverStart) { v.Revision = 0 }},
		{"content sha", func(v *model.ConsoleTaskDriverStart) { v.ContentSHA256 = "" }}, {"repository", func(v *model.ConsoleTaskDriverStart) { v.RepositoryName = "" }},
		{"branch", func(v *model.ConsoleTaskDriverStart) { v.BranchName = "" }}, {"schedule", func(v *model.ConsoleTaskDriverStart) { v.Schedule = "" }},
		{"task endpoint", func(v *model.ConsoleTaskDriverStart) { v.TaskEndpoint = "" }}, {"token instructions", func(v *model.ConsoleTaskDriverStart) { v.TokenInstructions = "" }},
		{"announcer", func(v *model.ConsoleTaskDriverStart) { v.AnnouncedBy = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := start
			tc.breakIt(&v)
			if validTaskDriverStart(v) {
				t.Fatalf("C1 missing %s was accepted", tc.name)
			}
		})
	}
}

func TestConsoleTaskDriverZeroTasksIsNotGreen(t *testing.T) {
	db, start, _, _ := taskDriverFixture(t, "driver-zero")
	if _, err := db.db.Exec(`DELETE FROM console_spec_task WHERE spec_id=?`, start.SpecID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO console_task_driver_start(spec_id,revision,content_sha256,repository_name,branch_name,schedule,task_endpoint,token_instructions,announced_by,announced_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, start.SpecID, 1, start.ContentSHA256, start.RepositoryName, start.BranchName, start.Schedule, start.TaskEndpoint, start.TokenInstructions, start.AnnouncedBy, start.AnnouncedAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := db.ReviewConsoleTaskDriver(context.Background(), start.SpecID, 1, "owner-a", start.AnnouncedAt); !errors.Is(err, ErrConsoleTaskNotGreen) {
		t.Fatalf("C7 zero tasks treated green err=%v", err)
	}
}

func TestConsoleTaskDriverUnassignedTaskAppearsForNoAgent(t *testing.T) {
	db, _, dev, review := taskDriverFixture(t, "driver-unassigned")
	ctx := context.Background()
	for _, id := range []string{dev, review} {
		tasks, err := db.ListConsoleTasksForAgent(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatalf("C10 unassigned task leaked to agent=%s tasks=%v", id, tasks)
		}
	}
}

func TestConsoleTaskDriverAgentListCarriesOptionalMetadata(t *testing.T) {
	db, _, dev, review := taskDriverFixture(t, "driver-metadata")
	ctx := context.Background()
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "driver-task", "owner-a", dev, review); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC)
	planned := time.Date(2026, 10, 15, 4, 5, 6, 0, time.UTC)
	blocker, resources := "blocked-by-db", "need-test-pg"
	if err := db.UpdateConsoleSpecTaskMetadata(ctx, "driver-task", "owner-a", &started, &planned, &blocker, &resources); err != nil {
		t.Fatal(err)
	}
	tasks, err := db.ListConsoleTasksForAgent(ctx, dev)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("agent task list err=%v tasks=%v", err, tasks)
	}
	got := tasks[0]
	if got.StartedAt == nil || !got.StartedAt.Equal(started) || got.PlannedFinishAt == nil || !got.PlannedFinishAt.Equal(planned) || got.Blocker == nil || *got.Blocker != blocker || got.ResourceCoordination == nil || *got.ResourceCoordination != resources {
		t.Fatalf("metadata mismatch: %+v", got)
	}
}
