package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func v2DeliveryFixture(t *testing.T) (*SQLite, context.Context, model.SessionDeliveryACK, time.Time) {
	t.Helper()
	db, ctx := newTestSQLite(t)
	now := time.Date(2026, 10, 27, 8, 0, 0, 123456000, time.UTC)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "ack-test", KeyID: "owner", SessionName: "seat", SourceMessageID: "source", Envelope: `{}`, State: "sent", Attempts: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	d, err := db.BindSessionDeliveryACK(ctx, "ack-test", "owner", "seat", 2, "driver")
	if err != nil || d.AckProtocol != 2 || d.AckGeneration != 1 {
		t.Fatalf("bind v2: %+v %v", d, err)
	}
	return db, ctx, model.SessionDeliveryACK{DeliveryID: d.ID, KeyID: d.KeyID, SessionName: d.SessionName, Protocol: 2, ProcessingMode: "driver", Generation: 1, Phase: "queued", OccurredAt: now}, now
}

func mustApplyACK(t *testing.T, db *SQLite, ctx context.Context, a model.SessionDeliveryACK, now time.Time) *model.SessionDelivery {
	t.Helper()
	d, _, err := db.ApplySessionDeliveryACK(ctx, a, now)
	if err != nil {
		t.Fatalf("apply %s: %v", a.Phase, err)
	}
	return d
}

func TestDeliveryACKV2BindingSurvivesConnectionAndRetryChanges(t *testing.T) {
	db, ctx, a, _ := v2DeliveryFixture(t)
	// A newly connected desktop/legacy peer cannot relabel a v2 driver item.
	d, err := db.BindSessionDeliveryACK(ctx, a.DeliveryID, a.KeyID, a.SessionName, 1, "desktop")
	if err != nil || d.AckProtocol != 2 || d.ProcessingMode != "driver" || d.AckGeneration != 1 {
		t.Fatalf("connection changed delivery identity: %+v %v", d, err)
	}
	if _, err := db.BindSessionDeliveryACK(ctx, a.DeliveryID, "other-owner", a.SessionName, 2, "driver"); !errors.Is(err, ErrDeliveryACKIdentity) {
		t.Fatalf("wrong owner bound delivery: %v", err)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET attempts=10 WHERE id=?`, a.DeliveryID); err != nil {
		t.Fatal(err)
	}
	d, err = db.BindSessionDeliveryACK(ctx, a.DeliveryID, a.KeyID, a.SessionName, 2, "driver")
	if err != nil || d.AckGeneration != 1 || d.Attempts != 10 {
		t.Fatalf("transport attempts changed logical generation: %+v %v", d, err)
	}
}

func TestDeliveryACKV2QueueFullAtomicallyDeadLettersAndCreatesFixedNotice(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "sender-key", SessionName: "sender", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionEndpointDeliveryACK(ctx, "sender-key", "sender", true); err != nil {
		t.Fatal(err)
	}
	original := model.Envelope{ID: "queue-full-message", From: "sender#sender-key", To: "seat#owner", Body: "PRIVATE BODY MUST NOT LEAK"}
	raw, _ := json.Marshal(original)
	if _, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET envelope_json=?,source_type='session_inbound' WHERE id=?`, string(raw), a.DeliveryID); err != nil {
		t.Fatal(err)
	}
	failure := model.SessionDeliveryACK{DeliveryID: "ack-test", KeyID: "owner", SessionName: "seat", Protocol: 2,
		ProcessingMode: "driver", Generation: 1, Phase: "failed", FailureReason: "queue_full", Retryable: false, OccurredAt: now}
	dead, changed, err := db.ApplySessionDeliveryACK(ctx, failure, now)
	if err != nil || !changed || dead.State != "dead" || dead.LastError != "queue_full" {
		t.Fatalf("queue_full terminal transition: %+v changed=%t err=%v", dead, changed, err)
	}
	notice, err := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("ack-test"))
	if err != nil || notice == nil || notice.State != "queued" || notice.SourceType != "failure_notice" {
		t.Fatalf("missing durable sender notice: %+v %v", notice, err)
	}
	var payload model.Envelope
	if err := json.Unmarshal([]byte(notice.Envelope), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Body != "发送未进入目标收件箱：目标 Agent 忙且收件箱已满；本条未提交，请缩短排队或稍后重新发送。" || strings.Contains(payload.Body, "PRIVATE BODY") || payload.Meta["failed_delivery_id"] != "ack-test" {
		t.Fatalf("unsafe or inaccurate notice: %+v", payload)
	}
	if _, changed, err := db.ApplySessionDeliveryACK(ctx, failure, now); err != nil || changed {
		t.Fatalf("duplicate ACK was not idempotent: changed=%t err=%v", changed, err)
	}
	var count int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM session_delivery WHERE source_type='failure_notice' AND source_message_id=?`, "ack-test").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate durable notice count=%d err=%v", count, err)
	}
}

func TestDeliveryACKV2QueueFullCannotOverrideDurableQueuedReceipt(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "sender-key", SessionName: "sender", Active: true}); err != nil {
		t.Fatal(err)
	}
	original := model.Envelope{ID: "queue-full-message", From: "sender#sender-key", To: "seat#owner", Body: "private"}
	raw, _ := json.Marshal(original)
	if _, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET envelope_json=?,source_type='session_inbound' WHERE id=?`, string(raw), a.DeliveryID); err != nil {
		t.Fatal(err)
	}
	mustApplyACK(t, db, ctx, a, now)
	queued, _ := db.GetSessionDelivery(ctx, "ack-test")
	if queued.State != "delivered" || queued.DeliveredAt == nil {
		t.Fatalf("fixture did not persist queued receipt: %+v", queued)
	}
	failure := model.SessionDeliveryACK{DeliveryID: "ack-test", KeyID: "owner", SessionName: "seat", Protocol: 2,
		ProcessingMode: "driver", Generation: 1, Phase: "failed", FailureReason: "queue_full", Retryable: false, OccurredAt: now}
	if _, _, err := db.ApplySessionDeliveryACK(ctx, failure, now); !errors.Is(err, ErrDeliveryACKTransition) {
		t.Fatalf("queue_full overrode durable queued receipt: %v", err)
	}
	got, _ := db.GetSessionDelivery(ctx, "ack-test")
	if got.State != "delivered" || got.DeliveredAt == nil || got.LastError != "" {
		t.Fatalf("durable queued receipt changed: %+v", got)
	}
	if notice, err := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("ack-test")); err != nil || notice != nil {
		t.Fatalf("persisted delivery got a not-submitted notice: %+v err=%v", notice, err)
	}
}

func TestLegacyQueueFullTerminalDoesNotCreateNegotiatedNotice(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "sender-key", SessionName: "sender", Active: true}); err != nil {
		t.Fatal(err)
	}
	original := model.Envelope{ID: "legacy-source", From: "sender#sender-key", To: "seat#owner", Body: "private"}
	raw, _ := json.Marshal(original)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "legacy-queue-full", KeyID: "owner", SessionName: "seat", Envelope: string(raw), SourceType: "session_inbound", State: "sent", Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSessionDelivery(ctx, "legacy-queue-full", "dead", "queue_full", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSessionDelivery(ctx, "legacy-queue-full")
	if err != nil || got.State != "dead" || got.LastError != "queue_full" {
		t.Fatalf("legacy terminal behavior changed: %+v err=%v", got, err)
	}
	if notice, err := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("legacy-queue-full")); err != nil || notice != nil {
		t.Fatalf("legacy queue_full created ACKv2 notice: %+v err=%v", notice, err)
	}
}

func TestDeliveryACKV2QueueFullNoticeInsertFailureRollsBackTerminal(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "sender-key", SessionName: "sender", Active: true}); err != nil {
		t.Fatal(err)
	}
	original := model.Envelope{ID: "queue-full-message", From: "sender#sender-key", To: "seat#owner", Body: "private"}
	raw, _ := json.Marshal(original)
	if _, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET envelope_json=?,source_type='session_inbound' WHERE id=?`, string(raw), a.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `CREATE TRIGGER fail_queue_full_notice BEFORE INSERT ON session_delivery WHEN NEW.source_type='failure_notice' BEGIN SELECT RAISE(ABORT,'synthetic notice insert fault'); END`); err != nil {
		t.Fatal(err)
	}
	failure := model.SessionDeliveryACK{DeliveryID: "ack-test", KeyID: "owner", SessionName: "seat", Protocol: 2,
		ProcessingMode: "driver", Generation: 1, Phase: "failed", FailureReason: "queue_full", OccurredAt: now}
	if _, _, err := db.ApplySessionDeliveryACK(ctx, failure, now); err == nil {
		t.Fatal("notice insert fault must fail the terminal transaction")
	}
	got, _ := db.GetSessionDelivery(ctx, "ack-test")
	if got.State != "sent" || got.LastError != "" {
		t.Fatalf("terminal state committed without notice: %+v", got)
	}
}

func TestDeliveryACKV2QueueFullFeishuNoticeUsesDurableOutboundQueue(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	if _, err := db.db.ExecContext(ctx, `INSERT INTO bot_channel(id,name,app_id,purpose,can_send,can_receive,active) VALUES('bot-id','bot-name','app','general',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	original := model.Envelope{ID: "queue-full-feishu-message", From: "ou_sender#owner#bot-name", To: "seat#owner", Body: "PRIVATE FEISHU BODY MUST NOT LEAK"}
	raw, _ := json.Marshal(original)
	if _, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET envelope_json=?,source_type='feishu_inbound' WHERE id=?`, string(raw), a.DeliveryID); err != nil {
		t.Fatal(err)
	}
	failure := model.SessionDeliveryACK{DeliveryID: a.DeliveryID, KeyID: a.KeyID, SessionName: a.SessionName, Protocol: 2,
		ProcessingMode: "driver", Generation: 1, Phase: "failed", FailureReason: "queue_full", OccurredAt: now}
	if _, _, err := db.ApplySessionDeliveryACK(ctx, failure, now); err != nil {
		t.Fatal(err)
	}
	id := model.FailureNoticeDeliveryID(a.DeliveryID)
	var entity, content, status string
	if err := db.db.QueryRowContext(ctx, `SELECT chat_entity_id,content_json,status FROM message WHERE id=?`, id).Scan(&entity, &content, &status); err != nil {
		t.Fatalf("durable Feishu sender notice missing: %v", err)
	}
	if entity != "bot-id:personal:ou_sender" || status != "queued" || strings.Contains(content, "PRIVATE FEISHU BODY") || !strings.Contains(content, "本条未提交") {
		t.Fatalf("unsafe or malformed Feishu notice: entity=%s status=%s content=%s", entity, status, content)
	}
	if _, _, err := db.ApplySessionDeliveryACK(ctx, failure, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM message WHERE id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate Feishu notice count=%d err=%v", count, err)
	}
}

func TestDeliveryACKV2PreviouslyAttemptedUntaggedRowStaysLegacy(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "legacy", KeyID: "o", SessionName: "s", Envelope: `{}`, State: "sent", Attempts: 2}); err != nil {
		t.Fatal(err)
	}
	d, err := db.BindSessionDeliveryACK(ctx, "legacy", "o", "s", 2, "driver")
	if err != nil || d.AckProtocol != 1 || d.ProcessingMode != "legacy" {
		t.Fatalf("historical dispatch promoted by new connection: %+v %v", d, err)
	}
}

func TestDeliveryACKV2OrderedReplayPreservesOriginalTimesAndTurn(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	queuedAt := now.Add(time.Second)
	mustApplyACK(t, db, ctx, a, queuedAt)
	a.Phase, a.TurnID, a.OccurredAt = "injected", "original-turn", now.Add(2*time.Second)
	mustApplyACK(t, db, ctx, a, now.Add(3*time.Second))
	admission := a
	a.Phase, a.TurnOutcome, a.OccurredAt = "processed", "completed", now.Add(4*time.Second)
	terminal := mustApplyACK(t, db, ctx, a, now.Add(5*time.Second))
	for _, replay := range []model.SessionDeliveryACK{a, admission, {DeliveryID: a.DeliveryID, KeyID: a.KeyID, SessionName: a.SessionName, Protocol: 2, ProcessingMode: "driver", Generation: 1, Phase: "queued", OccurredAt: now}} {
		d, changed, err := db.ApplySessionDeliveryACK(ctx, replay, now.Add(time.Hour))
		if err != nil || changed || !reflect.DeepEqual(d, terminal) {
			t.Fatalf("replay altered durable maximum stage/evidence: %+v changed=%v err=%v", d, changed, err)
		}
	}
	if !terminal.DeliveredAt.Equal(queuedAt) || !terminal.InjectedAt.Equal(admission.OccurredAt) || !terminal.ProcessedAt.Equal(a.OccurredAt) {
		t.Fatalf("receipt times replaced helper occurrence/first queue times: %+v", terminal)
	}
}

func TestDeliveryACKV2RejectsMissingPredecessorAndMismatchedIdentity(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	for _, phase := range []string{"injected", "processed", "admission_unknown"} {
		bad := a
		bad.Phase, bad.TurnID, bad.TurnOutcome = phase, "turn", "completed"
		if _, _, err := db.ApplySessionDeliveryACK(ctx, bad, now); err == nil {
			t.Fatalf("%s accepted before queued", phase)
		}
	}
	before := mustApplyACK(t, db, ctx, a, now)
	for _, mutate := range []func(*model.SessionDeliveryACK){
		func(a *model.SessionDeliveryACK) { a.Protocol = 1 },
		func(a *model.SessionDeliveryACK) { a.ProcessingMode = "desktop" },
		func(a *model.SessionDeliveryACK) { a.Generation++ },
		func(a *model.SessionDeliveryACK) { a.KeyID = "other" },
		func(a *model.SessionDeliveryACK) { a.SessionName = "other" },
		func(a *model.SessionDeliveryACK) { a.OccurredAt = time.Time{} },
	} {
		bad := a
		mutate(&bad)
		if _, _, err := db.ApplySessionDeliveryACK(ctx, bad, now); err == nil {
			t.Fatalf("mismatched ACK accepted: %+v", bad)
		}
	}
	a.Phase, a.TurnID, a.TurnOutcome = "processed", "turn", "completed"
	if _, _, err := db.ApplySessionDeliveryACK(ctx, a, now); err == nil {
		t.Fatal("processed was accepted without injected")
	}
	after, _ := db.GetSessionDelivery(ctx, a.DeliveryID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected ACK modified delivery: before=%+v after=%+v", before, after)
	}
}

func TestDeliveryACKV2QueuedDoesNotExpireRequeueOrBlockNextDelivery(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	mustApplyACK(t, db, ctx, a, now)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "next", KeyID: a.KeyID, SessionName: a.SessionName, Envelope: `{}`, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	d, err := db.ClaimSessionDelivery(ctx, a.KeyID, a.SessionName, now.Add(time.Second))
	if err != nil || d == nil || d.ID != "next" {
		t.Fatalf("confirmed local queue blocked next delivery or got retried: %+v %v", d, err)
	}
	if _, err := db.ReleaseSessionDeliveryLeases(ctx, a.KeyID, a.SessionName, "reconnect", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExpireSessionDeliveries(ctx, a.KeyID, a.SessionName, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SweepSessionDeliveryFailures(ctx, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	d, err = db.GetSessionDelivery(ctx, a.DeliveryID)
	if err != nil || d.State != "delivered" || d.Attempts != 1 || !d.DeliveredAt.Equal(now) {
		t.Fatalf("local durable queue was invalidated by transport cleanup: %+v %v", d, err)
	}
}

func TestDeliveryACKV2AdmissionUnknownIsIsolatedAndNotProcessed(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	mustApplyACK(t, db, ctx, a, now)
	a.Phase = "admission_unknown"
	d := mustApplyACK(t, db, ctx, a, now)
	if d.State != "admission_unknown" || d.InjectedAt != nil || d.ProcessedAt != nil || d.LastError != "model_admission_unknown" {
		t.Fatalf("unknown was promoted to admission/completion: %+v", d)
	}
	if claim, err := db.ClaimSessionDelivery(ctx, a.KeyID, a.SessionName, now.Add(time.Hour)); err != nil || claim != nil {
		t.Fatalf("uncertain dispatch was automatically replayed: %+v %v", claim, err)
	}
	a.Phase, a.TurnID, a.TurnOutcome = "processed", "assumed-turn", "completed"
	if _, _, err := db.ApplySessionDeliveryACK(ctx, a, now); err == nil {
		t.Fatal("unknown accepted a fabricated terminal without admission")
	}
}

func TestDeliveryACKV2RejectsTurnOrOutcomeReplacementAndLateCancel(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	mustApplyACK(t, db, ctx, a, now)
	a.Phase, a.TurnID = "injected", "original-turn"
	mustApplyACK(t, db, ctx, a, now)
	if cancelled, err := db.ConfirmSessionDeliveryCancelled(ctx, "source", a.DeliveryID, a.KeyID, a.SessionName); err != nil || cancelled {
		t.Fatalf("late cancellation overrode admission: cancelled=%v err=%v", cancelled, err)
	}
	wrongTurn := a
	wrongTurn.TurnID = "replacement-turn"
	if _, _, err := db.ApplySessionDeliveryACK(ctx, wrongTurn, now); err == nil {
		t.Fatal("replay replaced admitted turn")
	}
	a.Phase, a.TurnOutcome = "processed", "failed"
	mustApplyACK(t, db, ctx, a, now)
	a.TurnOutcome = "completed"
	if _, _, err := db.ApplySessionDeliveryACK(ctx, a, now); err == nil {
		t.Fatal("replay changed a failed model turn into success")
	}
}

func TestDeliveryACKV2RejectedDispatchIsNotAModelTerminal(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	mustApplyACK(t, db, ctx, a, now)
	a.Phase = "failed"
	d := mustApplyACK(t, db, ctx, a, now)
	if d.State != "dead" || d.InjectedAt != nil || d.ProcessedAt != nil || d.TurnOutcome != "" {
		t.Fatalf("rejected dispatch became a model terminal: %+v", d)
	}
	if _, changed, err := db.ApplySessionDeliveryACK(ctx, a, now.Add(time.Hour)); err != nil || changed {
		t.Fatalf("rejected dispatch ACK not idempotent: changed=%v err=%v", changed, err)
	}
}

// Binding capability alone does not prove that the helper durably owns a delivery.
// Preserve the original undelivered transport TTL until the queued ACK commits.
func TestDeliveryACKV2UnreceivedStillUsesTransportExpiry(t *testing.T) {
	for _, released := range []bool{false, true} {
		t.Run(fmt.Sprintf("released=%v", released), func(t *testing.T) {
			db, ctx, a, now := v2DeliveryFixture(t)
			if released {
				if _, err := db.ReleaseSessionDeliveryLeases(ctx, a.KeyID, a.SessionName, "disconnect", now); err != nil {
					t.Fatal(err)
				}
			}
			expired, err := db.ExpireSessionDeliveries(ctx, a.KeyID, a.SessionName, now.Add(31*time.Minute))
			if err != nil || len(expired) != 1 || expired[0].ID != a.DeliveryID {
				t.Fatalf("unreceived v2 transport must still expire: %+v %v", expired, err)
			}
			d, err := db.GetSessionDelivery(ctx, a.DeliveryID)
			if err != nil || d.State != "dead" || d.LastError != "delivery_expired" || d.DeliveredAt != nil || d.InjectedAt != nil || d.ProcessedAt != nil {
				t.Fatalf("transport expiry must not invent helper/model evidence: %+v %v", d, err)
			}
		})
	}
}

func TestDeliveryACKV2TransportUnknownKeepsAdmissionAndAcceptsLateTerminal(t *testing.T) {
	db, ctx, a, now := v2DeliveryFixture(t)
	mustApplyACK(t, db, ctx, a, now)
	a.Phase, a.TurnID = "injected", "original-turn"
	admitted := mustApplyACK(t, db, ctx, a, now)
	a.Phase, a.TurnID = "admission_unknown", ""
	uncertain := mustApplyACK(t, db, ctx, a, now.Add(time.Second))
	if uncertain.State != "injected" || uncertain.LastError != "turn_terminal_unknown" || uncertain.ProcessedAt != nil || uncertain.TurnID != admitted.TurnID || !uncertain.InjectedAt.Equal(*admitted.InjectedAt) {
		t.Fatalf("watchdog must preserve admission and expose transport uncertainty: %+v", uncertain)
	}
	repeated, changed, err := db.ApplySessionDeliveryACK(ctx, a, now.Add(2*time.Second))
	if err != nil || changed || !reflect.DeepEqual(repeated, uncertain) {
		t.Fatalf("repeated unknown must not rewrite evidence: %+v %v", repeated, err)
	}
	a.Phase, a.TurnID, a.TurnOutcome, a.OccurredAt = "processed", "original-turn", "completed", now.Add(3*time.Second)
	terminal := mustApplyACK(t, db, ctx, a, now.Add(4*time.Second))
	if terminal.State != "processed" || terminal.LastError != "" || terminal.TurnOutcome != "completed" {
		t.Fatalf("late true terminal must finish original turn: %+v", terminal)
	}
}
