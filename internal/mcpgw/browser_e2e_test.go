package mcpgw

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/extwire"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

// TestBrowserAuthorizeChainReachesLoopback drives real Chromium through
// authorize → connect form → Feishu stand-in that skips consent (immediate 302,
// as for an already-authorized user) → callback → client loopback. Opt-in:
// DW_BROWSER_E2E=1 and NODE_PATH with playwright (r56.2 现网缺陷回归)。
func TestBrowserAuthorizeChainReachesLoopback(t *testing.T) {
	if os.Getenv("DW_BROWSER_E2E") != "1" || os.Getenv("NODE_PATH") == "" {
		t.Skip("browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>")
	}
	hits := make(chan string, 4)
	loop, err := net.Listen("tcp", "127.0.0.1:43123")
	if err != nil {
		t.Skipf("loopback port 43123 busy: %v", err)
	}
	loopSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.String()
		_, _ = w.Write([]byte("loopback ok"))
	})}
	go func() { _ = loopSrv.Serve(loop) }()
	t.Cleanup(func() { _ = loopSrv.Close() })

	hub := httptest.NewUnstartedServer(nil)
	hub.StartTLS()
	t.Cleanup(hub.Close)
	hubURL := hub.URL
	callback := hubURL + "/mcp/oauth/feishu/callback"

	feishu := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize": // consent already granted: bounce straight back
			q := r.URL.Query()
			http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {"feishu-code"}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
		case "/token":
			_ = r.ParseForm()
			writeJSON(w, 200, map[string]any{"access_token": "feishu-private-token", "token_type": "Bearer", "code": 0})
		case "/user":
			writeJSON(w, 200, map[string]any{"code": 0, "data": feishulogin.UserInfo{Name: "名字", EnName: "Alice", OpenID: "open-trusted", TenantKey: "team", UnionID: "union"}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(feishu.Close)

	_, store := testDB(t)
	seats := &seatsFake{now: time.Now()}
	g, err := New(Config{PublicOrigin: hubURL, Resource: hubURL + "/mcp", Seats: seats, Login: loginFake{},
		Clients: clientsFake{client: Client{ID: clientID, Name: "test client", Redirects: []string{clientRedirect}, AuthMethod: "none"}},
		Store: store, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	oauth, err := feishulogin.NewFeishuClient(feishulogin.OAuthConfig{AppID: "app", AppSecret: "test-only-secret", RedirectURI: callback,
		AuthorizeURL: feishu.URL + "/authorize", TokenURL: feishu.URL + "/token", UserInfoURL: feishu.URL + "/user", HTTPClient: feishu.Client()})
	if err != nil {
		t.Fatal(err)
	}
	login, err := feishulogin.NewHandler(feishulogin.Config{AppID: "app", TeamTenantKey: "team", RedirectURI: callback, OAuth: oauth,
		States: LoginStates{Store: store}, Tokens: extwire.Issuer{Seats: seats, Resource: hubURL + "/mcp"}, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	g.cfg.Login = login
	mux := http.NewServeMux()
	g.Register(mux)
	hub.Config.Handler = mux

	sum := sha256.Sum256([]byte(verifier))
	start := hubURL + "/oauth/authorize?" + url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {clientRedirect},
		"resource": {hubURL + "/mcp"}, "scope": {extshared.ScopeString()}, "state": {"outer-state"},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}}.Encode()
	script := filepath.Join(t.TempDir(), "drive.js")
	if err := os.WriteFile(script, []byte(`const { chromium } = require('playwright');
(async () => {
  const b = await chromium.launch(); const ctx = await b.newContext({ ignoreHTTPSErrors: true });
  const p = await ctx.newPage(); const msgs = []; p.on('console', m => msgs.push(m.text()));
  await p.goto(process.argv[2]);
  await p.fill('input[name=short_name]', 'bot'); await p.fill('input[name=target_agent]', 'target.zzc');
  await p.click('button[type=submit], input[type=submit], button');
  await p.waitForTimeout(3000);
  console.log('FINAL_URL ' + p.url()); for (const m of msgs) console.log('CONSOLE ' + m.replace(/\n/g, ' '));
  await b.close();
})().catch(e => { console.log('DRIVER_ERROR ' + e); process.exit(2); });
`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "node", script, start).CombinedOutput()
	t.Logf("driver output:\n%s", out)
	if err != nil {
		t.Fatalf("BROWSER_DRIVER_FAILED %v", err)
	}
	if strings.Contains(string(out), "origin_rejected") || strings.Contains(string(out), "violates the following Content Security Policy") {
		t.Fatalf("BROWSER_CHAIN_BLOCKED")
	}
	select {
	case hit := <-hits:
		if !strings.Contains(hit, "code=") || !strings.Contains(hit, "state=outer-state") {
			t.Fatalf("BROWSER_LOOPBACK_BAD_QUERY %q", hit)
		}
		t.Logf("loopback hit %s", hit)
	default:
		t.Fatalf("BROWSER_LOOPBACK_NOT_REACHED")
	}
}
