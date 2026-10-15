package m8

import (
	"context"
	"net/http"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// RequireAgentSpecDirect uses the Agent's existing dedicated credential, never
// a claimed actor, service key, Console cookie or page capability. This gate is
// deliberately separate from task/inference capability routes.
func (h *Hub) RequireAgentSpecDirect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Identity == nil {
			writeAgentWSError(w, http.StatusUnauthorized, "agent_auth_required")
			return
		}
		authorization := strings.Fields(r.Header.Get("Authorization"))
		if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") {
			writeAgentWSError(w, http.StatusUnauthorized, "agent_auth_required")
			return
		}
		agent, _, err := h.Identity.AuthenticateAgent(r.Context(), authorization[1])
		if err != nil || agent == nil || agent.State != identity.AgentActive {
			writeAgentWSError(w, http.StatusForbidden, "agent_inactive_or_invalid")
			return
		}
		// AuthenticateAgent rechecks owner state; retain an explicit active predicate
		// so unknown/future states cannot acquire direct-write permission.
		owner, err := h.Identity.Store.GetUser(r.Context(), agent.OwnerLC)
		if err != nil || owner == nil || owner.State != identity.UserActive {
			writeAgentWSError(w, http.StatusForbidden, "owner_inactive")
			return
		}
		if r.Header.Get("X-DW-Spec-Write") != "direct-v1" {
			writeAgentWSError(w, http.StatusUpgradeRequired, "upgrade_required")
			return
		}
		if err = h.syncNewPlatformUser(r.Context(), agent.OwnerLC, nil); err != nil {
			writeAgentWSError(w, http.StatusServiceUnavailable, "console_unavailable")
			return
		}
		ctx := context.WithValue(r.Context(), consolePrincipalContextKey{}, identity.Lower(agent.OwnerLC))
		ctx = context.WithValue(ctx, consoleSpecActorContextKey{}, consoleSpecActor{ID: agent.ID, Kind: "agent"})
		ctx = context.WithValue(ctx, consoleSpecDirectContextKey{}, true)
		// An arbitrary shared/edit capability can never select a different write path.
		r.Header.Del("X-DW-Spec-Edit-Capability")
		next(w, r.WithContext(ctx))
	}
}
