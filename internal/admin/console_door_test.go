package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 🔴 并存期双路一致性闸(Nova-260921-A / B1-b)。
//
// 判据来源(不是我自己定的):
//   - 组长派工第四节:同一能力**两条路的授权判据必须等价**;凡一条路拒绝的,另一条也必须拒绝;
//     **闸要咬住**(任一侧改判据必须红);重点核「从 console tab 能不能做到旧 admin 不让做的事」。
//   - 组长派工第五节:阳格 非 management 打开 console admin tab ⇒ 拒(**四项逐项验**);
//     阴格 management ⇒ 四项都可用;阳格 **构造一处两路分叉** ⇒ 一致性闸必须红;
//     阴格 旧 admin 原有行为**不因本次搬迁而改变**;每格要有**阳性对照**。
//   - 板件 REQUIREMENT-REGISTRY-20260921.md:42「增删管理者(**剩最后一个不能删**)」。
//
// 🔴 G1 的期望值**全部硬写在测试里**,一律**不调用**产品的 adminCapabilityAllowed()。
// 理由(本组新立纪律「先证观测量对变异敏感」):若期望值由产品判据算出,
// 改判据时期望值跟着一起变 ⇒ 闸**永远绿** ⇒ 那是**盲量**,不是闸。

// gateConsoleCookie 与 m8.consoleSessionCookie 是同一个 cookie 名(m8 那个是私有的)。
//
// 🔴 漂移的后果是**响亮失败**,不是假绿:名字若对不上,门 B 收不到凭据
// ⇒ 所有"management 应放行"的格子立刻红,而 TestConsoleDoorFixtureSelfCheck 会**先**红
// 并在消息里点명这条可能。宁可这样,也不要"导出一个常量给测试用"扩大 m8 的公开面。
const gateConsoleCookie = "dw_console_session"

// gateFileID 与 fakeFileBrowser.List 返回的那一条一致(files_test.go)。
const gateFileID = "0123456789abcdef0123456789abcdef"

func gateHashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type authorityClass int

const (
	unauthorized authorityClass = iota // 这道门必须拒
	authorized                         // 这道门必须放行
)

type twoDoorFixture struct {
	srv     *Server
	db      *store.SQLite
	mux     *http.ServeMux
	ctx     context.Context
	now     time.Time
	cookies map[string]*http.Cookie // principal → 已建好的 console 会话 cookie(见 consoleCookie)
}

// newTwoDoorFixture 装配**两道门都在同一个 mux 上**的装置。
//
// 🔴 关键:`admin.New()` 自带的 Prefix 是 **console 鉴权关闭**的 hub(admin.go:60),
// 那样门 B 会恒 404 ⇒ 每一格都"绿"而什么都没验(假绿)。所以这里显式换成开启的 hub,
// 并由 selfCheck 断言"management 确实进得去" —— **装置不成立时先红装置,不产出结论**。
func newTwoDoorFixture(t *testing.T, consoleAuthEnabled bool) *twoDoorFixture {
	return newTwoDoorFixtureMode(t, consoleAuthEnabled, false)
}

func newTwoDoorFixtureMode(t *testing.T, consoleAuthEnabled, newPlatform bool) *twoDoorFixture {
	t.Helper()
	f := &twoDoorFixture{ctx: context.Background(), now: time.Now(), cookies: map[string]*http.Cookie{}}
	db, err := store.OpenSQLite(t.TempDir() + "/two-door.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.db = db

	mode := m8.ConsoleAuthDisabled()
	if consoleAuthEnabled {
		mode, err = m8.ParseConsoleAuthEnabled("true")
		if err != nil {
			t.Fatal(err)
		}
	}

	// principal 形状**刻意含历史/退化形状** —— 板件 9556558b 刚退回 B1-a 的同型错误:
	// 「夹具起点选在了新建组上 ⇒ 绿而无判别力」。只取"刚 promote 的干净 management"
	// 会漏掉真正会出事的那几格。
	f.upsertPrincipal("p-mgmt-1", "管理者一", true)
	f.upsertPlatformRole("p-mgmt-1", "management", true)
	f.upsertPrincipal("p-mgmt-2", "管理者二", true)
	f.upsertPlatformRole("p-mgmt-2", "management", true)
	f.upsertPrincipal("p-owner-default", "普通业主(无角色行)", true) // ⇒ COALESCE 落 owner
	f.upsertPrincipal("p-owner-explicit", "普通业主(显式 owner)", true)
	f.upsertPlatformRole("p-owner-explicit", "owner", true)
	f.upsertPrincipal("p-mgmt-off", "management 已停用", true)
	f.upsertPlatformRole("p-mgmt-off", "management", false)
	f.upsertPlatformRole("p-mgmt-off", "owner", true)
	f.upsertPrincipal("p-mgmt-both", "两行都 active", true)
	f.upsertPlatformRole("p-mgmt-both", "management", true)
	f.upsertPlatformRole("p-mgmt-both", "owner", true)
	// 🔴 停用 principal ＋ active 的 management 行 = **悬空授权**。
	// 它无法经 HTTP 触发(ResolveConsoleSession 要求 p.active=1)⇒ 由名册页那一格覆盖。
	f.upsertPrincipal("p-inactive", "已停用 principal", false)
	f.upsertPlatformRole("p-inactive", "management", true)

	srv := New(db)
	srv.NewPlatform = newPlatform
	srv.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	srv.SecretKey = "gate-secret-key"
	srv.Files = &fakeFileBrowser{}
	srv.Prefix = m8.New(db, mode) // 🔴 必须在 MountConsole 之前装配
	srv.sessions["tok-a"] = "admin"
	mux := http.NewServeMux()
	srv.Mount(mux)
	srv.MountConsole(mux)
	f.srv = srv
	f.mux = mux
	return f
}

func (f *twoDoorFixture) upsertPrincipal(id, display string, active bool) {
	p := model.ConsolePrincipal{ID: id, Provider: "feishu", ProviderIssuer: "app", ProviderSubject: id + "-openid", DisplayName: display, Active: active}
	if err := f.db.UpsertConsolePrincipal(f.ctx, p); err != nil {
		panic(err)
	}
}

func (f *twoDoorFixture) upsertPlatformRole(id, role string, active bool) {
	if err := f.db.UpsertConsolePlatformRole(f.ctx, model.ConsolePlatformRole{PrincipalID: id, Role: role, Active: active}); err != nil {
		panic(err)
	}
}

// consoleCookie 给某个 principal 建一条真实 console 会话并返回它的 cookie。
// 🔴 **必须记忆化**:console_session.token_hash 上有 UNIQUE 约束,
// 同一个 principal 建第二次会直接 panic ⇒ 整个测试二进制挂掉,后面的格子**一条都不跑**
// (「一条假失败吃掉同套件后面所有断言」—— 本组今天已经撞过这个形状)。
func (f *twoDoorFixture) consoleCookie(principalID string) *http.Cookie {
	if c, ok := f.cookies[principalID]; ok {
		return c
	}
	raw := "raw-" + principalID
	session := model.ConsoleSession{
		ID: "sess-" + principalID, PrincipalID: principalID, TokenHash: gateHashToken(raw),
		ExpiresAt: f.now.Add(24 * time.Hour), CreatedAt: f.now,
	}
	if err := f.db.CreateConsoleSession(f.ctx, session, f.now); err != nil {
		panic(err)
	}
	c := &http.Cookie{Name: gateConsoleCookie, Value: raw}
	f.cookies[principalID] = c
	return c
}

func (f *twoDoorFixture) do(t *testing.T, method, path string, cookies []*http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

// ── 装置自证:装置不成立时**先红装置**,不产出任何结论 ─────────────────────────
//
// 🔴 这一格是"拒绝假装验过"的写法:下面所有格子的绿,都建立在
// "management 真的进得去、匿名真的被挡、角色真的解析成我以为的那个"之上。
func TestConsoleDoorFixtureSelfCheck(t *testing.T) {
	f := newTwoDoorFixture(t, true)

	if got, err := f.db.ResolveConsolePlatformRole(f.ctx, "p-mgmt-1"); err != nil || got != "management" {
		t.Fatalf("装置坏:role(p-mgmt-1)=%q err=%v,期望 management —— 下面的矩阵一律不采信", got, err)
	}
	// 🔴 F1 的前提**用真数据证**,不是读码:没有角色行的 principal 解析成 owner。
	// 若这条不成立,"owner 必须被拒"那一格就没有判别力(它本来也进不来)。
	if got, err := f.db.ResolveConsolePlatformRole(f.ctx, "p-owner-default"); err != nil || got != "owner" {
		t.Fatalf("装置坏:role(p-owner-default)=%q err=%v,期望 owner(COALESCE 默认)—— F1 的前提没验到", got, err)
	}
	if got, err := f.db.ResolveConsolePlatformRole(f.ctx, "p-mgmt-off"); err != nil || got != "owner" {
		t.Fatalf("装置坏:role(p-mgmt-off)=%q err=%v,期望 owner(management 行 active=0 不该算)", got, err)
	}
	if got, err := f.db.ResolveConsolePlatformRole(f.ctx, "p-mgmt-both"); err != nil || got != "management" {
		t.Fatalf("装置坏:role(p-mgmt-both)=%q err=%v,期望 management(取最强 active)", got, err)
	}
	if got, err := f.db.ResolveConsolePlatformRole(f.ctx, "p-inactive"); err != nil || got != "" {
		t.Fatalf("装置坏:role(p-inactive)=%q err=%v,期望 \"\"(principal 停用 ⇒ 无角色)", got, err)
	}

	rec := f.do(t, http.MethodGet, "/console/admin", []*http.Cookie{f.consoleCookie("p-mgmt-1")}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("装置坏:management 打不开门 B 索引(code=%d body=%.120q)—— "+
			"若为 401,先怀疑 gateConsoleCookie 与 m8.consoleSessionCookie 漂移(名字对不上 ⇒ 门 B 收不到凭据)",
			rec.Code, rec.Body.String())
	}
	rec = f.do(t, http.MethodGet, "/admin", nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("装置坏:门 A 匿名未被引到登录(code=%d location=%q)", rec.Code, rec.Header().Get("Location"))
	}
	rec = f.do(t, http.MethodGet, "/admin", []*http.Cookie{{Name: "wp_admin", Value: "tok-a"}}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("装置坏:门 A 带 wp_admin 进不去(code=%d)", rec.Code)
	}
}

// ── 矩阵定义 ────────────────────────────────────────────────────────────────

type gateRoute struct {
	capability adminCapability
	method     string
	doorA      string // "" ⇒ 门 A **没有**这条路由(Nova 新增能力)
	doorB      string
	form       url.Values
	// allowWant 是**授权主体命中时期望的确切状态码**。
	// 🔴 不用"非拒即过":那会把 500 读成通过。
	allowWant int
	// post 的 allow 方向若会产生真实副作用,用**早退的无效载荷**探到 handler 即可
	// (400 = 已越过授权层、进入业务校验),既不烧数据又能判别。
	reachesHandler bool
}

func gateRoutes() []gateRoute {
	return []gateRoute{
		{capability: capConsoleIndex, method: http.MethodGet, doorA: "", doorB: "/console/admin", allowWant: http.StatusOK},
		{capability: capFiles, method: http.MethodGet, doorA: "/admin/files", doorB: "/console/admin/files", allowWant: http.StatusOK},
		{capability: capFileContent, method: http.MethodGet, doorA: "/admin/files/" + gateFileID + "/content", doorB: "/console/admin/files/" + gateFileID + "/content", allowWant: http.StatusOK},
		{capability: capProvision, method: http.MethodGet, doorA: "/admin/provision", doorB: "/console/admin/provision", allowWant: http.StatusOK},
		{capability: capAPIKeys, method: http.MethodGet, doorA: "/admin/api-keys", doorB: "/console/admin/api-keys", allowWant: http.StatusOK},
		{capability: capBotChannels, method: http.MethodGet, doorA: "/admin/bot-channels", doorB: "/console/admin/bot-channels", allowWant: http.StatusOK},
		{capability: capPlatformRoles, method: http.MethodGet, doorA: "", doorB: "/console/admin/platform-roles", allowWant: http.StatusOK},
		{capability: capAPIKeys, method: http.MethodPost, doorA: "/admin/api-keys", doorB: "/console/admin/api-keys",
			form: url.Values{}, allowWant: http.StatusBadRequest, reachesHandler: true},
		{capability: capBotChannels, method: http.MethodPost, doorA: "/admin/bot-channels", doorB: "/console/admin/bot-channels",
			form: url.Values{"action": {"delete"}, "id": {"__gate_probe_nonexistent__"}}, allowWant: http.StatusOK, reachesHandler: true},
		{capability: capPlatformRoles, method: http.MethodPost, doorA: "", doorB: "/console/admin/platform-roles",
			form: url.Values{"action": {"promote"}, "principal_id": {"p-mgmt-2"}}, allowWant: http.StatusOK, reachesHandler: true},
	}
}

type gateSubject struct {
	name    string
	cookies func(f *twoDoorFixture) []*http.Cookie
	classA  authorityClass
	classB  authorityClass
	// denyWantA／denyWantB:无权时期望的**确切**状态码。
	// 门 A 恒为 303 → /admin/login;门 B 分 401(没有 console 会话)与 403(有会话但不是 management)。
	denyWantA int
	denyWantB int
}

func adminCookie() *http.Cookie { return &http.Cookie{Name: "wp_admin", Value: "tok-a"} }

func gateSubjects() []gateSubject {
	return []gateSubject{
		{name: "anon", cookies: func(*twoDoorFixture) []*http.Cookie { return nil },
			classA: unauthorized, classB: unauthorized, denyWantA: http.StatusSeeOther, denyWantB: http.StatusUnauthorized},
		{name: "wp_admin_only", cookies: func(*twoDoorFixture) []*http.Cookie { return []*http.Cookie{adminCookie()} },
			// 🔴 派工第三节逐字:「旧 wp_admin cookie **不进** console tab」⇒ 门 B 必须 401。
			classA: authorized, classB: unauthorized, denyWantB: http.StatusUnauthorized},
		// 🔴 console 管理者身份在门 A 上**不算凭据**(反之亦然):两套凭据互不相干。
		{name: "console_management_only", cookies: func(f *twoDoorFixture) []*http.Cookie { return []*http.Cookie{f.consoleCookie("p-mgmt-1")} },
			classA: unauthorized, classB: authorized, denyWantA: http.StatusSeeOther},
		{name: "both_credentials", cookies: func(f *twoDoorFixture) []*http.Cookie {
			return []*http.Cookie{adminCookie(), f.consoleCookie("p-mgmt-1")}
		}, classA: authorized, classB: authorized},
		{name: "console_owner_default_no_role_row", cookies: func(f *twoDoorFixture) []*http.Cookie { return []*http.Cookie{f.consoleCookie("p-owner-default")} },
			classA: unauthorized, classB: unauthorized, denyWantA: http.StatusSeeOther, denyWantB: http.StatusForbidden},
		{name: "console_owner_explicit", cookies: func(f *twoDoorFixture) []*http.Cookie { return []*http.Cookie{f.consoleCookie("p-owner-explicit")} },
			classA: unauthorized, classB: unauthorized, denyWantA: http.StatusSeeOther, denyWantB: http.StatusForbidden},
		{name: "console_management_deactivated", cookies: func(f *twoDoorFixture) []*http.Cookie { return []*http.Cookie{f.consoleCookie("p-mgmt-off")} },
			classA: unauthorized, classB: unauthorized, denyWantA: http.StatusSeeOther, denyWantB: http.StatusForbidden},
		{name: "console_management_and_owner_rows", cookies: func(f *twoDoorFixture) []*http.Cookie { return []*http.Cookie{f.consoleCookie("p-mgmt-both")} },
			classA: unauthorized, classB: authorized, denyWantA: http.StatusSeeOther},
	}
}

// ── G1 授权矩阵 ─────────────────────────────────────────────────────────────

func TestG1ConsoleDoorAuthorizationMatrix(t *testing.T) {
	f := newTwoDoorFixture(t, true)
	for _, route := range gateRoutes() {
		for _, subject := range gateSubjects() {
			for _, door := range []string{"A", "B"} {
				path := route.doorA
				class, denyWant := subject.classA, subject.denyWantA
				if door == "B" {
					path, class, denyWant = route.doorB, subject.classB, subject.denyWantB
				}
				if path == "" {
					continue
				}
				name := door + "/" + string(route.capability) + "/" + route.method + "/" + subject.name
				t.Run(name, func(t *testing.T) {
					rec := f.do(t, route.method, path, subject.cookies(f), route.form)
					if class == authorized {
						if rec.Code != route.allowWant {
							t.Fatalf("判据:有权主体必须放行到 handler。门 %s 能力 %s 主体 %s ⇒ code=%d want=%d body=%.160q",
								door, route.capability, subject.name, rec.Code, route.allowWant, gateBodySnippet(route, rec))
						}
						return
					}
					if rec.Code != denyWant {
						t.Fatalf("判据:无权主体必须被拒(确切码)。门 %s 能力 %s 主体 %s ⇒ code=%d want=%d body=%.160q",
							door, route.capability, subject.name, rec.Code, denyWant, gateBodySnippet(route, rec))
					}
					if door == "A" && rec.Header().Get("Location") != "/admin/login" {
						t.Fatalf("门 A 拒绝必须引到登录页(既有行为),location=%q", rec.Header().Get("Location"))
					}
				})
			}
		}
	}
}

// gateBodySnippet 🔴 **凭证纪律**(派工第四节③,无例外):
// api-keys 那格**绝不打印响应体** —— 业主已定"明文显示",该页字节 ≡ 凭据本身。
// 失败消息里只给长度与状态码,不给内容。
func gateBodySnippet(route gateRoute, rec *httptest.ResponseRecorder) string {
	if route.capability == capAPIKeys && route.method == http.MethodGet {
		return "<redacted: api-keys page body is credential material>"
	}
	return rec.Body.String()
}

// 🔴 门 A 上**不存在**那两个新增能力:任何主体(含 wp_admin)都拿不到。
// 这一格把"platformRoles 没有双路对照面"这件事**钉成断言**,而不是只写在注释里。
func TestG1DoorAHasNoConsoleOnlyCapabilities(t *testing.T) {
	f := newTwoDoorFixture(t, true)
	for _, path := range []string{"/admin/platform-roles", "/admin/console-admin", "/admin"} {
		t.Run(path, func(t *testing.T) {
			if path == "/admin" {
				rec := f.do(t, http.MethodGet, path, []*http.Cookie{adminCookie()}, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("门 A 状态页必须仍然可用(并存,不是迁移):code=%d", rec.Code)
				}
				return
			}
			for _, subject := range gateSubjects() {
				rec := f.do(t, http.MethodGet, path, subject.cookies(f), nil)
				if rec.Code != http.StatusNotFound {
					t.Fatalf("门 A 上不该有 %s(主体 %s):code=%d want=404", path, subject.name, rec.Code)
				}
			}
		})
	}
}

// ── G2 双路一致性(派工点名的核心)────────────────────────────────────────────
//
// 🔴 判据不是"都能用",而是**凡一条路拒绝的,另一条路也必须拒绝**。
// 做法:把主体按"这道门上有权/无权"归类,然后要求**同一个能力**在两道门上
// 对同一权限类别给出**同一种结局**(放行 / 拒绝)。
// 🔴 结局由**观测到的状态码**判定,不由产品判据推出。
func TestG2TwoDoorEquivalence(t *testing.T) {
	f := newTwoDoorFixture(t, true)
	denied := func(code int) bool {
		switch code {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusSeeOther, http.StatusMethodNotAllowed:
			return true
		}
		return false
	}
	for _, route := range gateRoutes() {
		if route.doorA == "" || route.doorB == "" {
			continue // 单路能力:没有对照面,由 G1/G5 各自守(见 TestG1DoorAHasNoConsoleOnlyCapabilities)
		}
		for _, class := range []authorityClass{authorized, unauthorized} {
			t.Run(string(route.capability)+"/"+route.method+"/"+[]string{"unauthorized", "authorized"}[class], func(t *testing.T) {
				checked := 0
				for _, subject := range gateSubjects() {
					wantClass := class
					recA := f.do(t, route.method, route.doorA, subject.cookies(f), route.form)
					recB := f.do(t, route.method, route.doorB, subject.cookies(f), route.form)
					if subject.classA != wantClass || subject.classB != wantClass {
						continue // 只比"两道门上属于同一权限类别"的主体
					}
					checked++
					if denied(recA.Code) != denied(recB.Code) {
						t.Fatalf("🔴 两路分叉:能力 %s %s 主体 %s ⇒ 门 A code=%d(%v) 门 B code=%d(%v)。\n"+
							"判据:凡一条路拒绝的,另一条路也必须拒绝(组长派工第四节①)。",
							route.capability, route.method, subject.name, recA.Code, denied(recA.Code), recB.Code, denied(recB.Code))
					}
				}
				if checked == 0 {
					t.Fatalf("这一格一个主体都没比到 ⇒ 绿而无判别力(装置或分类写错了)")
				}
			})
		}
	}
}

// ── G3b 越权面:门 A 有的,门 B 不得顺手多给 ──────────────────────────────────
//
// 🔴 派工第四节①「重点核**从 console tab 能不能做到旧 admin 不让做的事**」。
// 做法:**从真实源码**取门 A 的路由清单(见 wiring 测试里的 doorARoutePatternsFromSource),
// 对每一条求出它的门 B 孪生路径,凡不在业主确认范围内的 ⇒ **必须 404/405**。
// 🔵 用真实清单而不是手写清单:将来有人在 `Mount()` 里加一条 `/admin/newthing`,
// 本格会**自动**去探 `/console/admin/newthing` —— 不需要有人记得来改测试。
func TestG3ConsoleDoorExposesOnlyOwnerConfirmedScope(t *testing.T) {
	f := newTwoDoorFixture(t, true)
	// 🔴 allowed 从**门 B 的真实路由表**取(不是从 gateRoutes 那份测试清单),
	// 这样本格验的是"门 A 的每一条,其门 B 孪生要么在真实表里、要么被拒";
	// 而"真实表 == 业主确认范围"由 TestG3aConsoleDoorRouteTableIsExactlyOwnerScope 单独钉。
	allowed := map[string]bool{}
	for _, route := range f.srv.consoleDoorRoutes() {
		probe := gateSubstituteWildcards(route.pattern)
		if route.get != nil {
			allowed[http.MethodGet+" "+probe] = true
		}
		if route.post != nil {
			allowed[http.MethodPost+" "+probe] = true
		}
	}
	probed := 0
	for _, pattern := range doorARoutePatternsFromSource(t) {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok {
			continue
		}
		if !strings.HasPrefix(path, "/admin/") && path != "/admin" {
			continue
		}
		twin := consoleDoorPrefix + strings.TrimPrefix(path, adminDoorPrefix)
		if twin == consoleDoorPrefix+"/login" {
			continue // 门 B 没有自己的登录页(不该有:业主不需要在 console 里再登一次)
		}
		probed++
		// 🔴 路径里的 {wildcard} 用探针值替掉,否则请求打不到真实路由上,404 就没有意义
		// (而 404 正是本格想看到的 ⇒ 不替换 = **本格永远绿**,那是假绿)。
		probe := gateSubstituteWildcards(twin)
		if allowed[method+" "+probe] {
			continue
		}
		t.Run(method+" "+probe, func(t *testing.T) {
			rec := f.do(t, method, probe, []*http.Cookie{adminCookie(), f.consoleCookie("p-mgmt-1")}, nil)
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("🔴 越权面:门 B 暴露了业主未确认的能力 %s %s ⇒ code=%d。\n"+
					"判据:门 B 只能有业主确认的那几项(板件 BATCH-B1-CONTENT-20260921.md §六-2)。\n"+
					"🔴 /admin/agent-groups 尤其不得出现(派工逐字:被 B1-a 替代,不是搬迁)。",
					method, probe, rec.Code)
			}
		})
	}
	if probed < 20 {
		t.Fatalf("只探到 %d 条门 A 路由的孪生路径 —— 少于预期(门 A 有 40+ 条)。\n"+
			"这说明 doorARoutePatternsFromSource 没读到真实清单 ⇒ 本格的绿没有判别力", probed)
	}
	// 🔴 点名格:派工逐字要求"不搬"的那一条,单独钉一次(不依赖上面的遍历)。
	for _, probe := range []string{"/console/admin/agent-groups", "/console/admin/members", "/console/admin/config", "/console/admin/cleanup"} {
		rec := f.do(t, http.MethodGet, probe, []*http.Cookie{adminCookie(), f.consoleCookie("p-mgmt-1")}, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("🔴 %s 必须 404(不在业主确认的搬迁范围内),实际 code=%d", probe, rec.Code)
		}
	}
}

// ── G4a 门 A／门 B 各自的 URL 都指向自己那道门(F2)───────────────────────────
//
// 🔴 F2 实测:在范围内的 4 个 handler 原本发出 **10 处** `/admin/...` 绝对 URL,
// 业主在 console 里没有 wp_admin cookie ⇒ 门 B 页面上每个按钮都会 303 到 /admin/login
// ⇒ 「页面能看、按钮全废」。改成按门解析前缀后,本格钉住它不再回退。
var gateAbsoluteAdminURL = regexp.MustCompile(`(?:action|href)\s*=\s*"?/admin(?:/|")`)

func TestG4DoorPagesLinkInsideTheirOwnDoor(t *testing.T) {
	f := newTwoDoorFixture(t, true)
	mgmt := []*http.Cookie{f.consoleCookie("p-mgmt-1")}
	pages := []gatePage{
		{path: "/admin/files", cookies: []*http.Cookie{adminCookie()}, mustContain: []string{
			`href="/admin">返回`, `href="/admin/files/` + gateFileID + `/content"`}},
		{path: "/admin/provision", cookies: []*http.Cookie{adminCookie()}, mustContain: []string{
			`href="/admin">返回后台首页`, `action=/admin/provision`}},
		{path: "/admin/bot-channels", cookies: []*http.Cookie{adminCookie()}, mustContain: []string{
			`action=/admin/bot-channels`}},
		{path: "/console/admin", cookies: mgmt, mustContain: []string{
			`href="/console/admin/files"`, `href="/console/admin/provision"`, `href="/console/admin/api-keys"`,
			`href="/console/admin/bot-channels"`, `href="/console/admin/platform-roles"`,
			`data-dw-placeholder="knowledge"`, `data-dw-placeholder="skill"`, `data-dw-placeholder="mcp"`}},
		{path: "/console/admin/files", cookies: mgmt, mustContain: []string{
			`href="/console/admin">返回`, `href="/console/admin/files/` + gateFileID + `/content"`}},
		{path: "/console/admin/provision", cookies: mgmt, mustContain: []string{
			`href="/console/admin">返回后台首页`, `action=/console/admin/provision`}},
		{path: "/console/admin/bot-channels", cookies: mgmt, mustContain: []string{
			`action=/console/admin/bot-channels`}},
		{path: "/console/admin/platform-roles", cookies: mgmt, mustContain: []string{
			`action="/console/admin/platform-roles"`, `data-dw-action="promote"`, `data-dw-action="demote"`}},
	}
	for _, page := range pages {
		t.Run(page.path, func(t *testing.T) {
			rec := f.do(t, http.MethodGet, page.path, page.cookies, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("装置坏:%s ⇒ code=%d(期望 200),本页结论不采信", page.path, rec.Code)
			}
			if page.capabilityIsAPIKeys() {
				return // 🔴 凭证纪律:api-keys 页不参与任何正文断言/打印
			}
			body := rec.Body.String()
			if strings.HasPrefix(page.path, "/console/admin") {
				if gateAbsoluteAdminURL.MatchString(body) {
					t.Fatalf("🔴 门 B 页面里有指向**门 A**的绝对 URL(%s)。\n"+
						"后果:业主在 console 里没有 wp_admin cookie ⇒ 这个按钮会 303 到 /admin/login\n"+
						"⇒「页面能看、按钮全废」。判据:门 B 页面里的 action/href 必须都在 /console/admin 下。",
						gateFirstMatch(body))
				}
				for _, want := range page.mustContain {
					if !strings.Contains(body, want) {
						t.Fatalf("🔴 %s 里少了 %q ⇒ 门 B 的链接/表单没指向自己这道门。\n"+
							"后果:业主在 console 里没有 wp_admin cookie,落到门 A 就是 303 → /admin/login\n"+
							"⇒「页面能看、按钮全废」。", page.path, want)
					}
				}
				return
			}
			if strings.Contains(body, consoleDoorPrefix) {
				t.Fatalf("🔴 门 A 页面里出现了 %q ⇒ base 前缀解析错了,回退口被改坏。判据:门 A 输出必须与搬迁前逐字同形。", consoleDoorPrefix)
			}
			// 🔴 **正向**也要断:每一处该指向自己那道门的 URL 都必须真的在。
			// 只断"没有指向对面"是不够的 —— 把 URL 整个删掉也能满足它,
			// 而那正是「页面能看、按钮全废」的另一种写法。
			for _, want := range page.mustContain {
				if !strings.Contains(body, want) {
					t.Fatalf("🔴 %s 里少了 %q ⇒ 这道门的链接/表单没指向自己。\n"+
						"判据:页面里每一处 action/href 都必须落在**本页所属那道门**的前缀下。", page.path, want)
				}
			}
		})
	}
	// 🔴 api-keys 两页**单独**验前缀,但只看状态码与"是否含门 A 绝对 URL"的**布尔结果**,
	// 不打印正文、不断言正文内容(凭证纪律)。
	for _, path := range []string{"/admin/api-keys", "/console/admin/api-keys"} {
		cookies := []*http.Cookie{adminCookie()}
		if strings.HasPrefix(path, "/console") {
			cookies = []*http.Cookie{f.consoleCookie("p-mgmt-1")}
		}
		rec := f.do(t, http.MethodGet, path, cookies, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s ⇒ code=%d", path, rec.Code)
		}
		body := rec.Body.String()
		leak := gateAbsoluteAdminURL.MatchString(body)
		if strings.HasPrefix(path, "/console") && leak {
			t.Fatalf("🔴 门 B 凭证页里有指向门 A 的绝对 URL(正文不打印)。判据同 G4。")
		}
		if !strings.HasPrefix(path, "/console") && strings.Contains(body, consoleDoorPrefix) {
			t.Fatalf("🔴 门 A 凭证页里出现了 %q(正文不打印)", consoleDoorPrefix)
		}
	}
}

// gatePage 是 G4a 要逐页核前缀的一个页面。
type gatePage struct {
	path        string
	cookies     []*http.Cookie
	mustContain []string // 该页**必须**出现的 URL 片段(正向断言,防"把链接删掉也算过")
}

// capabilityIsAPIKeys 🔴 凭证页走**单独**那条分支:只看布尔结果,不打印正文、不断言正文内容
// (派工第四节③:凭证页字节 ≡ 凭据本身,不得进板件/消息/交付件)。
func (p gatePage) capabilityIsAPIKeys() bool { return strings.Contains(p.path, "api-keys") }

func gateFirstMatch(body string) string {
	loc := gateAbsoluteAdminURL.FindStringIndex(body)
	if loc == nil {
		return ""
	}
	start := loc[0] - 40
	if start < 0 {
		start = 0
	}
	end := loc[1] + 40
	if end > len(body) {
		end = len(body)
	}
	return body[start:end]
}

// ── G5 最后一个管理者不能删(板件 REQUIREMENT-REGISTRY-20260921.md:42 逐字)──────

// gateRealManagers **在测试里独立算一遍**"此刻真能行使管理权的人"。
// 🔴 不调用产品的 realManagers():期望值若由被测代码算出,改代码时期望值跟着变 ⇒ **盲量**。
// 判据(独立来源):console_platform_role 里 role=management 且 active=1,
// **且** console_principal.active=1(因为 ResolveConsolePlatformRole 的 SQL 带 p.active=1)。
func gateRealManagers(t *testing.T, f *twoDoorFixture) []string {
	t.Helper()
	rows, err := f.db.ListActiveConsoleManagement(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	principals, err := f.db.ListConsolePrincipals(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]bool{}
	for _, p := range principals {
		if p.Active {
			active[p.ID] = true
		}
	}
	out := []string{}
	for _, id := range rows {
		if active[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (f *twoDoorFixture) demote(t *testing.T, actor, target string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, http.MethodPost, "/console/admin/platform-roles",
		[]*http.Cookie{f.consoleCookie(actor)},
		url.Values{"action": {"demote"}, "principal_id": {target}})
}

func TestG5LastManagementCannotBeRemoved(t *testing.T) {
	// 装置里"真管理者"有三个:p-mgmt-1 · p-mgmt-2 · p-mgmt-both。
	// 🔴 p-inactive 库里有 active 的 management 行,但 principal 已停用 ⇒ **不算**(见 gateRealManagers)。
	t.Run("装置自证:真管理者恰好三个,悬空那条不算", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		got := gateRealManagers(t, f)
		want := []string{"p-mgmt-1", "p-mgmt-2", "p-mgmt-both"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("装置坏:真管理者=%v want=%v —— 下面几格的前提不成立,结论不采信", got, want)
		}
	})

	t.Run("只剩一个时撤销必须被拒", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		for _, target := range []string{"p-mgmt-2", "p-mgmt-both"} {
			if rec := f.demote(t, "p-mgmt-1", target); rec.Code != http.StatusOK {
				t.Fatalf("装置坏:三个真管理者时撤销 %s 应成功,code=%d body=%.200q", target, rec.Code, rec.Body.String())
			}
		}
		if got := gateRealManagers(t, f); len(got) != 1 || got[0] != "p-mgmt-1" {
			t.Fatalf("装置坏:真管理者=%v,期望只剩 p-mgmt-1", got)
		}
		rec := f.demote(t, "p-mgmt-1", "p-mgmt-1")
		if rec.Code != http.StatusConflict {
			t.Fatalf("🔴 判据:撤掉最后一个真管理者必须被拒(want 409,got %d body=%.200q)。\n"+
				"后果:撤掉后**所有人**(含操作者自己)都失去这个入口,而全仓没有别的运行时路径能加回来。",
				rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "last management") {
			t.Fatalf("拒绝消息必须说清**为什么**与后果(判据写在它生效的地方),body=%.200q", rec.Body.String())
		}
		if got := gateRealManagers(t, f); len(got) != 1 || got[0] != "p-mgmt-1" {
			t.Fatalf("被拒后库里不该变:真管理者=%v", got)
		}
	})

	t.Run("🔴 悬空行不得被当成「还有人管」——最后一个真管理者仍必须被保住", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		// 撤到只剩 p-mgmt-1(真)＋ p-inactive(悬空)。
		for _, target := range []string{"p-mgmt-2", "p-mgmt-both"} {
			if rec := f.demote(t, "p-mgmt-1", target); rec.Code != http.StatusOK {
				t.Fatalf("装置坏:code=%d body=%.200q", rec.Code, rec.Body.String())
			}
		}
		rows, err := f.db.ListActiveConsoleManagement(f.ctx)
		if err != nil || len(rows) != 2 {
			t.Fatalf("装置坏:management 行数=%d err=%v,期望 2(p-mgmt-1 ＋ 悬空的 p-inactive)", len(rows), err)
		}
		// 🔴 这一格是本闸第一跑咬出来的真缺陷:若按**行数**计数,这里是 2 ⇒ 放行 ⇒
		// 最后一个真管理者被撤掉,而 p-inactive 那条悬空行**不授权** ⇒ 自我锁死。
		rec := f.demote(t, "p-mgmt-1", "p-mgmt-1")
		if rec.Code != http.StatusConflict {
			t.Fatalf("🔴 判据:计数必须按**真管理者**,不能把停用 principal 上的悬空 management 行算进去。\n"+
				"got %d want 409 body=%.200q", rec.Code, rec.Body.String())
		}
	})

	t.Run("多个真管理者时撤销其中一个必须成功,且当场生效", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		rec := f.demote(t, "p-mgmt-1", "p-mgmt-2")
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%.200q", rec.Code, rec.Body.String())
		}
		got := gateRealManagers(t, f)
		if strings.Join(got, ",") != "p-mgmt-1,p-mgmt-both" {
			t.Fatalf("真管理者=%v want [p-mgmt-1 p-mgmt-both]", got)
		}
		// 🔴 被撤的人**当场就进不来了** —— 不是"库里改了但界面还开着"。
		rec = f.do(t, http.MethodGet, "/console/admin", []*http.Cookie{f.consoleCookie("p-mgmt-2")}, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("被撤销管理权后必须立刻 403,got %d", rec.Code)
		}
	})

	t.Run("数不出当前有几个管理者时必须拒(fail-closed)", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		before, err := f.db.ListActiveConsoleManagement(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		f.srv.Repo = &brokenRosterCount{Repository: f.db}
		rec := f.demote(t, "p-mgmt-1", "p-mgmt-2")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("🔴 判据:数不出管理者个数时必须**拒**(want 503,got %d body=%.200q)。\n"+
				"「查不了」不等于「通过」,也不等于「还有很多个,删一个没关系」。",
				rec.Code, rec.Body.String())
		}
		after, err := f.db.ListActiveConsoleManagement(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(before, ",") != strings.Join(after, ",") {
			t.Fatalf("被拒后库里不该变:before=%v after=%v", before, after)
		}
	})

	t.Run("给停用 principal 授管理权必须被拒(悬空授权)", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		rec := f.do(t, http.MethodPost, "/console/admin/platform-roles",
			[]*http.Cookie{f.consoleCookie("p-mgmt-1")},
			url.Values{"action": {"promote"}, "principal_id": {"p-inactive"}})
		if rec.Code != http.StatusConflict {
			t.Fatalf("want 409(停用 principal 授了也不生效 ⇒ 库里像有权、运行时没有),got %d body=%.200q",
				rec.Code, rec.Body.String())
		}
	})

	t.Run("名册页必须把悬空授权与「为什么点不动」都显示出来", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		page := func() string {
			rec := f.do(t, http.MethodGet, "/console/admin/platform-roles", []*http.Cookie{f.consoleCookie("p-mgmt-1")}, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d", rec.Code)
			}
			return rec.Body.String()
		}
		body := page()
		if !strings.Contains(body, `data-dw-dangling="p-inactive"`) {
			t.Fatalf("p-inactive 上挂着 active 的 management 行,名册页必须把它标成悬空\n" +
				"(否则库里像「他是管理者」、运行时像「他不是」,两边都没错却没人看得见)")
		}
		for _, target := range []string{"p-mgmt-2", "p-mgmt-both"} {
			if rec := f.demote(t, "p-mgmt-1", target); rec.Code != http.StatusOK {
				t.Fatalf("装置坏:code=%d", rec.Code)
			}
		}
		body = page()
		if !strings.Contains(body, "剩最后一个管理者") {
			t.Fatalf("只剩一个真管理者时,页面必须写明为什么撤销按钮点不动(判据写在它生效的地方)")
		}
	})
}

// brokenRosterCount 让"数当前有几个管理者"失败,用来验 fail-closed。
type brokenRosterCount struct {
	store.Repository
}

func (b *brokenRosterCount) ListActiveConsoleManagement(context.Context) ([]string, error) {
	return nil, io.ErrUnexpectedEOF
}

// ── G6 审计归属(F3)─────────────────────────────────────────────────────────

func TestG6AuditAttribution(t *testing.T) {
	t.Run("门 B 的管理动作必须记下是谁干的", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		rec := f.do(t, http.MethodPost, "/console/admin/platform-roles",
			[]*http.Cookie{f.consoleCookie("p-mgmt-1")},
			url.Values{"action": {"demote"}, "principal_id": {"p-mgmt-2"}})
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%.200q", rec.Code, rec.Body.String())
		}
		rows, err := f.db.ListRecentAudit(f.ctx, "admin_platform_role_", 10)
		if err != nil || len(rows) != 1 {
			t.Fatalf("审计行 err=%v rows=%d want 1", err, len(rows))
		}
		if rows[0].Actor != "console_principal:p-mgmt-1" {
			t.Fatalf("🔴 判据:门 B 的管理动作必须归属到 principal。actor=%q want=console_principal:p-mgmt-1\n"+
				"后果:改之前它会是无差别的 \"admin\" ⇒「谁撤销了谁的管理权」查不到。", rows[0].Actor)
		}
		if rows[0].Action != "admin_platform_role_demote" || rows[0].Target != "p-mgmt-2" {
			t.Fatalf("action=%q target=%q", rows[0].Action, rows[0].Target)
		}
	})

	t.Run("门 A 的审计归属形状不得因本次改动而变", func(t *testing.T) {
		f := newTwoDoorFixture(t, true)
		rec := f.do(t, http.MethodPost, "/admin/bot-channels", []*http.Cookie{adminCookie()},
			url.Values{"action": {"delete"}, "id": {"__gate_probe_nonexistent__"}})
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%.200q", rec.Code, rec.Body.String())
		}
		rows, err := f.db.ListRecentAudit(f.ctx, "admin_bot_channel_delete", 10)
		if err != nil || len(rows) != 1 {
			t.Fatalf("审计行 err=%v rows=%d want 1", err, len(rows))
		}
		if !strings.HasPrefix(rows[0].Actor, "admin_session:") || strings.Contains(rows[0].Actor, "console_principal") {
			t.Fatalf("🔴 门 A 的 actor 形状必须与改动前一致(admin_session:<token 前 8 位>),got %q", rows[0].Actor)
		}
		if rows[0].Actor != "admin_session:tok-a" {
			t.Fatalf("actor=%q want=admin_session:tok-a", rows[0].Actor)
		}
	})
}

// ── console 鉴权开关关闭 ⇒ 门 B 整体关死,门 A 仍是回退口 ─────────────────────
//
// 🔴 这一格是"部署后、开关打开前"与"回滚态"的形状。业主逐字要「有一个回退的地方」,
// 所以:开关关掉时**门 B 必须一个都进不去**,而**门 A 必须照常**。
func TestG1ConsoleAuthDisabledClosesDoorBOnly(t *testing.T) {
	f := newTwoDoorFixture(t, false)
	for _, route := range gateRoutes() {
		rec := f.do(t, route.method, route.doorB, []*http.Cookie{f.consoleCookie("p-mgmt-1")}, route.form)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("开关关闭时门 B %s %s 必须 404(不泄露\"这里有个管理入口\"),got %d", route.method, route.doorB, rec.Code)
		}
	}
	for _, path := range []string{"/admin/files", "/admin/provision", "/admin/bot-channels"} {
		rec := f.do(t, http.MethodGet, path, []*http.Cookie{adminCookie()}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("开关关闭时门 A(回退口)%s 必须照常 200,got %d", path, rec.Code)
		}
	}
}

// gateSubstituteWildcards 把路由模式里的 {wildcard} 换成能真打到路由上的探针值。
// 🔴 不替换的话请求会 404,而 404 正是"越权面"那一格**想看到**的结果 ⇒ 本格会永远绿(假绿)。
func gateSubstituteWildcards(pattern string) string {
	return strings.NewReplacer(
		"{id}", gateFileID, "{specID}", "gate", "{itemID}", "gate",
		"{owner}", "gate", "{keyword}", "gate", "{name}", "gate", "{rest}", "gate",
	).Replace(pattern)
}

// ── G4c 导航页不许有**第二份清单**,也不许有死链 ──────────────────────────────
//
// 🔴 本格是变异 M17 咬出来的缺口:往 consoleAdminIndex 的 HTML 里手写一条
// `<a href="/console/admin/members">成员配置</a>` ⇒ 全仓 **rc=0、零转红**(实测),
// 而那条链接**打不开**(门 B 没有 members 路由 ⇒ 404)。两个后果都是本组今日反复撞的形状:
//
//	① 死链:业主点一下 404,会当成故障反复点(板件 B1-a 今日已因「点了没反应」卡过一次)
//	② 第二份清单:console_door.go 的头注**声称**「条目由路由表生成,不写第二份清单」,
//	   而 M17 证明那句话当时**没有任何闸在守** ⇒ 描述与实现不符(本线今日同型第五次)
//
// 判据(期望值**独立**从路由表算,不从页面读 ⇒ 不是盲量):
//
//	① 页面上每一个 href 都必须是路由表里"该单列"的那条,**一条不多、一条不少、不重复**
//	② 每一个 href 都**真打得开**(management 身份 GET ⇒ 200)
//
// 🔵 两腿各自被**单独**证明有牙(实测,不是推论):
//
//	M17(手写一条 /console/admin/members 链接)⇒ ①红:「不在路由表里」。
//	   🔴 此时②**没跑到**(①用 t.Fatalf 先中止)⇒ 不能说"②也验过了"。
//	M18(入口留在表里、但 MountConsole 不注册它的 GET)⇒ ②红:「死链,code=405」。
//	   🔵 是 405 不是 404:该 pattern 的 POST 仍在 ⇒ Go 的 mux 回 405。
//	   本格判据是"打不开"(≠200),405 与 404 同样算 —— 业主点下去都不是页面。
var gateIndexHref = regexp.MustCompile(`href="([^"]+)"`)

func TestGateConsoleAdminIndexHasNoSecondListAndNoDeadLink(t *testing.T) {
	f := newTwoDoorFixture(t, true)
	rec := f.do(t, http.MethodGet, consoleDoorPrefix, []*http.Cookie{f.consoleCookie("p-mgmt-1")}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("装置坏:导航页 ⇒ code=%d(期望 200),本格结论不采信", rec.Code)
	}
	want := map[string]bool{}
	for _, route := range f.srv.consoleDoorRoutes() {
		if route.title == "" || route.get == nil {
			continue // 与导航页同一条口径:不单列的不该有入口
		}
		want[route.pattern] = true
	}
	if len(want) != 5 {
		t.Fatalf("前提坏了:路由表里该单列的应是 5 项(文件对象·Provision·凭证·机器人通道·管理者名单),实为 %d ⇒ 本格结论不采信", len(want))
	}
	seen := map[string]bool{}
	for _, match := range gateIndexHref.FindAllStringSubmatch(rec.Body.String(), -1) {
		href := match[1]
		if seen[href] {
			t.Fatalf("🔴 导航页里 %s 出现两次 ⇒ 同一能力两个入口,UI 与闸都无法唯一寻址", href)
		}
		seen[href] = true
		if !want[href] {
			t.Fatalf("🔴 导航页有一条**不在路由表里**的链接:%s。\n"+
				"判据:条目由 consoleDoorRoutes() 生成,不许手写第二份清单(两份必漂移)。\n"+
				"后果:这类链接通常指向门 B 没有的路由 ⇒ 业主点下去 404,会当成故障反复点。", href)
		}
	}
	for href := range want {
		if !seen[href] {
			t.Fatalf("🔴 路由表里该单列的 %s 在导航页上**没有入口** ⇒ 能力在、业主看不见(等于没搬)", href)
		}
	}
	for href := range seen {
		probe := f.do(t, http.MethodGet, href, []*http.Cookie{f.consoleCookie("p-mgmt-1")}, nil)
		if probe.Code != http.StatusOK {
			t.Fatalf("🔴 导航页上的死链:%s ⇒ management 身份 GET code=%d(期望 200)。\n"+
				"判据:导航页上每一条链接都必须真能打开 —— 点不动的入口等于没有入口。", href, probe.Code)
		}
	}
}

func TestNewPlatformConsoleDoorHidesLegacyIdentityAndRolePages(t *testing.T) {
	f := newTwoDoorFixtureMode(t, true, true)
	mgmt := []*http.Cookie{f.consoleCookie("p-mgmt-1")}
	index := f.do(t, http.MethodGet, consoleDoorPrefix, mgmt, nil)
	if index.Code != http.StatusOK {
		t.Fatalf("new-platform console admin index=%d", index.Code)
	}
	for _, path := range []string{"/console/admin/provision", "/console/admin/api-keys", "/console/admin/platform-roles"} {
		if strings.Contains(index.Body.String(), `href="`+path+`"`) {
			t.Errorf("new-platform index leaked legacy link %s", path)
		}
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := f.do(t, method, path, mgmt, nil)
			if rec.Code != http.StatusNotFound {
				t.Errorf("new-platform %s %s=%d, want 404", method, path, rec.Code)
			}
		}
	}
}
