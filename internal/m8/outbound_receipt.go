package m8

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
)

const inboxReceiptRequestKey = "inbox_receipt_v1"

// Observation requests never require or change a target delivery mode.
func inboxReceiptRequested(env model.Envelope) bool {
	requested, ok := env.Meta[inboxReceiptRequestKey].(bool)
	nonce := metaString(env.Meta, outboundReceiptNonceKey)
	_, err := hex.DecodeString(nonce)
	return ok && requested && len(nonce) == 32 && err == nil
}

const outboundReceiptRequestKey = "outbound_receipt_v1"
const outboundReceiptNonceKey = "outbound_receipt_nonce"

func outboundReceiptRequested(env model.Envelope) bool {
	requested, ok := env.Meta[outboundReceiptRequestKey].(bool)
	return ok && requested
}

// Explicit opt-in only. A nil RouteEnvelope result alone isn't acceptance:
// loop termination/dedup can return nil without queuing. Success is emitted at
// the actual durable enqueue site, never by this ingress wrapper.
func (h *Hub) handleConfirmedOutbound(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if !outboundReceiptRequested(env) {
		return false
	}
	nonce := metaString(env.Meta, outboundReceiptNonceKey)
	_, nonceErr := hex.DecodeString(nonce)
	target, err := parseAddress(env.To)
	if nonceErr != nil || len(nonce) != 32 || err != nil || target.Kind != addressSession || target.SessionName == systemAddressName || isAddressableFeishuID(target.SessionName) || strings.HasPrefix(env.To, "@") || strings.HasPrefix(env.To, "#") || !reliablePrimaryEnvelope(env) || len(env.ID) > 128 {
		h.writeOutboundReceipt(ctx, c, env.ID, nonce, "rejected", "")
		return true
	}
	if err := h.RouteEnvelope(ctx, env); err != nil {
		// No raw error, target metadata, original body, or credentials in receipt.
		h.writeOutboundReceipt(ctx, c, env.ID, nonce, "rejected", "")
	}
	return true
}

func (h *Hub) writeOutboundReceipt(ctx context.Context, c *sessionClient, sourceID, nonce, state, deliveryID string) {
	if c == nil || len(sourceID) > 128 {
		return
	}
	if len(nonce) != 32 {
		nonce = ""
	}
	meta := map[string]any{"type": "outbound_receipt", "version": 1, "source_message_id": sourceID, "nonce": nonce, "state": state, "durable": state == "queued", "system": true, "no_mirror": true}
	if deliveryID != "" {
		meta["delivery_id"] = deliveryID
	}
	// Failed receipt write does not undo a committed queue item. The sender must
	// retain an uncertain outcome and must not blindly resubmit the envelope.
	if err := c.write(ctx, model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, c.keyID), To: sessionAddress(c.sessionName, c.keyID), TS: time.Now().Unix(), Meta: meta}); err != nil {
		c.obsConn.OutboundReceiptWriteFailed()
	}
}

func (h *Hub) writeQueuedReceiptToSource(ctx context.Context, env model.Envelope, nonce, deliveryID string) {
	source, err := parseAddress(env.From)
	if err != nil || source.Kind != addressSession {
		return
	}
	h.mu.Lock()
	c := h.sessionClients[source.KeyID][source.SessionName]
	h.mu.Unlock()
	if route, _ := ctx.Value(subAgentDeliveryKey{}).(*subagent.DeliveryRoute); route != nil && route.From != nil {
		c, _ = ctx.Value(subAgentClientKey{}).(*sessionClient)
	}
	h.writeOutboundReceipt(ctx, c, env.ID, nonce, "queued", deliveryID)
}

// Only new clients explicitly request this safe, body-free capability response.
// Old Hub versions won't answer; a new helper then refuses before sending body.
func (h *Hub) writeOutboundCapabilities(c *sessionClient, env model.Envelope) {
	if !outboundReceiptRequested(env) {
		return
	}
	nonce := metaString(env.Meta, "outbound_receipt_cap_nonce")
	if _, err := hex.DecodeString(nonce); err != nil || len(nonce) != 32 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.write(ctx, model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, c.keyID), To: sessionAddress(c.sessionName, c.keyID), TS: time.Now().Unix(), Meta: map[string]any{"type": "outbound_capabilities", "version": 1, "nonce": nonce, "durable_agent_queue": h.Repo != nil, "system": true, "no_mirror": true}})
}
