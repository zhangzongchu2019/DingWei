package feishulogin

import (
	"context"
	"strings"
	"testing"
)

type fixedAuthorize struct {
	fakeOAuth
	raw string
}

func (f *fixedAuthorize) AuthorizationURL(string, string) (string, error) { return f.raw, nil }

var _ = context.Background

// Browsers enforce form-action across the connect POST's 303 to Feishu; with
// 'self' alone Chromium refuses the hop (r56.1 现网实测，2026-11-09)。
func TestConnectPageCSPAllowsExactFeishuAuthorizeOrigin(t *testing.T) {
	fixture := newConnectFixture(t)
	recorder, _ := fixture.getForm(t, "outer-attempt-csp")
	csp := recorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "form-action 'self' https://accounts.feishu.cn;") {
		t.Fatalf("CSP_FORM_ACTION_MISSING_FEISHU %q", csp)
	}
	for _, keep := range []string{"default-src 'none'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, keep) {
			t.Fatalf("CSP_DIRECTIVE_DROPPED %q in %q", keep, csp)
		}
	}
}

func TestConnectPageCSPRejectsUnusualAuthorizeOrigins(t *testing.T) {
	for _, raw := range []string{
		"http://accounts.feishu.cn/open-apis/authen/v1/authorize",
		"https://evil.example; script-src *",
		"https://user@accounts.feishu.cn/x",
		"::not a url",
	} {
		if got := authorizeOriginOf(&fixedAuthorize{raw: raw}, testRedirectURI); got != "" {
			t.Fatalf("CSP_ORIGIN_ACCEPTED %q -> %q", raw, got)
		}
	}
	if got := authorizeOriginOf(&fixedAuthorize{raw: DefaultAuthorizeURL + "?state=x"}, testRedirectURI); got != "https://accounts.feishu.cn" {
		t.Fatalf("CSP_ORIGIN_DEFAULT %q", got)
	}
	if connectPageCSPFor("") != connectPageCSP {
		t.Fatal("CSP_EMPTY_ORIGIN_NOT_SELF_ONLY")
	}
}
