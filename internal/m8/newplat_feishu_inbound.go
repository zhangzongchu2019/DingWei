package m8

import (
	"context"
	"sort"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Resolve Feishu directly through np_identity; do not project into legacy rows.
func (h *Hub) newPlatformFeishuUser(ctx context.Context, msg model.Message) (*identity.User, bool) {
	if h.Identity == nil || strings.TrimSpace(msg.BotChannelID) == "" || strings.TrimSpace(msg.SenderOpenID) == "" {
		return nil, false
	}
	u, err := h.Identity.Store.ResolveIdentity(ctx, identity.Identity{Provider: "feishu", Issuer: msg.BotChannelID, Subject: msg.SenderOpenID})
	return u, err == nil
}

// Recheck np_agent on every dispatch so suspend/revoke is effective before an
// old socket has necessarily disappeared.
func (h *Hub) newPlatformOnlineSessions(ctx context.Context, ownerLC string) []string {
	keyID := AgentKeyID(ownerLC)
	h.mu.Lock()
	var names []string
	for name, c := range h.sessionClients[keyID] {
		if c != nil {
			names = append(names, name)
		}
	}
	h.mu.Unlock()
	active := names[:0]
	for _, name := range names {
		ag, err := h.Identity.Store.FindAgentByName(ctx, ownerLC, identity.Lower(name))
		if err == nil && ag.State == identity.AgentActive {
			active = append(active, name)
		}
	}
	sort.Strings(active)
	return active
}

func (h *Hub) resolveNewPlatformFeishuSession(ctx context.Context, ownerLC, requested string) ownerSessionResolveResult {
	sessions := h.newPlatformOnlineSessions(ctx, ownerLC)
	if strings.TrimSpace(requested) == "" {
		if len(sessions) == 1 {
			return ownerSessionResolveResult{keyID: AgentKeyID(ownerLC), sessionName: sessions[0], status: ownerSessionUniqueHit}
		}
		if len(sessions) > 1 {
			return ownerSessionResolveResult{status: ownerSessionAmbiguous, candidates: sessions}
		}
	}
	var exact, fuzzy []string
	for _, name := range sessions {
		if ownerSessionExactMatch(ownerLC, name, requested) || strings.EqualFold(name, requested) {
			exact = append(exact, name)
		} else if ownerSessionFuzzyMatch(name, requested) {
			fuzzy = append(fuzzy, name)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = fuzzy
	}
	if len(matches) == 0 {
		return ownerSessionResolveResult{status: ownerSessionNoMatch}
	}
	if len(matches) > 1 {
		return ownerSessionResolveResult{status: ownerSessionAmbiguous, candidates: matches}
	}
	return ownerSessionResolveResult{keyID: AgentKeyID(ownerLC), sessionName: matches[0], status: ownerSessionUniqueHit}
}

func (h *Hub) newPlatformUsersShareAgentGroup(ctx context.Context, sourceOwner string, target *identity.Agent) bool {
	targetGroups, err := h.newPlatformGroupKeys(ctx, target)
	if err != nil {
		return false
	}
	sources, err := h.Identity.Store.ListAgents(ctx, sourceOwner)
	if err != nil {
		return false
	}
	for i := range sources {
		if sources[i].State != identity.AgentActive {
			continue
		}
		groups, err := h.newPlatformGroupKeys(ctx, &sources[i])
		if err != nil {
			continue
		}
		for code := range groups {
			if targetGroups[code] {
				return true
			}
		}
	}
	return false
}
