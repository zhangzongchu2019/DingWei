package m8

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 🔴 Nova-260921-A(B1-b):`RequireConsoleManagement` 的闸。
//
// 它守的是 admin 能力搬进 console 的那道门(`/console/admin/*`)。
// **为什么必须有一个新守卫、不能用现成的动作动词** —— 见 console_management.go 头注,
// 而 TestRequireConsoleManagementIsNotSubstitutableByActionVerb 把那条理由**做成断言**,
// 不是只写在注释里(本仓今天已被"注释里写了、没人读到"咬过)。

func newManagementTestHub(t *testing.T) (*Hub, *store.SQLite) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "console-management.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// 🔴 形状**刻意含历史/退化格**:板件 9556558b 刚退回 B1-a 的同型错误
	// (「夹具起点选在了新建组上 ⇒ 绿而无判别力」)。只放一个干净的 management 进来,
	// 就永远验不到"没有角色行的普通业主"这一格 —— 而那一格**才是**会出事的那格。
	shapes := []struct {
		id        string
		principal bool // principal 是否 active
		roles     []struct {
			role   string
			active bool
		}
	}{
		{id: "p-mgmt", principal: true, roles: []struct {
			role   string
			active bool
		}{{"management", true}}},
		{id: "p-owner-explicit", principal: true, roles: []struct {
			role   string
			active bool
		}{{"owner", true}}},
		{id: "p-no-role-row", principal: true}, // ⇒ ResolveConsolePlatformRole COALESCE 成 owner
		{id: "p-mgmt-off", principal: true, roles: []struct {
			role   string
			active bool
		}{{"management", false}, {"owner", true}}},
		{id: "p-mgmt-and-owner", principal: true, roles: []struct {
			role   string
			active bool
		}{{"management", true}, {"owner", true}}},
		{id: "p-inactive", principal: false, roles: []struct {
			role   string
			active bool
		}{{"management", true}}},
	}
	for _, shape := range shapes {
		// 🔴 DisplayName 自 0.2.1.89 起是【必填】:上游给 store.UpsertConsolePrincipal 的身份校验
		// 加了一条 DisplayName 非空(internal/store/console_auth.go)。rebase 到 841d2837 后本夹具缺它
		// ⇒ 三格(RoleMatrix / NotSubstitutableByActionVerb / FailsClosed 的开关关闭腿)全部
		// 塌在**装置阶段**,报 "console principal identity fields required"。
		// 🔵 补法与仓内既有夹具同口径(DisplayName = ID,见 console_auth_test.go / console_agent_groups_test.go)。
		p := model.ConsolePrincipal{ID: shape.id, Provider: "feishu", ProviderIssuer: "app",
			ProviderSubject: shape.id + "-openid", DisplayName: shape.id, Active: shape.principal}
		if err := db.UpsertConsolePrincipal(ctx, p); err != nil {
			t.Fatal(err)
		}
		for _, role := range shape.roles {
			if err := db.UpsertConsolePlatformRole(ctx, model.ConsolePlatformRole{
				PrincipalID: shape.id, Role: role.role, Active: role.active,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return New(db, ConsoleAuthMode{enabled: true}), db
}

func runManagementGuard(hub *Hub, principalID string) (*httptest.ResponseRecorder, bool) {
	called := false
	handler := hub.RequireConsoleManagement(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/console/admin", nil)
	if principalID != "" {
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principalID))
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec, called
}

func TestRequireConsoleManagementRoleMatrix(t *testing.T) {
	hub, db := newManagementTestHub(t)
	// 期望值**硬写在这里**,不由产品的 consolePlatformActionAllowed / ResolveConsolePlatformRole 推出。
	// 🔴 否则改判据时期望值跟着变 ⇒ 闸永远绿(盲量)。
	cases := []struct {
		principal string
		wantRole  string // 独立写下的"我以为它会解析成什么"
		wantAllow bool
	}{
		{"p-mgmt", "management", true},
		{"p-owner-explicit", "owner", false},
		{"p-no-role-row", "owner", false}, // 🔴 没有角色行 ⇒ 默认 owner ⇒ **必须拒**
		{"p-mgmt-off", "owner", false},    // management 行 active=0 ⇒ 不算
		{"p-mgmt-and-owner", "management", true},
		{"p-inactive", "", false}, // principal 停用 ⇒ 无角色
		{"p-does-not-exist", "", false},
		{"", "", false}, // 没有 principal(守卫被漏挂)⇒ 拒,不是放行
	}
	for _, tc := range cases {
		t.Run(tc.principal, func(t *testing.T) {
			if tc.principal != "" {
				gotRole, err := db.ResolveConsolePlatformRole(context.Background(), tc.principal)
				if err != nil {
					t.Fatal(err)
				}
				if gotRole != tc.wantRole {
					t.Fatalf("装置/前提不成立:role(%s)=%q,我以为=%q ⇒ 本格的绿没有判别力,先修前提",
						tc.principal, gotRole, tc.wantRole)
				}
			}
			rec, called := runManagementGuard(hub, tc.principal)
			if tc.wantAllow {
				if rec.Code != http.StatusNoContent || !called {
					t.Fatalf("management 必须放行:code=%d called=%v", rec.Code, called)
				}
				return
			}
			if called {
				t.Fatalf("🔴 非 management 的主体(%s)打穿了守卫 —— 它拿到了 admin 能力入口", tc.principal)
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("拒的形状必须是 403(已认证但无权),got %d", rec.Code)
			}
		})
	}
}

// 🔴 阳性对照:证明"用现成的动作动词守门"这个错法**真的会让普通业主进来**。
//
// 这一格的价值不在本次:它把「为什么不用 RequireConsolePlatformAction("read")」
// 从一句论断变成一条会自己报警的断言。若将来有人"简化"成动作动词,
// 这一格仍然绿(它测的是两个守卫的**差别**),而 RequireConsoleManagementRoleMatrix 会红 ——
// 🔵 反过来说:若哪天 consolePlatformActionAllowed(owner,"read") 变成 false,
// 本格会红并告诉你"这条对照已失效,危险不再存在",那时才可以删掉它。
func TestRequireConsoleManagementIsNotSubstitutableByActionVerb(t *testing.T) {
	hub, _ := newManagementTestHub(t)
	if !consolePlatformActionAllowed(consolePlatformRoleOwner, consolePlatformActionRead) {
		t.Fatalf("对照失效:owner 已经不满足 read 动作了 ⇒ 「用动作动词守门会放进普通业主」这个危险不再成立。\n" +
			"这时本测试该被删掉或改写,而不是留着当装饰。")
	}
	// 同一个主体(没有角色行 ⇒ 默认 owner),两个守卫:
	readGuardCalled := false
	hub.RequireConsolePlatformAction(consolePlatformActionRead, func(w http.ResponseWriter, r *http.Request) {
		readGuardCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/console/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "p-no-role-row"))
	rec := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionRead, func(w http.ResponseWriter, r *http.Request) {
		readGuardCalled = true
		w.WriteHeader(http.StatusNoContent)
	})(rec, req)
	if !readGuardCalled {
		t.Fatalf("对照腿坏了:read 动作守卫本该放行默认 owner(这正是危险所在),却没有")
	}
	rec2, mgmtCalled := runManagementGuard(hub, "p-no-role-row")
	if mgmtCalled || rec2.Code != http.StatusForbidden {
		t.Fatalf("🔴 RequireConsoleManagement 放行了默认 owner(code=%d called=%v)—— 那就等于把明文凭证页发给每个普通业主", rec2.Code, mgmtCalled)
	}
}

// fail-closed:开关关闭 ／ repo 不具备取数面 ／ 角色查询报错 ⇒ 一律拒。
func TestRequireConsoleManagementFailsClosed(t *testing.T) {
	t.Run("console 鉴权开关关闭 ⇒ 404,不是放行", func(t *testing.T) {
		hub, _ := newManagementTestHub(t)
		hub.consoleAuthEnabled = false
		rec, called := runManagementGuard(hub, "p-mgmt")
		if called || rec.Code != http.StatusNotFound {
			t.Fatalf("开关关闭时门必须整体关死(404,不泄露\"这里有个管理入口\"),got code=%d called=%v", rec.Code, called)
		}
	})
	t.Run("repo 不具备角色取数面 ⇒ 403", func(t *testing.T) {
		hub := New(&stubNoPlatformRole{}, ConsoleAuthMode{enabled: true})
		rec, called := runManagementGuard(hub, "p-mgmt")
		if called || rec.Code != http.StatusForbidden {
			t.Fatalf("查不了 ≠ 通过:code=%d called=%v", rec.Code, called)
		}
	})
	t.Run("角色查询报错 ⇒ 503", func(t *testing.T) {
		// 🔴 这一格需要 stub **真的满足** consolePlatformRepository(否则断言先失败 ⇒ 变成 403,
		// 那就测到了另一条路上,却看起来像"503 那格过了")。所以内嵌真 *SQLite,只覆盖那一个方法。
		db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "role-error.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := db.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		hub := New(&stubRoleError{SQLite: db}, ConsoleAuthMode{enabled: true})
		rec, called := runManagementGuard(hub, "p-mgmt")
		if called || rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("查不了 ≠ 通过:code=%d called=%v", rec.Code, called)
		}
	})
}

// stubNoPlatformRole 满足 store.Repository(靠内嵌),但**不**提供 ResolveConsolePlatformRole
// ⇒ `h.Repo.(consolePlatformRepository)` 断言失败。
type stubNoPlatformRole struct{ store.Repository }

// stubRoleError 提供**完整**取数面(内嵌真 *SQLite),只让角色查询报错。
type stubRoleError struct {
	*store.SQLite
}

func (stubRoleError) ResolveConsolePlatformRole(context.Context, string) (string, error) {
	return "", errors.New("gate probe: role lookup failed")
}

func TestConsolePrincipalAccessor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/console/admin", nil)
	if got := ConsolePrincipal(req); got != "" {
		t.Fatalf("没有守卫注入时必须是空串,got %q", got)
	}
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "p-mgmt"))
	if got := ConsolePrincipal(req); got != "p-mgmt" {
		t.Fatalf("got %q want p-mgmt(门 B 的审计归属靠它)", got)
	}
	if got := ConsolePrincipal(nil); got != "" {
		t.Fatalf("nil 请求必须返回空串而不是 panic,got %q", got)
	}
}
