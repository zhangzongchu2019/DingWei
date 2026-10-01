package m8

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

const npOrigin = "https://dw.cmjrt.com:55055"

// viewEnv 在 agentEnv 之上：再开一个用户 lbl（简称 lb），并为两人各发一个 console 会话；
// 两人名下各有一个同名 Agent「Kimi」——专门用来验「按 full_name 不串号」。
type viewEnv struct {
	*agentEnv
	ob                 *identity.Onboarding
	srv                *httptest.Server
	tokZZC, tokLBL     string
	zKimiCred, lKimiCr string
}

func newViewEnv(t *testing.T) *viewEnv {
	t.Helper()
	e := newAgentEnv(t)
	ctx := e.ctx
	if err := e.svc.Store.CreateUser(ctx, identity.User{UserID: "lbl", ShortName: "lb", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	v := &viewEnv{agentEnv: e, ob: identity.NewOnboarding(e.svc), tokZZC: "tok-zzc", tokLBL: "tok-lbl"}
	e.hub.Onboarding = v.ob
	e.hub.PublicOrigin = npOrigin
	for user, tok := range map[string]string{"zzc": v.tokZZC, "lbl": v.tokLBL} {
		if err := e.svc.Store.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret(tok), UserIDLC: user, ExpiresAt: e.svc.Now().Add(identity.SessionTTL)}); err != nil {
			t.Fatal(err)
		}
	}
	v.zKimiCred, _ = e.enroll("dev-zk", "Kimi")
	v.lKimiCr, _ = v.enrollFor("lbl", "dev-lk", "kimi")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", e.hub.HandleAgentWS)
	mux.HandleFunc("GET /view/{sessionName}", e.hub.RequireConsoleAgent("view", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "page:"+r.PathValue("sessionName"))
	}))
	mux.HandleFunc("/view/{sessionName}/{rest...}", e.hub.RequireConsoleViewSubPath(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "sub:"+r.PathValue("rest")) }))
	v.srv = httptest.NewServer(mux)
	t.Cleanup(v.srv.Close)
	return v
}

func (v *viewEnv) enrollFor(owner, device, name string) (string, string) {
	v.t.Helper()
	digest, _ := identity.PairingDigest("000102030405060708090a0b0c0d0e0f", "123456")
	r, err := v.svc.Apply(v.ctx, identity.ApplyRequest{OwnerUserID: owner, PairingSalt: "000102030405060708090a0b0c0d0e0f", PairingDigest: digest, DeviceFingerprint: device, SourceIP: "127.0.0.2"})
	if err != nil {
		v.t.Fatal(err)
	}
	ag, _, err := v.svc.Approve(v.ctx, owner, r.ApplicationID, "123456", name, "g")
	if err != nil {
		v.t.Fatal(err)
	}
	p, _ := v.svc.Poll(v.ctx, r.ApplicationID, r.PollToken)
	return p.Credential, ag.ID
}

func (v *viewEnv) get(method, path, tok, origin string) (int, string) {
	v.t.Helper()
	req, _ := http.NewRequest(method, v.srv.URL+path, nil)
	if tok != "" {
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		v.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(b))
}

func TestNewPlatformViewAuth(t *testing.T) {
	v := newViewEnv(t)
	cases := []struct {
		name, method, path, tok, origin string
		status                          int
		body                            string
	}{
		{"no session", "GET", "/view/Kimi.zzc", "", "", 401, `{"error":"unauthenticated"}`},
		{"owner page", "GET", "/view/Kimi.zzc", v.tokZZC, "", 200, "page:Kimi.zzc"},
		{"owner page case-insensitive", "GET", "/view/KIMI.ZZC", v.tokZZC, "", 200, "page:KIMI.ZZC"},
		{"other user's agent", "GET", "/view/Kimi.zzc", v.tokLBL, "", 404, `{"error":"not_found"}`},
		{"own same-named agent", "GET", "/view/kimi.lb", v.tokLBL, "", 200, "page:kimi.lb"},
		{"bare session name", "GET", "/view/kimi", v.tokZZC, "", 404, `{"error":"not_found"}`},
		{"no such agent", "GET", "/view/ghost.zzc", v.tokZZC, "", 404, `{"error":"not_found"}`},
		{"subpath GET", "GET", "/view/Kimi.zzc/events", v.tokZZC, "", 200, "sub:events"},
		{"subpath POST no origin", "POST", "/view/Kimi.zzc/input", v.tokZZC, "", 403, `{"error":"origin_mismatch"}`},
		{"subpath POST wrong origin", "POST", "/view/Kimi.zzc/input", v.tokZZC, "https://evil.example", 403, `{"error":"origin_mismatch"}`},
		{"subpath POST ok", "POST", "/view/Kimi.zzc/input", v.tokZZC, npOrigin, 200, "sub:input"},
		{"subpath POST other user", "POST", "/view/Kimi.zzc/input", v.tokLBL, npOrigin, 404, `{"error":"not_found"}`},
	}
	for _, c := range cases {
		if st, body := v.get(c.method, c.path, c.tok, c.origin); st != c.status || body != c.body {
			t.Errorf("%s: %d %s, want %d %s", c.name, st, body, c.status, c.body)
		}
	}
	// 必须改密的会话不算登录
	_ = v.svc.Store.CreateUser(v.ctx, identity.User{UserID: "temp", ShortName: "tp", State: identity.UserActive, PasswordHash: "x", MustChangePassword: true})
	_ = v.svc.Store.CreateSession(v.ctx, identity.Session{TokenHash: identity.HashSecret("tok-tmp"), UserIDLC: "temp", ExpiresAt: v.svc.Now().Add(identity.SessionTTL)})
	if st, body := v.get("GET", "/view/Kimi.zzc", "tok-tmp", ""); st != 403 || body != `{"error":"must_change_password"}` {
		t.Errorf("must-change session: %d %s", st, body)
	}
	// WebSocket 升级缺 Origin ⇒ 拒（防跨站 WebSocket 劫持）
	req, _ := http.NewRequest("GET", v.srv.URL+"/view/Kimi.zzc", nil)
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: v.tokZZC})
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Errorf("websocket upgrade without Origin: %d, want 403", res.StatusCode)
	}
}

// 防串号：两人各有一个已连入的同名 Agent，full_name 各自定位；裸名一个都不命中。
func TestNewPlatformTerminalLookupByFullName(t *testing.T) {
	v := newViewEnv(t)
	for _, cred := range []string{v.zKimiCred, v.lKimiCr} {
		c, st, body := v.dial(cred, "")
		if c == nil {
			t.Fatalf("dial: %d %s", st, body)
		}
		defer c.Close(websocket.StatusNormalClosure, "done")
		reader(c)
	}
	waitSessionOnline(t, v.hub, "u_zzc", "kimi")
	waitSessionOnline(t, v.hub, "u_lbl", "kimi")
	look := func(ref string) (string, bool) {
		v.hub.mu.Lock()
		defer v.hub.mu.Unlock()
		st, ok := v.hub.terminalBySessionNameLocked(ref)
		if !ok {
			return "", false
		}
		return st.keyID + "/" + st.sessionName, true
	}
	if got, ok := look("Kimi.zzc"); !ok || got != "u_zzc/kimi" {
		t.Fatalf("Kimi.zzc ⇒ %q %v, want u_zzc/kimi", got, ok)
	}
	if got, ok := look("KIMI.LB"); !ok || got != "u_lbl/kimi" {
		t.Fatalf("KIMI.LB ⇒ %q %v, want u_lbl/kimi", got, ok)
	}
	for _, bare := range []string{"kimi", "Kimi", "opus50ts"} {
		if got, ok := look(bare); ok {
			t.Fatalf("bare %q must not resolve in new-platform mode, got %q", bare, got)
		}
	}
	// 离线的 Agent：full_name 合法但无会话 ⇒ 查不到
	if _, ok := look("Opus50TS.zzc"); ok {
		t.Fatal("offline agent resolved")
	}
}

// ＃unlock：只认归属用户的飞书身份，且只在其名下页面里找码。
func TestNewPlatformUnlockOnlyOwner(t *testing.T) {
	v := newViewEnv(t)
	bind := func(user, short, openID string) {
		_ = v.svc.Store.CreateUser(v.ctx, identity.User{UserID: user, ShortName: short, State: identity.UserPendingBind})
		_ = v.svc.Store.CreateRegistrationCode(v.ctx, user, "rc-"+user, v.svc.Now().Add(identity.RegistrationCodeTTL))
		if _, err := v.svc.Store.RedeemRegistrationCode(v.ctx, "rc-"+user, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: openID}, v.svc.Now()); err != nil {
			t.Fatal(err)
		}
	}
	bind("ann", "an", "ou_ann")
	bind("ben", "bn", "ou_ben")
	cred, _ := v.enrollFor("ann", "dev-ann", "Worker")
	c, st, body := v.dial(cred, "")
	if c == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	defer c.Close(websocket.StatusNormalClosure, "done")
	reader(c)
	waitSessionOnline(t, v.hub, "u_ann", "worker")
	v.hub.mu.Lock()
	term := v.hub.terminalForSession("u_ann", "worker")
	term.viewers["vw1"] = &terminalViewer{id: "vw1", code: "PGCODE01"}
	v.hub.mu.Unlock()
	send := func(openID, text string) string {
		res, handled, err := v.hub.dispatchTerminalInputCommand(context.Background(), msgFrom(openID, "devplatform"), text)
		if err != nil || !handled {
			t.Fatalf("dispatch %q: handled=%v err=%v", text, handled, err)
		}
		return res.Reply
	}
	if r := send("ou_ben", "#unlock PGCODE01"); !strings.Contains(r, "未找到") {
		t.Fatalf("non-owner unlock: %q", r)
	}
	if r := send("ou_stranger", "#unlock PGCODE01"); !strings.Contains(r, "未绑定") {
		t.Fatalf("unbound sender: %q", r)
	}
	v.hub.mu.Lock()
	writer := term.writerPage
	v.hub.mu.Unlock()
	if writer != "" {
		t.Fatalf("writer granted by non-owner: %q", writer)
	}
	if r := send("ou_ann", "#unlock pgcode01"); !strings.Contains(r, "已解锁") {
		t.Fatalf("owner unlock: %q", r)
	}
	v.hub.mu.Lock()
	writer = term.writerPage
	v.hub.mu.Unlock()
	if writer != "vw1" {
		t.Fatalf("writerPage=%q, want vw1", writer)
	}
	if r := send("ou_ann", "#lock PGCODE01"); !strings.Contains(r, "已锁定") {
		t.Fatalf("owner lock: %q", r)
	}
}

// 生产上旧平台 console 授权可能是开着的：新平台的 view 子路径 POST 不得再要求旧平台挂在 console_agent 上的页面 capability，
// 否则新平台用户在 view 页里永远发不出输入。测试环境默认关着旧授权，看不到这一格，故单独打开验。
func TestNewPlatformViewSubpathWithLegacyConsoleAuthOn(t *testing.T) {
	v := newViewEnv(t)
	v.hub.consoleAuthEnabled = true
	if st, body := v.get("POST", "/view/Kimi.zzc/input", v.tokZZC, npOrigin); st != 200 || body != "sub:input" {
		t.Fatalf("owner POST with legacy console auth on: %d %s, want 200 sub:input", st, body)
	}
	if st, _ := v.get("POST", "/view/Kimi.zzc/input", v.tokLBL, npOrigin); st != 404 {
		t.Fatalf("other user with legacy console auth on: %d, want 404", st)
	}
}

// 新平台 console 主页与接口的认人：页面跳登录页、接口回机器码；登录后放行并带上用户（tosonnet50ts 登录页的进入条件）。
func TestNewPlatformConsolePrincipal(t *testing.T) {
	v := newViewEnv(t)
	var gotPrincipal string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /console", v.hub.RequireConsolePrincipal(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal, _ = r.Context().Value(consolePrincipalContextKey{}).(string)
		_, _ = io.WriteString(w, "console")
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(tok, accept string) (int, string, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/console", nil)
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		res, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Location"), strings.TrimSpace(string(b))
	}
	if st, loc, _ := call("", "text/html,application/xhtml+xml"); st != 302 || loc != "/console/login" {
		t.Errorf("page, no session: %d %q", st, loc)
	}
	if st, _, body := call("", "application/json"); st != 401 || body != `{"error":"unauthenticated"}` {
		t.Errorf("api, no session: %d %s", st, body)
	}
	_ = v.svc.Store.CreateUser(v.ctx, identity.User{UserID: "temp2", ShortName: "t2", Type: identity.UserManager, State: identity.UserActive, PasswordHash: "x", MustChangePassword: true})
	_ = v.svc.Store.CreateSession(v.ctx, identity.Session{TokenHash: identity.HashSecret("tok-mc"), UserIDLC: "temp2", ExpiresAt: v.svc.Now().Add(identity.SessionTTL)})
	if st, loc, _ := call("tok-mc", "text/html"); st != 302 || loc != "/console/login#change" {
		t.Errorf("page, must change: %d %q", st, loc)
	}
	if st, _, body := call("tok-mc", "application/json"); st != 403 || body != `{"error":"must_change_password"}` {
		t.Errorf("api, must change: %d %s", st, body)
	}
	if st, _, body := call(v.tokZZC, "text/html"); st != 200 || body != "console" || gotPrincipal != "zzc" {
		t.Errorf("logged in: %d %s principal=%q", st, body, gotPrincipal)
	}
}
