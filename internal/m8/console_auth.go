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

const (
	// D1-b(组长 2026-09-18 裁,已报业主):滑动续期必须有绝对上限。
	// 『一直打开就不用重登』**不等于**『永远不用重登』——
	// 没有上限意味着一个被窃的 cookie 永久有效。
	consoleSessionAbsoluteTTL = 30 * 24 * time.Hour
	// 剩余不足一半才续:每会话最多每 consoleSessionTTL/2 写一次库,与开了几个标签页无关。
	consoleSessionRenewBelow = consoleSessionTTL / 2
	// 再加一道进程内节流,防同一瞬间多个标签页同时触发。
	consoleSessionTouchMin = time.Minute
)

// newConsoleSessionCookie 是【唯一】构造 console 会话 cookie 的地方。
//
// 🔴 登录与续期必须共用它。组长把这一条定为硬闸,理由照抄:
// 只延后 DB 里的 expires_at 是不够的 —— cookie 的 MaxAge 在登录那一刻写死,
// 到点浏览器自己就把它删了,服务端再宽松也收不到凭据。
// 而属性只要有一处不同就是一次**静默降级**:漏一个 Secure 不会让任何测试变红,
// 只会让 cookie 允许明文发送。共用一个构造器 ⇒ 属性不一致在结构上不可能发生。
func newConsoleSessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     consoleSessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(consoleSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

func (h *Hub) now() time.Time {
	if h.NowFunc != nil {
		return h.NowFunc()
	}
	return time.Now()
}

// consoleTouchAllowed 是写放大闸:同一会话 consoleSessionTouchMin 内只放行一次落库。
func (h *Hub) consoleTouchAllowed(sessionID string, now time.Time) bool {
	h.consoleTouchMu.Lock()
	defer h.consoleTouchMu.Unlock()
	if last, ok := h.consoleTouched[sessionID]; ok && now.Sub(last) < consoleSessionTouchMin {
		return false
	}
	if h.consoleTouched == nil {
		// New() 里已初始化;这里只兜零值 Hub(测试直接构造)的情况。
		h.consoleTouched = make(map[string]time.Time)
	}
	h.consoleTouched[sessionID] = now
	return true
}

// renewConsoleSession 实现业主第 7 条:页面一直开着就不必到期重走飞书登录。
//
// 之所以不需要前端配合:console 页每 15s 打一次 /api/console/agents、
// 每个 view 页每 5s 打一次 /view/{s}/status,两者都经 authenticateConsole。
// 页面开着 ⇒ 服务端每几秒就见它一次 ⇒ 在这里续,就等于「开着即续」。
//
// D1-c(组长裁):**不要求「真人活动」**,页面在轮询即可 —— 业主原话就是『只要一直打开』。
func (h *Hub) renewConsoleSession(r *http.Request, w http.ResponseWriter, repo consoleAuthRepository, session *model.ConsoleSession, rawToken string, now time.Time) {
	if session.CreatedAt.IsZero() {
		// 算不出绝对上限就不续(fail-closed):宁可让用户重登,
		// 也不能让一个上限未知的会话被无限延长。
		return
	}
	if session.ExpiresAt.Sub(now) >= consoleSessionRenewBelow {
		return
	}
	newExpiry := now.Add(consoleSessionTTL)
	if cap := session.CreatedAt.Add(consoleSessionAbsoluteTTL); newExpiry.After(cap) {
		newExpiry = cap
	}
	if !newExpiry.After(session.ExpiresAt) {
		// 已经贴到 30 天绝对上限:不再延长,到点按原样过期。
		return
	}
	if !h.consoleTouchAllowed(session.ID, now) {
		return
	}
	if err := repo.TouchConsoleSession(r.Context(), session.ID, newExpiry, now); err != nil {
		// 续期失败不影响本次请求:凭据此刻仍然有效,下一次轮询还会再试。
		return
	}
	session.ExpiresAt = newExpiry
	// 🔴 DB 与 cookie 必须一起延 —— 只延一边就会「服务端明明是对的」却仍在第 12 小时失败。
	http.SetCookie(w, newConsoleSessionCookie(rawToken))
}

type consoleAuthRepository interface {
	ResolveConsoleSession(context.Context, string, time.Time) (*model.ConsoleSession, error)
	TouchConsoleSession(context.Context, string, time.Time, time.Time) error
	ResolveGrantedConsoleAgent(context.Context, string, string, string) (*model.ConsoleAgent, error)
	ListConsoleAgents(context.Context, string) ([]model.ConsoleAgent, error)
	RotateConsolePageCapability(context.Context, model.ConsolePageCapability, string, string, time.Time) error
	ResolveConsolePageCapability(context.Context, string, string, string, time.Time) (*model.ConsolePageCapability, error)
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
	now := h.now()
	session, err := repo.ResolveConsoleSession(r.Context(), hashConsoleToken(cookie.Value), now)
	if err != nil {
		http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
		return nil, nil, false
	}
	if session == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil, nil, false
	}
	h.renewConsoleSession(r, w, repo, session, cookie.Value, now)
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
	fmt.Fprint(w, consolePageHTML)
}

// consolePageHTML 业主 2026-09-16 定:右侧改为**多 tab**,每点一个 Agent 增加一个属性页。
//
// 🔴 关键设计:切换 tab 用**显示/隐藏**,**不换 iframe 的 src**。
//
//	换 src 会把上一个 Agent 的页面整个销毁 —— 那正是业主此前每次切 Agent 都要重新加载的原因。
//
// 🔴 上限 8(业主定)。为什么必须有上限:每个 view 页是一条 **SSE 长连接**,
//
//	而浏览器对同一源的 **HTTP/1.1 并发连接上限是 6**(HTTP/2 多路复用后才不适用)。
//	⇒ **无上限时,第 7 个 tab 会直接卡住,且表现为「页面一直转圈」而非报错** —— 又一种没有声音的失败。
const consolePageHTML = `<!doctype html><meta charset=utf-8><title>DingWei Agent Console</title>
<style>
body{margin:0;display:grid;grid-template-columns:240px 1fr;height:100vh;font:14px sans-serif}
nav{overflow:auto;border-right:1px solid #ddd;padding:10px}
nav button{display:block;width:100%;margin:3px 0;text-align:left;padding:4px 6px}
#platform-items a{display:block;padding:4px 6px;margin:3px 0;border:1px solid #ddd;border-radius:4px;color:#174f8a;text-decoration:none}
#platform-actions{display:flex;flex-wrap:wrap;gap:4px;margin:6px 0 12px}
#platform-actions button{width:auto;margin:0;font-size:12px}
main{display:flex;flex-direction:column;min-width:0}
#tabs{display:flex;flex-wrap:wrap;gap:4px;border-bottom:1px solid #ddd;padding:6px;min-height:28px}
#tabs .tab{display:inline-flex;align-items:center;gap:6px;border:1px solid #ccc;border-radius:4px;
  padding:3px 8px;cursor:pointer;background:#f7f7f7;max-width:200px}
#tabs .tab.on{background:#fff;border-color:#4a90d9;font-weight:600}
#tabs .tab.offline{opacity:.5}
#tabs .tab .x{color:#999;font-weight:400}
#tabs .tab .x:hover{color:#c00}
#panes{flex:1;position:relative;min-height:0}
#panes iframe{position:absolute;inset:0;width:100%;height:100%;border:0}
#hint{padding:10px;color:#888}
</style>
<nav><section id=platform><h3 style="margin:4px 0 8px">Work platform</h3><div id=platform-items></div><div id=platform-actions></div></section><h3 id=agents style="margin:4px 0 8px">Agents</h3><div id=a></div></nav>
<main><div id=tabs></div><div id=panes><div id=hint>左栏选一个 Agent</div></div></main>
<script>
const MAX_TABS = 8;   // 业主 2026-09-16 定;见上方注释:HTTP/1.1 每源 6 条长连接的硬限制
// 短名 = 三段式 <owner>-<name>-<suffix> 的中间段;取不到就原样显示(T-138,业主裁定命名归他)
function dwShortName(s){var p=String(s).split('-');return p.length===3?p[1]:s}
const tabs = new Map();   // session -> {tab, pane}
let agentSnapshot = new Map(); // session -> online;只描述左栏最新快照,不拥有 tab/pane
function renderPlatform(platform){
  var items=document.getElementById('platform-items'); items.replaceChildren();
  (Array.isArray(platform.items)?platform.items:[]).forEach(function(item){
    var a=document.createElement('a'); a.textContent=item.title; a.href=item.href; a.dataset.itemId=item.id;
    if(item.status==='pending') a.dataset.status='pending';
    items.append(a);
  });
  var actions=document.getElementById('platform-actions'); actions.replaceChildren();
  (Array.isArray(platform.actions)?platform.actions:[]).filter(function(action){return action!=='read'}).forEach(function(action){
    var b=document.createElement('button'); b.type='button'; b.dataset.action=action; b.textContent=action; actions.append(b);
  });
  document.documentElement.dataset.platformRole=platform.role||'';
}
async function refreshPlatform(){
  try{
    var r=await fetch('/api/console/platform',{cache:'no-store'}); if(!r.ok) return;
    renderPlatform(await r.json());
  }catch(_){ /* 平台入口失败不得清空已打开的 Agent tab */ }
}
function activate(session){
  tabs.forEach(function(v,k){
    v.tab.classList.toggle('on', k===session);
    v.pane.style.display = (k===session) ? 'block' : 'none';
  });
  var h=document.getElementById('hint'); if(h) h.style.display='none';
}
function closeTab(session){
  var e=tabs.get(session); if(!e) return;
  e.tab.remove(); e.pane.remove(); tabs.delete(session);   // 🔴 关闭才真正断开那条 SSE
  var last=Array.from(tabs.keys()).pop();
  if(last) activate(last);
  else { var h=document.getElementById('hint'); if(h) h.style.display='block'; }
}
function openTab(session){
  if(tabs.has(session)){ activate(session); return; }      // 已开过 ⇒ 切过去,不重建
  if(tabs.size >= MAX_TABS){
    alert('最多同时打开 ' + MAX_TABS + ' 个 Agent。\n' +
          '每个页面占一条长连接,超过浏览器上限后新页会一直转圈而不报错。\n' +
          '请先关掉一个再打开。');
    return;
  }
  var tab=document.createElement('span'); tab.className='tab'; tab.title=session;
  var label=document.createElement('span'); label.textContent=dwShortName(session);
  var x=document.createElement('span'); x.className='x'; x.textContent='×'; x.title='关闭';
  tab.append(label,x);
  tab.onclick=function(ev){ if(ev.target===x){ ev.stopPropagation(); closeTab(session); } else activate(session); };
  var pane=document.createElement('iframe');
  pane.title='Agent view: '+session;
  pane.src='/view/'+encodeURIComponent(session);
  pane.style.display='none';
  document.getElementById('tabs').append(tab);
  document.getElementById('panes').append(pane);
  tabs.set(session,{tab:tab,pane:pane});
  activate(session);
}
function applyAgentSnapshot(agents){
  agentSnapshot = new Map(agents.map(function(a){return [a.session, !!a.online]}));
  // 🔴 只更新既有 tab 的可用状态。tab/pane 对象与 iframe src 均不得在列表刷新时改变。
  tabs.forEach(function(e,session){
    var online=agentSnapshot.get(session)===true;
    e.tab.classList.toggle('offline', !online);
    e.tab.setAttribute('aria-disabled', online?'false':'true');
    e.pane.style.pointerEvents=online?'':'none';
  });
}
function renderAgentList(agents){
  var nav=document.getElementById('a');
  nav.replaceChildren(); // 只重建左栏;右侧 tabs/panes 不在本函数所有权内
  function mk(a){
    var b=document.createElement('button');
    b.textContent=dwShortName(a.session); b.title=a.session; b.disabled=!a.online;
    b.onclick=function(){ openTab(a.session) };
    return b;
  }
  var on=agents.filter(function(a){return a.online});
  var off=agents.filter(function(a){return !a.online});
  on.forEach(function(a){nav.append(mk(a))});
  if(off.length){
    var d=document.createElement('details');
    var sm=document.createElement('summary'); sm.textContent='离线 ('+off.length+')';
    d.append(sm); off.forEach(function(a){d.append(mk(a))}); nav.append(d);
  }
}
async function refreshAgents(){
  try{
    var r=await fetch('/api/console/agents',{cache:'no-store'});
    if(!r.ok) return;
    var x=await r.json();
    var agents=Array.isArray(x.agents)?x.agents:[];
    renderAgentList(agents);
    applyAgentSnapshot(agents);
  }catch(_){ /* 保留上一份可见快照;短暂请求失败不得把左栏清空 */ }
}
refreshPlatform();
refreshAgents();
setInterval(refreshAgents,15000);
</script>`

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
	// 🔴 本 handler 挂在 RequireConsoleAgent 下，而该守卫在开关关闭时**仍然放行**
	//    （它同时包着 /view/* 这条回滚路径，**不能**改成拒绝）
	//    ⇒ 本 handler 在开关关闭时**确实会被无凭据请求打到**，下面这几行不是多余的。
	// 🔵 合并取 coder56solzzc 版（0f4f8e04）：它比组长版**多检一项 repoOK**
	//    （组长版 `h.Repo.(consoleAuthRepository)` 是**无检查断言**，类型不对会 panic），
	//    且加了「开关关闭 ⇒ 404」，与 RequireConsolePrincipal 的改动一致。
	repo, repoOK := h.Repo.(consoleAuthRepository)
	session, sessionOK := r.Context().Value(consoleSessionContextKey{}).(model.ConsoleSession)
	agent, agentOK := r.Context().Value(consoleAgentContextKey{}).(model.ConsoleAgent)
	if !h.consoleAuthEnabled || !repoOK || !sessionOK || !agentOK {
		http.NotFound(w, r)
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
		// This is an internal trust marker. Strip it before every branch,
		// including the compatibility path where unified auth is disabled.
		r.Header.Del("X-DW-Console-Capability-Verified")
		r.Header.Del("X-DW-Console-Principal")
		permission := "view"
		if r.Method != http.MethodGet {
			permission = "interact"
		}
		h.RequireConsoleAgent(permission, func(w http.ResponseWriter, r *http.Request) {
			if !h.consoleAuthEnabled || r.Method == http.MethodGet || !consoleCapabilityRequired(r.PathValue("rest")) {
				next(w, r)
				return
			}
			repo, repoOK := h.Repo.(consoleAuthRepository)
			agent, agentOK := r.Context().Value(consoleAgentContextKey{}).(model.ConsoleAgent)
			pageID := strings.TrimSpace(r.Header.Get("X-DW-Console-Page"))
			raw := strings.TrimSpace(r.Header.Get("X-DW-Console-Capability"))
			if !repoOK || !agentOK || pageID == "" || raw == "" {
				http.Error(w, "console capability required", http.StatusForbidden)
				return
			}
			capability, err := repo.ResolveConsolePageCapability(r.Context(), hashConsoleToken(raw), agent.ID, pageID, time.Now())
			if err != nil {
				http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
				return
			}
			if capability == nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			r.Header.Set("X-DW-Console-Capability-Verified", "1")
			if session, ok := r.Context().Value(consoleSessionContextKey{}).(model.ConsoleSession); ok {
				r.Header.Set("X-DW-Console-Principal", session.PrincipalID)
			}
			next(w, r)
		})(w, r)
	}
}

func consoleCapabilityRequired(rest string) bool {
	switch strings.Trim(strings.TrimSpace(rest), "/") {
	case "input", "control", "interrupt":
		return true
	default:
		return false
	}
}
