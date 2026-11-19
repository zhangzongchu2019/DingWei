package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
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
	DisplayName string    `json:"display_name,omitempty"`
	OwnerKey    string    `json:"owner_key"`
	Online      bool      `json:"online"`
	CreatedAt   time.Time `json:"created_at"`
}

type npGroupView struct {
	Code                string              `json:"code"`
	ShortName           string              `json:"short_name"`
	CreatorPrincipal    string              `json:"creator_principal"`
	AdditionsClosed     bool                `json:"additions_closed"`
	LeaderOnly          bool                `json:"leader_only"`
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

func displayNameForConsole(a identity.Agent) string {
	if a.Origin != identity.OriginExternal || !extshared.ExternalDisplayNamesEnabled() {
		return ""
	}
	normalized, err := extshared.NormalizeExternalDisplayName(a.DisplayName)
	if err != nil {
		return ""
	}
	return normalized
}

type npOwnedAgentView struct {
	ID          string `json:"id"`
	SessionName string `json:"session_name"`
	FullName    string `json:"full_name"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Online      bool   `json:"online"`
	External    bool   `json:"external,omitempty"`
	Enrollment  string `json:"enrollment,omitempty"`
	Pairing     string `json:"pairing,omitempty"`
	Target      string `json:"target,omitempty"`
}

func (h *Hub) npGroupView(info identity.GroupInfo) npGroupView {
	v := npGroupView{
		Code: info.Code, ShortName: info.ShortName, CreatorPrincipal: info.CreatorLC, AdditionsClosed: info.AdditionsClosed, LeaderOnly: info.LeaderOnly,
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
			Name: m.Agent.Name, DisplayName: displayNameForConsole(m.Agent), OwnerKey: key, Online: h.AgentOnline(m.Agent.ID), CreatedAt: m.JoinedAt,
		})
	}
	return v
}

// groupNameInvalidText 编组名不合规的提示；与页面 GROUP_NAME_RULE_TEXT 逐字相同（console_group_name_text_test.go 钉住）。
const groupNameInvalidText = "编组名只能用 1～24 个字母、数字或下划线（可用中文、日文、韩文；不含空格、连字符、点号及其他符号）"

func writeGroupError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
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
			if _, ag := extshared.ParseAGCode(g.Code); ag {
				continue
			}
			views = append(views, h.npGroupView(g))
		}
		ownedViews := make([]npOwnedAgentView, 0, len(owned))
		for i := range owned {
			ag := &owned[i]
			full := h.Identity.FullName(r.Context(), ag)
			view := npOwnedAgentView{ID: ag.ID, SessionName: full, FullName: full, Name: ag.Name, DisplayName: displayNameForConsole(*ag), Online: h.AgentOnline(ag.ID), External: ag.Origin == identity.OriginExternal}
			if view.External && strings.Contains(ag.Name, "_EX") {
				view.Pairing = "未创建"
				view.Target = "—"
				view.Enrollment = "已验证／未编组"
				if codes, e := h.Identity.Store.ListAgentGroups(r.Context(), ag.ID); e == nil && len(codes) > 0 {
					view.Enrollment = "已验证／已编组"
				}
			}
			ownedViews = append(ownedViews, view)
		}
		externalCandidates := []npOwnedAgentView{}
		if h.Identity.ExternalGroups {
			if store, ok := h.Identity.Store.(interface {
				ExternalGroupCandidates(context.Context, string, string) ([]identity.Agent, error)
			}); ok {
				items, e := store.ExternalGroupCandidates(r.Context(), actor, h.Identity.CommunicationTenantKey)
				if e != nil {
					http.Error(w, "agent groups unavailable", 503)
					return
				}
				for _, a := range items {
					full := h.Identity.FullName(r.Context(), &a)
					externalCandidates = append(externalCandidates, npOwnedAgentView{ID: a.ID, SessionName: full, FullName: full, Name: a.Name, DisplayName: displayNameForConsole(a), External: true, Online: h.AgentOnline(a.ID)})
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"principal": actor, "groups": views, "owned_agents": ownedViews, "external_candidates": externalCandidates})
	case http.MethodPost:
		var req struct {
			AgentID   string `json:"agent_id"`
			ShortName string `json:"short_name"`
		}
		// R-V2-0102-GROUP-FIXES B：三种 400 改为带码的中文（页面 groupAction 显示 message）；其余分支不动。
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			writeGroupError(w, http.StatusBadRequest, "bad_request", "请求格式不正确，请刷新页面后重试")
			return
		}
		if strings.TrimSpace(req.AgentID) == "" {
			writeGroupError(w, http.StatusBadRequest, "group_leader_required", "请选择组长")
			return
		}
		req.ShortName = identity.NormalizeGroupName(req.ShortName) // 去不可见字符与首尾空白，校验与落库同值
		if !identity.ValidGroupName(req.ShortName) {
			writeGroupError(w, http.StatusBadRequest, "group_name_invalid", groupNameInvalidText)
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
	if _, ag := extshared.ParseAGCode(code); ag {
		http.Error(w, "请使用临时编组属性页", http.StatusForbidden)
		return
	}
	var err error
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/communication"):
		var req struct {
			LeaderOnly *bool `json:"leader_only"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.LeaderOnly == nil {
			http.Error(w, "leader_only required", http.StatusBadRequest)
			return
		}
		err = h.Identity.SetGroupLeaderOnly(r.Context(), actor, code, *req.LeaderOnly)
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
		h.broadcastNewPlatformDirectories(r.Context())
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
