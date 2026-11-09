package mcpgw

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

type zeroResolver struct{ ids []string }

func (c *zeroResolver) Resolve(_ context.Context, id string) (Client, error) {
	c.ids = append(c.ids, id)
	return Client{}, errParams
}

func zeroQuery() url.Values {
	q := authorizationQuery()
	q.Set("client_id", codexDefaultClientID)
	q.Set("redirect_uri", "http://127.0.0.1:49152/callback")
	return q
}

func TestZeroConfigPinnedDocument(t *testing.T) {
	if fmt.Sprintf("%x", sha256.Sum256(codexOriginalMetadata)) != "88c1677cc7dc907c07184571bba9ecaa342904424823d883270f9c8f62940004" {
		t.Fatal("ZERO_PROVENANCE: original upstream bytes changed")
	}
	c, err := defaultCodexClient()
	if err != nil || c.ID != codexDefaultClientID || c.AuthMethod != "none" || !reflect.DeepEqual(c.Redirects, []string{"http://127.0.0.1/callback"}) || !reflect.DeepEqual(c.Grants, []string{"authorization_code"}) || !reflect.DeepEqual(c.Responses, []string{"code"}) {
		t.Fatalf("ZERO_EFFECTIVE_POLICY: %+v %v", c, err)
	}
}

func TestZeroConfigInvalidCopyFailsClosed(t *testing.T) {
	original := append([]byte(nil), codexOriginalMetadata...)
	t.Cleanup(func() { codexOriginalMetadata = original })
	for _, name := range []string{"json", "id", "auth", "redirect", "external_https", "https_loopback", "grant", "response"} {
		t.Run(name, func(t *testing.T) {
			var c Client
			if json.Unmarshal(original, &c) != nil {
				t.Fatal("fixture")
			}
			switch name {
			case "id":
				c.ID = "https://other.example/client.json"
			case "auth":
				c.AuthMethod = ""
			case "redirect":
				c.Redirects = nil
			case "external_https":
				c.Redirects = []string{"https://other.example/callback"}
			case "https_loopback":
				c.Redirects = []string{"https://127.0.0.1/callback"}
			case "grant":
				c.Grants = []string{"refresh_token"}
			case "response":
				c.Responses = []string{"token"}
			}
			codexOriginalMetadata, _ = json.Marshal(c)
			if name == "json" {
				codexOriginalMetadata = []byte("{")
			}
			g, _, mux := fixture(t)
			resolver := &zeroResolver{}
			g.cfg.Clients = resolver
			q := zeroQuery()
			w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
			if w.Code != 400 || len(resolver.ids) != 0 {
				t.Fatalf("ZERO_COPY_%s: status=%d external=%v", name, w.Code, resolver.ids)
			}
			if _, err := defaultCodexClient(); err == nil {
				t.Fatalf("ZERO_COPY_%s: invalid local document accepted", name)
			}
		})
	}
}

func TestZeroConfigExactLocalResolution(t *testing.T) {
	g, _, mux := fixture(t)
	resolver := &zeroResolver{}
	g.cfg.Clients = resolver
	for _, tc := range []struct {
		id              string
		status, fetches int
	}{
		{codexDefaultClientID, 302, 0},
		{testOrigin + codexClientPath, 302, 0},
		{codexDefaultClientID + "?evil=1", 400, 1},
		{codexDefaultClientID + "#fragment", 400, 2},
		{"https://chatgpt.com.evil.example/oauth/codex/client.json", 400, 3},
		{"http://127.0.0.1/private.json", 400, 4},
	} {
		q := zeroQuery()
		q.Set("client_id", tc.id)
		w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
		if w.Code != tc.status || len(resolver.ids) != tc.fetches {
			t.Fatalf("ZERO_EXACT_ID: %s status=%d external=%v", tc.id, w.Code, resolver.ids)
		}
	}
}

func TestZeroConfigRedirectGuards(t *testing.T) {
	_, _, mux := fixture(t)
	for _, tc := range []struct {
		name, uri string
		status    int
	}{
		{"port", "http://127.0.0.1:1/callback", 302},
		{"port_max", "http://127.0.0.1:65535/callback", 302},
		{"port_invalid", "http://127.0.0.1:65536/callback", 400},
		{"localhost", "http://localhost:49152/callback", 400},
		{"https_localhost", "https://LOCALHOST:49152/callback", 400},
		{"host", "http://192.0.2.1:49152/callback", 400},
		{"ipv6_not_registered", "http://[::1]:49152/callback", 400},
		{"scheme", "https://127.0.0.1:49152/callback", 400},
		{"path", "http://127.0.0.1:49152/callback/", 400},
		{"escaped_path", "http://127.0.0.1:49152/%63allback", 400},
		{"query", "http://127.0.0.1:49152/callback?extra=1", 400},
		{"fragment", "http://127.0.0.1:49152/callback#fragment", 400},
		{"userinfo", "http://user@127.0.0.1:49152/callback", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := zeroQuery()
			q.Set("redirect_uri", tc.uri)
			w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
			if w.Code != tc.status {
				t.Fatalf("ZERO_REDIRECT_%s: %d %s", tc.name, w.Code, w.Body)
			}
		})
	}
}

func TestZeroConfigAuthorizationGuards(t *testing.T) {
	_, _, mux := fixture(t)
	for _, tc := range []struct{ key, value string }{
		{"code_challenge_method", "plain"}, {"code_challenge", ""},
		{"resource", "https://foreign.example/mcp"}, {"scope", "list_online"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			q := zeroQuery()
			q.Set(tc.key, tc.value)
			w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
			if w.Code != 400 || w.Header().Get("Location") != "" {
				t.Fatalf("ZERO_AUTH_%s: %d %s", tc.key, w.Code, w.Body)
			}
		})
	}
}

func TestZeroConfigTokenBindings(t *testing.T) {
	for _, key := range []string{"", "client_id", "redirect_uri", "resource", "code_verifier"} {
		t.Run(key, func(t *testing.T) {
			g, _, mux := oauthFixture(t)
			g.cfg.Clients = &zeroResolver{}
			q := zeroQuery()
			q.Set("redirect_uri", "http://127.0.0.1:49152/callback/Ab_cd-1234567")
			w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
			if w.Code != 302 {
				t.Fatal("ZERO_LOGIN", w.Code, w.Body)
			}
			u, _ := url.Parse(w.Header().Get("Location"))
			_, callback := finishFeishu(t, mux, u.Query().Get("attempt_id"), w.Result().Cookies())
			if callback.Code != 302 {
				t.Fatal("ZERO_CALLBACK", callback.Code, callback.Body)
			}
			u, _ = url.Parse(callback.Header().Get("Location"))
			form := tokenForm(u.Query().Get("code"))
			form.Set("client_id", q.Get("client_id"))
			form.Set("redirect_uri", q.Get("redirect_uri"))
			want := 200
			if key != "" {
				form.Set(key, strings.Repeat("z", 43))
				if key == "redirect_uri" {
					form.Set(key, "http://127.0.0.1:49152/callback/anotherValidSegment")
				}
				want = 400
			}
			w = redeem(mux, form)
			if w.Code != want {
				t.Fatalf("ZERO_TOKEN_%s: %d %s", key, w.Code, w.Body)
			}
		})
	}
}

func TestCodexNativeCallbackPathsBothClients(t *testing.T) {
	g, _, mux := fixture(t)
	g.cfg.Clients = &zeroResolver{}
	for i, id := range []string{codexDefaultClientID, testOrigin + codexClientPath} {
		label := []string{"default", "explicit"}[i]
		for _, tc := range []struct {
			name, path string
			status     int
		}{
			{"base", "/callback", 302},
			{"desktop12", "/callback/Ab_cd-1234567", 302},
			{"one", "/callback/a", 302},
			{"max64", "/callback/" + strings.Repeat("a", 64), 302},
			{"empty", "/callback/", 400},
			{"over64", "/callback/" + strings.Repeat("a", 65), 400},
			{"slash", "/callback/a/b", 400},
			{"dot", "/callback/a.b", 400},
			{"dotdot", "/callback/..", 400},
			{"unicode", "/callback/名字", 400},
			{"encoded", "/callback/%61", 400},
			{"prefix", "/callback-evil/a", 400},
			{"suffix", "/callback/a/", 400},
		} {
			t.Run(label+"/"+tc.name, func(t *testing.T) {
				q := zeroQuery()
				q.Set("client_id", id)
				q.Set("redirect_uri", "http://127.0.0.1:49152"+tc.path)
				w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
				if w.Code != tc.status {
					t.Fatalf("NATIVE_CALLBACK_%s: %d %s", tc.name, w.Code, w.Body)
				}
			})
		}
	}
}

func TestCodexCallbackExtensionDoesNotApplyToOtherClients(t *testing.T) {
	_, _, mux := fixture(t)
	q := authorizationQuery()
	q.Set("redirect_uri", "http://127.0.0.1:43123/callback/Ab_cd-1234567")
	w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
	if w.Code != 400 {
		t.Fatalf("NATIVE_CALLBACK_EXACT_CLIENTS_ONLY: %d", w.Code)
	}
}

func TestCodexNativeRedirectGuardsBothClients(t *testing.T) {
	_, _, mux := fixture(t)
	for i, id := range []string{codexDefaultClientID, testOrigin + codexClientPath} {
		label := []string{"default", "explicit"}[i]
		for _, tc := range []struct {
			name, uri string
			status    int
		}{
			{"ok", "http://127.0.0.1:49152/callback/Ab_cd-1234567", 302},
			{"localhost", "http://localhost:49152/callback/Ab_cd-1234567", 400},
			{"public_ip", "http://192.0.2.1:49152/callback/Ab_cd-1234567", 400},
			{"https", "https://127.0.0.1:49152/callback/Ab_cd-1234567", 400},
			{"uppercase", "HTTP://127.0.0.1:49152/callback/Ab_cd-1234567", 400},
			{"query", "http://127.0.0.1:49152/callback/Ab_cd-1234567?extra=1", 400},
			{"fragment", "http://127.0.0.1:49152/callback/Ab_cd-1234567#f", 400},
			{"userinfo", "http://user@127.0.0.1:49152/callback/Ab_cd-1234567", 400},
			{"relative", "/callback/Ab_cd-1234567", 400},
			{"scheme_less", "//127.0.0.1/callback/Ab_cd-1234567", 400},
			{"no_host", "http:/callback/Ab_cd-1234567", 400},
		} {
			t.Run(label+"/"+tc.name, func(t *testing.T) {
				q := zeroQuery()
				q.Set("client_id", id)
				q.Set("redirect_uri", tc.uri)
				w := serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
				if w.Code != tc.status {
					t.Fatalf("NATIVE_REDIRECT_%s: %d", tc.name, w.Code)
				}
			})
		}
	}
}

func TestCodexCallbackTokenExactForBothClients(t *testing.T) {
	for i, id := range []string{codexDefaultClientID, testOrigin + codexClientPath} {
		for _, changed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", i, changed), func(t *testing.T) {
				g, _, mux := fixture(t)
				q := zeroQuery()
				actual := "http://127.0.0.1:49152/callback/Ab_cd-1234567"
				grant := codeGrant{Authorization: authorization{ClientID: id, Redirect: actual, Challenge: q.Get("code_challenge")}, Issued: feishulogin.CallbackResult{Token: "local-token"}}
				raw, _ := json.Marshal(grant)
				if err := g.cfg.Store.Put(context.Background(), "code", hash("bound-code"), raw, g.cfg.Now().Add(oauthTTL), g.cfg.Now()); err != nil {
					t.Fatal(err)
				}
				form := tokenForm("bound-code")
				form.Set("client_id", id)
				form.Set("redirect_uri", actual)
				want := 200
				if changed {
					form.Set("redirect_uri", "http://127.0.0.1:49152/callback/anotherSegment")
					want = 400
				}
				w := redeem(mux, form)
				if w.Code != want {
					t.Fatalf("NATIVE_TOKEN_EXACT_REDIRECT: %d %s", w.Code, w.Body)
				}
			})
		}
	}
}

func TestZeroConfigEveryRequestReauthenticates(t *testing.T) {
	_, seats, mux := fixture(t)
	if w := serve(mux, requestCompat("initialize", "")); w.Code != 200 {
		t.Fatal(w.Code)
	}
	seats.authErr = extshared.ErrTenantRejected
	w := serve(mux, requestCompat("tools/list", ""))
	if w.Code != 403 || seats.authCalls != 2 {
		t.Fatalf("ZERO_REQUEST_AUTH: %d calls=%d", w.Code, seats.authCalls)
	}
}
