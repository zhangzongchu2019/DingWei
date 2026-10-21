package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func clarificationFixture(t *testing.T) (*SQLite, time.Time, PilotQuestion) {
	t.Helper()
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "requirement_unclear")
	// questionFixture's owner-selected PM is now the same current policy;
	// no second policy registration is needed merely to enter the new UI.
	return db, now, q
}
func clarificationApply(t *testing.T, db *SQLite, now time.Time, a BlockerActor, q PilotQuestion, u ClarificationUpdate) (ClarificationResult, error) {
	t.Helper()
	ctx := context.Background()
	c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	return db.AppendClarification(ctx, c, a, q.ID, u, true, func() error { return nil }, now)
}
func clarificationUpdate(id string) ClarificationUpdate {
	return ClarificationUpdate{RequestID: id, Kind: "explanation", EntryRefs: []string{"A1"}, Body: "维持原验收范围，本条补充边界解释"}
}

func TestClarificationAppendReplayAndEqualTextSupplement(t *testing.T) {
	db, now, q := clarificationFixture(t)
	pm := BlockerActor{UserID: "pm"}
	u := clarificationUpdate("one")
	first, err := clarificationApply(t, db, now, pm, q, u)
	if err != nil || first.Post.Floor != 1 || first.Post.Role != "pm" || first.Post.Evidence != "未提供证据" || first.ExecutionAuthorized {
		t.Fatal("CLARIFICATION_FIRST_POST_FAILED", first, err)
	}
	again, err := clarificationApply(t, db, now, pm, q, u)
	if err != nil || !again.Replayed || again.Post.Floor != first.Post.Floor {
		t.Fatal("CLARIFICATION_ID_REPLAY_DUPLICATED", again, err)
	}
	u.Body = "same id changed body"
	if _, err = clarificationApply(t, db, now, pm, q, u); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("CLARIFICATION_REPLAY_PAYLOAD_CHANGED", err)
	}
	u = clarificationUpdate("two")
	second, err := clarificationApply(t, db, now, pm, q, u)
	if err != nil || second.Replayed || second.Post.Floor != 2 || second.Post.Body != first.Post.Body {
		t.Fatal("CLARIFICATION_EQUAL_TEXT_SUPPLEMENT_LOST", second, err)
	}
	for table, want := range map[string]int{"pilot_question_answer": 2, "spec_clarification_post": 2, "spec_clarification_request": 2, "console_task_blocker_input": 2} {
		var n int
		if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Fatal("CLARIFICATION_APPEND_NOT_ATOMIC", table, n, err)
		}
	}
	var contentBefore, contentAfter, shaBefore, shaAfter string
	if err = db.db.QueryRow(`SELECT content_markdown,content_sha256 FROM console_spec_revision WHERE spec_id='r32' AND revision=1`).Scan(&contentBefore, &shaBefore); err != nil {
		t.Fatal(err)
	}
	u = clarificationUpdate("correction")
	u.Kind = "correction"
	u.Corrects = 1
	u.Reason = "更正第一条中一处表述"
	u.Body = "只追加更正，不改第一条"
	if _, err = clarificationApply(t, db, now, pm, q, u); err != nil {
		t.Fatal(err)
	}
	if err = db.db.QueryRow(`SELECT content_markdown,content_sha256 FROM console_spec_revision WHERE spec_id='r32' AND revision=1`).Scan(&contentAfter, &shaAfter); err != nil {
		t.Fatal(err)
	}
	if contentBefore != contentAfter || shaBefore != shaAfter {
		t.Fatal("CLARIFICATION_MODIFIED_FINAL_SPEC")
	}
	if _, err = db.db.Exec(`UPDATE spec_clarification_post SET role='pm' WHERE floor=1`); err == nil {
		t.Fatal("CLARIFICATION_METADATA_MUTABLE")
	}
	// Isolate the append-only triggers from redundant FK protection: a missing
	// trigger must not appear covered merely because request/post FKs reject it.
	if _, err = db.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(`DELETE FROM spec_clarification_post WHERE floor=1`); err == nil {
		t.Fatal("CLARIFICATION_POST_DELETABLE")
	}
	if _, err = db.db.Exec(`UPDATE pilot_question_answer SET body='rewritten' WHERE event_id=?`, first.Post.EventID); err == nil {
		t.Fatal("CLARIFICATION_BODY_MUTABLE")
	}
	if _, err = db.db.Exec(`DELETE FROM pilot_question_answer WHERE event_id=?`, first.Post.EventID); err == nil {
		t.Fatal("CLARIFICATION_BODY_DELETABLE")
	}
	if _, err = db.db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	var original string
	if err = db.db.QueryRow(`SELECT body FROM pilot_question_answer WHERE event_id=?`, first.Post.EventID).Scan(&original); err != nil || original != first.Post.Body {
		t.Fatal("CLARIFICATION_CORRECTION_OVERWROTE_BODY", original, err)
	}
}

func TestClarificationAuthorityCurrentPolicyAndDeputy(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	for _, a := range []BlockerActor{{UserID: "owner-a"}, {UserID: "leader"}, {AgentID: "dev"}, {AgentID: "review"}, {UserID: "stranger"}} {
		if _, err := clarificationApply(t, db, now, a, q, clarificationUpdate("impersonate-"+a.Key())); !errors.Is(err, ErrTaskFlowPermission) {
			t.Fatal("CLARIFICATION_NON_PM_AUTHORITY", a, err)
		}
	}
	c, err := db.QuestionAuthorizationContext(ctx, q.SpecID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	u := ClarificationPolicyUpdate{ExpectedVersion: 1, PMUser: "pm", DeputyUser: "deputy"}
	if _, err = db.SetClarificationPolicy(ctx, c, BlockerActor{UserID: "owner-a"}, u, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_OWNER_APPOINTED_DEPUTY", err)
	}
	p, err := db.SetClarificationPolicy(ctx, c, BlockerActor{UserID: "pm"}, u, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	post, err := clarificationApply(t, db, now, BlockerActor{UserID: "deputy"}, q, clarificationUpdate("deputy-one"))
	if err != nil || post.Post.Role != "deputy" {
		t.Fatal("CLARIFICATION_PREAPPOINTED_DEPUTY_REJECTED", post, err)
	}
	confirm := clarificationUpdate("pm-confirms")
	confirm.Kind = "confirmation"
	confirm.Confirms = post.Post.Floor
	if _, err = clarificationApply(t, db, now, BlockerActor{UserID: "deputy"}, q, confirm); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_DEPUTY_SELF_CONFIRMED", err)
	}
	if _, err = clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, confirm); err != nil {
		t.Fatal(err)
	}
	_, err = db.SetClarificationPolicy(ctx, c, BlockerActor{UserID: "pm"}, ClarificationPolicyUpdate{ExpectedVersion: p.Version, PMUser: "pm"}, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	// Even an idempotent replay must not recover rights after revocation.
	if _, err = clarificationApply(t, db, now, BlockerActor{UserID: "deputy"}, q, clarificationUpdate("deputy-one")); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_REVOKED_DEPUTY_REPLAY_READ", err)
	}
	_, err = db.SetClarificationPolicy(ctx, c, BlockerActor{UserID: "owner-a"}, ClarificationPolicyUpdate{ExpectedVersion: p.Version + 1, PMUser: "nextpm"}, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, clarificationUpdate("old-pm")); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_OLD_PM_KEPT_AUTHORITY", err)
	}
	if _, err = clarificationApply(t, db, now, BlockerActor{UserID: "nextpm"}, q, clarificationUpdate("new-pm")); err != nil {
		t.Fatal(err)
	}
	var role string
	if err = db.db.QueryRow(`SELECT role FROM spec_clarification_post WHERE floor=?`, post.Post.Floor).Scan(&role); err != nil || role != "deputy" {
		t.Fatal("CLARIFICATION_HISTORY_RECLASSIFIED", role, err)
	}
}

func TestClarificationTechnicalDoesNotBecomeAuthorityOrCompleteTask(t *testing.T) {
	db, now, q := clarificationFixture(t)
	u := clarificationUpdate("technical")
	u.Kind = "technical"
	out, err := clarificationApply(t, db, now, BlockerActor{AgentID: "dev"}, q, u)
	if err != nil || out.Post.Role != "technical" || out.ExecutionAuthorized {
		t.Fatal("CLARIFICATION_TECHNICAL_REJECTED", out, err)
	}
	var first, status string
	if err = db.db.QueryRow(`SELECT COALESCE(first_substantive_answer_at,'') FROM pilot_question WHERE id=?`, q.ID).Scan(&first); err != nil || first != "" {
		t.Fatal("CLARIFICATION_AGENT_BECAME_FIRST_AUTHORITY", first, err)
	}
	if err = db.db.QueryRow(`SELECT status FROM console_spec_task WHERE id='task'`).Scan(&status); err != nil || status != "planned" {
		t.Fatal("CLARIFICATION_TECHNICAL_STARTED_TASK", status, err)
	}
	if _, err = db.db.Exec(`UPDATE console_spec_task SET needs_reassignment=1 WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	u.RequestID = "revoked-agent"
	if _, err = clarificationApply(t, db, now, BlockerActor{AgentID: "dev"}, q, u); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_AGENT_ROLE_REVOKED_BYPASS", err)
	}
}

func TestClarificationFinalCheckAndAuditFailuresRollBackAll(t *testing.T) {
	for _, failure := range []string{"identity", "audit", "blocker"} {
		t.Run(failure, func(t *testing.T) {
			db, now, q := clarificationFixture(t)
			ctx := context.Background()
			c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
			if err != nil {
				t.Fatal(err)
			}
			check := func() error { return nil }
			sentinel := errors.New("injected current PG failure")
			if failure == "identity" {
				check = func() error { return sentinel }
			}
			if failure == "audit" {
				if _, err = db.db.Exec(`CREATE TRIGGER fail_clarification_audit BEFORE INSERT ON pilot_question_audit WHEN NEW.operation='clarification_append' BEGIN SELECT RAISE(ABORT,'injected audit'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "blocker" {
				if _, err = db.db.Exec(`CREATE TRIGGER fail_clarification_input BEFORE INSERT ON console_task_blocker_input BEGIN SELECT RAISE(ABORT,'injected blocker'); END`); err != nil {
					t.Fatal(err)
				}
			}
			u := clarificationUpdate("atomic")
			u.Kind = "scope_change"
			if _, err = db.AppendClarification(ctx, c, BlockerActor{UserID: "pm"}, q.ID, u, true, check, now); err == nil || failure == "identity" && !errors.Is(err, sentinel) {
				t.Fatal("CLARIFICATION_PARTIAL_COMMIT", err)
			}
			for _, table := range []string{"spec_clarification_post", "spec_clarification_request", "spec_clarification_floor", "pilot_question_answer", "pilot_question_change", "console_task_blocker_input", "console_task_blocker_outbox"} {
				var n int
				if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
					t.Fatal("CLARIFICATION_ATOMIC_ROLLBACK_MISSING", table, n, err)
				}
			}
		})
	}
}

func TestClarificationAnchorCorrectionScopeAndWriteFreeze(t *testing.T) {
	db, now, q := clarificationFixture(t)
	pm := BlockerActor{UserID: "pm"}
	ctx := context.Background()
	for _, u := range []ClarificationUpdate{
		{RequestID: "missing", Kind: "explanation", Body: "解释"},
		{RequestID: "unknown", Kind: "explanation", Body: "解释", EntryRefs: []string{"not-in-spec"}},
		{RequestID: "duplicate", Kind: "explanation", Body: "解释", EntryRefs: []string{"A1", "A1"}},
		{RequestID: "wrongfloor", Kind: "correction", Body: "解释", EntryRefs: []string{"A1"}, Corrects: 1, Reason: "no such floor"},
	} {
		if _, err := clarificationApply(t, db, now, pm, q, u); !errors.Is(err, ErrTaskFlowInvalid) {
			t.Fatal("CLARIFICATION_BAD_ANCHOR_OR_REFERENCE", u.RequestID, err)
		}
	}
	c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendClarification(ctx, c, pm, q.ID, clarificationUpdate("disabled"), false, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_DISABLED_WRITES", err)
	}
	u := clarificationUpdate("scope")
	u.Kind = "scope_change"
	if _, err = clarificationApply(t, db, now, pm, q, u); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.db.QueryRow(`SELECT count(*) FROM pilot_question_change WHERE state='pending'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("CLARIFICATION_SCOPE_CHANGE_MISSING_GATE", n, err)
	}
	if err = taskQuestionChangesClear(ctx, db.db, "task"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("CLARIFICATION_SCOPE_CHANGE_NOT_BLOCKING", err)
	}
}

func TestClarificationReplyBudgetAndReplayOrder(t *testing.T) {
	db, now, _ := clarificationFixture(t)
	// Use a real spec-only question: no D2 blocker exists to mask withdrawal
	// of the thread's own budget. Task-linked replies share the separate D2 gate.
	created, err := questionApply(t, db, now, BlockerActor{UserID: "owner-a"}, "", QuestionUpdate{
		RequestID: "spec-only-budget", ExpectedVersion: 1, Operation: "create", Kind: "requirement_unclear",
		Title: "spec 级问题", ObservedFact: "无关联任务", Question: "请解释 A1", ExpectedAnswer: "保持范围的解释",
		EntryRefs: []string{"A1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := created.Question
	pm := BlockerActor{UserID: "pm"}
	for i := 0; i < 10; i++ {
		if _, err := clarificationApply(t, db, now, pm, q, clarificationUpdate(fmt.Sprintf("budget-%d", i))); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := clarificationApply(t, db, now, pm, q, clarificationUpdate("budget-10")); !errors.Is(err, ErrBlockerRateLimited) {
		t.Fatal("CLARIFICATION_RATE_LIMIT_BYPASS", err)
	}
	if out, err := clarificationApply(t, db, now, pm, q, clarificationUpdate("budget-0")); err != nil || !out.Replayed {
		t.Fatal("CLARIFICATION_REPLAY_CHARGED_BUDGET", out, err)
	}
	if _, err := clarificationApply(t, db, now.Add(61*time.Second), pm, q, clarificationUpdate("budget-11")); err != nil {
		t.Fatal(err)
	}
}

func TestClarificationLegacySnapshotAnchorsAndHistory(t *testing.T) {
	db, now, q := clarificationFixture(t)
	if _, err := db.db.Exec(`UPDATE console_spec_revision SET acceptance_json='[]' WHERE spec_id='r32' AND revision=1`); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := db.db.QueryRow(`SELECT content_markdown FROM console_spec_revision WHERE spec_id='r32' AND revision=1`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	line := 0
	for i, s := range strings.Split(body, "\n") {
		if strings.TrimSpace(s) != "" {
			line = i + 1
			break
		}
	}
	if line == 0 {
		t.Fatal("fixture needs body")
	}
	u := clarificationUpdate("snapshot")
	u.EntryRefs = []string{fmt.Sprintf("snapshot:%s:line:%d", q.SHA, line)}
	if _, err := clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, u); err != nil {
		t.Fatal("CLARIFICATION_LEGACY_SNAPSHOT_REJECTED", err)
	}
	var n int
	if err := db.db.QueryRow(`SELECT count(*) FROM spec_clarification_anchor`).Scan(&n); err != nil || n == 0 {
		t.Fatal("CLARIFICATION_SNAPSHOT_NOT_PINNED", n, err)
	}
}

func TestClarificationLegacyAnswerPathsCannotBypassEnrollment(t *testing.T) {
	db, now, q := clarificationFixture(t)
	for _, actor := range []BlockerActor{{UserID: "pm"}, {UserID: "owner-a"}, {AgentID: "dev"}} {
		u := QuestionUpdate{RequestID: "old-" + actor.Key(), Operation: "answer", ExpectedVersion: q.Version, EventID: "old-event", AnswerKind: "substantive", Body: "old broad answer", Evidence: "git:evidence"}
		if actor.AgentID != "" {
			u.Operation = "input"
		}
		c, _ := db.QuestionAuthorizationContext(context.Background(), "", "", q.ID)
		if _, err := db.UpdateQuestion(context.Background(), c, actor, q.ID, u, func() error { return nil }, now); !errors.Is(err, ErrClarificationUpgradeRequired) {
			t.Fatal("CLARIFICATION_LEGACY_WRITE_BYPASS", actor, err)
		}
	}
	var n int
	if err := db.db.QueryRow(`SELECT count(*) FROM pilot_question_answer`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestClarificationContinuedResolvedDiscussionDoesNotReopen(t *testing.T) {
	db, now, q := clarificationFixture(t)
	if _, err := db.db.Exec(`UPDATE pilot_question SET state='resolved' WHERE id=?`, q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE console_task_blocker SET state='resolved' WHERE id=?`, q.BlockerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE console_spec_task SET status='done' WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	if _, err := clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, clarificationUpdate("continued")); err != nil {
		t.Fatal(err)
	}
	var state, task string
	if err := db.db.QueryRow(`SELECT state FROM pilot_question WHERE id=?`, q.ID).Scan(&state); err != nil || state != "resolved" {
		t.Fatal("CLARIFICATION_RESOLVED_REOPENED", state, err)
	}
	if err := db.db.QueryRow(`SELECT status FROM console_spec_task WHERE id='task'`).Scan(&task); err != nil || task != "done" {
		t.Fatal("CLARIFICATION_DONE_REOPENED", task, err)
	}
	var n int
	if err := db.db.QueryRow(`SELECT count(*) FROM console_task_blocker_outbox`).Scan(&n); err != nil || n != 0 {
		t.Fatal("CLARIFICATION_COMPLETED_TASK_WOKEN", n, err)
	}
}

func TestClarificationReadPaginationCurrentGrantAndPrivateHistory(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	// A legacy private answer has no new thread metadata and is not silently
	// promoted into a public authoritative post.
	if _, err := db.db.Exec(`INSERT INTO pilot_question_answer(question_id,generation,actor_id,event_id,payload_sha,answer_kind,body,evidence_ref,scope_change,created_at) VALUES(?,1,'user:owner-a','legacy','legacy','substantive','private historical material','evidence',0,?)`, q.ID, now.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, clarificationUpdate(fmt.Sprintf("page-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.ReadClarificationThread(ctx, c, BlockerActor{UserID: "pm"}, q.ID, 0, 2, func() error { return nil })
	if err != nil || len(page.Posts) != 2 || page.NextBefore != 2 || page.Posts[0].Floor != 3 || page.Posts[1].Floor != 2 {
		t.Fatal("CLARIFICATION_PAGINATION_INVALID", page, err)
	}
	next, err := db.ReadClarificationThread(ctx, c, BlockerActor{UserID: "pm"}, q.ID, page.NextBefore, 2, func() error { return nil })
	if err != nil || len(next.Posts) != 1 || next.Posts[0].Floor != 1 || next.NextBefore != 0 {
		t.Fatal("CLARIFICATION_PAGE_REPLAY_OR_LEAK", next, err)
	}
	for _, post := range append(page.Posts, next.Posts...) {
		if post.Body == "private historical material" {
			t.Fatal("CLARIFICATION_LEGACY_PRIVATE_HISTORY_LEAKED")
		}
	}
	for _, a := range []BlockerActor{{UserID: "stranger"}, {AgentID: "other"}} {
		if _, err = db.ReadClarificationThread(ctx, c, a, q.ID, 0, 2, func() error { return nil }); !errors.Is(err, ErrTaskFlowPermission) {
			t.Fatal("CLARIFICATION_THREAD_READ_LEAK", a, err)
		}
	}
	pc, err := db.QuestionAuthorizationContext(ctx, q.SpecID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetClarificationPolicy(ctx, pc, BlockerActor{UserID: "owner-a"}, ClarificationPolicyUpdate{ExpectedVersion: 1, PMUser: "nextpm"}, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadClarificationThread(ctx, c, BlockerActor{UserID: "pm"}, q.ID, 0, 2, func() error { return nil }); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_OLD_PM_READ_GRANT_SURVIVED", err)
	}
	denied := errors.New("identity changed")
	if got, err := db.ReadClarificationThread(ctx, c, BlockerActor{UserID: "nextpm"}, q.ID, 0, 2, func() error { return denied }); !errors.Is(err, denied) || len(got.Posts) != 0 {
		t.Fatal("CLARIFICATION_READ_CHECK_FAILED_OPEN", got, err)
	}
}

func TestClarificationPolicyFailureAndConcurrentRevocation(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	pc, err := db.QuestionAuthorizationContext(ctx, q.SpecID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	posted := make(chan error, 1)
	go func() {
		_, e := db.AppendClarification(ctx, c, BlockerActor{UserID: "pm"}, q.ID, clarificationUpdate("before-revoke"), true, func() error { close(entered); <-release; return nil }, now)
		posted <- e
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("append did not reach final check")
	}
	revoked := make(chan error, 1)
	go func() {
		_, e := db.SetClarificationPolicy(ctx, pc, BlockerActor{UserID: "owner-a"}, ClarificationPolicyUpdate{ExpectedVersion: 1, PMUser: "nextpm"}, func() error { return nil }, now)
		revoked <- e
	}()
	close(release)
	if err = <-posted; err != nil {
		t.Fatal(err)
	}
	if err = <-revoked; err != nil {
		t.Fatal(err)
	}
	if _, err = clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, clarificationUpdate("after-revoke")); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_REVOKE_ORDER_BROKEN", err)
	}
	failure := errors.New("PG principal inactive")
	if _, err = db.SetClarificationPolicy(ctx, pc, BlockerActor{UserID: "owner-a"}, ClarificationPolicyUpdate{ExpectedVersion: 2, PMUser: "thirdpm"}, func() error { return failure }, now); !errors.Is(err, failure) {
		t.Fatal("CLARIFICATION_POLICY_CHECK_IGNORED", err)
	}
	p, err := db.ClarificationPolicy(ctx, q.SpecID)
	if err != nil || p.PMUser != "nextpm" || p.Version != 2 {
		t.Fatal("CLARIFICATION_POLICY_PARTIAL_COMMIT", p, err)
	}
	var n int
	if err = db.db.QueryRow(`SELECT count(*) FROM spec_clarification_policy_history`).Scan(&n); err != nil || n != 2 {
		t.Fatal("CLARIFICATION_POLICY_HISTORY_PARTIAL_COMMIT", n, err)
	}
}
