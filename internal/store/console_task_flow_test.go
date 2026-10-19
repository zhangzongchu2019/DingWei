package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"testing"
	"time"
)

func taskFlowFixture(t *testing.T) (*SQLite, time.Time, TaskFlowState) {
	t.Helper()
	db, now := lineageFixture(t, "r32")
	ctx := context.Background()
	for _, id := range []string{"dev", "review", "other"} {
		if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: id, KeyID: id, SessionName: id, OwnerPrincipal: "owner-a", Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	createLineageTask(t, db, now, "r32", "task", "delivered")
	state, err := db.AssignConsoleTaskFlow(ctx, "task", "owner-a", "group", "dev", "review", 1, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	return db, now, state
}
func flowApply(t *testing.T, db *SQLite, now time.Time, s TaskFlowState, actor, op string) (TaskFlowState, error) {
	t.Helper()
	u := TaskFlowUpdate{RequestID: fmt.Sprintf("%s-%d-%s", actor, s.Version, op), ExpectedVersion: s.Version, Operation: op, Blocker: "waiting", CaseName: consoleTaskFallbackCase}
	return db.UpdateConsoleTaskFlow(context.Background(), s.ID, actor, u, func() error { return nil }, now)
}
func TestR32TaskStateCasesAndIndependentAgentSameOwner(t *testing.T) {
	db, now, s := taskFlowFixture(t)
	ctx := context.Background()
	if s.Developer == s.Reviewer {
		t.Fatal("SAME_AGENT_PAIR_ACCEPTED")
	}
	if _, err := db.AssignConsoleTaskFlow(ctx, "task", "owner-a", "group", "dev", "dev", s.Version, func() error { return nil }, now); err == nil {
		t.Fatal("SAME_AGENT_PAIR_ACCEPTED")
	}
	// Same owner, different Agent IDs is explicitly valid (leader ruling).
	if s.Developer != "dev" || s.Reviewer != "review" {
		t.Fatal("SAME_OWNER_DIFFERENT_AGENTS_REJECTED")
	}
	if _, err := flowApply(t, db, now, s, "other", "start"); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatalf("OTHER_TASK_WRITER_ACCEPTED: %v", err)
	}
	var err error
	if _, err = flowApply(t, db, now, s, "review", "start"); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatalf("reviewer starts: %v", err)
	}
	s, err = flowApply(t, db, now, s, "dev", "start")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = flowApply(t, db, now, s, "dev", "complete"); !errors.Is(err, ErrConsoleTaskNotGreen) {
		t.Fatalf("UNCONFIRMED_TASK_DONE: %v", err)
	}
	s, err = flowApply(t, db, now, s, "dev", "mark_case")
	if err != nil {
		t.Fatal(err)
	}
	p, n, e := db.ConsoleSpecTaskCaseProgress(ctx, "task")
	if e != nil || p != 0 || n != 1 {
		t.Fatalf("UNCONFIRMED_PROGRESS: %d/%d %v", p, n, e)
	}
	if _, err = flowApply(t, db, now, s, "dev", "confirm_case"); !errors.Is(err, ErrConsoleTaskCasePermission) {
		t.Fatalf("DEVELOPER_SELF_CONFIRMED: %v", err)
	}
	s, err = flowApply(t, db, now, s, "review", "confirm_case")
	if err != nil {
		t.Fatal(err)
	}
	s, err = flowApply(t, db, now, s, "dev", "mark_case")
	if err != nil {
		t.Fatal(err)
	}
	if p, _, _ := db.ConsoleSpecTaskCaseProgress(ctx, "task"); p != 0 {
		t.Fatal("REMARK_KEPT_CONFIRMATION")
	}
	s, err = flowApply(t, db, now, s, "review", "confirm_case")
	if err != nil {
		t.Fatal(err)
	}
	s, err = flowApply(t, db, now, s, "dev", "complete")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = flowApply(t, db, now, s, "dev", "start"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatalf("DONE_AGENT_REOPENED: %v", err)
	}
}
func TestR32TaskDependencyAndIllegalTransitions(t *testing.T) {
	db, now, s := taskFlowFixture(t)
	ctx := context.Background()
	createLineageTask(t, db, now, "r32", "dep", "dep")
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec_task SET depends_on_json='["dep"]' WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	if _, err := flowApply(t, db, now, s, "dev", "start"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatalf("UNFINISHED_DEPENDENCY_STARTED: %v", err)
	}
	if _, err := flowApply(t, db, now, s, "dev", "resume"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatalf("ILLEGAL_TASK_TRANSITION: %v", err)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec_task SET status='done' WHERE id='dep'`); err != nil {
		t.Fatal(err)
	}
	if _, err := flowApply(t, db, now, s, "dev", "start"); err != nil {
		t.Fatal(err)
	}
}
func TestR32TaskAtomicAuditRecheckAndIdempotency(t *testing.T) {
	db, now, s := taskFlowFixture(t)
	ctx := context.Background()
	u := TaskFlowUpdate{RequestID: "stable", ExpectedVersion: s.Version, Operation: "start"}
	denied := errors.New("PG connection lost")
	if _, err := db.UpdateConsoleTaskFlow(ctx, "task", "dev", u, func() error { return denied }, now); !errors.Is(err, denied) {
		t.Fatalf("PG_RECHECK_IGNORED: %v", err)
	}
	after, _ := db.ConsoleTaskFlowState(ctx, "task")
	if after.Version != s.Version {
		t.Fatal("RECHECK_LEFT_PARTIAL_TASK")
	}
	if _, err := db.db.ExecContext(ctx, `CREATE TRIGGER r32_fail_audit BEFORE INSERT ON console_task_flow_audit BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateConsoleTaskFlow(ctx, "task", "dev", u, func() error { return nil }, now); err == nil {
		t.Fatal("AUDIT_FAILURE_ACCEPTED")
	}
	if _, err := db.db.ExecContext(ctx, `DROP TRIGGER r32_fail_audit`); err != nil {
		t.Fatal(err)
	}
	after, _ = db.ConsoleTaskFlowState(ctx, "task")
	if after.Version != s.Version {
		t.Fatal("AUDIT_LEFT_PARTIAL_TASK")
	}
	result, err := db.UpdateConsoleTaskFlow(ctx, "task", "dev", u, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.UpdateConsoleTaskFlow(ctx, "task", "dev", u, func() error { return nil }, now)
	if err != nil || again.Version != result.Version {
		t.Fatalf("IDEMPOTENT_WRITE_REPEATED: %v %v", again, err)
	}
	u.Operation = "block"
	if _, err = db.UpdateConsoleTaskFlow(ctx, "task", "dev", u, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatalf("ID_REUSED_FOR_DIFFERENT_PAYLOAD: %v", err)
	}
}
func TestR32ReassignmentClearsCasesAndNotifiesBoth(t *testing.T) {
	db, now, s := taskFlowFixture(t)
	ctx := context.Background()
	var err error
	s, err = flowApply(t, db, now, s, "dev", "mark_case")
	if err != nil {
		t.Fatal(err)
	}
	s, err = flowApply(t, db, now, s, "review", "confirm_case")
	if err != nil {
		t.Fatal(err)
	}
	s, err = db.AssignConsoleTaskFlow(ctx, "task", "owner-a", "group", "other", "review", s.Version, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	if p, _, _ := db.ConsoleSpecTaskCaseProgress(ctx, "task"); p != 0 {
		t.Fatal("REASSIGN_KEPT_CONFIRMATION")
	}
	items, err := db.PendingConsoleTaskNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("REASSIGN_NOTICE_MISSING: %d", len(items))
	}
	if _, err = db.AssignConsoleTaskFlow(ctx, "task", "owner-a", "group", "other", "review", s.Version, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	again, _ := db.PendingConsoleTaskNotifications(ctx)
	if len(again) != len(items) {
		t.Fatal("DUPLICATE_ASSIGNMENT_NOTICE")
	}
	if _, err = flowApply(t, db, now, s, "dev", "start"); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatalf("REASSIGNED_ACTOR_WRITES: %v", err)
	}
	if err = db.InvalidateConsoleTaskFlow(ctx, "task", s.Version, now); err != nil {
		t.Fatal(err)
	}
	if _, err = flowApply(t, db, now, s, "other", "start"); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatalf("REVOKED_TASK_WRITES: %v", err)
	}
}

func TestR32OutboxFailureRollsBackAssignment(t *testing.T) {
	db, now, s := taskFlowFixture(t)
	ctx := context.Background()
	if _, err := db.db.ExecContext(ctx, `CREATE TRIGGER r32_fail_outbox BEFORE INSERT ON console_task_flow_outbox BEGIN SELECT RAISE(ABORT,'outbox unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AssignConsoleTaskFlow(ctx, "task", "owner-a", "group", "other", "review", s.Version, func() error { return nil }, now); err == nil {
		t.Fatal("OUTBOX_FAILURE_ACCEPTED")
	}
	after, err := db.ConsoleTaskFlowState(ctx, "task")
	if err != nil || after.Version != s.Version || after.Developer != s.Developer {
		t.Fatalf("OUTBOX_LEFT_PARTIAL_ASSIGNMENT: %+v %v", after, err)
	}
}

func TestR32CandidateStageRechecksPGBeforeCommit(t *testing.T) {
	db, now := lineageFixture(t, "candidate-check")
	ctx := context.Background()
	agent := model.ConsoleAgent{ID: "split-agent", KeyID: "split-key", SessionName: "split", OwnerPrincipal: "owner-a", Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	request := model.ConsoleSpecPresplitRequest{ID: "candidate-check", SpecID: "candidate-check", AgentID: agent.ID, CreatedAt: now, DeadlineAt: now.Add(time.Hour)}
	if _, _, err := db.StartConsoleSpecPresplit(ctx, request, "owner-a"); err != nil {
		t.Fatal(err)
	}
	denied := errors.New("fixture PG authorization connection lost")
	err := db.CompleteConsoleSpecPresplit(ctx, request.ID, agent, []ConsoleSpecPresplitTask{{ID: "candidate", Title: "candidate", Content: "work", DependsOn: []string{}}}, now, func() error { return denied })
	if !errors.Is(err, denied) {
		t.Fatalf("CANDIDATE_PG_RECHECK_IGNORED: %v", err)
	}
	candidate, err := db.ConsoleTaskCandidates(ctx, request.SpecID, "owner-a")
	if err != nil || candidate != nil {
		t.Fatalf("CANDIDATE_PG_FAILURE_LEFT_WRITE: %+v %v", candidate, err)
	}
	state, err := db.ConsoleSpecPresplit(ctx, request.SpecID, "owner-a", now, now.Add(-time.Hour))
	if err != nil || state == nil || state.Status != "pending" {
		t.Fatalf("request partially completed: %+v %v", state, err)
	}
}

func TestR32OwnerExplicitlyReopensCompletedInvalidAssignment(t *testing.T) {
	db, now, s := taskFlowFixture(t)
	ctx := context.Background()
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec_task SET status='done' WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	if err := db.InvalidateConsoleTaskFlow(ctx, s.ID, s.Version, now); err != nil {
		t.Fatal(err)
	}
	s, err := db.ConsoleTaskFlowState(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AssignConsoleTaskFlow(ctx, s.ID, "owner-a", "group", "other", "review", s.Version, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatalf("DONE_REASSIGNED_WITHOUT_CONFIRMATION: %v", err)
	}
	s, err = db.AssignConsoleTaskFlow(ctx, s.ID, "owner-a", "group", "other", "review", s.Version, func() error { return nil }, now, true)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.db.QueryRowContext(ctx, `SELECT status FROM console_spec_task WHERE id='task'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "planned" || s.NeedsReassignment {
		t.Fatalf("OWNER_CANNOT_RESOLVE_INVALID_COMPLETED_TASK: %s %+v", status, s)
	}
}

func TestR32TaskOutboxStablePagesPreserveWaitingRows(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	ctx := context.Background()
	for i := 0; i < 201; i++ {
		if _, err := db.db.ExecContext(ctx, `INSERT INTO console_task_flow_outbox(id,task_id,assignment_version,recipient_agent_id,role,event,body,created_at) VALUES(?,?,?,?,'owner','invalidated','{}',?)`, fmt.Sprintf("000-waiting-%03d", i), state.ID, state.AssignmentVersion, fmt.Sprintf("owner:waiting-%03d", i), now.UTC().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	after := ""
	pages := 0
	for {
		rows, err := db.PendingConsoleTaskNotificationsAfter(ctx, after)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		pages++
		if len(rows) > 100 {
			t.Fatal("OUTBOX_PAGE_UNBOUNDED")
		}
		for _, r := range rows {
			if seen[r.ID] || r.ID <= after {
				t.Fatal("OUTBOX_CURSOR_NOT_ADVANCED")
			}
			seen[r.ID] = true
		}
		after = rows[len(rows)-1].ID
	}
	if len(seen) != 203 || pages != 3 {
		t.Fatalf("WAITING_OUTBOX_STARVED_LATER_PAGE: rows=%d pages=%d", len(seen), pages)
	}
	all, err := db.PendingConsoleTaskNotifications(ctx)
	if err != nil || len(all) != 203 {
		t.Fatalf("BOARD_PENDING_PROJECTION_TRUNCATED: count=%d err=%v", len(all), err)
	}
	rows, err := db.PendingConsoleTaskNotificationsAfter(ctx, "")
	if err != nil || len(rows) != 100 {
		t.Fatal("WAITING_ROWS_WERE_CONSUMED", err, len(rows))
	}
}
