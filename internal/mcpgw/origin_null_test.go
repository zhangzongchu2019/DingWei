package mcpgw

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/extwire"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

// Browsers serialize a form POST Origin as "null" when the page that holds the
// form was served with Referrer-Policy: no-referrer (r56.1 现网实测
// origin_rejected，2026-11-09)。页面必须用 same-origin，来源守卫本身不放宽。
func TestConnectPageReferrerPolicyLetsBrowserSendOrigin(t *testing.T) {
	_, _, mux := oauthFixture(t)
	attempt, cookies := browserStart(t, mux)
	r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+url.QueryEscape(attempt), nil)
	addCookies(r, cookies)
	w := serve(mux, r)
	if w.Code != 200 {
		t.Fatalf("connect %d %s", w.Code, w.Body)
	}
	if got := w.Header().Values("Referrer-Policy"); len(got) != 1 || got[0] != "same-origin" {
		t.Fatalf("ORIGIN_NULL_CONNECT_PAGE_POLICY %q", got)
	}
}

func TestGatewayGuardReferrerPolicyIsSameOrigin(t *testing.T) {
	_, _, mux := oauthFixture(t)
	w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+authorizationQuery().Encode(), nil))
	if w.Code != 302 {
		t.Fatalf("authorize %d %s", w.Code, w.Body)
	}
	if got := w.Header().Values("Referrer-Policy"); len(got) != 1 || got[0] != "same-origin" {
		t.Fatalf("ORIGIN_NULL_GUARD_POLICY %q", got)
	}
}

func TestGatewayGuardStillRejectsNullOrigin(t *testing.T) {
	_, _, mux := oauthFixture(t)
	attempt, cookies := browserStart(t, mux)
	form := url.Values{"attempt_id": {attempt}, "csrf_token": {"x"}, "short_name": {"bot"}, "target_agent": {"target.zzc"}}
	r := httptest.NewRequest("POST", testOrigin+"/feishu/connect", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "null")
	addCookies(r, cookies)
	w := serve(mux, r)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "origin_rejected") {
		t.Fatalf("ORIGIN_NULL_GUARD_RELAXED %d %s", w.Code, w.Body)
	}
}

// The shared oauthFixture points Feishu at an http test server, which by design
// keeps form-action 'self'. Here the default production authorize endpoint is
// used so the gateway must pass the Feishu origin through unchanged.
func TestConnectPageCSPThroughGatewayAllowsFeishuHop(t *testing.T) {
	g, seats, _ := oauthFixture(t)
	oauth, e := feishulogin.NewFeishuClient(feishulogin.OAuthConfig{AppID: "app", AppSecret: "test-only-secret", RedirectURI: testOrigin + "/mcp/oauth/feishu/callback"})
	if e != nil {
		t.Fatal(e)
	}
	login, e := feishulogin.NewHandler(feishulogin.Config{AppID: "app", TeamTenantKey: "team", RedirectURI: testOrigin + "/mcp/oauth/feishu/callback", OAuth: oauth, States: LoginStates{Store: g.cfg.Store}, Tokens: extwire.Issuer{Seats: seats, Resource: testOrigin + "/mcp"}, Now: g.cfg.Now})
	if e != nil {
		t.Fatal(e)
	}
	g.cfg.Login = login
	mux := http.NewServeMux()
	g.Register(mux)
	attempt, cookies := browserStart(t, mux)
	r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+url.QueryEscape(attempt), nil)
	addCookies(r, cookies)
	w := serve(mux, r)
	if csp := w.Header().Get("Content-Security-Policy"); w.Code != 200 || !strings.Contains(csp, "form-action 'self' https://accounts.feishu.cn;") {
		t.Fatalf("CSP_GATEWAY_FORM_ACTION %d %q", w.Code, csp)
	}
}
