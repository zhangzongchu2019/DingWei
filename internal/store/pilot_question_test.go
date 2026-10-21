package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"
)

func questionFixture(t *testing.T) (*SQLite, time.Time, QuestionContext) {
	t.Helper()
	db, now, _ := taskFlowFixture(t)
	c, err := db.QuestionAuthorizationContext(context.Background(), "r32", "task", "")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := db.QuestionAuthorizationContext(context.Background(), "r32", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetQuestionPolicy(context.Background(), spec, BlockerActor{UserID: "owner-a"}, QuestionUpdate{RequestID: "policy", PMUser: "pm", LeaderUser: "leader"}, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	return db, now, c
}
func questionApply(t *testing.T, db *SQLite, now time.Time, a BlockerActor, qid string, u QuestionUpdate) (QuestionResult, error) {
	t.Helper()
	c, err := db.QuestionAuthorizationContext(context.Background(), "r32", u.TaskID, qid)
	if err != nil {
		t.Fatal(err)
	}
	// Tests that exercise D3 lifecycle consequences now submit through the
	// approved typed append API, never through old substantive/input bypasses.
	if u.Operation == "answer" || u.Operation == "input" {
		kind := "explanation"
		if u.Operation == "input" {
			kind = "technical"
		} else if u.ScopeChange {
			kind = "scope_change"
		} else {
			var originalKind string
			if e := db.db.QueryRow(`SELECT kind FROM pilot_question WHERE id=?`, qid).Scan(&originalKind); e != nil {
				t.Fatal(e)
			}
			if originalKind == "change" {
				kind = "scope_change"
			}
		}
		post, e := db.AppendClarification(context.Background(), c, a, qid, ClarificationUpdate{RequestID: u.RequestID, Kind: kind, EntryRefs: []string{"A1"}, Body: u.Body, Evidence: u.Evidence}, true, func() error { return nil }, now)
		if e != nil {
			return QuestionResult{}, e
		}
		q, e := scanQuestion(db.db.QueryRow(`SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
		if e != nil {
			return QuestionResult{}, e
		}
		outcome := "recorded"
		if post.Replayed {
			outcome = "unchanged"
		}
		return QuestionResult{Question: q, Outcome: outcome, Replayed: post.Replayed}, nil
	}
	return db.UpdateQuestion(context.Background(), c, a, qid, u, func() error { return nil }, now)
}
func questionCreate(t *testing.T, db *SQLite, now time.Time, kind string) PilotQuestion {
	t.Helper()
	c, err := db.QuestionAuthorizationContext(context.Background(), "r32", "task", "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := db.UpdateQuestion(context.Background(), c, BlockerActor{AgentID: "dev"}, "", QuestionUpdate{RequestID: "create-" + kind, ExpectedVersion: c.Task.Version, Operation: "create", EntryRefs: []string{"A1"}, TaskID: "task", Kind: kind, Title: "需求问题", ObservedFact: "缺少明确事实", Question: "请给出可核对结论", ExpectedAnswer: "明确结论与证据"}, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	return out.Question
}
func TestPilotQuestionAnswerAtomicAndUsesOnlyD2Wake(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "requirement_unclear")
	if q.PMUser != "pm" || q.Responsible != "user:pm" || q.BlockerID == "" {
		t.Fatal(q)
	}
	u := QuestionUpdate{RequestID: "answer", ExpectedVersion: q.Version, Operation: "answer", EventID: "event", AnswerKind: "substantive", Body: "仅本次范围有明确结论，仍须核对", Evidence: "git:evidence"}
	out, err := questionApply(t, db, now.Add(time.Minute), BlockerActor{UserID: "pm"}, q.ID, u)
	if err != nil || out.Question.FirstSubstantiveAt == "" || out.ExecutionAuthorized {
		t.Fatal("QUESTION_ANSWER_CHAIN_FAILED", out, err)
	}
	for table, want := range map[string]int{"pilot_question_answer": 1, "console_task_blocker_input": 1, "console_task_blocker_outbox": 1} {
		var n int
		if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Fatal("QUESTION_D2_INPUT_NOT_ATOMIC", table, n, err)
		}
	}
	again, err := questionApply(t, db, now.Add(2*time.Minute), BlockerActor{UserID: "pm"}, q.ID, u)
	if err != nil || !again.Replayed {
		t.Fatal("QUESTION_REQUEST_REPLAY_FAILED", again, err)
	}
	var n int
	db.db.QueryRow(`SELECT count(*) FROM console_task_blocker_outbox`).Scan(&n)
	if n != 1 {
		t.Fatal("QUESTION_REPLY_DUPLICATED_WAKE", n)
	}
	var status string
	db.db.QueryRow(`SELECT status FROM console_spec_task WHERE id='task'`).Scan(&status)
	if status != "planned" {
		t.Fatal("QUESTION_ANSWER_STARTED_TASK", status)
	}
	u.RequestID = "answer"
	u.ExpectedVersion = out.Question.Version
	u.Body = "changed same event"
	if _, err = questionApply(t, db, now.Add(3*time.Minute), BlockerActor{UserID: "pm"}, q.ID, u); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("QUESTION_EVENT_REPLACED", err)
	}
}
func TestPilotQuestionNoPartialCommitAndAgentNotPM(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "technical")
	u := QuestionUpdate{RequestID: "reply", ExpectedVersion: q.Version, Operation: "answer", EventID: "event", AnswerKind: "substantive", Body: "有新技术结论", Evidence: "git:evidence"}
	if _, err := questionApply(t, db, now, BlockerActor{AgentID: "dev"}, q.ID, u); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("QUESTION_AGENT_IMPERSONATED_PM", err)
	}
	c, err := db.QuestionAuthorizationContext(context.Background(), "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected final PG check")
	if _, err = db.AppendClarification(context.Background(), c, BlockerActor{UserID: "pm"}, q.ID, ClarificationUpdate{RequestID: u.RequestID, Kind: "explanation", EntryRefs: []string{"A1"}, Body: u.Body, Evidence: u.Evidence}, true, func() error { return injected }, now); !errors.Is(err, injected) {
		t.Fatal("QUESTION_FINAL_CHECK_IGNORED", err)
	}
	for _, table := range []string{"pilot_question_answer", "console_task_blocker_input", "console_task_blocker_outbox"} {
		var n int
		if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("QUESTION_PARTIAL_ANSWER_COMMITTED", table, n, err)
		}
	}
	u.AnswerKind = "ack"
	u.Body = "收到"
	out, err := questionApply(t, db, now, BlockerActor{UserID: "pm"}, q.ID, u)
	if err != nil || out.Question.FirstSubstantiveAt != "" {
		t.Fatal("QUESTION_ACK_STOPPED_SLA", out, err)
	}
}

func questionCount(t *testing.T, db *SQLite, table string) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestPilotQuestionReminderExactlySixtyMinutesAndOncePerGeneration(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "technical")
	ctx := context.Background()
	c, _ := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	for _, at := range []time.Time{now.Add(59*time.Minute + 59*time.Second), now.Add(-time.Second)} {
		if err := db.QueueQuestionReminder(ctx, c, q.ID, func() error { return nil }, at); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	db.db.QueryRow(`SELECT count(*) FROM pilot_question_outbox WHERE kind='remind_60m'`).Scan(&n)
	if n != 0 {
		t.Fatal("QUESTION_SLA_EARLY_REMINDER", n)
	}
	for i := 0; i < 3; i++ {
		if err := db.QueueQuestionReminder(ctx, c, q.ID, func() error { return nil }, now.Add(time.Hour+time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	db.db.QueryRow(`SELECT count(*) FROM pilot_question_outbox WHERE kind='remind_60m'`).Scan(&n)
	if n != 1 {
		t.Fatal("QUESTION_SLA_DUPLICATE_REMINDER", n)
	}
	oldDue := q.DueAt
	out, err := questionApply(t, db, now.Add(61*time.Minute), BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "assign", ExpectedVersion: q.Version, Operation: "assign", PMUser: "pm2", LeaderUser: "leader2", Responsible: "user:pm2"})
	if err != nil || out.Question.DueAt != oldDue {
		t.Fatal("QUESTION_ASSIGN_RESET_SLA", out, err)
	}
	c, _ = db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err = db.QueueQuestionReminder(ctx, c, q.ID, func() error { return nil }, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	db.db.QueryRow(`SELECT count(*) FROM pilot_question_outbox WHERE kind='remind_60m'`).Scan(&n)
	if n != 1 {
		t.Fatal("QUESTION_ASSIGN_SECOND_SLA", n)
	}
	var recipient string
	db.db.QueryRow(`SELECT recipient_user FROM pilot_question_outbox WHERE kind='remind_60m'`).Scan(&recipient)
	if recipient != "leader2" {
		t.Fatal("QUESTION_PENDING_REMINDER_NOT_REBOUND", recipient)
	}
}
func TestPilotQuestionReplyFirstCancelsNoticeAndUnknownNeverRetries(t *testing.T) {
	for _, answered := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "reply_first"}[answered], func(t *testing.T) {
			db, now, _ := questionFixture(t)
			q := questionCreate(t, db, now, "requirement_unclear")
			ctx := context.Background()
			c, _ := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
			if err := db.QueueQuestionReminder(ctx, c, q.ID, func() error { return nil }, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			notices, err := db.PendingQuestionNotices(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			var notice QuestionNotice
			for _, n := range notices {
				if n.Kind == "remind_60m" {
					notice = n
				}
			}
			if notice.ID == "" {
				t.Fatal("no reminder")
			}
			if answered {
				if _, err = questionApply(t, db, now.Add(time.Hour), BlockerActor{UserID: "pm"}, q.ID, QuestionUpdate{RequestID: "answer", ExpectedVersion: q.Version, Operation: "answer", EventID: "reply", AnswerKind: "substantive", Body: "可验证的明确答复", Evidence: "git:answer"}); err != nil {
					t.Fatal(err)
				}
			}
			c, _ = db.QuestionAuthorizationContext(ctx, "", "", q.ID)
			_, ready, err := db.PrepareQuestionNotice(ctx, c, notice, true, func() error { return nil }, now.Add(time.Hour))
			if err != nil || ready == answered {
				t.Fatal("QUESTION_REPLY_FIRST_REMINDER_SENT", ready, err)
			}
			if !answered {
				// Crash after dispatch persisted and before provider receipt: never resend.
				_, ready, err = db.PrepareQuestionNotice(ctx, c, notice, true, func() error { return nil }, now.Add(61*time.Minute))
				if err != nil || ready {
					t.Fatal("QUESTION_UNKNOWN_NOTICE_RETRIED", ready, err)
				}
			}
			var state string
			db.db.QueryRow(`SELECT state FROM pilot_question_outbox WHERE id=?`, notice.ID).Scan(&state)
			want := "unknown"
			if answered {
				want = "cancelled"
			}
			if state != want {
				t.Fatal("QUESTION_NOTICE_STATE", state, want)
			}
		})
	}
}
func TestPilotQuestionUnconfiguredNeverClaimsSent(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "resource")
	ctx := context.Background()
	c, _ := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	ns, _ := db.PendingQuestionNotices(ctx, "")
	if len(ns) != 1 {
		t.Fatal(ns)
	}
	_, ready, err := db.PrepareQuestionNotice(ctx, c, ns[0], false, func() error { return nil }, now)
	if err != nil || ready {
		t.Fatal("QUESTION_UNCONFIGURED_SENT", ready, err)
	}
	var state string
	db.db.QueryRow(`SELECT state FROM pilot_question_outbox WHERE id=?`, ns[0].ID).Scan(&state)
	if state != "pending_config" {
		t.Fatal("QUESTION_UNCONFIGURED_FALSE_DELIVERY", state)
	}
	_ = q
}
func TestPilotQuestionDistinctTypedRepliesAppendAndCurrentPolicyRevokes(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "requirement_unclear")
	u := QuestionUpdate{RequestID: "first", ExpectedVersion: q.Version, Operation: "answer", EventID: "event1", AnswerKind: "substantive", Body: "明确实质答复", Evidence: "git:one"}
	out, err := questionApply(t, db, now.Add(time.Minute), BlockerActor{UserID: "pm"}, q.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	u.RequestID = "second"
	u.EventID = "event2"
	u.ExpectedVersion = out.Question.Version
	out, err = questionApply(t, db, now.Add(2*time.Minute), BlockerActor{UserID: "pm"}, q.ID, u)
	if err != nil || out.Outcome != "recorded" || questionCount(t, db, "pilot_question_answer") != 2 || questionCount(t, db, "console_task_blocker_input") != 2 || questionCount(t, db, "console_task_blocker_outbox") != 2 {
		t.Fatal("QUESTION_CONTENT_DUPLICATED_WAKE", out, err)
	}
	_, err = questionApply(t, db, now.Add(3*time.Minute), BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "reassign", ExpectedVersion: out.Question.Version, Operation: "assign", PMUser: "newpm", Responsible: "user:newpm", LeaderUser: "leader"})
	if err != nil {
		t.Fatal(err)
	}
	policyContext, _ := db.QuestionAuthorizationContext(context.Background(), q.SpecID, "", "")
	policy, _ := db.ClarificationPolicy(context.Background(), q.SpecID)
	if _, err = db.SetClarificationPolicy(context.Background(), policyContext, BlockerActor{UserID: "owner-a"}, ClarificationPolicyUpdate{ExpectedVersion: policy.Version, PMUser: "newpm"}, func() error { return nil }, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	c, _ := db.QuestionAuthorizationContext(context.Background(), "", "", q.ID)
	if _, _, _, err = db.ReadQuestion(context.Background(), c, BlockerActor{UserID: "pm"}, q.ID, 0, func() error { return nil }); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("QUESTION_FORMER_PM_READ", err)
	}
	u.RequestID = "first"
	u.EventID = "event1"
	u.ExpectedVersion = q.Version
	if _, err = questionApply(t, db, now.Add(4*time.Minute), BlockerActor{UserID: "pm"}, q.ID, u); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("QUESTION_REPLAY_LEAKS_FORMER_PM", err)
	}
}
func TestPilotQuestionBusinessChangeBlocksAllOldScopeAndNeedsHumanDecision(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "change")
	ctx := context.Background()
	out, err := questionApply(t, db, now.Add(time.Minute), BlockerActor{UserID: "pm"}, q.ID, QuestionUpdate{RequestID: "business", ExpectedVersion: q.Version, Operation: "answer", EventID: "event", AnswerKind: "substantive", Body: "业务范围须追加验收条件", Evidence: "git:change"})
	if err != nil {
		t.Fatal(err)
	}
	if err = taskQuestionChangesClear(ctx, db.db, "task"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("QUESTION_PENDING_CHANGE_EXECUTION_BYPASS", err)
	}
	var cid string
	db.db.QueryRow(`SELECT id FROM pilot_question_change WHERE question_id=?`, q.ID).Scan(&cid)
	if _, err = questionApply(t, db, now.Add(2*time.Minute), BlockerActor{UserID: "pm"}, q.ID, QuestionUpdate{RequestID: "pm-decide", ExpectedVersion: out.Question.Version, Operation: "reject_change", ChangeID: cid, Reason: "PM不能代替所有者裁定"}); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("QUESTION_PM_OWNER_DECISION", err)
	}
	if _, err = questionApply(t, db, now.Add(3*time.Minute), BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "owner-decide", ExpectedVersion: out.Question.Version, Operation: "reject_change", ChangeID: cid, Reason: "维持本轮已确认范围，另立后续项"}); err != nil {
		t.Fatal(err)
	}
	if err = taskQuestionChangesClear(ctx, db.db, "task"); err != nil {
		t.Fatal("QUESTION_REJECTED_CHANGE_STILL_BLOCKS", err)
	}
	var current int
	db.db.QueryRow(`SELECT current_revision FROM console_spec WHERE id='r32'`).Scan(&current)
	if current != q.Revision {
		t.Fatal("QUESTION_ANSWER_OVERWROTE_SPEC", current)
	}
}
func TestPilotQuestionReadGrantIsOnlyQuestionAndHistoryBounded(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "technical")
	ctx := context.Background()
	c, _ := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if _, _, _, err := db.ReadQuestion(ctx, c, BlockerActor{UserID: "outsider"}, q.ID, 0, func() error { return nil }); !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("QUESTION_UNGRANTED_READ", err)
	}
	ids, err := db.QuestionCandidateIDs(ctx, BlockerActor{AgentID: "outsider"}, "", "", 100)
	if err != nil || len(ids) != 0 {
		t.Fatal("QUESTION_BLANK_ID_LIST_LEAK", ids, err)
	}
	for i := 0; i < 103; i++ {
		_, err = db.db.Exec(`INSERT INTO pilot_question_answer(question_id,generation,actor_id,event_id,payload_sha,answer_kind,body,evidence_ref,scope_change,created_at) VALUES(?,1,'user:pm',?,?,'substantive','body','git:ref',0,?)`, q.ID, fmt.Sprint(i), fmt.Sprint(i), now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
	got, answers, _, err := db.ReadQuestion(ctx, c, BlockerActor{UserID: "pm"}, q.ID, 0, func() error { return nil })
	if err != nil || len(answers) != 101 || got.CanManage || got.CanConfigureRecipients || got.LeaderUser != "" {
		t.Fatal("QUESTION_MINIMUM_GRANT_OR_HISTORY_BOUND", got, len(answers), err)
	}
	_, older, _, err := db.ReadQuestion(ctx, c, BlockerActor{UserID: "pm"}, q.ID, answers[100].Sequence, func() error { return nil })
	if err != nil || len(older) != 2 {
		t.Fatal("QUESTION_HISTORY_PAGE_LOST", len(older), err)
	}
}
func TestPilotQuestionReassignRevokesSharedD2WaiterGrant(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "requirement_unclear")
	if _, err := questionApply(t, db, now, BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "move", ExpectedVersion: q.Version, Operation: "assign", PMUser: "newpm", Responsible: "user:newpm", LeaderUser: "leader"}); err != nil {
		t.Fatal(err)
	}
	state, err := db.ConsoleTaskFlowState(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	cards, _, err := db.ReadTaskBlockers(context.Background(), state, BlockerActor{AgentID: "dev"}, q.BlockerID, "", 10, func() error { return nil })
	if err != nil || len(cards) != 1 {
		t.Fatal(err, cards)
	}
	if cards[0].WaitingPrincipal != "user:newpm" {
		t.Fatal("QUESTION_REASSIGN_RETAINED_D2_WAITER", cards[0].WaitingPrincipal)
	}
	_, err = db.UpdateTaskBlocker(context.Background(), state, BlockerActor{UserID: "pm"}, q.BlockerID, BlockerUpdate{RequestID: "old-input", ExpectedVersion: cards[0].Version, Operation: "input", EventID: "old", Body: "旧联系人无权追加", Evidence: "git:old"}, func() error { return nil }, now)
	if !errors.Is(err, ErrTaskFlowPermission) {
		t.Fatal("QUESTION_OLD_PM_D2_INPUT_ALLOWED", err)
	}
}
func TestPilotQuestionMigrationRollbackAndReapply(t *testing.T) {
	db, _, _ := taskFlowFixture(t)
	ctx := context.Background()
	for _, name := range []string{"pilot_question_answer", "pilot_question_event", "pilot_question_request", "pilot_question_outbox", "pilot_question_change", "pilot_question_audit", "pilot_question", "pilot_question_policy"} {
		if _, err := db.db.Exec(`DROP TABLE ` + name); err != nil {
			t.Fatal(err)
		}
	}
	name := "0059_pilot_questions.sql"
	if _, err := db.db.Exec(`DELETE FROM schema_migrations WHERE version=?`, name); err != nil {
		t.Fatal(err)
	}
	raw, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if err = db.applyMigration(ctx, name, string(raw)+";BAD_QUESTION_MIGRATION;", digest); err == nil {
		t.Fatal("QUESTION_BAD_MIGRATION_ACCEPTED")
	}
	var tables, ledger int
	db.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'pilot_question%'`).Scan(&tables)
	db.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=?`, name).Scan(&ledger)
	if tables != 0 || ledger != 0 {
		t.Fatal("QUESTION_PARTIAL_MIGRATION", tables, ledger)
	}
	if err = db.applyMigration(ctx, name, string(raw), digest); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'pilot_question%'`).Scan(&tables)
	if tables != 8 {
		t.Fatal("QUESTION_MIGRATION_TABLES", tables)
	}
}
func TestPilotQuestionCASAndClockRollbackFailClosed(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "technical")
	u := QuestionUpdate{RequestID: "bad-version", ExpectedVersion: q.Version + 1, Operation: "answer", EventID: "event", AnswerKind: "substantive", Body: "真实答复", Evidence: "git:one"}
	c, _ := db.QuestionAuthorizationContext(context.Background(), "", "", q.ID)
	if _, err := db.UpdateQuestion(context.Background(), c, BlockerActor{UserID: "pm"}, q.ID, u, func() error { return nil }, now); !errors.Is(err, ErrClarificationUpgradeRequired) {
		t.Fatal("QUESTION_UNTYPED_LEGACY_ACCEPTED", err)
	}
	u.RequestID = "clock"
	u.ExpectedVersion = q.Version
	if _, err := questionApply(t, db, now.Add(-time.Second), BlockerActor{UserID: "pm"}, q.ID, u); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("QUESTION_CLOCK_ROLLBACK_ANSWER", err)
	}
}
func TestPilotQuestionProductionFullAndAckDoesNotStopSLA(t *testing.T) {
	db, now, c := questionFixture(t)
	out, err := db.UpdateQuestion(context.Background(), c, BlockerActor{AgentID: "dev"}, "", QuestionUpdate{RequestID: "risk", ExpectedVersion: c.Task.Version, Operation: "create", EntryRefs: []string{"A1"}, TaskID: "task", Kind: "resource", Title: "生产事项", ObservedFact: "涉及生产资源", Question: "明确授权", ExpectedAnswer: "正式授权证据", ProductionOrSecurity: true}, func() error { return nil }, now)
	if err != nil || out.Question.Mode != "full" {
		t.Fatal("QUESTION_PRODUCTION_NOT_FULL", out, err)
	}
	q := out.Question
	out, err = questionApply(t, db, now.Add(time.Minute), BlockerActor{UserID: "pm"}, q.ID, QuestionUpdate{RequestID: "ack-as-substantive", ExpectedVersion: q.Version, Operation: "answer", EventID: "ack", AnswerKind: "substantive", Body: "收到", Evidence: "git:received"})
	if err != nil || out.Question.FirstSubstantiveAt != "" || questionCount(t, db, "console_task_blocker_input") != 0 {
		t.Fatal("QUESTION_ACK_TREATED_AS_SUBSTANTIVE", out, err)
	}
	var mode string
	db.db.QueryRow(`SELECT mode FROM console_task_blocker WHERE id=?`, q.BlockerID).Scan(&mode)
	if mode != "full" {
		t.Fatal("QUESTION_RISK_D2_MODE_DOWNGRADED", mode)
	}
}
func TestPilotQuestionSpecOnlyAndCompletedTaskNeverWake(t *testing.T) {
	for _, done := range []bool{false, true} {
		t.Run(fmt.Sprint(done), func(t *testing.T) {
			db, now, _ := questionFixture(t)
			ctx := context.Background()
			task := ""
			if done {
				task = "task"
				if _, err := db.db.Exec(`UPDATE console_spec_task SET status='done' WHERE id='task'`); err != nil {
					t.Fatal(err)
				}
			}
			c, err := db.QuestionAuthorizationContext(ctx, "r32", task, "")
			if err != nil {
				t.Fatal(err)
			}
			v := c.Revision
			if done {
				v = c.Task.Version
			}
			out, err := db.UpdateQuestion(ctx, c, BlockerActor{UserID: "owner-a"}, "", QuestionUpdate{RequestID: "post", ExpectedVersion: v, Operation: "create", EntryRefs: []string{"A1"}, TaskID: task, Kind: "technical", Title: "后续问题", ObservedFact: "后续事实", Question: "后续澄清", ExpectedAnswer: "独立答复"}, func() error { return nil }, now)
			if err != nil || out.Question.BlockerID != "" {
				t.Fatal("QUESTION_POST_COMPLETION_CREATED_BLOCKER", out, err)
			}
			out, err = questionApply(t, db, now, BlockerActor{UserID: "pm"}, out.Question.ID, QuestionUpdate{RequestID: "answer", ExpectedVersion: out.Question.Version, Operation: "answer", EventID: "post", AnswerKind: "substantive", Body: "后续明确结论", Evidence: "git:one"})
			if err != nil {
				t.Fatal(err)
			}
			if questionCount(t, db, "console_task_blocker_outbox") != 0 || questionCount(t, db, "console_task_blocker") != 0 {
				t.Fatal("QUESTION_SPEC_OR_DONE_AUTO_WAKE")
			}
			if done {
				var status string
				db.db.QueryRow(`SELECT status FROM console_spec_task WHERE id='task'`).Scan(&status)
				if status != "done" {
					t.Fatal("QUESTION_COMPLETED_TASK_REOPENED", status)
				}
			}
		})
	}
}
func TestPilotQuestionPMDelegationLimitedToExistingQuestionReaders(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "technical")
	for _, u := range []QuestionUpdate{
		{RequestID: "external", ExpectedVersion: q.Version, Operation: "assign", PMUser: "pm", LeaderUser: "leader", Responsible: "user:outsider"},
		{RequestID: "pm-replace", ExpectedVersion: q.Version, Operation: "assign", PMUser: "newpm", LeaderUser: "leader", Responsible: "agent:dev"},
		{RequestID: "leader-replace", ExpectedVersion: q.Version, Operation: "assign", PMUser: "pm", LeaderUser: "newleader", Responsible: "agent:dev"},
	} {
		if _, err := questionApply(t, db, now, BlockerActor{UserID: "pm"}, q.ID, u); !errors.Is(err, ErrTaskFlowPermission) {
			t.Fatal("QUESTION_PM_GRANTED_NEW_RIGHTS", err)
		}
	}
	out, err := questionApply(t, db, now, BlockerActor{UserID: "pm"}, q.ID, QuestionUpdate{RequestID: "delegate", ExpectedVersion: q.Version, Operation: "assign", PMUser: "pm", LeaderUser: "", Responsible: "agent:dev"})
	if err != nil || out.Question.Responsible != "agent:dev" {
		t.Fatal("QUESTION_PM_TECHNICAL_DELEGATION", out, err)
	}
	out, err = questionApply(t, db, now.Add(time.Minute), BlockerActor{AgentID: "dev"}, q.ID, QuestionUpdate{RequestID: "technical", ExpectedVersion: out.Question.Version, Operation: "input", EventID: "facts", Body: "实际技术事实与证据", Evidence: "git:tech"})
	if err != nil || out.Question.FirstSubstantiveAt != "" {
		t.Fatal("QUESTION_DELEGATED_TECHNICAL_BECAME_AUTHORITY", out, err)
	}
}
func TestPilotQuestionSentReminderReassignmentNeverCreatesSecondReminder(t *testing.T) {
	db, now, _ := questionFixture(t)
	ctx := context.Background()
	q := questionCreate(t, db, now, "technical")
	c, _ := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err := db.QueueQuestionReminder(ctx, c, q.ID, func() error { return nil }, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ns, _ := db.PendingQuestionNotices(ctx, "")
	for _, n := range ns {
		if n.Kind == "remind_60m" {
			_, ready, err := db.PrepareQuestionNotice(ctx, c, n, true, func() error { return nil }, now.Add(time.Hour))
			if err != nil || !ready {
				t.Fatal(err, ready)
			}
			if err = db.FinishQuestionNotice(ctx, n.ID, "provider", true, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := questionApply(t, db, now.Add(61*time.Minute), BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "reassign-after-sent", ExpectedVersion: q.Version, Operation: "assign", PMUser: "pm", LeaderUser: "newleader", Responsible: "user:pm"}); err != nil {
		t.Fatal(err)
	}
	c, _ = db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err := db.QueueQuestionReminder(ctx, c, q.ID, func() error { return nil }, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	db.db.QueryRow(`SELECT count(*) FROM pilot_question_outbox WHERE kind='remind_60m'`).Scan(&count)
	if count != 1 {
		t.Fatal("QUESTION_ASSIGN_SECOND_SLA", count)
	}
}
func TestPilotQuestionSpecOnlyChangeCanLinkActualNewRevision(t *testing.T) {
	db, now, _ := questionFixture(t)
	ctx := context.Background()
	c, _ := db.QuestionAuthorizationContext(ctx, "r32", "", "")
	out, err := db.UpdateQuestion(ctx, c, BlockerActor{UserID: "owner-a"}, "", QuestionUpdate{RequestID: "spec-change", ExpectedVersion: c.Revision, Operation: "create", EntryRefs: []string{"A1"}, Kind: "change", Title: "需变更", ObservedFact: "原范围缺口", Question: "请明确新范围", ExpectedAnswer: "明确新增条件"}, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	q := out.Question
	out, err = questionApply(t, db, now, BlockerActor{UserID: "pm"}, q.ID, QuestionUpdate{RequestID: "proposal", ExpectedVersion: q.Version, Operation: "answer", EventID: "proposal", AnswerKind: "substantive", Body: "新增明确条件", Evidence: "git:proposal"})
	if err != nil {
		t.Fatal(err)
	}
	var cid string
	db.db.QueryRow(`SELECT id FROM pilot_question_change WHERE question_id=?`, q.ID).Scan(&cid)
	// Disposable fixture simulates the existing R8 human-confirmed revision path.
	if _, err = db.db.Exec(`INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_by_kind,created_via,supersedes_revision,created_at) SELECT spec_id,2,content_markdown,acceptance_json,input_anchors_json,'new-sha',created_by,created_by_kind,created_via,revision,created_at FROM console_spec_revision WHERE spec_id='r32' AND revision=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(`UPDATE console_spec SET current_revision=2 WHERE id='r32'`); err != nil {
		t.Fatal(err)
	}
	out, err = questionApply(t, db, now, BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "confirm", ExpectedVersion: out.Question.Version, Operation: "confirm_change", ChangeID: cid, NewRevision: 2, Reason: "已通过原R8流程形成真实新修订"})
	if err != nil || out.Question.State != "superseded" {
		t.Fatal("QUESTION_SPEC_CHANGE_NEW_REVISION_DEADLOCK", out, err)
	}
	if err = taskQuestionChangesClear(ctx, db.db, "task"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("QUESTION_CONFIRMED_NEW_SCOPE_STARTED_OLD_TASK", err)
	}
}
func TestPilotQuestionCancelPreservesPendingBusinessProposal(t *testing.T) {
	db, now, _ := questionFixture(t)
	ctx := context.Background()
	q := questionCreate(t, db, now, "change")
	out, err := questionApply(t, db, now, BlockerActor{UserID: "pm"}, q.ID, QuestionUpdate{RequestID: "proposal", ExpectedVersion: q.Version, Operation: "answer", EventID: "change", AnswerKind: "substantive", Body: "必须确认业务变更", Evidence: "git:change"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(`UPDATE console_spec_task SET needs_reassignment=1 WHERE id='task'`); err != nil {
		t.Fatal(err)
	}
	out, err = questionApply(t, db, now, BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "cancel", ExpectedVersion: out.Question.Version, Operation: "cancel", Reason: "指派失效，取消旧问题等待但保留业务变更待决"})
	if err != nil || out.Question.State != "cancelled" {
		t.Fatal("QUESTION_OWNER_CANCEL_SCOPE_DEADLOCK", out, err)
	}
	if err = taskQuestionChangesClear(ctx, db.db, "task"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("QUESTION_CANCEL_DROPPED_PENDING_CHANGE_GATE", err)
	}
}

func TestPilotQuestionRecipientCaseCanonicalBeforeHash(t *testing.T) {
	db, now, c := questionFixture(t)
	spec, err := db.QuestionAuthorizationContext(context.Background(), c.SpecID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	u := QuestionUpdate{RequestID: "case-policy", ExpectedVersion: 1, PMUser: "PM", LeaderUser: "LEADER"}
	policy, err := db.SetQuestionPolicy(context.Background(), spec, BlockerActor{UserID: "owner-a"}, u, func() error { return nil }, now)
	if err != nil || policy.PMUser != "pm" || policy.LeaderUser != "leader" {
		t.Fatal("QUESTION_STORE_POLICY_CASE", policy, err)
	}
	u.PMUser = "pm"
	u.LeaderUser = "leader"
	if _, err = db.SetQuestionPolicy(context.Background(), spec, BlockerActor{UserID: "owner-a"}, u, func() error { return nil }, now); err != nil {
		t.Fatal("QUESTION_POLICY_CASE_CHANGED_REPLAY_HASH", err)
	}
	q := questionCreate(t, db, now, "technical")
	out, err := questionApply(t, db, now, BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "case-assign", Operation: "assign", ExpectedVersion: q.Version, PMUser: "PM", LeaderUser: "LEADER"})
	if err != nil || out.Question.PMUser != "pm" || out.Question.LeaderUser != "leader" {
		t.Fatal("QUESTION_STORE_ASSIGN_CASE", out, err)
	}
}

func TestPilotQuestionOldDispatchCannotResetToUnsent(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "technical")
	ctx := context.Background()
	ns, err := db.PendingQuestionNotices(ctx, "")
	if err != nil || len(ns) != 1 {
		t.Fatal(err, ns)
	}
	n := ns[0]
	c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ready, e := db.PrepareQuestionNotice(ctx, c, n, true, func() error { return nil }, now); e != nil || !ready {
		t.Fatal(e, ready)
	}
	if err = db.DeferUnsentQuestionNotice(ctx, n, "pending_config", false, now); err != nil {
		t.Fatal(err)
	}
	var state string
	db.db.QueryRow(`SELECT state FROM pilot_question_outbox WHERE id=?`, n.ID).Scan(&state)
	if state != "unknown" {
		t.Fatal("QUESTION_OLD_DISPATCH_RESET_AS_UNSENT", state)
	}
	if _, ready, e := db.PrepareQuestionNotice(ctx, c, n, true, func() error { return nil }, now); e != nil || ready {
		t.Fatal("QUESTION_OLD_DISPATCH_RETRIED", e, ready)
	}
	if err = db.DeferUnsentQuestionNotice(ctx, n, "queued", true, now); err != nil {
		t.Fatal(err)
	}
	db.db.QueryRow(`SELECT state FROM pilot_question_outbox WHERE id=?`, n.ID).Scan(&state)
	if state != "unknown" {
		t.Fatal("QUESTION_UNKNOWN_RESET_AS_UNSENT", state)
	}
}
