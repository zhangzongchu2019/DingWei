package m8

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// ExternalSeats 是A路可选接线；未装配时现有helper路径不变。
type ExternalSeats interface {
	EnqueueFromHelper(context.Context, string, string, string, string) (extseat.SendReceipt, error)
	Online(context.Context) ([]extseat.OnlineAgent, error)
}

func (h *Hub) routeExternalSeat(ctx context.Context, keyID, name string, env model.Envelope) (bool, error) {
	if h.Identity == nil || !strings.Contains(name, "_") {
		return false, nil
	}
	target, ok := h.newPlatformAgentAt(ctx, keyID, name)
	if !ok || target.Origin != identity.OriginExternal {
		return false, nil
	}
	if h.ExternalSeats == nil {
		return true, extshared.ErrNotPermitted
	}
	from, e := parseAddress(env.From)
	if e != nil {
		return true, e
	}
	endpoint, ok, e := h.sourceAgentIdentity(ctx, from, env)
	if e != nil {
		return true, e
	}
	if !ok {
		return true, extshared.ErrNotPermitted
	}
	source, ok := h.newPlatformAgentAt(ctx, endpoint.KeyID, endpoint.SessionName)
	if !ok || source.Origin == identity.OriginExternal {
		return true, extshared.ErrNotPermitted
	}
	// 此入口只允许普通通讯数据；特殊系统/任务/控制信封不开放给外部席位。
	typ := metaString(env.Meta, "type")
	if typ != "" && typ != "content" && typ != "agent_route" {
		return true, extshared.ErrNotPermitted
	}
	r, e := h.ExternalSeats.EnqueueFromHelper(ctx, source.ID, target.ID, env.ID, env.Body)
	if e != nil {
		return true, e
	}
	if outboundReceiptRequested(env) || inboxReceiptRequested(env) {
		h.writeQueuedReceiptToSource(ctx, env, metaString(env.Meta, outboundReceiptNonceKey), r.DeliveryID)
	}
	h.publishInputObservation(keyID, name, env, r.DeliveryID, "hub_received", "primary", true)
	h.publishSourceCopyObservation(env, r.DeliveryID, "hub_received")
	return true, nil
}

// HubHelperPresence 供A的ListOnline取得helper快照，不递归调用外部presence。
type HubHelperPresence struct{ Hub *Hub }

func (p HubHelperPresence) Online(ctx context.Context) ([]extseat.OnlineAgent, error) {
	h := p.Hub
	if h == nil || h.Identity == nil {
		return nil, nil
	}
	hidden := map[string]bool{}
	if h.Repo != nil {
		endpoints, err := h.Repo.ListSessionEndpoints(ctx)
		if err != nil {
			return nil, err
		}
		for _, ep := range endpoints {
			if ep.NoDirectory {
				hidden[ep.KeyID+"#"+ep.SessionName] = true
			}
		}
	}
	h.mu.Lock()
	var snaps []onlineSessionSnapshot
	for k, ss := range h.sessionClients {
		for n, c := range ss {
			snaps = append(snaps, onlineSessionSnapshot{KeyID: k, Session: n, Client: c})
		}
	}
	h.mu.Unlock()
	var out []extseat.OnlineAgent
	for _, snap := range snaps {
		if hidden[snap.KeyID+"#"+snap.Session] {
			continue
		}
		ag, ok := h.newPlatformAgentAt(ctx, snap.KeyID, snap.Session)
		if !ok || ag.State != identity.AgentActive || ag.Origin == identity.OriginExternal {
			continue
		}
		out = append(out, extseat.OnlineAgent{Agent: *ag, FullName: h.Identity.FullName(ctx, ag), LastSeenAt: snap.Client.lastSeen()})
	}
	return out, nil
}

// HubExtseatProjector 只做稳定ID的持久入队及唤醒；保留helper现有ACK/重试语义。
type HubExtseatProjector struct{ Hub *Hub }

func (p HubExtseatProjector) Project(ctx context.Context, d extseat.Delivery, target identity.Agent) error {
	h := p.Hub
	if h == nil || h.Repo == nil || h.Identity == nil {
		return extshared.ErrNotPermitted
	}
	current, e := h.Identity.Store.GetAgent(ctx, target.ID)
	if e != nil || current.State != identity.AgentActive || current.Origin == identity.OriginExternal {
		return extshared.ErrTargetNotInAG
	}
	source, _, e := h.Identity.ResolveFullName(ctx, d.From)
	if e != nil || source.Origin != identity.OriginExternal {
		return extshared.ErrUnauthenticated
	}
	key := AgentKeyID(current.OwnerLC)
	name := current.NameLC()
	env := model.Envelope{ID: d.ID, From: sessionAddress(source.NameLC(), AgentKeyID(source.OwnerLC)), To: sessionAddress(name, key), Body: d.Body, TS: d.CreatedAt.Unix(), Meta: map[string]any{"type": "delivery", "delivery_id": d.ID, "delivery_version": 1, "ack_required": true, "source_message_id": d.ID}}
	raw, e := json.Marshal(env)
	if e != nil {
		return e
	}
	// 首次插入前查同ID内容，防止把冲突误当INSERT OR IGNORE成功。
	old, e := h.Repo.GetSessionDelivery(ctx, d.ID)
	if e != nil {
		return e
	}
	if old != nil {
		if old.KeyID != key || old.SessionName != name || old.Envelope != string(raw) {
			return extshared.ErrIdempotencyKey
		}
	} else if e = h.Repo.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: d.ID, KeyID: key, SessionName: name, Envelope: string(raw), SourceMessageID: d.ID, SourceType: "extseat_inbound", CreatedAt: h.now().UTC(), NextAttemptAt: h.now().UTC()}); e != nil {
		return e
	}
	persisted, e := h.Repo.GetSessionDelivery(ctx, d.ID)
	if e != nil {
		return e
	}
	if persisted == nil {
		return errors.New("extseat projection not persisted")
	}
	if persisted.KeyID != key || persisted.SessionName != name || persisted.Envelope != string(raw) {
		return extshared.ErrIdempotencyKey
	}
	h.mu.Lock()
	c := h.sessionClients[key][name]
	h.mu.Unlock()
	if c != nil {
		h.scheduleSessionDeliveryDrain(c, 0)
	}
	return nil
}
