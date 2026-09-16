package fileservice

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ArchiveHandler is internal-only. The bearer authenticates Hub; Hub has
// already checked the owner role, while Repository independently checks that
// the file belongs to the declared owner scope.
type ArchiveHandler struct {
	Repo  *Repository
	Token string
	Clock Clock
}

func (h ArchiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || h.Repo == nil || h.Clock == nil || h.Token == "" || !sameSecret(bearer(r), h.Token) {
		hide(w)
		return
	}
	var input struct {
		FileID         string `json:"file_id"`
		ActorPrincipal string `json:"actor_principal"`
		OwnerKey       string `json:"owner_key"`
		Reason         string `json:"reason"`
		Archived       *bool  `json:"archived"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || input.Archived == nil || strings.TrimSpace(input.FileID) == "" || strings.TrimSpace(input.ActorPrincipal) == "" || strings.TrimSpace(input.OwnerKey) == "" || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 {
		hide(w)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		hide(w)
		return
	}
	if err := h.Repo.SetArchivedByOwner(r.Context(), input.FileID, *input.Archived, input.ActorPrincipal, input.OwnerKey, input.Reason, h.Clock.Now()); err != nil {
		hide(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
