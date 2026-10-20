package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
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
		t.Fatal("BLOCKER_FINAL_RECHECK_COMMITTED", err)
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
	if _, err = db.UpdateTaskBlocker(ctx, bad, BlockerActor{AgentID: "dev"}, out.Blocker.ID, BlockerUpdate{RequestID: "state-race", ExpectedVersion: state.Version, Operation: "create", Kind: "technical", Where: "another valid card", ExpectedInput: "evidence"}, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowConflict) {
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
	if out.Blocker.SpecID != "" || out.Blocker.Developer != "" || out.Blocker.Mode != "" || out.Blocker.FullApprovalRef != "" || out.Blocker.Where != "prod/security" {
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
	out, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, BlockerUpdate{RequestID: "stale-finish", ExpectedVersion: b.Version, Operation: "finish_recheck", InputVersion: 1, LeaseID: oldLease, Checks: []BlockerCheck{{"materials", "pass", "git:material"}, {"environment", "pass", "local:isolated"}, {"authorization", "pass", "owner:recheck-only"}}})
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

func TestBlockerRecheckRequiresCurrentDeveloperVersionAndLease(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	b := blockerCreate(t, db, now, "card", "waiting", "user:owner-a", false)
	b = blockerInput(t, db, now, b, "facts")
	base := BlockerUpdate{RequestID: "reviewer-begin", ExpectedVersion: b.Version, Operation: "begin_recheck", InputVersion: b.InputVersion}
	if _, err := blockerApply(t, db, now, BlockerActor{AgentID: "review"}, b.ID, base); err == nil {
		t.Fatal("BLOCKER_REVIEWER_OPENED_DEVELOPER_LEASE")
	}
	base.RequestID = "stale-version"
	base.ExpectedVersion = b.Version - 1
	if _, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, base); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_CARD_CAS_BYPASS", err)
	}
	base.RequestID = "begin-current"
	base.ExpectedVersion = b.Version
	out, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	b = out.Blocker
	finish := BlockerUpdate{RequestID: "forged-lease", ExpectedVersion: b.Version, Operation: "finish_recheck", InputVersion: b.InputVersion, LeaseID: "wrong", Checks: []BlockerCheck{{"materials", "pass", "git:material"}, {"environment", "pass", "local:fixture"}, {"authorization", "pass", "owner:recheck-only"}}}
	if _, err = blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, finish); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_FORGED_LEASE_ACCEPTED", err)
	}
	finish.RequestID = "expired-lease"
	finish.LeaseID = b.LeaseID
	if _, err = blockerApply(t, db, now.Add(6*time.Minute), BlockerActor{AgentID: "dev"}, b.ID, finish); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_EXPIRED_LEASE_ACCEPTED", err)
	}
}
func TestBlockerOutboxReconciliationRepairsMissingSignalWithoutNewInput(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	ctx := context.Background()
	b := blockerCreate(t, db, now, "card", "waiting", "user:owner-a", false)
	b = blockerInput(t, db, now, b, "facts")
	before, _ := db.PendingBlockerWakes(ctx, "")
	if len(before) != 1 {
		t.Fatal(before)
	}
	if _, err := db.db.Exec(`DELETE FROM console_task_blocker_outbox WHERE blocker_id=?`, b.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		state, err := db.ConsoleTaskFlowState(ctx, "task")
		if err != nil {
			t.Fatal(err)
		}
		if err = db.ReconcileTaskBlockers(ctx, state, func() error { return nil }, now); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := db.PendingBlockerWakes(ctx, "")
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].InputVersion != 1 {
		t.Fatal("BLOCKER_OUTBOX_NOT_REPAIRED_IDEMPOTENTLY", after)
	}
}

func TestBlockerOwnerCannotImpersonateResumeAfterResolution(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	var err error
	state, err = flowApply(t, db, now, state, "dev", "start")
	if err != nil {
		t.Fatal(err)
	}
	b := blockerCreate(t, db, now, "card", "waiting", "user:owner-a", false)
	b = blockerInput(t, db, now, b, "answer")
	blockerFinish(t, db, now, b, "pass")
	if err = db.UpdateConsoleTaskProposalPlanAuthorized(context.Background(), "task", "owner-a", false, "delivered", "in_progress", 1, nil, "", func() error { return nil }); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("BLOCKER_OWNER_IMPERSONATED_RESUME", err)
	}
}

func TestBlockerReadOnlySnapshotAndBoundedInputHistory(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	ctx := context.Background()
	b := blockerCreate(t, db, now, "card", "waiting", "user:owner-a", false)
	for i := 0; i < 105; i++ {
		b = blockerInput(t, db, now.Add(time.Duration(i)*time.Minute), b, fmt.Sprintf("event-%d", i))
	}
	state, err := db.ConsoleTaskFlowState(ctx, "task")
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err = db.db.QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	cards, inputs, err := db.ReadTaskBlockers(ctx, state, BlockerActor{AgentID: "dev"}, b.ID, "", 2, func() error { return nil })
	if err != nil || len(cards) != 1 || len(inputs) != 101 || inputs[0].InputVersion != 105 || inputs[100].InputVersion != 5 {
		t.Fatal("BLOCKER_INPUT_HISTORY_UNBOUNDED_OR_SKIPPED", len(cards), len(inputs), err)
	}
	_, older, err := db.ReadTaskBlockers(ctx, state, BlockerActor{AgentID: "dev"}, b.ID, "", 2, func() error { return nil }, 5)
	if err != nil || len(older) != 4 || older[0].InputVersion != 4 || older[3].InputVersion != 1 {
		t.Fatal("BLOCKER_INPUT_CONTINUATION_BROKEN", older, err)
	}
	if err = db.db.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("BLOCKER_GET_WROTE_STATE", before, after)
	}
	if _, _, err = db.ReadTaskBlockers(ctx, state, BlockerActor{AgentID: "dev"}, b.ID, "", 102, func() error { return nil }); !errors.Is(err, ErrTaskFlowInvalid) {
		t.Fatal("BLOCKER_UNBOUNDED_READ_ACCEPTED", err)
	}
}

func TestBlockerLegacyDriverStartCannotBypassUnresolvedCards(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	ctx := context.Background()
	var start model.ConsoleTaskDriverStart
	if err := db.db.QueryRow(`SELECT spec_id,revision,content_sha256 FROM console_spec_task WHERE id='task'`).Scan(&start.SpecID, &start.Revision, &start.ContentSHA256); err != nil {
		t.Fatal(err)
	}
	start.RepositoryName = "local-fixture"
	start.BranchName = "fixture"
	start.Schedule = "test-only"
	start.TaskEndpoint = "http://127.0.0.1/fixture"
	start.TokenInstructions = "synthetic no credentials"
	start.AnnouncedBy = "owner-a"
	start.AnnouncedAt = now
	if err := db.StartConsoleTaskDriver(ctx, start); err != nil {
		t.Fatal("positive control", err)
	}
	if _, err := db.db.Exec(`DELETE FROM console_task_driver_start WHERE spec_id=? AND revision=?`, start.SpecID, start.Revision); err != nil {
		t.Fatal(err)
	}
	blockerCreate(t, db, now, "card", "waiting", "user:owner-a", false)
	if err := db.StartConsoleTaskDriver(ctx, start); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("BLOCKER_LEGACY_DRIVER_START_BYPASS", err)
	}
	var n int
	if err := db.db.QueryRow(`SELECT count(*) FROM console_task_driver_start`).Scan(&n); err != nil || n != 0 {
		t.Fatal("BLOCKER_LEGACY_START_RECORD_CREATED", n, err)
	}
}

func TestBlockerMigrationFailureRollsBackDDLAndLedger(t *testing.T) {
	db, _, _ := taskFlowFixture(t)
	ctx := context.Background()
	// Only this disposable fixture is reset to its immediately preceding schema.
	for _, table := range []string{"console_task_blocker_condition", "console_task_blocker_outbox", "console_task_blocker_input", "console_task_blocker_audit", "console_task_blocker_request", "console_task_blocker"} {
		if _, err := db.db.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatal(err)
		}
	}
	name := "0058_pilot_task_blockers.sql"
	if _, err := db.db.Exec(`DELETE FROM schema_migrations WHERE version=?`, name); err != nil {
		t.Fatal(err)
	}
	raw, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if err = db.applyMigration(ctx, name, string(raw)+";DELIBERATELY_INVALID_SQL;", digest); err == nil {
		t.Fatal("BLOCKER_BAD_MIGRATION_ACCEPTED")
	}
	var tables, ledger, tasks int
	if err = db.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'console_task_blocker%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err = db.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=?`, name).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err = db.db.QueryRow(`SELECT count(*) FROM console_spec_task`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || ledger != 0 || tasks != 1 {
		t.Fatal("BLOCKER_MIGRATION_LEFT_PARTIAL_STATE", tables, ledger, tasks)
	}
	if err = db.applyMigration(ctx, name, string(raw), digest); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'console_task_blocker%'`).Scan(&tables); err != nil || tables != 6 {
		t.Fatal("BLOCKER_MIGRATION_REAPPLY_WRONG", tables, err)
	}
}

func TestBlockerCachedReplyUsesCurrentReadingRights(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	b := blockerCreate(t, db, now, "card", "PRIVATE-CARD-CONTEXT", "agent:dev", false)
	u := BlockerUpdate{RequestID: "previous-developer-reply", ExpectedVersion: b.Version, Operation: "input", Source: "agent:dev", EventID: "answer", Body: "material", Evidence: "git:evidence"}
	out, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, u)
	if err != nil || out.Blocker.SpecID == "" || out.TaskVersion == 0 {
		t.Fatal("positive control did not have reading rights", out, err)
	}
	state, err := db.ConsoleTaskFlowState(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AssignConsoleTaskFlow(context.Background(), "task", state.Owner, state.GroupCode, "other", "review", state.Version, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	// The former developer remains explicitly addressed, but is now only a waiter.
	again, err := blockerApply(t, db, now, BlockerActor{AgentID: "dev"}, b.ID, u)
	if err != nil || !again.Replayed {
		t.Fatal(err, again)
	}
	if again.Blocker.SpecID != "" || again.Blocker.Developer != "" || again.Blocker.Where != "" || again.TaskVersion != 0 {
		t.Fatal("BLOCKER_CACHED_REPLY_RETAINED_OLD_READING_AUTHORITY", again)
	}
	var n int
	if err = db.db.QueryRow(`SELECT count(*) FROM console_task_blocker_input WHERE blocker_id=?`, b.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("BLOCKER_CACHED_REPLY_CREATED_NEW_INPUT", n, err)
	}
}

func TestBlockerAdoptPersistsCurrentRevisionTuple(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	ctx := context.Background()
	b := blockerCreate(t, db, now, "card", "wait", "user:owner-a", true)
	b = blockerInput(t, db, now, b, "old-answer")
	// Model a fully materialized current task reference, not a broken FK or an
	// unconfirmed change proposal. The old revision remains immutable history.
	if _, err := db.db.Exec(`INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_by_kind,created_via,supersedes_revision,created_at) SELECT spec_id,2,content_markdown,acceptance_json,input_anchors_json,'new-sha',created_by,created_by_kind,created_via,revision,created_at FROM console_spec_revision WHERE spec_id='r32' AND revision=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE console_spec_task SET revision=2,content_sha256='new-sha',version=version+1 WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	state, err := db.ConsoleTaskFlowState(ctx, "task")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ReconcileTaskBlockers(ctx, state, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	old := blockerRead(t, db, b.ID)
	if old.State != "needs_reassignment" {
		t.Fatal(old)
	}
	out, err := blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, BlockerUpdate{RequestID: "adopt-new-revision", ExpectedVersion: old.Version, Operation: "adopt"})
	if err != nil {
		t.Fatal(err)
	}
	actual := blockerRead(t, db, b.ID)
	if actual.Revision != 2 || actual.SHA != "new-sha" || actual.State != "ready_to_recheck" || actual.Generation != out.Blocker.Generation || actual.FullApprovalRef != "" {
		t.Fatal("BLOCKER_ADOPT_REVISION_NOT_DURABLE", actual, out)
	}
	again := blockerCreate(t, db, now, "same-current-card", "wait", "user:owner-a", true)
	if again.ID != b.ID || again.Generation != actual.Generation {
		t.Fatal("BLOCKER_ADOPT_FINGERPRINT_NOT_REBOUND", again, actual)
	}
}

func TestBlockerWaiterChangeKeepsCurrentCardFingerprint(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	b := blockerCreate(t, db, now, "card", "where", "user:owner-a", false)
	out, err := blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, BlockerUpdate{RequestID: "waiter-change", ExpectedVersion: b.Version, Operation: "assign_waiter", WaitingPrincipal: "agent:other"})
	if err != nil {
		t.Fatal(err)
	}
	again := blockerCreate(t, db, now, "same-current-waiter", "where", "agent:other", false)
	if again.ID != b.ID || again.Version != out.Blocker.Version {
		t.Fatal("BLOCKER_WAITER_CHANGE_CREATED_DUPLICATE", again, out)
	}
}

func TestBlockerReplyBudgetAndIdempotentReplay(t *testing.T) {
	db, now, _ := taskFlowFixture(t)
	b := blockerCreate(t, db, now, "budget", "waiting", "user:owner-a", false)
	var last BlockerUpdate
	for i := 0; i < blockerReplyPerMinute; i++ {
		last = BlockerUpdate{RequestID: fmt.Sprint("b", i), Operation: "input", ExpectedVersion: b.Version, Source: "user:owner-a", EventID: fmt.Sprint(i), Body: "answer", Evidence: "git:evidence"}
		out, err := blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, last)
		if err != nil {
			t.Fatal(err)
		}
		b = out.Blocker
	}
	replay, err := blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, last)
	if err != nil || !replay.Replayed {
		t.Fatal("BLOCKER_RATE_REPLAY_CHARGED", err)
	}
	next := BlockerUpdate{RequestID: "over", Operation: "input", ExpectedVersion: b.Version, Source: "user:owner-a", EventID: "over", Body: "answer", Evidence: "git:evidence"}
	if _, err = blockerApply(t, db, now, BlockerActor{UserID: "owner-a"}, b.ID, next); !errors.Is(err, ErrBlockerRateLimited) {
		t.Fatal("BLOCKER_REPLY_FREQUENCY_UNBOUNDED", err)
	}
	if got := blockerRead(t, db, b.ID); got.Version != b.Version {
		t.Fatal("BLOCKER_REJECT_CHANGED_STATE")
	}
	for i := blockerReplyPerMinute; i < blockerReplyTotal; i++ {
		next.RequestID = fmt.Sprint("total", i)
		next.EventID = next.RequestID
		next.ExpectedVersion = b.Version
		out, e := blockerApply(t, db, now.Add(time.Duration(i)*time.Minute), BlockerActor{UserID: "owner-a"}, b.ID, next)
		if e != nil {
			t.Fatal(e)
		}
		b = out.Blocker
	}
	next.RequestID = "exhausted"
	next.EventID = "exhausted"
	next.ExpectedVersion = b.Version
	if _, err = blockerApply(t, db, now.Add(24*time.Hour), BlockerActor{UserID: "owner-a"}, b.ID, next); !errors.Is(err, ErrBlockerRateLimited) {
		t.Fatal("BLOCKER_REPLY_TOTAL_UNBOUNDED", err)
	}
}

func TestBlockerAddressedWaiterGetsOnlyNeededContext(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	b := blockerCreate(t, db, now, "minimal", "where exactly", "user:outside", false)
	state, _ = db.ConsoleTaskFlowState(context.Background(), "task")
	cards, inputs, err := db.ReadTaskBlockers(context.Background(), state, BlockerActor{UserID: "outside"}, b.ID, "", 1, func() error { return nil })
	if err != nil || len(cards) != 1 || cards[0].Where != "where exactly" || cards[0].ExpectedInput != b.ExpectedInput || len(inputs) != 0 {
		t.Fatal("BLOCKER_WAITER_MINIMAL_CONTEXT", err, cards)
	}
	if cards[0].SpecID != "" || cards[0].SHA != "" || cards[0].Developer != "" || len(cards[0].Conditions) != 0 {
		t.Fatal("BLOCKER_WAITER_EXCESS_CONTEXT", cards)
	}
	if _, _, err = db.ReadTaskBlockers(context.Background(), state, BlockerActor{UserID: "stranger"}, b.ID, "", 1, func() error { return nil }); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("BLOCKER_WAITER_ADDRESS_NOT_ENFORCED", err)
	}
	if _, _, err = db.ReadTaskBlockers(context.Background(), state, BlockerActor{UserID: "outside"}, "", "", 1, func() error { return nil }); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("BLOCKER_WAITER_LISTED_OTHER_CARDS", err)
	}
}
