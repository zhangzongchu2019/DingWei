package fileservice

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// SpecPermanentHandler is internal-only. It copies one ready object to the
// content-addressed permanent tier without enabling frozen retrieval tickets.
type SpecPermanentHandler struct {
	Manager       *Manager
	Token         string
	PublicBaseURL string
}

func (h SpecPermanentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || h.Manager == nil || h.Token == "" || !sameSecret(bearer(r), h.Token) {
		hide(w)
		return
	}
	var input struct {
		FileID         string `json:"file_id"`
		ActorPrincipal string `json:"actor_principal"`
		OwnerKey       string `json:"owner_key"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || strings.TrimSpace(input.FileID) == "" || strings.TrimSpace(input.ActorPrincipal) == "" || strings.TrimSpace(input.OwnerKey) == "" {
		hide(w)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		hide(w)
		return
	}
	key, err := h.Manager.PersistForSpec(r.Context(), input.FileID, input.ActorPrincipal, input.OwnerKey)
	if err != nil {
		hide(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": strings.TrimRight(h.PublicBaseURL, "/") + "/f/" + key})
}
