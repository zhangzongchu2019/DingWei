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
	"POST /api/enroll/claims":            "临时无属主申请：限频与短期码，不能匿名建号",
	"GET /api/enroll/claims/{id}":        "设备私有轮询凭证",
	"POST /api/enroll/claims/{id}/ack":   "设备私有领取确认凭证",
	"POST /console/login/password":       "登录本身",
	"POST /console/login/code":           "登录本身（取登录码）",
	"GET /console/login/status":          "登录轮询，凭一次性登录码",
	"POST /console/logout":               "退出；无会话也幂等",
	"POST /api/console/account/password": "改密：必须改密的会话正需要它；自身校验旧口令（另有一格：无会话 401）",
	"GET /ws/agent":                      "Agent 专属凭据",
	"POST /api/enroll/applications":      "客户端入网申请：凭配对码摘要",
	"GET /api/enroll/applications/{id}":  "客户端轮询：凭轮询令牌",
}

// newPlatformSpecTaskExempt spec／任务路由里【不】按 N3 会话认人的（R-V2-SPEC-TASK-SHIM-IMPL H1），口径同 095：
// 共享链接与静态资源公开读、写凭链接里的编辑能力令牌；Agent 侧凭专属凭据＋能力令牌。每条下面另有一格钉住无凭据时的应答。
var newPlatformSpecTaskExempt = map[string]string{
	"GET /console/specs/{specID}":                                        "共享 spec 页（公开页，095 同）",
	"GET /console/spec-assets/{name}":                                    "spec 页静态资源（公开，095 同）",
	"GET /api/specs/shared/{specID}":                                     "共享 spec 读（公开，095 同）",
	"PATCH /api/specs/shared/{specID}":                                   "共享 spec 写：凭编辑能力令牌（无令牌按登录者判，均无 ⇒ 拒）",
	"POST /api/specs/shared/{specID}/versions":                           "共享 spec 开新版：同上",
	"GET /api/agent/console/task-driver/specs/{specID}/{revision}/tasks": "Agent 拉任务：专属凭据＋能力令牌",
	"POST /api/agent/console/specs":                                      "Agent 建 spec：专属凭据＋能力令牌",
	"PATCH /api/agent/console/specs/{specID}":                            "Agent 改 spec：专属凭据＋能力令牌",
	"POST /api/agent/console/specs/{specID}/inferences":                  "Agent 回推论：专属凭据＋能力令牌",
}

// newPlatformOptionalExempt 只在某些编译形态下才注册的公开路由：注册了 ⇒ 按豁免处理，并且必须真的公开（无会话 200）；
// 没注册 ⇒ 不要求（与 newPlatformAuthExempt 不同，后者要求「豁免项必须已注册」）。
var newPlatformOptionalExempt = map[string]string{
	// registerNewPlatformLoginPage 只在 Hub 带有登录页实现（tosonnet50ts/g3-enroll-client 合入）时注册；
	// 登录页本身必须不认人，否则未登录的人永远到不了登录页。
	"GET /console/login": "登录页本身（公开页）；仅在页面件合入后注册",
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
	if err := wireNewPlatform(context.Background(), rec, hub, newPlatformConfig{publicOrigin: origin}, identity.NewMemStore(), nil); err != nil {
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
	// R-V2-SPEC-TASK-SHIM-IMPL G4（翻转原「一条都不能注册」）：spec 与任务路由在新平台全部注册，且与旧平台注册的是同一组。
	// 认人：非豁免的照常进下面的全路由扫描（无会话 401／302、必须改密 403）；豁免的逐条钉住无凭据应答。
	oldRec := &routeRecorder{mux: http.NewServeMux()}
	registerSpecTaskRoutes(oldRec, newplatHub(t))
	for _, p := range oldRec.patterns {
		if !seen[p] {
			t.Errorf("spec/task route %q not registered in new-platform mode", p)
		}
	}
	nSpecTask := 0
	for _, p := range rec.patterns {
		if specTaskRoute.MatchString(p) {
			nSpecTask++
		}
	}
	if nSpecTask != len(oldRec.patterns) || nSpecTask != 35 {
		t.Errorf("spec/task routes in new-platform mode=%d, registerSpecTaskRoutes=%d (want both 35)", nSpecTask, len(oldRec.patterns))
	}
	for p := range newPlatformSpecTaskExempt {
		if !seen[p] {
			t.Errorf("spec/task exempt route %q is not registered", p)
		}
		if _, dup := newPlatformAuthExempt[p]; dup {
			t.Errorf("%q is in two exempt lists", p)
		}
	}
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/console/specs/x", 200},             // 共享页外壳（内容由页面再取，取不到即提示）
		{"GET", "/console/spec-assets/nope.js", 404}, // 不认识的资源名
		{"GET", "/api/specs/shared/nope", 404},
		{"PATCH", "/api/specs/shared/nope", 404}, // 查无此 spec 先于判权
		{"POST", "/api/specs/shared/nope/versions", 404},
		{"GET", "/api/agent/console/task-driver/specs/x/1/tasks", 403},
		{"POST", "/api/agent/console/specs", 403},
		{"PATCH", "/api/agent/console/specs/x", 403},
		{"POST", "/api/agent/console/specs/x/inferences", 403},
	} {
		if st, loc, body := do(c.method, c.path, "", "application/json"); st != c.want {
			t.Errorf("exempt %s %s without credentials: %d loc=%q %s, want %d", c.method, c.path, st, loc, body, c.want)
		}
	}
	// 可选豁免：注册了就必须真公开 —— 无会话、不跟随跳转地取 HTML，得 200（不是 302 回登录页、也不是 401）。
	for p := range newPlatformOptionalExempt {
		if _, dup := newPlatformAuthExempt[p]; dup {
			t.Errorf("%q is in both exempt lists", p)
		}
		if !seen[p] {
			continue
		}
		path := p[strings.Index(p, " ")+1:]
		if st, loc, _ := do("GET", path, "", "text/html,application/xhtml+xml"); st != 200 {
			t.Errorf("optional public route %s: %d loc=%q, want 200 without a session", p, st, loc)
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
		if _, ok := newPlatformOptionalExempt[k]; ok {
			continue
		}
		if _, ok := newPlatformSpecTaskExempt[k]; ok {
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
