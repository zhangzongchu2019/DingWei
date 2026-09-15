package m8

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const consoleSessionCookie = "dw_console_session"

type consoleAuthRepository interface {
	ResolveConsoleSession(context.Context, string, time.Time) (*model.ConsoleSession, error)
	ResolveGrantedConsoleAgent(context.Context, string, string, string) (*model.ConsoleAgent, error)
	ListConsoleAgents(context.Context, string) ([]model.ConsoleAgent, error)
	RotateConsolePageCapability(context.Context, model.ConsolePageCapability, string, string, time.Time) error
}

type consolePrincipalContextKey struct{}
type consoleAgentContextKey struct{}
type consoleSessionContextKey struct{}

func hashConsoleToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type ConsoleAuthMode struct{ enabled bool }

func (m ConsoleAuthMode) Enabled() bool    { return m.enabled }
func ConsoleAuthDisabled() ConsoleAuthMode { return ConsoleAuthMode{} }

func ParseConsoleAuthEnabled(raw string) (ConsoleAuthMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return ConsoleAuthMode{enabled: true}, nil
	case "", "0", "false", "no", "n", "off":
		return ConsoleAuthMode{}, nil
	default:
		return ConsoleAuthMode{}, fmt.Errorf("invalid WP_CONSOLE_AUTH_ENABLED value %q", raw)
	}
}

func randomConsoleToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Hub) authenticateConsole(w http.ResponseWriter, r *http.Request) (consoleAuthRepository, *model.ConsoleSession, bool) {
	repo, ok := h.Repo.(consoleAuthRepository)
	if !ok {
		http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
		return nil, nil, false
	}
	cookie, err := r.Cookie(consoleSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil, nil, false
	}
	session, err := repo.ResolveConsoleSession(r.Context(), hashConsoleToken(cookie.Value), time.Now())
	if err != nil {
		http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
		return nil, nil, false
	}
	if session == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil, nil, false
	}
	return repo, session, true
}

func (h *Hub) RequireConsolePrincipal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 🔴 2026-09-15 修：开关关闭时【拒绝】，不再放行。
		//
		// 原来是 next(w, r) 直接放行，且**不设 principal 的 context 值** ——
		// 实测后果（本地试跑，非推理）：
		//   WP_CONSOLE_AUTH_ENABLED=false 时
		//     GET /console            ⇒ HTTP 200，**未登录即可拿到页面**
		//     GET /api/console/agents ⇒ **panic**（interface conversion: nil, not string），连接被关
		//     连打 5 次 ⇒ 累计 6 次 panic（服务不整体崩，但任何人无需凭据即可触发）
		//
		// ⚠️ 而「开关关闭」是【部署代码后、打开开关前】的必经状态，
		//    也是启用单写明的【回滚状态】 ⇒ 这个洞正好落在最该安全的那一格上。
		//
		// 为什么是拒绝而不是修 handler 了事：**控制台端点在开关关闭时没有任何合法用途**，
		// 放行既不服务任何人、又制造暴露面。404 而非 401：不泄露「这里有个控制台」。
		//
		// 🔴 本守卫**只**用于 /console 与 /api/console/agents（已逐条核过路由表）；
		//    **RequireConsoleAgent 不做同样改动** —— 它还包着 /view/{s} 与 /ws/view/{s}，
		//    而那条路径正是关掉开关后要回落的**旧飞书解锁通路**，改成拒绝会打断回滚。
		if !h.consoleAuthEnabled {
			http.NotFound(w, r)
			return
		}
		_, session, ok := h.authenticateConsole(w, r)
		if !ok {
			return
		}
		ctx := context.WithValue(r.Context(), consolePrincipalContextKey{}, session.PrincipalID)
		ctx = context.WithValue(ctx, consoleSessionContextKey{}, *session)
		next(w, r.WithContext(ctx))
	}
}

// RequireConsoleAgent protects every public view surface when unified auth is
// enabled. Authorization is derived only from the explicit principal grant;
// key_id and session_name are returned after authorization as runtime targets.
func (h *Hub) RequireConsoleAgent(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.consoleAuthEnabled {
			next(w, r)
			return
		}
		repo, session, ok := h.authenticateConsole(w, r)
		if !ok {
			return
		}
		sessionName := strings.TrimSpace(r.PathValue("sessionName"))
		agent, err := repo.ResolveGrantedConsoleAgent(r.Context(), session.PrincipalID, sessionName, permission)
		if err != nil {
			http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
			return
		}
		if agent == nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), consolePrincipalContextKey{}, session.PrincipalID)
		ctx = context.WithValue(ctx, consoleSessionContextKey{}, *session)
		ctx = context.WithValue(ctx, consoleAgentContextKey{}, *agent)
		next(w, r.WithContext(ctx))
	}
}

func (h *Hub) HandleConsolePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, `<!doctype html><meta charset=utf-8><title>DingWei Agent Console</title><style>body{margin:0;display:grid;grid-template-columns:280px 1fr;height:100vh;font:14px sans-serif}nav{overflow:auto;border-right:1px solid #ddd;padding:12px}button{display:block;width:100%;margin:4px 0;text-align:left}iframe{width:100%;height:100%;border:0}</style><nav><h2>Agents</h2><div id=a></div></nav><iframe id=v title="Agent view"></iframe><script>fetch('/api/console/agents').then(r=>r.json()).then(x=>x.agents.forEach(a=>{let b=document.createElement('button');b.textContent=a.session+(a.online?'':' (offline)');b.disabled=!a.online;b.onclick=()=>v.src='/view/'+encodeURIComponent(a.session);document.querySelector('#a').append(b)}))</script>`)
}

func (h *Hub) HandleConsoleAgents(w http.ResponseWriter, r *http.Request) {
	repo := h.Repo.(consoleAuthRepository)
	// 🔴 深度防御：即使守卫被改动或被绕过，handler 也不得 panic。
	// 「任何 handler 都不该因为缺一个 context 值而 panic」——
	// 上面那道闸去掉暴露面，这一行保证即便闸失效也只是 500，不是 panic。
	principal, ok := r.Context().Value(consolePrincipalContextKey{}).(string)
	if !ok || principal == "" {
		http.Error(w, "console unavailable", 500)
		return
	}
	agents, err := repo.ListConsoleAgents(r.Context(), principal)
	if err != nil {
		http.Error(w, "console unavailable", 503)
		return
	}
	type item struct {
		Session string `json:"session"`
		Online  bool   `json:"online"`
	}
	out := make([]item, 0, len(agents))
	h.mu.Lock()
	for _, a := range agents {
		out = append(out, item{a.SessionName, h.sessionClients[a.KeyID] != nil && h.sessionClients[a.KeyID][a.SessionName] != nil})
	}
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"agents": out})
}

func (h *Hub) HandleConsoleCapability(w http.ResponseWriter, r *http.Request) {
	repo := h.Repo.(consoleAuthRepository)
	// 🔴 深度防御，同 HandleConsoleAgents。
	// ⚠️ 本 handler 挂在 RequireConsoleAgent 下，而该守卫在开关关闭时**仍然放行**
	//    （因为它同时包着 /view/* 这条回滚路径，不能改成拒绝）
	//    ⇒ 本 handler 在开关关闭时**确实会被无凭据请求打到**，这两行不是多余的。
	session, okS := r.Context().Value(consoleSessionContextKey{}).(model.ConsoleSession)
	agent, okA := r.Context().Value(consoleAgentContextKey{}).(model.ConsoleAgent)
	if !okS || !okA {
		http.Error(w, "capability unavailable", 500)
		return
	}
	var req struct {
		PageID string `json:"page_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.PageID) == "" {
		http.Error(w, "page_id required", 400)
		return
	}
	raw, err := randomConsoleToken()
	if err != nil {
		http.Error(w, "capability unavailable", 503)
		return
	}
	id, err := randomConsoleToken()
	if err != nil {
		http.Error(w, "capability unavailable", 503)
		return
	}
	now := time.Now()
	capability := model.ConsolePageCapability{ID: id, SessionID: session.ID, PrincipalID: session.PrincipalID, AgentID: agent.ID, PageID: req.PageID, TokenHash: hashConsoleToken(raw), ExpiresAt: now.Add(5 * time.Minute)}
	if err := repo.RotateConsolePageCapability(r.Context(), capability, agent.KeyID, agent.SessionName, now); err != nil {
		http.Error(w, "forbidden", 403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"page_id": req.PageID, "capability": raw, "expires_in": 300})
}

func (h *Hub) RequireConsoleViewSubPath(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		permission := "view"
		if r.Method != http.MethodGet {
			permission = "interact"
		}
		h.RequireConsoleAgent(permission, next)(w, r)
	}
}
