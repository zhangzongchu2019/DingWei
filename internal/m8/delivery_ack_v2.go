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
	if phase == "failed" && (metaString(env.Meta, "reason") != "session_unavailable" || metaBool(env.Meta, "retryable")) {
		// Unknown admission must be isolated, never converted to retryable failed.
		return true
	}
	occurred, err := time.Parse(time.RFC3339Nano, metaString(env.Meta, "occurred_at"))
	if err != nil {
		return true
	}
	ack := model.SessionDeliveryACK{DeliveryID: d.ID, KeyID: c.keyID, SessionName: c.sessionName,
		Protocol: 2, ProcessingMode: mode, Generation: d.AckGeneration, Phase: phase,
		OccurredAt: occurred, TurnID: metaString(env.Meta, "turn_id"), TurnOutcome: metaString(env.Meta, "turn_outcome")}
	result, _, err := h.Repo.ApplySessionDeliveryACK(ctx, ack, h.now().UTC())
	if err != nil {
		return true
	}
	// The confirmation is emitted only after the phase transaction commits.
	_ = c.write(ctx, model.Envelope{ID: randomHex(16), From: sessionAddress("workpulse", c.keyID), To: sessionAddress(c.sessionName, c.keyID), TS: h.now().Unix(), Meta: map[string]any{
		"type": "delivery_ack_confirm", "delivery_id": d.ID, "ack_protocol": 2,
		"processing_mode": mode, "ack_generation": d.AckGeneration, "state": phase,
	}})
	// Re-emitting the current maximum stage supports replay after Hub restart;
	// a late queued ACK must not publish a regression from injected/processed.
	switch result.State {
	case "delivered":
		h.publishDeliveryUpdateForDelivery(*result, "delivered", "helper_queued", "", "")
	case "injected":
		h.publishDeliveryUpdateForDelivery(*result, "injected", "cli_injected", "", "")
	case "processed":
		h.publishDeliveryUpdateForDelivery(*result, "processed", "turn_processed", "", "")
	case "admission_unknown":
		h.publishDeliveryUpdateForDelivery(*result, "admission_unknown", "helper_queued", "", "model_admission_unknown")
	case "dead":
		h.publishDeliveryUpdateForDelivery(*result, "dead", "helper_received", "failed", result.LastError)
	}
	if result.SourceMessageID != "" {
		_ = h.Repo.AckMessage(ctx, result.SourceMessageID)
	}
	go h.drainSessionDeliveries(context.Background(), c)
	return true
}
