package store

import (
	"context"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func outboundRetentionFixture(t *testing.T) (*SQLite, context.Context, *clock.Fake) {
	t.Helper()
	db := openMigrated(t, filepath.Join(t.TempDir(), "retention.db"))
	t.Cleanup(func() { db.Close() })
	c := &clock.Fake{T: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	db.SetClock(c)
	return db, context.Background(), c
}
func seedOutboundFence(t *testing.T, db *SQLite, id string, at time.Time) {
	t.Helper()
	_, err := db.db.Exec(`INSERT INTO outbound_intent(sender_id,source_message_id,target_id,digest,delivery_id,created_at) VALUES('sender',?,'target',?,?,?)`, id, strings.Repeat("a", 64), "original-"+id, at.UTC().Format(outboundIntentStamp))
	if err != nil {
		t.Fatal(err)
	}
}
func TestOutboundIntentRetentionReleasesExpiredCapacityBoundedly(t *testing.T) {
	db, ctx, c := outboundRetentionFixture(t)
	// Leave at least the global limit even after one cleanup batch: a global
	// all-time-count regression must still reject this otherwise valid admission.
	expired := c.T.Add(-DefaultOutboundIntentRetention - time.Nanosecond)
	_, err := db.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<65792)
 INSERT INTO outbound_intent(sender_id,source_message_id,target_id,digest,delivery_id,created_at)
 SELECT CASE WHEN i<=4096 THEN 'sender' ELSE printf('bulk-%d',i) END,printf('expired-%d',i),'target',?,printf('expired-delivery-%d',i),? FROM n`, strings.Repeat("a", 64), expired.UTC().Format(outboundIntentStamp))
	if err != nil {
		t.Fatal(err)
	}
	v, d := outboundFixture(1)
	got, created, err := db.CommitOutboundSessionDelivery(ctx, v, d)
	if err != nil || !created || got.DeliveryID != d.ID {
		t.Fatal("expired fences still blocked a new admission", created, err)
	}
	var remaining int
	if err = db.db.QueryRow(`SELECT count(*) FROM outbound_intent WHERE created_at<?`, c.T.Add(-DefaultOutboundIntentRetention).Format(outboundIntentStamp)).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 65792-OutboundIntentCleanupBatch {
		t.Fatalf("cleanup must remove exactly one bounded batch of expired fences, remaining=%d", remaining)
	}
	again, created, err := db.CommitOutboundSessionDelivery(ctx, v, d)
	if err != nil || created || again.DeliveryID != got.DeliveryID {
		t.Fatal("new retained receipt was not stable", err)
	}
}
func TestOutboundIntentRetentionPreservesWindowAndBoundary(t *testing.T) {
	db, ctx, c := outboundRetentionFixture(t)
	cutoff := c.T.Add(-DefaultOutboundIntentRetention)
	for i, at := range []time.Time{cutoff.Add(-time.Nanosecond), cutoff, cutoff.Add(time.Nanosecond), c.T} {
		seedOutboundFence(t, db, fmt.Sprint(i), at)
	}
	v, d := outboundFixture(99)
	if _, _, err := db.CommitOutboundSessionDelivery(ctx, v, d); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		old, err := db.GetOutboundIntent(ctx, "sender", fmt.Sprint(i))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if old != nil {
				t.Fatal("expired fence survived cleanup")
			}
			continue
		}
		if old == nil {
			t.Fatalf("window fence %d was erased", i)
		}
		replay, delivery := outboundFixture(100 + i)
		replay.SourceMessageID = fmt.Sprint(i)
		delivery.SourceMessageID = replay.SourceMessageID
		got, created, err := db.CommitOutboundSessionDelivery(ctx, replay, delivery)
		if err != nil || created || got.DeliveryID != old.DeliveryID {
			t.Fatal("inside-window replay lost original receipt", i, err)
		}
	}
}
func TestOutboundIntentRetentionPreservesPendingPhysicalBindings(t *testing.T) {
	db, ctx, c := outboundRetentionFixture(t)
	c.T = c.T.Add(-8 * 24 * time.Hour)
	v, d := outboundFixture(1)
	if _, _, err := db.CommitOutboundSessionDelivery(ctx, v, d); err != nil {
		t.Fatal(err)
	}
	c.Advance(8 * 24 * time.Hour)
	trigger := func(n int) {
		t.Helper()
		nv, nd := outboundFixture(n)
		if _, _, err := db.CommitOutboundSessionDelivery(ctx, nv, nd); err != nil {
			t.Fatal(err)
		}
	}
	trigger(2)
	if old, _ := db.GetOutboundIntentForDelivery(ctx, d.ID); old == nil {
		t.Fatal("GC erased the stable identity of an unfinished delivery")
	}
	if _, err := db.db.Exec(`UPDATE session_delivery SET state='processed' WHERE id=?`, d.ID); err != nil {
		t.Fatal(err)
	}
	notice := model.SessionDelivery{ID: "pending-failure-notice", KeyID: "fixture", SessionName: "source", Envelope: `{}`, SourceType: "failure_notice", SourceMessageID: d.ID, State: "queued", CreatedAt: c.T}
	if err := db.EnqueueSessionDelivery(ctx, notice); err != nil {
		t.Fatal(err)
	}
	trigger(3)
	if old, _ := db.GetOutboundIntentForDelivery(ctx, d.ID); old == nil {
		t.Fatal("GC erased the stable sender binding of a pending failure notice")
	}
	if _, err := db.db.Exec(`UPDATE session_delivery SET state='processed' WHERE id=?`, notice.ID); err != nil {
		t.Fatal(err)
	}
	trigger(4)
	if old, _ := db.GetOutboundIntentForDelivery(ctx, d.ID); old != nil {
		t.Fatal("completed expired binding was never reclaimed")
	}
}
func TestOutboundIntentRetentionUsesServerAcceptanceClock(t *testing.T) {
	db, ctx, c := outboundRetentionFixture(t)
	v, d := outboundFixture(1)
	d.CreatedAt = c.T.Add(-30 * 24 * time.Hour)
	got, _, err := db.CommitOutboundSessionDelivery(ctx, v, d)
	if err != nil || !got.CreatedAt.Equal(c.T) {
		t.Fatal("queue/caller timestamp controls fence age", got, err)
	}
	if _, err = db.db.Exec(`UPDATE session_delivery SET state='processed' WHERE id=?`, d.ID); err != nil {
		t.Fatal(err)
	}
	c.Advance(DefaultOutboundIntentRetention + time.Nanosecond)
	nv, nd := outboundFixture(2)
	if _, _, err = db.CommitOutboundSessionDelivery(ctx, nv, nd); err != nil {
		t.Fatal(err)
	}
	if old, _ := db.GetOutboundIntentForDelivery(ctx, d.ID); old != nil {
		t.Fatal("server-created fence did not expire at nanosecond boundary")
	}
}
func TestOutboundIntentRetentionConfiguration(t *testing.T) {
	db, _, _ := outboundRetentionFixture(t)
	for _, raw := range []string{"", "168h", "48h", "26h1ns", "720h"} {
		if err := db.ConfigureOutboundIntentRetention(raw); err != nil {
			t.Fatal(raw, err)
		}
		want := DefaultOutboundIntentRetention
		if raw != "" {
			want, _ = time.ParseDuration(raw)
		}
		if db.OutboundIntentRetention() != want {
			t.Fatal("retention ignored configuration", raw)
		}
	}
	before := db.OutboundIntentRetention()
	for _, raw := range []string{"garbage", "SENSITIVE_BAD_INPUT", "0", "-1h", "24h", "26h", "720h1ns", "721h", "999999999999999999999h"} {
		err := db.ConfigureOutboundIntentRetention(raw)
		if err == nil {
			t.Fatal("unsafe retention accepted", raw)
		}
		if raw == "SENSITIVE_BAD_INPUT" && strings.Contains(err.Error(), raw) {
			t.Fatal("invalid raw value leaked in configuration error")
		}
		if db.OutboundIntentRetention() != before {
			t.Fatal("invalid input changed active retention")
		}
	}
}

func TestOutboundIntentRetentionConfiguredWindowControlsGC(t *testing.T) {
	db, ctx, c := outboundRetentionFixture(t)
	if err := db.ConfigureOutboundIntentRetention("48h"); err != nil {
		t.Fatal(err)
	}
	seedOutboundFence(t, db, "outside-configured", c.T.Add(-49*time.Hour))
	seedOutboundFence(t, db, "inside-configured", c.T.Add(-47*time.Hour))
	v, d := outboundFixture(99)
	if _, _, err := db.CommitOutboundSessionDelivery(ctx, v, d); err != nil {
		t.Fatal(err)
	}
	if old, _ := db.GetOutboundIntent(ctx, "sender", "outside-configured"); old != nil {
		t.Fatal("configured retention did not govern cleanup")
	}
	if old, _ := db.GetOutboundIntent(ctx, "sender", "inside-configured"); old == nil {
		t.Fatal("configured in-window fence erased")
	}
}
func TestOutboundIntentRetentionCleanupRollsBackWithQueueFailure(t *testing.T) {
	db, ctx, c := outboundRetentionFixture(t)
	seedOutboundFence(t, db, "expired", c.T.Add(-8*24*time.Hour))
	if _, err := db.db.Exec(`CREATE TRIGGER fail_retention_queue BEFORE INSERT ON session_delivery BEGIN SELECT RAISE(ABORT,'synthetic retention queue failure'); END`); err != nil {
		t.Fatal(err)
	}
	v, d := outboundFixture(99)
	if _, _, err := db.CommitOutboundSessionDelivery(ctx, v, d); err == nil {
		t.Fatal("failed queue accepted intent")
	}
	if old, _ := db.GetOutboundIntent(ctx, "sender", "expired"); old == nil {
		t.Fatal("cleanup escaped the failed acceptance transaction")
	}
	if partial, _ := db.GetOutboundIntent(ctx, v.SenderID, v.SourceMessageID); partial != nil {
		t.Fatal("failed acceptance left a fence")
	}
	if _, err := db.db.Exec(`DROP TRIGGER fail_retention_queue`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := db.CommitOutboundSessionDelivery(ctx, v, d); err != nil || !created {
		t.Fatal("same-ID retry did not commit", err)
	}
	if old, _ := db.GetOutboundIntent(ctx, "sender", "expired"); old != nil {
		t.Fatal("successful retry did not reclaim expired fence")
	}
}
