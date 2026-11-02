package m8

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// HandleConsoleAgentNameUsed is R14's only new API. No owner input and no
// directory result: the authenticated new-platform principal is authoritative.
func (h *Hub) HandleConsoleAgentNameUsed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner := platformPrincipal(r)
	if h.Identity == nil || owner == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	u, err := h.Identity.Store.GetUser(r.Context(), owner)
	if err != nil || u.State != identity.UserActive {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	if len(q) != 1 || len(q["name_lc"]) != 1 {
		http.Error(w, "invalid name query", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(q.Get("name_lc"))
	if name == "" || len(name) > 100 || name != identity.Lower(name) {
		http.Error(w, "invalid normalized name", http.StatusBadRequest)
		return
	}
	// FindAgentByName includes active and suspended, unlike the online roster.
	a, err := h.Identity.Store.FindAgentByName(r.Context(), owner, name)
	if err != nil && !errors.Is(err, identity.ErrNotFound) {
		http.Error(w, "name check unavailable", http.StatusServiceUnavailable)
		return
	}
	used := err == nil && a != nil && a.OwnerLC == owner && (a.State == identity.AgentActive || a.State == identity.AgentSuspended)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(used)
}
