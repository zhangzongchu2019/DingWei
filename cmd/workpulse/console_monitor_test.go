package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
)

const monitorPath = "/api/console/monitor/snapshot"

// monitorStore 在 MemStore 外面加两种故障注入（只作用于第 N 次及以后的 ResolveSession）：
// 存储报错，或者存储返回一个「会话有效但用户非 active」的用户 —— 用来钉 guard 自身的 fail-closed，
// 而不是只靠外层 RequireConsolePrincipal。
type monitorStore struct {
	*identity.MemStore
	calls    atomic.Int64
	failFrom atomic.Int64 // 0 = 不注入
	mode     atomic.Value // "error" | "inactive"
}

func (s *monitorStore) ResolveSession(ctx context.Context, tokenHash string, now time.Time) (*identity.Session, *identity.User, error) {
	n := s.calls.Add(1)
	if from := s.failFrom.Load(); from > 0 && n >= from {
		if s.mode.Load() == "inactive" {
			sess, u, err := s.MemStore.ResolveSession(ctx, tokenHash, now)
			if err == nil {
				u.State = identity.UserDisabled
			}
			return sess, u, err
		}
		return nil, nil, errors.New("injected store failure")
	}
	return s.MemStore.ResolveSession(ctx, tokenHash, now)
}

type monitorRig struct {
	t   *testing.T
	srv *httptest.Server
	st  *monitorStore
}

func newMonitorRig(t *testing.T) *monitorRig {
	t.Helper()
	hub := newplatHub(t)
	st := &monitorStore{MemStore: identity.NewMemStore()}
	rec := &routeRecorder{mux: http.NewServeMux()}
	if err := wireNewPlatform(context.Background(), rec, hub, newPlatformConfig{publicOrigin: "https://dw.example"}, st, nil); err != nil {
		t.Fatal(err)
	}
	registerConsoleRoutes(rec, hub, true)
	ctx := context.Background()
	now := hub.Identity.Now()
	users := []identity.User{
		{UserID: "admin", ShortName: "adm", Type: identity.UserPlatformAdmin, State: identity.UserActive},
		{UserID: "mgr", ShortName: "mgr", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "mem", ShortName: "mem", Type: identity.UserMember, State: identity.UserActive},
		{UserID: "admin2", ShortName: "ad2", Type: identity.UserPlatformAdmin, State: identity.UserActive, MustChangePassword: true},
		{UserID: "admin3", ShortName: "ad3", Type: identity.UserPlatformAdmin, State: identity.UserPendingBind},
	}
	for _, u := range users {
		u.CreatedAt = now
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("tok-" + u.UserID), UserIDLC: u.UserIDLC(), ExpiresAt: now.Add(identity.SessionTTL)}); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(rec.mux)
	t.Cleanup(srv.Close)
	return &monitorRig{t: t, srv: srv, st: st}
}

func (r *monitorRig) get(tok string, mutate func(*http.Request)) (int, string) {
	r.t.Helper()
	req, _ := http.NewRequest("GET", r.srv.URL+monitorPath, nil)
	req.Header.Set("Accept", "application/json")
	if tok != "" {
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
	}
	if mutate != nil {
		mutate(req)
	}
	res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestMonitorSnapshotOnlyActivePlatformAdmin(t *testing.T) {
	r := newMonitorRig(t)

	code, body := r.get("tok-admin", nil)
	if code != http.StatusOK {
		t.Fatalf("active platform_admin: %d %s", code, body)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(body), &snap); err != nil || snap["schema"] != "dw-obs/v1" {
		t.Fatalf("snapshot body: %v %s", err, body)
	}

	cases := []struct {
		name, tok string
		mutate    func(*http.Request)
		want      int
		wantBody  string
	}{
		{"manager is management but not platform_admin", "tok-mgr", nil, 403, "platform_admin_required"},
		{"member", "tok-mem", nil, 403, "platform_admin_required"},
		{"member spoofing owner via query and headers", "tok-mem", func(q *http.Request) {
			q.URL.RawQuery = "owner=admin&role=platform_admin"
			q.Header.Set("X-DW-Owner", "admin")
			q.Header.Set("X-DW-Console-Principal", "admin")
		}, 403, "platform_admin_required"},
		{"no session", "", nil, 401, "unauthenticated"},
		{"unknown token", "tok-nobody", nil, 401, "unauthenticated"},
		{"must change password", "tok-admin2", nil, 403, "must_change_password"},
		{"platform_admin not active", "tok-admin3", nil, 401, "unauthenticated"},
	}
	for _, c := range cases {
		code, body := r.get(c.tok, c.mutate)
		if code != c.want || !strings.Contains(body, c.wantBody) {
			t.Errorf("%s: got %d %s want %d %s", c.name, code, body, c.want, c.wantBody)
		}
		if code != 200 && strings.Contains(body, "dw-obs/v1") {
			t.Errorf("%s: denied response leaked snapshot", c.name)
		}
	}
}

// guard 自身 fail-closed：外层认人通过之后，guard 的那次现查若存储报错或用户已非 active，都不能放行。
// 先量一次正常请求的现查次数 d（外层可能不止一次），再只在第 d 次（最后一次＝guard）注入，确保红的是 guard 本身。
func TestMonitorGuardFailsClosedOnItsOwnLookup(t *testing.T) {
	for _, mode := range []string{"error", "inactive"} {
		r := newMonitorRig(t)
		base := r.st.calls.Load()
		if code, body := r.get("tok-admin", nil); code != http.StatusOK {
			t.Fatalf("baseline: %d %s", code, body)
		}
		d := r.st.calls.Load() - base
		if d < 2 {
			t.Fatalf("want at least outer + guard lookups, got %d", d)
		}
		r.st.mode.Store(mode)
		base = r.st.calls.Load()
		r.st.failFrom.Store(base + d) // 只有最后一次（guard）注入
		code, body := r.get("tok-admin", nil)
		if code != http.StatusForbidden && code != http.StatusUnauthorized || strings.Contains(body, "dw-obs/v1") {
			t.Fatalf("mode %s: guard must fail closed, got %d %s", mode, code, body)
		}
		if got := r.st.calls.Load() - base; got != d {
			t.Fatalf("mode %s: injection must hit the last lookup (guard); lookups %d want %d", mode, got, d)
		}
	}
}

// 写方法不暴露（路由只注册 GET）。
func TestMonitorSnapshotIsGetOnly(t *testing.T) {
	r := newMonitorRig(t)
	req, _ := http.NewRequest("POST", r.srv.URL+monitorPath, strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "tok-admin"})
	req.Header.Set("Origin", "https://dw.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s: %d", monitorPath, res.StatusCode)
	}
}

// S1：route 标签只能是实际注册的 pattern（去掉方法前缀）或固定的 _unmatched；方法只能落在固定集合（其他归 OTHER）。
// 用真实的 wireNewPlatform＋registerConsoleRoutes 注册表，打一批已注册、未注册、带 id、带 query 的请求。
func TestMonitorRouteLabelsAreRegisteredPatternsOnly(t *testing.T) {
	hub := newplatHub(t)
	rec := &routeRecorder{mux: http.NewServeMux()}
	if err := wireNewPlatform(context.Background(), rec, hub, newPlatformConfig{publicOrigin: "https://dw.example"}, identity.NewMemStore(), nil); err != nil {
		t.Fatal(err)
	}
	registerConsoleRoutes(rec, hub, true)
	allowed := map[string]bool{"_unmatched": true}
	for _, p := range rec.patterns {
		if i := strings.IndexByte(p, ' '); i >= 0 {
			p = strings.TrimSpace(p[i+1:])
		}
		allowed[p] = true
	}
	reg := obs.New(nil)
	srv := httptest.NewServer(reg.Middleware("public", rec.mux))
	defer srv.Close()
	reqs := []struct{ method, path string }{
		{"GET", "/console"}, {"GET", "/api/console/platform"}, {"GET", monitorPath},
		{"GET", "/view/some-agent.zzc"}, {"POST", "/view/some-agent.zzc/input?token=abc"},
		{"GET", "/api/console/agents"}, {"GET", "/no/such/route/123"}, {"PROPFIND", "/console"},
		{"DELETE", "/api/console/event-feeds/feed-xyz"}, {"GET", "/ws/view/agent-q"},
	}
	for _, q := range reqs {
		req, _ := http.NewRequest(q.method, srv.URL+q.path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
	snap := reg.Snapshot()
	if len(snap.HTTP.Series) == 0 {
		t.Fatal("no series recorded")
	}
	methods := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true, "HEAD": true, "OPTIONS": true, "OTHER": true}
	sawOther := false
	for _, s := range snap.HTTP.Series {
		if !allowed[s.Route] {
			t.Errorf("route label %q is not a registered pattern", s.Route)
		}
		if !methods[s.Method] {
			t.Errorf("method label %q outside fixed set", s.Method)
		}
		if s.Method == "OTHER" {
			sawOther = true
		}
		for _, bad := range []string{"some-agent", "feed-xyz", "token", "123", "agent-q"} {
			if strings.Contains(s.Route, bad) {
				t.Errorf("route label %q carries request data %q", s.Route, bad)
			}
		}
	}
	if !sawOther {
		t.Errorf("PROPFIND must be recorded under OTHER: %+v", snap.HTTP.Series)
	}
}

// 独立页与快照接口同一套 guard；页面请求（Accept html）未登录／改密／失活走 302，非 platform_admin 一律 403。
func TestMonitorPageSameGuardAsSnapshot(t *testing.T) {
	r := newMonitorRig(t)
	page := func(tok string) (int, string, string, string) {
		req, _ := http.NewRequest("GET", r.srv.URL+"/console/monitor", nil)
		req.Header.Set("Accept", "text/html")
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		}
		res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Location"), res.Header.Get("Content-Security-Policy"), string(b)
	}
	code, _, csp, body := page("tok-admin")
	if code != 200 || csp == "" || !strings.Contains(body, "/api/console/monitor/snapshot") {
		t.Fatalf("admin page: %d csp=%q", code, csp)
	}
	for _, c := range []struct {
		tok      string
		code     int
		loc, has string
	}{
		{"tok-mgr", 403, "", "platform_admin_required"},
		{"tok-mem", 403, "", "platform_admin_required"},
		{"", 302, "/console/login", ""},
		{"tok-nobody", 302, "/console/login", ""},
		{"tok-admin2", 302, "/console/login#change", ""},
		{"tok-admin3", 302, "/console/login", ""},
	} {
		code, loc, _, body := page(c.tok)
		if code != c.code || loc != c.loc || !strings.Contains(body, c.has) || strings.Contains(body, "monitor/snapshot") {
			t.Errorf("token %q: got %d loc=%q body=%.80q; want %d loc=%q", c.tok, code, loc, body, c.code, c.loc)
		}
	}
}
