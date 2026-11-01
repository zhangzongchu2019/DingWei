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
		if h.Onboarding != nil {
			// 新平台：角色由身份用户类型推出（newplat_console.go）。
			if r2, ok := h.newPlatformRequireAction(action, w, r); ok {
				next(w, r2)
			}
			return
		}
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

// builtin 入口占用的 id。放行同 id 提交会让页面出现两个 data-item-id 相同的项,
// 闸与 UI 都无法唯一寻址(与既有 "agents" 保留同一理由)。
var consolePlatformReservedItemIDs = map[string]bool{
	"agents":                 true,
	"openspec-confirmations": true,
	"change-evidence":        true,
	"agent-groups":           true,
	"specs":                  true,
	"task-board":             true,
	"pilot-questions":        true,
	"admin":                  true,
}

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
	if !consolePlatformItemID.MatchString(req.ID) || consolePlatformReservedItemIDs[req.ID] || req.Kind == "" || req.Title == "" || len(req.Title) > 120 {
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
	if h.Onboarding != nil {
		h.handleNewPlatformConsolePlatform(w, r)
		return
	}
	repo := h.Repo.(consolePlatformRepository)
	role := platformRole(r)
	items, err := repo.ListConsolePlatformItems(r.Context(), role == consolePlatformRoleManagement)
	if err != nil {
		http.Error(w, "console platform unavailable", http.StatusServiceUnavailable)
		return
	}
	// Agent 列表是 console 外壳本身,不是可点击的平台入口。保留 id=agents
	// 的占用规则,但不把一个没有动作的假入口下发给客户端。
	builtins := []model.ConsolePlatformItem{{
		ID: "specs", Kind: "spec", Title: "Specs",
		Href: "#specs", Status: "approved", Active: true,
	}, {
		ID: "task-board", Kind: "task-board", Title: "任务管理",
		Href: "#task-board", Status: "approved", Active: true,
	}, {
		// Agent 编组入口:0.2.1.55 起由 console 自己接成内部 tab;
		// S-GROUP-ENTRY(2026-09-29)起对**所有 principal** 下发,不再关在 role==management 里。
		//
		// 🟡 2026-10-01 更新:「加入到其他业主编组」面板已按业主 S5 从本页移除(组长裁定台账 494ac55b);
		//    入口仍对所有 principal 下发 —— 本页现在承载「Agent 管理」(自备审核、Agent 列表、托管)。
		//    下面是 S-GROUP-ENTRY 当时的历史理由,留作取证。
		// 🔴 为什么必须发给普通业主(修缺陷,不是放宽策略):
		//   「加入到其他业主编组」那个面板**只在这个入口里面** —— console_auth.go 的
		//   openGroupWorkspace 全文件仅一个调用点(renderPlatform 里 item.id==='agent-groups'
		//   的 onclick),没有第二条打开路径。而该功能的目标用户恰恰是"另一位业主",其默认角色
		//   是 owner(ResolveConsolePlatformRole 对每个 active principal COALESCE(…, 'owner'))。
		//   ⇒ 入口只发 management = 功能对它的目标用户不存在。取证与派工见板件
		//   runbooks/DEFECT-group-entry-is-management-only-20260929.md ·
		//   runbooks/DISPATCH-20260929-S-GROUP-ENTRY.md。
		//
		// 🔵 可见面**不是**授权面(与下面 admin 那一项同一条道理):六条 /api/console/agent-groups*
		//   挂的是 RequireConsolePrincipal(不是 RequireConsoleManagement);列表按 principal 逐组算
		//   可见性(visible = canManage || 该组成员里有我自己的 Agent);删除/剔除/开关添加另由
		//   canManageConsoleAgentGroup 守 ⇒ 本项对所有角色可见**不放宽任何授权**。
		// 🔴 角色口径与 platformActions **不再同一个**:动作集仍只对 management 放开
		//   (add/edit/delete/approve),本项只是"看得见、点得开自己有权的那部分"。
		// 🔴 形态是内部 tab(href 为 console 内锚、**无 target**):顶层导航会带走右栏那一堆活
		//   iframe 与它们的 SSE(实测 pane 数 2→0),指向旧 admin 独立文档的形态已被否掉。
		ID: "agent-groups", Kind: "agent-groups", Title: "Agent 管理",
		Href: "#agent-groups", Status: "approved", Active: true,
	}}
	if role == consolePlatformRoleManagement {
		// 平台管理入口(Nova-260921-A / B1-b):admin 能力搬进 console。
		//
		// 🔴 与上面编组入口的区别是**授权域**:本项指向 `/console/admin` —— 由 console 角色授权的
		// 新门(internal/admin/console_door.go),业主不需要在 console 里再登一次
		// (业主原话「admin 接纳 console 的鉴权方式」)。
		// 🔴 它**留在本块内不动**:role==management 同时是 RequireConsoleManagement 那道门的判据,
		// 而 /console/admin 里有**明文凭证页** ⇒ 跟着 agent-groups 一起移出去,等于把凭证页开给
		// 每个默认角色的业主。S-GROUP-ENTRY 只改编组那一项的可见面。
		// 🔵 UI 可见性不是授权判据:`/console/admin/*` 自己还有 RequireConsoleManagement 那道门,
		// "只对 management 发这一项"只是为了不让普通业主看到一个点了就 403 的入口。
		// 🔴 形态仍是 `_blank`(独立文档):顶层导航会带走右栏那一堆活 iframe 与它们的 SSE ——
		// 与编组入口当初被改成内部 tab 的同一条实测理由。
		builtins = append(builtins, model.ConsolePlatformItem{
			// 新:console 内部「平台管理」页签(组长令 2026-10-01;第一期先承载 N3「新用户注册邀请」)。
			// 仍只发给 management —— 可见面不是授权面,接口自己按用户类型再判一次(G3 设计件 3.3)。
			ID: "platform-admin", Kind: "platform-admin", Title: "平台管理",
			Href: "#platform-admin", Status: "approved", Active: true,
		}, model.ConsolePlatformItem{
			// 旧:独立文档的后台,保持原样(id/target 不动,既有测试钉着);改名只为与上面新页签区分。
			ID: "admin", Kind: "admin", Title: "旧版后台",
			Href: "/console/admin", Target: "_blank", Status: "approved", Active: true,
		})
	}
	if h.ChangeEvidence != nil && h.ChangeEvidence.Store != nil && h.ChangeEvidence.Provider != nil {
		builtins = append(builtins, model.ConsolePlatformItem{ID: "change-evidence", Kind: "change-evidence", Title: "变更风险与验收证据", Href: "/console/change-evidence", Target: "_blank", Status: "approved", Active: true})
	}
	if h.openSpecConfirmationReady() {
		builtins = append(builtins, model.ConsolePlatformItem{ID: "openspec-confirmations", Kind: "openspec-confirmations", Title: "委员会版本确认", Href: "/console/openspec-confirmations", Target: "_blank", Status: "approved", Active: true})
	}
	items = append(builtins, items...)
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
