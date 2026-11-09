package feishulogin

import (
	"strings"
	"testing"
)

func TestFormRedirectPolicyValidatedOrigins(t *testing.T) {
	policy := FormRedirectPolicy(connectPageCSP, "https://accounts.feishu.cn/authorize?state=not-in-policy", "http://127.0.0.1:18999/callback?x=1")
	for _, part := range []string{"form-action 'self' https://accounts.feishu.cn http://127.0.0.1:18999;", "default-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, part) {
			t.Fatalf("ZC_FORM_REDIRECT_ALLOWLIST missing %s: %s", part, policy)
		}
	}
	if strings.Contains(policy, "state=") || strings.Contains(policy, "?x=") {
		t.Fatal("query leaked to CSP")
	}
	for _, raw := range []string{"https://evil.example;script-src/", "https://*.example/", "https://user:pass@example.com", "data:text/html,x", "//example.com", "https://bad.example'", "https://bad.example\n"} {
		if got := FormRedirectPolicy(connectPageCSP, raw); got != connectPageCSP {
			t.Fatalf("ZC_FORM_POLICY_UNSAFE_SOURCE %q: %s", raw, got)
		}
	}
}
