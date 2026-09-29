package m8

import (
	"context"
	"net/http"
)

// Nova-260921-A(B1-b):admin 能力搬进 console 的那道门。
//
// 🔴 为什么**不能**用现成的 RequireConsolePlatformAction("read") 守这道门:
//
//	console_platform.go:45        role=owner ⇒ read／submit **都允许**
//	store/console_auth.go:222     ResolveConsolePlatformRole 用 COALESCE(…, 'owner')
//	                              ⇒ **每个 active principal 默认就是 owner**
//	⇒ 用"动作动词"守门,等于**每个登录了 console 的普通业主**都能打开
//	  凭证明文页 ／ 文件对象 ／ Provision 下发 ／ 机器人通道。
//	(业主原话是「仅对管理者有效」;板件 REQUIREMENT-REGISTRY-20260921.md:42「管理层入口(仅管理者)」)
//
// ⇒ 本守卫的判据是**角色身份 == management**,不是任何动作动词。
//
// 🔴 fail-closed:开关关闭 ／ 取不到 repo ／ 取不到 principal ／ 角色查询出错 ⇒ **一律拒**。
// 「查不了」不等于「通过」。
//
// 必须在 RequireConsolePrincipal **之后**(它提供 principal 的 context 值)。
// 即使被漏挂,principal 取不到 ⇒ 403,不会静默放行。
func (h *Hub) RequireConsoleManagement(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 与 RequireConsolePrincipal 同一口径:开关关闭时控制台端点**没有任何合法用途**,
		// 404 而非 401/403 —— 不泄露"这里有个管理入口"。
		// 🔵 而旧 /admin/* 那道门不看这个开关 ⇒ 业主"回退的地方"在回滚态下仍然可用。
		if !h.consoleAuthEnabled {
			http.NotFound(w, r)
			return
		}
		repo, repoOK := h.Repo.(consolePlatformRepository)
		principalID := ConsolePrincipal(r)
		if !repoOK || principalID == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		role, err := repo.ResolveConsolePlatformRole(r.Context(), principalID)
		if err != nil {
			http.Error(w, "console platform unavailable", http.StatusServiceUnavailable)
			return
		}
		if role != consolePlatformRoleManagement {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), consolePlatformRoleContextKey{}, role)
		next(w, r.WithContext(ctx))
	}
}

// ConsolePrincipal 返回守卫放进请求的 console principal id;取不到返回 ""。
//
// 🔴 它**不是**授权判据,只是把已授权的身份交给同请求的下游(例如 admin 门 B 的审计归属)。
// 授权只由 RequireConsolePrincipal ／ RequireConsoleManagement ／ RequireConsolePlatformAction 做。
// 导出它是为了让**另一个包**能读到(internal/admin 不能访问 m8 的私有 context key,
// 而 admin → m8 的依赖方向已存在,反向会成环)。
func ConsolePrincipal(r *http.Request) string {
	if r == nil {
		return ""
	}
	principalID, _ := r.Context().Value(consolePrincipalContextKey{}).(string)
	return principalID
}
