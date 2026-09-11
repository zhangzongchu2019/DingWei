package fcs

// rpc_test.go — proves the phase-2 hub↔FCS HTTP RPC (走甲) end-to-end over a real
// httptest server: outbound Send, resolve, app-error propagation, and the inbound
// fanout stream driven through the ACTUAL hub adapters (NewHubGateway /
// NewHubReceiver over NewRPCClient) — i.e. the exact composition cmd/workpulse wires
// when FCS_RPC_URL is set. Reuses stubGateway/stubReceiver/recordingSink from
// service_test.go (same package).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

type subscribeErrService struct{ Service }

func (subscribeErrService) Subscribe(string, string, InboundSink) (func(), error) {
	return nil, errors.New("subscription rejected")
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

func TestRPCSubscribeReportsServerRejectionBeforeStreamEstablished(t *testing.T) {
	base := NewService(&stubGateway{}, &stubReceiver{})
	client := newRPCPair(t, subscribeErrService{Service: base})
	_, err := client.Subscribe("hub-rejected", "vtest", sinkFunc(func(context.Context, InboundEnvelope) (FanoutReceipt, error) {
		return FanoutReceipt{}, nil
	}))
	if err == nil || !strings.Contains(err.Error(), "subscription rejected") {
		t.Fatalf("Subscribe error = %v, want server rejection", err)
	}
}

func TestRPCInboundReconnectsAfterStreamEOF(t *testing.T) {
	var streams atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+RPCPathInbound, func(w http.ResponseWriter, r *http.Request) {
		attempt := streams.Add(1)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		if attempt == 1 {
			return // force a clean EOF; the client must resubscribe
		}
		_ = json.NewEncoder(w).Encode(InboundEnvelope{RequestID: "req-after-reconnect", Content: `{"text":"restored"}`})
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST "+RPCPathReceipt, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got := make(chan InboundEnvelope, 1)
	client := NewRPCClient(srv.URL, srv.Client())
	cancel, err := client.Subscribe("hub-reconnect", "vtest", sinkFunc(func(_ context.Context, env InboundEnvelope) (FanoutReceipt, error) {
		got <- env
		return FanoutReceipt{RequestID: env.RequestID, AuthenticatedHubID: "hub-reconnect", Effect: EffectDelivered}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	select {
	case env := <-got:
		if env.RequestID != "req-after-reconnect" || streams.Load() < 2 {
			t.Fatalf("env=%+v streams=%d", env, streams.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("inbound did not recover after EOF; streams=%d", streams.Load())
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
	receiver := NewHubReceiver(client, "hub-A", "vtest")
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
		IngressProvenance: model.IngressFeishuWSAuthenticated,
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
	if m.ChatEntityID != "bot-a:personal:ou_x" || m.FeishuMsgID != "fe-rpc-1" || m.Content != `{"t":"hi rpc"}` ||
		m.IngressProvenance != model.IngressFeishuWSAuthenticated {
		t.Fatalf("inbound not round-tripped faithfully over rpc: %+v", m)
	}
}

func TestEnvToMessageRejectsUnknownWireProvenance(t *testing.T) {
	for _, provenance := range []string{"", "sender_claimed_verified", "future_unknown"} {
		msg := envToMessage(InboundEnvelope{Source: SourceStub{Provenance: provenance}})
		if msg.IngressProvenance != model.IngressUntrusted {
			t.Fatalf("provenance %q normalized to %q", provenance, msg.IngressProvenance)
		}
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

// 🔴 飞书入站广播的核心不变量(业主原始设计意图):
// 每条入站消息应**复制广播给所有已订阅 Hub**,而不是任选一个。
// 该意图曾被 hubID 冲突悄悄退化成单播 —— 两个 Hub 共用一个 id 时后者顶掉前者,
// 外部表现为「消息随机投到两边之一」(2026-09-10 由 prodops 定位、组长复核)。
//
// 三格互为护栏:只测「两个不同 id 都收到」是不够的 ——
// 一个把消息发给所有 sink、连退订都不处理的实现同样能过。
func TestFanoutBroadcastsToAllDistinctHubs(t *testing.T) {
	svc := NewService(&stubGateway{}, &stubReceiver{inbound: make(chan model.Message, 1)})
	var a, b, c int
	stopA, err := svc.Subscribe("hub-a", "vtest", sinkFunc(func(context.Context, InboundEnvelope) (FanoutReceipt, error) {
		a++
		return FanoutReceipt{}, nil
	}))
	if err != nil {
		t.Fatalf("subscribe hub-a: %v", err)
	}
	stopB, err := svc.Subscribe("hub-b", "vtest", sinkFunc(func(context.Context, InboundEnvelope) (FanoutReceipt, error) {
		b++
		return FanoutReceipt{}, nil
	}))
	if err != nil {
		t.Fatalf("subscribe hub-b: %v", err)
	}

	// ① 不同 id ⇒ 两边都收到。这才是「广播」,退化为单播即本缺陷。
	if err := svc.fanout(context.Background(), InboundEnvelope{}); err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if a != 1 || b != 1 {
		t.Fatalf("广播应送达全部不同 hub:a=%d b=%d", a, b)
	}

	// ② 同 id 重复订阅仍顶替。本次不改该语义(只加告警与订阅数日志),
	//    此格把「顶替」钉成**已知且被测到**的行为,而不是靠外部现象倒推。
	if _, err := svc.Subscribe("hub-a", "vtest", sinkFunc(func(context.Context, InboundEnvelope) (FanoutReceipt, error) {
		c++
		return FanoutReceipt{}, nil
	})); err != nil {
		t.Fatalf("subscribe duplicate hub-a: %v", err)
	}
	if err := svc.fanout(context.Background(), InboundEnvelope{}); err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if a != 1 {
		t.Fatalf("同 id 重复订阅后旧 sink 不应再收到:a=%d", a)
	}
	if c != 1 || b != 2 {
		t.Fatalf("新 sink 与另一 hub 都应收到:c=%d b=%d", c, b)
	}

	// ③ 被顶替者的 stop() 必须是**空操作** —— 不得连带删掉顶替它的那个订阅。
	//    这条是读码时没想到、由本用例跑出来的:Subscribe 返回的 stop 闭包里有
	//    `if current.id == subscriptionID` 守卫,故旧订阅的 stop 不会误删新订阅。
	//    组长最初把本格写成「stopA 后 c 不应再收到」,结果 c=2 —— **是断言错了,不是代码错了**。
	//    该守卫是正确且必要的设计,此处把它钉住。
	stopA()
	if err := svc.fanout(context.Background(), InboundEnvelope{}); err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if c != 2 {
		t.Fatalf("被顶替者的 stop 必须是空操作,新订阅应继续收到:c=%d", c)
	}
	if b != 3 {
		t.Fatalf("未退订的 hub 应继续收到:b=%d", b)
	}

	// ④ 反向对照:**当前在位**订阅退订后,必须真的不再收到 ——
	//    防「发给所有人、退订也不管」式的假修复。
	stopB()
	if err := svc.fanout(context.Background(), InboundEnvelope{}); err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if b != 3 {
		t.Fatalf("在位订阅退订后不得再收到:b=%d", b)
	}
	if c != 3 {
		t.Fatalf("未退订的 hub-a 新订阅应继续收到:c=%d", c)
	}
}

// 🔴 出站落款(业主 2026-09-10 定):HUB→飞书方向,正文末尾附 [hub-id version]。
// 灰度期间多个 Hub 各自独立回复是**预期行为**(那正是要比较的东西),
// 前提是每条回复都能被认出来自谁。
//
// 四格互为护栏:只测「有落款」不够 —— 一个把任意字符串都拼上去的实现也能过。
//
// T-062:期望值由 `0.1.2.9` 改为 `v0.1.2.9`。**这不是为了让测试通过而放宽**——
// 裸点分四段会被飞书识别成 IP 并转成 http://0.1.2.9/ 假链接(业主实测截图为证),
// 加 v 前缀是修复本身,本测试原先的期望值编码的是有缺陷的旧行为。
func TestOutboundStampsHubAndVersion(t *testing.T) {
	gw := &stubGateway{}
	svc := NewService(gw, &stubReceiver{inbound: make(chan model.Message, 1)})
	if _, err := svc.Subscribe("hub-dw2-a", "0.1.2.9", sinkFunc(
		func(context.Context, InboundEnvelope) (FanoutReceipt, error) { return FanoutReceipt{}, nil })); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// ① 已注册且报了版本 ⇒ 落款含实例与版本
	if _, err := svc.Send(context.Background(), OutboundRequest{HubID: "hub-dw2-a", Text: "回复正文"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := gw.sent[len(gw.sent)-1].Text; got != "回复正文 [hub-dw2-a v0.1.2.9]" {
		t.Fatalf("落款不对:%q", got)
	}

	// ② 版本取**注册时**登记的值,调用方自报的假版本不得生效
	//    (本条把「盖章由唯一知道真相的一方来盖」钉住)
	if _, err := svc.Send(context.Background(), OutboundRequest{HubID: "hub-dw2-a", Text: "x"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := gw.sent[len(gw.sent)-1].Text; got != "x [hub-dw2-a v0.1.2.9]" {
		t.Fatalf("版本应取注册值:%q", got)
	}

	// ③ 幂等:已带同一落款的正文不得再盖一次(过渡期/重发会遇到)
	if _, err := svc.Send(context.Background(), OutboundRequest{HubID: "hub-dw2-a", Text: "y [hub-dw2-a v0.1.2.9]"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := gw.sent[len(gw.sent)-1].Text; got != "y [hub-dw2-a v0.1.2.9]" {
		t.Fatalf("重复盖章:%q", got)
	}

	// ④ 反向对照:未知来源不得盖章 —— 宁可没有落款,也不盖一个可能是错的
	if _, err := svc.Send(context.Background(), OutboundRequest{Text: "z"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := gw.sent[len(gw.sent)-1].Text; got != "z" {
		t.Fatalf("未知来源不应盖章:%q", got)
	}
}
