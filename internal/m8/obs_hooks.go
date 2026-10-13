package m8

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
)

// obsWSReadErr 把读循环的终止错误转成 obs 的分类输入：对端关闭帧给关闭码，请求上下文结束给哨兵，其他原样（记为网络类）。
func obsWSReadErr(ctx context.Context, err error) error {
	if code := websocket.CloseStatus(err); code != -1 {
		return obs.PeerClose(int(code))
	}
	if ctx.Err() != nil {
		return obs.ContextDone()
	}
	return err
}

// RequireConsolePlatformAdmin 监控首片的鉴权（组长 2026-10-13T08:30:38Z 补裁）：
// 全站运维数据只给「当前真实 active 的 platform_admin」。manager 虽然在 newPlatformRole 里同属 management，这里一律 403；
// 不读 header／query 里的 owner，不另设 token。每次请求都现查会话与用户（失活、必须改密、存储失败都 fail-closed）。
// 旧平台（Onboarding 为 nil）没有 platform_admin 概念 ⇒ 403。
// 须套在 RequireConsolePrincipal 里面（外层先给 401／302 与写操作 Origin 口径）。
func (h *Hub) RequireConsolePlatformAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Onboarding == nil {
			writeAgentWSError(w, http.StatusForbidden, "platform_admin_required")
			return
		}
		c, err := r.Cookie(httpapi.SessionCookie)
		if err != nil || c.Value == "" {
			newPlatformDeny(w, r, false)
			return
		}
		u, err := h.Onboarding.ResolveSession(r.Context(), c.Value)
		if err != nil {
			newPlatformDeny(w, r, errors.Is(err, identity.ErrMustChange))
			return
		}
		if u == nil || u.State != identity.UserActive || u.Type != identity.UserPlatformAdmin {
			writeAgentWSError(w, http.StatusForbidden, "platform_admin_required")
			return
		}
		next(w, r)
	}
}
