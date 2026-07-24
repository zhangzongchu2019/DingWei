package feishu

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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
	if msg.IngressProvenance != model.IngressFeishuWSAuthenticated {
		t.Fatalf("provenance = %q, want ws authenticated", msg.IngressProvenance)
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
	if msg.IngressProvenance != model.IngressFeishuWSAuthenticated {
		t.Fatalf("provenance = %q, want ws authenticated", msg.IngressProvenance)
	}
	if msg.Content != `{"text":"plain text"}` {
		t.Fatalf("Content = %s", msg.Content)
	}
}

func TestLarkGatewayIngressObservabilityStagesWithoutChangingDelivery(t *testing.T) {
	var logs bytes.Buffer
	g := &LarkGateway{
		botChannelID: "dev",
		logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	msgID := "om_observed"
	chatType := "p2p"
	content := `{"text":"hello"}`
	openID := "ou_observed"
	ev := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Sender:  &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &openID}},
		Message: &larkim.EventMessage{MessageId: &msgID, ChatType: &chatType, Content: &content},
	}}
	var got model.Message
	if err := g.observeInboundEvent(context.Background(), ev, func(_ context.Context, msg model.Message) error {
		got = msg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got.FeishuMsgID != msgID || got.Content != content {
		t.Fatalf("callback message=%+v", got)
	}
	if g.ingress.eventReceived.Load() != 1 || g.ingress.normalized.Load() != 1 ||
		g.ingress.enqueued.Load() != 1 || g.ingress.rejected.Load() != 0 ||
		g.ingress.enqueueError.Load() != 0 {
		t.Fatalf("counters received=%d normalized=%d rejected=%d enqueued=%d enqueue_error=%d",
			g.ingress.eventReceived.Load(), g.ingress.normalized.Load(), g.ingress.rejected.Load(),
			g.ingress.enqueued.Load(), g.ingress.enqueueError.Load())
	}
	for _, stage := range []string{"event_received", "normalized", "message_enqueued"} {
		if !strings.Contains(logs.String(), "feishu ingress "+stage) ||
			!strings.Contains(logs.String(), msgID) {
			t.Fatalf("missing stage=%s logs=%s", stage, logs.String())
		}
	}
}

func TestLarkGatewayIngressObservabilitySeparatesRejectAndEnqueueError(t *testing.T) {
	var logs bytes.Buffer
	g := &LarkGateway{
		botChannelID: "dev",
		logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	if err := g.observeInboundEvent(context.Background(), nil, func(context.Context, model.Message) error {
		t.Fatal("normalization reject must not invoke callback")
		return nil
	}); err == nil {
		t.Fatal("nil event unexpectedly accepted")
	}
	msgID := "om_enqueue_error"
	chatType := "p2p"
	content := `{"text":"hello"}`
	openID := "ou_error"
	ev := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Sender:  &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &openID}},
		Message: &larkim.EventMessage{MessageId: &msgID, ChatType: &chatType, Content: &content},
	}}
	sentinel := errors.New("store unavailable")
	if err := g.observeInboundEvent(context.Background(), ev, func(context.Context, model.Message) error {
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("enqueue error=%v", err)
	}
	if g.ingress.eventReceived.Load() != 2 || g.ingress.normalized.Load() != 1 ||
		g.ingress.rejected.Load() != 1 || g.ingress.enqueued.Load() != 0 ||
		g.ingress.enqueueError.Load() != 1 {
		t.Fatalf("counters received=%d normalized=%d rejected=%d enqueued=%d enqueue_error=%d",
			g.ingress.eventReceived.Load(), g.ingress.normalized.Load(), g.ingress.rejected.Load(),
			g.ingress.enqueued.Load(), g.ingress.enqueueError.Load())
	}
	if !strings.Contains(logs.String(), "feishu ingress rejected") ||
		!strings.Contains(logs.String(), "feishu ingress message_enqueue_error") ||
		!strings.Contains(logs.String(), msgID) {
		t.Fatalf("logs=%s", logs.String())
	}
}

func TestLarkGatewayWSLifecycleCountersCoverAllTransitions(t *testing.T) {
	var logs bytes.Buffer
	g := &LarkGateway{
		botChannelID: "dev",
		logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	for _, state := range []string{"ready", "reconnecting", "reconnected", "disconnected"} {
		g.observeWSLifecycle(state, nil)
	}
	g.observeWSLifecycle("error", errors.New("connection lost"))
	if g.ingress.wsReady.Load() != 1 || g.ingress.wsReconnecting.Load() != 1 ||
		g.ingress.wsReconnected.Load() != 1 || g.ingress.wsDisconnected.Load() != 1 ||
		g.ingress.wsError.Load() != 1 {
		t.Fatalf("lifecycle counters ready=%d reconnecting=%d reconnected=%d disconnected=%d error=%d",
			g.ingress.wsReady.Load(), g.ingress.wsReconnecting.Load(),
			g.ingress.wsReconnected.Load(), g.ingress.wsDisconnected.Load(),
			g.ingress.wsError.Load())
	}
	for _, state := range []string{"ready", "reconnecting", "reconnected", "disconnected", "error"} {
		if !strings.Contains(logs.String(), `"state":"`+state+`"`) {
			t.Fatalf("missing lifecycle state=%s logs=%s", state, logs.String())
		}
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
