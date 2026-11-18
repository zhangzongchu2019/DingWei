package m8

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// S-R62-CONSOLE-SLIM（业主令 rulings ㊿-补129）：整个菜单只保留「Agent」与「平台管理」。
// 本格专守菜单形状：新平台管理角色恰为两项、普通角色恰为一项；两平台被摘项不得回潮。
// 删保护：把任一被摘项加回 handler（或换序、或多发一项）⇒ 本格红。
// 只摘入口：后端路由、表、数据、迁移不在本格判据内（直连旧 URL 的行为保持现状）。

var consoleMenuSlimmedIDs = []string{"specs", "task-board", "pilot-questions", "coordinate-reviews", "openspec-confirmations"}

func assertConsoleMenuNoSlimmedIDs(t *testing.T, body string) {
	t.Helper()
	for _, gone := range consoleMenuSlimmedIDs {
		if strings.Contains(body, `"id":"`+gone+`"`) {
			t.Errorf("菜单不得再下发被摘项 %q: %s", gone, body)
		}
	}
}

func TestNewPlatformConsoleMenuSlimExactlyTwoEntries(t *testing.T) {
	v := newViewEnv(t)
	for _, u := range []identity.User{
		{UserID: "mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "mem", ShortName: "me", Type: identity.UserMember, State: identity.UserActive},
	} {
		_ = v.svc.Store.CreateUser(v.ctx, u)
		_ = v.svc.Store.CreateSession(v.ctx, identity.Session{TokenHash: identity.HashSecret("tok-" + u.UserID), UserIDLC: u.UserID, ExpiresAt: v.svc.Now().Add(identity.SessionTTL)})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/platform", v.hub.RequireConsolePrincipal(v.hub.RequireConsolePlatformAction("read", v.hub.HandleConsolePlatform)))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	call := func(tok string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/console/platform", nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}
	ids := func(body string) string {
		var resp struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range resp.Items {
			out = append(out, it.ID)
		}
		return strings.Join(out, ",")
	}
	if st, body := call("tok-mgr"); st != 200 || ids(body) != "agent-groups,platform-admin" {
		t.Fatalf("管理角色菜单应恰为 agent-groups,platform-admin 两项: %d %s", st, body)
	} else {
		assertConsoleMenuNoSlimmedIDs(t, body)
	}
	if st, body := call("tok-mem"); st != 200 || ids(body) != "agent-groups" {
		t.Fatalf("普通角色菜单应恰为 agent-groups 一项: %d %s", st, body)
	} else {
		assertConsoleMenuNoSlimmedIDs(t, body)
	}
}

func TestLegacyConsoleMenuSlimRemovedEntriesAbsent(t *testing.T) {
	for _, principalID := range []string{"manager", "owner"} {
		role, items := consolePlatformItemsFor(t, principalID)
		var ids []string
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		body := strings.Join(ids, ",")
		for _, gone := range []string{"specs", "task-board", "coordinate-reviews", "openspec-confirmations"} {
			if strings.Contains(","+body+",", ","+gone+",") {
				t.Errorf("%s 菜单不得再下发被摘项 %q: %s", principalID, gone, body)
			}
		}
		if _, ok := findPlatformItem(items, "agent-groups"); !ok {
			t.Errorf("%s 菜单必须保留 agent-groups: %s", principalID, body)
		}
		_, hasAdmin := findPlatformItem(items, "platform-admin")
		if role == consolePlatformRoleManagement && !hasAdmin {
			t.Errorf("management 菜单必须保留 platform-admin: %s", body)
		}
		if role == consolePlatformRoleOwner && hasAdmin {
			t.Errorf("owner 菜单不得下发 platform-admin: %s", body)
		}
	}
}
