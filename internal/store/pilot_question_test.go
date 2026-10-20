package store

import (
	"context"
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
	return db.UpdateQuestion(context.Background(), c, a, qid, u, func() error { return nil }, now)
}
func questionCreate(t *testing.T, db *SQLite, now time.Time, kind string) PilotQuestion {
	t.Helper()
	c, err := db.QuestionAuthorizationContext(context.Background(), "r32", "task", "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := db.UpdateQuestion(context.Background(), c, BlockerActor{AgentID: "dev"}, "", QuestionUpdate{RequestID: "create-" + kind, ExpectedVersion: c.Task.Version, Operation: "create", TaskID: "task", Kind: kind, Title: "需求问题", ObservedFact: "缺少明确事实", Question: "请给出可核对结论", ExpectedAnswer: "明确结论与证据"}, func() error { return nil }, now)
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
	u.RequestID = "changed"
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
	if _, err = db.UpdateQuestion(context.Background(), c, BlockerActor{UserID: "pm"}, q.ID, u, func() error { return injected }, now); !errors.Is(err, injected) {
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
func TestPilotQuestionSameContentNewEventDeduplicatesAndRoleRevokes(t *testing.T) {
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
	if err != nil || out.Outcome != "unchanged" || questionCount(t, db, "pilot_question_answer") != 1 || questionCount(t, db, "console_task_blocker_input") != 1 || questionCount(t, db, "console_task_blocker_outbox") != 1 {
		t.Fatal("QUESTION_CONTENT_DUPLICATED_WAKE", out, err)
	}
	_, err = questionApply(t, db, now.Add(3*time.Minute), BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "reassign", ExpectedVersion: out.Question.Version, Operation: "assign", PMUser: "newpm", Responsible: "user:newpm", LeaderUser: "leader"})
	if err != nil {
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
	if err != nil || len(answers) != 101 || got.CanManage || got.CanAssign || got.LeaderUser != "" {
		t.Fatal("QUESTION_MINIMUM_GRANT_OR_HISTORY_BOUND", got, len(answers), err)
	}
	_, older, _, err := db.ReadQuestion(ctx, c, BlockerActor{UserID: "pm"}, q.ID, answers[100].Sequence, func() error { return nil })
	if err != nil || len(older) != 2 {
		t.Fatal("QUESTION_HISTORY_PAGE_LOST", len(older), err)
	}
}
