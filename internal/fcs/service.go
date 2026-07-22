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
	"sync"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Service wraps the in-proc Feishu gateway/receiver as the FCS boundary.
type service struct {
	gw    feishu.Gateway
	rx    feishu.Receiver
	dedup IngressDedup // ingress request_id assignment + dedup (G1-3)

	mu    sync.RWMutex
	sinks map[string]InboundSink // authenticatedHubID → sink (fanout targets)
}

// NewService builds the FCS server side over an existing feishu Gateway+Receiver.
// (Extraction phase 1: same internal/feishu, just behind the FCS seam. Phase 2
// moves this into its own process with RPC transport.)
func NewService(gw feishu.Gateway, rx feishu.Receiver) *service {
	return &service{gw: gw, rx: rx, dedup: newMemDedup(), sinks: map[string]InboundSink{}}
}

// Send is the single outbound exit (converges runOutboundLoop + worker.sendReply).
func (s *service) Send(ctx context.Context, req OutboundRequest) (OutboundResult, error) {
	msgID, err := s.gw.Send(ctx, feishu.OutMessage{
		BotChannelID: req.BotChannelID,
		ToID:         req.ToID,
		ToName:       req.ToName,
		ToType:       req.ToType,
		Text:         req.Text,
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
func (s *service) Subscribe(authenticatedHubID string, sink InboundSink) (func(), error) {
	if authenticatedHubID == "" || sink == nil {
		return nil, fmt.Errorf("fcs: subscribe requires non-empty hub id and sink")
	}
	s.mu.Lock()
	s.sinks[authenticatedHubID] = sink
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.sinks, authenticatedHubID)
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
	for id, sink := range s.sinks {
		targets[id] = sink
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
