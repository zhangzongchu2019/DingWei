package admin

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Nova-260921-A(B1-b):admin 能力搬进 console 的**那道新门**。
//
//	门 A(旧,🔴 不动)  /admin/*          ＋ wp_admin cookie   ⇒ requireAuth
//	门 B(新)           /console/admin/*  ＋ console 角色      ⇒ requireConsoleCap
//
// 业主原话「admin 接纳 console 的鉴权方式」⇒ **业主不该在 console 里再登一次**;
// 业主原话「先并存一段时间,有一个回退的地方」⇒ **门 A 保持原样,它就是回退口**。
//
// 🔴 两道门**共用同一批 handler 函数**(filesPage／provisionPage／apiKeys／botChannels/…),
// 只换授权包装 ⇒ 行为分叉在结构上只剩"授权层"这一处,而授权层由 capability.go 的
// **同一份判据**决定。剩下的由闸证明(G1 授权矩阵 ／ G2 双路一致性 ／ G3 越权面 ／ G4 门 A 冻结)。
const (
	// consoleDoorPrefix 是门 B 的路径前缀。
	consoleDoorPrefix = "/console/admin"
	// adminDoorPrefix 是门 A 的路径前缀(既有,未改)。
	adminDoorPrefix = "/admin"
)

// consoleActorContextKey 只用于把**已授权**的 console principal 交给同请求的审计写入点。
// 🔴 它不是授权判据(判据在 m8.RequireConsolePrincipal ＋ m8.RequireConsoleManagement)。
type consoleActorContextKey struct{}

// consolePlatformRoster 是「增删管理者」这一格需要的最小取数面。
//
// 🔴 用**窄接口 ＋ 运行时断言**(与 m8 的 consolePlatformRepository／consoleAuthRepository 同手法),
// 不往 store.Repository 那个大接口上加方法 —— 免得波及全仓每一个实现与替身。
type consolePlatformRoster interface {
	ListConsolePrincipals(context.Context) ([]model.ConsolePrincipal, error)
	ListActiveConsoleManagement(context.Context) ([]string, error)
	UpsertConsolePlatformRole(context.Context, model.ConsolePlatformRole) error
}

// adminBasePrefix 返回**本页所属那道门**的前缀,供页面里的链接与 form action 使用。
//
// 🔴 为什么必须有它(F2,实测逐处清点):搬迁范围内的 **4 个 handler** 共发出 **10 处**
// `/admin/...` 绝对 URL —— filesPage ×3 · botChannels ×2 · apiKeys ×3 · provisionPage ×2。
// (🔵 用 handler 名而不是行号:行号随树变,本仓已在"跨席给行号不带锚"上栽过。)
// 业主在 console 里**没有** wp_admin cookie ⇒ 若照原样复用,门 B 页面上每一个按钮
// 都会落到门 A ⇒ `requireAuth` ⇒ **303 → /admin/login** ⇒ 「**页面能看、按钮全废**」。
//
// 🔴 `members()`(成员配置)**刻意不改**:它**不在**业主确认的搬迁范围内
// (板件 BATCH-B1-CONTENT-20260921.md §六-2 只列 文件对象／Provision／凭证 API key／机器人通道),
// 门 B 也不提供它 ⇒ 它发出的 `/admin/members` 绝对 URL 本来就是对的。
// 「增删管理者」是 `console_platform_role` 名册(REQUIREMENT-REGISTRY-20260921.md:42),
// **不是** `/admin/members` 的成员名册 —— 两者是不同的表、不同的权力。
//
// 🔴 门 A 的解析结果恒为 "/admin" ⇒ 门 A 输出字节与改动前逐字相同。
// 🔵 **这句话本身是读码结论,不当验收依据**:G4 用 `bcd5f337` 基线**两次实跑逐页 diff** 钉住。
func adminBasePrefix(r *http.Request) string {
	if r == nil || r.URL == nil {
		return adminDoorPrefix
	}
	path := r.URL.Path
	if path == consoleDoorPrefix || strings.HasPrefix(path, consoleDoorPrefix+"/") {
		return consoleDoorPrefix
	}
	return adminDoorPrefix
}

// consoleAdminActor 返回门 B 上已授权的 principal;门 A 或取不到时返回 ""。
func consoleAdminActor(r *http.Request) string {
	if r == nil {
		return ""
	}
	actor, _ := r.Context().Value(consoleActorContextKey{}).(string)
	return strings.TrimSpace(actor)
}

// consoleDoorRoute 是门 B 的一条路由。**挂载与导航页都只读这张表** ——
// 🔴 不许各写一份清单:两份清单必漂移(本仓已在"两份版本计划件各说各话"上吃过亏)。
type consoleDoorRoute struct {
	pattern    string // 完整路径模式(含前缀),直接喂给 mux.HandleFunc
	capability adminCapability
	title      string // 导航页显示名;"" ⇒ 不单列(见 adminCapabilityIndexEntry)
	get        http.HandlerFunc
	post       http.HandlerFunc
}

func (s *Server) consoleDoorRoutes() []consoleDoorRoute {
	p := consoleDoorPrefix
	routes := []consoleDoorRoute{
		{pattern: p, capability: capConsoleIndex, title: "", get: s.consoleAdminIndex},
		{pattern: p + "/files", capability: capFiles, title: "文件对象浏览", get: s.filesPage},
		// capFileContent 从"文件对象浏览"页内每行的链接进入 ⇒ **不单列**(单列它没有意义)。
		{pattern: p + "/files/{id}/content", capability: capFileContent, title: "", get: s.fileContent},
		{pattern: p + "/provision", capability: capProvision, title: "Provision 下发", get: s.provisionPage, post: s.manageProvision},
		{pattern: p + "/api-keys", capability: capAPIKeys, title: "凭证 API key", get: s.apiKeys, post: s.manageAPIKey},
		{pattern: p + "/bot-channels", capability: capBotChannels, title: "机器人通道", get: s.botChannels, post: s.upsertBotChannel},
		{pattern: p + "/platform-roles", capability: capPlatformRoles, title: "管理者名单(增删)", get: s.consolePlatformRolesPage, post: s.manageConsolePlatformRole},
	}
	if !s.NewPlatform {
		return routes
	}
	// 新平台的身份与角色由 np_user/np_identity 及管理 JSON 入口维护；
	// provision、旧 API key 和手工管理者名单都依赖旧表或会被垫片覆盖。
	out := make([]consoleDoorRoute, 0, len(routes)-3)
	for _, route := range routes {
		switch route.capability {
		case capProvision, capAPIKeys, capPlatformRoles:
			continue
		default:
			out = append(out, route)
		}
	}
	return out
}

// MountConsole 注册门 B。**必须在 s.Prefix 被装配成"console 鉴权开启"的 hub 之后调用**
// (cmd/workpulse/main.go:168 赋值,紧随其后的 adm.Mount 之后调用本函数)。
//
// 🔴 若 s.Prefix 为 nil,requireConsoleCap **关死整道门**(503),不是放行。
func (s *Server) MountConsole(mux *http.ServeMux) {
	for _, route := range s.consoleDoorRoutes() {
		if route.get != nil {
			mux.HandleFunc("GET "+route.pattern, s.requireConsoleCap(route.capability, route.get))
		}
		if route.post != nil {
			mux.HandleFunc("POST "+route.pattern, s.requireConsoleCap(route.capability, route.post))
		}
	}
}

// requireConsoleCap 是门 B 的授权包装。三层,顺序不可换:
//
//	① RequireConsolePrincipal   没有有效 console 会话 ⇒ 401;鉴权开关关闭 ⇒ 404
//	② RequireConsoleManagement  🔴 判据是 **role == management**,**不是**任何动作动词
//	                            (用 RequireConsolePlatformAction("read") 会让**每个默认 owner**
//	                             都进得来 —— 见 internal/m8/console_management.go 头注)
//	③ 能力表                    表外能力 ⇒ 404(fail-closed:查不到 ≠ 通过)
//
// 🔴 取不到 actor 也拒:守卫放行了却读不出 principal 是**装置矛盾**,
// 此时继续下去会让审计记成无差别的 "admin"(F3),那是静默降级。
func (s *Server) requireConsoleCap(capability adminCapability, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hub := s.Prefix
		if hub == nil {
			http.Error(w, "console admin door unavailable", http.StatusServiceUnavailable)
			return
		}
		hub.RequireConsolePrincipal(hub.RequireConsoleManagement(func(w http.ResponseWriter, r *http.Request) {
			if !adminCapabilityAllowed(subjectConsoleManagement, capability) {
				http.Error(w, fmt.Sprintf("admin capability %q is not in the owner-confirmed console scope", string(capability)), http.StatusNotFound)
				return
			}
			actor := m8.ConsolePrincipal(r)
			if actor == "" {
				http.Error(w, "console admin actor unavailable", http.StatusForbidden)
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), consoleActorContextKey{}, actor)))
		}))(w, r)
	}
}

// requireAuthCap 是门 A 上**在搬迁范围内**那 10 条路由的授权包装。
//
// 🔴 它与门 B 调**同一份** adminCapabilityAllowed ⇒ "两路判据分叉"结构上不可能。
// 🔵 门 A 其余 30+ 条路由**仍挂原样的 requireAuth**(业主:「其余暂不搬」)——
// 本次不给它们套能力表,免得把"搬迁范围"这张表变成整个旧后台的开关。
func (s *Server) requireAuthCap(capability adminCapability, next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !adminCapabilityAllowed(subjectAdminSession, capability) {
			http.Error(w, fmt.Sprintf("admin capability %q is not in the owner-confirmed scope", string(capability)), http.StatusNotFound)
			return
		}
		next(w, r)
	})
}

// consoleAdminIndex 是门 B 的导航页。
//
// 🔴 条目**由能力表生成**,不是手写第二份清单。
// 🔴 占位项(知识库／skill／MCP)**不是链接** —— 业主说"留下空间",
// 做成点不动的死链只会让人以为坏了([[feature-built-but-never-switched-on]] 的反面)。
func (s *Server) consoleAdminIndex(w http.ResponseWriter, r *http.Request) {
	base := adminBasePrefix(r)
	writeHTMLHeader(w, "平台管理")
	_, _ = fmt.Fprintf(w, `<h3>平台管理</h3>
<p>本入口只对 <code>console_platform_role = management</code> 有效;普通业主(默认角色 <code>owner</code>)打开即 403。</p>
<h4>管理能力</h4><ul>`)
	for _, route := range s.consoleDoorRoutes() {
		if route.title == "" || route.get == nil {
			continue
		}
		_, _ = fmt.Fprintf(w, `<li><a href="%s" data-dw-capability="%s">%s</a></li>`,
			esc(base+strings.TrimPrefix(route.pattern, consoleDoorPrefix)), esc(string(route.capability)), esc(route.title))
	}
	_, _ = w.Write([]byte(`</ul>
<h4>后续(占位,未实现)</h4><ul>
<li data-dw-placeholder="knowledge">知识库管理 —— <b>未实现</b>(业主要求留出位置)</li>
<li data-dw-placeholder="skill">skill 管理 —— <b>未实现</b>(业主要求留出位置)</li>
<li data-dw-placeholder="mcp">MCP 管理 —— <b>未实现</b>(业主要求留出位置)</li>
</ul>
<p>旧 admin 后台(<code>/admin</code>)按业主决定<b>并存至 0.2.2.0</b>,作为回退入口。</p>`))
}

// realManagers 把"库里有 active 的 management 行"收窄成"**此刻真的能行使管理权**的人"。
//
// 🔴 两者的差别不是理论上的,是本格闸第一跑就咬出来的:
// `ResolveConsolePlatformRole` 的 SQL 带 `p.active=1`,所以**停用 principal 上的 management 行
// 不授予任何权力**(它是一条悬空授权)。若「剩最后一个不能删」按**原始行数**计数,
// 一条悬空行就能让**最后一个真管理者**被撤掉 —— 而撤掉之后全仓没有任何运行时路径能加回来。
// ⇒ 守卫必须按真管理者计数;悬空行另外**显示出来**(看不见的授权等于没有授权,反之亦然)。
//
// 返回 (真管理者集合, 悬空的 management 行)。
func realManagers(principals []model.ConsolePrincipal, managementRows []string) (map[string]bool, []string) {
	active := make(map[string]bool, len(principals))
	for _, p := range principals {
		if p.Active {
			active[p.ID] = true
		}
	}
	real := make(map[string]bool, len(managementRows))
	var dangling []string
	for _, id := range managementRows {
		if active[id] {
			real[id] = true
			continue
		}
		dangling = append(dangling, id)
	}
	return real, dangling
}

// consolePlatformRolesPage 是「增删管理者」这一格(Nova-260921-A 新增,门 A 没有对应入口)。
func (s *Server) consolePlatformRolesPage(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.Repo.(consolePlatformRoster)
	if !ok {
		http.Error(w, "management roster unavailable", http.StatusServiceUnavailable)
		return
	}
	principals, err := repo.ListConsolePrincipals(r.Context())
	if err != nil {
		http.Error(w, "management roster unavailable", http.StatusServiceUnavailable)
		return
	}
	managers, err := repo.ListActiveConsoleManagement(r.Context())
	if err != nil {
		http.Error(w, "management roster unavailable", http.StatusServiceUnavailable)
		return
	}
	isManager, dangling := realManagers(principals, managers)
	realCount := len(isManager) // 🔴 真管理者数,不是 management 行数(见 realManagers)
	danglingSet := make(map[string]bool, len(dangling))
	for _, id := range dangling {
		danglingSet[id] = true
	}
	base := adminBasePrefix(r)
	writeHTMLHeader(w, "管理者名单")
	_, _ = fmt.Fprintf(w, `<h3>管理者名单</h3>
<p><a href="%s">返回</a></p>
<p>判据:<code>console_platform_role = management</code>(active)。只有管理者能打开平台管理入口。</p>
<p>🔴 <b>剩最后一个管理者时不能撤销</b> —— 撤销后所有人(包括操作者自己)都会失去本入口,且没有任何运行时路径能加回来(此前只有 seed 能写这张表)。</p>
<table border=1 cellpadding=4><tr><th>principal</th><th>显示名</th><th>principal 启用</th><th>管理者</th><th>操作</th></tr>`, esc(base))
	if len(principals) == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=5>暂无 console principal。principal 由登录或 seed 产生。</td></tr>`))
	}
	listed := make(map[string]bool, len(principals))
	for _, p := range principals {
		listed[p.ID] = true
		// 🔴 悬空行必须**在这一行上就能看见**:库里 management 还 active,但 principal 已停用
		// ⇒ 运行时不授权。藏起来的话,它会同时骗两拨人 ——
		// 界面上像"这人不是管理者",而库里像"这人是管理者"。
		trAttr := ""
		management := fmt.Sprintf("%t", isManager[p.ID])
		if danglingSet[p.ID] {
			trAttr = ` data-dw-dangling="` + esc(p.ID) + `"`
			management = `false <b title="console_platform_role 里 management 行仍 active,但 principal 已停用 ⇒ ResolveConsolePlatformRole 返回空,不授权。它同时不计入「最后一个管理者」的计数。">🔴 悬空(库里有 active 的 management 行,但不授权)</b>`
		}
		_, _ = w.Write([]byte(`<tr` + trAttr + `><td><code>` + esc(p.ID) + `</code></td><td>` + esc(p.DisplayName) + `</td><td>` + fmt.Sprintf("%t", p.Active) + `</td><td>` + management + `</td><td>`))
		manager := isManager[p.ID]
		lastManager := realCount <= 1
		writePlatformRoleForm(w, base, p.ID, "promote", "提升为管理者",
			manager || !p.Active, platformRoleBlockReason("promote", manager, !p.Active, lastManager))
		writePlatformRoleForm(w, base, p.ID, "demote", "撤销管理者",
			!manager || !p.Active || lastManager, platformRoleBlockReason("demote", manager, !p.Active, lastManager))
		_, _ = w.Write([]byte(`</td></tr>`))
	}
	// 🔴 悬空授权要**看得见**:management 行还 active,但 principal 已停用(或不存在)
	// ⇒ ResolveConsolePlatformRole 对它返回 "" ⇒ **库里说他是管理者、运行时说不是**。
	// 这一行还影响「最后一个不能删」的计数(见 realManagers),所以它不是纯展示。
	for _, id := range dangling {
		if !listed[id] {
			_, _ = fmt.Fprintf(w, `<tr data-dw-dangling="%s"><td><code>%s</code></td><td colspan=3>🔴 悬空:该 principal 不存在,management 行仍在</td><td>—</td></tr>`,
				esc(id), esc(id))
		}
	}
	_, _ = w.Write([]byte(`</table>`))
}

// platformRoleBlockReason 把"为什么这个按钮点不动"写在按钮上。
// 🔴 判据写在它生效的地方 —— 不是让人点了以后才从错误消息里学。
// 🔴 每个被禁用的按钮都必须有原因:返回 "" 而按钮 disabled,等于界面上出现一个
// 说不清为什么点不动的东西(本仓反复撞的"没有声音的失败")。
func platformRoleBlockReason(action string, manager bool, principalInactive bool, lastManager bool) string {
	switch {
	case principalInactive:
		return "principal 已停用:先恢复该 principal 再改它的管理权"
	case action == "promote" && manager:
		return "已是管理者"
	case action == "demote" && !manager:
		return "不是管理者"
	case action == "demote" && lastManager:
		return "剩最后一个管理者,不能撤销(服务端同样会拒)"
	}
	return ""
}

func writePlatformRoleForm(w http.ResponseWriter, base, principalID, action, label string, disabled bool, reason string) {
	title := ""
	disabledAttr := ""
	if disabled {
		disabledAttr = " disabled"
		title = ` title="` + esc(reason) + `"`
	}
	_, _ = fmt.Fprintf(w, `<form method=post action="%s"><input type=hidden name=action value=%s><input type=hidden name=principal_id value="%s"><button data-dw-action="%s"%s%s>%s</button></form>`,
		esc(base+"/platform-roles"), esc(action), esc(principalID), esc(action), disabledAttr, title, esc(label))
}

// manageConsolePlatformRole 处理「增删管理者」。
//
// 🔴 fail-closed 三处:取数面缺失 ／ 列举失败 ／ **数不出当前有几个管理者** ⇒ 一律拒。
// 「查不了」不等于「通过」,也不等于「0 个」。
func (s *Server) manageConsolePlatformRole(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.Repo.(consolePlatformRoster)
	if !ok {
		http.Error(w, "management roster unavailable", http.StatusServiceUnavailable)
		return
	}
	action := strings.TrimSpace(r.FormValue("action"))
	principalID := strings.TrimSpace(r.FormValue("principal_id"))
	if principalID == "" || (action != "promote" && action != "demote") {
		http.Error(w, "action must be promote or demote, with principal_id", http.StatusBadRequest)
		return
	}
	principals, err := repo.ListConsolePrincipals(r.Context())
	if err != nil {
		http.Error(w, "management roster unavailable", http.StatusServiceUnavailable)
		return
	}
	var target *model.ConsolePrincipal
	for i := range principals {
		if principals[i].ID == principalID {
			target = &principals[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "principal not found", http.StatusNotFound)
		return
	}
	if !target.Active {
		// 给停用 principal 授管理权是**写了不生效**的悬空授权:
		// ResolveConsolePlatformRole 只对 p.active=1 返回角色 ⇒ 库里像有权、运行时没有。
		http.Error(w, "principal is inactive: reactivate it before changing its management role", http.StatusConflict)
		return
	}
	managers, err := repo.ListActiveConsoleManagement(r.Context())
	if err != nil {
		http.Error(w, "management roster unavailable: refusing to change the roster without an accurate count", http.StatusServiceUnavailable)
		return
	}
	isManager, _ := realManagers(principals, managers) // 🔴 悬空行不算管理者(见 realManagers)
	// 🔴「剩最后一个不能删」—— 板件 REQUIREMENT-REGISTRY-20260921.md:42 逐字。
	// UI 上那个按钮已就地禁用;**服务端仍然拒一遍**(UI 可见性不是授权判据)。
	// 🔴 计数用**真管理者**(见 realManagers):悬空行不授权,把它算进去会放走最后一个真管理者。
	if action == "demote" && isManager[principalID] && len(isManager) <= 1 {
		http.Error(w, "refusing to remove the last management principal: everyone (including you) would lose this entry", http.StatusConflict)
		return
	}
	if err := repo.UpsertConsolePlatformRole(r.Context(), model.ConsolePlatformRole{
		PrincipalID: principalID, Role: "management", Active: action == "promote",
	}); err != nil {
		http.Error(w, "management roster unavailable", http.StatusServiceUnavailable)
		return
	}
	auditAction := "admin_platform_role_promote"
	message := "management granted"
	if action == "demote" {
		auditAction = "admin_platform_role_demote"
		message = "management revoked"
	}
	// 🔴 审计归属(F3):adminUser(r) 现在认门 B 的 console principal,
	// 否则"谁撤销了谁的管理权"在审计里全是无差别的 "admin"。
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), auditAction, principalID)
	writeOK(w, message)
}
