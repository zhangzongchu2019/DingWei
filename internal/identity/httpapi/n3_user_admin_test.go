package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// A11 最小管理接口（组长 16:55:51Z）：POST /api/console/platform/users/{user_id}/state｜type。

// sessionAs 直接在存储里给 userID 建一个会话，返回带该会话 cookie 的浏览器。
func (e *n3env) sessionAs(userID, token string) *http.Client {
	e.t.Helper()
	if err := e.ob.Store.CreateSession(context.Background(), identity.Session{
		TokenHash: identity.HashSecret(token), UserIDLC: identity.Lower(userID), ExpiresAt: e.clk.Now().Add(identity.SessionTTL),
	}); err != nil {
		e.t.Fatal(err)
	}
	jar := &fakeClockJar{cookies: map[string]*http.Cookie{httpapi.SessionCookie: {Name: httpapi.SessionCookie, Value: token}}}
	return &http.Client{Jar: jar}
}

func TestA11HTTP_UserStateAndTypeEndpoints(t *testing.T) {
	e := newN3Env(t)
	ctx := context.Background()
	for _, u := range []identity.User{
		{UserID: "Mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "Alice", ShortName: "al", Type: identity.UserMember, State: identity.UserActive},
	} {
		if err := e.ob.Store.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	var changed []string
	e.ob.OnUserChanged = func(userLC string) { changed = append(changed, userLC) }
	mgr, alice, anon := e.sessionAs("mgr", "tok-mgr"), e.sessionAs("alice", "tok-alice"), e.browser()

	// 未登录 401；非管理者 403；跨站（无 Origin）403。都不改任何东西。
	if st, b := e.call(anon, "POST", "/api/console/platform/users/mgr/state", map[string]any{"state": "disabled"}, true); st != 401 || b["error"] != "unauthenticated" {
		t.Fatalf("anon: %d %v", st, b)
	}
	if st, b := e.call(alice, "POST", "/api/console/platform/users/mgr/state", map[string]any{"state": "disabled"}, true); st != 403 || b["error"] != "forbidden" {
		t.Fatalf("member disables: %d %v", st, b)
	}
	if st, b := e.call(alice, "POST", "/api/console/platform/users/alice/type", map[string]any{"type": "manager"}, true); st != 403 || b["error"] != "forbidden" {
		t.Fatalf("member promotes self: %d %v", st, b)
	}
	if st, b := e.call(mgr, "POST", "/api/console/platform/users/alice/state", map[string]any{"state": "disabled"}, false); st != 403 || b["error"] != "origin_mismatch" {
		t.Fatalf("no origin: %d %v", st, b)
	}
	if len(changed) != 0 {
		t.Fatalf("refused calls changed users: %v", changed)
	}
	// 管理者：改类型、停用，各 200；停用即吊销该用户会话（之后同一 cookie 401）。
	if st, b := e.call(mgr, "POST", "/api/console/platform/users/Alice/type", map[string]any{"type": "manager"}, true); st != 200 {
		t.Fatalf("promote: %d %v", st, b)
	}
	if st, b := e.call(mgr, "POST", "/api/console/platform/users/alice/type", map[string]any{"type": "member"}, true); st != 200 {
		t.Fatalf("demote: %d %v", st, b)
	}
	if st, b := e.call(mgr, "POST", "/api/console/platform/users/alice/state", map[string]any{"state": "disabled"}, true); st != 200 {
		t.Fatalf("disable: %d %v", st, b)
	}
	if st, _ := e.call(alice, "GET", "/api/console/agents", nil, false); st != 401 {
		t.Fatalf("disabled user's session still works: %d", st)
	}
	if len(changed) != 3 || changed[0] != "alice" || changed[2] != "alice" {
		t.Fatalf("OnUserChanged calls=%v, want 3 × alice", changed)
	}
	// 护栏的 HTTP 映射：改自己 403；坏值 400；查无此人 404；平台管理者类型不可改 403。
	for _, c := range []struct {
		path string
		body map[string]any
		st   int
	}{
		{"/api/console/platform/users/mgr/state", map[string]any{"state": "disabled"}, 403},
		{"/api/console/platform/users/alice/state", map[string]any{"state": "pending_bind"}, 400},
		{"/api/console/platform/users/alice/type", map[string]any{"type": "platform_admin"}, 400},
		{"/api/console/platform/users/nobody/state", map[string]any{"state": "disabled"}, 404},
		{"/api/console/platform/users/root/type", map[string]any{"type": "member"}, 403},
	} {
		if st, b := e.call(mgr, "POST", c.path, c.body, true); st != c.st {
			t.Errorf("%s %v: %d %v, want %d", c.path, c.body, st, b, c.st)
		}
	}
}
