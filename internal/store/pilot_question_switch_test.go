package store

import (
	"context"
	"testing"
)

func TestPilotQuestionDisabledReasonDoesNotResetUnknown(t *testing.T) {
	db, now, _ := questionFixture(t)
	q := questionCreate(t, db, now, "technical")
	ctx := context.Background()
	c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := db.PendingQuestionNotices(ctx, "")
	if err != nil || len(ns) != 1 {
		t.Fatal(err, ns)
	}
	n := ns[0]
	if _, ready, err := db.PrepareQuestionNotice(ctx, c, n, false, func() error { return nil }, now, QuestionFeishuDisabledReason); err != nil || ready {
		t.Fatal(err, ready)
	}
	var state, reason string
	if err = db.db.QueryRow(`SELECT state,reason FROM pilot_question_outbox WHERE id=?`, n.ID).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	if state != "pending_config" || reason != QuestionFeishuDisabledReason {
		t.Fatal("D3_DISABLED_REASON_NOT_RECORDED", state, reason)
	}
	if _, ready, err := db.PrepareQuestionNotice(ctx, c, n, true, func() error { return nil }, now); err != nil || !ready {
		t.Fatal(err, ready)
	}
	// Turning off after an uncertain attempt cannot make it retryable.
	if _, ready, err := db.PrepareQuestionNotice(ctx, c, n, false, func() error { return nil }, now, QuestionFeishuDisabledReason); err != nil || ready {
		t.Fatal(err, ready)
	}
	if err = db.db.QueryRow(`SELECT state,reason FROM pilot_question_outbox WHERE id=?`, n.ID).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	if state != "unknown" || reason != "dispatch_receipt_unknown_no_retry" {
		t.Fatal("D3_DISABLED_RESET_UNCERTAIN_ATTEMPT", state, reason)
	}
}
