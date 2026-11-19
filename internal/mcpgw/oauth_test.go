package mcpgw

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/extwire"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

const verifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"

func authorizationQuery() url.Values {
	sum := sha256.Sum256([]byte(verifier))
	return url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {clientRedirect}, "resource": {testOrigin + "/mcp"}, "scope": {extshared.ScopeString()}, "state": {"outer-state"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}}
}
func oauthFixture(t *testing.T) (*Gateway, *seatsFake, *http.ServeMux) {
	return oauthFixtureWithStore(t, nil)
}
func oauthFixtureWithStore(t *testing.T, store Store) (*Gateway, *seatsFake, *http.ServeMux) {
	return oauthConsentFixture(t, store, nil)
}
func oauthConsentFixture(t *testing.T, store Store, defaults feishulogin.DefaultTargets) (*Gateway, *seatsFake, *http.ServeMux) {
	t.Helper()
	g, seats, _ := fixture(t)
	if store != nil {
		g.cfg.Store = store
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.ParseForm() != nil || r.PostForm.Get("code") != "feishu-code" || r.PostForm.Get("redirect_uri") != testOrigin+"/mcp/oauth/feishu/callback" {
				t.Error("upstream code/redirect not bound")
				w.WriteHeader(400)
				return
			}
			writeJSON(w, 200, map[string]any{"access_token": "feishu-private-token", "token_type": "Bearer", "code": 0})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer feishu-private-token" {
				t.Error("missing upstream token")
			}
			writeJSON(w, 200, map[string]any{"code": 0, "data": feishulogin.UserInfo{Name: "名字", EnName: "Alice", OpenID: "open-trusted", TenantKey: "team", UnionID: "union"}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(upstream.Close)
	oauth, e := feishulogin.NewFeishuClient(feishulogin.OAuthConfig{AppID: "app", AppSecret: "test-only-secret", RedirectURI: testOrigin + "/mcp/oauth/feishu/callback", AuthorizeURL: upstream.URL + "/authorize", TokenURL: upstream.URL + "/token", UserInfoURL: upstream.URL + "/user", HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	login, e := feishulogin.NewHandler(feishulogin.Config{AppID: "app", TeamTenantKey: "team", RedirectURI: testOrigin + "/mcp/oauth/feishu/callback", Defaults: defaults, OAuth: oauth, States: LoginStates{Store: g.cfg.Store}, Tokens: extwire.Issuer{Seats: seats, Resource: testOrigin + "/mcp"}, Now: g.cfg.Now})
	if e != nil {
		t.Fatal(e)
	}
	g.cfg.Login = login
	mux := http.NewServeMux()
	g.Register(mux)
	return g, seats, mux
}
func browserStart(t *testing.T, mux *http.ServeMux) (string, []*http.Cookie) {
	t.Helper()
	w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+authorizationQuery().Encode(), nil))
	if w.Code != 302 {
		t.Fatalf("authorize %d %s", w.Code, w.Body)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	return u.Query().Get("attempt_id"), w.Result().Cookies()
}
func addCookies(r *http.Request, cookies []*http.Cookie) {
	for _, c := range cookies {
		r.AddCookie(c)
	}
}
func finishFeishu(t *testing.T, mux *http.ServeMux, attempt string, cookies []*http.Cookie) (string, *httptest.ResponseRecorder) {
	t.Helper()
	r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+url.QueryEscape(attempt), nil)
	addCookies(r, cookies)
	w := serve(mux, r)
	if w.Code != 200 {
		t.Fatalf("connect %d %s", w.Code, w.Body)
	}
	re := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	match := re.FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("no C CSRF")
	}
	form := url.Values{"attempt_id": {attempt}, "csrf_token": {html.UnescapeString(match[1])}, "short_name": {"bot"}, "target_agent": {"target.zzc"}}
	r = httptest.NewRequest("POST", testOrigin+"/feishu/connect", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", testOrigin)
	addCookies(r, cookies)
	w = serve(mux, r)
	if w.Code != 303 {
		t.Fatalf("C Start %d %s", w.Code, w.Body)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	state := u.Query().Get("state")
	callbackURL := testOrigin + "/mcp/oauth/feishu/callback?" + url.Values{"state": {state}, "code": {"feishu-code"}, "redirect_uri": {"https://attacker.example"}, "tenant_key": {"evil"}, "target_agent": {"evil.zzc"}}.Encode()
	r = httptest.NewRequest("GET", callbackURL, nil)
	addCookies(r, cookies)
	w = serve(mux, r)
	return callbackURL, w
}
func mintCode(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	attempt, cookies := browserStart(t, mux)
	_, w := finishFeishu(t, mux, attempt, cookies)
	if w.Code != 302 {
		t.Fatalf("callback %d %s", w.Code, w.Body)
	}
	u, e := url.Parse(w.Header().Get("Location"))
	if e != nil || u.Scheme != "http" || u.Host != "127.0.0.1:43123" || u.Query().Get("state") != "outer-state" || u.Query().Get("iss") != testOrigin {
		t.Fatalf("unbound redirect %s", w.Header().Get("Location"))
	}
	if strings.Contains(w.Header().Get("Location"), "local-token") {
		t.Fatal("token in browser URL")
	}
	return u.Query().Get("code")
}
func tokenForm(code string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {clientRedirect}, "resource": {testOrigin + "/mcp"}, "code_verifier": {verifier}}
}
func redeem(mux http.Handler, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", testOrigin+"/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serve(mux, r)
}
func TestOAuthRealCAndExtwireFlow(t *testing.T) {
	_, s, mux := oauthFixture(t)
	code := mintCode(t, mux)
	w := redeem(mux, tokenForm(code))
	if w.Code != 200 {
		t.Fatalf("token %d %s", w.Code, w.Body)
	}
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out["access_token"] != "local-token" || out["token_type"] != "Bearer" || out["scope"] != extshared.ScopeString() || out["refresh_token"] != nil {
		t.Fatalf("invalid token response %s", w.Body)
	}
	if len(s.issued) != 1 || s.issued[0].Principal.Subject != "open-trusted" || s.issued[0].Principal.TenantKey != "team" || s.issued[0].ShortName != "bot" || s.issued[0].TargetAgent != "target.zzc" {
		t.Fatalf("C/A intent not server-bound")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token cacheable")
	}
	// 换到的令牌能直接调用，无 initialize；REST 共用同一认证。
	if w = serve(mux, rpcRequestFor("tools/call", "list_online", `{}`)); w.Code != 200 {
		t.Fatal("issued token unusable")
	}
}
func TestOAuthCodeReplay(t *testing.T) {
	_, _, mux := oauthFixture(t)
	code := mintCode(t, mux)
	if w := redeem(mux, tokenForm(code)); w.Code != 200 {
		t.Fatal("first exchange failed")
	}
	if w := redeem(mux, tokenForm(code)); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Fatalf("authorization code reusable: %d %s", w.Code, w.Body)
	}
}
func TestOAuthPKCEAndBindings(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"pkce", "code_verifier", strings.Repeat("z", 43)},
		{"redirect", "redirect_uri", "https://attacker.example/callback"},
		{"client", "client_id", "https://other.example/metadata.json"},
		{"resource", "resource", "https://other.example/mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, mux := oauthFixture(t)
			f := tokenForm(mintCode(t, mux))
			f.Set(tc.key, tc.value)
			w := redeem(mux, f)
			if w.Code != 400 || strings.Contains(w.Body.String(), "local-token") {
				t.Fatalf("%s not bound %d %s", tc.key, w.Code, w.Body)
			}
		})
	}
}
func TestOAuthAuthorizeValidation(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"redirect_uri", "https://attacker.example/callback"}, {"code_challenge_method", "plain"}, {"code_challenge", ""}, {"scope", "list_online"}, {"scope", extshared.ScopeString() + " execute"}, {"resource", "https://evil.example/mcp"}, {"response_type", "token"}, {"client_id", "https://attacker.example/client"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			_, _, mux := fixture(t)
			q := authorizationQuery()
			q.Set(tc.key, tc.value)
			w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
			if w.Code != 400 || w.Header().Get("Location") != "" {
				t.Fatalf("untrusted request redirected %d %s", w.Code, w.Header().Get("Location"))
			}
		})
	}
}
func TestOAuthCallbackReplayAndBrowserBinding(t *testing.T) {
	_, s, mux := oauthFixture(t)
	a, c := browserStart(t, mux)
	r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+a, nil)
	if w := serve(mux, r); w.Code != 400 {
		t.Fatal("unbound browser accepted")
	}
	cb, w := finishFeishu(t, mux, a, c)
	if w.Code != 302 {
		t.Fatal("callback failed")
	}
	r = httptest.NewRequest("GET", cb, nil)
	addCookies(r, c)
	if w = serve(mux, r); w.Code != 400 || len(s.issued) != 1 {
		t.Fatal("C callback replay issued token")
	}
}
func TestOAuthExpiryAndRevocationBeforeExchange(t *testing.T) {
	for _, kind := range []string{"code_expired", "token_revoked"} {
		t.Run(kind, func(t *testing.T) {
			g, s, mux := oauthFixture(t)
			code := mintCode(t, mux)
			if kind == "code_expired" {
				g.cfg.Now = func() time.Time { return s.now.Add(oauthTTL) }
			} else {
				s.authErr = extshared.ErrTokenRevoked
			}
			w := redeem(mux, tokenForm(code))
			if w.Code != 400 || strings.Contains(w.Body.String(), "local-token") {
				t.Fatalf("expired/revoked grant accepted %d", w.Code)
			}
		})
	}
}
func TestOAuthMetadataAndOrigin(t *testing.T) {
	_, _, mux := fixture(t)
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"} {
		w := serve(mux, httptest.NewRequest("GET", testOrigin+path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), testOrigin) {
			t.Fatal("metadata missing")
		}
		if strings.Contains(w.Body.String(), "registration_endpoint") || strings.Contains(w.Body.String(), "refresh_token") {
			t.Fatal("unsupported grant/registration advertised")
		}
	}
	for _, path := range []string{"/mcp", "/mcp/clients/codex.json", "/api/external/list_online", "/oauth/authorize", "/oauth/token", "/feishu/connect", "/mcp/oauth/feishu/callback", "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/mcp"} {
		r := httptest.NewRequest("POST", testOrigin+path, strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://evil.example")
		// /mcp challenges before protocol/Origin validation; a valid bearer
		// must still be rejected by the unchanged Origin guard.
		r.Header.Set("Authorization", "Bearer local-token")
		if w := serve(mux, r); w.Code != 403 {
			t.Fatalf("Origin guard absent on %s: %d", path, w.Code)
		}
	}
}
func TestOAuthConcurrentExchangeOnlyOneWinner(t *testing.T) {
	_, _, mux := oauthFixture(t)
	code := mintCode(t, mux)
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := redeem(mux, tokenForm(code))
			if w.Code == 200 {
				wins.Add(1)
			} else if w.Code != 400 {
				t.Errorf("unexpected exchange status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("code redeemed %d times", wins.Load())
	}
}

// 飞书重定向 URL 以业主裁定为准（rulings ㊹-补10：/mcp/oauth/feishu/callback，443 与 :50443 两条），
// 业主在飞书后台登记的正是这条路径；旧路径不得再登记，免得两条回调并存。
func TestFeishuCallbackPathMatchesOwnerRuling(t *testing.T) {
	_, _, mux := oauthFixture(t)
	if w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/feishu/callback?state=x&code=y", nil)); w.Code != 404 {
		t.Fatalf("legacy callback path still served: %d", w.Code)
	}
	if w := serve(mux, httptest.NewRequest("GET", testOrigin+"/mcp/oauth/feishu/callback?state=x&code=y", nil)); w.Code == 404 {
		t.Fatal("ruled callback path /mcp/oauth/feishu/callback not registered")
	}
}
