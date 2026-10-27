package store

import (
	"context"
	"errors"
	"reflect"
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
