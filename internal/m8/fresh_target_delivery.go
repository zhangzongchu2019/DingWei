package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	capabilityPendingState      = "capability_pending"
	capabilityPendingProvenance = "capability_pending_ack_v1"
	capabilityPendingTTL        = 30 * time.Minute
	capabilityPendingSweepEvery = time.Minute
)

var capabilityPendingStartupSweepTimeout = 5 * time.Second

// freshTargetDeliveryRepository is deliberately a narrow optional extension.
// Older repository implementations continue to reject an unregistered target
// rather than silently accepting a delivery they cannot resolve safely.
type freshTargetDeliveryRepository interface {
	ResolveCapabilityPendingSessionDeliveries(ctx context.Context, keyID, sessionName string, deliveryACK bool, now time.Time) ([]model.SessionDelivery, error)
	ExpireCapabilityPendingSessionDeliveries(ctx context.Context, now time.Time) ([]model.SessionDelivery, error)
}

// Optional repository extension: the global sweep replaces the narrow pending
// sweep when supported, and atomically persists sender notices with expiry.
type offlineFailureNoticeRepository interface {
	SweepSessionDeliveryFailures(context.Context, time.Time) ([]model.SessionDelivery, error)
}

type capabilityPendingAdmission struct {
	mu          sync.Mutex
	resolved    bool
	deliveryACK bool
}

func (h *Hub) freshTargetDeliveryRepo() freshTargetDeliveryRepository {
	if h == nil || h.Repo == nil {
		return nil
	}
	repo, _ := h.Repo.(freshTargetDeliveryRepository)
	return repo
}

func (h *Hub) beginCapabilityPendingAdmission(keyID, sessionName string) *capabilityPendingAdmission {
	h.mu.Lock()
	defer h.mu.Unlock()
	a := &capabilityPendingAdmission{}
	h.capabilityPendingAdmissions[terminalKey(keyID, sessionName)] = a
	return a
}

func (h *Hub) endCapabilityPendingAdmission(keyID, sessionName string, a *capabilityPendingAdmission) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := terminalKey(keyID, sessionName)
	if h.capabilityPendingAdmissions[key] == a {
		delete(h.capabilityPendingAdmissions, key)
	}
}

func (h *Hub) capabilityPendingAdmission(keyID, sessionName string) *capabilityPendingAdmission {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.capabilityPendingAdmissions[terminalKey(keyID, sessionName)]
}

// resolveCapabilityPendingAdmission runs resolve while holding the same
// admission lock as routeToSession's pending INSERT. That ordering is what
// prevents a capability report from completing between the route snapshot and
// a later INSERT, leaving a stranded capability_pending row.
func (h *Hub) resolveCapabilityPendingAdmission(ctx context.Context, keyID, sessionName string, deliveryACK bool, now time.Time) ([]model.SessionDelivery, error) {
	repo := h.freshTargetDeliveryRepo()
	if repo == nil {
		return nil, nil
	}
	a := h.capabilityPendingAdmission(keyID, sessionName)
	if a == nil {
		return repo.ResolveCapabilityPendingSessionDeliveries(ctx, keyID, sessionName, deliveryACK, now)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.resolved {
		return nil, nil
	}
	dead, err := repo.ResolveCapabilityPendingSessionDeliveries(ctx, keyID, sessionName, deliveryACK, now)
	if err != nil {
		return nil, err
	}
	a.resolved = true
	a.deliveryACK = deliveryACK
	return dead, nil
}

// startCapabilityPendingCleanup covers startup and no-reconnect expiry. The
// global extension handles all nonterminal states; legacy repositories retain
// the capability-pending-only fallback.
func (h *Hub) startCapabilityPendingCleanup() {
	if h.freshTargetDeliveryRepo() == nil {
		return
	}
	h.capabilityPendingCleanupOnce.Do(func() {
		// Startup expiry is synchronous so a just-constructed Hub has already
		// swept historical pending rows before it accepts new routes. Bound that
		// first pass: a stalled repository must not prevent Hub startup forever;
		// the periodic loop below will retry it.
		startupCtx, cancel := context.WithTimeout(context.Background(), capabilityPendingStartupSweepTimeout)
		h.expireCapabilityPendingDeliveries(startupCtx, h.now().UTC())
		cancel()
		stop := make(chan struct{})
		h.capabilityPendingCleanupStop = stop
		ticker := time.NewTicker(capabilityPendingSweepEvery)
		h.capabilityPendingCleanupWG.Add(1)
		go func() {
			defer h.capabilityPendingCleanupWG.Done()
			defer ticker.Stop()
			h.runCapabilityPendingCleanup(context.Background(), stop, ticker.C)
		}()
	})
}

// runCapabilityPendingCleanup keeps the startup sweep and periodic sweep in a
// small testable loop. Production supplies the minute ticker; tests can supply
// a deterministic tick without shortening the production interval.
func (h *Hub) runCapabilityPendingCleanup(ctx context.Context, stop <-chan struct{}, ticks <-chan time.Time) {
	for {
		select {
		case <-ticks:
			h.expireCapabilityPendingDeliveries(ctx, h.now().UTC())
		case <-stop:
			if h.capabilityPendingCleanupBeforeExit != nil {
				h.capabilityPendingCleanupBeforeExit()
			}
			return
		}
	}
}

func (h *Hub) expireCapabilityPendingDeliveries(ctx context.Context, now time.Time) {
	repo := h.freshTargetDeliveryRepo()
	if repo == nil {
		return
	}
	var expired []model.SessionDelivery
	var err error
	if all, ok := h.Repo.(offlineFailureNoticeRepository); ok {
		expired, err = all.SweepSessionDeliveryFailures(ctx, now)
	} else {
		expired, err = repo.ExpireCapabilityPendingSessionDeliveries(ctx, now)
	}
	if err != nil {
		log.Printf("delivery expiry sweep failed: %v", err)
		return
	}
	for _, d := range expired {
		if d.Provenance == capabilityPendingProvenance {
			h.finishCapabilityPendingDelivery(ctx, d, pendingCapabilityFailureReason(d))
		} else {
			h.publishDeliveryUpdateForDelivery(d, "dead", "helper_received", "failed", d.LastError)
			h.drainDurableFailureNotice(ctx, d)
		}
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
	if h.drainDurableFailureNotice(ctx, d) {
		return
	}
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

// Persistence is the authority; a live sender merely accelerates the same
// ordinary ACK queue drain. Offline senders pick it up after reconnect.
func (h *Hub) drainDurableFailureNotice(ctx context.Context, d model.SessionDelivery) bool {
	if h.Repo == nil {
		return false
	}
	notice, err := h.Repo.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID(d.ID))
	if err != nil || notice == nil || notice.Provenance != model.FailureNoticeProvenance {
		return false
	}
	h.mu.Lock()
	var c *sessionClient
	if clients := h.sessionClients[notice.KeyID]; clients != nil {
		c = clients[notice.SessionName]
	}
	h.mu.Unlock()
	if c != nil && h.sessionSupportsDeliveryACK(c) {
		go h.drainSessionDeliveries(context.Background(), c)
	}
	return true
}
