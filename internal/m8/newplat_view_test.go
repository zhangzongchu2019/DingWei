package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
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
	ag, _, err := v.svc.ApproveWithNewGroup(v.ctx, owner, r.ApplicationID, "123456", name, tg())
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
	sendAs := func(m model.Message, text string) string {
		res, handled, err := v.hub.dispatchTerminalInputCommand(context.Background(), m, text)
		if err != nil || !handled {
			t.Fatalf("dispatch %q: handled=%v err=%v", text, handled, err)
		}
		return res.Reply
	}
	send := func(openID, text string) string {
		return sendAs(trustedMsgFrom(openID, "devplatform", model.IngressFeishuWSAuthenticated), text)
	}
	// 不可信入站（未签名 webhook）冒充属主：拒，不授写权（板 6c90284e）
	if r := sendAs(msgFrom("ou_ann", "devplatform"), "#unlock PGCODE01"); r != untrustedIngressReply {
		t.Fatalf("untrusted owner unlock: %q", r)
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

// 新平台 /api/console/platform：角色由用户类型推出；管理角色才有 platform-admin；普通用户做管理动作 ⇒ 403。
func TestNewPlatformConsolePlatform(t *testing.T) {
	v := newViewEnv(t)
	for _, u := range []identity.User{
		{UserID: "mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "mem", ShortName: "me", Type: identity.UserMember, State: identity.UserActive},
	} {
		_ = v.svc.Store.CreateUser(v.ctx, u)
		_ = v.svc.Store.CreateSession(v.ctx, identity.Session{TokenHash: identity.HashSecret("tok-" + u.UserID), UserIDLC: u.UserID, ExpiresAt: v.svc.Now().Add(identity.SessionTTL)})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/platform", v.hub.RequireConsolePrincipal(v.hub.RequireConsolePlatformAction("read", v.hub.HandleConsolePlatform)))
	mux.HandleFunc("POST /api/console/platform/items", v.hub.RequireConsolePrincipal(v.hub.RequireConsolePlatformAction("add", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "added") })))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	call := func(method, path, tok string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		req.Header.Set("Accept", "application/json")
		if method != "GET" {
			req.Header.Set("Origin", npOrigin)
		}
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}
	if st, body := call("GET", "/api/console/platform", "tok-mgr"); st != 200 || !strings.Contains(body, `"role":"management"`) || !strings.Contains(body, `"id":"platform-admin"`) {
		t.Errorf("manager platform: %d %s", st, body)
	}
	st, body := call("GET", "/api/console/platform", "tok-mem")
	if st != 200 || !strings.Contains(body, `"role":"owner"`) || strings.Contains(body, "platform-admin") ||
		!strings.Contains(body, `"items":[{"id":"agent-groups","kind":"agent-groups","title":"Agent 管理","href":"#agent-groups","status":"approved","active":true,`) ||
		strings.Count(body, `"id":`) != 1 {
		t.Errorf("member platform: %d %s", st, body)
	}
	if strings.Contains(body, `"id":"specs"`) {
		t.Errorf("new platform must not serve legacy specs entry: %s", body)
	}
	if st, body := call("POST", "/api/console/platform/items", "tok-mem"); st != 403 || body != `{"error":"forbidden"}` {
		t.Errorf("member add: %d %s", st, body)
	}
	if st, body := call("POST", "/api/console/platform/items", "tok-mgr"); st != 200 || body != "added" {
		t.Errorf("manager add: %d %s", st, body)
	}
	if st, _ := call("GET", "/api/console/platform", ""); st != 401 {
		t.Errorf("no session: %d", st)
	}
	// 写操作缺 Origin ⇒ 403（与 N3、入网接口、view 授权同口径）
	req, _ := http.NewRequest("POST", srv.URL+"/api/console/platform/items", nil)
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "tok-mgr"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 403 || strings.TrimSpace(string(b)) != `{"error":"origin_mismatch"}` {
		t.Errorf("write without Origin: %d %s", res.StatusCode, b)
	}
}

// 页面拿 /status 的 session 拼 capability 与上传票据的路径、带 X-DingWei-Session 上传头，并与票据的 session 严格比较
// ⇒ 反代给 view-service 的 X-DW-Session（即 /status 的 session）与票据的会话名必须是同一个规范 full_name。
func TestNewPlatformViewProxySessionIsCanonicalFullName(t *testing.T) {
	v := newViewEnv(t)
	vs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path+" session="+r.Header.Get("X-DW-Session"))
	}))
	defer vs.Close()
	// 简称本身带大写的用户：规范写法里简称也得转小写
	if err := v.svc.Store.CreateUser(v.ctx, identity.User{UserID: "mix", ShortName: "Mx", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	v.enrollFor("mix", "dev-mx", "Bot")
	for ref, want := range map[string]string{"Kimi.zzc": "kimi.zzc", "KIMI.ZZC": "kimi.zzc", "kimi.zzc": "kimi.zzc", "kimi.LB": "kimi.lb", "Bot.Mx": "bot.mx"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/view/"+ref+"/status", nil)
		if !v.hub.proxyToViewService(rec, req, ref, vs.URL) {
			t.Fatalf("%s: proxy unreachable", ref)
		}
		if got := rec.Body.String(); got != "/status session="+want {
			t.Errorf("%s: %q, want /status session=%s", ref, got, want)
		}
	}
}

func TestNewPlatformConsoleUploadTicketAndCapability(t *testing.T) {
	v := newViewEnv(t)
	issuer := &recordingFileIssuer{}
	v.hub.fileIssuer = issuer
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agents/{sessionName}/capability", v.hub.RequireConsoleAgent("interact", v.hub.HandleConsoleCapability))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/file-upload-ticket", v.hub.RequireConsoleAgent("interact", v.hub.HandleConsoleFileUploadTicket))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	post := func(path, tok, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		req.Header.Set("Origin", npOrigin)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	const ticketPath = "/api/console/agents/%s/file-upload-ticket"
	// 离线：认过人，但没有在线会话 ⇒ 409，不发票
	if st, _ := post(fmt.Sprintf(ticketPath, "Kimi.zzc"), v.tokZZC, `{"max_bytes":10}`); st != http.StatusConflict || issuer.calls != 0 {
		t.Fatalf("offline: %d calls=%d", st, issuer.calls)
	}
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
	st, out := post(fmt.Sprintf(ticketPath, "KIMI.ZZC"), v.tokZZC, `{"max_bytes":10}`)
	if st != 200 || out["session"] != "kimi.zzc" || out["ticket"] != "opaque-ticket" || out["upload_url"] != "https://files.example/files/upload" {
		t.Fatalf("owner ticket: %d %v", st, out)
	}
	if issuer.calls != 1 || issuer.req.SessionName != "kimi.zzc" || issuer.req.OwnerKey != "u_zzc" || issuer.req.MaxBytes != 10 || issuer.req.Operation != "upload" {
		t.Fatalf("issued: calls=%d %+v", issuer.calls, issuer.req)
	}
	// 同名的另一人的 Agent：各发各的，不串号
	if st, out := post(fmt.Sprintf(ticketPath, "kimi.lb"), v.tokLBL, `{"max_bytes":7}`); st != 200 || out["session"] != "kimi.lb" || issuer.req.OwnerKey != "u_lbl" || issuer.req.SessionName != "kimi.lb" {
		t.Fatalf("lbl ticket: %d %v %+v", st, out, issuer.req)
	}
	// 非归属用户 ⇒ 404，不发票
	if st, out := post(fmt.Sprintf(ticketPath, "Kimi.zzc"), v.tokLBL, `{"max_bytes":10}`); st != 404 || out["error"] != "not_found" || issuer.calls != 2 {
		t.Fatalf("other user: %d %v calls=%d", st, out, issuer.calls)
	}
	if st, _ := post(fmt.Sprintf(ticketPath, "Kimi.zzc"), v.tokZZC, `{"max_bytes":0}`); st != 400 || issuer.calls != 2 {
		t.Fatalf("bad size: %d calls=%d", st, issuer.calls)
	}
	// capability：新平台不发（页面收 404 即按无 capability 处理）；认人仍在前面
	if st, out := post("/api/console/agents/Kimi.zzc/capability", v.tokZZC, `{"page_id":"p1"}`); st != 404 || out["error"] != "capability_not_used" {
		t.Fatalf("owner capability: %d %v", st, out)
	}
	if st, out := post("/api/console/agents/Kimi.zzc/capability", v.tokLBL, `{"page_id":"p1"}`); st != 404 || out["error"] != "not_found" {
		t.Fatalf("other user capability: %d %v", st, out)
	}
}

// 组长裁定 140013Z ①②：浏览器打开 /view 页（含子路径）与 /console 同一口径 ⇒ 未登录 302 /console/login、
// 必须改密 302 /console/login#change；WebSocket 升级保持 401／403；/api/ 接口不论 Accept 一律不重定向。
func TestNewPlatformPagesRedirectAPIsDoNot(t *testing.T) {
	v := newViewEnv(t)
	_ = v.svc.Store.CreateUser(v.ctx, identity.User{UserID: "temp", ShortName: "tp", State: identity.UserActive, PasswordHash: "x", MustChangePassword: true})
	_ = v.svc.Store.CreateSession(v.ctx, identity.Session{TokenHash: identity.HashSecret("tok-tmp"), UserIDLC: "temp", ExpiresAt: v.svc.Now().Add(identity.SessionTTL)})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /view/{sessionName}", v.hub.RequireConsoleAgent("view", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "page") }))
	mux.HandleFunc("/view/{sessionName}/{rest...}", v.hub.RequireConsoleViewSubPath(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "sub") }))
	mux.HandleFunc("GET /console", v.hub.RequireConsolePrincipal(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "console") }))
	mux.HandleFunc("GET /api/console/agent-groups", v.hub.RequireConsolePrincipal(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "api") }))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	const html = "text/html,application/xhtml+xml"
	cases := []struct {
		name, path, tok, accept string
		ws                      bool
		status                  int
		loc, body               string
	}{
		{"view page, no session", "/view/Kimi.zzc", "", html, false, 302, "/console/login", ""},
		{"view page, must change", "/view/Kimi.zzc", "tok-tmp", html, false, 302, "/console/login#change", ""},
		{"view subpath, no session", "/view/Kimi.zzc/index.html", "", html, false, 302, "/console/login", ""},
		{"view subpath, must change", "/view/Kimi.zzc/index.html", "tok-tmp", html, false, 302, "/console/login#change", ""},
		{"view page json, no session", "/view/Kimi.zzc", "", "application/json", false, 401, "", `{"error":"unauthenticated"}`},
		{"view ws with html accept, no session", "/view/Kimi.zzc", "", html, true, 401, "", `{"error":"unauthenticated"}`},
		{"view ws with html accept, must change", "/view/Kimi.zzc", "tok-tmp", html, true, 403, "", `{"error":"must_change_password"}`},
		{"console page, must change", "/console", "tok-tmp", html, false, 302, "/console/login#change", ""},
		{"api with html accept, no session", "/api/console/agent-groups", "", html, false, 401, "", `{"error":"unauthenticated"}`},
		{"api with html accept, must change", "/api/console/agent-groups", "tok-tmp", html, false, 403, "", `{"error":"must_change_password"}`},
		{"view page, owner", "/view/Kimi.zzc", v.tokZZC, html, false, 200, "", "page"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest("GET", srv.URL+c.path, nil)
		if c.tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: c.tok})
		}
		req.Header.Set("Accept", c.accept)
		if c.ws {
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Origin", npOrigin)
		}
		res, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		body := strings.TrimSpace(string(b))
		if res.StatusCode != c.status || res.Header.Get("Location") != c.loc || (c.body != "" && body != c.body) {
			t.Errorf("%s: %d loc=%q %s, want %d loc=%q %s", c.name, res.StatusCode, res.Header.Get("Location"), body, c.status, c.loc, c.body)
		}
	}
}
