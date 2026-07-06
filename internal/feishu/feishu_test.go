package feishu

import (
	"context"
	"testing"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestFakeGatewayRecordsSentMessages(t *testing.T) {
	f := &Fake{}
	msgID, err := f.Send(context.Background(), OutMessage{BotChannelID: "bot1", ToID: "chat1", Text: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msgID == "" {
		t.Fatalf("empty fake msg id")
	}
	sent := f.SentMessages()
	if len(sent) != 1 || sent[0].ToID != "chat1" || sent[0].Text != "hello" {
		t.Fatalf("sent = %+v", sent)
	}
	sent[0].Text = "mutated"
	if f.SentMessages()[0].Text != "hello" {
		t.Fatalf("SentMessages did not return a copy")
	}
}

func TestLarkGatewayMessageFromEventPersonal(t *testing.T) {
	g := &LarkGateway{botChannelID: "dev"}
	msgID := "om_1"
	chatType := "p2p"
	content := `{"text":"帮助"}`
	createTime := "1719820800000"
	openID := "ou_1"
	msg, err := g.messageFromEvent(&larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &openID}},
		Message: &larkim.EventMessage{
			MessageId:  &msgID,
			ChatType:   &chatType,
			Content:    &content,
			CreateTime: &createTime,
		},
	}})
	if err != nil {
		t.Fatalf("messageFromEvent: %v", err)
	}
	if msg.ID != msgID || msg.FeishuMsgID != msgID || msg.ChatEntityID != "dev:personal:ou_1" || msg.ChatType != model.ChatPersonal {
		t.Fatalf("message = %+v", msg)
	}
	if msg.SenderOpenID != openID || msg.Content != content {
		t.Fatalf("message sender/content = %+v", msg)
	}
	if want := time.UnixMilli(1719820800000).UTC(); !msg.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt = %s, want %s", msg.CreatedAt, want)
	}
}

func TestLarkGatewayMessageFromEventGroup(t *testing.T) {
	g := &LarkGateway{botChannelID: "dev"}
	msgID := "om_2"
	chatType := "group"
	chatID := "oc_1"
	openID := "ou_1"
	msg, err := g.messageFromEvent(&larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &openID}},
		Message: &larkim.EventMessage{
			MessageId: &msgID,
			ChatType:  &chatType,
			ChatId:    &chatID,
			Content:   ptr("plain text"),
		},
	}})
	if err != nil {
		t.Fatalf("messageFromEvent: %v", err)
	}
	if msg.ChatEntityID != "dev:group:oc_1" || msg.ChatType != model.ChatGroup || msg.SenderOpenID != openID {
		t.Fatalf("message = %+v", msg)
	}
	if msg.Content != `{"text":"plain text"}` {
		t.Fatalf("Content = %s", msg.Content)
	}
}

func TestReceiveIDType(t *testing.T) {
	if got := receiveIDType("group", "oc_1"); got != "chat_id" {
		t.Fatalf("group receive id type = %s", got)
	}
	if got := receiveIDType("personal", "ou_1"); got != "open_id" {
		t.Fatalf("personal receive id type = %s", got)
	}
	if got := receiveIDType("", "oc_1"); got != "chat_id" {
		t.Fatalf("oc_ prefix receive id type = %s", got)
	}
}

func ptr(s string) *string { return &s }
