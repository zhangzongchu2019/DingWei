package fcs

// service.go — FCS server-side skeleton (design §2/§3). Wraps internal/feishu
// (Gateway + Receiver + SeenPersonCollector) behind the transport-agnostic
// Service interface. This is the B0-independent extraction skeleton: it converges
// outbound to a single exit, adapts inbound model.Message → InboundEnvelope, and
// fans out to subscribed hubs with per-hub receipts.
//
// NOT yet wired: real RPC transport (gRPC/HTTP), the provenance trust root (§5 —
// SourceStub filled minimally here, real B0 source object + hub trust_decision
// land with B0), and the authoritative fanout ledger persistence (§6 — here the
// ledger is in-memory receipt collection; durable ledger is a later step).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Service wraps the in-proc Feishu gateway/receiver as the FCS boundary.
type service struct {
	gw    feishu.Gateway
	rx    feishu.Receiver
	dedup IngressDedup // ingress request_id assignment + dedup (G1-3)

	mu               sync.RWMutex
	sinks            map[string]sinkSubscription // authenticatedHubID → current subscription
	nextSubscription uint64
}

type sinkSubscription struct {
	id      uint64
	sink    InboundSink
	version string // 注册时登记的 Hub 版本,用于出站落款(灰度分辨)
}

// NewService builds the FCS server side over an existing feishu Gateway+Receiver.
// (Extraction phase 1: same internal/feishu, just behind the FCS seam. Phase 2
// moves this into its own process with RPC transport.)
func NewService(gw feishu.Gateway, rx feishu.Receiver) *service {
	return &service{gw: gw, rx: rx, dedup: newMemDedup(), sinks: map[string]sinkSubscription{}}
}

// Send is the single outbound exit (converges runOutboundLoop + worker.sendReply).
// stampVersion 给版本号补 `v` 前缀后再入落款。
// T-062:裸点分四段(如 0.1.2.9)会被飞书富文本识别成 IP 地址并自动转成
// 可点链接 http://0.1.2.9/,落款渲染成 `[hub-dw2-a [0.1.2.9](http://0.1.2.9/)]` ——
// 既难读,也给出一个指向别处的假链接。加 `v` 后不再被判定为 IP。
// 只改渲染,不改 version.Version 本身(后者与仓内 VERSION 文件有一致性测试)。
func stampVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// hubStamp 返回该 Hub 的出站落款,形如 " [hub-dw2-a v0.1.2.9]"(版本经 stampVersion 补 v)。
// 版本取自**注册时**登记的值,不取调用方本条消息自报的值。
func (s *service) hubStamp(hubID string) string {
	hubID = strings.TrimSpace(hubID)
	if hubID == "" {
		return "" // 不知来源就不盖章;宁可没有落款,也不盖一个可能是错的
	}
	s.mu.RLock()
	sub, ok := s.sinks[hubID]
	s.mu.RUnlock()
	if ok && sub.version != "" {
		return fmt.Sprintf(" [%s %s]", hubID, stampVersion(sub.version))
	}
	return fmt.Sprintf(" [%s]", hubID) // 已注册但没报版本:仍标出实例,便于灰度分辨
}

func (s *service) Send(ctx context.Context, req OutboundRequest) (OutboundResult, error) {
	// 业主 2026-09-10:HUB→飞书机器人→飞书账号方向,正文末尾附 [hub-id version]。
	// 用途是灰度期间肉眼分辨「这条是哪个 Hub 回的」——多 Hub 同时在线时,
	// 每个 Hub 各自独立回复是**预期行为**(那正是灰度要比较的东西),
	// 前提是每条回复都能被认出来自谁。
	text := req.Text
	if stamp := s.hubStamp(req.HubID); stamp != "" && !strings.HasSuffix(strings.TrimRight(text, " \t\r\n"), stamp) {
		text = strings.TrimRight(text, " \t\r\n") + stamp
	}
	msgID, err := s.gw.Send(ctx, feishu.OutMessage{
		BotChannelID: req.BotChannelID,
		ToID:         req.ToID,
		ToName:       req.ToName,
		ToType:       req.ToType,
		Text:         text,
	})
	if err != nil {
		return OutboundResult{}, err
	}
	return OutboundResult{FeishuMsgID: msgID}, nil
}

func (s *service) ResolveChatName(ctx context.Context, botChannelID, chatID string) (string, error) {
	return s.gw.ResolveChatName(ctx, botChannelID, chatID)
}
func (s *service) ResolveUserName(ctx context.Context, botChannelID, openID string) (string, error) {
	return s.gw.ResolveUserName(ctx, botChannelID, openID)
}
func (s *service) ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (string, error) {
	return s.gw.ResolveNameToID(ctx, botChannelID, name, chatType)
}

// Subscribe registers a hub as a fanout target (design §3.1/§6).
func (s *service) Subscribe(authenticatedHubID, hubVersion string, sink InboundSink) (func(), error) {
	if authenticatedHubID == "" || sink == nil {
		return nil, fmt.Errorf("fcs: subscribe requires non-empty hub id and sink")
	}
	s.mu.Lock()
	s.nextSubscription++
	subscriptionID := s.nextSubscription
	// 🔴 同一 hubID 重复订阅会**顶掉**前一个(map 赋值语义),而广播只发给在位的那些。
	// 这是「飞书消息随机丢一半」的直接成因:两个 Hub 共用一个 id 时,
	// 订阅表里永远只有最后连上的那个,而外部看起来像随机派送。
	// 顶替发生在启动瞬间,事后只能靠现象倒推 —— 故此处显式告警,把它变成可查的事实。
	if previous, exists := s.sinks[authenticatedHubID]; exists {
		log.Printf("fcs subscribe warning=duplicate_hub_id hub_id=%q previous_subscription=%d new_subscription=%d "+
			"effect=previous_subscription_replaced hint=每个 Hub 实例需配置全局唯一的 WP_HUB_ID",
			authenticatedHubID, previous.id, subscriptionID)
	}
	s.sinks[authenticatedHubID] = sinkSubscription{id: subscriptionID, sink: sink, version: strings.TrimSpace(hubVersion)}
	total := len(s.sinks)
	s.mu.Unlock()
	// 广播面大小是「配置对不对」的直接可观测量:预期几个 Hub 就该是几。
	// 只打 duplicate 告警不够 —— 那要有人恰好在看日志;
	// 把订阅数在每次订阅时报出来,任何一次巡检都能发现「怎么只有 1 个」。
	log.Printf("fcs subscribe hub_id=%q hub_version=%q subscription=%d active_sinks=%d", authenticatedHubID, hubVersion, subscriptionID, total)
	return func() {
		s.mu.Lock()
		if current, ok := s.sinks[authenticatedHubID]; ok && current.id == subscriptionID {
			delete(s.sinks, authenticatedHubID)
		}
		s.mu.Unlock()
	}, nil
}

// Run starts the underlying receiver and fans each inbound Feishu event out to all
// subscribed hubs (design §3.1). Blocks until ctx is cancelled or the receiver ends.
// If there is no receiver (e.g., FEISHU_TRANSPORT=fake → BuildGateway returns a nil
// receiver), Run is a no-op that blocks until ctx is cancelled — behavior-equivalent
// to today's main.go which starts no receiver goroutine in the fake case.
func (s *service) Run(ctx context.Context) error {
	if s.rx == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.rx.Start(ctx, s.Ingest)
}

// Ingest runs one inbound model.Message through the FCS ingress pipeline: ingress
// dedup + persistent random request_id (G1-3) → canonical InboundEnvelope → fanout
// to subscribed hubs. Both ingress transports use this single pipeline — the WS
// receiver (Run) and the webhook ingress (phase-2 cmd/fcs NewWebhookIngress) — so
// dedup + fanout behavior is identical regardless of how the Feishu event arrived.
//
// A native event seen again (retry / #sync mirror 回摄) reuses its request_id and is
// dropped so it is not re-injected (breaks the mirror loop, G2-2).
func (s *service) Ingest(ctx context.Context, m model.Message) error {
	requestID, firstSeen, err := s.dedup.RequestIDFor(ctx, m.BotChannelID, m.FeishuMsgID)
	if err != nil {
		return err
	}
	if !firstSeen {
		return nil // duplicate native event — drop, no re-injection
	}
	env, err := s.inboundEnvelope(m, requestID)
	if err != nil {
		return err
	}
	return s.fanout(ctx, env)
}

// fanout pushes one inbound envelope to every subscribed hub and collects receipts.
// Per-hub delivery failures are recorded (retryable) but do NOT suppress other hubs
// (design §6, A2 §4: R+A failure must not suppress R+B). Durable ledger + retry is
// a later step; here we surface the first hard error while still attempting all.
func (s *service) fanout(ctx context.Context, env InboundEnvelope) error {
	s.mu.RLock()
	targets := make(map[string]InboundSink, len(s.sinks))
	for id, subscription := range s.sinks {
		targets[id] = subscription.sink
	}
	s.mu.RUnlock()

	var firstErr error
	for id, sink := range targets {
		if _, err := sink.DeliverInbound(ctx, env); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("fcs: fanout to hub %s: %w", id, err)
			}
			// continue to other hubs (do not let one hub suppress the rest)
		}
	}
	return firstErr
}

// inboundEnvelope adapts a model.Message (from internal/feishu) into the canonical
// InboundEnvelope, minting the A0 message_id/request_id at this authenticated
// ingress boundary (A0 §2: mint at the first authenticated trusted component).
//
// Source is filled MINIMALLY here (kind=feishu, provenance placeholder). The real
// two-layer trust root (§5) + B0 canonical source object land with B0; the hub
// derives trust_decision (§5.3) — FCS never writes it.
func (s *service) inboundEnvelope(m model.Message, requestID string) (InboundEnvelope, error) {
	messageID, err := mintMessageID()
	if err != nil {
		return InboundEnvelope{}, err
	}
	return InboundEnvelope{
		BotChannelID: m.BotChannelID,
		FeishuMsgID:  m.FeishuMsgID,
		ChatType:     string(m.ChatType),
		ChatEntityID: m.ChatEntityID,
		SenderOpenID: m.SenderOpenID,
		Content:      m.Content,
		FeishuReply:  m.FeishuReply,
		MessageID:    messageID,
		RequestID:    requestID, // persistent RANDOM request_id (G1-3) — NOT the native FeishuMsgID
		Source: SourceStub{
			Kind:       "feishu",
			Provenance: string(model.NormalizeIngressProvenance(m.IngressProvenance)),
			AttestedBy: "fcs", // §5.1 层②; MUST be backed by Attestation evidence (§5.2) — stubbed pending B0
			// internal sensitive addressing (design §5.4 — never to view/logs)
			InternalBotChannelID: m.BotChannelID,
			InternalSenderOpenID: m.SenderOpenID,
		},
	}, nil
}

// mintMessageID mints an A0 message_id = msg_ + 32 lowercase hex (128-bit CSPRNG,
// crypto/rand — A0 frozen: "必须 crypto/rand、禁 math/rand"; ordering-opaque).
// Returns an error rather than panicking (claudek3 CR low item).
func mintMessageID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "msg_" + hex.EncodeToString(b[:]), nil
}

// Compile-time assertions that the skeleton satisfies the seams.
var (
	_ Service  = (*service)(nil)
	_ Resolver = (*service)(nil)
)
