package fcs

// service_test.go — validates the hub↔FCS seam end-to-end with stubs (B0-independent):
//   - Send converges to the single exit and delegates to the feishu Gateway;
//   - inbound events fan out to every subscribed hub with minted A0 message_id;
//   - a per-hub failure does not suppress delivery to other hubs (design §6, A2 §4).

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// stubGateway records outbound Send calls.
type stubGateway struct {
	mu   sync.Mutex
	sent []feishu.OutMessage
}

func (g *stubGateway) Send(_ context.Context, out feishu.OutMessage) (string, error) {
	g.mu.Lock()
	g.sent = append(g.sent, out)
	g.mu.Unlock()
	return "feishu-msg-1", nil
}
func (g *stubGateway) ResolveChatName(context.Context, string, string) (string, error) {
	return "chat-name", nil
}
func (g *stubGateway) ResolveUserName(context.Context, string, string) (string, error) {
	return "user-name", nil
}
func (g *stubGateway) ResolveNameToID(context.Context, string, string, string) (string, error) {
	return "resolved-id", nil
}

// stubReceiver lets the test push inbound events through the FCS fanout.
type stubReceiver struct{ inbound chan model.Message }

func (r *stubReceiver) Start(ctx context.Context, onInbound func(context.Context, model.Message) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-r.inbound:
			_ = onInbound(ctx, m)
		}
	}
}

// recordingSink records the envelopes it receives; optionally fails.
type recordingSink struct {
	mu   sync.Mutex
	got  []InboundEnvelope
	fail bool
}

func (s *recordingSink) DeliverInbound(_ context.Context, env InboundEnvelope) (FanoutReceipt, error) {
	s.mu.Lock()
	s.got = append(s.got, env)
	s.mu.Unlock()
	if s.fail {
		return FanoutReceipt{Effect: EffectRetryable}, errors.New("sink boom")
	}
	return FanoutReceipt{RequestID: env.RequestID, Effect: EffectDelivered, Attempt: 1}, nil
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func TestSendDelegatesToGateway(t *testing.T) {
	gw := &stubGateway{}
	svc := NewService(gw, &stubReceiver{})
	res, err := svc.Send(context.Background(), OutboundRequest{
		BotChannelID: "bot-a", ToID: "chat-1", Text: "hi", MessageID: "msg_" + strings.Repeat("0", 32),
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.FeishuMsgID != "feishu-msg-1" {
		t.Fatalf("FeishuMsgID = %q", res.FeishuMsgID)
	}
	if len(gw.sent) != 1 || gw.sent[0].BotChannelID != "bot-a" || gw.sent[0].Text != "hi" {
		t.Fatalf("gateway did not receive converted OutMessage: %+v", gw.sent)
	}
}

func TestInboundFanoutToAllHubs(t *testing.T) {
	rx := &stubReceiver{inbound: make(chan model.Message, 1)}
	svc := NewService(&stubGateway{}, rx)

	hubA := &recordingSink{}
	hubB := &recordingSink{fail: true} // hub B fails; must NOT suppress hub A
	if _, err := svc.Subscribe("hub-A", "vtest", hubA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Subscribe("hub-B", "vtest", hubB); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	rx.inbound <- model.Message{
		BotChannelID: "bot-a", FeishuMsgID: "fe-1", ChatType: model.ChatPersonal,
		ChatEntityID: "bot-a:personal:ou_x", SenderOpenID: "ou_x", Content: `{"t":"hello"}`,
	}

	// wait for fanout to reach both sinks
	deadline := time.Now().Add(2 * time.Second)
	for (hubA.count() == 0 || hubB.count() == 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hubA.count() != 1 {
		t.Fatalf("hub A got %d envelopes, want 1 (per-hub failure must not suppress others)", hubA.count())
	}
	if hubB.count() != 1 {
		t.Fatalf("hub B got %d envelopes, want 1", hubB.count())
	}

	env := hubA.got[0]
	if env.ChatEntityID != "bot-a:personal:ou_x" || env.FeishuMsgID != "fe-1" || env.Content != `{"t":"hello"}` {
		t.Fatalf("envelope not adapted from model.Message: %+v", env)
	}
	if !strings.HasPrefix(env.MessageID, "msg_") || len(env.MessageID) != 36 {
		t.Fatalf("message_id not A0-minted (msg_+32hex): %q", env.MessageID)
	}
	// G1-3: request_id is a persistent RANDOM req_ id, NOT the native FeishuMsgID.
	if !strings.HasPrefix(env.RequestID, "req_") || len(env.RequestID) != 36 {
		t.Fatalf("request_id not random req_+32hex (A2 §4.1): %q", env.RequestID)
	}
	if env.RequestID == env.FeishuMsgID {
		t.Fatalf("request_id leaks native FeishuMsgID (A2 §4.1 violation): %q", env.RequestID)
	}
	if env.Source.Kind != "feishu" || env.Source.Provenance != "untrusted" {
		t.Fatalf("source stub not filled (kind=feishu, provenance=untrusted fail-closed): %+v", env.Source)
	}
}

// TestIngressDedupDropsDuplicate: a native event seen again (retry / #sync mirror
// 回摄) reuses its request_id and is dropped, not re-injected (CR G1-3/G2-2).
func TestIngressDedupDropsDuplicate(t *testing.T) {
	rx := &stubReceiver{inbound: make(chan model.Message, 2)}
	svc := NewService(&stubGateway{}, rx)
	hub := &recordingSink{}
	if _, err := svc.Subscribe("hub-A", "vtest", hub); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = svc.Run(ctx) }()

	m := model.Message{BotChannelID: "bot-a", FeishuMsgID: "dup-1", ChatEntityID: "e", Content: "c"}
	rx.inbound <- m // first sight → delivered
	rx.inbound <- m // same native id → duplicate → dropped (no re-injection)

	deadline := time.Now().Add(2 * time.Second)
	for hub.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // let the duplicate be processed (and dropped)
	if hub.count() != 1 {
		t.Fatalf("duplicate native event delivered %d times, want 1 (dedup drops mirror回摄)", hub.count())
	}
}

func TestSubscribeStaleCancelDoesNotDeleteReplacement(t *testing.T) {
	svc := NewService(&stubGateway{}, &stubReceiver{})
	oldSink := &recordingSink{}
	newSink := &recordingSink{}
	cancelOld, err := svc.Subscribe("hub-A", "vtest", oldSink)
	if err != nil {
		t.Fatal(err)
	}
	cancelNew, err := svc.Subscribe("hub-A", "vtest", newSink)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelNew()
	cancelOld() // old HTTP handler tears down after reconnect established

	svc.mu.RLock()
	current, ok := svc.sinks["hub-A"]
	svc.mu.RUnlock()
	if !ok || current.sink != newSink {
		t.Fatalf("stale cancel removed replacement subscription: %+v", current)
	}
}
