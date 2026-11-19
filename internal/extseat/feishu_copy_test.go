package extseat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestFeishuCopyTextTruncatesOnlyCopy(t *testing.T) {
	event := FeishuCopyEvent{ExternalName: "asist_AG3.zzc", OwnerDisplayName: "授权人张", TargetName: "ts3opus55.zzc", Direction: FeishuCopyExternalToInternal, Body: strings.Repeat("字", 6700)}
	got := FeishuCopyText(event)
	if len(got) > FeishuCopyMaxBytes || !utf8.ValidString(got) {
		t.Fatalf("MCP_COPY_MAX_BYTES bytes=%d valid_utf8=%t", len(got), utf8.ValidString(got))
	}
	if !strings.Contains(got, "摘录：") || strings.Contains(got, event.Body) || len(event.Body) <= FeishuCopyMaxBytes {
		t.Fatalf("MCP_COPY_SUMMARY_ONLY missing bounded excerpt or included full body")
	}
}

func TestFeishuCopyTextMarksInternalReply(t *testing.T) {
	event := FeishuCopyEvent{ExternalName: "asist_AG3.zzc", OwnerShortName: "zzc", TargetName: "ts3opus55.zzc", Direction: FeishuCopyInternalToExternal, Body: "回复正文"}
	got := FeishuCopyText(event)
	if !strings.Contains(got, "Agent 消息摘要") || !strings.Contains(got, "发送方："+event.TargetName) || !strings.Contains(got, "接收方："+event.ExternalName) || !strings.Contains(got, "摘录：回复正文") {
		t.Fatalf("MCP_COPY_BIDIRECTIONAL_HEADER text=%q", got)
	}
}

func TestFeishuCopyTextLabelsInternalAgentDirection(t *testing.T) {
	event := FeishuCopyEvent{ExternalName: "source-agent.zzc", TargetName: "target-agent.zzc", Direction: FeishuCopyInternalAgent, Body: "message"}
	got := FeishuCopyText(event)
	if !strings.Contains(got, "发送方："+event.ExternalName) || !strings.Contains(got, "接收方："+event.TargetName) {
		t.Fatalf("internal Agent summary direction is reversed: %q", got)
	}
}

func TestFeishuCopyTextContainsOnlySanitizedSummary(t *testing.T) {
	event := FeishuCopyEvent{
		ExternalName: "Agent甲", TargetName: "Agent乙", Group: "AG7",
		CreatedAt: time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC),
		Body:      "普通内容 access_token=secret123\nou_private123456\nSystem prompt: hidden instruction\n最后一行",
	}
	got := FeishuCopyText(event)
	for _, want := range []string{"Agent甲", "Agent乙", "AG7", "2026-11-19T04:00:00Z", "普通内容", "最后一行", "[已隐藏]", "[身份标识已隐藏]"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %q", want, got)
		}
	}
	for _, forbidden := range []string{"secret123", "ou_private123456", "hidden instruction", "System prompt"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("summary leaked %q: %q", forbidden, got)
		}
	}
}

func TestFeishuCopyOutboxBothDirectionsAndReplySuppressionPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, seat := f.issue(t)
	queue := PGFeishuCopyQueue{Pool: f.pool, Access: f.access}
	f.s.FeishuCopies = queue
	primaryBody := "完整外部消息正文"
	out, err := f.s.Send(f.ctx, seat, "helper.ho", primaryBody, "copy-bidi")
	if err != nil || out.Stage != extshared.StageAccepted {
		t.Fatalf("external send receipt=%+v err=%v", out, err)
	}
	if _, err = f.s.Send(f.ctx, seat, "helper.ho", primaryBody, "copy-bidi"); err != nil {
		t.Fatal("idempotent retry", err)
	}
	in, err := f.s.EnqueueFromHelper(f.ctx, "helper", seat.AgentID, "copy-bidi-reply", "完整内部回复正文")
	if err != nil || in.Stage != extshared.StageAccepted {
		t.Fatalf("internal reply receipt=%+v err=%v", in, err)
	}
	row, err := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_feishu_copy WHERE seat_id=$1`, seat.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = row.Scan(&count); err != nil || count != 2 {
		t.Fatalf("MCP_COPY_BIDIRECTIONAL_HEADER event_count=%d err=%v", count, err)
	}
	for _, want := range []struct{ id, direction, body string }{
		{out.DeliveryID, FeishuCopyExternalToInternal, primaryBody},
		{in.DeliveryID, FeishuCopyInternalToExternal, "完整内部回复正文"},
	} {
		row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT direction,body FROM np_extseat_feishu_copy WHERE event_id=$1`, want.id)
		if err != nil {
			t.Fatal(err)
		}
		var direction, body string
		if err = row.Scan(&direction, &body); err != nil || direction != want.direction || body != safeFeishuExcerpt(want.body) {
			t.Fatalf("copy event=%s direction=%q body=%q err=%v", want.id, direction, body, err)
		}
	}
	// Fixture timestamps are historical by design. Give the rows distinct, fixed
	// order so ProjectOne deterministically selects out before in.
	for _, due := range []struct {
		id    string
		delta string
	}{{out.DeliveryID, "2 seconds"}, {in.DeliveryID, "1 second"}} {
		if _, err = f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_feishu_copy SET created_at=now()-$2::interval,expires_at=now()+interval '1 hour',next_attempt_at=now() WHERE event_id=$1`, due.id, due.delta); err != nil {
			t.Fatal(err)
		}
	}
	for _, eventID := range []string{out.DeliveryID, in.DeliveryID} {
		did, projectErr := queue.ProjectOne(context.Background(), func(_ context.Context, event FeishuCopyEvent) (FeishuCopyReceipt, error) {
			if event.ID != eventID {
				return FeishuCopyReceipt{}, errors.New("unexpected event selected")
			}
			return FeishuCopyReceipt{BotChannelID: "bot-channel", MessageID: "feishu-" + eventID}, nil
		})
		if projectErr != nil || !did {
			t.Fatalf("project %s did=%v err=%v", eventID, did, projectErr)
		}
		row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT state FROM np_extseat_feishu_copy WHERE event_id=$1`, eventID)
		if err != nil {
			t.Fatal(err)
		}
		var state string
		if err = row.Scan(&state); err != nil || state != "sent" {
			t.Fatalf("MCP_COPY_EVENT_SENT event=%s state=%q err=%v", eventID, state, err)
		}
		ok, lookupErr := queue.IsCopyReply(f.ctx, "bot-channel", "feishu-"+eventID)
		if lookupErr != nil || !ok {
			t.Fatalf("MCP_COPY_REPLY_LOOP_SUPPRESSED event=%s ok=%v err=%v", eventID, ok, lookupErr)
		}
	}
	ok, err := queue.IsCopyReply(f.ctx, "another-channel", "feishu-"+out.DeliveryID)
	if err != nil || ok {
		t.Fatalf("reply lookup crossed bot channel: ok=%v err=%v", ok, err)
	}
}

func TestFeishuCopyDeliveryFailureRetriesIndependentlyPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, seat := f.issue(t)
	queue := PGFeishuCopyQueue{Pool: f.pool, Access: f.access}
	f.s.FeishuCopies = queue
	primary, err := f.s.Send(f.ctx, seat, "helper.ho", "主消息保持完整", "copy-retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_feishu_copy SET created_at=now(),expires_at=now()+interval '1 hour',next_attempt_at=now() WHERE event_id=$1`, primary.DeliveryID); err != nil {
		t.Fatal(err)
	}
	did, err := queue.ProjectOne(f.ctx, func(context.Context, FeishuCopyEvent) (FeishuCopyReceipt, error) {
		return FeishuCopyReceipt{}, errors.New("synthetic Feishu outage")
	})
	if err != nil || !did {
		t.Fatalf("first copy attempt did=%v err=%v", did, err)
	}
	row, err := f.pool.QueryRow(f.ctx, f.access, `SELECT state,attempts FROM np_extseat_feishu_copy WHERE event_id=$1`, primary.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	if err = row.Scan(&state, &attempts); err != nil || state != "queued" || attempts != 1 {
		t.Fatalf("MCP_COPY_DELIVERY_RETRY_ISOLATED copy state=%s attempts=%d err=%v", state, attempts, err)
	}
	row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_outbox WHERE delivery_id=$1`, primary.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	var mainCount int
	if err = row.Scan(&mainCount); err != nil || mainCount != 1 {
		t.Fatalf("MCP_COPY_DELIVERY_RETRY_ISOLATED main outbox count=%d err=%v", mainCount, err)
	}
	if _, err = f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_feishu_copy SET next_attempt_at=now() WHERE event_id=$1`, primary.DeliveryID); err != nil {
		t.Fatal(err)
	}
	did, err = queue.ProjectOne(f.ctx, func(_ context.Context, event FeishuCopyEvent) (FeishuCopyReceipt, error) {
		if event.ID != primary.DeliveryID {
			return FeishuCopyReceipt{}, errors.New("copy event ID changed on retry")
		}
		return FeishuCopyReceipt{BotChannelID: "retry-bot", MessageID: "retry-message"}, nil
	})
	if err != nil || !did {
		t.Fatalf("retry copy attempt did=%v err=%v", did, err)
	}
	row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT state,attempts FROM np_extseat_feishu_copy WHERE event_id=$1`, primary.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&state, &attempts); err != nil || state != "sent" || attempts != 2 {
		t.Fatalf("retry result state=%s attempts=%d err=%v", state, attempts, err)
	}
}

func TestFeishuCopyPersistentPreferenceAndInternalRecipientResolutionPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	queue := PGFeishuCopyQueue{Pool: f.pool, Access: f.access}
	if _, err := f.pool.Exec(f.ctx, f.access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('inside','helperowner','inside','inside','selfhost','active',now()); INSERT INTO np_identity(provider,issuer,subject,user_id_lc) VALUES('feishu','verified-app','ou_owner123456','helperowner')`); err != nil {
		t.Fatal(err)
	}
	event := AgentFeishuCopyEvent{ID: "internal-copy-1", AgentID: "inside", SourceName: "inside.zzc", TargetName: "helper.zzc", Group: "G1", Body: "private content", CreatedAt: time.Now().UTC()}
	if err := queue.EnqueueAgent(f.ctx, event); err != nil {
		t.Fatal(err)
	}
	var count int
	row, err := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_agent_feishu_copy_outbox WHERE agent_id='inside'`)
	if err != nil || row.Scan(&count) != nil || count != 0 {
		t.Fatalf("INTERNAL_COPY_DEFAULT_OFF count=%d err=%v", count, err)
	}
	if err := queue.SetPreference(f.ctx, "helperowner", "inside", true); err != nil {
		t.Fatal(err)
	}
	if err := queue.EnqueueAgent(f.ctx, event); err != nil {
		t.Fatal(err)
	}
	var gotIssuer, gotSubject, excerpt string
	row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT recipient_issuer,recipient_subject,excerpt FROM np_agent_feishu_copy_outbox WHERE event_id=$1`, event.ID)
	if err != nil || row.Scan(&gotIssuer, &gotSubject, &excerpt) != nil || gotIssuer != "verified-app" || gotSubject != "ou_owner123456" || excerpt != "private content" {
		t.Fatalf("INTERNAL_COPY_VERIFIED_OWNER recipient=%q/%q excerpt=%q err=%v", gotIssuer, gotSubject, excerpt, err)
	}
	did, err := queue.ProjectAgentOne(f.ctx, func(_ context.Context, copied FeishuCopyEvent) (FeishuCopyReceipt, error) {
		if copied.Direction != FeishuCopyInternalAgent || copied.Group != event.Group || copied.ExternalName != event.SourceName || copied.TargetName != event.TargetName || copied.Body != event.Body {
			t.Fatalf("INTERNAL_COPY_SUMMARY_FIELDS event=%+v", copied)
		}
		return FeishuCopyReceipt{BotChannelID: "verified-bot", MessageID: "internal-copy-message"}, nil
	})
	if err != nil || !did {
		t.Fatalf("internal projection did=%v err=%v", did, err)
	}
	if ok, err := queue.IsCopyReply(f.ctx, "verified-bot", "internal-copy-message"); err != nil || !ok {
		t.Fatalf("INTERNAL_COPY_REPLY_MUST_NOT_ROUTE ok=%v err=%v", ok, err)
	}
	if err := queue.SetPreference(f.ctx, "helperowner", "inside", false); err != nil {
		t.Fatal(err)
	}
	event.ID = "internal-copy-optout"
	if err := queue.EnqueueAgent(f.ctx, event); err != nil {
		t.Fatal(err)
	}
	row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_agent_feishu_copy_outbox WHERE agent_id='inside'`)
	if err != nil || row.Scan(&count) != nil || count != 1 {
		t.Fatalf("PERSISTENT_OPTOUT_PRECEDES_ENV_OR_DEFAULT count=%d err=%v", count, err)
	}
	if _, err := f.pool.Exec(f.ctx, f.access, `INSERT INTO np_identity(provider,issuer,subject,user_id_lc) VALUES('feishu','second-app','ou_owner654321','helperowner')`); err != nil {
		t.Fatal(err)
	}
	if err := queue.SetPreference(f.ctx, "helperowner", "inside", true); err != nil {
		t.Fatal(err)
	}
	event.ID = "internal-copy-recipient-revoked"
	if err := queue.EnqueueAgent(f.ctx, event); !errors.Is(err, extshared.ErrNotPermitted) {
		t.Fatalf("INTERNAL_COPY_AMBIGUOUS_IDENTITY guessed recipient: %v", err)
	}
	// A queued copy created while there was exactly one verified identity is
	// invalidated if identity state changes before delivery.
	if _, err := f.pool.Exec(f.ctx, f.access, `DELETE FROM np_identity WHERE issuer='second-app'`); err != nil {
		t.Fatal(err)
	}
	if err := queue.EnqueueAgent(f.ctx, event); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, f.access, `INSERT INTO np_identity(provider,issuer,subject,user_id_lc) VALUES('feishu','second-app','ou_owner654321','helperowner')`); err != nil {
		t.Fatal(err)
	}
	sent := false
	did, err = queue.ProjectAgentOne(f.ctx, func(context.Context, FeishuCopyEvent) (FeishuCopyReceipt, error) {
		sent = true
		return FeishuCopyReceipt{BotChannelID: "bad", MessageID: "bad"}, nil
	})
	if err != nil || did || sent {
		t.Fatalf("INTERNAL_COPY_REVALIDATES_RECIPIENT did=%v sent=%v err=%v", did, sent, err)
	}
	row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT state,last_error FROM np_agent_feishu_copy_outbox WHERE event_id=$1`, event.ID)
	if err != nil {
		t.Fatal(err)
	}
	var invalidState, invalidReason string
	if err := row.Scan(&invalidState, &invalidReason); err != nil || invalidState != "dead" || invalidReason != "recipient_identity_changed" {
		t.Fatalf("INTERNAL_COPY_REVALIDATES_RECIPIENT state=%s reason=%s err=%v", invalidState, invalidReason, err)
	}
}

func TestExternalCopyPersistentOptOutBlocksDefaultAndLeavesPrimaryDeliveryPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, seat := f.issue(t)
	queue := PGFeishuCopyQueue{Pool: f.pool, Access: f.access}
	f.s.FeishuCopies = queue
	if err := queue.SetPreference(f.ctx, seat.OwnerLC, seat.AgentID, false); err != nil {
		t.Fatal(err)
	}
	primary, err := f.s.Send(f.ctx, seat, "helper.ho", "primary survives opt out", "copy-opt-out")
	if err != nil || primary.Stage != extshared.StageAccepted {
		t.Fatalf("PERSISTENT_OPTOUT_PRIMARY_UNAFFECTED receipt=%+v err=%v", primary, err)
	}
	row, err := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_feishu_copy WHERE event_id=$1`, primary.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := row.Scan(&count); err != nil || count != 0 {
		t.Fatalf("PERSISTENT_OPTOUT_PRECEDES_EXTERNAL_DEFAULT copy_count=%d err=%v", count, err)
	}
	if err := queue.SetPreference(f.ctx, seat.OwnerLC, seat.AgentID, true); err != nil {
		t.Fatal(err)
	}
	second, err := f.s.Send(f.ctx, seat, "helper.ho", "explicitly enabled", "copy-opt-in")
	if err != nil {
		t.Fatal(err)
	}
	row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_feishu_copy WHERE event_id=$1`, second.DeliveryID)
	if err != nil || row.Scan(&count) != nil || count != 1 {
		t.Fatalf("EXPLICIT_PREFERENCE_OVERRIDES_ORIGIN_DEFAULT copy_count=%d err=%v", count, err)
	}
}

func TestFeishuCopyHasIndependentExpiryAndTwentyAttemptLimitPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, seat := f.issue(t)
	queue := PGFeishuCopyQueue{Pool: f.pool, Access: f.access}
	f.s.FeishuCopies = queue
	receipt, err := f.s.Send(f.ctx, seat, "helper.ho", "copy lifetime", "copy-lifetime")
	if err != nil {
		t.Fatal(err)
	}
	var ttlSeconds int64
	row, err := f.pool.QueryRow(f.ctx, f.access, `SELECT extract(epoch FROM expires_at-created_at)::bigint FROM np_extseat_feishu_copy WHERE event_id=$1`, receipt.DeliveryID)
	if err != nil || row.Scan(&ttlSeconds) != nil || ttlSeconds != int64(FeishuCopyRetryTTL.Seconds()) {
		t.Fatalf("COPY_TTL_INDEPENDENT_OF_MESSAGE_TTL ttl_seconds=%d err=%v", ttlSeconds, err)
	}
	if _, err := f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_feishu_copy SET created_at=now(),expires_at=now()+interval '30 hours',next_attempt_at=now() WHERE event_id=$1`, receipt.DeliveryID); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= FeishuCopyAttemptLimit; attempt++ {
		did, projectErr := queue.ProjectOne(f.ctx, func(context.Context, FeishuCopyEvent) (FeishuCopyReceipt, error) {
			return FeishuCopyReceipt{}, errors.New("offline")
		})
		if projectErr != nil || !did {
			t.Fatalf("COPY_ATTEMPT_LIMIT attempt=%d did=%v err=%v", attempt, did, projectErr)
		}
		if attempt < FeishuCopyAttemptLimit {
			if _, err := f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_feishu_copy SET next_attempt_at=now() WHERE event_id=$1`, receipt.DeliveryID); err != nil {
				t.Fatal(err)
			}
		}
	}
	row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT state,attempts FROM np_extseat_feishu_copy WHERE event_id=$1`, receipt.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	if err := row.Scan(&state, &attempts); err != nil || state != "dead" || attempts != FeishuCopyAttemptLimit {
		t.Fatalf("COPY_ATTEMPT_LIMIT state=%s attempts=%d err=%v", state, attempts, err)
	}
}

func TestFeishuCopyExpirySweepDeadLettersWithoutSendingPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, seat := f.issue(t)
	queue := PGFeishuCopyQueue{Pool: f.pool, Access: f.access}
	f.s.FeishuCopies = queue
	receipt, err := f.s.Send(f.ctx, seat, "helper.ho", "expired copy", "copy-expired")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_feishu_copy SET expires_at=now()-interval '1 second',next_attempt_at=now() WHERE event_id=$1`, receipt.DeliveryID); err != nil {
		t.Fatal(err)
	}
	sent := false
	did, err := queue.ProjectOne(f.ctx, func(context.Context, FeishuCopyEvent) (FeishuCopyReceipt, error) {
		sent = true
		return FeishuCopyReceipt{BotChannelID: "x", MessageID: "y"}, nil
	})
	if err != nil || did || sent {
		t.Fatalf("COPY_EXPIRY_SWEEP sent=%v did=%v err=%v", sent, did, err)
	}
	row, err := f.pool.QueryRow(f.ctx, f.access, `SELECT state,last_error FROM np_extseat_feishu_copy WHERE event_id=$1`, receipt.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	var state, reason string
	if err := row.Scan(&state, &reason); err != nil || state != "dead" || reason != "message_expired" {
		t.Fatalf("COPY_EXPIRY_SWEEP state=%s reason=%s err=%v", state, reason, err)
	}
}
