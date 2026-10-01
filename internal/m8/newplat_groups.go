package m8

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 新平台编组接口（组长裁定 122610Z／132246Z：编组归 identity 的 np_group／np_group_member）。
// 路径、请求体、返回字段与旧平台同形（console 页不改）；成员与组长的 session_name 改放 full_name，另给 full_name 字段
// （编排件第八节：页签与编组成员一律用 full_name）。key_id／owner_key 为该 Agent 属主的虚拟 key（u_用户）。
// 认人在外层 RequireConsolePrincipal（新平台分支：N3 会话、必须改密 403、写操作校验 Origin）。

type npGroupMemberView struct {
	AgentID     string    `json:"agent_id"`
	Code        string    `json:"code"`
	KeyID       string    `json:"key_id"`
	SessionName string    `json:"session_name"`
	FullName    string    `json:"full_name"`
	Name        string    `json:"name"`
	OwnerKey    string    `json:"owner_key"`
	Online      bool      `json:"online"`
	CreatedAt   time.Time `json:"created_at"`
}

type npGroupView struct {
	Code                string              `json:"code"`
	ShortName           string              `json:"short_name"`
	CreatorPrincipal    string              `json:"creator_principal"`
	AdditionsClosed     bool                `json:"additions_closed"`
	LeaderAgentID       string              `json:"leader_agent_id"`
	LeaderKeyID         string              `json:"leader_key_id"`
	LeaderSessionName   string              `json:"leader_session_name"`
	LeaderFullName      string              `json:"leader_full_name"`
	LeaderOwnerKey      string              `json:"leader_owner_key"`
	LeaderConsoleMapped bool                `json:"leader_console_mapped"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
	Members             []npGroupMemberView `json:"members"`
	CanManage           bool                `json:"can_manage"`
	OwnerDisplayName    string              `json:"owner_display_name"`
}

type npOwnedAgentView struct {
	ID          string `json:"id"`
	SessionName string `json:"session_name"`
	FullName    string `json:"full_name"`
	Name        string `json:"name"`
	Online      bool   `json:"online"`
}

func (h *Hub) npGroupView(info identity.GroupInfo) npGroupView {
	v := npGroupView{
		Code: info.Code, ShortName: info.ShortName, CreatorPrincipal: info.CreatorLC, AdditionsClosed: info.AdditionsClosed,
		LeaderAgentID: info.LeaderAgentID, LeaderSessionName: info.LeaderFullName, LeaderFullName: info.LeaderFullName,
		LeaderConsoleMapped: true, CreatedAt: info.CreatedAt, UpdatedAt: info.UpdatedAt, CanManage: info.CanManage,
		Members: make([]npGroupMemberView, 0, len(info.Members)),
	}
	if info.Leader != nil {
		v.LeaderKeyID = AgentKeyID(info.Leader.OwnerLC)
		v.LeaderOwnerKey = v.LeaderKeyID
	}
	if info.Creator != nil {
		v.OwnerDisplayName = strings.TrimSpace(info.Creator.DisplayName)
		if v.OwnerDisplayName == "" {
			v.OwnerDisplayName = info.Creator.UserID
		}
	}
	for _, m := range info.Members {
		key := AgentKeyID(m.Agent.OwnerLC)
		v.Members = append(v.Members, npGroupMemberView{
			AgentID: m.Agent.ID, Code: info.Code, KeyID: key, SessionName: m.FullName, FullName: m.FullName,
			Name: m.Agent.Name, OwnerKey: key, Online: h.AgentOnline(m.Agent.ID), CreatedAt: m.JoinedAt,
		})
	}
	return v
}

// handleNewPlatformAgentGroups GET 列表／POST 建组（旧平台 HandleConsoleAgentGroups 的新平台分支）。
func (h *Hub) handleNewPlatformAgentGroups(w http.ResponseWriter, r *http.Request) {
	actor := platformPrincipal(r)
	switch r.Method {
	case http.MethodGet:
		groups, err := h.Identity.ListVisibleGroups(r.Context(), actor)
		if err != nil {
			http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
			return
		}
		owned, err := h.Identity.Store.ListAgents(r.Context(), actor)
		if err != nil {
			http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
			return
		}
		views := make([]npGroupView, 0, len(groups))
		for _, g := range groups {
			views = append(views, h.npGroupView(g))
		}
		ownedViews := make([]npOwnedAgentView, 0, len(owned))
		for i := range owned {
			ag := &owned[i]
			full := h.Identity.FullName(r.Context(), ag)
			ownedViews = append(ownedViews, npOwnedAgentView{ID: ag.ID, SessionName: full, FullName: full, Name: ag.Name, Online: h.AgentOnline(ag.ID)})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"principal": actor, "groups": views, "owned_agents": ownedViews})
	case http.MethodPost:
		var req struct {
			AgentID   string `json:"agent_id"`
			ShortName string `json:"short_name"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.AgentID) == "" {
			http.Error(w, "agent_id and short_name required", http.StatusBadRequest)
			return
		}
		req.ShortName = strings.TrimSpace(req.ShortName)
		if !identity.ValidGroupName(req.ShortName) {
			http.Error(w, "group name must be 1 to 8 letters or numbers", http.StatusBadRequest)
			return
		}
		g, err := h.Identity.CreateGroup(r.Context(), actor, req.AgentID, req.ShortName)
		switch {
		case errors.Is(err, identity.ErrGroupAgentUnavailable):
			http.Error(w, "owned agent not found", http.StatusNotFound)
			return
		case errors.Is(err, identity.ErrGroupNameTaken):
			suggestions, _ := h.Identity.SuggestGroupNames(r.Context(), req.ShortName, 2)
			http.Error(w, "团队名称不可用；建议名称："+strings.Join(suggestions, "、"), http.StatusConflict)
			return
		case err != nil:
			http.Error(w, "agent group create failed", http.StatusConflict)
			return
		}
		groups, err := h.Identity.ListVisibleGroups(r.Context(), actor)
		if err != nil {
			http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
			return
		}
		for _, info := range groups {
			if info.Code == g.Code {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(h.npGroupView(info))
				return
			}
		}
		http.Error(w, "agent groups unavailable", http.StatusServiceUnavailable)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNewPlatformAgentGroup 删组、入组、剔除、开关添加（旧平台 HandleConsoleAgentGroup 的新平台分支）；错误文案与状态码与旧平台同。
func (h *Hub) handleNewPlatformAgentGroup(w http.ResponseWriter, r *http.Request) {
	actor, code := platformPrincipal(r), r.PathValue("code")
	var err error
	switch {
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/members/"):
		err = h.Identity.LeaveGroup(r.Context(), actor, code, r.PathValue("agentID"))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/members"):
		var req struct {
			AgentID string `json:"agent_id"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "agent_id required", http.StatusBadRequest)
			return
		}
		err = h.Identity.JoinGroup(r.Context(), actor, code, req.AgentID)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/additions"):
		var req struct {
			Closed *bool `json:"closed"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Closed == nil {
			http.Error(w, "closed required", http.StatusBadRequest)
			return
		}
		err = h.Identity.SetGroupAdditionsClosed(r.Context(), actor, code, *req.Closed)
	case r.Method == http.MethodDelete:
		err = h.Identity.DeleteGroup(r.Context(), actor, code)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, identity.ErrGroupClosed):
		http.Error(w, "该编组已关闭添加", http.StatusConflict)
	case errors.Is(err, identity.ErrGroupMemberExists):
		http.Error(w, "该 Agent 已在该编组中", http.StatusConflict)
	case errors.Is(err, identity.ErrGroupAgentUnavailable):
		http.Error(w, "该 Agent 不可用（不存在、未激活或不属于当前业主）", http.StatusConflict)
	case errors.Is(err, identity.ErrGroupMemberNotFound):
		http.Error(w, "该 Agent 不在本编组中，可能列表已过期，请刷新", http.StatusConflict)
	case errors.Is(err, identity.ErrGroupLeaderProtected):
		http.Error(w, "组长不可剔除", http.StatusConflict)
	case errors.Is(err, identity.ErrForbidden):
		http.Error(w, "无权管理该编组", http.StatusForbidden)
	case errors.Is(err, identity.ErrNotFound):
		http.Error(w, "组码不存在", http.StatusNotFound)
	default:
		http.Error(w, "agent group action failed", http.StatusConflict)
	}
}
