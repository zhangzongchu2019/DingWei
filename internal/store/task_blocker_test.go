package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func blockerApply(t *testing.T, db *SQLite, now time.Time, actor BlockerActor, bid string, u BlockerUpdate) (BlockerResult, error) {
	t.Helper()
	state, err := db.ConsoleTaskFlowState(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	return db.UpdateTaskBlocker(context.Background(), state, actor, bid, u, func() error { return nil }, now)
}
func blockerCreate(t *testing.T, db *SQLite, now time.Time, request, where, waiting string, full bool) TaskBlocker {
	t.Helper()
	state, err := db.ConsoleTaskFlowState(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	result, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, "", BlockerUpdate{RequestID: request, ExpectedVersion: state.Version, Operation: "create", Kind: "technical", Where: where, WaitingPrincipal: waiting, ExpectedInput: "真实材料与授权记录", ProductionOrSecurity: full})
	if err != nil {
		t.Fatal(err)
	}
	return result.Blocker
}
func blockerRead(t *testing.T, db *SQLite, bid string) TaskBlocker {
	t.Helper()
	state, err := db.ConsoleTaskFlowState(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := db.ReadTaskBlockers(context.Background(), state, BlockerActor{AgentID: "dev"}, bid, "", 100, func() error { return nil })
	if err != nil || len(list) != 1 {
		t.Fatal(err, list)
	}
	return list[0]
}
func blockerInput(t *testing.T, db *SQLite, now time.Time, b TaskBlocker, event string) TaskBlocker {
	t.Helper()
	out, err := blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, BlockerUpdate{RequestID: "req-" + event, ExpectedVersion: b.Version, Operation: "input", Source: "user:owner-a", EventID: event, Body: "答复到件，仍需重核", Evidence: "git:immutable/evidence"})
	if err != nil {
		t.Fatal(err)
	}
	return out.Blocker
}
func blockerFinish(t *testing.T, db *SQLite, now time.Time, b TaskBlocker, material string) BlockerResult {
	t.Helper()
	begin, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: fmt.Sprintf("begin-%s-%d", b.ID, b.InputVersion), ExpectedVersion: b.Version, Operation: "begin_recheck", InputVersion: b.InputVersion})
	if err != nil {
		t.Fatal(err)
	}
	b = begin.Blocker
	out, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: fmt.Sprintf("finish-%s-%d", b.ID, b.InputVersion), ExpectedVersion: b.Version, Operation: "finish_recheck", InputVersion: b.InputVersion, LeaseID: b.LeaseID, Checks: []BlockerCheck{{"materials", material, "git:material"}, {"environment", "pass", "test:isolated-environment"}, {"authorization", "pass", "owner:current-recheck-only"}}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestBlockerLifecycleNoImplicitResumeAndNoRepeatedWake(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	ctx := context.Background()
	var err error
	state, err = flowApply(t, db, now, state, "dev", "start")
	if err != nil {
		t.Fatal(err)
	}
	b := blockerCreate(t, db, now, "create", "等待技术事实", "user:owner-a", false)
	state, _ = db.ConsoleTaskFlowState(ctx, "task")
	if _, err = flowApply(t, db, now, state, "dev", "resume"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_LEGACY_RESUME_BYPASS", err)
	}
	if _, err = blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: "no-input", ExpectedVersion: b.Version, Operation: "begin_recheck", InputVersion: 1}); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_RECHECK_WITHOUT_NEW_INPUT", err)
	}
	b = blockerInput(t, db, now, b, "answer-1")
	pending, err := db.PendingBlockerWakes(ctx, "")
	if err != nil || len(pending) != 1 {
		t.Fatal(err, pending)
	}
	// Identical event, a different HTTP request ID: no new material version/audit/wake.
	same := BlockerUpdate{RequestID: "repeat-answer", ExpectedVersion: b.Version, Operation: "input", Source: "user:owner-a", EventID: "answer-1", Body: "答复到件，仍需重核", Evidence: "git:immutable/evidence"}
	unchanged, err := blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, same)
	if err != nil || unchanged.Outcome != "unchanged" || unchanged.Blocker.InputVersion != 1 {
		t.Fatal("BLOCKER_EVENT_DUPLICATED", err, unchanged)
	}
	if _, err = blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, same); err != nil {
		t.Fatal(err)
	}
	same.RequestID = "changed-answer"
	same.Body = "changed"
	if _, err = blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, same); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_EVENT_REPLACED", err)
	}
	b = blockerFinish(t, db, now, b, "unknown").Blocker
	if b.State != "waiting" || b.InputVersion != b.CheckedInputVersion {
		t.Fatal("BLOCKER_UNKNOWN_RESOLVED", b)
	}
	if _, err = blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: "repeat-without-input", ExpectedVersion: b.Version, Operation: "begin_recheck", InputVersion: b.InputVersion}); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_NO_INPUT_REPEATED_RECHECK", err)
	}
	b = blockerInput(t, db, now, b, "answer-2")
	out := blockerFinish(t, db, now, b, "pass")
	b = out.Blocker
	if b.State != "resolved" || out.ExecutionAuthorized || out.Outcome != "rechecked_not_resumed" {
		t.Fatal("BLOCKER_RECHECK_AUTO_EXECUTED", out)
	}
	var status string
	if err = db.db.QueryRow(`SELECT status FROM console_spec_task WHERE id='task'`).Scan(&status); err != nil || status != "blocked" {
		t.Fatal("BLOCKER_RECHECK_AUTO_RESUMED", status, err)
	}
	state, _ = db.ConsoleTaskFlowState(ctx, "task")
	if _, err = flowApply(t, db, now, state, "dev", "resume"); err != nil {
		t.Fatal(err)
	}
	pending, _ = db.PendingBlockerWakes(ctx, "")
	if len(pending) != 2 {
		t.Fatal("BLOCKER_NO_INPUT_ADDED_WAKE", len(pending))
	}
}
func TestBlockerMultipleCardsLegacyGateAndPreservation(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	ctx := context.Background()
	state, err := flowApply(t, db, now, state, "dev", "start")
	if err != nil {
		t.Fatal(err)
	}
	state, err = flowApply(t, db, now, state, "dev", "block")
	if err != nil {
		t.Fatal(err)
	}
	b := blockerCreate(t, db, now, "new-card", "new reason", "user:owner-a", false)
	var legacy int
	db.db.QueryRow(`SELECT count(*) FROM console_task_blocker WHERE task_id='task' AND id LIKE 'legacy:%' AND state='needs_owner'`).Scan(&legacy)
	if legacy != 1 {
		t.Fatal("BLOCKER_LEGACY_TEXT_DROPPED", legacy)
	}
	b = blockerInput(t, db, now, b, "new-answer")
	blockerFinish(t, db, now, b, "pass")
	state, _ = db.ConsoleTaskFlowState(ctx, "task")
	if _, err = flowApply(t, db, now, state, "dev", "resume"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_OTHER_OPEN_CARD_BYPASS", err)
	}
	// Legacy owner planning route must share the same gate, even when dependencies
	// and completed case records would otherwise permit a state change.
	if err = db.UpdateConsoleTaskProposalPlanAuthorized(ctx, "task", "owner-a", false, "delivered", "in_progress", 1, nil, "", func() error { return nil }); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_OWNER_PLAN_BYPASS", err)
	}
}
func TestBlockerRequestReplayAndRollbackAreAtomic(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	ctx := context.Background()
	u := BlockerUpdate{RequestID: "stable", ExpectedVersion: state.Version, Operation: "create", Kind: "resource", Where: "resource", ExpectedInput: "real resource"}
	injected := errors.New("PG final check failed")
	if _, err := db.UpdateTaskBlocker(ctx, state, BlockerActor{AgentID: "dev"}, "", u, func() error { return injected }, now); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	for _, table := range []string{"console_task_blocker", "console_task_blocker_outbox", "console_task_blocker_input", "console_task_blocker_request", "console_task_blocker_audit"} {
		var n int
		if err := db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("BLOCKER_PARTIAL_COMMIT", table, n, err)
		}
	}
	out, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, "", u)
	if err != nil {
		t.Fatal(err)
	}
	again, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, "", u)
	if err != nil || !again.Replayed || again.Blocker.ID != out.Blocker.ID {
		t.Fatal("BLOCKER_REQUEST_REPLAY_CHANGED", err, again)
	}
	u.Where = "different"
	if _, err = blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, "", u); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_REQUEST_HASH_NOT_BOUND", err)
	}
	state, _ = db.ConsoleTaskFlowState(ctx, "task")
	bad := state
	bad.Version--
	if _, err = db.UpdateTaskBlocker(ctx, bad, BlockerActor{AgentID: "dev"}, out.Blocker.ID, BlockerUpdate{RequestID: "state-race", ExpectedVersion: out.Blocker.Version, Operation: "begin_recheck", InputVersion: 1}, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_INITIAL_STATE_NOT_RECHECKED", err)
	}
}
func TestBlockerWaiterRightsAndFullMode(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	b := blockerCreate(t, db, now, "full", "prod/security", "agent:other", true)
	if _, err := blockerApply(t, db, now, BlockerActor{AgentID: "review"}, b.ID, BlockerUpdate{RequestID: "reviewer-input", ExpectedVersion: b.Version, Operation: "input", Source: "agent:review", EventID: "reply", Body: "reply", Evidence: "ref"}); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("BLOCKER_UNADDRESSED_REPLY_ACCEPTED", err)
	}
	out, err := blockerApply(t, db, now, BlockerActor{AgentID: "other"}, b.ID, BlockerUpdate{RequestID: "waiting-agent", ExpectedVersion: b.Version, Operation: "input", Source: "agent:other", EventID: "answer", Body: "data", Evidence: "ref"})
	if err != nil {
		t.Fatal(err)
	}
	encoded := fmt.Sprintf("%+v", out.Blocker)
	if out.Blocker.SpecID != "" || out.Blocker.Developer != "" || strings.Contains(encoded, "prod/security") {
		t.Fatal("BLOCKER_WAITER_GOT_TASK_CONTEXT", out)
	}
	b = blockerRead(t, db, b.ID)
	if _, err = blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: "full-without-owner", ExpectedVersion: b.Version, Operation: "begin_recheck", InputVersion: b.InputVersion}); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_FULL_MODE_APPROVAL_BYPASS", err)
	}
	out, err = blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, BlockerUpdate{RequestID: "full-approval", ExpectedVersion: b.Version, Operation: "approve_full", FullApprovalRef: "owner:full-review"})
	if err != nil {
		t.Fatal(err)
	}
	b = out.Blocker
	if b.Mode != "full" || b.FullApprovalRef == "" || b.InputVersion != 2 {
		t.Fatal("BLOCKER_FULL_APPROVAL_NOT_NEW_INPUT", b)
	}
	result := blockerFinish(t, db, now, b, "pass")
	if result.Blocker.State != "resolved" || result.ExecutionAuthorized {
		t.Fatal("BLOCKER_FULL_FINISH_WRONG", result)
	}
}
func TestBlockerNewInputDuringRecheckAndAssignmentInvalidate(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	ctx := context.Background()
	b := blockerCreate(t, db, now, "card", "wait", "user:owner-a", false)
	b = blockerInput(t, db, now, b, "first")
	begin, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: "lease", ExpectedVersion: b.Version, Operation: "begin_recheck", InputVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	b = begin.Blocker
	oldLease := b.LeaseID
	b = blockerInput(t, db, now, b, "second")
	out, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: "stale-finish", ExpectedVersion: b.Version, Operation: "finish_recheck", InputVersion: 1, LeaseID: oldLease})
	if err != nil || out.Blocker.State != "ready_to_recheck" || out.Outcome != "new_input_requires_recheck" {
		t.Fatal("BLOCKER_NEW_INPUT_SWALLOWED", out, err)
	}
	state, _ = db.ConsoleTaskFlowState(ctx, "task")
	state, err = db.AssignConsoleTaskFlow(ctx, "task", state.Owner, state.GroupCode, "other", "review", state.Version, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: "former-executor", ExpectedVersion: out.Blocker.Version, Operation: "begin_recheck", InputVersion: 2}); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("BLOCKER_FORMER_EXECUTOR_ALLOWED", err)
	}
	if err = db.ReconcileTaskBlockers(ctx, state, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	var actual string
	db.db.QueryRow(`SELECT state FROM console_task_blocker WHERE id=?`, b.ID).Scan(&actual)
	if actual != "needs_reassignment" {
		t.Fatal("BLOCKER_REASSIGN_DID_NOT_INVALIDATE", actual)
	}
	old, err := scanBlocker(db.db.QueryRow(`SELECT `+blockerCols+` FROM console_task_blocker WHERE id=?`, b.ID))
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, BlockerUpdate{RequestID: "adopt", ExpectedVersion: old.Version, Operation: "adopt"})
	if err != nil || adopted.Blocker.Generation != 2 || adopted.Blocker.Developer != "other" {
		t.Fatal("BLOCKER_ADOPT_NOT_REBOUND", adopted, err)
	}
}

func TestBlockerDependencyConditionTransitionCreatesOneInput(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	ctx := context.Background()
	createLineageTask(t, db, now, "r32", "dependency", "dependency")
	if _, err := db.db.Exec(`UPDATE console_spec_task SET depends_on_json='["dependency"]' WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	b := blockerCreate(t, db, now, "blocked-on-gate", "等指定依赖", "user:owner-a", false)
	reconcile := func() {
		t.Helper()
		state, err := db.ConsoleTaskFlowState(ctx, "task")
		if err != nil {
			t.Fatal(err)
		}
		if err = db.ReconcileTaskBlockers(ctx, state, func() error { return nil }, now); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	reconcile()
	pending, _ := db.PendingBlockerWakes(ctx, "")
	if len(pending) != 0 {
		t.Fatal("BLOCKER_UNCHANGED_CONDITION_WOKE", pending)
	}
	if _, err := db.db.Exec(`UPDATE console_spec_task SET status='done' WHERE id='dependency'`); err != nil {
		t.Fatal(err)
	}
	reconcile()
	reconcile()
	b = blockerRead(t, db, b.ID)
	if b.InputVersion != 1 || b.State != "ready_to_recheck" {
		t.Fatal("BLOCKER_VERIFIED_CONDITION_NOT_WAKING", b)
	}
	pending, _ = db.PendingBlockerWakes(ctx, "")
	if len(pending) != 1 {
		t.Fatal("BLOCKER_CONDITION_DUPLICATE_WAKE", pending)
	}
	var source, body string
	if err := db.db.QueryRow(`SELECT source,body FROM console_task_blocker_input WHERE blocker_id=?`, b.ID).Scan(&source, &body); err != nil || source != "condition:dependency_gate" || !strings.Contains(body, "仅请求重核") {
		t.Fatal(err, source, body)
	}
	// A new false -> true transition is new evidence, never an online/retry event.
	if _, err := db.db.Exec(`UPDATE console_spec_task SET status='blocked' WHERE id='dependency'`); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if _, err := db.db.Exec(`UPDATE console_spec_task SET status='done' WHERE id='dependency'`); err != nil {
		t.Fatal(err)
	}
	reconcile()
	b = blockerRead(t, db, b.ID)
	if b.InputVersion != 2 {
		t.Fatal("BLOCKER_NEW_CONDITION_TRANSITION_LOST", b)
	}
}
