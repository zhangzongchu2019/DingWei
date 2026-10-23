package m8

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var inboxDeliveryID = regexp.MustCompile(`^dw-dlv_[0-9a-f]{32}$`)
var inboxInstance = regexp.MustCompile(`^[0-9a-f]{32}$`)

type inboxLagSample struct {
	Available      bool              `json:"available"`
	Schema         string            `json:"schema"`
	Instance       string            `json:"instance"`
	Seq            int64             `json:"seq"`
	Count          int64             `json:"disk_not_admitted_count"`
	Unknown        int64             `json:"dispatch_unknown_count"`
	Oldest         *int64            `json:"oldest_wait_ms"`
	ClockUncertain bool              `json:"clock_uncertain"`
	Deliveries     map[string]string `json:"deliveries"`
}
type inboxLagCache struct {
	sample   inboxLagSample
	received time.Time
}

func validInboxSample(s inboxLagSample) bool {
	if s.Schema != "inbox-progress/v1" || !inboxInstance.MatchString(s.Instance) || s.Seq <= 0 || s.Seq > 1<<53 || s.Count < 0 || s.Unknown < 0 || s.Count > 1e9 || s.Unknown > 1e9 || len(s.Deliveries) > 256 {
		return false
	}
	if s.Count == 0 && s.Oldest != nil || s.Count > 0 && s.Oldest == nil {
		return false
	}
	if s.Oldest != nil && (*s.Oldest < 0 || *s.Oldest > 30*86400000) {
		return false
	}
	for id, stage := range s.Deliveries {
		if !inboxDeliveryID.MatchString(id) {
			return false
		}
		switch stage {
		case "disk_received", "adapter_queued", "dispatching", "model_admitted", "terminal", "unknown", "failed", "cancelled":
		default:
			return false
		}
	}
	return true
}

// Reserved telemetry/query controls never enter the conversation or delivery ACK pipeline.
func (h *Hub) handleInboxLagControl(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	kind := metaString(env.Meta, "type")
	if kind != "inbox_progress_v1" && kind != "delivery_status_query_v1" {
		return false
	}
	if c == nil || len(env.Body) > 32768 {
		return true
	}
	now := h.now()
	h.mu.Lock()
	current := h.sessionClients[c.keyID][c.sessionName] == c
	lastAttempt := &c.lastInboxAttempt
	if kind == "delivery_status_query_v1" {
		lastAttempt = &c.lastInboxQuery
	}
	if !current || !lastAttempt.IsZero() && now.Sub(*lastAttempt) < 100*time.Millisecond {
		h.mu.Unlock()
		return true
	}
	*lastAttempt = now
	h.mu.Unlock()
	if _, ok := h.metricsBinding(ctx, c); !ok {
		return true
	}
	if kind == "delivery_status_query_v1" {
		nonce := metaString(env.Meta, "nonce")
		id := metaString(env.Meta, "delivery_id")
		if !inboxInstance.MatchString(nonce) || !inboxDeliveryID.MatchString(id) {
			return true
		}
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		result := h.deliveryStatusForSender(bounded, c, id)
		_ = c.write(bounded, model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, c.keyID), To: sessionAddress(c.sessionName, c.keyID), TS: now.Unix(), Meta: map[string]any{"type": "delivery_status_result_v1", "nonce": nonce, "delivery_id": id, "result": result, "no_mirror": true, "system": true}})
		return true
	}
	var sample inboxLagSample
	decoder := json.NewDecoder(strings.NewReader(env.Body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&sample) != nil || !validInboxSample(sample) {
		return true
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionClients[c.keyID][c.sessionName] != c {
		return true
	}
	if old := c.inboxLag; old != nil && (sample.Instance != old.sample.Instance || sample.Seq <= old.sample.Seq) {
		return true
	}
	c.inboxLag = &inboxLagCache{sample: sample, received: now}
	return true
}

// Public aggregate excludes delivery IDs and message contents. An absent or old
// snapshot is never represented as a fresh zero, and reads cannot renew it.
func (h *Hub) inboxLagForClient(ctx context.Context, c *sessionClient) map[string]any {
	result := map[string]any{"status": "offline", "source": "helper_self_report"}
	if c == nil {
		return result
	}
	if _, ok := h.metricsBinding(ctx, c); !ok {
		result["status"] = "unknown"
		return result
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionClients[c.keyID][c.sessionName] != c {
		return result
	}
	if c.inboxLag == nil {
		result["status"] = "unsupported"
		return result
	}
	age := h.now().Sub(c.inboxLag.received)
	if !c.inboxLag.sample.Available {
		result["status"] = "unknown"
		return result
	}
	result["observed_at_ms"] = c.inboxLag.received.UnixMilli()
	result["connection_generation"] = c.metricsGeneration
	result["connection_seq"] = c.connSeq
	result["sample_instance"] = c.inboxLag.sample.Instance
	result["sample_seq"] = c.inboxLag.sample.Seq
	if age < 0 || age >= 10*time.Second {
		result["status"] = "stale"
		return result
	}
	s := c.inboxLag.sample
	result["status"] = "ok"
	result["disk_not_admitted_count"] = s.Count
	result["dispatch_unknown_count"] = s.Unknown
	result["clock_uncertain"] = s.ClockUncertain
	result["remaining_fresh_ms"] = (10*time.Second - age).Milliseconds()
	result["oldest_wait_ms"] = nil
	if s.Oldest != nil {
		result["oldest_wait_ms"] = *s.Oldest + age.Milliseconds()
	}
	return result
}
func (h *Hub) currentInboxLag(ctx context.Context, fullName, agentID string) map[string]any {
	if h.Identity == nil {
		return map[string]any{"status": "unknown"}
	}
	ag, _, err := h.Identity.ResolveFullName(ctx, fullName)
	if err != nil || ag.ID != agentID {
		return map[string]any{"status": "unknown"}
	}
	h.mu.Lock()
	c := h.sessionClients[AgentKeyID(ag.OwnerLC)][ag.NameLC()]
	h.mu.Unlock()
	result := h.inboxLagForClient(ctx, c)
	result["agent_id"] = agentID
	return result
}

// Durable provenance proves ownership; current membership is rechecked on every
// query (and on the immediate receipt path), including leader-only exclusions.
func (h *Hub) deliveryStatusForSender(ctx context.Context, c *sessionClient, id string) map[string]any {
	denied := map[string]any{"ok": false, "status": "denied"}
	if h.Repo == nil || h.Identity == nil || c == nil || !inboxDeliveryID.MatchString(id) {
		return denied
	}
	if _, ok := h.metricsBinding(ctx, c); !ok {
		return denied
	}
	delivery, err := h.Repo.GetSessionDelivery(ctx, id)
	if err != nil || delivery == nil {
		return denied
	}
	var original model.Envelope
	if json.Unmarshal([]byte(delivery.Envelope), &original) != nil || original.From != sessionAddress(c.sessionName, c.keyID) {
		return denied
	}
	source, ok := h.newPlatformAgentAt(ctx, c.keyID, c.sessionName)
	if !ok {
		return denied
	}
	target, ok := h.newPlatformAgentAt(ctx, delivery.KeyID, delivery.SessionName)
	if !ok {
		return denied
	}
	user, err := h.Identity.Store.GetUser(ctx, target.OwnerLC)
	if err != nil || user.State != identity.UserActive {
		return denied
	}
	allowed, err := h.Identity.AgentsCanCommunicate(ctx, source, target)
	if err != nil || !allowed {
		return denied
	}
	h.mu.Lock()
	targetClient := h.sessionClients[delivery.KeyID][delivery.SessionName]
	h.mu.Unlock()
	aggregate := h.inboxLagForClient(ctx, targetClient)
	phase := "unknown"
	if aggregate["status"] == "ok" {
		h.mu.Lock()
		if targetClient != nil && targetClient.inboxLag != nil && h.sessionClients[delivery.KeyID][delivery.SessionName] == targetClient && targetClient.inboxLag.sample.Seq == aggregate["sample_seq"] && targetClient.inboxLag.sample.Instance == aggregate["sample_instance"] {
			if s := targetClient.inboxLag.sample.Deliveries[id]; s != "" {
				phase = s
			}
		}
		h.mu.Unlock()
	}
	// Legacy processed is intentionally not used as model admission evidence.
	// Sender receipts expose aggregate health, not the target's private epochs.
	delete(aggregate, "connection_generation")
	delete(aggregate, "sample_instance")
	return map[string]any{"ok": true, "status": "ok", "delivery_id": id, "source_message_id": delivery.SourceMessageID, "phase": phase, "inbox_lag": aggregate}
}
