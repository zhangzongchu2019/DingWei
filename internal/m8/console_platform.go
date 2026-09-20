package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
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
	CreateConsolePlatformItem(context.Context, model.ConsolePlatformItem, time.Time) error
	ListConsolePlatformItems(context.Context, bool) ([]model.ConsolePlatformItem, error)
	UpdateConsolePlatformItem(context.Context, string, string, string, time.Time) (bool, error)
	ApproveConsolePlatformItem(context.Context, string, time.Time) (bool, error)
	DeleteConsolePlatformItem(context.Context, string, time.Time) (bool, error)
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

var consolePlatformItemID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

type consolePlatformItemRequest struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Href  string `json:"href"`
}

func validConsolePlatformItemRequest(req consolePlatformItemRequest) bool {
	req.ID = strings.TrimSpace(req.ID)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Title = strings.TrimSpace(req.Title)
	req.Href = strings.TrimSpace(req.Href)
	if !consolePlatformItemID.MatchString(req.ID) || req.ID == "agents" || req.Kind == "" || req.Title == "" || len(req.Title) > 120 {
		return false
	}
	return (strings.HasPrefix(req.Href, "/") && !strings.HasPrefix(req.Href, "//")) || strings.HasPrefix(req.Href, "#")
}

func platformRole(r *http.Request) string {
	role, _ := r.Context().Value(consolePlatformRoleContextKey{}).(string)
	return role
}

func platformPrincipal(r *http.Request) string {
	principal, _ := r.Context().Value(consolePrincipalContextKey{}).(string)
	return principal
}

func platformActions(role string) []string {
	if role == consolePlatformRoleManagement {
		return []string{consolePlatformActionRead, consolePlatformActionSubmit, consolePlatformActionAdd,
			consolePlatformActionEdit, consolePlatformActionDelete, consolePlatformActionApprove}
	}
	return []string{consolePlatformActionRead, consolePlatformActionSubmit}
}

func (h *Hub) HandleConsolePlatform(w http.ResponseWriter, r *http.Request) {
	repo := h.Repo.(consolePlatformRepository)
	role := platformRole(r)
	items, err := repo.ListConsolePlatformItems(r.Context(), role == consolePlatformRoleManagement)
	if err != nil {
		http.Error(w, "console platform unavailable", http.StatusServiceUnavailable)
		return
	}
	// The existing Agent workspace is the first real mounted feature. Future
	// I/K/L items use the same response shape and persistence API.
	builtin := model.ConsolePlatformItem{ID: "agents", Kind: "agents", Title: "Agents", Href: "#agents", Status: "approved", Active: true}
	items = append([]model.ConsolePlatformItem{builtin}, items...)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"role": role, "actions": platformActions(role), "items": items})
}

func decodeConsolePlatformItem(w http.ResponseWriter, r *http.Request) (consolePlatformItemRequest, bool) {
	var req consolePlatformItemRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || !validConsolePlatformItemRequest(req) {
		http.Error(w, "invalid platform item", http.StatusBadRequest)
		return consolePlatformItemRequest{}, false
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Title = strings.TrimSpace(req.Title)
	req.Href = strings.TrimSpace(req.Href)
	return req, true
}

func (h *Hub) createConsolePlatformItem(w http.ResponseWriter, r *http.Request, status string) {
	req, ok := decodeConsolePlatformItem(w, r)
	if !ok {
		return
	}
	item := model.ConsolePlatformItem{ID: req.ID, Kind: req.Kind, Title: req.Title, Href: req.Href,
		Status: status, SubmittedBy: platformPrincipal(r), Active: true}
	if err := h.Repo.(consolePlatformRepository).CreateConsolePlatformItem(r.Context(), item, h.now()); err != nil {
		http.Error(w, "platform item unavailable", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": item.ID, "status": item.Status})
}

func (h *Hub) HandleConsolePlatformSubmit(w http.ResponseWriter, r *http.Request) {
	h.createConsolePlatformItem(w, r, "pending")
}

func (h *Hub) HandleConsolePlatformAdd(w http.ResponseWriter, r *http.Request) {
	h.createConsolePlatformItem(w, r, "approved")
}

func (h *Hub) HandleConsolePlatformEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Href  string `json:"href"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || strings.TrimSpace(req.Title) == "" || len(strings.TrimSpace(req.Title)) > 120 ||
		!validConsolePlatformItemRequest(consolePlatformItemRequest{ID: r.PathValue("itemID"), Kind: "mounted", Title: req.Title, Href: req.Href}) {
		http.Error(w, "invalid platform item", http.StatusBadRequest)
		return
	}
	ok, err := h.Repo.(consolePlatformRepository).UpdateConsolePlatformItem(r.Context(), r.PathValue("itemID"), req.Title, req.Href, h.now())
	if err != nil {
		http.Error(w, "platform item unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsolePlatformApprove(w http.ResponseWriter, r *http.Request) {
	ok, err := h.Repo.(consolePlatformRepository).ApproveConsolePlatformItem(r.Context(), r.PathValue("itemID"), h.now())
	if err != nil {
		http.Error(w, "platform item unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsolePlatformDelete(w http.ResponseWriter, r *http.Request) {
	ok, err := h.Repo.(consolePlatformRepository).DeleteConsolePlatformItem(r.Context(), r.PathValue("itemID"), h.now())
	if err != nil {
		http.Error(w, "platform item unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
