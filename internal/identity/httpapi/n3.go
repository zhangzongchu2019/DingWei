package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// SessionCookie 新平台 console 会话 cookie 名。与旧平台 dw_console_session 区分，避免同域混用。
const SessionCookie = "dw_np_session"

// N3 自举之后的登录、改密、开户接口（设计件 3.3）。
type N3 struct {
	OB           *identity.Onboarding
	PublicOrigin string
	TrustProxy   bool
	// Secure 为真时 cookie 带 Secure（生产必须为真；测试在 http 上跑时置假）。
	Secure bool
}

// Register 挂路由。飞书侧的注册与登录指令不走 HTTP，由 Hub 的飞书入口调用 OB.HandleRegister / OB.HandleLogin。
func (n *N3) Register(mux Mux) {
	mux.HandleFunc("POST /console/login/password", n.passwordLogin)
	mux.HandleFunc("POST /console/login/code", n.issueLoginCode)
	mux.HandleFunc("GET /console/login/status", n.loginStatus)
	mux.HandleFunc("POST /console/logout", n.logout)
	mux.HandleFunc("POST /api/console/account/password", n.authed(true, n.changePassword))
	mux.HandleFunc("GET /api/console/platform/users", n.authed(false, n.listUsers))
	mux.HandleFunc("POST /api/console/platform/users", n.authed(false, n.createUser))
	mux.HandleFunc("POST /api/console/platform/users/{user_id}/registration-code", n.authed(false, n.regenCode))
}

// Actor 供 API.Actor 使用：按会话 cookie 认出当前用户；必须改密的会话不算登录（N3 A2）。
func (n *N3) Actor(r *http.Request) (string, error) {
	u, err := n.user(r)
	if err != nil {
		return "", err // 含 identity.ErrMustChange：API 据此回 403 must_change_password
	}
	return u.UserIDLC(), nil
}

func (n *N3) user(r *http.Request) (*identity.User, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, identity.ErrNotFound
	}
	return n.OB.ResolveSession(r.Context(), c.Value)
}

func (n *N3) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: n.Secure,
		SameSite: http.SameSiteLaxMode, Expires: n.OB.Now().Add(identity.SessionTTL),
	})
}

func (n *N3) sameOrigin(r *http.Request) bool {
	return n.PublicOrigin != "" && r.Header.Get("Origin") == n.PublicOrigin
}

// authed 会话校验＋写接口 Origin 校验。allowMustChange 为真时，必须改密的会话也放行（只给改密接口用）。
func (n *N3) authed(allowMustChange bool, next func(http.ResponseWriter, *http.Request, *identity.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := n.user(r)
		if errors.Is(err, identity.ErrMustChange) && !allowMustChange {
			writeErr(w, http.StatusForbidden, "must_change_password", nil)
			return
		}
		if err != nil && !errors.Is(err, identity.ErrMustChange) {
			writeErr(w, http.StatusUnauthorized, "unauthenticated", nil)
			return
		}
		if r.Method != http.MethodGet && !n.sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "origin_mismatch", nil)
			return
		}
		next(w, r, u)
	}
}

func (n *N3) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if !n.sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "origin_mismatch", nil)
		return
	}
	var req struct {
		UserID   string `json:"user_id"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	api := &API{TrustProxy: n.TrustProxy}
	tok, u, err := n.OB.PasswordLogin(r.Context(), req.UserID, req.Password, api.sourceIP(r))
	if err != nil {
		if errors.Is(err, identity.ErrBadCredentials) {
			writeErr(w, http.StatusUnauthorized, "bad_credentials", nil)
			return
		}
		mapErr(w, err)
		return
	}
	n.setSession(w, tok)
	writeJSON(w, http.StatusOK, map[string]any{"user_id": u.UserID, "must_change_password": u.MustChangePassword})
}

func (n *N3) issueLoginCode(w http.ResponseWriter, r *http.Request) {
	if !n.sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "origin_mismatch", nil)
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	code, err := n.OB.IssueLoginCode(req.UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": code, "expires_at": n.OB.Now().Add(identity.LoginCodeTTL).UTC().Format(time.RFC3339),
	})
}

// loginStatus 浏览器轮询：飞书确认后一次性取走会话（设为 cookie），之后同一码再取为 pending。
func (n *N3) loginStatus(w http.ResponseWriter, r *http.Request) {
	tok, ok := n.OB.ClaimLogin(r.URL.Query().Get("code"))
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"state": "pending"})
		return
	}
	n.setSession(w, tok)
	writeJSON(w, http.StatusOK, map[string]any{"state": "confirmed"})
}

func (n *N3) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: n.Secure, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (n *N3) changePassword(w http.ResponseWriter, r *http.Request, u *identity.User) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch err := n.OB.ChangePassword(r.Context(), u.UserIDLC(), req.OldPassword, req.NewPassword); {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{})
	case errors.Is(err, identity.ErrBadCredentials):
		writeErr(w, http.StatusUnauthorized, "bad_credentials", nil)
	case errors.Is(err, identity.ErrWeakPassword):
		writeErr(w, http.StatusUnprocessableEntity, "weak_password", map[string]any{"min_length": identity.MinPasswordLen})
	default:
		mapErr(w, err)
	}
}

// listUsers 平台管理 Tab 的用户列表（字段按 tosonnet50ts 页面：user_id、display_name、short_name、type、state）。
// 只给管理用户；不含任何密码、码、会话。
func (n *N3) listUsers(w http.ResponseWriter, r *http.Request, u *identity.User) {
	if !identity.CanManageUsers(u) {
		writeErr(w, http.StatusForbidden, "forbidden", nil)
		return
	}
	users, err := n.OB.Store.ListUsers(r.Context())
	if err != nil {
		mapErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, x := range users {
		out = append(out, map[string]any{
			"user_id": x.UserID, "display_name": x.DisplayName, "short_name": x.ShortName,
			"type": string(x.Type), "state": string(x.State),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (n *N3) createUser(w http.ResponseWriter, r *http.Request, u *identity.User) {
	var req struct {
		DisplayName string `json:"display_name"`
		UserID      string `json:"user_id"`
		ShortName   string `json:"short_name"`
		Type        string `json:"type"`
	}
	if !decode(w, r, &req) {
		return
	}
	nu, code, exp, err := n.OB.CreateUser(r.Context(), u, identity.CreateUserRequest{
		DisplayName: req.DisplayName, UserID: req.UserID, ShortName: req.ShortName, Type: identity.UserType(req.Type),
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]any{
			"user_id": nu.UserID, "registration_code": code, "code_expires_at": exp.UTC().Format(time.RFC3339),
		})
	case errors.Is(err, identity.ErrNotAllowed):
		writeErr(w, http.StatusForbidden, "forbidden", nil)
	case errors.Is(err, identity.ErrConflict):
		writeErr(w, http.StatusConflict, "user_exists", nil)
	default:
		mapErr(w, err)
	}
}

func (n *N3) regenCode(w http.ResponseWriter, r *http.Request, u *identity.User) {
	code, exp, err := n.OB.RegenerateRegistrationCode(r.Context(), u, r.PathValue("user_id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"registration_code": code, "code_expires_at": exp.UTC().Format(time.RFC3339)})
	case errors.Is(err, identity.ErrNotAllowed):
		writeErr(w, http.StatusForbidden, "forbidden", nil)
	default:
		mapErr(w, err)
	}
}
