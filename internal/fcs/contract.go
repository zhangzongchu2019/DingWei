// Package fcs defines the hub↔FCS (Feishu Channel Service) boundary contract:
// the transport-agnostic interfaces + wire types for extracting Feishu from the
// hub monolith into an independent service (design 设计-A3-飞书独立服务-v0.6.md).
//
// Scope of THIS file (B0-independent foundation, manager 07:21 提速指令):
//   - outbound single exit (hub → FCS → Feishu), converging the two current Send
//     call sites (runOutboundLoop + worker.sendReply's queue-bypass direct Send);
//   - inbound fanout broadcast (FCS → subscribed hubs), service-receipt protocol;
//   - resolve (chat/user/name↔id);
//   - bot_channel identity carried on the wire.
//
// The provenance two-layer trust root (design §5, death-star gate) is STUBBED here
// (Source/Attestation below) pending B0's canonical meta.source{} object — per
// design §0/§11 the impl baseline is the preprod tag after B0's source object
// merges. Interfaces 先留位, real provenance wiring lands with B0.
//
// Transport (gRPC vs mTLS HTTP) is deliberately NOT decided here — these are the
// language-level seams; the wire encoding is chosen in a later step with dev55 (B).
package fcs

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// ─────────────────────────────────────────────────────────────────────────────
// Outbound (hub → FCS, single exit). Mirrors internal/feishu.OutMessage plus the
// A0 protected IDs the hub carries through. FCS routes by BotChannelID to Feishu.
// ─────────────────────────────────────────────────────────────────────────────

// OutboundRequest is one hub→FCS send. Converges runOutboundLoop + worker.sendReply.
type OutboundRequest struct {
	// HubID 标识发起这条出站的 Hub。桥接据此查注册时登记的版本并在正文末尾盖章。
	// 由调用方填写「我是谁」,版本仍取自注册表 —— 调用方无法自报一个假版本。
	HubID        string `json:"hub_id,omitempty"`
	BotChannelID string // which Feishu bot channel
	ToID         string // chat_id / open_id (与 ToName 二选一)
	ToName       string // 群名 / 姓名 (全局唯一可免 type)
	ToType       string // personal|group (按名发送时消歧)
	Text         string

	// A0 protected IDs carried through (hub does not mint; delivery_id is B1-owned).
	MessageID  string // A0 message_id (msg_+32hex)
	RequestID  string // A0 request_id (ingress idempotency key)
	DeliveryID string // B1 durable outbox owns; FCS/hub never generate (design §3.2)

	// Delivery role (A2 §5.2, enums delivery.role). A legal frame through the single
	// outbound exit MUST express its role — else #sync mirror frames and control
	// frames cannot pass (claudek3 CR G1-1):
	//   - primary (default): carries DeliveryID, enters B2.
	//   - copy: carries CopyID + CopyKind, ack_policy=none. #sync mirror = role=copy,
	//     copy_kind=feishu_sync (A2 §5.3 / design §7 — mirror goes through THIS exit).
	//   - control: carries ControlKind, does not advance primary ACK.
	Role        string // "" (=primary) | primary | copy | control
	CopyID      string // for role=copy (enums delivery.copy_id_regex)
	CopyKind    string // for role=copy: feishu_sync | view_observation | multi_hub_broadcast | audit_log
	ControlKind string // for role=control
}

// Delivery role constants (A2 §5.2 / v06-a0-enums.json delivery.role).
const (
	RolePrimary = "primary"
	RoleCopy    = "copy"
	RoleControl = "control"
)

// OutboundResult is the FCS→hub send result.
type OutboundResult struct {
	FeishuMsgID string // upstream Feishu message id
}

// ─────────────────────────────────────────────────────────────────────────────
// Inbound (FCS → hub, fanout broadcast). Replaces the in-proc
// onInbound(ctx, model.Message) callback (feishu.go:51) with an FCS→hub push.
// ─────────────────────────────────────────────────────────────────────────────

// InboundEnvelope is one canonical inbound (Feishu event, stamped at FCS boundary).
type InboundEnvelope struct {
	BotChannelID string
	FeishuMsgID  string // upstream idempotency (feishu event id)
	ChatType     string // personal|group
	// ChatEntityID encodes botChannel:chatType:feishuID — the explicit hub↔FCS
	// contract (design §3.4), formerly an implicit helper convention.
	ChatEntityID string
	SenderOpenID string // 群消息=发言人
	Content      string // content_json
	// Correlation data survives FCS even when its service attestation is not
	// implemented. Hub must consume/reject such a reply, never turn it into an
	// ordinary Agent instruction. This field alone is not authenticated evidence.
	FeishuReply *model.FeishuReplyMetadata

	// A0 protected IDs, FCS-minted at the authenticated ingress boundary
	// (A0 §2: mint at the first authenticated trusted platform component).
	MessageID string
	RequestID string

	// Source is the canonical source object carrying the provenance trust root
	// (design §5). STUB pending B0's meta.source{} schema. FCS fills kind=feishu +
	// raw provenance + attested_by=fcs + attestation evidence; the HUB derives
	// trust_decision at its auth boundary (three-condition fail-closed, §5.3) —
	// FCS never writes trust_decision (it is not sender-self-reportable).
	Source SourceStub
}

// SourceStub is a placeholder for B0's canonical meta.source{} object
// (design §0/§5.4). Field names/shape defer to B0's machine-readable schema;
// this stub only pins WHAT FCS must supply so the seam compiles before B0 lands.
type SourceStub struct {
	Kind string // "feishu"
	// Provenance is the raw enum computed by the authenticated FCS ingress adapter
	// and preserved over RPC. It is normalized fail-closed at both ends; it is not
	// an HTTP service attestation and never constitutes Hub trust_decision.
	Provenance string // webhook_verified|feishu_ws_authenticated|untrusted
	AttestedBy string // 层②: "fcs" — MUST be backed by verifiable transport evidence (Attestation)

	Attestation AttestationStub

	// Internal sensitive addressing (design §5.4) — MUST NOT reach view/普通日志.
	InternalBotChannelID string
	InternalChatID       string
	InternalSenderOpenID string
}

// AttestationStub is the FCS→Hub transport attestation evidence (design §5.2).
// The hub verifies it (service auth + tenant/key binding + epoch not revoked)
// as condition (a) of the three-condition trust decision (§5.3).
type AttestationStub struct {
	ServiceID string // FCS service identity
	Tenant    string
	KeyID     string
	KeyEpoch  uint64 // monotonic; revoked epochs fail-closed (design §5.2)
	Signature []byte // mTLS peer identity or HMAC signature
}

// ─────────────────────────────────────────────────────────────────────────────
// Fanout = service receipt (design §6, A2 §4). FCS owns the authoritative ledger
// keyed (RequestID, AuthenticatedHubID) and does per-hub retry. Hubs do NOT dedup
// across requests, do NOT self-retry, do NOT replay on target-online (hub 零暂存).
// ─────────────────────────────────────────────────────────────────────────────

// FanoutEffect is the per-hub effect recorded in the FCS ledger (NOT message_id/
// delivery_id — design §6, A2 §4 R7).
type FanoutEffect string

const (
	EffectDelivered FanoutEffect = "delivered" // hub accepted (forwarded)
	EffectRetryable FanoutEffect = "retryable" // transient; FCS retries this hub
	EffectFailed    FanoutEffect = "failed"    // terminal for this hub
)

// FanoutReceipt is the hub→FCS acknowledgement for one delivered inbound.
type FanoutReceipt struct {
	RequestID          string
	AuthenticatedHubID string
	Effect             FanoutEffect
	Attempt            int
}

// ─────────────────────────────────────────────────────────────────────────────
// Resolve (hub → FCS). FCS caches open_id↔name so hubs don't each hit Feishu API.
// ─────────────────────────────────────────────────────────────────────────────

// Resolver is the hub→FCS name/id resolution (mirrors feishu.Gateway resolves).
type Resolver interface {
	ResolveChatName(ctx context.Context, botChannelID, chatID string) (string, error)
	ResolveUserName(ctx context.Context, botChannelID, openID string) (string, error)
	ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (id string, err error)
}

// ─────────────────────────────────────────────────────────────────────────────
// Service interfaces (transport-agnostic seams).
// ─────────────────────────────────────────────────────────────────────────────

// Service is the FCS server side: the single Feishu boundary. It wraps
// internal/feishu (WS/webhook/Send/resolve) and owns bot credentials + the fanout
// ledger. Hubs call Send/Resolver over RPC; FCS pushes inbound via InboundSink.
type Service interface {
	// Send is the single outbound exit (hub → FCS → Feishu).
	Send(ctx context.Context, req OutboundRequest) (OutboundResult, error)
	Resolver

	// Subscribe registers a hub as a fanout target. FCS then pushes inbound
	// envelopes to sink.DeliverInbound and records per-hub receipts in its ledger.
	// Subscribe 注册一个 Hub 作为入站广播的接收方。
	// hubVersion 在**注册时**给出而非每条消息带:桥接据此为该 Hub 的出站消息盖章,
	// Hub 自身无法遗漏也无法伪造 —— 与「【源→目的】头由 Hub 加而非 Agent 自填」同一原则:
	// 盖章应由**唯一知道真相的那一方**来盖。
	Subscribe(authenticatedHubID, hubVersion string, sink InboundSink) (cancel func(), err error)
}

// IngressDedup assigns a persistent RANDOM request_id for a native Feishu event id
// and provides ingress dedup (A2 §4.1, claudek3 CR G1-3). The request_id is RANDOM
// (does NOT leak the native FeishuMsgID), PERSISTENT (same native id → same
// request_id across retries), and the (botChannelID, native-id)→request_id mapping
// lives in a protected dedup store. Dedup also breaks the mirror-回摄 loop (CR
// G2-2): a native event seen again reuses its request_id and is dropped, so an
// FCS-originated #sync mirror that comes back via WS is not re-injected.
//
// Phase-1 skeleton uses an in-memory impl (memDedup); the durable authoritative
// store is FCS/A3-owned (A2 §4) — a later step.
type IngressDedup interface {
	// RequestIDFor returns the persistent random request_id for (botChannelID,
	// nativeEventID), minting a fresh random one on first sight (reused on retry).
	// firstSeen=false ⇒ this native event was already ingested; the caller drops the
	// duplicate (no re-injection / no mirror loop).
	RequestIDFor(ctx context.Context, botChannelID, nativeEventID string) (requestID string, firstSeen bool, err error)
}

// InboundSink is the HUB side: it receives fanout-broadcast inbound from FCS.
// Today's onInbound(ctx, model.Message) (feishu.go:51, wired main.go:225) becomes
// this over the wire. The hub converts InboundEnvelope→model.Message, derives
// trust_decision (§5.3), and enqueues; then returns a FanoutReceipt.
type InboundSink interface {
	DeliverInbound(ctx context.Context, env InboundEnvelope) (FanoutReceipt, error)
}
