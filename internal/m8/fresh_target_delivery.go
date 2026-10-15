package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	capabilityPendingState      = "capability_pending"
	capabilityPendingProvenance = "capability_pending_ack_v1"
	capabilityPendingTTL        = 30 * time.Minute
	capabilityPendingSweepEvery = time.Minute
)

// freshTargetDeliveryRepository is deliberately a narrow optional extension.
// Older repository implementations continue to reject an unregistered target
// rather than silently accepting a delivery they cannot resolve safely.
type freshTargetDeliveryRepository interface {
	ResolveCapabilityPendingSessionDeliveries(ctx context.Context, keyID, sessionName string, deliveryACK bool, now time.Time) ([]model.SessionDelivery, error)
	ExpireCapabilityPendingSessionDeliveries(ctx context.Context, now time.Time) ([]model.SessionDelivery, error)
}

func (h *Hub) freshTargetDeliveryRepo() freshTargetDeliveryRepository {
	if h == nil || h.Repo == nil {
		return nil
	}
	repo, _ := h.Repo.(freshTargetDeliveryRepository)
	return repo
}

func (h *Hub) beginCapabilityPendingAdmission(keyID, sessionName string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.capabilityPendingAdmissions[terminalKey(keyID, sessionName)] = struct{}{}
}

func (h *Hub) endCapabilityPendingAdmission(keyID, sessionName string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.capabilityPendingAdmissions, terminalKey(keyID, sessionName))
}

// startCapabilityPendingCleanup covers both Hub startup and the no-reconnect
// case. Existing normal delivery expiry is per-drain; a capability-pending row
// has no ACK-capable client yet, so it needs this small independent reaper.
func (h *Hub) startCapabilityPendingCleanup() {
	if h.freshTargetDeliveryRepo() == nil {
		return
	}
	h.capabilityPendingCleanupOnce.Do(func() {
		stop := make(chan struct{})
		h.capabilityPendingCleanupStop = stop
		go func() {
			h.expireCapabilityPendingDeliveries(context.Background(), h.now().UTC())
			ticker := time.NewTicker(capabilityPendingSweepEvery)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					h.expireCapabilityPendingDeliveries(context.Background(), h.now().UTC())
				case <-stop:
					return
				}
			}
		}()
	})
}

func (h *Hub) expireCapabilityPendingDeliveries(ctx context.Context, now time.Time) {
	repo := h.freshTargetDeliveryRepo()
	if repo == nil {
		return
	}
	expired, err := repo.ExpireCapabilityPendingSessionDeliveries(ctx, now)
	if err != nil {
		log.Printf("capability pending expiry sweep failed: %v", err)
		return
	}
	for _, d := range expired {
		h.finishCapabilityPendingDelivery(ctx, d, pendingCapabilityFailureReason(d))
	}
}

func pendingCapabilityFailureReason(d model.SessionDelivery) string {
	if d.LastError != "" {
		return d.LastError
	}
	return "delivery_capability_unconfirmed_expired"
}

func (h *Hub) finishCapabilityPendingDelivery(ctx context.Context, d model.SessionDelivery, reason string) {
	h.publishDeliveryUpdateForDelivery(d, "dead", "capability_pending", "failed", reason)
	var original model.Envelope
	if err := json.Unmarshal([]byte(d.Envelope), &original); err != nil || original.From == "" {
		return
	}
	notice := model.Envelope{
		ID:   "delivery_failed_" + randomHex(8),
		From: sessionAddress(d.SessionName, d.KeyID),
		To:   original.From,
		Body: fmt.Sprintf("消息投递失败，目标会话未确认可靠投递能力（原因码 %s）", reason),
		Meta: map[string]any{"type": "delivery_status", "no_mirror": true, "system": true, "delivery_id": d.ID, "reason_code": reason},
	}
	if err := h.RouteEnvelope(ctx, notice); err != nil {
		log.Printf("capability pending dead notice route failed delivery_id=%s err=%v", d.ID, err)
	}
}
