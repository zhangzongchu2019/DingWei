package fcs

// hubclient.go — hub-side adapter (design §1/§3.1/§8). Implements the existing
// internal/feishu.Gateway + Receiver interfaces by calling the FCS Service. This
// makes the extraction transparent to existing hub code (main.go/worker keep
// programming against feishu.Gateway/Receiver — the wiring just swaps the concrete
// impl from the in-proc SDK to this FCS-backed client).
//
// Phase 1 (extraction): svc is the in-proc *service (same process, behind the seam).
// Phase 2 (independence): svc becomes an RPC stub to a remote FCS process — this
// file is unchanged; only the concrete Service impl changes.
//
// NOTE (design §0/§5, B0 dependency): InboundEnvelope carries A0 message_id/
// request_id + the SourceStub trust-root fields, but the current model.Message has
// no place for them (no Meta). Plumbing env.MessageID/RequestID/Source into the
// hub's Envelope.Meta protected keys — and deriving trust_decision (§5.3) — lands
// with B0's source object + A0/model integration. Until then this adapter converts
// only the existing model.Message fields and drops the A0/source fields (marked
// TODO) so hub behavior stays byte-equivalent to today.

import (
	"context"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// NewHubGateway returns a feishu.Gateway backed by the FCS Service (outbound + resolve).
func NewHubGateway(svc Service) feishu.Gateway { return gatewayClient{svc: svc} }

// NewHubReceiver returns a feishu.Receiver backed by the FCS Service fanout
// (inbound). authenticatedHubID identifies this hub in the FCS fanout ledger.
func NewHubReceiver(svc Service, authenticatedHubID string) feishu.Receiver {
	return receiverClient{svc: svc, hubID: authenticatedHubID}
}

// ── Gateway (hub → FCS, outbound single exit + resolve) ──────────────────────

type gatewayClient struct{ svc Service }

func (g gatewayClient) Send(ctx context.Context, out feishu.OutMessage) (string, error) {
	res, err := g.svc.Send(ctx, OutboundRequest{
		BotChannelID: out.BotChannelID,
		ToID:         out.ToID,
		ToName:       out.ToName,
		ToType:       out.ToType,
		Text:         out.Text,
		// A0 IDs (MessageID/RequestID/DeliveryID) are plumbed by the hub's outbound
		// builder (B1 owns delivery_id) — TODO once the outbound Envelope carries them.
	})
	return res.FeishuMsgID, err
}

func (g gatewayClient) ResolveChatName(ctx context.Context, botChannelID, chatID string) (string, error) {
	return g.svc.ResolveChatName(ctx, botChannelID, chatID)
}
func (g gatewayClient) ResolveUserName(ctx context.Context, botChannelID, openID string) (string, error) {
	return g.svc.ResolveUserName(ctx, botChannelID, openID)
}
func (g gatewayClient) ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (string, error) {
	return g.svc.ResolveNameToID(ctx, botChannelID, name, chatType)
}

// ── Receiver (FCS fanout → hub inbound) ──────────────────────────────────────

type receiverClient struct {
	svc   Service
	hubID string
}

// Start subscribes this hub to the FCS fanout and blocks until ctx is cancelled
// (mirrors feishu.Receiver.Start semantics: "建立长连接并阻塞"). The actual Feishu
// receiver is driven by the FCS Service (service.Run); this hub only receives the
// fanout-broadcast inbound and hands each to onInbound.
func (r receiverClient) Start(ctx context.Context, onInbound func(ctx context.Context, m model.Message) error) error {
	sink := sinkFunc(func(ctx context.Context, env InboundEnvelope) (FanoutReceipt, error) {
		err := onInbound(ctx, envToMessage(env))
		effect := EffectDelivered
		if err != nil {
			effect = EffectRetryable // transient hub-side failure; FCS may retry per ledger
		}
		return FanoutReceipt{
			RequestID:          env.RequestID,
			AuthenticatedHubID: r.hubID,
			Effect:             effect,
			Attempt:            1,
		}, err
	})
	cancel, err := r.svc.Subscribe(r.hubID, sink)
	if err != nil {
		return err
	}
	defer cancel()
	<-ctx.Done()
	return ctx.Err()
}

// envToMessage converts a fanout InboundEnvelope back into the hub's model.Message.
// TODO(B0/A0): carry env.MessageID/RequestID/Source into Envelope.Meta protected
// keys + derive trust_decision (§5.3) once B0's source object lands.
func envToMessage(env InboundEnvelope) model.Message {
	return model.Message{
		ChatEntityID: env.ChatEntityID,
		Direction:    model.DirectionIn,
		BotChannelID: env.BotChannelID,
		FeishuMsgID:  env.FeishuMsgID,
		ChatType:     model.ChatType(env.ChatType),
		SenderOpenID: env.SenderOpenID,
		Content:      env.Content,
	}
}

// sinkFunc adapts a func to the InboundSink interface.
type sinkFunc func(ctx context.Context, env InboundEnvelope) (FanoutReceipt, error)

func (f sinkFunc) DeliverInbound(ctx context.Context, env InboundEnvelope) (FanoutReceipt, error) {
	return f(ctx, env)
}

// Compile-time assertions.
var (
	_ feishu.Gateway  = gatewayClient{}
	_ feishu.Receiver = receiverClient{}
	_ InboundSink     = sinkFunc(nil)
)
