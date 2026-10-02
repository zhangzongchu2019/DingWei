package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

type n3env struct {
	t   *testing.T
	clk *clock.Fake
	ob  *identity.Onboarding
	srv *httptest.Server
	pw  string // 初始化脚本给出的一次性初始口令
}

func newN3Env(t *testing.T) *n3env {
	t.Helper()
	e := &n3env{t: t, clk: &clock.Fake{T: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}}
	store := identity.NewMemStore()
	svc := identity.NewService(store, e.clk.Now, nil)
	e.ob = identity.NewOnboarding(svc)
	// 第一个管理用户由初始化脚本建（台账 8971bddf）；ID 取 Root，好让用户列表按小写排序时排在 Alice 之后。
	pw, err := e.ob.BootstrapAdmin(context.Background(), "Root", "rt", "")
	if err != nil {
		t.Fatal(err)
	}
	e.pw = pw
	n3 := &httpapi.N3{OB: e.ob, PublicOrigin: origin}
	api := &httpapi.API{Svc: svc, PublicOrigin: origin, Actor: n3.Actor}
	mux := http.NewServeMux()
	n3.Register(mux)
	api.Register(mux)
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

// browser 一个带 cookie 的浏览器。
//
// 🔴 不用 net/http/cookiejar：它拿【墙上时钟】判 cookie 过期，而本格的服务端跑在假时钟上
// （2026-10-01T09:00Z 起，会话 12 小时 ⇒ cookie Expires＝21:00Z）。墙上时钟一过 21:00Z，jar 就把会话 cookie
// 当过期丢掉，整套用例在每天这个时刻之后一律 401（2026-10-01 22:3xZ 实测，newplat/main 上同样红）。
// 会话过期由服务端按假时钟判，浏览器这边只需记住、按删除指令删除，不需要自己判时间。
func (e *n3env) browser() *http.Client {
	return &http.Client{Jar: &fakeClockJar{cookies: map[string]*http.Cookie{}}}
}

// fakeClockJar 不看墙上时钟的 cookie 罐：本地测试服务只有一个主机，按名字存；
// Max-Age<0（服务端要求删除，如退出登录）即删除。
type fakeClockJar struct {
	mu      sync.Mutex
	cookies map[string]*http.Cookie
}

func (j *fakeClockJar) SetCookies(_ *url.URL, cs []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, c := range cs {
		if c.MaxAge < 0 {
			delete(j.cookies, c.Name)
			continue
		}
		j.cookies[c.Name] = &http.Cookie{Name: c.Name, Value: c.Value}
	}
}

func (j *fakeClockJar) Cookies(_ *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]*http.Cookie, 0, len(j.cookies))
	for _, c := range j.cookies {
		out = append(out, c)
	}
	return out
}

func (e *n3env) call(c *http.Client, method, path string, body any, withOrigin bool) (int, map[string]any) {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	if withOrigin {
		req.Header.Set("Origin", origin)
	}
	res, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

var feishuAlice = identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_alice"}

func TestN3HTTP_FullOnboardingFlow(t *testing.T) {
	e := newN3Env(t)
	admin := e.browser()
	if st, b := e.call(admin, "POST", "/console/login/password", map[string]any{"user_id": "Root", "password": "nope"}, true); st != 401 || b["error"] != "bad_credentials" {
		t.Fatalf("bad password: %d %v", st, b)
	}
	st, b := e.call(admin, "POST", "/console/login/password", map[string]any{"user_id": "Root", "password": e.pw}, true)
	if st != 200 || b["must_change_password"] != true {
		t.Fatalf("login: %d %v", st, b)
	}
	// N3 A2：改密前，除改密接口外一律拒。
	if st, b := e.call(admin, "GET", "/api/console/platform/users", nil, false); st != 403 || b["error"] != "must_change_password" {
		t.Fatalf("before change: %d %v", st, b)
	}
	// 组长裁定 140013Z ②（003a5de4 已定「一律 403」）：入网接口对必须改密的会话同样 403，不是 401。
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/console/agents"}, {"GET", "/api/console/enroll/applications"},
		{"POST", "/api/console/enroll/applications/x/approve"}, {"POST", "/api/console/enroll/applications/x/reject"},
		{"POST", "/api/console/agents/x/revoke"},
	} {
		if st, b := e.call(admin, c.method, c.path, map[string]any{}, true); st != 403 || b["error"] != "must_change_password" {
			t.Fatalf("%s %s before change: %d %v, want 403 must_change_password", c.method, c.path, st, b)
		}
	}
	if st, b := e.call(admin, "POST", "/api/console/account/password", map[string]any{"old_password": e.pw, "new_password": "x"}, true); st != 422 || b["error"] != "weak_password" {
		t.Fatalf("weak: %d %v", st, b)
	}
	if st, _ := e.call(admin, "POST", "/api/console/account/password", map[string]any{"old_password": e.pw, "new_password": "New-Strong-Pass-42"}, true); st != 200 {
		t.Fatalf("change: %d", st)
	}
	if st, _ := e.call(admin, "GET", "/api/console/platform/users", nil, false); st != 200 {
		t.Fatalf("after change: %d", st)
	}
	// 开户
	st, b = e.call(admin, "POST", "/api/console/platform/users", map[string]any{"display_name": "Alice", "user_id": "Alice", "short_name": "al", "type": "member"}, true)
	if st != 201 || b["user_id"] != "Alice" || len(b["registration_code"].(string)) != 8 || b["code_expires_at"] != "2026-10-02T09:00:00Z" {
		t.Fatalf("create user: %d %v", st, b)
	}
	if st, b := e.call(admin, "POST", "/api/console/platform/users", map[string]any{"user_id": "ALICE", "short_name": "zz", "type": "member"}, true); st != 409 || b["error"] != "user_exists" {
		t.Fatalf("dup: %d %v", st, b)
	}
	// 飞书侧绑定（Hub 的飞书入口会这样调用）
	if r := e.ob.HandleRegister(context.Background(), feishuAlice, b["registration_code"].(string)); !strings.Contains(r, "绑定成功") {
		t.Fatalf("register: %s", r)
	}
	// 以用户 ID 登录
	alice := e.browser()
	st, lc := e.call(alice, "POST", "/console/login/code", map[string]any{"user_id": "alice"}, true)
	if st != 200 || len(lc["code"].(string)) != 8 {
		t.Fatalf("login code: %d %v", st, lc)
	}
	code := lc["code"].(string)
	if st, b := e.call(alice, "GET", "/console/login/status?code="+code, nil, false); st != 200 || b["state"] != "pending" {
		t.Fatalf("status before confirm: %d %v", st, b)
	}
	if r := e.ob.HandleLogin(context.Background(), feishuAlice, code); !strings.Contains(r, "以 Alice 登录") {
		t.Fatalf("feishu confirm: %s", r)
	}
	if st, b := e.call(alice, "GET", "/console/login/status?code="+code, nil, false); st != 200 || b["state"] != "confirmed" {
		t.Fatalf("status after confirm: %d %v", st, b)
	}
	// 登录后，同一个 cookie 即是入网接口的当前用户（API.Actor 接 N3.Actor）。
	if st, b := e.call(alice, "GET", "/api/console/agents", nil, false); st != 200 || b["agents"] == nil {
		t.Fatalf("alice agents: %d %v", st, b)
	}
	// N3 A7：普通用户不能开户。
	// 真用户列表：只给管理用户，字段恰为五项、不含任何秘密
	st, ul := e.call(admin, "GET", "/api/console/platform/users", nil, false)
	users, _ := ul["users"].([]any)
	if st != 200 || len(users) != 2 {
		t.Fatalf("admin list users: %d %v", st, ul)
	}
	first := users[0].(map[string]any)
	if keys(first) != "display_name,short_name,state,type,user_id" || first["user_id"] != "Alice" || first["state"] != "active" || first["type"] != "member" {
		t.Fatalf("user row: %v", first)
	}
	if second := users[1].(map[string]any); second["user_id"] != "Root" || second["type"] != "platform_admin" {
		t.Fatalf("second row: %v", second)
	}
	if st, b := e.call(alice, "POST", "/api/console/platform/users", map[string]any{"user_id": "eve", "short_name": "ev", "type": "member"}, true); st != 403 || b["error"] != "forbidden" {
		t.Fatalf("member creates user: %d %v", st, b)
	}
	if st, _ := e.call(alice, "GET", "/api/console/platform/users", nil, false); st != 403 {
		t.Fatalf("member lists users: %d", st)
	}
	// 退出后会话 cookie 被清掉。
	e.call(alice, "POST", "/console/logout", nil, true)
	if st, _ := e.call(alice, "GET", "/api/console/agents", nil, false); st != 401 {
		t.Fatalf("after logout: %d", st)
	}
}

func TestN3HTTP_OriginRequiredOnLoginAndWrites(t *testing.T) {
	e := newN3Env(t)
	c := e.browser()
	for _, p := range []string{"/console/login/password", "/console/login/code"} {
		if st, b := e.call(c, "POST", p, map[string]any{"user_id": "Root", "password": e.pw}, false); st != 403 || b["error"] != "origin_mismatch" {
			t.Errorf("%s without Origin: %d %v", p, st, b)
		}
	}
	if st, _ := e.call(c, "POST", "/console/login/password", map[string]any{"user_id": "Root", "password": e.pw}, true); st != 200 {
		t.Fatalf("login: %d", st)
	}
	if st, b := e.call(c, "POST", "/api/console/account/password", map[string]any{"old_password": e.pw, "new_password": "New-Strong-Pass-42"}, false); st != 403 || b["error"] != "origin_mismatch" {
		t.Fatalf("change without Origin: %d %v", st, b)
	}
}
