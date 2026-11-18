package tempgroup

import (
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

// An isolated persisted-state fixture deliberately makes the group deadline
// earlier than both messages. Normal equal-TTL acceptance also touches the
// group and does not itself create this inversion. Never infer permission
// from the message's still-future expires_at, even before the group scanner.
func TestR62ExpiredGroupCannotDeliverUnexpiredQueuedMessagesPG(t *testing.T) {
	f := newFixture(t)
	raw, a := lifetimeIssue(t, f)
	start := f.now
	incoming := lifetimeIncoming(t, f, a, "unexpired-incoming")
	outgoing := lifetimeSend(t, f, a, "unexpired-outgoing")
	messageExpiry := start.Add(7 * 24 * time.Hour)
	lifetimeWant(t, incoming.ExpiresAt, messageExpiry, "incoming remains seven days")
	lifetimeWant(t, outgoing.ExpiresAt, messageExpiry, "outgoing remains seven days")
	groupExpiry := start.Add(3 * 24 * time.Hour)
	if _, err := f.p.Exec(f.ctx, f.s.Access, `UPDATE np_temp_group SET expires_at=$1 WHERE code=$2`, groupExpiry, string(a.Group)); err != nil {
		t.Fatal(err)
	}
	f.now = groupExpiry
	if !f.now.Before(messageExpiry) || !f.now.Before(a.ExpiresAt) {
		t.Fatal("fixture must isolate group expiry from message/token expiry")
	}
	if rows, err := f.seats.Check(f.ctx, a); !errors.Is(err, extshared.ErrGroupDissolved) || len(rows) != 0 {
		t.Fatalf("expired group leaked inbox summaries: n=%d err=%v", len(rows), err)
	}
	if rows, err := f.seats.Wait(f.ctx, a, 0); !errors.Is(err, extshared.ErrGroupDissolved) || len(rows) != 0 {
		t.Fatalf("expired group delivered unexpired body: n=%d err=%v", len(rows), err)
	}
	if rows, err := f.seats.WaitReply(f.ctx, a, start, 0); !errors.Is(err, extshared.ErrGroupDissolved) || len(rows) != 0 {
		t.Fatalf("expired group exposed reply: n=%d err=%v", len(rows), err)
	}
	if state, reason, err := f.seats.Receipt(f.ctx, a, incoming.DeliveryID); !errors.Is(err, extshared.ErrGroupDissolved) || state != "" || reason != "" {
		t.Fatalf("expired group exposed receipt: state=%s reason=%s err=%v", state, reason, err)
	}
	if _, err := f.seats.Authenticate(f.ctx, raw); !errors.Is(err, extshared.ErrGroupDissolved) {
		t.Fatalf("token cannot revive expired group: %v", err)
	}
	projection := &lifetimeProjection{}
	if did, err := f.seats.ProjectOne(f.ctx, projection); err != nil || !did || len(projection.ids) != 0 {
		t.Fatalf("expired group projected pending message: did=%t calls=%d err=%v", did, len(projection.ids), err)
	}
	if n := f.scalar(`SELECT count(*) FROM np_extseat_outbox WHERE delivery_id=$1 AND state='dead' AND dead_reason='group_unavailable'`, outgoing.DeliveryID); n != 1 {
		t.Fatal("unprojected outbox did not terminate group_unavailable")
	}
	// Message-only scanning does not mislabel a still-live message as expired.
	if err := f.seats.Expire(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.scalar(`SELECT count(*) FROM np_extseat_inbox WHERE id=$1 AND state='queued'`, incoming.DeliveryID); n != 1 {
		t.Fatal("poll denial/message scanner must not claim message expiry")
	}
	if err := f.s.Expire(f.ctx); err != nil {
		t.Fatal(err)
	}
	notifier := &recordNotifier{}
	f.s.Notifier = notifier
	f.s.Finalize = f.seats.SettleDissolvedGroup
	if err := f.s.NotifyPending(f.ctx); err != nil {
		t.Fatal(err)
	}
	if notifier.n != 1 {
		t.Fatal("group dissolution notification was not completed")
	}
	if n := f.scalar(`SELECT count(*) FROM np_extseat_inbox WHERE id=$1 AND state='dead' AND dead_reason='group_unavailable'`, incoming.DeliveryID); n != 1 {
		t.Fatal("group finalizer did not settle unreachable inbox")
	}
	lifetimeWant(t, lifetimeGroup(t, f, a), groupExpiry, "no polling renewal")
	lifetimeWant(t, lifetimeTime(t, f, `SELECT expires_at FROM np_extseat_inbox WHERE id=$1`, incoming.DeliveryID), messageExpiry, "inbox expiry not rewritten")
	lifetimeWant(t, lifetimeTime(t, f, `SELECT expires_at FROM np_extseat_outbox WHERE delivery_id=$1`, outgoing.DeliveryID), messageExpiry, "outbox expiry not rewritten")
	lifetimeWant(t, lifetimeTime(t, f, `SELECT expires_at FROM np_extseat_token WHERE id=$1`, a.TokenID), start.Add(14*24*time.Hour), "denied authentication did not renew token")
}
