package admin

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"net/http"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 🔴 装配闸(Nova-260921-A / B1-b)。
//
// 运行时闸(console_door_test.go)验的是**行为**;本件验的是**装配**,两者不互相替代:
//   - G7 若门 B 用的是**另一份 handler**,运行时矩阵照样可以全绿,而两条路的行为已经分叉了。
//     "共用同一批 handler"是本设计里唯一一条**结构性**的防分叉保证 ⇒ 必须有闸钉住它。
//   - 范围防蔓延:把某条门 A 路由错误地套上能力表(我自己在这一格上超围过一次:
//     `/admin/members` **不在**业主确认的搬迁范围里),运行时闸看不出来 —— 它照样 200。

type doorARoute struct {
	pattern    string // "GET /admin/files"
	wrapper    string // requireAuth | requireAuthCap
	capability string // requireAuthCap 的第一个实参;requireAuth 时为 ""
	handler    string // 末端 handler 的方法名
}

// doorARoutePatternsFromSource 从**真实源码**取门 A 的路由清单。
// 🔵 用手写清单的话,将来有人在 `Mount()` 里加一条 `/admin/newthing`,
// 越权面那一格(G3b)就**不会**去探它的门 B 孪生路径 ⇒ 靠记得 = 没有。
func doorARoutePatternsFromSource(t *testing.T) []string {
	t.Helper()
	table := doorARouteTableFromSource(t)
	out := make([]string, 0, len(table))
	for _, route := range table {
		out = append(out, route.pattern)
	}
	return out
}

func doorARouteTableFromSource(t *testing.T) map[string]doorARoute {
	t.Helper()
	fset := gotoken.NewFileSet()
	file, err := parser.ParseFile(fset, "admin.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	table := map[string]doorARoute{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "Mount" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HandleFunc" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != gotoken.STRING {
				return true
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			route := doorARoute{pattern: pattern}
			if wrapper, ok := call.Args[1].(*ast.CallExpr); ok {
				if wsel, ok := wrapper.Fun.(*ast.SelectorExpr); ok {
					route.wrapper = wsel.Sel.Name
				}
				for i, arg := range wrapper.Args {
					if asel, ok := arg.(*ast.SelectorExpr); ok {
						route.handler = asel.Sel.Name // 末端 handler
					}
					if i == 0 {
						if aid, ok := arg.(*ast.Ident); ok {
							route.capability = aid.Name
						}
					}
				}
			} else if hsel, ok := call.Args[1].(*ast.SelectorExpr); ok {
				route.handler = hsel.Sel.Name
			}
			table[pattern] = route
			return true
		})
	}
	if len(table) < 40 {
		t.Fatalf("只从 admin.go 的 Mount() 读到 %d 条路由(应有 40+)⇒ 解析没读到真实清单,本件结论不采信", len(table))
	}
	return table
}

func handlerNameOf(fn http.HandlerFunc) string {
	if fn == nil {
		return ""
	}
	full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	if i := strings.LastIndex(full, ")."); i >= 0 {
		full = full[i+2:]
	}
	return strings.TrimSuffix(full, "-fm")
}

// ── G7 门 B 与门 A **共用同一批 handler**(结构性防分叉)────────────────────────
func TestG7ConsoleDoorReusesTheSameHandlers(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/wiring.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	srv := New(db)
	doorA := doorARouteTableFromSource(t)
	doorAByPath := map[string]string{} // "GET /admin/files" -> handler 名
	for pattern, route := range doorA {
		doorAByPath[pattern] = route.handler
	}

	paired, solo := 0, 0
	for _, route := range srv.consoleDoorRoutes() {
		// 🔴 导航页不参与孪生比对:它的门 A 同形路径是 `/admin`(旧后台状态页),
		// 而那一页**不在搬迁范围**里 ⇒ 两者不是同一个东西,比 handler 名没有意义。
		if route.capability == capConsoleIndex {
			continue
		}
		for _, entry := range []struct {
			method  string
			handler http.HandlerFunc
		}{{http.MethodGet, route.get}, {http.MethodPost, route.post}} {
			if entry.handler == nil {
				continue
			}
			got := handlerNameOf(entry.handler)
			if got == "" {
				t.Fatalf("门 B %s %s 的 handler 名取不到", entry.method, route.pattern)
			}
			// 门 A 孪生路径:/console/admin/X ↔ /admin/X
			twin := entry.method + " " + adminDoorPrefix + strings.TrimPrefix(route.pattern, consoleDoorPrefix)
			want, hasTwin := doorAByPath[twin]
			if !hasTwin {
				solo++
				// 🔴 门 A 没有孪生 ⇒ 必须是**已声明的单路能力**(platformRoles)。
				// 若能力表说"门 A 也有"而源码里没有 ⇒ 表与装配不一致,同样红。
				if doorAFlag, _ := adminCapabilityDoors(route.capability); doorAFlag {
					t.Fatalf("能力 %s 在 adminCapabilityDoors 里声明**门 A 也有**,但源码里找不到 %s ⇒ 表与装配不一致", route.capability, twin)
				}
				continue
			}
			paired++
			if got != want {
				t.Fatalf("🔴 两路行为分叉的结构性来源:能力 %s 的 %s\n  门 B(%s)用 %s\n  门 A(%s)用 %s\n"+
					"判据:并存期两条路必须**共用同一个 handler 函数**,只允许授权包装不同。\n"+
					"若确实需要门 B 专属实现,先报组长 —— 那意味着"+
					"「同一能力两个入口」的行为等价性从此**只能靠测试维持**。",
					route.capability, entry.method, route.pattern, got, twin, want)
			}
		}
	}
	if paired < 8 {
		t.Fatalf("只对上 %d 条双路 handler(应为 8:files·files content·provision GET/POST·api-keys GET/POST·bot-channels GET/POST)⇒ 本格的绿没有判别力", paired)
	}
	if solo != 2 {
		t.Fatalf("单路能力条数=%d,期望 2(platform-roles 的 GET 与 POST —— 门 A 从来没有这个能力)⇒ 门 B 路由表与预期不符", solo)
	}
}

// ── 门 A:在范围内的必须挂能力表,范围外的一条都不许挂(防超围)──────────────────
func TestGateDoorACapabilityWiringMatchesOwnerScope(t *testing.T) {
	doorA := doorARouteTableFromSource(t)
	wantWrapped := map[string]string{
		"GET /admin/files":              "capFiles",
		"GET /admin/files/{id}/content": "capFileContent",
		"GET /admin/provision":          "capProvision",
		"POST /admin/provision":         "capProvision",
		"GET /admin/api-keys":           "capAPIKeys",
		"POST /admin/api-keys":          "capAPIKeys",
		"GET /admin/bot-channels":       "capBotChannels",
		"POST /admin/bot-channels":      "capBotChannels",
	}
	seen := 0
	for pattern, want := range wantWrapped {
		route, ok := doorA[pattern]
		if !ok {
			t.Fatalf("门 A 少了路由 %s(并存期旧后台必须原样活着)", pattern)
		}
		if route.wrapper != "requireAuthCap" || route.capability != want {
			t.Fatalf("🔴 %s 的授权包装是 %s(%q),期望 requireAuthCap(%s)。\n"+
				"判据:门 A 上**在搬迁范围内**的路由必须与门 B 调同一份能力判据,\n"+
				"否则「两路判据等价」只剩测试在维持,而不是结构在维持。",
				pattern, route.wrapper, route.capability, want)
		}
		seen++
	}
	if seen != len(wantWrapped) {
		t.Fatalf("只核到 %d/%d 条", seen, len(wantWrapped))
	}
	// 🔴 反向:范围外的一条都不许被套上能力表。
	// 这一条是**我自己踩过的坑**:`/admin/members`(成员配置)不在业主确认的 4 项里
	//(板件 BATCH-B1-CONTENT-20260921.md §六-2),门 B 也不提供它 ⇒ 不该动它。
	// 「增删管理者」是 console_platform_role 名册,**不是** /admin/members。
	for pattern, route := range doorA {
		if _, ok := wantWrapped[pattern]; ok {
			continue
		}
		if route.wrapper == "requireAuthCap" {
			t.Fatalf("🔴 超围:%s 被套上了能力表(%s),但它**不在**业主确认的搬迁范围内。\n"+
				"后果:业主的「回退的地方」开始依赖 Nova 的能力表 —— 改表会连带改坏回退口。\n"+
				"判据:门 A 只有那 8 条在范围内的路由挂 requireAuthCap,其余一律 requireAuth 原样。",
				pattern, route.capability)
		}
		_, path, _ := strings.Cut(pattern, " ")
		if strings.HasPrefix(path, "/admin") && path != "/admin/login" && route.wrapper != "requireAuth" {
			t.Fatalf("门 A 路由 %s 的授权包装是 %q,期望 requireAuth(旧后台原有行为不得因搬迁而改变)", pattern, route.wrapper)
		}
	}
}

// ── 门 B 路由表 == 业主确认的范围(多一项少一项都红)───────────────────────────
func TestG3aConsoleDoorRouteTableIsExactlyOwnerScope(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/scope.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, newPlatform := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "new-platform"}[newPlatform], func(t *testing.T) {
			srv := New(db)
			srv.NewPlatform = newPlatform
			hiddenOnNewPlatform := map[adminCapability]bool{capProvision: true, capAPIKeys: true, capPlatformRoles: true}
			want := map[string]adminCapability{
				"/console/admin": capConsoleIndex, "/console/admin/files": capFiles,
				"/console/admin/files/{id}/content": capFileContent, "/console/admin/bot-channels": capBotChannels,
			}
			if !newPlatform {
				want["/console/admin/provision"], want["/console/admin/api-keys"], want["/console/admin/platform-roles"] = capProvision, capAPIKeys, capPlatformRoles
			}
			got := map[string]adminCapability{}
			for _, route := range srv.consoleDoorRoutes() {
				if _, dup := got[route.pattern]; dup {
					t.Fatalf("门 B 路由表里 %s 出现两次", route.pattern)
				}
				got[route.pattern] = route.capability
			}
			if len(got) != len(want) {
				t.Fatalf("门 B 路由数=%d want=%d: got=%v want=%v", len(got), len(want), got, want)
			}
			for pattern, capability := range want {
				if got[pattern] != capability {
					t.Fatalf("门 B %s 能力=%q want=%q", pattern, got[pattern], capability)
				}
			}
			// 能力表 → 路由表：未来往 adminCapabilitiesInScope 新增能力时，若忘了挂路由必须红。
			for _, capability := range adminCapabilitiesInScope() {
				if newPlatform && hiddenOnNewPlatform[capability] {
					continue
				}
				found := false
				for _, route := range srv.consoleDoorRoutes() {
					if route.capability == capability {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("能力 %s 在当前模式范围内，但门 B 没有对应路由 ⇒ 两份清单漂移", capability)
				}
			}
			// 导航能力集合 → 路由表：文件内容页只从文件页进入，不得单列；其余可见能力恰好单列一次。
			wantTitles := map[adminCapability]bool{capFiles: true, capBotChannels: true}
			if !newPlatform {
				wantTitles[capProvision], wantTitles[capAPIKeys], wantTitles[capPlatformRoles] = true, true, true
			}
			gotTitles := map[adminCapability]bool{}
			for _, route := range srv.consoleDoorRoutes() {
				if route.title != "" {
					if gotTitles[route.capability] {
						t.Fatalf("能力 %s 在导航中重复", route.capability)
					}
					gotTitles[route.capability] = true
				}
			}
			if len(gotTitles) != len(wantTitles) {
				t.Fatalf("导航能力=%v want=%v", gotTitles, wantTitles)
			}
			for capability := range wantTitles {
				if !gotTitles[capability] {
					t.Fatalf("能力 %s 有路由但没有导航入口", capability)
				}
			}
		})
	}
}

// ── 门 B 的授权包装:三层都在(静态钉)──────────────────────────────────────────
func TestGateConsoleDoorGuardIsAssembled(t *testing.T) {
	fset := gotoken.NewFileSet()
	file, err := parser.ParseFile(fset, "console_door.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var mountWraps, guardHits, mountSites, unwrappedSites int
	var rangesOverTable bool
	var sawPrincipal, sawManagement, sawCapability, sawActor bool
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			return true
		}
		switch fn.Name.Name {
		case "MountConsole":
			// 🔴 判据:**门 B 的每一条路由都不可能绕过授权包装**。
			// 做法不是数"包装出现了几次"(那是个没有意义的数:循环里两处注册点覆盖 11 条路由),
			// 而是钉住三件事:① 注册点只有那两处 ② 两处**都**套了 requireConsoleCap
			// ③ 它们遍历的是 consoleDoorRoutes() 那张表(⇒ 表即全部路由,G3a 钉住表 == 业主范围)。
			// 将来若有人在这里加第三处 mux.HandleFunc 而没套包装 ⇒ 本格红。
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "requireConsoleCap":
					mountWraps++
				case "HandleFunc":
					mountSites++
					wrapped := false
					if len(call.Args) == 2 {
						ast.Inspect(call.Args[1], func(inner ast.Node) bool {
							if isel, ok := inner.(*ast.SelectorExpr); ok && isel.Sel.Name == "requireConsoleCap" {
								wrapped = true
							}
							return true
						})
					}
					if !wrapped {
						unwrappedSites++
					}
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				rs, ok := n.(*ast.RangeStmt)
				if !ok {
					return true
				}
				if call, ok := rs.X.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "consoleDoorRoutes" {
						rangesOverTable = true
					}
				}
				return true
			})
		case "requireConsoleCap":
			guardHits++
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "RequireConsolePrincipal":
					sawPrincipal = true
				case "RequireConsoleManagement":
					sawManagement = true
				case "adminCapabilityAllowed":
					sawCapability = true
				case "ConsolePrincipal":
					sawActor = true
				}
				return true
			})
		case "requireAuthCap":
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "adminCapabilityAllowed" {
					sawCapability = true
				}
				return true
			})
		}
		return true
	})
	if mountSites != 2 {
		t.Fatalf("MountConsole 里有 %d 处 mux.HandleFunc(期望 2:GET 与 POST 各一处,都在遍历路由表的循环里)。\n"+
			"多出来的注册点意味着**有路由可能绕过了 requireConsoleCap** ⇒ 必须先证明它套了包装。", mountSites)
	}
	if unwrappedSites != 0 {
		t.Fatalf("🔴 MountConsole 里有 %d 处注册**没有**套 requireConsoleCap ⇒ 那条路由没有授权门", unwrappedSites)
	}
	if mountWraps != 2 {
		t.Fatalf("requireConsoleCap 在 MountConsole 里出现 %d 次,期望 2", mountWraps)
	}
	if !rangesOverTable {
		t.Fatal("MountConsole 没有遍历 consoleDoorRoutes() ⇒ 路由表不再是唯一清单,G3a 的对账失效")
	}
	if guardHits != 1 {
		t.Fatalf("requireConsoleCap 声明了 %d 次,期望 1", guardHits)
	}
	// 🔴 判据写在它生效的地方:失败消息说清**少了哪一层**、后果是什么。
	if !sawPrincipal {
		t.Fatal("门 B 少了 RequireConsolePrincipal ⇒ 没有 console 会话也能打到 handler(无凭据暴露面)")
	}
	if !sawManagement {
		t.Fatal("🔴 门 B 少了 RequireConsoleManagement ⇒ **每个普通业主都能进**(默认角色就是 owner," +
			"而 owner 满足 read/submit)。这是本格最贵的一层,不许换成 RequireConsolePlatformAction。")
	}
	if !sawCapability {
		t.Fatal("两道门必须调**同一份** adminCapabilityAllowed(capability.go)⇒ 否则「两路判据等价」只剩测试在维持")
	}
	if !sawActor {
		t.Fatal("门 B 少了 m8.ConsolePrincipal ⇒ 审计归属会退回无差别的 \"admin\"(F3)")
	}
}
