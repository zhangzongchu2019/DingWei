package extseat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestFeishuCopyTextTruncatesOnlyCopy(t *testing.T) {
	event := FeishuCopyEvent{ExternalName: "asist_AG3.zzc", OwnerDisplayName: "授权人张", TargetName: "ts3opus55.zzc", Direction: FeishuCopyExternalToInternal, Body: strings.Repeat("字", 6700)}
	got := FeishuCopyText(event)
	if len(got) > 20000 || !utf8.ValidString(got) {
		t.Fatalf("MCP_COPY_MAX_BYTES bytes=%d valid_utf8=%t", len(got), utf8.ValidString(got))
	}
	if !strings.Contains(got, "只读提醒") || !strings.Contains(got, "原消息未截断") || len(event.Body) <= FeishuCopyMaxBytes {
		t.Fatalf("MCP_COPY_TRUNCATE_ONLY_COPY missing notice or mutated source")
	}
}

func TestFeishuCopyTextMarksInternalReply(t *testing.T) {
	event := FeishuCopyEvent{ExternalName: "asist_AG3.zzc", OwnerShortName: "zzc", TargetName: "ts3opus55.zzc", Direction: FeishuCopyInternalToExternal, Body: "回复正文"}
	got := FeishuCopyText(event)
	if !strings.Contains(got, "内部目标回复") || !strings.Contains(got, "授权人 zzc") || !strings.Contains(got, event.Body) || !strings.Contains(got, event.ExternalName) || !strings.Contains(got, event.TargetName) {
		t.Fatalf("MCP_COPY_BIDIRECTIONAL_HEADER text=%q", got)
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
		if err = row.Scan(&direction, &body); err != nil || direction != want.direction || body != want.body {
			t.Fatalf("copy event=%s direction=%q body=%q err=%v", want.id, direction, body, err)
		}
	}
	// Fixture timestamps are historical by design. Make the two rows due and live for the worker.
	if _, err = f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_feishu_copy SET created_at=now(),expires_at=now()+interval '1 hour',next_attempt_at=now()`); err != nil {
		t.Fatal(err)
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
