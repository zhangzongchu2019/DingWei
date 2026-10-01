package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// routeRecorder 记下真实注册了哪些路由，再交给 ServeMux。
type routeRecorder struct {
	mux      *http.ServeMux
	patterns []string
}

func (r *routeRecorder) HandleFunc(p string, h func(http.ResponseWriter, *http.Request)) {
	r.patterns = append(r.patterns, p)
	r.mux.HandleFunc(p, h)
}

// newPlatformAuthExempt 新平台模式下【不】按 N3 会话认人的路由，每条写明凭什么放行。
// 不在此表的路由一律按 N3 会话认人；新增路由若漏包认人中间件，下面的扫描用例必红（组长裁定 140013Z ③）。
var newPlatformAuthExempt = map[string]string{
	"POST /console/login/password":       "登录本身",
	"POST /console/login/code":           "登录本身（取登录码）",
	"GET /console/login/status":          "登录轮询，凭一次性登录码",
	"POST /console/logout":               "退出；无会话也幂等",
	"POST /api/console/account/password": "改密：必须改密的会话正需要它；自身校验旧口令（另有一格：无会话 401）",
	"GET /ws/agent":                      "Agent 专属凭据",
	"POST /api/enroll/applications":      "客户端入网申请：凭配对码摘要",
	"GET /api/enroll/applications/{id}":  "客户端轮询：凭轮询令牌",
}

// 新平台全路由扫描：路由取自 wireNewPlatform 与 registerConsoleRoutes 的【真实注册】（与 run 同一份），
// 对每条非豁免路由，以「无会话」与「必须改密的会话」各发 JSON 与 HTML 两种请求：
//
//	浏览器页面（GET＋text/html，非 /api/、非 WebSocket）⇒ 302 /console/login 与 /console/login#change
//	其余（含 /api/console/* 带 text/html）              ⇒ 401 unauthenticated 与 403 must_change_password
func TestNewPlatformRouteSweep(t *testing.T) {
	hub := newplatHub(t)
	rec := &routeRecorder{mux: http.NewServeMux()}
	const origin = "https://dw.szuma.net:55055"
	if err := wireNewPlatform(context.Background(), rec, hub, newPlatformConfig{publicOrigin: origin}, identity.NewMemStore()); err != nil {
		t.Fatal(err)
	}
	registerConsoleRoutes(rec, hub, true)
	ctx := context.Background()
	if _, err := hub.Onboarding.BootstrapAdmin(ctx, "Root", "rt", ""); err != nil { // 初始化脚本建的管理用户：必须改密
		t.Fatal(err)
	}
	if err := hub.Identity.Store.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("tok-must"), UserIDLC: "root", ExpiresAt: hub.Identity.Now().Add(identity.SessionTTL)}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(rec.mux)
	defer srv.Close()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func(method, path, tok, accept string) (int, string, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if strings.HasPrefix(path, "/ws/") {
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
		}
		res, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b := make([]byte, 200)
		n, _ := res.Body.Read(b)
		return res.StatusCode, res.Header.Get("Location"), strings.TrimSpace(string(b[:n]))
	}

	seen := map[string]bool{}
	for _, p := range rec.patterns {
		seen[p] = true
	}
	// 防空转：扫描必须真的拿到了路由（记录器坏了、注册函数被绕过时这里先红）。
	// 防空转，也是验收三格之①（板 003a5de4：新平台下 /console、/api/console/platform、/api/console/agent-groups 曾 404）。
	for _, must := range []string{"GET /console", "GET /api/console/platform", "GET /api/console/agent-groups", "GET /api/console/agents",
		"GET /api/console/enroll/applications", "GET /view/{sessionName}", "/view/{sessionName}/{rest...}", "GET /ws/view/{sessionName}"} {
		if !seen[must] {
			t.Fatalf("route %q not registered; recorded %d routes", must, len(rec.patterns))
		}
	}
	// 新平台第一版不含 specs 与 task-board：这两组路由一律不注册（含旧共享 spec 页，140013Z ④），访问 404
	for _, p := range rec.patterns {
		if specTaskRoute.MatchString(p) {
			t.Errorf("specs/task-board route registered in new-platform mode: %s", p)
		}
	}
	for _, path := range []string{"/console/specs", "/console/tasks", "/console/specs/x", "/console/spec-assets/x.js",
		"/api/console/specs", "/api/console/task-board", "/api/console/spec-editors", "/api/specs/shared/x"} {
		if st, _, _ := do("GET", path, "", "application/json"); st != 404 {
			t.Errorf("GET %s: %d, want 404 (not registered)", path, st)
		}
	}
	for p := range newPlatformAuthExempt {
		if !seen[p] {
			t.Errorf("exempt route %q is no longer registered; drop it from the exempt list", p)
		}
	}
	if st, _, body := do("POST", "/api/console/account/password", "", "application/json"); st != 401 || !strings.Contains(body, "unauthenticated") {
		t.Errorf("password change without session: %d %s", st, body)
	}

	param := regexp.MustCompile(`\{[^}]*\}`)
	keys := append([]string(nil), rec.patterns...)
	sort.Strings(keys)
	checked := 0
	for _, k := range keys {
		if _, ok := newPlatformAuthExempt[k]; ok {
			continue
		}
		methods, path := []string{"GET", "POST"}, k // 不带方法的模式两种都扫
		if i := strings.Index(k, " "); i > 0 {
			methods, path = []string{k[:i]}, k[i+1:]
		}
		concrete := param.ReplaceAllString(path, "x")
		for _, method := range methods {
			for _, accept := range []string{"application/json", "text/html,application/xhtml+xml"} {
				page := method == "GET" && strings.HasPrefix(accept, "text/html") && !strings.HasPrefix(concrete, "/api/") && !strings.HasPrefix(concrete, "/ws/")
				for _, c := range []struct {
					tok, wantLoc, wantErr string
					wantStatus            int
				}{
					{"", "/console/login", "unauthenticated", 401},
					{"tok-must", "/console/login#change", "must_change_password", 403},
				} {
					st, loc, body := do(method, concrete, c.tok, accept)
					checked++
					if page {
						if st != 302 || loc != c.wantLoc {
							t.Errorf("%s %s [%s] session=%q: %d loc=%q, want 302 %s", method, path, accept, c.tok, st, loc, c.wantLoc)
						}
					} else if st != c.wantStatus || !strings.Contains(body, c.wantErr) {
						t.Errorf("%s %s [%s] session=%q: %d %s, want %d %s", method, path, accept, c.tok, st, body, c.wantStatus, c.wantErr)
					}
				}
			}
		}
	}
	t.Logf("routes recorded=%d exempt=%d requests checked=%d", len(rec.patterns), len(newPlatformAuthExempt), checked)
}

// specTaskRoute specs 与 task-board 两组功能的路由形状（页面、console 接口、Agent capability 接口、共享链接）。
var specTaskRoute = regexp.MustCompile(`/(console/specs|console/tasks|console/spec-assets|api/console/specs|api/console/task-|api/console/spec-editors|api/agent/console/|api/specs/shared)`)
