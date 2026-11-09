package mcpgw

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
