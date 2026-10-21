package feishu

import (
	"encoding/json"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func clarificationEvent() *larkim.P2MessageReceiveV1 {
	ptr := func(s string) *string { return &s }
	return &larkim.P2MessageReceiveV1{EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "event-one", AppID: "app-one", TenantKey: "tenant-one"}}, Event: &larkim.P2MessageReceiveV1Data{
		Sender:  &larkim.EventSender{SenderId: &larkim.UserId{OpenId: ptr("ou_pm")}, SenderType: ptr("user"), TenantKey: ptr("tenant-one")},
		Message: &larkim.EventMessage{MessageId: ptr("om_reply"), ParentId: ptr("om_exact_notice"), RootId: ptr("om_other_root"), MessageType: ptr("text"), ChatType: ptr("p2p"), ChatId: ptr("oc_dm"), Content: ptr(`{"text":"没有前缀的解释"}`)},
	}}
}

func TestClarificationSDKPreservesAuthenticatedReplyMetadata(t *testing.T) {
	g := &LarkGateway{botChannelID: "bot-one", appID: "app-one"}
	event := clarificationEvent()
	m, err := g.messageFromEvent(event)
	if err != nil || m.FeishuReply == nil || !m.FeishuReply.Authenticated || m.FeishuReply.ParentID != "om_exact_notice" || m.FeishuReply.RootID != "om_other_root" || m.FeishuReply.AppID != "app-one" || m.FeishuReply.Tenant != "tenant-one" || m.FeishuReply.EventID != "event-one" || m.FeishuReply.SenderType != "user" || m.SenderOpenID != "ou_pm" {
		t.Fatal("CLARIFICATION_SDK_REPLY_EVIDENCE_LOST", m.FeishuReply, err)
	}
	// Ordinary client JSON cannot manufacture this trusted in-memory field.
	var forged model.Message
	if err = json.Unmarshal([]byte(`{"FeishuReply":{"authenticated":true,"parent_id":"om_exact_notice"}}`), &forged); err != nil || forged.FeishuReply != nil {
		t.Fatal("CLARIFICATION_REPLY_METADATA_JSON_FORGERY", forged.FeishuReply, err)
	}
}

func TestClarificationSDKRejectsApplicationAndTenantDrift(t *testing.T) {
	g := &LarkGateway{botChannelID: "bot-one", appID: "app-one"}
	e := clarificationEvent()
	e.EventV2Base.Header.AppID = "another-app"
	if _, err := g.messageFromEvent(e); err == nil {
		t.Fatal("CLARIFICATION_SDK_WRONG_APP_ACCEPTED")
	}
	e = clarificationEvent()
	e.EventV2Base.Header.TenantKey = "another-tenant"
	if _, err := g.messageFromEvent(e); err == nil {
		t.Fatal("CLARIFICATION_SDK_WRONG_TENANT_ACCEPTED")
	}
}

func TestClarificationWebhookNeverClaimsReplyAuthority(t *testing.T) {
	for _, body := range []string{
		`{"msg_id":"m","open_id":"u","parent_id":"notice","text":"解释"}`,
		`{"header":{"event_type":"im.message.receive_v1"},"event":{"sender":{"sender_id":{"open_id":"u"}},"message":{"message_id":"m","parent_id":"notice","chat_type":"p2p","content":"{\"text\":\"解释\"}"}}}`,
		`{"type":"event_callback","event":{"type":"message","message_id":"m","parent_id":"notice","open_id":"u","chat_type":"p2p","text":"解释"}}`,
	} {
		m, err := WebhookMessageFromPayload("bot", []byte(body))
		if err != nil || m.FeishuReply == nil || m.FeishuReply.Authenticated || m.FeishuReply.ParentID != "notice" {
			t.Fatal("CLARIFICATION_WEBHOOK_AUTHORITY_FORGED", m.FeishuReply, err)
		}
	}
}
