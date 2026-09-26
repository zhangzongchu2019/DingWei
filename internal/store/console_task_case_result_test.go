package store

import (
	"context"
	"errors"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestConsoleTaskCaseTwoStageRolesDenominatorFallbackAndCascade(t *testing.T) {
	ctx := context.Background()
	db, now := lineageFixture(t, "case-progress")
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec_revision SET acceptance_json='{"criteria":[{"id":"A1","text":"criterion","cases":["case-1","case-2"]}]}' WHERE spec_id='case-progress' AND revision=1`); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []model.ConsoleAgent{
		{ID: "dev", KeyID: "dev-key", SessionName: "dev", OwnerPrincipal: "owner-a", Active: true},
		{ID: "review", KeyID: "review-key", SessionName: "review", OwnerPrincipal: "owner-a", Active: true},
		{ID: "other", KeyID: "other-key", SessionName: "other", OwnerPrincipal: "owner-a", Active: true},
	} {
		if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
			t.Fatal(err)
		}
	}
	createLineageTask(t, db, now, "case-progress", "task", "dod")
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "task", "owner-a", "dev", "review"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConsoleSpecTaskAcceptances(ctx, "task", "owner-a", []string{"A1"}); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkConsoleSpecTaskCase(ctx, "task", "A1", "case-1", "other", now); !errors.Is(err, ErrConsoleTaskCasePermission) {
		t.Fatalf("K21 other developer mark err=%v", err)
	}
	if err := db.MarkConsoleSpecTaskCase(ctx, "task", "A1", "case-1", "dev", now); err != nil {
		t.Fatal(err)
	}
	if passed, total, err := db.ConsoleSpecTaskCaseProgress(ctx, "task"); err != nil || passed != 0 || total != 2 {
		t.Fatalf("K20 marked-only=%d/%d err=%v", passed, total, err)
	}
	if err := db.ConfirmConsoleSpecTaskCase(ctx, "task", "A1", "case-1", "dev", now); !errors.Is(err, ErrConsoleTaskCasePermission) {
		t.Fatalf("K22 developer self-confirm err=%v", err)
	}
	if err := db.ConfirmConsoleSpecTaskCase(ctx, "task", "A1", "case-1", "review", now); err != nil {
		t.Fatal(err)
	}
	if passed, total, err := db.ConsoleSpecTaskCaseProgress(ctx, "task"); err != nil || passed != 1 || total != 2 {
		t.Fatalf("K20 confirmed=%d/%d err=%v", passed, total, err)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec_revision SET acceptance_json='{"criteria":[{"id":"A1","cases":["case-1","case-2","case-3"]}]}' WHERE spec_id='case-progress' AND revision=1`); err != nil {
		t.Fatal(err)
	}
	if passed, total, _ := db.ConsoleSpecTaskCaseProgress(ctx, "task"); passed != 1 || total != 3 {
		t.Fatalf("K23 added case denominator=%d/%d want 1/3", passed, total)
	}
	createLineageTask(t, db, now, "case-progress", "fallback-task", "dod")
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "fallback-task", "owner-a", "dev", "review"); err != nil {
		t.Fatal(err)
	}
	if passed, total, _ := db.ConsoleSpecTaskCaseProgress(ctx, "fallback-task"); passed != 0 || total != 1 {
		t.Fatalf("K24 fallback=%d/%d want 0/1", passed, total)
	}
	if err := db.MarkConsoleSpecTaskCase(ctx, "fallback-task", "", consoleTaskFallbackCase, "dev", now); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmConsoleSpecTaskCase(ctx, "fallback-task", "", consoleTaskFallbackCase, "review", now); err != nil {
		t.Fatal(err)
	}
	if passed, total, _ := db.ConsoleSpecTaskCaseProgress(ctx, "fallback-task"); passed != 1 || total != 1 {
		t.Fatalf("K24 produced fallback=%d/%d want 1/1", passed, total)
	}
	if taskAcceptanceDisableCascade {
		if _, err := db.db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeleteConsoleSpecTask(ctx, "task", "owner-a"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_spec_task_case_result WHERE task_id='task'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("K25 cascade rows=%d err=%v", n, err)
	}
}

func TestConsoleTaskCaseAcceptanceIDIsApplicationValidatedBecauseCriteriaLiveInJSON(t *testing.T) {
	ctx := context.Background()
	db, now := lineageFixture(t, "case-membership")
	for _, a := range []model.ConsoleAgent{{ID: "dev", KeyID: "d", SessionName: "d", OwnerPrincipal: "owner-a", Active: true}, {ID: "rev", KeyID: "r", SessionName: "r", OwnerPrincipal: "owner-a", Active: true}} {
		if err := db.UpsertConsoleAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	createLineageTask(t, db, now, "case-membership", "task", "dod")
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, "task", "owner-a", "dev", "rev"); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkConsoleSpecTaskCase(ctx, "task", "A99", "invented", "dev", now); !errors.Is(err, ErrConsoleTaskCaseUnknown) {
		t.Fatalf("acceptance_id has no FK, so application validation must reject err=%v", err)
	}
}
