package tempgroup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// These tests use the production group and seat services on real PG. Literal
// durations deliberately avoid deriving expected expiry from the tested constants.
func lifetimeIssue(t *testing.T, f *fixture) (string, extseat.Seat) {
	t.Helper()
	raw, a, _, err := f.seats.IssueToken(f.ctx, extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: "lifetime", DisplayName: "lifetime"}, "bot", "ha.alice")
	if err != nil {
		t.Fatal(err)
	}
	return raw, a
}
func lifetimeTime(t *testing.T, f *fixture, query string, args ...any) time.Time {
	t.Helper()
	row, err := f.p.QueryRow(f.ctx, f.s.Access, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	var result time.Time
	if err = row.Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
func lifetimeGroup(t *testing.T, f *fixture, a extseat.Seat) time.Time {
	t.Helper()
	return lifetimeTime(t, f, `SELECT expires_at FROM np_temp_group WHERE code=$1`, string(a.Group))
}
func lifetimeWant(t *testing.T, got, want time.Time, label string) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s expiry=%s want=%s", label, got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}
func lifetimeSend(t *testing.T, f *fixture, a extseat.Seat, id string) extseat.SendReceipt {
	t.Helper()
	r, err := f.seats.Send(f.ctx, a, "ha.alice", "synthetic lifetime message", id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func lifetimeIncoming(t *testing.T, f *fixture, a extseat.Seat, id string) extseat.SendReceipt {
	t.Helper()
	r, err := f.seats.EnqueueFromHelper(f.ctx, "ha", a.AgentID, id, "synthetic incoming lifetime message")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type lifetimeProjection struct{ ids []string }

func (p *lifetimeProjection) Project(_ context.Context, d extseat.Delivery, _ identity.Agent) error {
	p.ids = append(p.ids, d.ID)
	return nil
}

func TestR62SevenDayGroupBoundaryPG(t *testing.T) {
	for _, c := range []struct {
		name  string
		delta time.Duration
		valid bool
	}{{"before", -time.Microsecond, true}, {"exact", 0, false}, {"after", time.Microsecond, false}} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			_, a := lifetimeIssue(t, f)
			start := f.now
			expiry := start.Add(7 * 24 * time.Hour)
			lifetimeWant(t, lifetimeGroup(t, f, a), expiry, "new seven-day AG")
			lifetimeWant(t, a.ExpiresAt, start.Add(14*24*time.Hour), "unchanged fourteen-day token")
			f.now = expiry.Add(c.delta)
			_, err := f.seats.Check(f.ctx, a)
			if c.valid && err != nil {
				t.Fatalf("AG expired before seven days: %v", err)
			}
			if !c.valid && !errors.Is(err, extshared.ErrGroupDissolved) {
				t.Fatalf("AG usable at/after seven days: %v", err)
			}
			lifetimeWant(t, lifetimeGroup(t, f, a), expiry, "poll must not extend AG")
			if err = f.s.Expire(f.ctx); err != nil {
				t.Fatal(err)
			}
			dissolved := f.scalar(`SELECT count(*) FROM np_temp_group WHERE code=$1 AND dissolved_at IS NOT NULL`, string(a.Group))
			if (dissolved == 0) != c.valid {
				t.Fatalf("seven-day scanner boundary: dissolved=%d valid=%t", dissolved, c.valid)
			}
		})
	}
}

func TestR62SevenDayMessageBoundaryIndependentOfRenewalsPG(t *testing.T) {
	for _, c := range []struct {
		name  string
		delta time.Duration
		valid bool
	}{{"before", -time.Microsecond, true}, {"exact", 0, false}, {"after", time.Microsecond, false}} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			raw, a := lifetimeIssue(t, f)
			start := f.now
			incoming := lifetimeIncoming(t, f, a, "original-incoming")
			outgoing := lifetimeSend(t, f, a, "original-outgoing")
			expiry := start.Add(7 * 24 * time.Hour)
			lifetimeWant(t, incoming.ExpiresAt, expiry, "new inbox seven-day message")
			lifetimeWant(t, outgoing.ExpiresAt, expiry, "new outbox seven-day message")
			f.now = start.Add(6 * 24 * time.Hour)
			lifetimeSend(t, f, a, "renew-group") // A real accepted send, not a fixture expiry UPDATE.
			fresh, err := f.seats.Authenticate(f.ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			lifetimeWant(t, fresh.ExpiresAt, f.now.Add(14*24*time.Hour), "renewed token")
			lifetimeWant(t, lifetimeGroup(t, f, a), f.now.Add(7*24*time.Hour), "renewed AG")
			for _, q := range []struct{ sql, id string }{{`SELECT expires_at FROM np_extseat_inbox WHERE id=$1`, incoming.DeliveryID}, {`SELECT expires_at FROM np_extseat_outbox WHERE delivery_id=$1`, outgoing.DeliveryID}, {`SELECT expires_at FROM np_extseat_send_key WHERE delivery_id=$1`, outgoing.DeliveryID}} {
				lifetimeWant(t, lifetimeTime(t, f, q.sql, q.id), expiry, "renewal must not extend old message/send key")
			}
			f.now = expiry.Add(c.delta)
			messages, err := f.seats.Wait(f.ctx, fresh, 0)
			if err != nil {
				t.Fatal(err)
			}
			if c.valid {
				if len(messages) != 1 || messages[0].ID != incoming.DeliveryID {
					t.Fatalf("inbox expired early: %+v", messages)
				}
				if err = f.seats.Ack(f.ctx, fresh, incoming.DeliveryID, int(messages[0].Generation)); err != nil {
					t.Fatal(err)
				}
			} else {
				if len(messages) != 0 {
					t.Fatalf("expired inbox delivered: %+v", messages)
				}
				if n := f.scalar(`SELECT count(*) FROM np_extseat_inbox WHERE id=$1 AND state='dead' AND dead_reason='message_expired'`, incoming.DeliveryID); n != 1 {
					t.Fatal("expired inbox not terminal")
				}
			}
			projection := &lifetimeProjection{}
			did, err := f.seats.ProjectOne(f.ctx, projection)
			if err != nil || !did {
				t.Fatalf("outbox select: did=%t err=%v", did, err)
			}
			if c.valid {
				if len(projection.ids) != 1 || projection.ids[0] != outgoing.DeliveryID {
					t.Fatalf("outbox expired early: %+v", projection.ids)
				}
			} else {
				if len(projection.ids) != 0 {
					t.Fatal("expired outbox reached projector")
				}
				if n := f.scalar(`SELECT count(*) FROM np_extseat_outbox WHERE delivery_id=$1 AND state='dead' AND dead_reason='message_expired'`, outgoing.DeliveryID); n != 1 {
					t.Fatal("expired outbox not terminal")
				}
			}
		})
	}
}

func TestR62PollingAndIdempotentEventsDoNotRenewGroupPG(t *testing.T) {
	f := newFixture(t)
	raw, a := lifetimeIssue(t, f)
	start := f.now
	expiry := start.Add(7 * 24 * time.Hour)
	f.now = start.Add(24 * time.Hour)
	fresh, err := f.seats.Authenticate(f.ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	lifetimeWant(t, lifetimeGroup(t, f, a), expiry, "token authentication must not renew AG")
	for i := 0; i < 3; i++ {
		f.now = f.now.Add(time.Minute)
		if _, err = f.seats.Check(f.ctx, fresh); err != nil {
			t.Fatal(err)
		}
		if _, err = f.seats.Wait(f.ctx, fresh, 0); err != nil {
			t.Fatal(err)
		}
		if _, err = f.seats.WaitReply(f.ctx, fresh, start, 0); err != nil {
			t.Fatal(err)
		}
		lifetimeWant(t, lifetimeGroup(t, f, a), expiry, "empty polling must not renew AG")
	}
	incoming := lifetimeIncoming(t, f, a, "ack-once")
	f.now = f.now.Add(time.Hour)
	messages, err := f.seats.Wait(f.ctx, fresh, 0)
	if err != nil || len(messages) != 1 {
		t.Fatalf("claim: %v %+v", err, messages)
	}
	if err = f.seats.Ack(f.ctx, fresh, incoming.DeliveryID, int(messages[0].Generation)); err != nil {
		t.Fatal(err)
	}
	ackExpiry := f.now.Add(7 * 24 * time.Hour)
	lifetimeWant(t, lifetimeGroup(t, f, a), ackExpiry, "first received ACK must renew")
	f.now = f.now.Add(time.Hour)
	if err = f.seats.Ack(f.ctx, fresh, incoming.DeliveryID, int(messages[0].Generation)); err != nil {
		t.Fatal(err)
	}
	lifetimeWant(t, lifetimeGroup(t, f, a), ackExpiry, "duplicate received ACK must not renew")
	f.now = f.now.Add(time.Hour)
	sent := lifetimeSend(t, f, a, "send-once")
	sendExpiry := f.now.Add(7 * 24 * time.Hour)
	lifetimeWant(t, lifetimeGroup(t, f, a), sendExpiry, "accepted send must renew")
	f.now = f.now.Add(time.Hour)
	retry := lifetimeSend(t, f, a, "send-once")
	if retry.DeliveryID != sent.DeliveryID {
		t.Fatal("retry created a second message")
	}
	lifetimeWant(t, retry.ExpiresAt, sent.ExpiresAt, "retry must keep message expiry")
	lifetimeWant(t, lifetimeGroup(t, f, a), sendExpiry, "same client ID must not renew AG")
}

func TestR62FourteenDayTokenBoundaryPG(t *testing.T) {
	for _, c := range []struct {
		name  string
		delta time.Duration
		valid bool
	}{{"before", -time.Microsecond, true}, {"exact", 0, false}, {"after", time.Microsecond, false}} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			raw, a := lifetimeIssue(t, f)
			start := f.now
			expiry := start.Add(14 * 24 * time.Hour)
			lifetimeWant(t, a.ExpiresAt, expiry, "token fourteen days unchanged")
			// Keep AG live via accepted sends without authenticating/renewing the token.
			f.now = start.Add(6 * 24 * time.Hour)
			lifetimeSend(t, f, a, "keep-group-6")
			f.now = start.Add(12 * 24 * time.Hour)
			lifetimeSend(t, f, a, "keep-group-12")
			groupExpiry := lifetimeGroup(t, f, a)
			f.now = expiry.Add(c.delta)
			fresh, err := f.seats.Authenticate(f.ctx, raw)
			if c.valid {
				if err != nil {
					t.Fatalf("token expired before fourteen days: %v", err)
				}
				lifetimeWant(t, fresh.ExpiresAt, f.now.Add(14*24*time.Hour), "token sliding renewal remains fourteen days")
			} else if !errors.Is(err, extshared.ErrTokenExpired) {
				t.Fatalf("token usable at/after fourteen days: %v", err)
			}
			lifetimeWant(t, lifetimeGroup(t, f, a), groupExpiry, "token auth must not renew group")
		})
	}
}

func TestR62PersistedLegacyExpiryNotRewrittenPG(t *testing.T) {
	f := newFixture(t)
	raw, a := lifetimeIssue(t, f)
	start := f.now
	incoming := lifetimeIncoming(t, f, a, "legacy-incoming")
	outgoing := lifetimeSend(t, f, a, "legacy-outgoing")
	// Isolated fixture seeds already-persisted pre-upgrade rows; this is not a migration.
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE np_temp_group SET expires_at=$1 WHERE code=$2`, []any{start.Add(3 * 24 * time.Hour), string(a.Group)}},
		{`UPDATE np_extseat_inbox SET expires_at=$1 WHERE id=$2`, []any{start.Add(24 * time.Hour), incoming.DeliveryID}},
		{`UPDATE np_extseat_outbox SET expires_at=$1 WHERE delivery_id=$2`, []any{start.Add(24 * time.Hour), outgoing.DeliveryID}},
		{`UPDATE np_extseat_send_key SET expires_at=$1 WHERE delivery_id=$2`, []any{start.Add(24 * time.Hour), outgoing.DeliveryID}},
	} {
		if _, err := f.p.Exec(f.ctx, f.s.Access, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	restarted := *f.seats
	f.seats = &restarted
	f.now = start.Add(12 * time.Hour)
	if _, err := f.seats.Authenticate(f.ctx, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.seats.Check(f.ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := f.seats.Expire(f.ctx); err != nil {
		t.Fatal(err)
	}
	lifetimeWant(t, lifetimeGroup(t, f, a), start.Add(3*24*time.Hour), "no retroactive legacy group rewrite")
	for _, q := range []struct{ sql, id string }{{`SELECT expires_at FROM np_extseat_inbox WHERE id=$1`, incoming.DeliveryID}, {`SELECT expires_at FROM np_extseat_outbox WHERE delivery_id=$1`, outgoing.DeliveryID}, {`SELECT expires_at FROM np_extseat_send_key WHERE delivery_id=$1`, outgoing.DeliveryID}} {
		lifetimeWant(t, lifetimeTime(t, f, q.sql, q.id), start.Add(24*time.Hour), "no retroactive legacy message rewrite")
	}
	current := lifetimeSend(t, f, a, "new-policy-message")
	lifetimeWant(t, current.ExpiresAt, f.now.Add(7*24*time.Hour), "new message after upgrade")
	lifetimeWant(t, lifetimeGroup(t, f, a), f.now.Add(7*24*time.Hour), "normal new event may renew old group")
	f.now = start.Add(24 * time.Hour)
	messages, err := f.seats.Wait(f.ctx, a, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("legacy message must expire at stored one-day deadline: %+v %v", messages, err)
	}
	if n := f.scalar(`SELECT count(*) FROM np_extseat_inbox WHERE id=$1 AND state='dead' AND dead_reason='message_expired'`, incoming.DeliveryID); n != 1 {
		t.Fatal("legacy inbox expiry ignored")
	}
	projection := &lifetimeProjection{}
	if did, err := f.seats.ProjectOne(f.ctx, projection); err != nil || !did || len(projection.ids) != 0 {
		t.Fatalf("legacy outbox expiry ignored: %t %v %+v", did, err, projection.ids)
	}
}
