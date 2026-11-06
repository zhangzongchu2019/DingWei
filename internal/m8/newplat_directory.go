package m8

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Each recipient gets a complete replacement snapshot, using the same policy as routing.
// Snapshot all owners so shared groups can include another owner's Agent.
func (h *Hub) broadcastNewPlatformDirectories(ctx context.Context) error {
	h.mu.Lock()
	var snapshots []onlineSessionSnapshot
	for key, sessions := range h.sessionClients {
		for name, c := range sessions {
			snapshots = append(snapshots, onlineSessionSnapshot{KeyID: key, Session: name, Client: c})
		}
	}
	h.mu.Unlock()
	endpoints, snapshotErr := h.Repo.ListSessionEndpoints(ctx)
	byEndpoint := map[string]model.SessionEndpoint{}
	for _, ep := range endpoints {
		byEndpoint[ep.KeyID+"#"+ep.SessionName] = ep
	}
	type peer struct {
		item  onlineSessionItem
		agent *identity.Agent
	}
	var peers []peer
	for _, snap := range snapshots {
		ag, ok := h.newPlatformAgentAt(ctx, snap.KeyID, snap.Session)
		if !ok || ag.State != identity.AgentActive {
			continue
		}
		ep := byEndpoint[snap.KeyID+"#"+snap.Session]
		if ep.NoDirectory {
			continue
		}
		if ep.KeyID == "" {
			ep = model.SessionEndpoint{KeyID: snap.KeyID, SessionName: snap.Session, Active: true}
		}
		owner := model.Member{OwnerKey: ag.OwnerLC, DisplayName: ag.OwnerLC, Active: true}
		if u, err := h.Identity.Store.GetUser(ctx, ag.OwnerLC); err == nil && u.State == identity.UserActive {
			owner.DisplayName = firstNonEmpty(u.DisplayName, u.ShortName, u.UserID)
		} else {
			continue
		}
		ep.FullSessionName = h.Identity.FullName(ctx, ag)
		peers = append(peers, peer{onlineSessionItem{Endpoint: ep, Owner: owner, Client: snap.Client}, ag})
	}
	if h.ExternalSeats != nil {
		external, err := h.ExternalSeats.Online(ctx)
		if err != nil {
			return err
		}
		for _, p := range external {
			if !p.External || p.Agent.Origin != identity.OriginExternal || p.Agent.State != identity.AgentActive {
				continue
			}
			ag := p.Agent
			owner := model.Member{OwnerKey: ag.OwnerLC, DisplayName: ag.OwnerLC, Active: true}
			ep := model.SessionEndpoint{KeyID: AgentKeyID(ag.OwnerLC), SessionName: ag.Name, FullSessionName: p.FullName, Active: true, LastSeenAt: p.LastSeenAt, Tool: "MCP/REST"}
			peers = append(peers, peer{item: onlineSessionItem{Endpoint: ep, Owner: owner}, agent: &ag})
		}
	}
	var writes sync.WaitGroup
	for _, recipient := range snapshots {
		source, ok := h.newPlatformAgentAt(ctx, recipient.KeyID, recipient.Session)
		var visible []onlineSessionItem
		owner := model.Member{OwnerKey: strings.TrimPrefix(recipient.KeyID, "u_"), Active: true}
		if snapshotErr == nil && ok {
			owner.OwnerKey = source.OwnerLC
			for _, target := range peers {
				if target.agent.ID == source.ID {
					continue
				}
				allowed, err := h.Identity.AgentsCanCommunicate(ctx, source, target.agent)
				if err != nil {
					visible = nil
					break
				} // Fail closed, including clearing previous client records.
				if allowed {
					visible = append(visible, target.item)
				}
			}
		}
		sort.Slice(visible, func(i, j int) bool {
			a, b := visible[i].Endpoint, visible[j].Endpoint
			if a.SessionName == b.SessionName {
				return a.KeyID < b.KeyID
			}
			return a.SessionName < b.SessionName
		})
		env := model.Envelope{ID: randomHex(16), To: sessionAddress(recipient.Session, recipient.KeyID), From: sessionAddress("workpulse", recipient.KeyID), Body: renderNewPlatformDirectory(owner, visible), TS: time.Now().Unix(), Meta: map[string]any{"type": "online_directory", "snapshot": true, "system": true, "no_mirror": true}}
		writes.Add(1)
		go func(c *sessionClient, e model.Envelope) {
			defer writes.Done()
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_ = c.write(wctx, e)
		}(recipient.Client, env)
	}
	writes.Wait()
	return snapshotErr
}

func renderNewPlatformDirectory(owner model.Member, items []onlineSessionItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【DingWei在线清单】可通讯的编组在线 Agent：%s\n", owner.OwnerKey)
	if len(items) == 0 {
		b.WriteString("暂无可通讯的在线 Agent。\n")
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. #%s · %s/%s · %s · 全名:%s\n", i+1, item.Endpoint.SessionName, unknown(item.Endpoint.Tool), unknown(item.Endpoint.Model), versionLabel(item.Endpoint.Version), item.Endpoint.FullSessionName)
	}
	return b.String()
}
