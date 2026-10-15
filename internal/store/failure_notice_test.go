package store

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func noticeFixture(t *testing.T) (*SQLite, time.Time) {
	t.Helper()
	db, ctx := newTestSQLite(t)
	now := time.Now().UTC()
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "source", SessionName: "sender", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionEndpointDeliveryACK(ctx, "source", "sender", true); err != nil {
		t.Fatal(err)
	}
	return db, now
}
func noticeOriginal(t *testing.T, db *SQLite, id, state, reason string, created time.Time) model.SessionDelivery {
	t.Helper()
	env := model.Envelope{ID: "message-" + id, From: "sender#source", To: "target#other", Body: "private original @someone #route must not appear"}
	raw, _ := json.Marshal(env)
	d := model.SessionDelivery{ID: id, KeyID: "other", SessionName: "target", Envelope: string(raw), SourceMessageID: env.ID, SourceType: "session_inbound", State: state, LastError: reason, CreatedAt: created}
	if err := db.EnqueueSessionDelivery(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestFailureNoticeAtomicRollbackAndSameIDRetry(t *testing.T) {
	db, now := noticeFixture(t)
	ctx := context.Background()
	d := noticeOriginal(t, db, "old", "queued", "", now.Add(-31*time.Minute))
	if _, err := db.db.ExecContext(ctx, `CREATE TRIGGER fail_notice BEFORE INSERT ON session_delivery WHEN NEW.source_type='failure_notice' BEGIN SELECT RAISE(ABORT,'synthetic notice insert fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExpireSessionDeliveries(ctx, "other", "target", now); err == nil {
		t.Fatal("notice insert fault must abort original terminal transition")
	}
	got, _ := db.GetSessionDelivery(ctx, d.ID)
	if got.State != "queued" {
		t.Fatalf("partial commit: %+v", got)
	}
	if _, err := db.db.ExecContext(ctx, `DROP TRIGGER fail_notice`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExpireSessionDeliveries(ctx, "other", "target", now); err != nil {
		t.Fatal(err)
	}
	id := model.FailureNoticeDeliveryID(d.ID)
	notice, err := db.GetSessionDelivery(ctx, id)
	if err != nil || notice == nil {
		t.Fatalf("notice missing %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal([]byte(notice.Envelope), &env); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(env.Body, "【系统投递失败通知】") || strings.ContainsAny(env.Body, "@#") || strings.Contains(env.Body, "private original") {
		t.Fatalf("unsafe body %q", env.Body)
	}
	if env.ID != id || env.From != "workpulse#source" || env.To != "sender#source" || notice.SourceMessageID != d.ID {
		t.Fatal("notice wire identity/correlation differs from durable row")
	}
	for key, want := range map[string]any{"type": "delivery", "delivery_id": id, "delivery_version": float64(1), "ack_required": true, "reply_policy": "no_reply", "conversation_terminal": true, "suppress_reply_origin": true, "no_mirror": true} {
		if env.Meta[key] != want {
			t.Fatalf("rollback wire contract %s=%v want %v", key, env.Meta[key], want)
		}
	}
	if env.Meta["system"] == true {
		t.Fatal("old helper would drop the notice instead of injecting")
	}
	if notice.KeyID != "source" || notice.SessionName != "sender" || notice.Provenance != model.FailureNoticeProvenance || env.Meta["kind"] != "reply" {
		t.Fatalf("notice identity/semantics %+v %+v", notice, env.Meta)
	}
	first, err := db.ClaimSessionDelivery(ctx, "source", "sender", now)
	if err != nil || first == nil || first.ID != id {
		t.Fatalf("claim %+v %v", first, err)
	}
	retry, err := db.ClaimSessionDelivery(ctx, "source", "sender", now.Add(2*time.Second))
	if err != nil || retry == nil || retry.ID != id {
		t.Fatalf("lost ACK must retry same ID: %+v %v", retry, err)
	}
	if err := db.UpdateSessionDelivery(ctx, id, "processed", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SweepSessionDeliveryFailures(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetSessionDelivery(ctx, id)
	if got.State != "processed" {
		t.Fatal("sweep regressed processed notice")
	}
	var count int
	db.db.QueryRowContext(ctx, `SELECT count(*) FROM session_delivery WHERE source_type='failure_notice'`).Scan(&count)
	if count != 1 {
		t.Fatalf("recursive/duplicate notices %d", count)
	}
}
func TestFailureNoticeGlobalSweepEveryNonterminalState(t *testing.T) {
	db, now := noticeFixture(t)
	ctx := context.Background()
	for _, state := range []string{"queued", "sent", "delivered", "injected", "capability_pending", "other_pending"} {
		d := noticeOriginal(t, db, "expire-"+state, state, "", now.Add(-31*time.Minute))
		if state == "capability_pending" {
			db.db.ExecContext(ctx, `UPDATE session_delivery SET provenance='capability_pending_ack_v1' WHERE id=?`, d.ID)
		}
	}
	for _, state := range []string{"processed", "dead", "cancelled"} {
		noticeOriginal(t, db, "terminal-"+state, state, "unlisted", now.Add(-time.Hour))
	}
	noticeOriginal(t, db, "fresh", "queued", "", now)
	attempt := noticeOriginal(t, db, "attempt-cap", "queued", "", now)
	db.db.ExecContext(ctx, `UPDATE session_delivery SET attempts=12 WHERE id=?`, attempt.ID)
	rows, err := db.SweepSessionDeliveryFailures(ctx, now)
	if err != nil || len(rows) != 7 {
		t.Fatalf("sweep %d %v", len(rows), err)
	}
	for _, d := range rows {
		n, _ := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID(d.ID))
		if n == nil {
			t.Fatalf("missing notice %s", d.ID)
		}
	}
	for _, state := range []string{"processed", "dead", "cancelled"} {
		d, _ := db.GetSessionDelivery(ctx, "terminal-"+state)
		if d.State != state {
			t.Fatal("terminal state changed")
		}
	}
	fresh, _ := db.GetSessionDelivery(ctx, "fresh")
	if fresh.State != "queued" {
		t.Fatal("fresh row expired")
	}
	if _, err := db.SweepSessionDeliveryFailures(ctx, now.Add(31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int
	db.db.QueryRowContext(ctx, `SELECT count(*) FROM session_delivery WHERE source_type='failure_notice'`).Scan(&count)
	if count != 8 {
		t.Fatalf("notice expiry must not recurse: got %d", count)
	}
}
func TestFailureNoticeRecentDeadBackfillAndConcurrentSweep(t *testing.T) {
	db, now := noticeFixture(t)
	ctx := context.Background()
	reasons := []string{"delivery_ack_unsupported", "delivery_capability_unconfirmed_expired", "delivery_expired"}
	for _, reason := range reasons {
		d := noticeOriginal(t, db, "recent-"+reason, "dead", reason, now.Add(-2*time.Hour))
		db.db.ExecContext(ctx, `UPDATE session_delivery SET updated_at=? WHERE id=?`, now.Add(-29*time.Minute).Format(time.RFC3339Nano), d.ID)
		noticeOriginal(t, db, "31min-"+reason, "dead", reason, now.Add(-31*time.Minute))
	}
	noticeOriginal(t, db, "historical-retry", "dead", "retry_exhausted", now.Add(-time.Minute))
	noticeOriginal(t, db, "too-old", "dead", "delivery_expired", now.Add(-31*time.Minute))
	noticeOriginal(t, db, "unknown", "dead", "cli_unavailable", now.Add(-time.Minute))
	noticeOriginal(t, db, "future", "dead", "delivery_expired", now.Add(time.Minute))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := db.SweepSessionDeliveryFailures(ctx, now); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	db.db.QueryRowContext(ctx, `SELECT count(*) FROM session_delivery WHERE source_type='failure_notice'`).Scan(&count)
	if count != 3 {
		t.Fatalf("backfill duplicated/outside window: %d", count)
	}
	excluded := []string{"historical-retry", "too-old", "unknown", "future"}
	for _, reason := range reasons {
		excluded = append(excluded, "31min-"+reason)
	}
	for _, id := range excluded {
		n, _ := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID(id))
		if n != nil {
			t.Fatalf("unproven old terminal backfilled: %s", id)
		}
	}
}
func TestFailureNoticeRetryExhaustionIsAtomic(t *testing.T) {
	db, now := noticeFixture(t)
	ctx := context.Background()
	d := noticeOriginal(t, db, "retry", "queued", "", now)
	db.db.ExecContext(ctx, `CREATE TRIGGER fail_notice BEFORE INSERT ON session_delivery WHEN NEW.source_type='failure_notice' BEGIN SELECT RAISE(ABORT,'synthetic'); END`)
	if _, _, _, err := db.FailSessionDeliveryRetryable(ctx, d.ID, "cli_unavailable", now, time.Second, time.Minute, 1); err == nil {
		t.Fatal("fault not propagated")
	}
	got, _ := db.GetSessionDelivery(ctx, d.ID)
	if got.State != "queued" {
		t.Fatal("retry dead committed without notice")
	}
	db.db.ExecContext(ctx, `DROP TRIGGER fail_notice`)
	dead, failed, _, err := db.FailSessionDeliveryRetryable(ctx, d.ID, "cli_unavailable", now, time.Second, time.Minute, 1)
	if err != nil || !dead || failed != 1 {
		t.Fatalf("retry %+v %d %v", dead, failed, err)
	}
	got, _ = db.GetSessionDelivery(ctx, d.ID)
	if got.LastError != "cli_unavailable" {
		t.Fatal("original retry diagnostic lost")
	}
	n, _ := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID(d.ID))
	if n == nil || !strings.Contains(n.Envelope, "retry_exhausted") {
		t.Fatal("notice missing")
	}
}
func TestFailureNoticeOnlyKnownACKSessionSource(t *testing.T) {
	for _, mode := range []string{"unknown", "no-ack", "feishu", "notice"} {
		t.Run(mode, func(t *testing.T) {
			db, now := noticeFixture(t)
			ctx := context.Background()
			d := noticeOriginal(t, db, mode, "queued", "", now.Add(-time.Hour))
			switch mode {
			case "unknown":
				db.db.ExecContext(ctx, `DELETE FROM session_endpoint WHERE key_id='source'`)
			case "no-ack":
				db.SetSessionEndpointDeliveryACK(ctx, "source", "sender", false)
			case "feishu":
				db.db.ExecContext(ctx, `UPDATE session_delivery SET source_type='feishu_inbound' WHERE id=?`, d.ID)
			case "notice":
				db.db.ExecContext(ctx, `UPDATE session_delivery SET source_type='failure_notice',provenance=? WHERE id=?`, model.FailureNoticeProvenance, d.ID)
			}
			if _, err := db.SweepSessionDeliveryFailures(ctx, now); err != nil {
				t.Fatal(err)
			}
			n, _ := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID(d.ID))
			if n != nil {
				t.Fatal("ineligible source or recursive notice")
			}
		})
	}
}

func TestFailureNoticeDistinctTargetsSharingOriginalMessageID(t *testing.T) {
	db, now := noticeFixture(t)
	ctx := context.Background()
	for _, target := range []string{"a", "b"} {
		raw, _ := json.Marshal(model.Envelope{ID: "shared-message", From: "sender#source", To: target + "#target", Body: "task"})
		if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "failed-" + target, KeyID: "target", SessionName: target, Envelope: string(raw), SourceMessageID: "shared-message", SourceType: "session_inbound", CreatedAt: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.SweepSessionDeliveryFailures(ctx, now); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"a", "b"} {
		n, _ := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("failed-"+target))
		if n == nil {
			t.Fatalf("existing source uniqueness index collapsed failure for target %s", target)
		}
	}
}

func TestFailureNoticeSafeTargetAndChineseReason(t *testing.T) {
	reasons := map[string]string{
		"retry_exhausted":                         "重试次数已用尽，仍未确认完成投递",
		"delivery_ack_unsupported":                "目标不支持投递确认，无法确认完成投递",
		"delivery_capability_unconfirmed_expired": "等待目标确认投递能力已超时",
		"delivery_expired":                        "投递等待已超时",
	}
	for reason, meaning := range reasons {
		t.Run(reason, func(t *testing.T) {
			db, now := noticeFixture(t)
			ctx := context.Background()
			raw, _ := json.Marshal(model.Envelope{ID: "m", From: "sender#source", To: "untrusted-address#RAW_PRIVATE_KEY", Body: "SECRET_BODY $(touch /tmp/COMMAND) @someone #route"})
			d := model.SessionDelivery{ID: "safe-target", KeyID: "RAW_ACCOUNT_KEY", SessionName: "目标Agent.Ab_2-9", Envelope: string(raw), SourceMessageID: "m", SourceType: "session_inbound", State: "queued", CreatedAt: now}
			if err := db.EnqueueSessionDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateSessionDelivery(ctx, d.ID, "dead", reason, now); err != nil {
				t.Fatal(err)
			}
			n, err := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID(d.ID))
			if err != nil || n == nil {
				t.Fatalf("missing notice %v", err)
			}
			var e model.Envelope
			if err := json.Unmarshal([]byte(n.Envelope), &e); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(e.Body, "【系统投递失败通知】\n") || !strings.Contains(e.Body, "Agent「目标Agent.Ab_2-9」") || !strings.Contains(e.Body, meaning) {
				t.Fatal("safe target or Chinese meaning missing")
			}
			for _, secret := range []string{"RAW_PRIVATE_KEY", "RAW_ACCOUNT_KEY", "SECRET_BODY", "COMMAND", "untrusted-address", "@", "#"} {
				if strings.Contains(e.Body, secret) {
					t.Fatal("unsafe source content reached body")
				}
			}
		})
	}
	for _, bad := range []string{"", "evil@route", "seat#SECRET", "$(command)", "x\ny", "<script>", "a\u202eb", strings.Repeat("a", 65), strings.Repeat("界", 65)} {
		if safeFailureNoticeTarget(bad) != "目标 Agent（名称无法安全显示）" {
			t.Fatal("unsafe or oversized target rendered")
		}
	}
	if safeFailureNoticeTarget(strings.Repeat("a", 64)) != "Agent「"+strings.Repeat("a", 64)+"」" {
		t.Fatal("bounded valid target rejected")
	}
}
