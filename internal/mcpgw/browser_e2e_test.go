package mcpgw

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	runBrowserAuthorizeChain(t, "target.zzc", false)
}

func TestBrowserCallbackExplainsMissingTarget(t *testing.T) {
	runBrowserAuthorizeChain(t, "missing.zzc", true)
}

func runBrowserAuthorizeChain(t *testing.T, target string, expectMissingTarget bool) {
	t.Helper()
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
	if expectMissingTarget {
		seats.issueErr = errors.Join(extshared.ErrTargetNotInAG, extshared.ErrTargetMissing)
	}
	g, err := New(Config{PublicOrigin: hubURL, Resource: hubURL + "/mcp", Seats: seats, Login: loginFake{},
		Clients: clientsFake{client: Client{ID: clientID, Name: "test client", Redirects: []string{clientRedirect}, AuthMethod: "none", Grants: []string{"authorization_code"}, Responses: []string{"code"}}},
		Store:   store, Now: time.Now})
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
  const p = await ctx.newPage(); const msgs = []; let cspErrors = 0;
  p.on('console', m => { msgs.push(m.text()); if (/Content Security Policy/i.test(m.text())) cspErrors++; });
  await p.goto(process.argv[2]);
  await p.fill('input[name=short_name]', 'bot'); await p.fill('input[name=target_agent]', process.argv[3]);
  await p.click('button[type=submit], input[type=submit], button');
  await p.waitForTimeout(3000);
  console.log('FINAL_URL ' + p.url()); console.log('BODY ' + (await p.locator('body').innerText()).replace(/\n/g, ' '));
  console.log('CSP_ERRORS ' + cspErrors);
  for (const m of msgs) console.log('CONSOLE ' + m.replace(/\n/g, ' '));
  await b.close();
})().catch(e => { console.log('DRIVER_ERROR ' + e); process.exit(2); });
`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	mode := "success"
	if expectMissingTarget {
		mode = "target-missing"
	}
	out, err := exec.CommandContext(ctx, "node", script, start, target, mode).CombinedOutput()
	t.Logf("driver output:\n%s", out)
	if err != nil {
		t.Fatalf("BROWSER_DRIVER_FAILED %v", err)
	}
	if strings.Contains(string(out), "origin_rejected") || strings.Contains(string(out), "violates the following Content Security Policy") {
		t.Fatalf("BROWSER_CHAIN_BLOCKED")
	}
	if !strings.Contains(string(out), "CSP_ERRORS 0") {
		t.Fatalf("BROWSER_CSP_ERRORS")
	}
	if expectMissingTarget {
		if !strings.Contains(string(out), "目标 Agent 不存在或不可用") || !strings.Contains(string(out), "access_denied") {
			t.Fatalf("BROWSER_TARGET_DIAGNOSTIC_MISSING")
		}
		return
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

// TestBrowserClaudeCodeLocalhostConsentChains exercises Claude Code's exact
// portless CIMD registration through both Feishu's immediate redirect and a
// consent form, using real Chromium and an ephemeral localhost listener.
func TestBrowserClaudeCodeLocalhostConsentChains(t *testing.T) {
	for _, consent := range []bool{false, true} {
		name := "immediate-302"
		if consent {
			name = "consent-page"
		}
		t.Run(name, func(t *testing.T) { runBrowserNativeLoopbackChain(t, consent, true) })
	}
}

func TestBrowserCodexIPLoopbackConsentChains(t *testing.T) {
	for _, consent := range []bool{false, true} {
		name := "immediate-302"
		if consent {
			name = "consent-page"
		}
		t.Run(name, func(t *testing.T) { runBrowserNativeLoopbackChain(t, consent, false) })
	}
}

func runBrowserNativeLoopbackChain(t *testing.T, consent, claude bool) {
	t.Helper()
	if os.Getenv("DW_BROWSER_E2E") != "1" || os.Getenv("NODE_PATH") == "" {
		t.Skip("browser E2E is opt-in: DW_BROWSER_E2E=1 NODE_PATH=<playwright>")
	}
	hits := make(chan string, 1)
	loopHost := "localhost"
	if !claude {
		loopHost = "127.0.0.1"
	}
	loop, err := net.Listen("tcp", net.JoinHostPort(loopHost, "0"))
	if err != nil {
		t.Fatalf("CLAUDE_BROWSER_LOCALHOST_LISTEN: %v", err)
	}
	loopURL := "http://" + net.JoinHostPort(loopHost, strconv.Itoa(loop.Addr().(*net.TCPAddr).Port)) + "/callback"
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
	clientID := claudeCodeClientID
	flowState := "claude-state"
	if !claude {
		clientID = hubURL + codexClientPath
		flowState = "codex-state"
	}
	feishu := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			q := r.URL.Query()
			if consent {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<form method="post" action="/consent"><input type="hidden" name="redirect_uri" value="` + html.EscapeString(q.Get("redirect_uri")) + `"><input type="hidden" name="state" value="` + html.EscapeString(q.Get("state")) + `"><button type="submit">Approve</button></form>`))
				return
			}
			http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {"feishu-code"}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
		case "/consent":
			_ = r.ParseForm()
			http.Redirect(w, r, r.Form.Get("redirect_uri")+"?"+url.Values{"code": {"feishu-code"}, "state": {r.Form.Get("state")}}.Encode(), http.StatusFound)
		case "/token":
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
	client := Client{}
	if claude {
		client = Client{ID: claudeCodeClientID, Name: "Claude Code", Redirects: []string{"http://localhost/callback", "http://127.0.0.1/callback"}, AuthMethod: "none", Grants: []string{"authorization_code", "refresh_token"}, Responses: []string{"code"}}
	}
	g, err := New(Config{PublicOrigin: hubURL, Resource: hubURL + "/mcp", Seats: seats, Login: loginFake{}, Clients: clientsFake{client: client}, Store: store, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	oauth, err := feishulogin.NewFeishuClient(feishulogin.OAuthConfig{AppID: "app", AppSecret: "test-only-secret", RedirectURI: callback, AuthorizeURL: feishu.URL + "/authorize", TokenURL: feishu.URL + "/token", UserInfoURL: feishu.URL + "/user", HTTPClient: feishu.Client()})
	if err != nil {
		t.Fatal(err)
	}
	login, err := feishulogin.NewHandler(feishulogin.Config{AppID: "app", TeamTenantKey: "team", RedirectURI: callback, OAuth: oauth, States: LoginStates{Store: store}, Tokens: extwire.Issuer{Seats: seats, Resource: hubURL + "/mcp"}, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	g.cfg.Login = login
	mux := http.NewServeMux()
	g.Register(mux)
	hub.Config.Handler = mux
	challenge := sha256.Sum256([]byte(verifier))
	start := hubURL + "/oauth/authorize?" + url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {loopURL}, "resource": {hubURL + "/mcp"}, "scope": {extshared.ScopeString()}, "state": {flowState}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}.Encode()
	script := filepath.Join(t.TempDir(), "drive.js")
	driver := `const { chromium } = require('playwright'); (async()=>{ const b=await chromium.launch(); const c=await b.newContext({ignoreHTTPSErrors:true}); const p=await c.newPage(); let n=0; p.on('console',m=>{if(/Content Security Policy/i.test(m.text())) n++}); await p.goto(process.argv[2]); await p.fill('input[name=short_name]','bot'); await p.fill('input[name=target_agent]','target.zzc'); await p.click('button[type=submit], input[type=submit], button'); if(process.argv[3]==='consent') await p.waitForSelector('button'); if(process.argv[3]==='consent') await p.click('button'); await p.waitForTimeout(1200); console.log('FINAL_URL '+p.url()); console.log('CSP_ERRORS '+n); await b.close(); })().catch(e=>{console.log('DRIVER_ERROR '+e);process.exit(2)});`
	if err := os.WriteFile(script, []byte(driver), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	mode := "immediate"
	if consent {
		mode = "consent"
	}
	out, err := exec.CommandContext(ctx, "node", script, start, mode).CombinedOutput()
	t.Logf("Claude Code browser output: %s", out)
	if err != nil {
		t.Fatalf("CLAUDE_BROWSER_DRIVER: %v", err)
	}
	if !strings.Contains(string(out), "CSP_ERRORS 0") {
		t.Fatalf("CLAUDE_BROWSER_CSP_ERRORS: %s", out)
	}
	select {
	case hit := <-hits:
		if !strings.Contains(hit, "code=") || !strings.Contains(hit, "state="+flowState) {
			t.Fatalf("CLAUDE_BROWSER_CALLBACK_QUERY %q", hit)
		}
	default:
		t.Fatalf("CLAUDE_BROWSER_CALLBACK_NOT_REACHED: %s", out)
	}
}
