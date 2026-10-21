package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestClarificationReplyEvidencePersistsQueueRestart(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "batch"}[batch], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reply.db")
			db := openMigrated(t, path)
			ctx := context.Background()
			if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "reply-bot", Name: "fixture", Active: true}); err != nil {
				t.Fatal(err)
			}
			if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "reply-person", BotChannelID: "reply-bot", Type: model.ChatPersonal, FeishuID: "ou_pm", Active: true}); err != nil {
				t.Fatal(err)
			}
			meta := &model.FeishuReplyMetadata{Authenticated: true, EventID: "event", MessageID: "om_reply", ParentID: "om_notice", RootID: "om_root", MessageType: "text", Tenant: "tenant", AppID: "app", ChatID: "oc_dm", SenderType: "user"}
			m := model.Message{ID: "reply-queue", ChatEntityID: "reply-person", Direction: model.DirectionIn, BotChannelID: "reply-bot", FeishuMsgID: "om_reply", ChatType: model.ChatPersonal, SenderOpenID: "ou_pm", IngressProvenance: model.IngressFeishuWSAuthenticated, FeishuReply: meta, Content: `{"text":"解释"}`, CreatedAt: time.Now().UTC()}
			var err error
			if batch {
				err = db.BatchEnqueueMessages(ctx, []model.Message{m})
			} else {
				err = db.EnqueueMessage(ctx, m)
			}
			if err != nil {
				t.Fatal(err)
			}
			// A new event ID for the same message must be remembered even when
			// INSERT OR IGNORE deduplicates the already queued provider message.
			alias := m
			copy := *meta
			copy.EventID = "event-alias"
			alias.FeishuReply = &copy
			if err = db.EnqueueMessage(ctx, alias); err != nil {
				t.Fatal(err)
			}
			conflict := alias
			other := copy
			other.MessageID = "om_other"
			conflict.ID = "other-queue"
			conflict.FeishuMsgID = "om_other"
			conflict.FeishuReply = &other
			if err = db.EnqueueMessage(ctx, conflict); !errors.Is(err, ErrTaskFlowConflict) {
				t.Fatal("CLARIFICATION_EVENT_ALIAS_LOST_AT_QUEUE_DEDUP", err)
			}
			changed := m
			changed.Content = `{"text":"altered same message"}`
			if err = db.EnqueueMessage(ctx, changed); !errors.Is(err, ErrTaskFlowConflict) {
				t.Fatal("CLARIFICATION_CHANGED_MESSAGE_IGNORED_AT_QUEUE", err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			db = openMigrated(t, path)
			defer db.Close()
			got, err := db.ClaimNextMessage(ctx, model.DirectionIn)
			if err != nil || got == nil || !reflect.DeepEqual(got.FeishuReply, meta) || got.IngressProvenance != m.IngressProvenance {
				t.Fatal("CLARIFICATION_QUEUE_REPLY_EVIDENCE_LOST", got, err)
			}
		})
	}
}
