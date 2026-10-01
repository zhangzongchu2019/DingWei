package m8

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新平台 console 的平台项与角色（组长台账 9c647ee7：新平台 console 全面接 N3，按「用户角色＋Agent 归属」授权）。

// newPlatformRole 由身份用户类型推出 console 角色：平台管理者与管理用户 ⇒ management；普通用户 ⇒ owner。
func (h *Hub) newPlatformRole(ctx context.Context, userLC string) (string, bool) {
	u, err := h.Identity.Store.GetUser(ctx, userLC)
	if err != nil || u.State != identity.UserActive {
		return "", false
	}
	if identity.CanManageUsers(u) {
		return consolePlatformRoleManagement, true
	}
	return consolePlatformRoleOwner, true
}

// newPlatformRequireAction RequireConsolePlatformAction 的新平台分支。
func (h *Hub) newPlatformRequireAction(action string, w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	userLC, _ := r.Context().Value(consolePrincipalContextKey{}).(string)
	role, ok := h.newPlatformRole(r.Context(), userLC)
	if !ok || !consolePlatformActionAllowed(role, action) {
		writeAgentWSError(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	return r.WithContext(context.WithValue(r.Context(), consolePlatformRoleContextKey{}, role)), true
}

// handleNewPlatformConsolePlatform 新平台的 /api/console/platform：形状与旧接口相同（role、actions、items）。
// 平台项：管理角色下发 platform-admin（console 内部 tab，无 target）。
// 不下发：Agent 编组（新平台编组归属待组长定，接好后再加，不给页面一个点不开的入口）；
//
//	Specs 与任务管理（旧平台功能，「旧功能带哪些」未定）。
func (h *Hub) handleNewPlatformConsolePlatform(w http.ResponseWriter, r *http.Request) {
	role := platformRole(r)
	// 编组已接上新平台（np_group，组长裁定 132246Z）⇒ 人人都有「Agent 管理」入口（编排件第八节定的标题；页面据 id 打开编组工作区）。
	items := []model.ConsolePlatformItem{{
		ID: "agent-groups", Kind: "agent-groups", Title: "Agent 管理",
		Href: "#agent-groups", Status: "approved", Active: true,
	}}
	if role == consolePlatformRoleManagement {
		items = append(items, model.ConsolePlatformItem{
			ID: "platform-admin", Kind: "platform-admin", Title: "平台管理",
			Href: "#platform-admin", Status: "approved", Active: true,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"role": role, "actions": platformActions(role), "items": items})
}
