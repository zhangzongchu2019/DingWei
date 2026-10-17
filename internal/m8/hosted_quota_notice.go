package m8

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var errQuotaNoticeUnavailable = errors.New("quota_notice_unavailable")

// QueueHostedQuotaNotice has two configured destinations, never a node-supplied
// address. The owner goes to their bound Feishu identity; ops to the exact active
// enrolled Agent. Missing routes fail closed and leave the outbox pending.
func (h *Hub) QueueHostedQuotaNotice(ctx context.Context, owner, opsFullName, recipient, id, text string) error {
	if h.Identity == nil || h.Repo == nil || owner == "" || !strings.HasPrefix(id, "quota-notice-") || len(text) > 4096 {
		return errQuotaNoticeUnavailable
	}
	u, err := h.Identity.Store.GetUser(ctx, owner)
	if err != nil || u.State != identity.UserActive || u.Type != identity.UserPlatformAdmin {
		return errQuotaNoticeUnavailable
	}
	switch recipient {
	case "owner":
		ident, err := h.Identity.Store.GetIdentity(ctx, owner, "feishu")
		if err != nil || ident.Subject == "" || ident.Issuer == "" {
			return errQuotaNoticeUnavailable
		}
		content, _ := json.Marshal(map[string]string{"text": text})
		return h.Repo.EnqueueMessage(ctx, model.Message{ID: id, Direction: model.DirectionOut, Status: "queued", ChatEntityID: ident.Issuer + ":personal:" + ident.Subject, BotChannelID: ident.Issuer, FeishuMsgID: id, ChatType: model.ChatPersonal, Content: string(content)})
	case "ops":
		ag, user, err := h.Identity.ResolveFullName(ctx, opsFullName)
		if err != nil || ag.State != identity.AgentActive || user.State != identity.UserActive || h.Repo == nil {
			return errQuotaNoticeUnavailable
		}
		env := model.Envelope{ID: id, To: opsFullName, From: "#workpulse", Body: text, TS: time.Now().Unix(), Meta: map[string]any{"type": "agent_route", "system_notification": true, "reply_policy": "no_reply", "no_mirror": true}}
		keyID := AgentKeyID(ag.OwnerLC)
		supported, known, err := h.Repo.SessionEndpointDeliveryACK(ctx, keyID, ag.NameLC())
		if err != nil || !known || !supported {
			return errQuotaNoticeUnavailable
		}
		deliveryID := "dw-" + id
		env.Meta["type"] = "delivery"
		env.Meta["delivery_id"] = deliveryID
		env.Meta["delivery_version"] = 1
		env.Meta["ack_required"] = true
		env.Meta["source_message_id"] = id
		payload, err := json.Marshal(sessionClientEnvelope(env))
		if err != nil {
			return errQuotaNoticeUnavailable
		}
		if err = h.Repo.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: deliveryID, KeyID: keyID, SessionName: ag.NameLC(), Envelope: string(payload), SourceMessageID: id, SourceType: "hosted_quota_notice"}); err != nil {
			return err
		}
		h.mu.Lock()
		c := h.sessionClients[keyID][ag.NameLC()]
		h.mu.Unlock()
		if c != nil {
			h.drainSessionDeliveries(ctx, c)
		}
		return nil
	}
	return errQuotaNoticeUnavailable
}
