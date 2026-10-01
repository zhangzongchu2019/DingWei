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
	dwversion "github.com/zhangzongchu2019/dingwei/internal/version"
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
	fmt.Fprint(w, renderConsolePage(dwversion.Version, dwversion.Commit))
}

// renderConsolePage 把版本号在【服务端】填进页面。
//
// 🔴 为什么必须服务端渲染、且模板里不许出现版本号字面量:
//
//	这一行的全部用处是回答业主那句「我屏幕上这份是哪一版」。若把版本号硬编码进前端字符串,
//	那么"页面是旧的"时【版本号也是旧的】—— 它会跟着旧页面一起骗人,等于没做。
//	守住这一点的是 TestConsolePageSourceCarriesNoVersionLiteral:模板里出现当前版本号即红。
//
// 🔴 为什么连 commit 一起显示(比业主原话多半步,理由在 internal/version/version.go 里已有实测):
//
//	0.2.0.7 发版锚 e741db4b 上线后,现网自报 version=0.2.0.5 commit=e741db4… ——
//	**commit 是新的、version 是旧的**。同理,同一版本号可以部署两次(修缺陷不抬号),
//	那时只看版本号【分辨不出新旧两份制品】,而这正是本次要消灭的那类来回。
//	commit 未注入时按 version.go 的既有口径只显版本号:宁可不显示,也不显示一个可能是错的来源。
func renderConsolePage(appVersion, commit string) string {
	text := appVersion
	if commit != "" && commit != "unknown" {
		short := commit
		if len(short) > 7 {
			short = short[:7]
		}
		text = appVersion + " · " + short
	}
	return strings.NewReplacer(
		"{{CONSOLE_VERSION_TEXT}}", htmlAttrEscape(text),
		"{{CONSOLE_VERSION}}", htmlAttrEscape(appVersion),
		"{{CONSOLE_COMMIT}}", htmlAttrEscape(commit),
	).Replace(consolePageHTML)
}

// htmlAttrEscape:版本号与 commit 都来自构建期注入(-ldflags -X),不是用户输入,
// 但注入值仍是【外部字符串】,原样拼进 HTML 属性属于无谓的口子,故统一转义。
func htmlAttrEscape(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(v)
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
:root{--row-alt:#e9eef5}body{margin:0;display:grid;grid-template-columns:240px 1fr;height:100vh;font:14px sans-serif}
[hidden]{display:none!important}
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
#tabs .tab .tab-state{color:#9d1b1b;font-size:11px;font-weight:400}
#panes{flex:1;position:relative;min-height:0}
#panes iframe{position:absolute;inset:0;width:100%;height:100%;border:0}
.agent-offline-reason{position:absolute;inset:0;z-index:2;align-items:center;justify-content:center;background:var(--bg);color:#9d1b1b;font-weight:600}
.group-workspace{position:absolute;inset:0;overflow:auto;padding:16px;background:inherit}
.group-workspace label{display:block;margin:8px 0}.group-workspace input,.group-workspace select{min-width:320px;padding:5px}
.group-workspace button{margin:4px;padding:5px 9px}.enroll-editor{border:1px solid #ccd;border-radius:6px;padding:10px;margin:8px 0}#hostedMask{position:fixed;inset:0;background:rgba(0,0,0,.45);display:flex;align-items:center;justify-content:center;z-index:50}#hostedMask[hidden]{display:none}.hosted-mask-box{background:#fff;color:#222;border-radius:8px;padding:18px 24px;min-width:300px}html[data-theme="dark"] .hosted-mask-box{background:#222;color:#eee}.hosted-stage{margin:6px 0}.hosted-stage[data-state="done"]{color:#1a7f37}.hosted-stage[data-state="current"]{font-weight:bold}.hosted-stage[data-state="failed"]{color:#9d1b1b;font-weight:bold}.hosted-stage[data-state="todo"]{opacity:.55}html[data-theme="dark"] .enroll-editor{border-color:#445}.agent-group{border:1px solid #ddd;border-radius:6px;padding:10px;margin:8px 0}
.group-toolbar{display:flex;align-items:center;justify-content:space-between}.group-table{width:100%;table-layout:fixed;border-collapse:collapse}.group-table th,.group-table td{padding:7px;border-bottom:1px solid #ddd;text-align:left}.group-table tbody tr:nth-child(even){background:var(--row-alt)}.group-table th:nth-child(1),.group-table td:nth-child(1){width:28%}.group-table th:nth-child(2),.group-table td:nth-child(2){width:28%}.group-table th:nth-child(3),.group-table td:nth-child(3){width:28%}.group-table th:nth-child(4),.group-table td:nth-child(4){width:16%}.group-row{cursor:pointer}.group-member-row{display:flex;align-items:center;gap:18px;max-width:520px;min-height:34px}.group-member-remove{visibility:hidden}.group-member-row:hover .group-member-remove,.group-member-row:focus-within .group-member-remove{visibility:visible}
.agent-tree-group{margin:5px 0}.agent-tree-group-label{display:block;font-weight:700;padding:4px 6px}.agent-tree-members{padding-left:10px}.agent-tree-agent{width:100%;text-align:left}
.group-row .group-pick{margin-right:auto}.group-feedback{min-height:1.2em;color:#9d1b1b}.group-block{border-radius:7px;padding:12px;margin:10px 0}.group-block-list{background:#f4f7fb}.group-block-detail{background:#f8f5ff}.group-block-add{background:#eef8f1}
#hint{padding:10px;color:#888}
/* 🔴 版本角标(业主 2026-09-24 批「版本号显示 ---- 可以」)。
   立项实证:业主连报五条「改动没生效」,全部源于【他那个页面是部署之前打开的】——
   /console 虽是 no-store,但 no-store 只在【重新请求时】才起作用,旧标签页不会自己去问。
   业主此前没有任何办法判断「我屏幕上这份是哪一版」,组长也只能对新旧制品跑 strings 对照。
   🔴 pointer-events:none 是硬要求:角标永不吞点击 ⇒ 结构上不可能遮挡内容(不靠位置侥幸)。 */
#app-version{position:fixed;right:6px;bottom:4px;z-index:5;pointer-events:none;
  font-size:11px;line-height:1.4;color:#8a8a8a;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
html[data-theme="dark"] #app-version{color:#7c8896}
/* 🔴 日夜主题(0.2.1.7):暗色**只在** [data-theme=dark] 下覆盖,浅色声明一条没改
      ⇒ 业主看惯的浅色观感零变化(git diff 可自证:上面那些规则原样未动)。
      🔵 外壳必须跟着暗:否则"面板是夜里、左栏和 tab 条是白天"的花脸,正是本版要消灭的。
      色值取自 renderer 的 [data-theme=dark] 变量集(renderer-canary-sse.html:26-42),两处观感一致。 */
html[data-theme="dark"]{color-scheme:dark;--row-alt:#242a31}
html[data-theme="dark"] body{background:#111316;color:#eef2f6}
html[data-theme="dark"] nav{border-right-color:#38414c}
html[data-theme="dark"] nav button{background:#1b1f24;color:#eef2f6;border:1px solid #38414c}
html[data-theme="dark"] nav button:disabled{color:#a8b0bb}
html[data-theme="dark"] #platform-items a{background:#1b1f24;border-color:#38414c;color:#6cb6ff}
html[data-theme="dark"] #platform-actions button{background:#1b1f24;color:#eef2f6;border:1px solid #38414c}
html[data-theme="dark"] #tabs{border-bottom-color:#38414c}
html[data-theme="dark"] #tabs .tab{background:#1b1f24;border-color:#38414c;color:#eef2f6}
html[data-theme="dark"] #tabs .tab.on{background:#242a31;border-color:#6cb6ff}
html[data-theme="dark"] #tabs .tab .x{color:#a8b0bb}
html[data-theme="dark"] #tabs .tab .x:hover{color:#ffb4a9}
html[data-theme="dark"] #hint{color:#a8b0bb}
html[data-theme="dark"] details,html[data-theme="dark"] summary{color:#a8b0bb}
html[data-theme="dark"] .group-block-list{background:#1b2530}html[data-theme="dark"] .group-block-detail{background:#252033}html[data-theme="dark"] .group-block-add{background:#182a22}
/* 🔴 nav button 那条是 display:block;width:100% ⇒ 不覆盖的话 ◐ 会变成一整条横杠 */
#themeBtn{display:inline-block;width:auto;margin:0;padding:2px 8px;flex:0 0 auto;font-size:13px;line-height:1.5}
</style>
<nav><section id=platform><h3 style="margin:4px 0 8px">Work platform</h3><div id=platform-status role=status></div><button id=retryPlatform type=button hidden>重试</button><div id=platform-items></div><div id=platform-actions></div></section><div id=agents-row style="display:flex;align-items:center;justify-content:space-between;margin:4px 0 8px"><h3 id=agents style="margin:0">Agents</h3><span><button id=agentNavMode type=button title="切换编组树/纯 Agent 列表">列表</button><button id=themeBtn type=button title="切换黑夜/白天（整个控制台）" aria-label="切换黑夜或白天主题">◐</button></span></div><div id=agents-status role=status></div><button id=retryAgents type=button hidden>重试</button><div id=a></div></nav>
<main><div id=tabs></div><div id=panes><div id=hint>左栏选一个 Agent</div></div></main>
<div id=app-version data-version="{{CONSOLE_VERSION}}" data-commit="{{CONSOLE_COMMIT}}">{{CONSOLE_VERSION_TEXT}}</div>
<script>
// 会话守卫(新平台 N3,第一期):接口回 401 {error:unauthenticated} ⇒ 回登录页;回 403 {error:must_change_password} ⇒ 登录页的改密表单。
// 旧平台的接口回的是纯文本、不是这种 JSON,所以对旧平台不起作用。window 不存在(测试把脚本抠出来在无 window 环境里求值)时跳过。
(function(){
  if(typeof window==='undefined'||typeof window.fetch!=='function')return;
  var raw=window.fetch.bind(window);
  window.fetch=async function(){
    var r=await raw.apply(null,arguments);
    if(r.status===401||r.status===403){
      try{
        var d=await r.clone().json();
        if(d&&d.error==='unauthenticated')location.assign('/console/login');
        else if(d&&d.error==='must_change_password')location.assign('/console/login#change');
      }catch(_){}
    }
    return r;
  };
})();
const MAX_TABS = 8;   // 业主 2026-09-16 定;见上方注释:HTTP/1.1 每源 6 条长连接的硬限制
// 短名 = 三段式 <owner>-<name>-<suffix> 的中间段;取不到就原样显示(T-138,业主裁定命名归他)
function dwShortName(s){var p=String(s).split('-');return p.length===3?p[1]:s}
const tabs = new Map();   // session -> {tab, pane}
let agentSnapshot = new Map(); // session -> online;只描述左栏最新快照,不拥有 tab/pane
// ── 🔴 日夜主题:全局(整个控制台 ＋ 所有已开 pane)＋ 持久化(跨 tab、跨重启)── 0.2.1.7 ──
// 业主原话:「◐ 应该要**提出来,放在整个 Agent 列表**,而不是某一个 tab」
// ⇒ 开关的**归属**在 console(见上面 nav 里的 #themeBtn),renderer 侧只负责**接收并应用**。
//
// 传输用 BroadcastChannel(与 renderer 的 page-identity 同一手法,见其 setupPageIdentityChannel):
//   它一次覆盖两类"看不见就漏掉"的对象 ——
//   ① **隐藏的 pane**:切 tab 是 display:none,iframe **不重载**(实测:里面打的标记切完仍在)
//      ⇒ 靠"显示时刷新"永远救不到它,必须主动送过去;
//   ② **其他浏览器 tab**:同源即同频道。
// 🔴 写入方必须【自己立刻 apply】:BroadcastChannel 与 storage 事件都**不会**在写入方自己那份文档上触发
//   (已由 view-service/console-theme-global-regression.js 的 T3 用"同一个任务内就变暗"钉住)。
// 🔴 持久化用 localStorage,**不用 sessionStorage**:后者不跨 tab ⇒ 不满足业主"含持久化"
//   (闸 T4 专门有一条"新开一个 tab 必须仍是暗"的判别腿)。
// 🔵 本段全部特性探测 ＋ try/catch,不是防御性堆砌,有两个真实理由:
//   · 同一份源码会被 view-service/renderer-t145-tabs-regression.js 抽出去,放进**极简 DOM 替身**
//     (只有 createElement/getElementById;**没有** window、localStorage、documentElement、
//      querySelectorAll、addEventListener)求值 ⇒ 裸写这些名字会让那件八条**全红**,
//      而报错看起来像"tab 逻辑坏了"(错误消息指错方向)。
//   · 隐私模式下取用 localStorage **即抛**;那不该让控制台白屏。
var DW_THEME_KEY='dw-theme';
var dwThemeChannel=null;
function dwThemeStore(){
  try{ return (typeof window!=='undefined'&&window.localStorage)?window.localStorage:null; }catch(_){ return null }
}
function dwThemeRead(){
  var s=dwThemeStore(); if(!s) return null;
  try{ var v=s.getItem(DW_THEME_KEY); return (v==='dark'||v==='light')?v:null; }catch(_){ return null }
}
function dwThemeWrite(v){
  var s=dwThemeStore(); if(!s) return;
  try{ if(s.getItem(DW_THEME_KEY)!==v) s.setItem(DW_THEME_KEY,v); }catch(_){ /* 存不下不影响本次会话 */ }
}
function dwThemeShell(v){
  try{
    if(typeof document==='undefined') return;
    if(document.documentElement) document.documentElement.dataset.theme=v;   // CSS 走 html[data-theme=dark]
    if(document.body) document.body.dataset.theme=v;                          // 兜底:选择器改了也还有线索
  }catch(_){}
}
function dwThemePublish(v){
  try{ if(dwThemeChannel) dwThemeChannel.postMessage({type:'theme',theme:v}); }catch(_){}
}
function dwThemeApply(v,spread){
  if(v!=='dark'&&v!=='light') return;
  dwThemeShell(v);                                  // 🔴 自己先变:等不到自己的事件
  dwThemeWrite(v);
  if(spread!==false) dwThemePublish(v);             // 收到广播时传 false ⇒ 不成回环
}
function dwThemeToggle(){
  var cur='light';
  try{ if(document.documentElement&&document.documentElement.dataset.theme==='dark') cur='dark'; }catch(_){}
  dwThemeApply(cur==='dark'?'light':'dark');
}
function dwThemeInit(){
  // 🔴 四段**各自** try/catch,不共用一个。理由不是风格:共用一个的话,
  //    频道建不起来(老浏览器 / 被策略禁掉)会**连带跳过**后面的 storage 退路
  //    ⇒ "一条路坏了 ⇒ 两条路都没了",而报错只会指向先坏的那条(错误消息指错方向)。
  try{ dwThemeShell(dwThemeRead()==='dark'?'dark':'light'); }catch(_){}   // 加载时读:重载/新开 tab 都得跟得上
  try{
    if(typeof document!=='undefined'&&document.getElementById){
      var btn=document.getElementById('themeBtn');
      if(btn&&btn.addEventListener) btn.addEventListener('click',dwThemeToggle);
    }
  }catch(_){}
  try{
    if(typeof window!=='undefined'&&('BroadcastChannel' in window)&&!dwThemeChannel){
      dwThemeChannel=new BroadcastChannel(DW_THEME_KEY);
      dwThemeChannel.onmessage=function(e){
        var v=e&&e.data&&e.data.theme;
        if(v!=='dark'&&v!=='light') return;
        dwThemeApply(v,false);   // 收到广播只应用,不再广播 ⇒ 不成回环
      };
    }
  }catch(_){ dwThemeChannel=null; }
  try{
    if(typeof window!=='undefined'&&window.addEventListener){
      // 别的文档绕过频道直接写了存储(例如 renderer 侧先落盘)也要跟随
      window.addEventListener('storage',function(e){
        if(!e||e.key!==DW_THEME_KEY) return;
        if(e.newValue==='dark'||e.newValue==='light') dwThemeApply(e.newValue,false);
      });
    }
  }catch(_){}
}
function renderPlatform(platform){
  var items=document.getElementById('platform-items'); items.replaceChildren();
  (Array.isArray(platform.items)?platform.items:[]).forEach(function(item){
    var a=document.createElement('a'); a.textContent=item.title; a.href=item.href; a.dataset.itemId=item.id;
    if(item.target) a.target=item.target;
    if(item.id==='agent-groups') a.onclick=function(ev){ev.preventDefault();openGroupWorkspace();};
    if(item.id==='platform-admin') a.onclick=function(ev){ev.preventDefault();openPlatformAdminWorkspace();};
    if(item.id==='specs') a.onclick=function(ev){ev.preventDefault();openSpecsWorkspace();};
    if(item.id==='task-board') a.onclick=function(ev){ev.preventDefault();openTaskBoardWorkspace();};
    if(item.status==='pending') a.dataset.status='pending';
    items.append(a);
  });
  var actions=document.getElementById('platform-actions'); actions.replaceChildren(); // B1:未接线五按钮退场;服务端权限动词不动
  document.documentElement.dataset.platformRole=platform.role||'';
}
async function refreshPlatform(){
  var status=document.getElementById('platform-status'),retry=document.getElementById('retryPlatform');
  status.textContent='工作台加载中…';retry.hidden=true;
  var controller=new AbortController(),timer=setTimeout(function(){controller.abort()},consoleFetchTimeoutMs);
  try{
    var r=await fetch('/api/console/platform',{cache:'no-store',signal:controller.signal});
    if(!r.ok){var detail=(await r.text()).trim();throw new Error('HTTP '+r.status+(detail?': '+detail:''));}
    var data=await r.json();if(!data||!Array.isArray(data.items))throw new Error('响应格式无效');
    renderPlatform(data);status.textContent=document.getElementById('platform-items').childElementCount?'':'没有可用工作台入口';retry.hidden=true;
  }catch(err){renderPlatform({items:[],role:document.documentElement.dataset.platformRole||''});status.textContent='工作台加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);retry.hidden=false;
  }finally{clearTimeout(timer);}
}
function activate(session){
  tabs.forEach(function(v,k){
    v.tab.classList.toggle('on', k===session);
    v.pane.style.display = (k===session) ? 'block' : 'none';
    if(v.offlineReason)v.offlineReason.style.display=(k===session&&!v.offlineReason.hidden)?'flex':'none';
  });
  var h=document.getElementById('hint'); if(h) h.style.display='none';
}
function closeTab(session){
  var e=tabs.get(session); if(!e) return;
  e.tab.remove(); e.pane.remove(); if(e.offlineReason)e.offlineReason.remove(); tabs.delete(session);   // 🔴 关闭才真正断开那条 SSE
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
  var offlineReason=document.createElement('div');offlineReason.className='agent-offline-reason';offlineReason.hidden=true;offlineReason.textContent='该 Agent 已离线，暂不可交互';
  document.getElementById('tabs').append(tab);
  document.getElementById('panes').append(pane,offlineReason);
  tabs.set(session,{tab:tab,pane:pane,offlineReason:offlineReason});
  activate(session);
}
function applyAgentSnapshot(agents){
  agentSnapshot = new Map(agents.map(function(a){return [a.full_name, !!a.online]}));   // 页签与成员一律按 full_name(名称.业主简称)
  // 🔴 只更新既有 tab 的可用状态。tab/pane 对象与 iframe src 均不得在列表刷新时改变。
  tabs.forEach(function(e,session){
    if(String(session).startsWith('__')) return; // Specs/编组是平台 tab,不是 Agent session
    var online=agentSnapshot.get(session)===true;
    e.tab.classList.toggle('offline', !online);
    e.tab.setAttribute('aria-disabled', online?'false':'true');
    var state=e.tab.querySelector('.tab-state');if(!state){state=document.createElement('span');state.className='tab-state';e.tab.insertBefore(state,e.tab.querySelector('.x'));}
    state.textContent=online?'':'离线，暂不可交互';state.hidden=online;
    e.tab.title=online?session:(session+' · Agent 离线，暂不可交互');
    e.offlineReason.hidden=online;e.offlineReason.style.display=online||!e.tab.classList.contains('on')?'none':'flex';
    e.pane.style.pointerEvents=online?'':'none';
  });
}
var agentListSnapshot=[],agentNavMode='tree',agentTreeAllowDuplicates=true;
function renderAgentList(agents){
  var nav=document.getElementById('a');
  nav.replaceChildren(); // 只重建左栏;右侧 tabs/panes 不在本函数所有权内
  function mk(a){
    var b=document.createElement('button');
    b.textContent=a.name||a.full_name; b.title=a.full_name; b.disabled=!a.online;
    b.onclick=function(){ openTab(a.full_name) };
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
// 编组成员的名字口径:full_name(名称.业主简称)。🔵 过渡:编组接口(旧形状)目前还只给 session_name,G3 后续让它也返回 full_name;
//    在那之前 memberName 退回 session_name,这样编组面板与现有 Hub 照常工作。编组接口迁完后删掉退回分支。
function memberName(member){return member.full_name||member.session_name;}
function memberShort(member){return member.full_name?String(member.full_name).split('.')[0]:dwShortName(member.session_name);}
function agentTreeMemberLabel(member,ownedIDs){return ownedIDs.has(member.agent_id)?memberShort(member):memberName(member);}
function agentTreeMemberOnline(member){var fresh=agentListSnapshot.find(function(agent){return agent.full_name===memberName(member);});return fresh?fresh.online===true:member.online===true;}
function agentTreeMemberDisabled(member){return !agentTreeMemberOnline(member);}
function agentTreeCanOpen(member,ownedIDs){return ownedIDs.has(member.agent_id)&&agentTreeMemberOnline(member);}
function renderAgentNav(){
  if(agentNavMode==='flat'||!groupSnapshot.groups.length){renderAgentList(agentListSnapshot);return;}
  var nav=document.getElementById('a'),ownedIDs=new Set((groupSnapshot.owned_agents||[]).map(function(a){return a.id;})),seen=new Set();nav.replaceChildren();
  (groupSnapshot.groups||[]).forEach(function(group){var section=document.createElement('section');section.className='agent-tree-group';section.dataset.groupCode=group.code;var title=document.createElement('span');title.className='agent-tree-group-label';title.textContent=group.short_name||'历史组';var members=document.createElement('div');members.className='agent-tree-members';(group.members||[]).forEach(function(member){if(!agentTreeAllowDuplicates&&seen.has(member.agent_id))return;seen.add(member.agent_id);var b=document.createElement('button');b.type='button';b.className='agent-tree-agent';b.dataset.session=memberName(member);b.title=memberName(member);b.textContent=agentTreeMemberLabel(member,ownedIDs);b.disabled=agentTreeMemberDisabled(member);b.setAttribute('aria-disabled',b.disabled?'true':'false');if(agentTreeCanOpen(member,ownedIDs))b.onclick=function(){openTab(memberName(member));};members.append(b);});section.append(title,members);nav.append(section);});
}
function agentNavSetMode(mode){agentNavMode=mode;document.getElementById('agentNavMode').textContent=mode==='tree'?'列表':'编组';renderAgentNav();}
async function refreshAgents(){
  var status=document.getElementById('agents-status'),retry=document.getElementById('retryAgents');
  status.textContent='Agent 列表加载中…';retry.hidden=true;
  var controller=new AbortController(),timer=setTimeout(function(){controller.abort()},consoleFetchTimeoutMs);
  try{
    var r=await fetch('/api/console/agents',{cache:'no-store',signal:controller.signal});
    if(!r.ok){var detail=(await r.text()).trim();throw new Error('HTTP '+r.status+(detail?': '+detail:''));}
    var x=await r.json();
    if(!x||!Array.isArray(x.agents))throw new Error('响应格式无效');
    var agents=x.agents;
    agentListSnapshot=agents;renderAgentNav();if(groupMode)renderGroups();
    applyAgentSnapshot(agents);
    status.textContent=agents.length?'':'没有自己的 Agent';retry.hidden=true;
  }catch(err){agentListSnapshot=[];renderAgentList([]);status.textContent='Agent 列表加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);retry.hidden=false;
  }finally{clearTimeout(timer);}
}
var consoleFetchTimeoutMs=8000,groupMode=false,groupRequestGeneration=0,groupFetchTimeoutMs=8000,groupLeaderSelection='',groupPanelMode='',groupSnapshot={groups:[],owned_agents:[]};
function groupStatus(text){var status=document.getElementById('group-status');if(status)status.textContent=text;}
function setGroupSelectState(text,disabled){['groupLeader','groupJoinAgent'].forEach(function(id){var select=document.getElementById(id);if(!select)return;select.replaceChildren();var option=document.createElement('option');option.value='';option.textContent=text;select.append(option);select.disabled=disabled;});}
function groupFeedback(id,text){var e=document.getElementById(id);if(e)e.textContent=text||'';}
function setGroupPanelMode(mode){var selected=(groupSnapshot.groups||[]).some(function(group){return group.code===window.selectedGroupCode;});groupPanelMode=mode==='detail'&&!selected?'':mode;document.getElementById('groupDetail').hidden=groupPanelMode!=='detail';document.getElementById('groupAddPanel').hidden=groupPanelMode!=='add';document.getElementById('groupAddOpen').textContent=groupPanelMode==='add'?'取消添加':'⊕ 添加编组';if(groupPanelMode==='add')document.getElementById('groupShortName').focus();}
function groupAction(path,method,body,feedback){return fetch(path,{method:method,headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined}).then(async function(r){if(!r.ok){groupFeedback(feedback||'group-status',(await r.text()).trim()||('HTTP '+r.status));return false;}await refreshGroups('action');groupFeedback(feedback||'group-status','操作成功');return true;}).catch(function(e){groupFeedback(feedback||'group-status','请求失败: '+String(e&&e.message||e));return false;});}
function groupShortName(s){var p=String(s).split('-');return p.length===3?p[1]:s;}
async function refreshGroups(source){var generation=++groupRequestGeneration;if(groupMode){var leader=document.getElementById('groupLeader');if(leader&&leader.value)groupLeaderSelection=leader.value;setGroupSelectState('加载中…',true);groupStatus('Agent 列表加载中…');}var controller=new AbortController();var timer=setTimeout(function(){controller.abort()},groupFetchTimeoutMs);try{var r=await fetch('/api/console/agent-groups',{cache:'no-store',signal:controller.signal});if(!r.ok){var detail=(await r.text()).trim();throw new Error('HTTP '+r.status+(detail?': '+detail:''));}var data=await r.json();if(!data||!Array.isArray(data.owned_agents)||!Array.isArray(data.groups))throw new Error('响应格式无效');if(generation!==groupRequestGeneration)return null;groupSnapshot=data;renderAgentNav();if(groupMode){renderGroups();if(!data.owned_agents.length){setGroupSelectState('没有自己的 Agent',true);groupStatus('没有自己的 Agent，无法建组或加入');}else if(source==='retry'){groupFeedback('groupRetryFeedback','已刷新 · '+data.groups.length+' 个编组');groupStatus('');}else groupStatus('');}return data;}catch(err){if(groupMode&&generation===groupRequestGeneration){setGroupSelectState('加载失败',true);var message='编组加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);groupStatus(message);if(source==='retry')groupFeedback('groupRetryFeedback',message);}return null;}finally{clearTimeout(timer);}}
// G3 · Agent 管理 Tab 的两块新增区(自备 Agent 审核 / Agent 列表)。
// 接口按 runbooks/DESIGN-G3-ENROLL-AUTH-API-20261001.md v1 第三节(定稿);错误一律是 JSON {"error":机器码,...},
//    页面只认机器码、自己写中文,不显示 Hub 的原文(设计件第四节)。
//    不重写 refreshGroups/renderGroups —— 它们的三态文案、AbortController、generation 守卫由 group 回归守着。
//    本块自带同款守卫:加载中/空/失败三态、超时、过期应答丢弃;名称有效性、重名、核码一律由 Hub 判,前端不预判。
// 自备审核区开关(组长裁定 2026-10-01:托管先行,自备封存,第五期再打开)。关闭时该区块不渲染、不发任何请求。
var FEATURE_SELFHOST_REVIEW=false;
var ENROLL_API={
  list:'/api/console/enroll/applications',
  approve:function(id){return '/api/console/enroll/applications/'+encodeURIComponent(id)+'/approve';},
  reject:function(id){return '/api/console/enroll/applications/'+encodeURIComponent(id)+'/reject';},
  revoke:function(id){return '/api/console/agents/'+encodeURIComponent(id)+'/revoke';},
  agents:'/api/console/agents'   // 设计件 3.2 新形状:{agents:[{agent_id,name,full_name,origin,group_code,online}]}
};
var ENROLL_GONE=['expired','not_pending','not_found','superseded','rejected'];
var enrollGeneration=0,enrollSnapshot=[],enrollOpenId='';
function enrollFeedback(id,text){var e=document.getElementById(id);if(e)e.textContent=text||'';}
function enrollTime(v){var d=new Date(v);return isNaN(d.getTime())?String(v||''):d.toLocaleString('zh-CN',{hour12:false});}
var CONSOLE_ERRORS={
  name_taken:'该名称已被你名下的 Agent 使用（不区分大小写），请换一个',
  name_invalid:'名称只能用大小写字母和数字，2～32 位，且不能是渠道保留字',
  group_required:'请选择所属编组',
  expired:'申请已超过 24 小时，已作废',
  not_pending:'该申请已被处理（已批准、已拒绝，或同设备的另一条已被批准）',
  not_found:'申请不存在',
  superseded:'同一设备的另一条申请已被批复，本条已作废',rejected:'该申请已被拒绝',
  not_owner:'这不是你名下的申请或 Agent',forbidden:'这不是你名下的 Agent',
  hosted_agent:'托管 Agent 请用托管操作（停用、销毁），不在这里吊销',origin_hosted:'托管 Agent 请用托管操作（停用、销毁），不在这里吊销',
  unauthenticated:'登录已失效，请重新登录',origin_mismatch:'请求来源不符，请刷新页面',
  combo_incompatible:'所选 CLI、LLM 和账号类型不兼容',combo_not_verified:'这个 CLI 与 LLM 的组合还没有验通，暂不能创建',forbidden_role:'你的角色不足以执行这个操作',plan_exceeds_quota:'所选资源套餐超出剩余额度',
  quota_exceeded:'配额不足，请联系管理员调整配额',node_no_capacity:'没有节点有足够的剩余资源',account_not_allowed:'你不能使用这个 LLM 账号',
  node_offline:'该 Agent 所在节点离线，操作无法执行',invalid_state:'该 Agent 当前状态不允许这个操作',confirm_required:'这个操作需要先确认',
  upgrade_failed_rolled_back:'升级失败，已自动回滚到原版本',
  user_exists:'该用户 ID 或简称已被使用，请换一个',bad_request:'填写的内容不符合规则：用户 ID 2～32 位字母数字，简称 2～16 位字母数字',
  must_change_password:'请先修改初始密码，再使用平台管理',internal:'服务端出错了，请稍后再试'
};
var HOSTED_FAIL_REASONS={
  queue_timeout:'排队超时（超过 10 分钟）',pull_timeout:'拉取镜像超时（超过 30 分钟）',start_timeout:'启动超时（超过 5 分钟）',
  image_pull_failed:'拉取镜像失败',start_failed:'启动失败',credential_inject_failed:'凭据写入失败',node_offline:'节点离线'
};
function consoleErrorText(status,data){
  var code=data&&typeof data.error==='string'?data.error:'';
  if(code==='pairing_mismatch'){
    if(data&&data.rejected===true)return '错误次数过多，该申请已自动拒绝';
    var left=data&&Number.isFinite(data.remaining)?data.remaining:null;
    return '配对码不符'+(left===null?'':'（还可再试 '+left+' 次）');
  }
  if(code==='rate_limited'){var wait=data&&Number.isFinite(data.retry_after_s)?data.retry_after_s:null;return '操作过于频繁'+(wait===null?'，请稍后再试':'，请 '+wait+' 秒后再试');}
  if(code==='quota_exceeded'&&data&&data.remaining&&typeof data.remaining==='object'){   // 被触发的维度剩余为 0:按维度给具体文案
    var DIMS=[['agents','Agent 数量'],['vcpu','CPU'],['mem_gb','内存'],['disk_gb','磁盘']],used=[];
    DIMS.forEach(function(d){if(Number.isFinite(data.remaining[d[0]])&&data.remaining[d[0]]<=0)used.push(d[1]);});   // 契约把负数归 0;<=0 是组长采纳的防御写法;没有用完的维度时落到中性回退文案
    if(used.length)return '配额不足：'+used.join('、')+'已用完，请换小一点的规格或联系管理员调整配额';
  }
  return CONSOLE_ERRORS[code]||('操作失败（HTTP '+status+(code?'：'+code:'')+'）');
}
async function consolePost(path,body){
  try{
    var r=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});
    var data=null;try{data=await r.json();}catch(_){}
    if(r.ok)return {ok:true,text:'',data:data||{}};
    return {ok:false,status:r.status,text:consoleErrorText(r.status,data),code:data&&data.error||'',data:data||{}};
  }catch(err){return {ok:false,status:0,text:'请求失败: '+String(err&&err.message||err),code:''};}
}
function enrollCloseEditor(){
  enrollOpenId='';var ed=document.getElementById('enrollEditor');if(ed)ed.hidden=true;
  ['enrollCode','enrollName'].forEach(function(id){var i=document.getElementById(id);if(i)i.value='';});
}
function enrollFillGroups(){
  var sel=document.getElementById('enrollGroup');if(!sel)return;var keep=sel.value;sel.replaceChildren();
  var own=(groupSnapshot.groups||[]).filter(function(g){return g.can_manage;});
  if(!own.length){var none=document.createElement('option');none.value='';none.textContent='（暂无可选编组,请先添加编组）';sel.append(none);}
  own.forEach(function(g){var o=document.createElement('option');o.value=g.code;o.textContent=g.short_name||'历史组';sel.append(o);});
  if(keep&&own.some(function(g){return g.code===keep;}))sel.value=keep;
}
function enrollOpenEditor(app){
  enrollOpenId=app.application_id;var ed=document.getElementById('enrollEditor');if(!ed)return;
  document.getElementById('enrollEditorTitle').textContent='审核申请 · 来源 '+(app.source_ip||'未知')+' · '+enrollTime(app.requested_at);
  enrollFillGroups();enrollFeedback('enrollFeedback','');ed.hidden=false;
  document.getElementById('enrollCode').focus();
}
function renderEnroll(){
  var body=document.getElementById('enrollItems'),empty=document.getElementById('enrollEmpty');if(!body||!empty)return;
  body.replaceChildren();
  enrollSnapshot.forEach(function(app){
    var row=document.createElement('tr');row.className='enroll-row';row.dataset.applicationId=app.application_id;
    var fp=String(app.device_fingerprint||'');
    [app.source_ip||'',fp.slice(0,12),app.client_version||'',enrollTime(app.requested_at)].forEach(function(v,i){var td=document.createElement('td');td.textContent=v;if(i===1){td.title=fp;if(app.same_device_count>1){var same=document.createElement('span');same.className='enroll-same-device';same.textContent=' · 同一设备重复申请（共 '+app.same_device_count+' 条）';td.append(same);}}row.append(td);});
    var action=document.createElement('td'),open=document.createElement('button');open.type='button';open.className='enroll-open';open.textContent='审核';
    open.onclick=function(){enrollOpenEditor(app);};action.append(open);row.append(action);body.append(row);
  });
  empty.hidden=enrollSnapshot.length>0;
  if(enrollOpenId&&!enrollSnapshot.some(function(a){return a.application_id===enrollOpenId;})){enrollCloseEditor();enrollFeedback('enrollNotice','该申请已不在待审核列表(可能已处理,或同一设备的另一条已被批复)');}
}
async function refreshEnroll(){
  var generation=++enrollGeneration,status=document.getElementById('enrollStatus'),retry=document.getElementById('enrollRetry');
  if(!status||!retry)return null;
  status.textContent='自备 Agent 申请加载中…';retry.hidden=true;
  var controller=new AbortController(),timer=setTimeout(function(){controller.abort()},groupFetchTimeoutMs);
  try{
    var r=await fetch(ENROLL_API.list,{cache:'no-store',signal:controller.signal});
    if(!r.ok){var detail=(await r.text()).trim();throw new Error('HTTP '+r.status+(detail?': '+detail:''));}
    var data=await r.json();if(!data||!Array.isArray(data.applications))throw new Error('响应格式无效');
    if(generation!==enrollGeneration)return null;
    enrollSnapshot=data.applications;renderEnroll();status.textContent='';return data;
  }catch(err){
    if(generation===enrollGeneration){enrollSnapshot=[];renderEnroll();document.getElementById('enrollEmpty').hidden=true;status.textContent='自备 Agent 申请加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);retry.hidden=false;}
    return null;
  }finally{clearTimeout(timer);}
}
function enrollBusy(on){['enrollApprove','enrollReject'].forEach(function(id){var b=document.getElementById(id);if(b)b.disabled=on;});}
async function enrollApprove(){
  var code=document.getElementById('enrollCode').value.trim(),name=document.getElementById('enrollName').value.trim(),group=document.getElementById('enrollGroup').value,openedId=enrollOpenId;
  if(!code||!name||!group){enrollFeedback('enrollFeedback','请填写客户端屏幕上的配对码、Agent 名称,并选择所属编组');return;}
  enrollBusy(true);var res=await consolePost(ENROLL_API.approve(openedId),{pairing_code:code,agent_name:name,group_code:group});enrollBusy(false);
  if(!res.ok){var gone=ENROLL_GONE.indexOf(res.code)>=0||res.status===410||(res.code==='pairing_mismatch'&&res.data.rejected===true);if(!gone){enrollFeedback('enrollFeedback',res.text);return;}enrollCloseEditor();enrollFeedback('enrollNotice',res.text);refreshEnroll();return;}   // 申请保留、输入保留:业主改名/改码后可再点通过
  enrollCloseEditor();enrollFeedback('enrollNotice','');refreshEnroll();refreshRoster();refreshGroups('enroll');refreshAgents();
}
async function enrollReject(){
  if(!confirm('拒绝即踢走：该申请将作废,客户端需重新申请。确认拒绝？'))return;
  enrollBusy(true);var res=await consolePost(ENROLL_API.reject(enrollOpenId));enrollBusy(false);
  if(!res.ok){if(ENROLL_GONE.indexOf(res.code)<0&&res.status!==410){enrollFeedback('enrollFeedback',res.text);return;}enrollCloseEditor();enrollFeedback('enrollNotice',res.text);refreshEnroll();return;}
  enrollCloseEditor();enrollFeedback('enrollNotice','');refreshEnroll();
}
var rosterAgents=[],rosterGeneration=0;
async function refreshRoster(){
  var generation=++rosterGeneration;
  var controller=new AbortController(),timer=setTimeout(function(){controller.abort()},groupFetchTimeoutMs);
  try{
    var r=await fetch(ENROLL_API.agents,{cache:'no-store',signal:controller.signal});
    if(!r.ok){var detail=(await r.text()).trim();throw new Error('HTTP '+r.status+(detail?': '+detail:''));}
    var d=await r.json();if(!d||!Array.isArray(d.agents))throw new Error('响应格式无效');
    if(generation!==rosterGeneration)return;
    rosterAgents=d.agents.filter(function(a){return a&&a.agent_id;});renderRoster();enrollFeedback('rosterStatus','');
  }catch(err){
    if(generation===rosterGeneration)enrollFeedback('rosterStatus','Agent 列表加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err));
  }finally{clearTimeout(timer);}
}
function renderRoster(){
  var body=document.getElementById('rosterItems'),empty=document.getElementById('rosterEmpty');if(!body||!empty)return;
  body.replaceChildren();
  rosterAgents.forEach(function(a){
    var row=document.createElement('tr');row.className='roster-row';row.dataset.agentId=a.agent_id;
    var name=document.createElement('td');name.textContent=a.name||'';name.title=a.full_name||'';
    var hs=a.origin==='hosted'?Object.assign({online:a.online},a.hosted||{},a.state==='suspended'?{state:'disabled'}:{}):null,st=document.createElement('td');   // 「是否停用」以顶层 state 为准(G3 口径):suspended ⇒ 已停用,不论有没有 hosted 子对象,也不论它说 running;hosted.state 只补运行细节
       // 托管生命周期在行里的 hosted 子对象(契约 v2:过渡形态)
    st.textContent=hs?hostedStateText(hs):(a.state==='suspended'?'已停用':(a.online?'在线':'离线'));
    var action=document.createElement('td'),revoke=document.createElement('button');revoke.type='button';revoke.className='roster-revoke';revoke.textContent='吊销';revoke.hidden=a.origin==='hosted';
    if(hs)hostedOpButtons(a,hs).forEach(function(b){action.append(b);});
    revoke.onclick=async function(){
      if(!confirm('吊销 '+a.name+'：该 Agent 将在 5 秒内断开,凭据作废;重装后需重新申请。确认吊销？'))return;
      revoke.disabled=true;var res=await consolePost(ENROLL_API.revoke(a.agent_id));revoke.disabled=false;
      if(!res.ok){enrollFeedback('rosterFeedback',res.text);return;}
      enrollFeedback('rosterFeedback',res.data&&res.data.already_revoked?'已吊销':'');refreshRoster();refreshGroups('revoke');refreshAgents();
    };
    action.append(revoke);row.append(name,st,action);body.append(row);
  });
  empty.hidden=rosterAgents.length>0;
}
// G3 · 托管 Agent 生成区、进度蒙版、托管生命周期操作(业主 2026-10-01 改序:托管先行)。
// 接口按 G4 契约 v2(runbooks/CONTRACT-G4-CONSOLE-API-DRAFT-20261001.md,草稿待冻结),全部集中在 HOSTED_API;托管行的生命周期状态
//    在 GET /api/console/agents 每行的 hosted 子对象里 {node_name,state,upgradable}(组长裁定:不另开读接口);Hub 掌握事实:
//    CLI×LLM 兼容与验通、套餐是否超额、节点可用与否都由应答给出,页面只据此置灰,不自己判定。
//    密钥永不出现:账号只显示名称与后四位。错误一律机器码,页面写中文。
var HOSTED_API={
  options:'/api/console/hosted/options',
  create:'/api/console/hosted/agents',
  job:function(id){return '/api/console/hosted/jobs/'+encodeURIComponent(id);},
  op:function(id,op){return '/api/console/hosted/agents/'+encodeURIComponent(id)+'/'+op;}
};
var HOSTED_STAGES=[['queued','排队'],['pulling','拉镜像'],['starting','启动'],['online','上线']];
var hostedOptions=null,hostedOptionsGeneration=0,hostedJobTimer=null,hostedPollMs=2000;
function hostedStateText(hs){
  if(!hs.state)return hs.online?'在线':'离线';   // 后端尚未给生命周期状态时,只显示在线与否,不编造状态
  var t={running:'运行中',disabled:'已停用',abnormal:'异常',busy:'处理中…'}[hs.state]||hs.state;
  return t+(hs.upgradable&&hs.state!=='busy'?'（可升级）':'');
}
function hostedFill(id,items,placeholder){
  var sel=document.getElementById(id);if(!sel)return;var keep=sel.value;sel.replaceChildren();
  var ph=document.createElement('option');ph.value='';ph.textContent=placeholder;sel.append(ph);
  items.forEach(function(it){var o=document.createElement('option');o.value=it.value;o.textContent=it.text;o.disabled=!!it.disabled;sel.append(o);});
  if(keep&&items.some(function(it){return it.value===keep&&!it.disabled;}))sel.value=keep;
}
function renderHostedForm(){
  var opts=hostedOptions;if(!opts)return;
  var combos=opts.combos||[],clis=[];combos.forEach(function(c){if(clis.indexOf(c.cli)<0)clis.push(c.cli);});
  hostedFill('hostedNode',(opts.nodes||[]).map(function(n){return {value:n.id,text:n.name+'（剩余 '+n.free_mem_gb+'G / '+n.free_vcpu+'C'+(n.free_disk_gb!==undefined?' / 盘 '+n.free_disk_gb+'G':'')+'）'+(n.online?'':'（离线）'),disabled:!n.online};}),'自动');
  hostedFill('hostedCli',clis.map(function(c){var ok=combos.some(function(x){return x.cli===c&&x.verified;});return {value:c,text:c+(ok?'':'（未验通）'),disabled:!ok};}),'请选择 CLI');
  renderHostedLlm();
  hostedFill('hostedPlan',(opts.plans||[]).map(function(p){return {value:p.id,text:p.name+(p.fits?'':'（超出剩余额度）'),disabled:!p.fits};}),'请选择资源套餐');
  var own=(groupSnapshot.groups||[]).filter(function(g){return g.can_manage;});
  hostedFill('hostedGroup',own.map(function(g){return {value:g.code,text:g.short_name||'历史组'};}),own.length?'请选择编组':'（暂无可选编组,请先添加编组）');
  var rem=(opts.quota&&opts.quota.remaining)||{};
  document.getElementById('hostedQuota').textContent='剩余额度:Agent '+rem.agents+' 个 · vCPU '+rem.vcpu+' · 内存 '+rem.mem_gb+'G · 磁盘 '+rem.disk_gb+'G';
}
// combos 是 CLI×LLM×凭据形态(cred_kind)的唯一事实源:不在表里=不兼容(不列);verified=false=未验通(置灰)。
function renderHostedLlm(){
  var opts=hostedOptions||{},cli=document.getElementById('hostedCli').value,seen=[];
  var rows=(opts.combos||[]).filter(function(c){return c.cli===cli;}),llms=[];
  rows.forEach(function(c){if(seen.indexOf(c.llm)<0){seen.push(c.llm);llms.push({value:c.llm,ok:rows.some(function(x){return x.llm===c.llm&&x.verified;})});}});
  hostedFill('hostedLlm',llms.map(function(l){return {value:l.value,text:l.value+(l.ok?'':'（未验通）'),disabled:!l.ok};}),cli?'请选择 LLM':'请先选择 CLI');
  renderHostedAccounts();
}
function renderHostedAccounts(){
  var opts=hostedOptions||{},cli=document.getElementById('hostedCli').value,llm=document.getElementById('hostedLlm').value;
  var rows=(opts.accounts||[]).filter(function(a){return a.llm===llm;});
  // 账号可选 ⇔ 存在 (该 CLI, 该 LLM, 该账号的凭据形态) 且已验通的组合
  var items=rows.map(function(a){var ok=(opts.combos||[]).some(function(c){return c.cli===cli&&c.llm===llm&&c.verified&&(!a.kind||c.cred_kind===a.kind);});return {value:a.id,text:a.name+'（…'+a.last4+'）'+(ok?'':'（未验通）'),disabled:!ok};});
  hostedFill('hostedAccount',items,!llm?'请先选择 LLM':(rows.length?'请选择账号':'（该 LLM 暂无可用团队账号）'));
}
async function refreshHostedOptions(){
  var generation=++hostedOptionsGeneration,status=document.getElementById('hostedStatus'),retry=document.getElementById('hostedRetry'),form=document.getElementById('hostedForm');
  if(!status||!retry||!form)return null;
  status.textContent='托管选项加载中…';retry.hidden=true;
  var controller=new AbortController(),timer=setTimeout(function(){controller.abort()},groupFetchTimeoutMs);
  try{
    var r=await fetch(HOSTED_API.options,{cache:'no-store',signal:controller.signal});
    if(!r.ok){var detail=(await r.text()).trim();throw new Error('HTTP '+r.status+(detail?': '+detail:''));}
    var data=await r.json();if(!data||!Array.isArray(data.combos)||!Array.isArray(data.plans))throw new Error('响应格式无效');
    if(generation!==hostedOptionsGeneration)return null;
    hostedOptions=data;form.hidden=false;status.textContent='';renderHostedForm();return data;
  }catch(err){
    if(generation===hostedOptionsGeneration){hostedOptions=null;form.hidden=true;status.textContent='托管选项加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);retry.hidden=false;}
    return null;
  }finally{clearTimeout(timer);}
}
function hostedMaskRender(name,stage,failReason,lastStage){
  var box=document.getElementById('hostedMaskBox');if(!box)return;box.replaceChildren();
  var title=document.createElement('h3');title.textContent='创建 Agent：'+name;box.append(title);
  var at=HOSTED_STAGES.findIndex(function(x){return x[0]===(stage==='failed'?lastStage:stage);});
  HOSTED_STAGES.forEach(function(x,i){
    var row=document.createElement('div');row.className='hosted-stage';row.dataset.stage=x[0];
    var state=stage==='failed'?(i<at?'done':(i===at?'failed':'todo')):(stage==='online'?'done':(i<at?'done':(i===at?'current':'todo')));
    row.dataset.state=state;row.textContent=(state==='done'?'✓ ':state==='failed'?'✗ ':state==='current'?'… ':'○ ')+x[1];box.append(row);
  });
  var line=document.createElement('div');line.id='hostedMaskResult';
  if(stage==='failed')line.textContent='创建失败：'+(HOSTED_FAIL_REASONS[failReason]||('创建失败（原因码 '+(failReason||'未知')+'）'));
  else if(stage==='online')line.textContent='已上线';
  var hint=document.createElement('div');hint.className='group-feedback';hint.textContent='点击任意处关闭（不影响后台创建）';
  box.append(line,hint);
}
function hostedMaskHide(){var m=document.getElementById('hostedMask');if(m)m.hidden=true;}
async function hostedTrackJob(jobId,name){
  var mask=document.getElementById('hostedMask');if(!mask)return;mask.hidden=false;
  var lastStage='queued';hostedMaskRender(name,'queued','',lastStage);
  clearTimeout(hostedJobTimer);
  async function tick(){
    if(!document.getElementById('hostedMask'))return;   // 面板已关
    var stage='',reason='';
    try{
      var r=await fetch(HOSTED_API.job(jobId),{cache:'no-store'});
      if(!r.ok)throw new Error('HTTP '+r.status);
      var d=await r.json();stage=String(d.stage||'');reason=String(d.reason||'');
    }catch(err){hostedJobTimer=setTimeout(tick,hostedPollMs);return;}   // 网络抖动:继续轮询,不当成失败
    if(stage==='failed'){hostedMaskRender(name,'failed',reason,lastStage);enrollFeedback('hostedFeedback','创建 '+name+' 失败：'+(HOSTED_FAIL_REASONS[reason]||('原因码 '+(reason||'未知'))));return;}
    if(HOSTED_STAGES.some(function(x){return x[0]===stage;}))lastStage=stage;
    hostedMaskRender(name,stage,'',lastStage);
    if(stage==='online'){enrollFeedback('hostedFeedback',name+' 已上线');refreshGroups('hosted');refreshAgents();refreshRoster();refreshHostedOptions();return;}
    hostedJobTimer=setTimeout(tick,hostedPollMs);
  }
  tick();
}
async function hostedCreate(){
  var v=function(id){return document.getElementById(id).value.trim();};
  var body={name:v('hostedName'),group_id:v('hostedGroup'),node_id:v('hostedNode')||'auto',cli:v('hostedCli'),llm:v('hostedLlm'),account_id:v('hostedAccount'),plan_id:v('hostedPlan')};
  if(!body.name||!body.group_id||!body.cli||!body.llm||!body.account_id||!body.plan_id){enrollFeedback('hostedFeedback','请填写名称并选择编组、CLI、LLM、账号和资源套餐');return;}
  var btn=document.getElementById('hostedCreateBtn');btn.disabled=true;
  var res=await consolePost(HOSTED_API.create,body);btn.disabled=false;
  if(!res.ok){enrollFeedback('hostedFeedback',res.text);return;}   // 输入保留,业主改名/改选后可再点创建
  enrollFeedback('hostedFeedback','');document.getElementById('hostedName').value='';
  hostedTrackJob(String(res.data.job_id||''),body.name);
}
function hostedOpButtons(a,hs){
  var name=a.name,busy=hs.state==='busy',list=[];
  var ops=hs.state==='disabled'
    ?[['enable','启用',true],['destroy','销毁',true]]   // 停用中:容器已停,重启无意义;暂停中不能升级,须先启用(f0fe9794)——给了也只会被 Hub 拒绝
    :[['upgrade','升级',false],['restart','重启',false],['disable','停用',true],['destroy','销毁',true]];
  ops.forEach(function(d){
    var b=document.createElement('button');b.type='button';b.className='hosted-op';b.dataset.op=d[0];b.textContent=d[1];b.disabled=busy;
    b.onclick=async function(){
      var body={};
      if(d[0]==='destroy'){if(!confirm('销毁 '+name+'：容器将被删除,凭据立即吊销;数据卷会先备份一次并保留 14 天,到期删除;备份不占你的配额。确认销毁？'))return;body.confirm=true;}
      if(d[0]==='disable'){if(!confirm('停用 '+name+'：容器将停止,数据卷保留,可再启用;该 Agent 离线且凭据暂停、无法再接入;停用期间仍计配额。确认停用？'))return;body.confirm=true;}
      list.forEach(function(x){x.disabled=true;});
      var res=await consolePost(HOSTED_API.op(a.agent_id,d[0]),body);
      if(!res.ok){enrollFeedback('rosterFeedback',res.text);list.forEach(function(x){x.disabled=busy;});return;}
      enrollFeedback('rosterFeedback','');refreshRoster();refreshGroups('hosted-op');refreshAgents();
    };
    list.push(b);
  });
  return list;
}
function mountEnrollPanels(pane){
  var anchor=pane.querySelector('#group-status');if(!anchor)return;
  anchor.insertAdjacentHTML('beforebegin',(FEATURE_SELFHOST_REVIEW?'<section id=enrollReview class="group-block"><div class=group-toolbar><h3>自备 Agent 审核</h3><span><button id=enrollRefresh type=button>刷新</button></span></div><table id=enrollTable class=group-table><thead><tr><th>IP 来源</th><th>设备指纹</th><th>客户端版本</th><th>申请时间</th><th>操作</th></tr></thead><tbody id=enrollItems></tbody></table><div id=enrollEmpty hidden>暂无待审核的自备 Agent 申请。</div><div id=enrollStatus role=status></div><button id=enrollRetry type=button hidden>重试加载</button><div id=enrollNotice class=group-feedback role=status></div><section id=enrollEditor class=enroll-editor hidden><h4 id=enrollEditorTitle></h4><label>配对码 <input id=enrollCode maxlength=6 inputmode=numeric autocomplete=off placeholder="照客户端屏幕填写 6 位配对码"></label><label>Agent 名称 <input id=enrollName autocomplete=off placeholder="大小写字母和数字"></label><label>Agent 编组 <select id=enrollGroup></select></label><button id=enrollApprove type=button>通过</button><button id=enrollReject type=button>拒绝</button><button id=enrollCancel type=button>收起</button><div id=enrollFeedback class=group-feedback></div></section></section>':'')+'<section id=hostedCreate class="group-block"><div class=group-toolbar><h3>托管 Agent 生成</h3><span><button id=hostedRefresh type=button>刷新</button></span></div><div id=hostedStatus role=status></div><button id=hostedRetry type=button hidden>重试加载</button><div id=hostedForm hidden><label>节点 <select id=hostedNode></select></label><label>CLI <select id=hostedCli></select></label><label>LLM <select id=hostedLlm></select></label><label>LLM 账号 <select id=hostedAccount></select></label><label>运行资源套餐 <select id=hostedPlan></select></label><label>Agent 名称 <input id=hostedName autocomplete=off placeholder="大小写字母和数字"></label><label>Agent 编组 <select id=hostedGroup></select></label><button id=hostedCreateBtn type=button>创建</button><div id=hostedQuota role=status></div><div id=hostedFeedback class=group-feedback></div></div></section><section id=agentRoster class="group-block"><h3>Agent 列表</h3><table id=rosterTable class=group-table><thead><tr><th>Agent</th><th>状态</th><th>操作</th></tr></thead><tbody id=rosterItems></tbody></table><div id=rosterStatus role=status></div><div id=rosterEmpty hidden>没有自己的 Agent。</div><div id=rosterFeedback class=group-feedback></div></section>');
  if(FEATURE_SELFHOST_REVIEW){
    document.getElementById('enrollRefresh').onclick=refreshEnroll;document.getElementById('enrollRetry').onclick=refreshEnroll;
    document.getElementById('enrollApprove').onclick=enrollApprove;document.getElementById('enrollReject').onclick=enrollReject;
    document.getElementById('enrollCancel').onclick=enrollCloseEditor;
  }
  var mask=document.createElement('div');mask.id='hostedMask';mask.hidden=true;mask.innerHTML='<div id=hostedMaskBox class=hosted-mask-box></div>';mask.onclick=hostedMaskHide;pane.append(mask);
  document.getElementById('hostedRefresh').onclick=refreshHostedOptions;document.getElementById('hostedRetry').onclick=refreshHostedOptions;
  document.getElementById('hostedCli').onchange=renderHostedLlm;document.getElementById('hostedLlm').onchange=renderHostedAccounts;
  document.getElementById('hostedCreateBtn').onclick=hostedCreate;
  refreshRoster();if(FEATURE_SELFHOST_REVIEW)refreshEnroll();refreshHostedOptions();
  var rosterTimer=setInterval(function(){if(!document.getElementById('rosterItems')){clearInterval(rosterTimer);return;}refreshRoster();},15000);
}
// 平台管理 Tab(组长令 2026-10-01:外壳 + 第一期的「新用户注册邀请」,示意图「平台管理 Tab」)。
// 入口只发给管理用户(服务端按角色下发);可见面不是授权面——接口自己再按用户类型判一次,403 forbidden 时这里只说"没有权限"。
// 接口按 G3 设计件 3.3:POST /api/console/platform/users 返回的注册码【只此一次】,页面只在"结果块"里显示、点完成即清掉,
// 不写 localStorage、不进日志。能力菜单只放已做的项(没有动作的假入口不放),以后每开放一项加一行。
var ADMIN_API={
  users:'/api/console/platform/users',
  regen:function(id){return '/api/console/platform/users/'+encodeURIComponent(id)+'/registration-code';}
};
var ADMIN_CAPABILITIES=[{id:'invite',title:'新用户注册邀请'}];
var ADMIN_USER_STATE={pending_bind:'待绑定飞书',active:'正常',disabled:'已停用'};
var ADMIN_USER_TYPE={member:'普通用户',manager:'管理用户',platform_admin:'平台管理员'};
var adminUsersGeneration=0;
function adminErrText(code,fallback){return code==='forbidden'?'你没有管理用户的权限':fallback;}   // forbidden 在这里指"不是管理用户",共用文案里指别的
function adminShowCode(user,res){
  var box=document.getElementById('inviteResult');if(!box)return;
  var code=String(res.registration_code||'');
  document.getElementById('inviteResultUser').textContent=user;
  document.getElementById('inviteCode').textContent=code;
  document.getElementById('inviteCmd').textContent='#register '+code;
  document.getElementById('inviteExpiry').textContent='有效至 '+enrollTime(res.code_expires_at)+'；重新生成会让旧码作废';
  enrollFeedback('inviteCopyFeedback','');box.hidden=false;
}
function adminClearCode(){
  var box=document.getElementById('inviteResult');if(!box)return;
  ['inviteResultUser','inviteCode','inviteCmd','inviteExpiry'].forEach(function(id){document.getElementById(id).textContent='';});
  enrollFeedback('inviteCopyFeedback','');box.hidden=true;
}
async function adminCopyCode(){
  var code=document.getElementById('inviteCode').textContent;if(!code)return;
  try{await navigator.clipboard.writeText(code);enrollFeedback('inviteCopyFeedback','已复制注册码');}
  catch(_){enrollFeedback('inviteCopyFeedback','复制失败,请手动选中注册码复制');}
}
async function adminCreateUser(){
  var v=function(id){return document.getElementById(id).value.trim();};
  var body={display_name:v('inviteName'),user_id:v('inviteUserId'),short_name:v('inviteShort'),type:v('inviteType')};
  if(!body.display_name||!body.user_id||!body.short_name){enrollFeedback('inviteFeedback','请填写用户名、用户 ID 和用户 ID 简称');return;}
  var btn=document.getElementById('inviteCreate');btn.disabled=true;
  var res=await consolePost(ADMIN_API.users,body);btn.disabled=false;
  if(!res.ok){enrollFeedback('inviteFeedback',adminErrText(res.code,res.text));return;}   // 输入保留,改完可再点
  enrollFeedback('inviteFeedback','');
  ['inviteName','inviteUserId','inviteShort'].forEach(function(id){document.getElementById(id).value='';});
  adminShowCode(String(res.data.user_id||body.user_id),res.data);refreshAdminUsers();
}
async function adminRegen(userId){
  var res=await consolePost(ADMIN_API.regen(userId));
  if(!res.ok){enrollFeedback('adminUsersFeedback',adminErrText(res.code,res.text));return;}
  enrollFeedback('adminUsersFeedback','');adminShowCode(userId,res.data);
}
function renderAdminUsers(users){
  var body=document.getElementById('adminUsersItems'),empty=document.getElementById('adminUsersEmpty');if(!body||!empty)return;
  body.replaceChildren();
  users.forEach(function(u){
    var row=document.createElement('tr');row.className='admin-user-row';row.dataset.userId=u.user_id;
    [u.user_id,u.display_name||'',u.short_name||'',ADMIN_USER_TYPE[u.type]||u.type||'',ADMIN_USER_STATE[u.state]||u.state||''].forEach(function(t){var td=document.createElement('td');td.textContent=t;row.append(td);});
    var action=document.createElement('td');
    if(u.state==='pending_bind'){var b=document.createElement('button');b.type='button';b.className='admin-regen';b.textContent='重新生成注册码';b.onclick=function(){adminRegen(u.user_id);};action.append(b);}
    row.append(action);body.append(row);
  });
  empty.hidden=users.length>0;
}
async function refreshAdminUsers(){
  var generation=++adminUsersGeneration,status=document.getElementById('adminUsersStatus'),retry=document.getElementById('adminUsersRetry');
  if(!status||!retry)return null;
  status.textContent='用户列表加载中…';retry.hidden=true;
  var controller=new AbortController(),timer=setTimeout(function(){controller.abort()},groupFetchTimeoutMs);
  try{
    var r=await fetch(ADMIN_API.users,{cache:'no-store',signal:controller.signal});
    var data=null;try{data=await r.json();}catch(_){}
    if(!r.ok)throw new Error(adminErrText(data&&data.error,consoleErrorText(r.status,data)));
    if(!data||!Array.isArray(data.users))throw new Error('响应格式无效');
    if(generation!==adminUsersGeneration)return null;
    renderAdminUsers(data.users);status.textContent='';return data;
  }catch(err){
    if(generation===adminUsersGeneration){renderAdminUsers([]);document.getElementById('adminUsersEmpty').hidden=true;status.textContent='用户列表加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);retry.hidden=false;}
    return null;
  }finally{clearTimeout(timer);}
}
function openPlatformAdminWorkspace(){
  if(tabs.has('__platform-admin')){activate('__platform-admin');return;}
  var tab=document.createElement('span');tab.className='tab';tab.title='平台管理';
  var label=document.createElement('span'),x=document.createElement('span');label.textContent='平台管理';x.className='x';x.textContent='×';tab.append(label,x);
  var pane=document.createElement('section');pane.className='group-workspace';
  pane.innerHTML='<h2>平台管理</h2><nav id=adminMenu></nav><section id=adminInvite class="group-block"><h3>新用户注册邀请</h3>'
   +'<label>用户名 <input id=inviteName autocomplete=off placeholder="显示名称,如 张三"></label>'
   +'<label>用户 ID <input id=inviteUserId autocomplete=off placeholder="2～32 位大小写字母和数字"></label>'
   +'<label>用户 ID 简称 <input id=inviteShort autocomplete=off placeholder="2～16 位大小写字母和数字"></label>'
   +'<label>用户类型 <select id=inviteType><option value=member>普通用户</option><option value=manager>管理用户</option></select></label>'
   +'<button id=inviteCreate type=button>生成注册码</button><div id=inviteFeedback class=group-feedback></div>'
   +'<section id=inviteResult class=enroll-editor hidden><h4>注册码(只显示这一次)</h4><div>用户 <b id=inviteResultUser></b></div><div><code id=inviteCode></code> <button id=inviteCopy type=button>复制注册码</button><span id=inviteCopyFeedback class=group-feedback></span></div><div id=inviteExpiry></div><div>让该用户在飞书里向机器人发送：<code id=inviteCmd></code></div><button id=inviteDone type=button>完成,关闭</button></section>'
   +'<h4>用户</h4><table id=adminUsersTable class=group-table><thead><tr><th>用户 ID</th><th>用户名</th><th>简称</th><th>类型</th><th>状态</th><th>操作</th></tr></thead><tbody id=adminUsersItems></tbody></table><div id=adminUsersEmpty hidden>暂无用户。</div><div id=adminUsersStatus role=status></div><button id=adminUsersRetry type=button hidden>重试加载</button><div id=adminUsersFeedback class=group-feedback></div></section>';
  tab.onclick=function(e){if(e.target===x){e.stopPropagation();closeTab('__platform-admin');}else activate('__platform-admin');};
  document.getElementById('tabs').append(tab);document.getElementById('panes').append(pane);tabs.set('__platform-admin',{tab:tab,pane:pane});
  var menu=pane.querySelector('#adminMenu');
  ADMIN_CAPABILITIES.forEach(function(c,i){var b=document.createElement('button');b.type='button';b.dataset.capability=c.id;b.textContent=c.title;if(i===0)b.setAttribute('aria-current','true');menu.append(b);});
  document.getElementById('inviteCreate').onclick=adminCreateUser;document.getElementById('inviteCopy').onclick=adminCopyCode;document.getElementById('inviteDone').onclick=adminClearCode;
  document.getElementById('adminUsersRetry').onclick=refreshAdminUsers;
  activate('__platform-admin');refreshAdminUsers();
}
function openSpecsWorkspace(){if(tabs.has('__specs')){activate('__specs');return;}var tab=document.createElement('span');tab.className='tab';tab.title='Specs';var label=document.createElement('span');label.textContent='Specs';var x=document.createElement('span');x.className='x';x.textContent='×';x.title='关闭';tab.append(label,x);var pane=document.createElement('iframe');pane.title='Specs workspace';pane.src='/console/specs?embedded=1';pane.style.display='none';tab.onclick=function(ev){if(ev.target===x){ev.stopPropagation();closeTab('__specs');}else activate('__specs');};document.getElementById('tabs').append(tab);document.getElementById('panes').append(pane);tabs.set('__specs',{tab:tab,pane:pane});activate('__specs');}
function openTaskBoardWorkspace(){if(tabs.has('__task-board')){activate('__task-board');return;}var tab=document.createElement('span');tab.className='tab';tab.title='任务管理';var label=document.createElement('span');label.textContent='任务管理';var x=document.createElement('span');x.className='x';x.textContent='×';x.title='关闭';tab.append(label,x);var pane=document.createElement('iframe');pane.title='任务管理';pane.src='/console/tasks?embedded=1';pane.style.display='none';tab.onclick=function(ev){if(ev.target===x){ev.stopPropagation();closeTab('__task-board');}else activate('__task-board');};document.getElementById('tabs').append(tab);document.getElementById('panes').append(pane);tabs.set('__task-board',{tab:tab,pane:pane});activate('__task-board');}
dwThemeInit();   // 🔴 主题先行:先把已存偏好应用到外壳,再渲染列表(否则先白一下再变暗)
document.getElementById('retryPlatform').onclick=refreshPlatform;
document.getElementById('retryAgents').onclick=refreshAgents;
var agentNavModeButton=document.getElementById('agentNavMode');if(agentNavModeButton)agentNavModeButton.onclick=function(){agentNavSetMode(agentNavMode==='tree'?'flat':'tree');};
refreshPlatform();
refreshAgents();
refreshGroups();
setInterval(refreshAgents,15000);

function renderGroups(){
 var body=document.getElementById('groupListItems'),detail=document.getElementById('groupDetail');if(!body||!detail)return;
 body.replaceChildren();var groups=groupSnapshot.groups||[],owned=groupSnapshot.owned_agents||[],selected=groups.find(function(g){return g.code===window.selectedGroupCode;});if(!selected&&groups.length){selected=groups[0];window.selectedGroupCode=selected.code;}
 groups.forEach(function(g){var row=document.createElement('tr');row.className='group-row';row.onclick=function(e){if(e.target.closest('button'))return;window.selectedGroupCode=g.code;renderGroups();setGroupPanelMode('detail');};[g.short_name||'历史组',groupShortName(g.leader_session_name),g.owner_display_name||''].forEach(function(v){var td=document.createElement('td');td.textContent=v;row.append(td);});var action=document.createElement('td');if(g.can_manage){var del=document.createElement('button');del.type='button';del.className='group-delete';del.textContent='删除';del.onclick=function(){if(confirm('确认删除该编组？'))groupAction('/api/console/agent-groups/'+encodeURIComponent(g.code),'DELETE',null,'groupManageFeedback');};action.append(del);}row.append(action);body.append(row);});
 document.getElementById('groupEmptyCreate').hidden=groups.length>0;function fill(id,agents,empty){var s=document.getElementById(id);s.replaceChildren();if(!agents.length){var n=document.createElement('option');n.value='';n.textContent=empty;s.append(n);}else agents.forEach(function(a){var o=document.createElement('option');o.value=a.id;o.textContent=groupShortName(a.session_name)+(a.online?'':'（offline）');s.append(o);});s.disabled=!agents.length;}fill('groupLeader',owned,'没有自己的 Agent');var leaders=document.getElementById('groupLeader');if(groupLeaderSelection&&Array.from(leaders.options).some(function(o){return o.value===groupLeaderSelection;}))leaders.value=groupLeaderSelection;setGroupPanelMode(groupPanelMode);if(!selected)return;
 document.getElementById('selectedGroupName').textContent=selected.short_name||'历史组';var members=document.getElementById('groupMembers');members.replaceChildren();var ordered=(selected.members||[]).slice().sort(function(a,b){var al=a.key_id===selected.leader_key_id&&a.session_name===selected.leader_session_name,bl=b.key_id===selected.leader_key_id&&b.session_name===selected.leader_session_name;return al===bl?0:(al?-1:1);});ordered.forEach(function(m){var leader=m.key_id===selected.leader_key_id&&m.session_name===selected.leader_session_name,row=document.createElement('div'),label=document.createElement('span');row.className='group-member-row';row.dataset.agentId=m.agent_id;label.textContent=groupShortName(m.session_name)+(leader?'（组长）':'')+(agentTreeMemberOnline(m)?'':'（offline）');row.append(label);if(selected.can_manage&&!leader){var rm=document.createElement('button');rm.type='button';rm.className='group-member-remove';rm.textContent='剔除';rm.onclick=function(){if(confirm('确认剔除该 Agent？该 Agent 与本组成员的消息可达性会变。'))groupAction('/api/console/agent-groups/'+encodeURIComponent(selected.code)+'/members/'+encodeURIComponent(m.agent_id),'DELETE',null,'groupMemberFeedback');};row.append(rm);}members.append(row);});var ids=new Set((selected.members||[]).map(function(m){return m.agent_id;}));fill('groupJoinAgent',owned.filter(function(a){return !ids.has(a.id);}), '没有可加入的 Agent');document.getElementById('groupMemberAdd').hidden=!selected.can_manage;var toggle=document.getElementById('groupAdditionsToggle');toggle.hidden=!selected.can_manage;toggle.textContent=selected.additions_closed?'开放添加':'关闭添加';toggle.onclick=function(){groupAction('/api/console/agent-groups/'+encodeURIComponent(selected.code)+'/additions','POST',{closed:!selected.additions_closed},'groupManageFeedback');};
}
function openGroupWorkspace(){
 if(tabs.has('__agent-groups')){activate('__agent-groups');return;}var tab=document.createElement('span');tab.className='tab';tab.title='Agent 管理';var label=document.createElement('span'),x=document.createElement('span');label.textContent='Agent 管理';x.className='x';x.textContent='×';tab.append(label,x);var pane=document.createElement('section');pane.className='group-workspace';
 pane.innerHTML='<h2>Agent 管理</h2><section class="group-block group-block-list"><div class=group-toolbar><h3>编组列表</h3><span><button id=groupAddOpen type=button>⊕ 添加编组</button></span></div><table id=groupListTable class=group-table><thead><tr><th>组名</th><th>组长</th><th>业主</th><th>操作</th></tr></thead><tbody id=groupListItems></tbody></table><div id=groupEmptyCreate>尚无编组，可以添加第一个编组。</div><div id=groupRetry><button id=retryGroups type=button>重试加载</button><div id=groupRetryFeedback class=group-feedback></div></div></section><section id=groupAddPanel class="group-block group-block-add" hidden><label>8 字符组名 <input id=groupShortName maxlength=8></label><label>指定组长 <select id=groupLeader disabled></select></label><button id=createGroup type=button>创建编组</button><div id=groupNameFeedback class=group-feedback></div></section><section id=groupDetail class="group-block group-block-detail" hidden><h3 id=selectedGroupName></h3><h4>成员列表</h4><div id=groupMembers></div><div id=groupMemberFeedback class=group-feedback></div><section id=groupMemberAdd class="group-block group-block-add"><h4>继续添加新成员</h4><select id=groupJoinAgent disabled></select><button id=joinGroup type=button>添加</button><div id=groupJoinFeedback class=group-feedback></div></section><button id=groupAdditionsToggle type=button></button><div id=groupManageFeedback class=group-feedback></div></section><div id=group-status></div>';
 tab.onclick=function(e){if(e.target===x){e.stopPropagation();closeTab('__agent-groups');}else activate('__agent-groups');};document.getElementById('tabs').append(tab);document.getElementById('panes').append(pane);tabs.set('__agent-groups',{tab:tab,pane:pane});groupMode=true;document.getElementById('groupAddOpen').onclick=function(){setGroupPanelMode(groupPanelMode==='add'?'':'add');};document.getElementById('retryGroups').onclick=function(){refreshGroups('retry');};document.getElementById('createGroup').onclick=function(){var input=document.getElementById('groupShortName'),name=input.value.trim(),id=document.getElementById('groupLeader').value;if(!name){groupFeedback('groupNameFeedback','请输入团队名称');return;}groupAction('/api/console/agent-groups','POST',{agent_id:id,short_name:name},'groupNameFeedback').then(function(ok){if(ok){input.value='';setGroupPanelMode('detail');}});};document.getElementById('joinGroup').onclick=function(){var g=(groupSnapshot.groups||[]).find(function(v){return v.code===window.selectedGroupCode;}),id=document.getElementById('groupJoinAgent').value;if(g&&id)groupAction('/api/console/agent-groups/'+encodeURIComponent(g.code)+'/members','POST',{agent_id:id},'groupJoinFeedback');};mountEnrollPanels(pane);activate('__agent-groups');refreshGroups('initial');
}

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

const (
	ConsoleAgentCapabilitySpecsNew = "console-specs:new"
	ConsoleAgentCapabilityTasks    = "console-tasks"
)

func ConsoleAgentSpecCapabilityPage(specID string) string {
	return "console-spec:" + strings.TrimSpace(specID)
}

// ConsoleAgentCapabilityPageAllowed is the route-wiring registry. It prevents
// Agent handlers from deriving their expected page id from caller-controlled
// input. The signing endpoint deliberately accepts opaque view page ids: an
// issued capability is usable only where a consumer derives that exact id.
func ConsoleAgentCapabilityPageAllowed(pageID string) bool {
	pageID = strings.TrimSpace(pageID)
	if pageID == ConsoleAgentCapabilitySpecsNew || pageID == ConsoleAgentCapabilityTasks {
		return true
	}
	if !strings.HasPrefix(pageID, "console-spec:") {
		return false
	}
	specID := strings.TrimPrefix(pageID, "console-spec:")
	// The concrete route derives this id from net/http PathValue and owns the
	// existence/principal checks. Character preferences here cannot expand what
	// a capability consumer will accept.
	return specID != ""
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
	req.PageID = strings.TrimSpace(req.PageID)
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

func (h *Hub) RequireConsoleSpecAgentCapability(pageID func(*http.Request) string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, repoOK := h.Repo.(consoleAuthRepository)
		specRepo, specOK := h.consoleSpecs()
		keyID := strings.TrimSpace(r.Header.Get("X-DW-Agent-Key-ID"))
		sessionName := strings.TrimSpace(r.Header.Get("X-DW-Agent-Session"))
		secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		meta, authErr := h.Repo.ResolveServiceAPISecret(r.Context(), keyID, HashAPIKey(secret))
		agent, agentErr := specRepo.ResolveConsoleAgentByEndpoint(r.Context(), keyID, sessionName)
		raw := strings.TrimSpace(r.Header.Get("X-DW-Console-Capability"))
		expectedPage := pageID(r)
		if !h.consoleAuthEnabled || !repoOK || !specOK || keyID == "" || sessionName == "" || secret == "" || authErr != nil || meta == nil || agentErr != nil || agent == nil || !agent.Active || raw == "" || expectedPage == "" {
			http.Error(w, "console capability required", http.StatusForbidden)
			return
		}
		capability, err := repo.ResolveConsolePageCapability(r.Context(), hashConsoleToken(raw), agent.ID, expectedPage, time.Now())
		if err != nil {
			http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
			return
		}
		if capability == nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), consolePrincipalContextKey{}, capability.PrincipalID)
		ctx = context.WithValue(ctx, consoleSpecActorContextKey{}, consoleSpecActor{ID: capability.AgentID, Kind: "agent"})
		next(w, r.WithContext(ctx))
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
