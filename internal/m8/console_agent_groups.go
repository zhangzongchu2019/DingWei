package m8

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type consoleAgentGroupRepository interface {
	ListOwnedConsoleAgents(context.Context, string) ([]model.ConsoleAgent, error)
	ListAgentGroups(context.Context) ([]model.AgentGroup, error)
	ListAgentGroupMembers(context.Context, string) ([]model.AgentGroupMember, error)
	CreateConsoleAgentGroup(context.Context, model.AgentGroup) error
	DeleteConsoleAgentGroup(context.Context, string, string, bool) error
	SetConsoleAgentGroupAdditionsClosed(context.Context, string, string, bool, bool) error
	AssignOwnedConsoleAgentGroup(context.Context, string, string, string) error
	UnassignConsoleAgentGroup(context.Context, string, string, string, bool) error
}

type consoleAgentGroupView struct {
	model.AgentGroup
	Members   []model.AgentGroupMember `json:"members"`
	CanManage bool                     `json:"can_manage"`
}

func createConsoleAgentGroup(ctx context.Context, repo consoleAgentGroupRepository, principal string, leader model.ConsoleAgent, ownerKey, shortName string, codeCandidate func() string) (model.AgentGroup, error) {
	group := model.AgentGroup{Code: shortName + codeCandidate(), ShortName: shortName, CreatorPrincipal: principal,
		LeaderKeyID: leader.KeyID, LeaderSessionName: leader.SessionName, LeaderOwnerKey: ownerKey}
	return group, repo.CreateConsoleAgentGroup(ctx, group)
}

func (h *Hub) HandleConsoleAgentGroups(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.Repo.(consoleAgentGroupRepository)
	if !ok {
		http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := platformPrincipal(r)
	owned, err := repo.ListOwnedConsoleAgents(r.Context(), principal)
	if err != nil {
		http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		ownedEndpoint := map[string]bool{}
		for _, agent := range owned {
			ownedEndpoint[agent.KeyID+"\x00"+agent.SessionName] = true
		}
		groups, err := repo.ListAgentGroups(r.Context())
		if err != nil {
			http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
			return
		}
		views := make([]consoleAgentGroupView, 0)
		for _, group := range groups {
			members, memberErr := repo.ListAgentGroupMembers(r.Context(), group.Code)
			if memberErr != nil {
				http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
				return
			}
			canManage := group.CreatorPrincipal == principal || (platformRole(r) == consolePlatformRoleManagement && group.CreatorPrincipal == "" && !group.LeaderConsoleMapped)
			visible := canManage
			for _, member := range members {
				visible = visible || ownedEndpoint[member.KeyID+"\x00"+member.SessionName]
			}
			if visible {
				views = append(views, consoleAgentGroupView{AgentGroup: group, Members: members, CanManage: canManage})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"principal": principal, "groups": views, "owned_agents": owned})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		AgentID   string `json:"agent_id"`
		ShortName string `json:"short_name"`
	}
	request.ShortName = strings.TrimSpace(request.ShortName)
	if json.NewDecoder(r.Body).Decode(&request) != nil || strings.TrimSpace(request.AgentID) == "" {
		http.Error(w, "agent_id and short_name required", http.StatusBadRequest)
		return
	}
	request.ShortName = strings.TrimSpace(request.ShortName)
	if !regexp.MustCompile(`^[a-z0-9-]{8}$`).MatchString(request.ShortName) {
		http.Error(w, "short_name must be 8 characters [a-z0-9-]", http.StatusBadRequest)
		return
	}
	var leader *model.ConsoleAgent
	for i := range owned {
		if owned[i].ID == request.AgentID {
			leader = &owned[i]
			break
		}
	}
	if leader == nil {
		http.Error(w, "owned agent not found", http.StatusNotFound)
		return
	}
	endpoints, err := h.Repo.ListSessionEndpoints(r.Context())
	if err != nil {
		http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
		return
	}
	ownerKey := ""
	for _, endpoint := range endpoints {
		if endpoint.KeyID == leader.KeyID && endpoint.SessionName == leader.SessionName {
			ownerKey = endpoint.OwnerKey
			break
		}
	}
	if ownerKey == "" {
		http.Error(w, "owned agent endpoint not found", http.StatusConflict)
		return
	}
	suffix, err := randomGroupSuffix(24)
	if err != nil {
		http.Error(w, "agent group entropy unavailable", http.StatusServiceUnavailable)
		return
	}
	group, err := createConsoleAgentGroup(r.Context(), repo, principal, *leader, ownerKey, request.ShortName, func() string { return suffix })
	if err != nil {
		if errors.Is(err, store.ErrAgentGroupShortNameConflict) {
			http.Error(w, "short_name already exists", http.StatusConflict)
		} else {
			http.Error(w, "agent group create failed", http.StatusConflict)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(group)
}

const groupSuffixAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

func randomGroupSuffix(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = groupSuffixAlphabet[int(raw[i]&63)]
	}
	return string(out), nil
}

func (h *Hub) HandleConsoleAgentGroup(w http.ResponseWriter, r *http.Request) {
	repo := h.Repo.(consoleAgentGroupRepository)
	principal, code := platformPrincipal(r), r.PathValue("code")
	management := platformRole(r) == consolePlatformRoleManagement
	var err error
	switch {
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/members/"):
		err = repo.UnassignConsoleAgentGroup(r.Context(), code, principal, r.PathValue("agentID"), management)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/members"):
		var request struct {
			AgentID string `json:"agent_id"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "agent_id required", http.StatusBadRequest)
			return
		}
		err = repo.AssignOwnedConsoleAgentGroup(r.Context(), code, principal, request.AgentID)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/additions"):
		var request struct {
			Closed *bool `json:"closed"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Closed == nil {
			http.Error(w, "closed required", http.StatusBadRequest)
			return
		}
		err = repo.SetConsoleAgentGroupAdditionsClosed(r.Context(), code, principal, *request.Closed, management)
	case r.Method == http.MethodDelete:
		err = repo.DeleteConsoleAgentGroup(r.Context(), code, principal, management)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "agent group action failed", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
