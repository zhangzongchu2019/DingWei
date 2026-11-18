package m8

import (
	"context"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func deliveryACKConnectionProtocol(c *sessionClient) (int, string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.deliveryACKV2 {
		return 2, "driver"
	}
	if c.desktopInboxV1 {
		return 1, "desktop"
	}
	return 1, "legacy"
}

func (h *Hub) handleDeliveryACKV2(ctx context.Context, c *sessionClient, env model.Envelope, d *model.SessionDelivery) bool {
	protocol, mode := deliveryACKConnectionProtocol(c)
	if protocol != 2 || mode != "driver" || metaInt(env.Meta, "ack_protocol") != 2 || metaString(env.Meta, "processing_mode") != d.ProcessingMode || metaInt(env.Meta, "ack_generation") != d.AckGeneration {
		return true
	}
	h.mu.Lock()
	current := h.sessionClients[c.keyID][c.sessionName]
	h.mu.Unlock()
	if c.conn != nil && current != c {
		return true
	}
	phase := metaString(env.Meta, "state")
	if phase == "busy" {
		if d.DeliveredAt == nil && d.InjectedAt == nil {
			_ = h.Repo.UpdateSessionDeliveryBusy(ctx, d.ID, "helper_busy", h.now().UTC().Add(15*time.Second))
			h.scheduleSessionDeliveryDrain(c, 15*time.Second)
		}
		return true
	}
	if phase == "failed" {
		reason := metaString(env.Meta, "reason")
		retryable, hasRetryable := env.Meta["retryable"].(bool)
		switch reason {
		case "session_unavailable":
			if retryable || !hasRetryable {
				return true
			}
		case "queue_full":
			c.stateMu.Lock()
			queueFullCap := c.deliveryACKQueueFullV1
			c.stateMu.Unlock()
			if !queueFullCap || retryable || !hasRetryable {
				return true
			}
		default:
			// Unknown admission must be isolated; queue_full is separately negotiated.
			return true
		}
	}
	occurred, err := time.Parse(time.RFC3339Nano, metaString(env.Meta, "occurred_at"))
	if err != nil {
		return true
	}
	ack := model.SessionDeliveryACK{DeliveryID: d.ID, KeyID: c.keyID, SessionName: c.sessionName,
		Protocol: 2, ProcessingMode: mode, Generation: d.AckGeneration, Phase: phase,
		FailureReason: metaString(env.Meta, "reason"), Retryable: metaBool(env.Meta, "retryable"),
		OccurredAt: occurred, TurnID: metaString(env.Meta, "turn_id"), TurnOutcome: metaString(env.Meta, "turn_outcome")}
	result, _, err := h.Repo.ApplySessionDeliveryACK(ctx, ack, h.now().UTC())
	if err != nil {
		return true
	}
	// The confirmation is emitted only after the phase transaction commits.
	if err := c.write(ctx, model.Envelope{ID: randomHex(16), From: sessionAddress("workpulse", c.keyID), To: sessionAddress(c.sessionName, c.keyID), TS: h.now().Unix(), Meta: map[string]any{
		"type": "delivery_ack_confirm", "delivery_id": d.ID, "ack_protocol": 2,
		"processing_mode": mode, "ack_generation": d.AckGeneration, "state": phase,
	}}); err != nil {
		c.obsConn.DeliveryACKConfirmWriteFailed()
	}
	// Re-emitting the current maximum stage supports replay after Hub restart;
	// a late queued ACK must not publish a regression from injected/processed.
	switch result.State {
	case "delivered":
		h.publishDeliveryUpdateForDelivery(*result, "delivered", "helper_queued", "", "")
	case "injected":
		reason := ""
		if result.LastError == "turn_terminal_unknown" {
			reason = result.LastError
		}
		h.publishDeliveryUpdateForDelivery(*result, "injected", "cli_injected", "", reason)
	case "processed":
		reason := ""
		if env.Meta["late_confirmation"] == true {
			reason = "late_native_confirmation"
		}
		h.publishDeliveryUpdateForDelivery(*result, "processed", "turn_processed", "", reason)
	case "admission_unknown":
		h.rememberAdmissionUnknown(*result)
		h.publishDeliveryUpdateForDelivery(*result, "admission_unknown", "helper_queued", "", "model_admission_unknown")
	case "dead":
		h.publishDeliveryUpdateForDelivery(*result, "dead", "helper_received", "failed", result.LastError)
		if result.LastError == "queue_full" {
			h.drainDurableFailureNotice(ctx, *result)
		}
	}
	if result.SourceMessageID != "" {
		_ = h.Repo.AckMessage(ctx, result.SourceMessageID)
	}
	h.backgroundWork("owned", func() { h.drainSessionDeliveries(context.Background(), c) })
	return true
}
