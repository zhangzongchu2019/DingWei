package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func testHostedAccess(t *testing.T, raw string) hostedAccess {
	t.Helper()
	p, err := parseHostedAccess(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHostedAccessConfiguration(t *testing.T) {
	for _, raw := range []string{"", "  ", "owner", "owner, manager", "owner,owner"} {
		p, err := parseHostedAccess(raw)
		if err != nil {
			t.Fatalf("valid %q: %v", raw, err)
		}
		if strings.TrimSpace(raw) == "" && len(p.users) != 0 {
			t.Fatal("empty policy opened")
		}
	}
	for _, raw := range []string{"Owner", "owner,", ",owner", "owner,,admin", "*", "a", "owner.agent", "用户"} {
		if _, err := parseHostedAccess(raw); err == nil {
			t.Fatalf("accepted invalid %q", raw)
		}
	}
}

func fenceIdentity(t *testing.T) (*identity.MemStore, func(*http.Request) (string, error)) {
	t.Helper()
	s := identity.NewMemStore()
	svc := identity.NewService(s, nil, nil)
	for _, u := range []identity.User{
		{UserID: "owner", ShortName: "own", Type: identity.UserPlatformAdmin, State: identity.UserActive},
		{UserID: "admin", ShortName: "adm", Type: identity.UserPlatformAdmin, State: identity.UserActive},
		{UserID: "manager", ShortName: "mgr", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "member", ShortName: "mem", Type: identity.UserMember, State: identity.UserActive},
	} {
		if err := s.CreateUser(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateSession(context.Background(), identity.Session{UserIDLC: u.UserIDLC(), TokenHash: identity.HashSecret("fixture-" + u.UserID), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	n3 := &httpapi.N3{OB: identity.NewOnboarding(svc)}
	return s, n3.Actor
}

type fenceRoute struct{ method, path string }

func fenceRoutes() []fenceRoute {
	r := []fenceRoute{
		{"GET", "/api/console/hosted/options"}, {"POST", "/api/console/hosted/agents"},
		{"GET", "/api/console/hosted/jobs/missing"},
		{"GET", "/api/console/admin/quotas"}, {"PUT", "/api/console/admin/quotas/member"},
		{"GET", "/api/console/llm-accounts"}, {"POST", "/api/console/llm-accounts"},
		{"PUT", "/api/console/llm-accounts/missing/secret"},
		{"POST", "/api/console/llm-accounts/missing/disable"}, {"POST", "/api/console/llm-accounts/missing/enable"},
		{"GET", "/api/console/admin/llm-accounts"},
		{"POST", "/api/console/admin/llm-accounts/missing/disable"}, {"POST", "/api/console/admin/llm-accounts/missing/enable"},
		{"POST", "/api/console/llm-account-logins"}, {"GET", "/api/console/llm-account-logins/missing"}, {"POST", "/api/console/llm-account-logins/missing/cancel"},
		{"GET", "/api/console/hosted/quota-monitor"}, {"GET", "/console/hosted-quota"},
		{"POST", "/api/console/hosted/nodes/missing/holds/missing/release"},
	}
	for _, op := range []string{"upgrade", "restart", "disable", "enable", "destroy", "refresh_cred"} {
		r = append(r, fenceRoute{"POST", "/api/console/hosted/agents/missing/" + op})
	}
	return r
}

type fenceUnreadBody struct{}

func (fenceUnreadBody) Read([]byte) (int, error) { panic("denied request body read") }
func (fenceUnreadBody) Close() error             { return nil }

func fenceRequest(route fenceRoute, token, bearer string, body io.Reader) *http.Request {
	r := httptest.NewRequest(route.method, route.path, body)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
	}
	if bearer != "" {
		r.Header.Set("Authorization", bearer)
	}
	// None of these caller-supplied names grant authority.
	r.Header.Set("X-User-ID", "owner")
	r.Header.Set("X-Test-Actor", "owner")
	r.Header.Set("X-Forwarded-User", "owner")
	r.Header.Set("X-Role", "platform_admin")
	r.Header.Set("Origin", testRouteOrigin)
	return r
}

func TestHostedAccessEveryHumanRouteRejectsBeforeBusiness(t *testing.T) {
	_, actor := fenceIdentity(t)
	for _, policy := range []string{"owner", "", "   "} {
		p := testHostedAccess(t, policy)
		mux := http.NewServeMux()
		// Nil business dependencies deliberately fail if any denied handler runs.
		registerHostedRoutes(mux, hostedDeps{access: p, actor: actor, login: &hosted.LoginService{}})
		hostedQuotaRoutes{access: p, actor: actor, owner: "owner"}.register(mux)
		callers := []struct{ token, bearer string }{{"", ""}, {"fixture-admin", ""}, {"fixture-manager", ""}, {"fixture-member", ""}, {"forged-owner", ""}, {"", "Bearer agent-fixture"}, {"fixture-owner", "Bearer agent-fixture"}, {"fixture-owner", "Basic forged"}}
		if strings.TrimSpace(policy) == "" {
			callers = append(callers, struct{ token, bearer string }{"fixture-owner", ""})
		}
		for _, route := range fenceRoutes() {
			for _, c := range callers {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, fenceRequest(route, c.token, c.bearer, fenceUnreadBody{}))
				if w.Code != 403 || strings.TrimSpace(w.Body.String()) != `{"error":"forbidden"}` {
					t.Fatalf("policy=%q %s %s caller=%q bearer=%t: %d %s", policy, route.method, route.path, c.token, c.bearer != "", w.Code, w.Body.String())
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("denial cacheable")
				}
			}
		}
	}
}

func TestHostedAccessAllowedRealSessionReachesEveryBusinessRoute(t *testing.T) {
	deps, st, _, gw, pool := mkHostedDeps(t)
	_, actor := fenceIdentity(t)
	deps.access = testHostedAccess(t, "owner")
	deps.actor = actor
	deps.ownsAgent = func(*http.Request, string, string) bool { return true }
	deps.isManager = func(*http.Request, string) bool { return true }
	deps.login = hosted.NewLoginService(st, pool, gw, fakeImagesRT{}, deps.orch.AccountComboKnown, testRouteOrigin)
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	svc := identity.NewService(identity.NewMemStore(), nil, nil)
	// Quota owner tests below use the full existing real-identity rig; here a
	// non-owner business denial distinguishes the inner handler from the fence.
	hostedQuotaRoutes{access: deps.access, actor: actor, identity: svc, owner: "different", store: st}.register(mux)
	for _, route := range fenceRoutes() {
		if strings.Contains(route.path, "quota-monitor") || route.path == "/console/hosted-quota" || strings.Contains(route.path, "/holds/") {
			continue
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, fenceRequest(route, "fixture-owner", "", strings.NewReader(`{}`)))
		if w.Code == 403 && strings.TrimSpace(w.Body.String()) == `{"error":"forbidden"}` {
			t.Fatalf("allowed route fenced: %s %s", route.method, route.path)
		}
		if w.Code == 405 || w.Code >= 500 {
			t.Fatalf("allowed route not functioning: %s %s %d %s", route.method, route.path, w.Code, w.Body.String())
		}
	}
}

func TestHostedAccessFreshIdentityAndStartupSnapshot(t *testing.T) {
	s, actor := fenceIdentity(t)
	p := testHostedAccess(t, "owner")
	allowed := func(token string) bool {
		_, ok := p.human(fenceRequest(fenceRoute{"GET", "/"}, token, "", nil), actor)
		return ok
	}
	if !allowed("fixture-owner") {
		t.Fatal("owner denied")
	}
	// Role promotion never extends the list; active newcomers with sessions also
	// stay fenced regardless of the platform's default hosted quota.
	if err := s.SetUserType(context.Background(), "manager", identity.UserPlatformAdmin); err != nil {
		t.Fatal(err)
	}
	if allowed("fixture-manager") {
		t.Fatal("role promotion bypass")
	}
	if err := s.CreateUser(context.Background(), identity.User{UserID: "newuser", ShortName: "new", Type: identity.UserPlatformAdmin, State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(context.Background(), identity.Session{UserIDLC: "newuser", TokenHash: identity.HashSecret("fixture-newuser"), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if allowed("fixture-newuser") {
		t.Fatal("new active user bypass")
	}
	t.Setenv("DW_G4_ACCESS_ALLOW_USER_IDS", "newuser")
	if !allowed("fixture-owner") || allowed("fixture-newuser") {
		t.Fatal("policy changed without restart")
	}
	if err := s.SetUserState(context.Background(), "owner", identity.UserDisabled, time.Now()); err != nil {
		t.Fatal(err)
	}
	if allowed("fixture-owner") {
		t.Fatal("existing session bypassed disable")
	}
	if err := s.SetUserState(context.Background(), "owner", identity.UserActive, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(context.Background(), identity.HashSecret("fixture-owner"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if allowed("fixture-owner") {
		t.Fatal("revoked session bypass")
	}
}

func TestHostedAccessEmptyListPreservesOnlyRegisteredOpsBearer(t *testing.T) {
	d, _, credential, g := quotaRouteRig(t)
	d.access = hostedAccess{}
	mux := http.NewServeMux()
	d.register(mux)
	for _, c := range []struct {
		route         fenceRoute
		token, bearer string
		code          int
	}{
		{fenceRoute{"GET", "/api/console/hosted/quota-monitor"}, "fixture-owner", "Bearer " + credential, 200},
		{fenceRoute{"GET", "/api/console/hosted/quota-monitor"}, "tok-owner", "", 403},
		{fenceRoute{"GET", "/api/console/hosted/quota-monitor"}, "", "Bearer invalid", 403},
		{fenceRoute{"GET", "/console/hosted-quota"}, "tok-owner", "Bearer " + credential, 403},
		{fenceRoute{"POST", "/api/console/hosted/nodes/nd_release/holds/ag_release/release"}, "tok-owner", "Bearer " + credential, 403},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, fenceRequest(c.route, c.token, c.bearer, strings.NewReader(`{}`)))
		if w.Code != c.code {
			t.Fatalf("ops boundary %s: %d %s", c.route.path, w.Code, w.Body.String())
		}
		if c.code == 200 && (strings.Contains(w.Body.String(), `"can_release":true`) || strings.Contains(w.Body.String(), "private-secret")) {
			t.Fatal("ops authority/disclosure")
		}
	}
	if g.calls != 0 {
		t.Fatal("ops sent command")
	}
}

func TestHostedAccessRegistrationCannotActivateHostedAccess(t *testing.T) {
	s, actor := fenceIdentity(t)
	svc := identity.NewService(s, nil, nil)
	ob := identity.NewOnboarding(svc)
	admin, err := s.GetUser(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	_, code, _, err := ob.CreateUser(context.Background(), admin, identity.CreateUserRequest{UserID: "newbound", ShortName: "nb", Type: identity.UserMember})
	if err != nil {
		t.Fatal(err)
	}
	reply := ob.HandleRegister(context.Background(), identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "newbound-fixture"}, code)
	if !strings.Contains(reply, "绑定成功") {
		t.Fatalf("real registration failed: %s", reply)
	}
	u, err := s.GetUser(context.Background(), "newbound")
	if err != nil || u.State != identity.UserActive {
		t.Fatal("registration did not activate")
	}
	if err := s.CreateSession(context.Background(), identity.Session{UserIDLC: "newbound", TokenHash: identity.HashSecret("fixture-newbound"), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerHostedRoutes(mux, hostedDeps{access: testHostedAccess(t, "owner"), actor: actor})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, fenceRequest(fenceRoute{"GET", "/api/console/hosted/options"}, "fixture-newbound", "", nil))
	if w.Code != 403 {
		t.Fatalf("newly registered user bypassed fence: %d", w.Code)
	}
}

func TestHostedAccessMustChangeExpiredAndMissingIdentity(t *testing.T) {
	s, actor := fenceIdentity(t)
	p := testHostedAccess(t, "owner")
	if err := s.SetPassword(context.Background(), "owner", "fixture-hash", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.human(fenceRequest(fenceRoute{"GET", "/"}, "fixture-owner", "", nil), actor); ok {
		t.Fatal("must-change session accepted")
	}
	if err := s.SetPassword(context.Background(), "owner", "fixture-hash", false); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(context.Background(), identity.Session{UserIDLC: "owner", TokenHash: identity.HashSecret("fixture-expired"), ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.human(fenceRequest(fenceRoute{"GET", "/"}, "fixture-expired", "", nil), actor); ok {
		t.Fatal("expired session accepted")
	}
	if _, ok := p.human(fenceRequest(fenceRoute{"GET", "/"}, "fixture-owner", "", nil), nil); ok {
		t.Fatal("nil resolver accepted")
	}
}

func TestHostedAccessRealAssemblyUnsetEmptyAndNamed(t *testing.T) {
	for _, policy := range []string{"<unset>", "", "owner"} {
		t.Run(policy, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			key := filepath.Join(t.TempDir(), "fixture.key")
			if err := os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DW_G4_MASTER_KEY_FILE", key)
			if policy == "<unset>" {
				t.Setenv("DW_G4_ACCESS_ALLOW_USER_IDS", "")
				if err := os.Unsetenv("DW_G4_ACCESS_ALLOW_USER_IDS"); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("DW_G4_ACCESS_ALLOW_USER_IDS", policy)
			}
			s, _ := fenceIdentity(t)
			hub := newplatHub(t)
			mux := http.NewServeMux()
			if err := wireNewPlatform(ctx, mux, hub, newPlatformConfig{publicOrigin: testRouteOrigin}, s, pgtest.Pool(t)); err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{"fixture-owner", "fixture-admin", "fixture-manager", "fixture-member", ""} {
				allowed := policy == "owner" && token == "fixture-owner"
				for _, route := range fenceRoutes() {
					if allowed {
						continue
					} // Every positive business route has its dedicated fixture above.
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, fenceRequest(route, token, "", fenceUnreadBody{}))
					if w.Code != 403 || strings.TrimSpace(w.Body.String()) != `{"error":"forbidden"}` {
						t.Fatalf("assembly %q %s %s: %d %s", policy, token, route.path, w.Code, w.Body.String())
					}
				}
				w := httptest.NewRecorder()
				hub.HandleConsolePage(w, fenceRequest(fenceRoute{"GET", "/console"}, token, "", nil))
				flag := "false"
				if allowed {
					flag = "true"
				}
				if !strings.Contains(w.Body.String(), "var FEATURE_HOSTED='"+flag+"'==='true';") {
					t.Fatalf("page identity/assembly mismatch policy=%q caller=%q", policy, token)
				}
			}
			if policy == "owner" {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, fenceRequest(fenceRoute{"GET", "/api/console/hosted/options"}, "fixture-owner", "", nil))
				if w.Code != 200 {
					t.Fatalf("real allowed options %d %s", w.Code, w.Body.String())
				}
				t.Setenv("DW_G4_ACCESS_ALLOW_USER_IDS", "admin")
				for _, token := range []string{"fixture-owner", "fixture-admin"} {
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, fenceRequest(fenceRoute{"GET", "/api/console/hosted/options"}, token, "", nil))
					want := 403
					if token == "fixture-owner" {
						want = 200
					}
					if w.Code != want {
						t.Fatal("live env changed policy")
					}
				}
			}
			// Machine endpoints keep their existing authenticators, independently of the human list.
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/node/login/missing", strings.NewReader(`{}`)))
			if w.Code != 404 || strings.TrimSpace(w.Body.String()) != `{"error":"not_found"}` {
				t.Fatalf("node login existing missing-session contract changed: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
