package mcpgw

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

const publicRegistration = `{"client_name":"Local editor","redirect_uris":["http://127.0.0.1:43123/callback"],"token_endpoint_auth_method":"none"}`

func TestDCRPublicMetadataAndRedirectPolicy(t *testing.T) {
	p := DCRPolicy{HTTPSRedirects: []string{"https://vendor.example/oauth/callback?fixed=1"}}
	if e := p.validate(); e != nil {
		t.Fatal(e)
	}
	c, e := p.metadata([]byte(publicRegistration))
	if e != nil || c.AuthMethod != "none" {
		t.Fatalf("public metadata rejected: %v", e)
	}
	for _, raw := range []string{"http://127.0.0.1/callback", "http://127.0.0.1:65535/callback", "http://[::1]:37/callback", "http://localhost:1/callback", "https://vendor.example/oauth/callback?fixed=1"} {
		if e := p.redirect(raw); e != nil {
			t.Errorf("allowed redirect rejected %s: %v", raw, e)
		}
	}
	for raw, kind := range map[string]string{
		"http://evil.example/callback": "loopback_bad", "http://127.0.0.2/callback": "loopback_bad", "http://127.1/callback": "loopback_bad", "http://localhost.evil/callback": "loopback_bad",
		"http://127.0.0.1:65536/callback": "malformed", "http://127.0.0.1:0/callback": "malformed", "http://user@127.0.0.1/callback": "malformed", "http://127.0.0.1/callback#secret": "malformed",
		"https://vendor.example/other": "https_unlisted", "https://vendor.example/oauth/callback?fixed=2": "https_unlisted", "https://127.0.0.1/cb": "https_unlisted", "https://other.example/callback": "https_unlisted",
		"codebuddy://oauth/callback?private=value": "custom_scheme", "codebuddy:/callback": "custom_scheme",
	} {
		var bad *dcrRedirectError
		if e := p.redirect(raw); !errors.As(e, &bad) || bad.Kind != kind {
			t.Errorf("unsafe redirect accepted/wrong category %q: %v", raw, e)
		} else if bad.Kind == "custom_scheme" && bad.Scheme != "codebuddy" {
			t.Errorf("scheme diagnostics %q", bad.Scheme)
		}
	}
	for _, fragment := range []string{
		`"client_secret":""`, `"client_id":"chosen"`, `"token_endpoint_auth_method":"client_secret_basic"`, `"grant_types":["refresh_token"]`, `"response_types":["token"]`, `"response_types":["code","code"]`, `"scope":"admin"`, `"jwks_uri":"https://evil.example/key"`,
	} {
		base := strings.TrimSuffix(publicRegistration, "}") + "," + fragment + "}"
		if _, e = p.metadata([]byte(base)); e == nil {
			t.Errorf("forbidden metadata accepted %s", fragment)
		}
	}
	basic := strings.Replace(publicRegistration, `"token_endpoint_auth_method":"none"`, `"token_endpoint_auth_method":"client_secret_basic"`, 1)
	if _, e = p.metadata([]byte(basic)); e == nil {
		t.Fatal("secret authentication method accepted")
	}
	missing := strings.ReplaceAll(publicRegistration, `,"token_endpoint_auth_method":"none"`, "")
	if _, e = p.metadata([]byte(missing)); e == nil {
		t.Fatal("missing method silently downgraded to none")
	}
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"x":1,"x":2}`, `{"extension":{"x":1,"x":2}}`} {
		if _, e := p.metadata([]byte(raw)); e == nil {
			t.Errorf("bad JSON accepted %s", raw)
		}
	}
	for _, ttl := range []time.Duration{7*24*time.Hour - time.Nanosecond, 90*24*time.Hour + time.Nanosecond, -time.Second} {
		if _, e := NewDCR(DCRPolicy{BoundTTL: ttl}, nil); e == nil {
			t.Errorf("unsafe bound TTL accepted %v", ttl)
		}
	}
	for _, ttl := range []time.Duration{7 * 24 * time.Hour, 90 * 24 * time.Hour} {
		if _, e := NewDCR(DCRPolicy{BoundTTL: ttl}, nil); e != nil {
			t.Fatal(e)
		}
	}
	if got := filteredScheme(strings.Repeat("a", 40) + "?private/中文"); len(got) != 32 {
		t.Fatalf("scheme log not bounded: %q", got)
	}
}

func TestDCRTrustedProxySource(t *testing.T) {
	p := DCRPolicy{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	for _, tc := range []struct {
		peer, xff, want string
		bad             bool
	}{
		{"192.0.2.9:51", "1.2.3.4", "192.0.2.9", false},
		{"10.0.0.1:51", "1.2.3.4, 192.0.2.8, 10.0.0.2", "192.0.2.8", false},
		{"10.0.0.1:51", "", "10.0.0.1", false},
		{"[2001:db8:abcd:7::9]:51", "1.2.3.4", "2001:db8:abcd:7::/64", false},
		{"10.0.0.1:51", "192.0.2.8, bad", "", true},
	} {
		r := httptest.NewRequest("POST", "https://hub.example/oauth/register", nil)
		r.RemoteAddr = tc.peer
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		got, e := p.source(r)
		if tc.bad {
			if e == nil {
				t.Fatal("bad proxy chain accepted")
			}
		} else if e != nil || got != tc.want {
			t.Fatalf("source %q want %q: %v", got, tc.want, e)
		}
	}
	r := httptest.NewRequest("POST", "https://hub.example", nil)
	r.RemoteAddr = "10.0.0.1:51"
	r.Header["X-Forwarded-For"] = []string{"192.0.2.1", "192.0.2.2"}
	if _, e := p.source(r); e == nil {
		t.Fatal("multiple proxy headers accepted")
	}
}

func TestDCRLoopbackPortDoesNotChangeOtherBindings(t *testing.T) {
	c := Client{Redirects: []string{"http://127.0.0.1:1/callback?fixed=1"}}
	if !registered(c, "http://127.0.0.1:54321/callback?fixed=1") {
		t.Fatal("dynamic loopback port rejected")
	}
	for _, uri := range []string{"http://localhost:54321/callback?fixed=1", "http://127.0.0.1:54321/other?fixed=1", "http://127.0.0.1:54321/callback?fixed=2"} {
		if registered(c, uri) {
			t.Fatal("loopback changed host/path/query")
		}
	}
	_, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
}
