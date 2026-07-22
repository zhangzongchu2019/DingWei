package fcs

// rpc_test.go — proves the phase-2 hub↔FCS HTTP RPC (走甲) end-to-end over a real
// httptest server: outbound Send, resolve, app-error propagation, and the inbound
// fanout stream driven through the ACTUAL hub adapters (NewHubGateway /
// NewHubReceiver over NewRPCClient) — i.e. the exact composition cmd/workpulse wires
// when FCS_RPC_URL is set. Reuses stubGateway/stubReceiver/recordingSink from
// service_test.go (same package).

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// errGateway makes Send fail so we can assert error propagation across the wire
// (Resolve* are promoted from the embedded *stubGateway).
type errGateway struct{ *stubGateway }

func (errGateway) Send(context.Context, feishu.OutMessage) (string, error) {
	return "", errors.New("feishu send exploded")
}

func newRPCPair(t *testing.T, svc Service) Service {
	t.Helper()
	srv := httptest.NewServer(NewRPCServer(svc, nil))
	t.Cleanup(srv.Close)
	return NewRPCClient(srv.URL, srv.Client())
}

func TestRPCSendRoundtrip(t *testing.T) {
	gw := &stubGateway{}
	client := newRPCPair(t, NewService(gw, &stubReceiver{}))

	res, err := client.Send(context.Background(), OutboundRequest{
		BotChannelID: "bot-a", ToID: "chat-1", Text: "hi over rpc",
		MessageID: "msg_" + strings.Repeat("0", 32), Role: RolePrimary,
	})
	if err != nil {
		t.Fatalf("Send over rpc: %v", err)
	}
	if res.FeishuMsgID != "feishu-msg-1" {
		t.Fatalf("FeishuMsgID = %q, want feishu-msg-1", res.FeishuMsgID)
	}
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if len(gw.sent) != 1 || gw.sent[0].BotChannelID != "bot-a" || gw.sent[0].Text != "hi over rpc" {
		t.Fatalf("gateway did not receive the wire OutMessage: %+v", gw.sent)
	}
}

func TestRPCSendPropagatesError(t *testing.T) {
	client := newRPCPair(t, NewService(errGateway{&stubGateway{}}, &stubReceiver{}))
	_, err := client.Send(context.Background(), OutboundRequest{BotChannelID: "bot-a", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "feishu send exploded") {
		t.Fatalf("app error not propagated across rpc: %v", err)
	}
}

func TestRPCResolveRoundtrip(t *testing.T) {
	client := newRPCPair(t, NewService(&stubGateway{}, &stubReceiver{}))
	ctx := context.Background()
	if name, err := client.ResolveChatName(ctx, "bot-a", "oc_1"); err != nil || name != "chat-name" {
		t.Fatalf("ResolveChatName = %q, %v", name, err)
	}
	if name, err := client.ResolveUserName(ctx, "bot-a", "ou_1"); err != nil || name != "user-name" {
		t.Fatalf("ResolveUserName = %q, %v", name, err)
	}
	if id, err := client.ResolveNameToID(ctx, "bot-a", "Alice", "personal"); err != nil || id != "resolved-id" {
		t.Fatalf("ResolveNameToID = %q, %v", id, err)
	}
}

// TestRPCInboundFanoutThroughHubAdapters drives a Feishu event through the FCS
// Ingest→fanout pipeline and asserts it arrives at the hub via the REAL adapters
// (NewHubReceiver over NewRPCClient → onInbound) — the whole inbound path over HTTP.
func TestRPCInboundFanoutThroughHubAdapters(t *testing.T) {
	gw := &stubGateway{}
	rx := &stubReceiver{inbound: make(chan model.Message, 1)}
	svc := NewService(gw, rx) // concrete *service so we can await subscription
	client := newRPCPair(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = svc.Run(ctx) }() // drives rx → Ingest → fanout

	var mu sync.Mutex
	var got []model.Message
	receiver := NewHubReceiver(client, "hub-A")
	go func() {
		_ = receiver.Start(ctx, func(_ context.Context, m model.Message) error {
			mu.Lock()
			got = append(got, m)
			mu.Unlock()
			return nil
		})
	}()

	// Wait until the FCS server has registered the hub's fanout sink (the RPC server
	// calls svc.Subscribe only after the stream opens) before pushing the event.
	waitSinks(t, svc, 1)

	rx.inbound <- model.Message{
		BotChannelID: "bot-a", FeishuMsgID: "fe-rpc-1", ChatType: model.ChatPersonal,
		ChatEntityID: "bot-a:personal:ou_x", SenderOpenID: "ou_x", Content: `{"t":"hi rpc"}`,
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("hub received %d inbound over rpc, want 1", len(got))
	}
	m := got[0]
	if m.ChatEntityID != "bot-a:personal:ou_x" || m.FeishuMsgID != "fe-rpc-1" || m.Content != `{"t":"hi rpc"}` {
		t.Fatalf("inbound not round-tripped faithfully over rpc: %+v", m)
	}
}

// waitSinks blocks until svc has at least n fanout sinks registered (same-package
// access to the concrete service's subscription map).
func waitSinks(t *testing.T, svc *service, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.RLock()
		c := len(svc.sinks)
		svc.mu.RUnlock()
		if c >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fcs did not register %d fanout sink(s) in time", n)
}
