package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
	"github.com/zhangzongchu2019/dingwei/internal/mcpgw"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

type wiringClients struct{}

func (wiringClients) Resolve(context.Context, string) (mcpgw.Client, error) {
	return mcpgw.Client{ID: "https://client.example/client.json", Name: "test", Redirects: []string{"http://127.0.0.1:43123/callback"}, AuthMethod: "none", Grants: []string{"authorization_code"}, Responses: []string{"code"}}, nil
}

const wiringOrigin = "https://wiring.example"

func wiringConfig() r53Config {
	return r53Config{tenantKey: "team", resource: wiringOrigin + "/mcp", publicOrigin: wiringOrigin}
}
func wiringKey(t *testing.T) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, key, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DW_MCPGW_OAUTH_KEY_FILE", p)
}

// 完整路径集合来自真实 ServeMux 的只读反射；无需为产品注册器增加测试 hook。
func wiringPaths(t *testing.T, mux *http.ServeMux) []string {
	t.Helper()
	patterns := reflect.ValueOf(mux).Elem().FieldByName("patterns")
	if !patterns.IsValid() {
		t.Fatal("Go ServeMux patterns unavailable; update route set probe")
	}
	out := make([]string, 0, patterns.Len())
	for i := 0; i < patterns.Len(); i++ {
		out = append(out, patterns.Index(i).Elem().FieldByName("str").String())
	}
	sort.Strings(out)
	return out
}
func TestR53DisabledRouteSetUnchanged(t *testing.T) {
	for _, flag := range []string{"", "0", "true"} {
		t.Run(flag, func(t *testing.T) {
			t.Setenv("DW_R53_EXTSEAT", flag)
			mux := http.NewServeMux()
			hub := newplatHub(t)
			if err := wireNewPlatform(context.Background(), mux, hub, newPlatformConfig{publicOrigin: wiringOrigin}, identity.NewMemStore(), nil); err != nil {
				t.Fatal(err)
			}
			registerConsoleRoutes(mux, hub, true)
			before := wiringPaths(t, mux)
			if err := wireR53(context.Background(), r53Config{}, nil, nil, r53Dependencies{mux: mux}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, wiringPaths(t, mux)) {
				t.Fatal("disabled r53 changed registered route set")
			}
		})
	}
}
func TestR53ResourceMustMatchPublicOrigin(t *testing.T) {
	t.Setenv("DW_R53_EXTSEAT", "1")
	for _, origin := range []string{"https://other.example", "", wiringOrigin + "/", wiringOrigin + "?", "http://wiring.example"} {
		cfg := wiringConfig()
		cfg.publicOrigin = origin
		err := wireR53(context.Background(), cfg, nil, nil, r53Dependencies{})
		if err == nil || !strings.Contains(err.Error(), "Resource 必须等于 PublicOrigin") {
			t.Fatalf("origin=%q: want consistency refusal, got %v", origin, err)
		}
	}
}
func TestR53WiringPG(t *testing.T) {
	t.Setenv("DW_R53_EXTSEAT", "1")
	wiringKey(t)
	jobs := &lifecycle.Group{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), runTasksKey{}, jobs))
	defer cancel()
	store, pool, closeStore, err := openNewPlatformStore(ctx, pgSchemaDSN(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	defer func() {
		cancel()
		waitCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := jobs.Wait(waitCtx); err != nil {
			t.Error("r53 worker did not stop", err)
		}
	}()
	cfg := wiringConfig()
	checker := pgTableChecker{pool: pool}
	hub := newplatHub(t)
	hub.Identity = identity.NewService(store, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	d := r53Dependencies{mux: mux, hub: hub, pool: pool, appID: "app", appSecret: "test-only", clients: wiringClients{}}
	if err := wireR53(ctx, cfg, checker, nil, d); !errors.Is(err, errR53GroupsMissing) || !strings.Contains(err.Error(), "D 路") {
		t.Fatalf("missing D accepted or wrong error: %v", err)
	}
	if !reflect.DeepEqual(wiringPaths(t, mux), []string{"/healthz"}) {
		t.Fatal("nil groups partially registered routes")
	}
	_, err = pool.Exec(ctx, repo.SystemCtx(), `INSERT INTO np_user(user_id,user_id_lc,short_name,short_name_lc,type,state,created_at) VALUES('ho','ho','ho','ho','member','active',now()); INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('helper','ho','helper','helper','selfhost','active',now());`)
	if err != nil {
		t.Fatal(err)
	}
	group := &tempgroup.Service{Pool: pool, Notifier: hub}
	t.Run("missing_key_keeps_hub", func(t *testing.T) {
		t.Setenv("DW_MCPGW_OAUTH_KEY_FILE", filepath.Join(t.TempDir(), "absent"))
		if err := wireR53(ctx, cfg, checker, group, d); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(wiringPaths(t, mux), []string{"/healthz"}) || hub.ExternalSeats != nil {
			t.Fatal("bad key activated B")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", wiringOrigin+"/healthz", nil))
		if w.Code != 204 {
			t.Fatal("bad key broke Hub")
		}
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.ParseForm() != nil || r.PostForm.Get("client_id") != "app" || r.PostForm.Get("client_secret") != "test-only" || r.PostForm.Get("redirect_uri") != wiringOrigin+"/mcp/oauth/feishu/callback" || r.PostForm.Get("code") != "feishu-code" {
				t.Error("Feishu exchange not bound")
				w.WriteHeader(400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "upstream-only", "token_type": "Bearer"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer upstream-only" {
				t.Error("missing upstream bearer")
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": feishulogin.UserInfo{EnName: "Alice", OpenID: "trusted-open", TenantKey: "team"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	d.oauth, err = feishulogin.NewFeishuClient(feishulogin.OAuthConfig{AppID: d.appID, AppSecret: d.appSecret, RedirectURI: wiringOrigin + "/mcp/oauth/feishu/callback", AuthorizeURL: upstream.URL + "/authorize", TokenURL: upstream.URL + "/token", UserInfoURL: upstream.URL + "/user", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := wireR53(ctx, cfg, checker, group, d); err != nil {
		t.Fatal(err)
	}
	want := []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/mcp", "/api/external/", "/feishu/connect", "/healthz", "/mcp", "/mcp/oauth/feishu/callback", "/oauth/authorize", "/oauth/token"}
	sort.Strings(want)
	if !reflect.DeepEqual(wiringPaths(t, mux), want) {
		t.Fatalf("registered routes %v", wiringPaths(t, mux))
	}
	if hub.ExternalSeats == nil {
		t.Fatal("Hub external seats not connected")
	}
	const verifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {"https://client.example/client.json"}, "redirect_uri": {"http://127.0.0.1:43123/callback"}, "resource": {cfg.resource}, "scope": {extshared.ScopeString()}, "state": {"outer-state"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}}
	request := func(method, path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
		var body *strings.Reader
		if form == nil {
			body = strings.NewReader("")
		} else {
			body = strings.NewReader(form.Encode())
		}
		r := httptest.NewRequest(method, wiringOrigin+path, body)
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/oauth/authorize?"+q.Encode(), nil, nil)
	if w.Code != 302 {
		t.Fatalf("authorize %d", w.Code)
	}
	cookies := w.Result().Cookies()
	location := w.Header().Get("Location")
	u, _ := url.Parse(location)
	attempt := u.Query().Get("attempt_id")
	w = request("GET", location, nil, cookies)
	if w.Code != 200 {
		t.Fatalf("connect %d", w.Code)
	}
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(m) != 2 {
		t.Fatal("missing C CSRF")
	}
	w = request("POST", "/feishu/connect", url.Values{"attempt_id": {attempt}, "csrf_token": {html.UnescapeString(m[1])}, "short_name": {"bot"}, "target_agent": {"helper.ho"}}, cookies)
	if w.Code != 303 {
		t.Fatalf("start %d", w.Code)
	}
	u, _ = url.Parse(w.Header().Get("Location"))
	state := u.Query().Get("state")
	w = request("GET", "/mcp/oauth/feishu/callback?"+url.Values{"state": {state}, "code": {"feishu-code"}}.Encode(), nil, cookies)
	if w.Code != 302 {
		t.Fatalf("callback %d %s", w.Code, w.Body)
	}
	u, _ = url.Parse(w.Header().Get("Location"))
	if u.Query().Get("state") != "outer-state" || u.Query().Get("iss") != wiringOrigin {
		t.Fatal("callback binding")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {u.Query().Get("code")}, "client_id": {q.Get("client_id")}, "redirect_uri": {q.Get("redirect_uri")}, "resource": {cfg.resource}, "code_verifier": {verifier}}
	w = request("POST", "/oauth/token", form, nil)
	if w.Code != 200 {
		t.Fatalf("token %d %s", w.Code, w.Body)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	if json.Unmarshal(w.Body.Bytes(), &token) != nil || token.AccessToken == "" || token.Scope != extshared.ScopeString() {
		t.Fatal("invalid token")
	}
	r := httptest.NewRequest("POST", cfg.resource, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Authorization", "Bearer "+token.AccessToken)
	r.Header.Set("MCP-Protocol-Version", mcpgw.ProtocolVersion)
	r.Header.Set("Mcp-Method", "tools/list")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "list_online") || strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("tools/list %d %s", w.Code, w.Body)
	}
	if w = request("POST", "/oauth/token", form, nil); w.Code == 200 {
		t.Fatal("login code replay accepted")
	}

	list, err := group.List(ctx, "ho")
	if err != nil || len(list) != 1 || len(list[0].Members) != 2 {
		t.Fatalf("real D Console list: %v %v", list, err)
	}
	if hub.TempGroups != group {
		t.Fatal("D not wired into Hub")
	}
	if err = group.Dissolve(ctx, "ho", list[0].Code, list[0].ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if err = group.NotifyPending(ctx); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", wiringOrigin+"/api/external/online", nil)
	r.Header.Set("Authorization", "Bearer "+token.AccessToken)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("dissolved token remains usable")
	}
	cancel()
}
