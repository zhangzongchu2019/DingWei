package httpapi_test

// 业主反馈（板 7a138dcf）：GET /api/console/platform/users 对「待绑定飞书」行一直给出当前有效注册码
// （registration_code、code_expires_at，与建用户、重新生成两个接口同名），直到绑定、过期或重新生成；
// 只有管理用户拿得到；Hub 未配密钥则不出这两个键。

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

func adminReady(t *testing.T, e *n3env) *http.Client {
	t.Helper()
	admin := e.browser()
	if st, b := e.call(admin, "POST", "/console/login/password", map[string]any{"user_id": "Root", "password": e.pw}, true); st != 200 {
		t.Fatalf("login: %d %v", st, b)
	}
	if st, _ := e.call(admin, "POST", "/api/console/account/password", map[string]any{"old_password": e.pw, "new_password": "New-Strong-Pass-42"}, true); st != 200 {
		t.Fatalf("change password: %d", st)
	}
	return admin
}

func userRows(t *testing.T, e *n3env, c *http.Client) map[string]map[string]any {
	t.Helper()
	st, b := e.call(c, "GET", "/api/console/platform/users", nil, false)
	if st != 200 {
		t.Fatalf("list users: %d %v", st, b)
	}
	out := map[string]map[string]any{}
	for _, it := range b["users"].([]any) {
		m := it.(map[string]any)
		out[m["user_id"].(string)] = m
	}
	return out
}

func TestUserListShowsCurrentRegistrationCode(t *testing.T) {
	e := newN3Env(t)
	e.ob.CodeKey = "k-secret-1"
	admin := adminReady(t, e)
	st, b := e.call(admin, "POST", "/api/console/platform/users", map[string]any{"user_id": "Alice", "short_name": "al", "type": "member"}, true)
	if st != 201 {
		t.Fatalf("create: %d %v", st, b)
	}
	code1, exp1 := b["registration_code"].(string), b["code_expires_at"].(string)

	rows := userRows(t, e, admin)
	if rows["Alice"]["registration_code"] != code1 || rows["Alice"]["code_expires_at"] != exp1 {
		t.Fatalf("pending row: %v, want code %s exp %s", rows["Alice"], code1, exp1)
	}
	if _, has := rows["Root"]["registration_code"]; has {
		t.Fatalf("active admin row must not carry a code: %v", rows["Root"])
	}

	// 重新生成 ⇒ 列表给新码
	st, b = e.call(admin, "POST", "/api/console/platform/users/Alice/registration-code", map[string]any{}, true)
	if st != 200 {
		t.Fatalf("regenerate: %d %v", st, b)
	}
	if rows := userRows(t, e, admin); rows["Alice"]["registration_code"] != b["registration_code"] || rows["Alice"]["registration_code"] == code1 {
		t.Fatalf("after regenerate: %v", rows["Alice"])
	}

	// 普通用户 Bob：绑定后行里不再有码；他自己拿不到列表（403）
	st, b = e.call(admin, "POST", "/api/console/platform/users", map[string]any{"user_id": "Bob", "short_name": "bb", "type": "member"}, true)
	if st != 201 {
		t.Fatalf("create bob: %d %v", st, b)
	}
	if r := e.ob.HandleRegister(context.Background(), identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_bob"}, b["registration_code"].(string)); !strings.Contains(r, "绑定成功") {
		t.Fatalf("bind bob: %s", r)
	}
	if _, has := userRows(t, e, admin)["Bob"]["registration_code"]; has {
		t.Fatal("bound user's row must not carry a code")
	}
	if err := e.ob.Store.CreateSession(context.Background(), identity.Session{TokenHash: identity.HashSecret("tok-bob"), UserIDLC: "bob",
		ExpiresAt: e.clk.Now().Add(identity.SessionTTL)}); err != nil {
		t.Fatal(err)
	}
	bob := e.browser()
	u, _ := url.Parse(e.srv.URL)
	bob.Jar.SetCookies(u, []*http.Cookie{{Name: httpapi.SessionCookie, Value: "tok-bob"}})
	if st, b := e.call(bob, "GET", "/api/console/platform/users", nil, false); st != 403 || strings.Contains(e.srv.URL+toString(b), "registration_code") {
		t.Fatalf("member listing users: %d %v, want 403 without any code", st, b)
	}

	// 停用的待绑定用户：码还在库里有效，但已不能用来绑定 ⇒ 不展示（只给 pending_bind 行）
	if st, b := e.call(admin, "POST", "/api/console/platform/users", map[string]any{"user_id": "Carol", "short_name": "cr", "type": "member"}, true); st != 201 {
		t.Fatalf("create carol: %d %v", st, b)
	}
	if err := e.ob.Store.SetUserState(context.Background(), "carol", identity.UserDisabled, e.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if row := userRows(t, e, admin)["Carol"]; row["state"] != "disabled" || row["registration_code"] != nil {
		t.Fatalf("disabled pending user must not show a code: %v", row)
	}

	// 过期 ⇒ 不再给
	e.clk.T = e.clk.T.Add(identity.RegistrationCodeTTL + time.Minute)
	admin = e.browser() // 会话 12 小时，过了 24 小时要重新登录
	if st, b := e.call(admin, "POST", "/console/login/password", map[string]any{"user_id": "Root", "password": "New-Strong-Pass-42"}, true); st != 200 {
		t.Fatalf("re-login: %d %v", st, b)
	}
	if _, has := userRows(t, e, admin)["Alice"]["registration_code"]; has {
		t.Fatal("expired code must not be shown")
	}
}

func TestUserListWithoutKeyShowsNoCode(t *testing.T) {
	e := newN3Env(t) // CodeKey 为空
	admin := adminReady(t, e)
	if st, b := e.call(admin, "POST", "/api/console/platform/users", map[string]any{"user_id": "Alice", "short_name": "al", "type": "member"}, true); st != 201 {
		t.Fatalf("create: %d %v", st, b)
	}
	row := userRows(t, e, admin)["Alice"]
	if _, has := row["registration_code"]; has {
		t.Fatalf("no key ⇒ no code: %v", row)
	}
	if _, has := row["code_expires_at"]; has {
		t.Fatalf("no key ⇒ no expiry either: %v", row)
	}
}

func toString(m map[string]any) string {
	var sb strings.Builder
	for k, v := range m {
		sb.WriteString(k)
		if s, ok := v.(string); ok {
			sb.WriteString(s)
		}
	}
	return sb.String()
}
