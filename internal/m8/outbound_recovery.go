package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const outboundRecoveryKey = "outbound_recovery_v1"

var errOutboundDenied = errors.New("outbound_permission_denied")

type outboundRecoveryContext struct{}
type outboundIntentRepository interface {
	GetOutboundIntent(context.Context, string, string) (*model.OutboundIntent, error)
	GetOutboundIntentForDelivery(context.Context, string) (*model.OutboundIntent, error)
	CommitOutboundSessionDelivery(context.Context, model.OutboundIntent, model.SessionDelivery) (*model.OutboundIntent, bool, error)
}

// Digest only caller business metadata; transport nonces and authenticated Hub
// enrichment must not turn a reconnect into a new business request.
func outboundRecoveryDigest(env model.Envelope) (string, error) {
	meta := map[string]any{}
	for k, v := range env.Meta {
		switch k {
		case outboundReceiptNonceKey, outboundReceiptRequestKey, inboxReceiptRequestKey, outboundRecoveryKey, "outbound_sender_id", "outbound_target_id":
			continue
		}
		meta[k] = v
	}

	raw, e := json.Marshal([]any{env.To, env.Body, meta})
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func recoveryIntent(ctx context.Context) *model.OutboundIntent {
	v, _ := ctx.Value(outboundRecoveryContext{}).(*model.OutboundIntent)
	return v
}

func (h *Hub) writeRecoveryReceipt(ctx context.Context, c *sessionClient, env model.Envelope, state, reason, id string) {
	meta := map[string]any{"type": "outbound_receipt", "version": 1, "recovery_version": 1, "source_message_id": env.ID, "nonce": metaString(env.Meta, outboundReceiptNonceKey), "state": state, "reason": reason, "durable": state == "queued", "system": true, "no_mirror": true}
	if id != "" {
		meta["delivery_id"] = id
	}
	_ = c.write(ctx, model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, c.keyID), To: sessionAddress(c.sessionName, c.keyID), TS: time.Now().Unix(), Meta: meta})
}
func (h *Hub) handleRecoverableOutbound(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if !metaBool(env.Meta, outboundRecoveryKey) {
		return false
	}
	reply := func(state, reason, id string) { h.writeRecoveryReceipt(ctx, c, env, state, reason, id) }
	repo, ok := h.Repo.(outboundIntentRepository)
	nonce := metaString(env.Meta, outboundReceiptNonceKey)
	_, ne := hex.DecodeString(nonce)
	sid := c.metricsAgentID
	addr, ae := parseAddress(env.To)
	if !ok || sid == "" || ne != nil || len(nonce) != 32 || len(env.ID) != 32 || len(env.Body) > 1<<20 || ae != nil || addr.Kind != addressSession || !reliablePrimaryEnvelope(env) || metaString(env.Meta, "outbound_sender_id") != sid {
		reply("rejected", "outbound_recovery_unsupported", "")
		return true
	}
	if _, e := hex.DecodeString(env.ID); e != nil {
		reply("rejected", "invalid_source_id", "")
		return true
	}
	// No AMR side effect may precede the acceptance transaction. Actual fan-out
	// and syntax-notice paths remain unsupported, rather than becoming literals.
	if metaBool(env.Meta, ipcBodyAMRMetaKey) {
		candidates, looked := parseAMRCandidatesDetailed(env.Body, c.sessionName, h.ipcBodyAMRMaxTargets())
		if len(candidates) > 0 || looked {
			reply("rejected", "outbound_recovery_unsupported", "")
			return true
		}
	}
	digest, e := outboundRecoveryDigest(env)
	if e != nil {
		reply("rejected", "invalid_metadata", "")
		return true
	}
	targetID := metaString(env.Meta, "outbound_target_id")
	if h.Identity == nil {
		reply("rejected", "outbound_recovery_unsupported", "")
		return true
	}
	// Read by pinned ID so storage errors remain unknown; a name resolver that
	// masks DB failures as not-found is unsuitable as the authority here.
	target, e := h.Identity.Store.GetAgent(ctx, targetID)
	if e != nil {
		if errors.Is(e, identity.ErrNotFound) {
			reply("rejected", "target_identity_changed", "")
		} else {
			reply("unknown", "identity_unavailable", "")
		}
		return true
	}
	user, e := h.Identity.Store.GetUser(ctx, target.OwnerLC)
	if e != nil {
		reply("unknown", "identity_unavailable", "")
		return true
	}
	source, e := h.Identity.Store.GetAgent(ctx, sid)
	if e != nil {
		reply("unknown", "identity_unavailable", "")
		return true
	}
	if target.State != identity.AgentActive || user.State != identity.UserActive || target.Origin == identity.OriginExternal || !strings.EqualFold(addr.SessionName, target.NameLC()+"."+user.ShortNameLC()) {
		reply("rejected", "target_identity_changed", "")
		return true
	}
	sourceUser, e := h.Identity.Store.GetUser(ctx, source.OwnerLC)
	if e != nil {
		reply("unknown", "identity_unavailable", "")
		return true
	}
	if sourceUser.State != identity.UserActive || source.State != identity.AgentActive {
		reply("rejected", "permission_denied", "")
		return true
	}
	allowed, e := h.Identity.AgentsCanCommunicate(ctx, source, target)
	if e != nil {
		reply("unknown", "identity_unavailable", "")
		return true
	}
	if !allowed {
		reply("rejected", "permission_denied", "")
		return true
	}
	old, e := repo.GetOutboundIntent(ctx, sid, env.ID)
	if e != nil {
		reply("unknown", "outbound_storage_unavailable", "")
		return true
	}
	if old != nil {
		if old.TargetID != targetID || old.Digest != digest {
			reply("rejected", "idempotency_conflict", "")
		} else {
			reply("queued", "", old.DeliveryID)
		}
		return true
	}
	intent := &model.OutboundIntent{SenderID: sid, SourceMessageID: env.ID, TargetID: targetID, Digest: digest}
	env.To = sessionAddress(target.NameLC(), AgentKeyID(target.OwnerLC))
	ctx = context.WithValue(ctx, outboundRecoveryContext{}, intent)
	if e = h.RouteEnvelope(ctx, env); e != nil {
		if errors.Is(e, store.ErrOutboundConflict) {
			reply("rejected", "idempotency_conflict", "")
		} else {
			reply("unknown", "outbound_route_unavailable", "")
		}
	}
	return true
}

// The durable ledger, never client-supplied metadata, supplies the physical
// target binding. Temporary identity/storage errors retain the same queue row.
func (h *Hub) checkOutboundDelivery(ctx context.Context, c *sessionClient, d *model.SessionDelivery) error {
	repo, ok := h.Repo.(outboundIntentRepository)
	if !ok {
		return nil
	}
	intent, e := repo.GetOutboundIntentForDelivery(ctx, d.ID)
	if e != nil || intent == nil {
		return e
	}
	if h.Identity == nil || c.metricsAgentID != intent.TargetID {
		return errOutboundDenied
	}
	a, e := h.Identity.Store.GetAgent(ctx, intent.SenderID)
	if e != nil {
		if errors.Is(e, identity.ErrNotFound) {
			return errOutboundDenied
		}
		return e
	}
	b, e := h.Identity.Store.GetAgent(ctx, intent.TargetID)
	if e != nil {
		if errors.Is(e, identity.ErrNotFound) {
			return errOutboundDenied
		}
		return e
	}
	if a.State != identity.AgentActive || b.State != identity.AgentActive || b.Origin == identity.OriginExternal || sessionAddress(b.NameLC(), AgentKeyID(b.OwnerLC)) != sessionAddress(c.sessionName, c.keyID) {
		return errOutboundDenied
	}
	for _, ag := range []*identity.Agent{a, b} {
		u, e := h.Identity.Store.GetUser(ctx, ag.OwnerLC)
		if e != nil {
			return e
		}
		if u.State != identity.UserActive {
			return errOutboundDenied
		}
	}
	allowed, e := h.Identity.AgentsCanCommunicate(ctx, a, b)
	if e != nil {
		return e
	}
	if !allowed {
		return errOutboundDenied
	}
	return nil
}
