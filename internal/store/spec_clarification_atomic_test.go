package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func clarificationNoticeFixture(t *testing.T) (*SQLite, time.Time, QuestionContext, PilotQuestion, QuestionNotice, ClarificationNotice) {
	t.Helper()
	db, now, _ := clarificationFixture(t)
	ctx := context.Background()
	c, err := db.QuestionAuthorizationContext(ctx, "r32", "task", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := questionApply(t, db, now, BlockerActor{AgentID: "dev"}, "", QuestionUpdate{RequestID: "anchored-native", ExpectedVersion: c.Task.Version, TaskID: "task", Operation: "create", Kind: "requirement_unclear", Title: "范围", ObservedFact: "事实", Question: "解释A1", ExpectedAnswer: "边界", EntryRefs: []string{"A1"}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Question
	page, err := db.PendingQuestionNotices(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var notice QuestionNotice
	for _, n := range page {
		if n.QuestionID == q.ID && n.Kind == "question" {
			notice = n
		}
	}
	if notice.ID == "" {
		t.Fatal("fixture notice missing")
	}
	c, err = db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ready, err := db.PrepareQuestionNotice(ctx, c, notice, true, func() error { return nil }, now); err != nil || !ready {
		t.Fatal(ready, err)
	}
	binding := ClarificationBinding{Issuer: "bot", Tenant: "tenant", AppID: "app", Subject: "ou_pm", BindingSHA: strings.Repeat("b", 64)}
	snapshot, err := db.PrepareClarificationNotice(ctx, c, notice, binding, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	return db, now, c, q, notice, snapshot
}

func TestClarificationNoticeFinishIsAtomicWithProviderParent(t *testing.T) {
	db, now, _, _, n, snapshot := clarificationNoticeFixture(t)
	ctx := context.Background()
	if _, err := db.db.Exec(`CREATE TRIGGER fail_parent_map BEFORE UPDATE ON spec_clarification_notice WHEN NEW.state='sent' BEGIN SELECT RAISE(ABORT,'injected parent persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishQuestionNotice(ctx, n.ID, "om_parent", true, now); err == nil {
		t.Fatal("CLARIFICATION_PARENT_FAILURE_NOT_ATOMIC")
	}
	var state, id string
	if err := db.db.QueryRow(`SELECT state,provider_message_id FROM pilot_question_outbox WHERE id=?`, n.ID).Scan(&state, &id); err != nil || state != "dispatching" || id != "" {
		t.Fatal("CLARIFICATION_OUTBOX_SENT_WITHOUT_PARENT", state, id, err)
	}
	if err := db.db.QueryRow(`SELECT state,provider_message_id FROM spec_clarification_notice WHERE notice_id=?`, n.ID).Scan(&state, &id); err != nil || state != "prepared" || id != "" {
		t.Fatal("CLARIFICATION_PARTIAL_PARENT_MAP", state, id, err)
	}
	if _, err := db.db.Exec(`DROP TRIGGER fail_parent_map`); err != nil {
		t.Fatal(err)
	}
	// Retrying RECEIPT persistence, not a second provider send.
	if err := db.FinishQuestionNotice(ctx, n.ID, "om_parent", true, now); err != nil {
		t.Fatal(err)
	}
	got, err := db.ClarificationNoticeByParent(ctx, "bot", "tenant", "om_parent")
	if err != nil || got.NoticeID != snapshot.NoticeID || got.State != "sent" {
		t.Fatal("CLARIFICATION_EXACT_PARENT_NOT_PERSISTED", got, err)
	}
}

func TestClarificationInboundPostRollbackAndUncertainReceiptNoRetry(t *testing.T) {
	db, now, c, q, n, _ := clarificationNoticeFixture(t)
	ctx := context.Background()
	if err := db.FinishQuestionNotice(ctx, n.ID, "om_parent", true, now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.ClarificationNoticeByParent(ctx, "bot", "tenant", "om_parent")
	if err != nil {
		t.Fatal(err)
	}
	message := model.Message{BotChannelID: "bot", SenderOpenID: "ou_pm", FeishuMsgID: "om_reply", ChatType: model.ChatPersonal, IngressProvenance: model.IngressFeishuWSAuthenticated, FeishuReply: &model.FeishuReplyMetadata{Authenticated: true, ContentSHA: strings.Repeat("a", 64), EventID: "event", MessageID: "om_reply", ParentID: "om_parent", RootID: "om_parent", MessageType: "text", Tenant: "tenant", AppID: "app", ChatID: "oc_dm", SenderType: "user"}}
	in, err := db.StageClarificationInbound(ctx, message, "无前缀的解释", now)
	if err != nil {
		t.Fatal(err)
	}
	u := ClarificationUpdate{Kind: "explanation", Body: in.Body}
	checks := 0
	failure := errors.New("last binding check failed")
	if _, err = db.RecordFeishuClarification(ctx, c, snapshot, snapshot.Binding, BlockerActor{UserID: "pm"}, in, u, true, func() error {
		checks++
		if checks == 2 {
			return failure
		}
		return nil
	}, now); !errors.Is(err, failure) {
		t.Fatal("CLARIFICATION_LAST_BINDING_CHECK_IGNORED", err)
	}
	for _, table := range []string{"pilot_question_answer", "spec_clarification_post", "spec_clarification_request", "console_task_blocker_input", "console_task_blocker_outbox"} {
		var count int
		if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("CLARIFICATION_INBOUND_PARTIAL_COMMIT", table, count, err)
		}
	}
	pending, err := readClarificationInbound(ctx, db.db, "bot", "tenant", "om_reply")
	if err != nil || pending.State != "pending_match" || pending.Floor != 0 {
		t.Fatal("CLARIFICATION_INBOUND_LEDGER_PARTIAL_COMMIT", pending.State, pending.Floor, err)
	}
	result, err := db.RecordFeishuClarification(ctx, c, snapshot, snapshot.Binding, BlockerActor{UserID: "pm"}, in, u, true, func() error { return nil }, now)
	if err != nil || result.Post.QuestionID != q.ID || result.Post.Source != "feishu" {
		t.Fatal("CLARIFICATION_INBOUND_RETRY_FAILED", result, err)
	}
	if claimed, err := db.ClaimClarificationReceipt(ctx, in); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err = db.FinishClarificationReceipt(ctx, in, false); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimClarificationReceipt(ctx, in); err != nil || claimed {
		t.Fatal("CLARIFICATION_UNCERTAIN_RECEIPT_RETRIED", claimed, err)
	}
	got, err := readClarificationInbound(ctx, db.db, "bot", "tenant", "om_reply")
	if err != nil || got.State != "recorded" || got.ReceiptState != "unknown" {
		t.Fatal("CLARIFICATION_RECEIPT_STATE_LOST", got.State, got.ReceiptState, err)
	}
}

func TestClarificationFreshGenerationPinsCurrentDeputyPolicy(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	c, e := db.QuestionAuthorizationContext(ctx, q.SpecID, "", "")
	if e != nil {
		t.Fatal(e)
	}
	policy, e := db.SetClarificationPolicy(ctx, c, BlockerActor{UserID: "pm"}, ClarificationPolicyUpdate{ExpectedVersion: 1, PMUser: "pm", DeputyUser: "owner-a"}, func() error { return nil }, now)
	if e != nil {
		t.Fatal(e)
	}
	result, e := questionApply(t, db, now, BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "cancel", Operation: "cancel", ExpectedVersion: q.Version, Reason: "explicit old generation cancellation"})
	if e != nil {
		t.Fatal(e)
	}
	result, e = questionApply(t, db, now, BlockerActor{UserID: "owner-a"}, q.ID, QuestionUpdate{RequestID: "reopen", Operation: "reopen", ExpectedVersion: result.Question.Version, Reason: "fresh authorized notification"})
	if e != nil {
		t.Fatal(e)
	}
	if result.Question.PolicyVersion != policy.Version {
		t.Fatal("CLARIFICATION_NEW_GENERATION_STALE_POLICY", result.Question.PolicyVersion, policy.Version)
	}
}

func TestClarificationReminderDoesNotAcquireReplyAnchor(t *testing.T) {
	db, now, c, _, notice, snapshot := clarificationNoticeFixture(t)
	ctx := context.Background()
	var before, after int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM spec_clarification_notice").Scan(&before); err != nil {
		t.Fatal(err)
	}
	notice.Kind = "remind_60m"
	if _, err := db.PrepareClarificationNotice(ctx, c, notice, snapshot.Binding, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowInvalid) {
		t.Fatalf("提醒不得新增答复回写授权: %v", err)
	}
	if err := db.db.QueryRow("SELECT COUNT(*) FROM spec_clarification_notice").Scan(&after); err != nil || before != after {
		t.Fatalf("提醒改变回复锚: before=%d after=%d err=%v", before, after, err)
	}
}
