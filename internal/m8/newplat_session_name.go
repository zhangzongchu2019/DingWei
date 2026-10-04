package m8

import (
	"context"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// newPlatformVisibleSession resolves a user-visible full_name through the
// identity authority. A dotted input is treated as an attempted full_name and
// never falls through to fuzzy matching when it is invalid or belongs to a
// different owner.
type newPlatformVisibleSession struct {
	keyID       string
	sessionName string
	ownerLC     string
	fullName    bool
	found       bool
}

func (h *Hub) resolveNewPlatformVisibleSession(ctx context.Context, requested string) newPlatformVisibleSession {
	requested = strings.TrimSpace(requested)
	if h.Identity == nil || !strings.Contains(requested, ".") {
		return newPlatformVisibleSession{}
	}
	result := newPlatformVisibleSession{fullName: true}
	ag, user, err := h.Identity.ResolveFullName(ctx, requested)
	if err != nil || ag.State != identity.AgentActive || user.State != identity.UserActive {
		return result
	}
	result.keyID = AgentKeyID(ag.OwnerLC)
	result.sessionName = ag.NameLC()
	result.ownerLC = user.UserIDLC()
	result.found = true
	return result
}
