package mcpgw

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extwire"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

func dcrRequest(t *testing.T, c *http.Client, method, uri, body, media string) (*http.Response, []byte) {
	t.Helper()
	r, e := http.NewRequest(method, uri, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	if media != "" {
		r.Header.Set("Content-Type", media)
	}
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	return resp, b
}

func TestDCRActualHTTPRegisterFeishuTokenAndInitialize(t *testing.T) {
	_, pool, key := dcrPG(t, true)
	ctx := context.Background()
	// Only an internal target exists. No external seat/AG is pre-created.
	dcrExec(t, pool, `INSERT INTO np_user(user_id,user_id_lc,short_name,short_name_lc,type,state,created_at) VALUES('owner','owner','zzc','zzc','member','active',now()); INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('target-id','owner','target','target','selfhost','active',now())`)
	hub := httptest.NewTLSServer(nil)
	defer hub.Close()
	origin := hub.URL
	resource := origin + "/mcp"
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.ParseForm() != nil || r.PostForm.Get("code") != "feishu-code" || r.PostForm.Get("redirect_uri") != origin+"/mcp/oauth/feishu/callback" {
				t.Error("upstream redirect not bound")
				w.WriteHeader(400)
				return
			}
			writeJSON(w, 200, map[string]any{"code": 0, "access_token": "test-feishu-upstream-token", "token_type": "Bearer"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer test-feishu-upstream-token" {
				t.Error("upstream bearer missing")
			}
			writeJSON(w, 200, map[string]any{"code": 0, "data": feishulogin.UserInfo{Name: "User", EnName: "Alice", OpenID: "trusted-person", TenantKey: "team"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	access := testAccess(t)
	store, e := NewSQLStore(ctx, pool, access, resource, key)
	if e != nil {
		t.Fatal(e)
	}
	dynamic, e := NewSQLDynamicClients(ctx, pool, access, resource, key, 0)
	if e != nil {
		t.Fatal(e)
	}
	d, e := NewDCR(DCRPolicy{Enabled: true}, dynamic)
	if e != nil {
		t.Fatal(e)
	}
	seats := &extseat.Service{Pool: pool, Access: access, Identity: identity.NewService(identity.NewPGStore(pool), nil, nil), Groups: &tempgroup.Service{Pool: pool, Access: access}, TenantKey: "team", Resource: resource}
	oauth, e := feishulogin.NewFeishuClient(feishulogin.OAuthConfig{AppID: "app", AppSecret: "test-only-upstream-secret", RedirectURI: origin + "/mcp/oauth/feishu/callback", AuthorizeURL: upstream.URL + "/authorize", TokenURL: upstream.URL + "/token", UserInfoURL: upstream.URL + "/user", HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	login, e := feishulogin.NewHandler(feishulogin.Config{AppID: "app", TeamTenantKey: "team", RedirectURI: origin + "/mcp/oauth/feishu/callback", OAuth: oauth, States: LoginStates{Store: store}, Tokens: extwire.Issuer{Seats: seats, Resource: resource}})
	if e != nil {
		t.Fatal(e)
	}
	g, e := New(Config{PublicOrigin: origin, Resource: resource, Seats: seats, Login: login, Clients: MetadataClients{}, Store: store, DCR: d})
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	hub.Config.Handler = mux
	browser := hub.Client()
	browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	browser.Jar, _ = cookiejar.New(nil)
	resp, b := dcrRequest(t, browser, "GET", origin+"/.well-known/oauth-authorization-server", "", "")
	var metadata map[string]any
	if json.Unmarshal(b, &metadata) != nil || resp.StatusCode != 200 || metadata["registration_endpoint"] != origin+"/oauth/register" {
		t.Fatalf("DCR discovery missing: %d %s", resp.StatusCode, b)
	}
	resp, b = dcrRequest(t, browser, "POST", origin+"/oauth/register", publicRegistration, "application/json")
	if resp.StatusCode != 201 {
		t.Fatalf("DCR public registration %d %s", resp.StatusCode, b)
	}
	var registration map[string]any
	if json.Unmarshal(b, &registration) != nil {
		t.Fatal("bad registration response")
	}
	id, _ := registration["client_id"].(string)
	if !strings.HasPrefix(id, dcrPrefix) || registration["token_endpoint_auth_method"] != "none" {
		t.Fatal("bad public client identity")
	}
	for _, field := range []string{"client_secret", "client_secret_expires_at", "registration_access_token", "registration_client_uri"} {
		if _, ok := registration[field]; ok {
			t.Fatal("public client was issued secret/management capability")
		}
	}
	if dcrCount(t, pool, `SELECT count(*) FROM np_extseat`) != 0 {
		t.Fatal("registration created an Agent")
	}
	query := authorizationQuery()
	query.Set("client_id", id)
	query.Set("resource", resource)
	bad := url.Values{}
	for k, v := range query {
		bad[k] = append([]string(nil), v...)
	}
	bad.Set("code_challenge_method", "plain")
	resp, _ = dcrRequest(t, browser, "GET", origin+"/oauth/authorize?"+bad.Encode(), "", "")
	if resp.StatusCode != 400 {
		t.Fatal("DCR authorization did not enforce S256")
	}
	resp, _ = dcrRequest(t, browser, "GET", origin+"/oauth/authorize?"+query.Encode(), "", "")
	if resp.StatusCode != 302 {
		t.Fatalf("DCR authorize=%d", resp.StatusCode)
	}
	connect := resp.Header.Get("Location")
	resp, b = dcrRequest(t, browser, "GET", origin+connect, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("connect=%d %s", resp.StatusCode, b)
	}
	match := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindSubmatch(b)
	if len(match) != 2 {
		t.Fatal("missing browser-bound connect CSRF")
	}
	u, _ := url.Parse(connect)
	form := url.Values{"attempt_id": {u.Query().Get("attempt_id")}, "csrf_token": {html.UnescapeString(string(match[1]))}, "short_name": {"editor"}, "target_agent": {"target.zzc"}}
	r, e := http.NewRequest("POST", origin+"/feishu/connect", strings.NewReader(form.Encode()))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	resp, e = browser.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Fatalf("connect post %d", resp.StatusCode)
	}
	up, _ := url.Parse(resp.Header.Get("Location"))
	state := up.Query().Get("state")
	if state == "" {
		t.Fatal("missing Feishu state")
	}
	resp, b = dcrRequest(t, browser, "GET", origin+"/mcp/oauth/feishu/callback?"+url.Values{"code": {"feishu-code"}, "state": {state}}.Encode(), "", "")
	if resp.StatusCode != 302 {
		t.Fatalf("trusted Feishu callback=%d %s", resp.StatusCode, b)
	}
	callback, _ := url.Parse(resp.Header.Get("Location"))
	if callback.Host != "127.0.0.1:43123" || callback.Query().Get("state") != "outer-state" {
		t.Fatal("client callback lost exact binding")
	}
	code := callback.Query().Get("code")
	if code == "" {
		t.Fatal("missing code")
	}
	if dcrCount(t, pool, `SELECT count(*) FROM np_extseat`) != 1 {
		t.Fatal("valid OAuth did not automatically create external Agent")
	}
	if dcrCount(t, pool, `SELECT count(*) FROM mcpgw_dcr_binding`) != 0 {
		t.Fatal("client bound before successful token exchange")
	}
	token := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {id}, "redirect_uri": {clientRedirect}, "code_verifier": {verifier}, "resource": {resource}}
	negativeCode, e := store.Get(ctx, "code", hash(code), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Put(ctx, "code", hash("negative-copy"), negativeCode, time.Now().Add(time.Minute), time.Now()); e != nil {
		t.Fatal(e)
	}
	badToken := url.Values{}
	for k, v := range token {
		badToken[k] = append([]string(nil), v...)
	}
	badToken.Set("code", "negative-copy")
	badToken.Set("code_verifier", "Z"+verifier[1:])
	resp, _ = dcrRequest(t, browser, "POST", origin+"/oauth/token", badToken.Encode(), "application/x-www-form-urlencoded")
	if resp.StatusCode != 400 || dcrCount(t, pool, `SELECT count(*) FROM mcpgw_dcr_binding`) != 0 {
		t.Fatal("failed PKCE exchange bound/renewed a client")
	}
	d.Policy.Enabled = false // Existing five-minute snapshots finish while new DCR authorization is closed.
	resp, b = dcrRequest(t, browser, "POST", origin+"/oauth/token", token.Encode(), "application/x-www-form-urlencoded")
	if resp.StatusCode != 200 {
		t.Fatalf("token exchange=%d %s", resp.StatusCode, b)
	}
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(b, &issued) != nil || issued.AccessToken == "" {
		t.Fatal("missing local bearer")
	}
	if dcrCount(t, pool, `SELECT count(*) FROM mcpgw_dcr_binding`) != 1 {
		t.Fatal("successful token exchange did not persist bound client")
	}
	r, e = http.NewRequest("POST", resource, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"DCR editor","version":"test"}}}`))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("MCP-Protocol-Version", "2025-06-18")
	r.Header.Set("Authorization", "Bearer "+issued.AccessToken)
	resp, e = browser.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Contains(b, []byte(`"protocolVersion":"2025-06-18"`)) {
		t.Fatalf("authenticated DCR initialize=%d %s", resp.StatusCode, b)
	}
	resp, _ = dcrRequest(t, browser, "POST", origin+"/oauth/token", token.Encode(), "application/x-www-form-urlencoded")
	if resp.StatusCode != 400 {
		t.Fatal("authorization code replay accepted")
	}
	// Removing registration does not revoke the already-issued seat token.
	dcrExec(t, pool, `DELETE FROM mcpgw_dcr_client WHERE client_id=$1`, id)
	if _, e = seats.Authenticate(ctx, issued.AccessToken); e != nil {
		t.Fatal("client expiry changed bearer permissions", e)
	}
}

func TestDCRHTTPRejectionsRateAndSafeDiagnostics(t *testing.T) {
	s, p, _ := dcrPG(t, false)
	g, _, _ := fixture(t)
	d, e := NewDCR(DCRPolicy{Enabled: true}, s)
	if e != nil {
		t.Fatal(e)
	}
	g.cfg.DCR = d
	var logs bytes.Buffer
	g.cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	for _, tc := range []struct {
		body     string
		code     int
		category string
	}{
		{strings.Replace(publicRegistration, "http://127.0.0.1:43123/callback", "http://evil.example/callback?private-body", 1), 400, "loopback_bad"},
		{strings.Replace(publicRegistration, "http://127.0.0.1:43123/callback", "https://evil.example/callback?private-body", 1), 400, "https_unlisted"},
		{strings.Replace(publicRegistration, "http://127.0.0.1:43123/callback", "codebuddy://oauth/callback?private-body", 1), 400, "custom_scheme"},
		{strings.TrimSuffix(publicRegistration, "}") + `,"client_secret":"not-allowed"}`, 400, ""},
	} {
		dcrExec(t, p, `DELETE FROM mcpgw_dcr_bucket`)
		resp, b := dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", tc.body, "application/json")
		if resp.StatusCode != tc.code {
			t.Fatalf("unsafe registration %d: %s", resp.StatusCode, b)
		}
		if tc.category != "" && !strings.Contains(logs.String(), tc.category) {
			t.Fatal("missing fixed redirect category")
		}
	}
	if strings.Contains(logs.String(), "private-body") || strings.Contains(logs.String(), "evil.example") || !strings.Contains(logs.String(), `"scheme":"codebuddy"`) {
		t.Fatal("redirect diagnostic leaked URI or omitted scheme")
	}
	dcrExec(t, p, `DELETE FROM mcpgw_dcr_bucket`)
	for i := 0; i < 5; i++ {
		resp, _ := dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", `{}`, "application/json")
		if resp.StatusCode != 400 {
			t.Fatal("metadata failures did not reach validation")
		}
	}
	resp, _ := dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", publicRegistration, "application/json")
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatal("bad metadata bypassed persistent rate limit")
	}
	dcrExec(t, p, `DELETE FROM mcpgw_dcr_bucket`)
	resp, _ = dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", strings.Repeat("a", 16*1024+1), "application/json")
	if resp.StatusCode != 413 {
		t.Fatal("registration body not bounded")
	}
	dcrExec(t, p, `DELETE FROM mcpgw_dcr_bucket`)
	resp, _ = dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", publicRegistration, "text/plain")
	if resp.StatusCode != 415 {
		t.Fatal("wrong media accepted")
	}
	dcrExec(t, p, `DELETE FROM mcpgw_dcr_bucket`)
	resp, _ = dcrRequest(t, server.Client(), "GET", server.URL+"/oauth/register", "", "")
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" {
		t.Fatal("registration method not constrained")
	}
	d.Policy.Enabled = false
	resp, _ = dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", publicRegistration, "application/json")
	if resp.StatusCode != 404 {
		t.Fatal("disabled DCR still registered clients")
	}
	d.Policy.Enabled = true
	d.Store = nil
	resp, _ = dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", publicRegistration, "application/json")
	if resp.StatusCode != 503 {
		t.Fatal("unready DCR fell back to volatile state")
	}
}
