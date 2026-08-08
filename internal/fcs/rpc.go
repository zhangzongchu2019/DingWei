package fcs

// rpc.go — the phase-2 hub↔FCS wire (走甲: 最小独立进程 + 简单 HTTP RPC, 张宗楚 07-21).
//
// This pins the transport-level contract that the phase-1 seam (contract.go) left
// deliberately open. It is a deliberately SIMPLE JSON-over-HTTP protocol chosen for
// "马上能灰度": one FCS process fronts the single Feishu boundary; N hub versions
// (grayscale) each talk to it over these endpoints.
//
//   POST {RPCPathSend}      body=OutboundRequest        → wireSendResp
//   POST {RPCPathResolve}   body=wireResolveReq         → wireResolveResp
//   GET  {RPCPathInbound}?hub=<id>                      → NDJSON stream of InboundEnvelope
//   POST {RPCPathReceipt}   body=FanoutReceipt          → 202 (best-effort)
//
// Application errors are carried in the response body's Err field with HTTP 200 so
// the client can reconstruct the original error; non-2xx status = transport/framing
// failure. Credentials NEVER cross this wire (design §8) — only non-secret routing
// fields + the A0 protected IDs already on the envelope.
//
// EXPLICITLY OUT OF SCOPE THIS ROUND (张宗楚 后补, not 本轮): provenance two-layer
// trust root, durable fanout ledger + per-hub retry, key/epoch management. Inbound
// fanout here is best-effort at-most-once over the open stream; a hub that drops or
// disconnects loses in-flight events until the durable ledger lands. This is a
// documented reduced-scope decision, not an oversight.

// RPC endpoint paths (mounted by NewRPCServer, called by NewRPCClient).
const (
	RPCPathSend    = "/rpc/send"
	RPCPathResolve = "/rpc/resolve"
	RPCPathInbound = "/rpc/inbound"
	RPCPathReceipt = "/rpc/receipt"
)

// Resolve kinds (wireResolveReq.Kind).
const (
	resolveChat = "chat" // A=chatID          → chat name
	resolveUser = "user" // A=openID          → user name
	resolveName = "name" // A=name, B=chatType → id
)

// wireSendResp is the /rpc/send response. Err carries the app-level Send error.
type wireSendResp struct {
	FeishuMsgID string `json:"feishu_msg_id,omitempty"`
	Err         string `json:"err,omitempty"`
}

// wireResolveReq is the /rpc/resolve request (one endpoint, kind-dispatched).
type wireResolveReq struct {
	Kind         string `json:"kind"`
	BotChannelID string `json:"bot_channel_id"`
	A            string `json:"a"`           // chatID | openID | name
	B            string `json:"b,omitempty"` // chatType (kind=name only)
}

// wireResolveResp is the /rpc/resolve response.
type wireResolveResp struct {
	Value string `json:"value,omitempty"`
	Err   string `json:"err,omitempty"`
}
