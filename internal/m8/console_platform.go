package m8

import (
	"context"
	"net/http"
)

const (
	consolePlatformRoleManagement = "management"
	consolePlatformRoleOwner      = "owner"

	consolePlatformActionRead    = "read"
	consolePlatformActionSubmit  = "submit"
	consolePlatformActionAdd     = "add"
	consolePlatformActionEdit    = "edit"
	consolePlatformActionDelete  = "delete"
	consolePlatformActionApprove = "approve"
)

type consolePlatformRepository interface {
	ResolveConsolePlatformRole(context.Context, string) (string, error)
}

type consolePlatformRoleContextKey struct{}

func consolePlatformActionAllowed(role, action string) bool {
	switch role {
	case consolePlatformRoleManagement:
		switch action {
		case consolePlatformActionRead, consolePlatformActionSubmit, consolePlatformActionAdd,
			consolePlatformActionEdit, consolePlatformActionDelete, consolePlatformActionApprove:
			return true
		}
	case consolePlatformRoleOwner:
		return action == consolePlatformActionRead || action == consolePlatformActionSubmit
	}
	return false
}

// RequireConsolePlatformAction is deliberately separate from Agent grants.
// It must run after RequireConsolePrincipal, which supplies the authenticated
// principal ID. UI visibility is never an authorization decision.
func (h *Hub) RequireConsolePlatformAction(action string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principalID, ok := r.Context().Value(consolePrincipalContextKey{}).(string)
		repo, repoOK := h.Repo.(consolePlatformRepository)
		if !ok || principalID == "" || !repoOK {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		role, err := repo.ResolveConsolePlatformRole(r.Context(), principalID)
		if err != nil {
			http.Error(w, "console platform unavailable", http.StatusServiceUnavailable)
			return
		}
		if !consolePlatformActionAllowed(role, action) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), consolePlatformRoleContextKey{}, role)
		next(w, r.WithContext(ctx))
	}
}
