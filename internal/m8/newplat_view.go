package m8

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新平台 view 授权（组长台账 738c4a03：G3 下一件、版本二进入条件）。
//
// 路由键：新平台一律用 full_name（名称.用户简称），不用会话名 —— 两个用户可以各有一个同名 Agent，
// 会话名＝名称小写，只按会话名寻址会串号。旧平台的全名全局唯一，所以以前没出这个问题。
// 落点（单一咽喉）：所有 view 路径最终都经 terminalBySessionNameLocked 找会话；新平台模式下它改走
// newPlatformTerminalLocked，按 full_name 解析成 (u_用户, 名称小写) 精确定位；裸会话名一律查不到。
// 鉴权：新平台会话 cookie 认人，只放行该 Agent 的归属用户；写操作与 WebSocket 升级另校验 Origin。

// newPlatformTerminalLocked 调用方须持有 h.mu。
func (h *Hub) newPlatformTerminalLocked(fullName string) (*terminalState, bool) {
	ag, _, err := h.Identity.ResolveFullName(context.Background(), fullName)
	if err != nil {
		return nil, false
	}
	keyID, session := AgentKeyID(ag.OwnerLC), ag.NameLC()
	if h.sessionClients[keyID][session] == nil {
		return nil, false
	}
	return h.terminalForSession(keyID, session), true
}

// viewSessionNameAllowed keeps the legacy session-name grammar and, only in new-platform mode,
// also accepts a canonical full_name that resolves to a current Agent. This is used by the
// signed view-service callbacks; it does not turn arbitrary dotted strings into routable names.
func (h *Hub) viewSessionNameAllowed(ctx context.Context, name string) bool {
	if sessionNamePattern.MatchString(name) {
		return true
	}
	if h.Identity == nil {
		return false
	}
	_, _, err := h.Identity.ResolveFullName(ctx, name)
	return err == nil
}

// viewInternalSession resolves a public new-platform full_name to the key/session pair used
// by terminal state and sessionClients. Legacy mode keeps its existing global session lookup.
func (h *Hub) viewInternalSession(ctx context.Context, name string) (string, string, bool) {
	name = strings.TrimSpace(name)
	if h.Identity != nil {
		ag, _, err := h.Identity.ResolveFullName(ctx, name)
		if err != nil {
			return "", "", false
		}
		return AgentKeyID(ag.OwnerLC), ag.NameLC(), true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.terminalBySessionNameLocked(name)
	if !ok {
		return "", "", false
	}
	return st.keyID, st.sessionName, true
}

// grantNewPlatformViewServiceInput gives the current View Service virtual viewer the same
// single-writer grant used by #unlock. It never selects a disconnected browser viewer.
func (h *Hub) grantNewPlatformViewServiceInput(ctx context.Context, fullName string) bool {
	keyID, sessionName, ok := h.viewInternalSession(ctx, fullName)
	if !ok {
		return false
	}
	h.mu.Lock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	if st == nil {
		h.mu.Unlock()
		return false
	}
	h.sweepStaleViewViewersLocked(st)
	var target *terminalViewer
	for _, viewer := range st.viewers {
		if viewer.conn != nil || !viewer.disconnectedAt.IsZero() || viewer.lastSeen.IsZero() {
			continue
		}
		if target == nil || viewer.lastSeen.After(target.lastSeen) {
			target = viewer
		}
	}
	if target != nil && st.writerPage == target.id && st.writerToken != "" && time.Now().Before(st.writerUntil) {
		// /status is polled every five seconds. Reissuing here would invalidate the
		// token cached by View Service and recreate the intermittent 403 fixed by T-141.
		st.writerUntil = time.Now().Add(terminalTokenTTL)
		if st.cookieToken == st.writerToken {
			st.cookieUntil = st.writerUntil
		}
		h.mu.Unlock()
		return true
	}
	h.mu.Unlock()
	if target == nil {
		return false
	}
	_, err := h.grantTerminalInput(keyID, sessionName, target.code)
	return err == nil
}

// newPlatformViewAuth 新平台 view 路由的认人与授权；通过返回 true，否则已写好拒绝应答。
// 不是归属用户与 Agent 不存在回同一个 404，不泄露别人名下有没有这个 Agent。
func (h *Hub) newPlatformViewAuth(w http.ResponseWriter, r *http.Request) bool {
	c, err := r.Cookie(httpapi.SessionCookie)
	if err != nil || c.Value == "" {
		newPlatformDeny(w, r, false)
		return false
	}
	user, err := h.resolveNewPlatformHTTPSession(w, r, c.Value)
	if err != nil {
		newPlatformDeny(w, r, errors.Is(err, identity.ErrMustChange))
		return false
	}
	ag, _, err := h.Identity.ResolveFullName(r.Context(), strings.TrimSpace(r.PathValue("sessionName")))
	if err != nil || ag.OwnerLC != user.UserIDLC() {
		writeAgentWSError(w, http.StatusNotFound, "not_found")
		return false
	}
	// 写操作与 WebSocket 升级都校验 Origin（后者防跨站 WebSocket 劫持）。
	if (r.Method != http.MethodGet || strings.EqualFold(r.Header.Get("Upgrade"), "websocket")) &&
		!h.originAllowed(r.Header.Get("Origin"), h.PublicOrigin != "" && r.Header.Get("Origin") == h.PublicOrigin, len(r.Header.Values("Origin"))) {
		writeAgentWSError(w, http.StatusForbidden, "origin_mismatch")
		return false
	}
	return true
}

// newPlatformViewCapability verifies a console-issued terminal page capability and, on success,
// writes only the attested principal header consumed by view-service. The caller strips both
// trust headers before reaching here. Missing/invalid capabilities deliberately return false
// without an HTTP error so view-service falls back to its existing #unlock page-lock path.
func (h *Hub) newPlatformViewCapability(w http.ResponseWriter, r *http.Request) (bool, error) {
	repo, ok := h.Repo.(consoleAuthRepository)
	if !ok {
		return false, errors.New("console repository unavailable")
	}
	pageID := strings.TrimSpace(r.Header.Get("X-DW-Console-Page"))
	raw := strings.TrimSpace(r.Header.Get("X-DW-Console-Capability"))
	if pageID == "" || raw == "" {
		return false, nil
	}
	ag, _, err := h.Identity.ResolveFullName(r.Context(), strings.TrimSpace(r.PathValue("sessionName")))
	if err != nil {
		return false, nil
	}
	sess, ok := h.newPlatformSessionProjection(w, r)
	if !ok || sess.PrincipalID != ag.OwnerLC {
		return false, nil
	}
	capability, err := repo.ResolveConsolePageCapability(r.Context(), hashConsoleToken(raw), ag.ID, pageID, h.now())
	if err != nil {
		return false, err
	}
	owner := identity.Lower(ag.OwnerLC)
	if capability == nil || capability.PrincipalID != owner ||
		!h.newPlatformSessionValid(r.Context(), capability.SessionID, owner) {
		return false, nil
	}
	r.Header.Set("X-DW-Console-Principal", owner)
	return true, nil
}

// dispatchNewPlatformTerminalInput 新平台的 ＃unlock／＃lock：发信人（信道给的飞书身份）映射成用户，
// 只在该用户虚拟 key 下的页面里按码查找；不支持带会话名的旧写法。
func (h *Hub) dispatchNewPlatformTerminalInput(ctx context.Context, msg model.Message, commandText string) (model.PrefixDispatchResult, bool, error) {
	action, sessionName, pageCode, ok := parseTerminalInputCommand(commandText)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if sessionName != "" || pageCode == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "新平台请用「＃unlock 页面码」或「＃lock 页面码」，页面码在 view 页上。"}, true, nil
	}
	if !trustedViewUnlockIngress(msg.IngressProvenance) { // 同 dispatchNewPlatformCommand：不可信入站一律拒（板 6c90284e）
		return model.PrefixDispatchResult{Matched: true, Reply: untrustedIngressReply}, true, nil
	}
	sender := identity.Identity{Provider: "feishu", Issuer: strings.TrimSpace(msg.BotChannelID), Subject: strings.TrimSpace(msg.SenderOpenID)}
	user, err := h.Identity.Store.ResolveIdentity(ctx, sender)
	if err != nil || user.State != identity.UserActive {
		return model.PrefixDispatchResult{Matched: true, Reply: "此飞书身份未绑定到可用的平台用户，无法解锁。"}, true, nil
	}
	keyID := AgentKeyID(user.UserIDLC())
	h.mu.Lock()
	var mk, ms string
	n := 0
	for _, st := range h.terminals {
		if st.keyID != keyID {
			continue
		}
		for _, v := range st.viewers {
			if strings.EqualFold(v.code, pageCode) {
				mk, ms = st.keyID, st.sessionName
				n++
			}
		}
	}
	h.mu.Unlock()
	switch {
	case n == 0:
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到页面码 " + pageCode + " 对应的你名下的页面（先在网页打开会话）"}, true, nil
	case n > 1:
		return model.PrefixDispatchResult{Matched: true, Reply: "页面码 " + pageCode + " 命中多个页面，请刷新其中一个再试"}, true, nil
	}
	if action == "lock" {
		h.revokeTerminalInput(mk, ms)
		return model.PrefixDispatchResult{Matched: true, Reply: "已锁定输入。"}, true, nil
	}
	if _, err := h.grantTerminalInput(mk, ms, pageCode); err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "已解锁输入，8小时无交互自动回收。"}, true, nil
}

// newPlatformPrincipal 新平台 console 页面与接口的认人（RequireConsolePrincipal 的新平台分支）。
// 浏览器打开页面（GET 且要 HTML）：未登录 ⇒ 302 到 /console/login；必须改密 ⇒ 302 到 /console/login#change。
// 接口：未登录 ⇒ 401 unauthenticated；必须改密 ⇒ 403 must_change_password（console 页的守卫认这两个码弹回登录页）。
func (h *Hub) newPlatformPrincipal(w http.ResponseWriter, r *http.Request) (string, bool) {
	var user *identity.User
	var err error
	if c, cerr := r.Cookie(httpapi.SessionCookie); cerr == nil && c.Value != "" {
		user, err = h.resolveNewPlatformHTTPSession(w, r, c.Value)
	} else {
		err = identity.ErrNotFound
	}
	if err != nil {
		newPlatformDeny(w, r, errors.Is(err, identity.ErrMustChange))
		return "", false
	}
	// 写操作校验 Origin（与 N3、入网接口、view 授权同一口径；cookie 之外再挡一层跨站请求）。
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !h.originAllowed(r.Header.Get("Origin"), h.PublicOrigin != "" && r.Header.Get("Origin") == h.PublicOrigin, len(r.Header.Values("Origin"))) {
		writeAgentWSError(w, http.StatusForbidden, "origin_mismatch")
		return "", false
	}
	return user.UserIDLC(), true
}

func (h *Hub) resolveNewPlatformHTTPSession(w http.ResponseWriter, r *http.Request, token string) (*identity.User, error) {
	u, renewed, err := h.Onboarding.ResolveSessionForHTTP(r.Context(), token)
	if renewed {
		secure := r.TLS != nil || strings.HasPrefix(h.PublicOrigin, "https://")
		http.SetCookie(w, httpapi.NewSessionCookie(token, secure, h.Onboarding.Now()))
	}
	return u, err
}

// newPlatformWantsPage 浏览器打开页面：GET、要 HTML、不是 WebSocket 升级、不是 /api/ 接口。
// 接口一律不重定向（组长裁定 140013Z ②：必须改密访问任何 /api/console/* 一律 403，不论 Accept）。
func newPlatformWantsPage(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") &&
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && !strings.HasPrefix(r.URL.Path, "/api/")
}

// newPlatformDeny 未登录或必须改密时的拒绝应答（/console 与 /view 同一口径，组长裁定 140013Z ①）：
// 浏览器打开页面 ⇒ 302 到 /console/login（必须改密 ⇒ /console/login#change）；接口与 WebSocket ⇒ 401 unauthenticated／403 must_change_password。
func newPlatformDeny(w http.ResponseWriter, r *http.Request, mustChange bool) {
	switch {
	case newPlatformWantsPage(r) && mustChange:
		http.Redirect(w, r, "/console/login#change", http.StatusFound)
	case newPlatformWantsPage(r):
		http.Redirect(w, r, "/console/login", http.StatusFound)
	case mustChange:
		writeAgentWSError(w, http.StatusForbidden, "must_change_password")
	default:
		writeAgentWSError(w, http.StatusUnauthorized, "unauthenticated")
	}
}

// newPlatformCanonicalName full_name 的规范写法：名称小写.简称小写。
// view-service 的会话键（X-DW-Session ⇒ /status 的 session）、上传票据的会话名都用它 ——
// 页面拿 /status 的 session 拼 capability 与上传票据的路径、带上传头，并与票据的 session 严格比较，
// 所以这几处必须同值；地址栏里大小写不同的写法（Kimi.ZZC）也落到同一个键上。
func (h *Hub) newPlatformCanonicalName(ctx context.Context, fullName string) (string, *identity.Agent, bool) {
	ag, u, err := h.Identity.ResolveFullName(ctx, fullName)
	if err != nil {
		return "", nil, false
	}
	return ag.NameLC() + "." + identity.Lower(u.ShortName), ag, true
}

// handleNewPlatformFileUploadTicket 新平台的上传票据（RequireConsoleAgent 已按 cookie＋full_name 认过归属用户、校过 Origin）。
// 票据的会话名＝规范 full_name：上传时页面带的 X-DingWei-Session 头就是它，文件服务要求两者相等。
func (h *Hub) handleNewPlatformFileUploadTicket(w http.ResponseWriter, r *http.Request) {
	canon, ag, ok := h.newPlatformCanonicalName(r.Context(), strings.TrimSpace(r.PathValue("sessionName")))
	if !ok {
		writeAgentWSError(w, http.StatusNotFound, "not_found")
		return
	}
	h.writeConsoleUploadTicket(w, r, AgentKeyID(ag.OwnerLC), ag.NameLC(), canon)
}
