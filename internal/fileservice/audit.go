package fileservice

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// AuditHandler exposes only metadata needed to verify FIFO and tombstones. It
// must be mounted on the service's internal listener; the bearer token is an
// additional fail-closed control, not permission to publish it through nginx.
type AuditHandler struct {
	Repo  *Repository
	Token string
}

func (h AuditHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Repo == nil || h.Token == "" || !sameSecret(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), h.Token) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
	}
	state := State(r.URL.Query().Get("state"))
	items, err := h.Repo.ListStable(r.Context(), state, limit)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"files": items})
}

func sameSecret(a, b string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
