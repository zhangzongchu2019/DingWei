package m8

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// agentEnv 一个启用了 /ws/agent 的 Hub：内存身份存储＋真 HTTP＋真 WS。
type agentEnv struct {
	t    *testing.T
	ctx  context.Context
	hub  *Hub
	svc  *identity.Service
	srv  *httptest.Server
	cred string // 已批准 Agent "Opus50TS"（归 zzc）的专属凭据
	agID string
}

func newAgentEnv(t *testing.T) *agentEnv {
	t.Helper()
	hub, _, ctx := newTestHub(t)
	store := identity.NewMemStore()
	if err := store.CreateUser(ctx, identity.User{UserID: "zzc", ShortName: "zzc", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	svc := identity.NewService(store, nil, nil)
	svc.OnRevoked = hub.DisconnectAgent
	svc.NameAllowed = func(n string) bool { return !isReservedForNaming(n) }
	hub.Identity = svc
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", hub.HandleAgentWS)
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := &agentEnv{t: t, ctx: ctx, hub: hub, svc: svc, srv: srv}
	e.cred, e.agID = e.enroll("dev-1", "Opus50TS")
	return e
}

// enroll 走一遍申请→批准→取凭据（业务层，HTTP 层另有用例覆盖）。
func (e *agentEnv) enroll(device, name string) (cred, agentID string) {
	e.t.Helper()
	digest, _ := identity.PairingDigest("000102030405060708090a0b0c0d0e0f", "123456")
	r, err := e.svc.Apply(e.ctx, identity.ApplyRequest{OwnerUserID: "zzc", PairingSalt: "000102030405060708090a0b0c0d0e0f",
		PairingDigest: digest, DeviceFingerprint: device, ClientVersion: "0.3.0", SourceIP: "127.0.0.1"})
	if err != nil {
		e.t.Fatal(err)
	}
	ag, _, err := e.svc.Approve(e.ctx, "zzc", r.ApplicationID, "123456", name, "g1")
	if err != nil {
		e.t.Fatal(err)
	}
	p, err := e.svc.Poll(e.ctx, r.ApplicationID, r.PollToken)
	if err != nil || p.Credential == "" {
		e.t.Fatalf("poll: %+v %v", p, err)
	}
	return p.Credential, ag.ID
}

func (e *agentEnv) wsURL(query string) string {
	u := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/ws/agent"
	if query != "" {
		u += "?" + query
	}
	return u
}

func (e *agentEnv) dial(cred, query string) (*websocket.Conn, int, string) {
	e.t.Helper()
	h := http.Header{}
	if cred != "" {
		h.Set("Authorization", "Bearer "+cred)
	}
	conn, resp, err := websocket.Dial(e.ctx, e.wsURL(query), &websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		return conn, http.StatusSwitchingProtocols, ""
	}
	if resp == nil {
		e.t.Fatalf("dial: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return nil, resp.StatusCode, strings.TrimSpace(string(b))
}

// reader 持续读一条连接（处理控制帧），返回一个在连接结束时收到关闭码的通道。
func reader(conn *websocket.Conn) <-chan websocket.StatusCode {
	done := make(chan websocket.StatusCode, 1)
	go func() {
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				done <- websocket.CloseStatus(err)
				return
			}
		}
	}()
	return done
}

func TestAgentWS_IdentityFromCredentialNotFromURL(t *testing.T) {
	e := newAgentEnv(t)
	conn, st, body := e.dial(e.cred, "name=evil&key_id=k_evil&version=0.3.0&tool=claude&os=linux")
	if conn == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	reader(conn)
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	if e.hub.sessionOnline("k_evil", "evil") || e.hub.sessionOnline("u_zzc", "evil") {
		t.Fatal("self-reported name/key_id in URL must be ignored")
	}
	if !e.hub.AgentOnline(e.agID) {
		t.Fatal("AgentOnline=false for connected agent")
	}
	e.hub.mu.Lock()
	c := e.hub.sessionClients["u_zzc"]["opus50ts"]
	e.hub.mu.Unlock()
	if c.ownerKey != "u_zzc" || c.tool != "claude" || c.version != "0.3.0" || c.osName != "linux" {
		t.Fatalf("client fields: owner=%q tool=%q version=%q os=%q", c.ownerKey, c.tool, c.version, c.osName)
	}
}

func TestAgentWS_HandshakeErrors(t *testing.T) {
	e := newAgentEnv(t)
	for name, cred := range map[string]string{"no header": "", "unknown credential": "dwa_" + strings.Repeat("0", 64)} {
		if _, st, body := e.dial(cred, ""); st != 401 || body != `{"error":"credential_invalid"}` {
			t.Errorf("%s: %d %s", name, st, body)
		}
	}
	if _, err := e.svc.Revoke(e.ctx, "zzc", e.agID); err != nil {
		t.Fatal(err)
	}
	if _, st, body := e.dial(e.cred, ""); st != 401 || body != `{"error":"credential_revoked"}` {
		t.Fatalf("revoked: %d %s", st, body)
	}
}

// V1 A5：另一个连接拿同一凭据连入 ⇒ 被拒，已在线的不掉线。
func TestAgentWS_SecondConnectionRejectedWhileAlive(t *testing.T) {
	e := newAgentEnv(t)
	conn1, _, _ := e.dial(e.cred, "")
	defer conn1.Close(websocket.StatusNormalClosure, "done")
	closed1 := reader(conn1)
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	if _, st, body := e.dial(e.cred, ""); st != 409 || body != `{"error":"already_connected"}` {
		t.Fatalf("second: %d %s", st, body)
	}
	select {
	case code := <-closed1:
		t.Fatalf("first connection was dropped (close=%d)", code)
	case <-time.After(300 * time.Millisecond):
	}
	if !e.hub.sessionOnline("u_zzc", "opus50ts") {
		t.Fatal("first connection no longer registered")
	}
}

// 断网重连：旧连接已死（不回 pong）⇒ 探活超时后换上新连接，不会把自己卡死。
func TestAgentWS_DeadOldConnectionReplaced(t *testing.T) {
	old := agentLivenessProbe
	agentLivenessProbe = 300 * time.Millisecond
	t.Cleanup(func() { agentLivenessProbe = old })
	e := newAgentEnv(t)
	conn1, _, _ := e.dial(e.cred, "") // 故意不读：模拟已死的对端，收不到 pong
	defer conn1.CloseNow()
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	e.hub.mu.Lock()
	first := e.hub.sessionClients["u_zzc"]["opus50ts"]
	e.hub.mu.Unlock()
	conn2, st, body := e.dial(e.cred, "")
	if conn2 == nil {
		t.Fatalf("reconnect after dead peer: %d %s", st, body)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "done")
	reader(conn2)
	deadline := time.Now().Add(3 * time.Second)
	for {
		e.hub.mu.Lock()
		cur := e.hub.sessionClients["u_zzc"]["opus50ts"]
		e.hub.mu.Unlock()
		if cur != nil && cur != first {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("new connection did not replace the dead one")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// V2 A1：吊销返回时在线连接已以 4401 断开。
func TestAgentWS_RevokeDisconnectsWith4401(t *testing.T) {
	e := newAgentEnv(t)
	conn, _, _ := e.dial(e.cred, "")
	defer conn.CloseNow()
	closed := reader(conn)
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	if already, err := e.svc.Revoke(e.ctx, "zzc", e.agID); already || err != nil {
		t.Fatalf("revoke: %v %v", already, err)
	}
	select {
	case code := <-closed:
		if code != StatusCredentialRevoked {
			t.Fatalf("close code=%d, want 4401", code)
		}
	case <-time.After(time.Second):
		t.Fatal("connection still open after revoke returned")
	}
	if e.hub.AgentOnline(e.agID) {
		t.Fatal("AgentOnline=true after revoke")
	}
}

// 吊销一个 Agent 不牵连同用户的另一个（V2 A9）。
func TestAgentWS_RevokeDoesNotAffectSibling(t *testing.T) {
	e := newAgentEnv(t)
	cred2, _ := e.enroll("dev-2", "Kimi")
	c1, _, _ := e.dial(e.cred, "")
	defer c1.CloseNow()
	c2, _, _ := e.dial(cred2, "")
	defer c2.Close(websocket.StatusNormalClosure, "done")
	reader(c1)
	closed2 := reader(c2)
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	waitSessionOnline(t, e.hub, "u_zzc", "kimi")
	if _, err := e.svc.Revoke(e.ctx, "zzc", e.agID); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-closed2:
		t.Fatalf("sibling dropped (close=%d)", code)
	case <-time.After(300 * time.Millisecond):
	}
	if !e.hub.sessionOnline("u_zzc", "kimi") {
		t.Fatal("sibling offline")
	}
}

func TestAgentWS_ReservedNameRejectedAtApproval(t *testing.T) {
	e := newAgentEnv(t)
	digest, _ := identity.PairingDigest("000102030405060708090a0b0c0d0e0f", "123456")
	r, _ := e.svc.Apply(e.ctx, identity.ApplyRequest{OwnerUserID: "zzc", PairingSalt: "000102030405060708090a0b0c0d0e0f",
		PairingDigest: digest, DeviceFingerprint: "dev-x", SourceIP: "127.0.0.1"})
	if _, _, err := e.svc.Approve(e.ctx, "zzc", r.ApplicationID, "123456", "Feishu", "g"); !errors.Is(err, identity.ErrNameInvalid) {
		t.Fatalf("reserved channel word approved: %v", err)
	}
}

func TestSessionWS_GoneWhenCredentialOnly(t *testing.T) {
	e := newAgentEnv(t)
	e.hub.AgentCredentialOnly = true
	h := http.Header{}
	h.Set("Authorization", "Bearer whatever")
	_, resp, err := websocket.Dial(e.ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/ws/session/x?key_id=k", &websocket.DialOptions{HTTPHeader: h})
	if err == nil || resp == nil {
		t.Fatalf("old entry should be refused: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusGone || strings.TrimSpace(string(b)) != `{"error":"use_ws_agent"}` {
		t.Fatalf("old entry: %d %s", resp.StatusCode, b)
	}
}

func TestIsInternalSource(t *testing.T) {
	cases := []struct {
		remote, xff, real string
		want              bool
	}{
		{"127.0.0.1:5000", "", "", true},
		{"10.1.2.3:5000", "", "", true},
		{"192.168.0.9:5000", "172.16.0.4", "", true},
		{"203.0.113.9:5000", "", "", false},                    // 公网直连
		{"127.0.0.1:5000", "203.0.113.9", "", false},           // 经 nginx 的公网请求
		{"127.0.0.1:5000", "10.0.0.1, 203.0.113.9", "", false}, // 客户端先塞一个内网地址
		{"127.0.0.1:5000", "", "198.51.100.7", false},
		{"127.0.0.1:5000", "not-an-ip", "", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/ws/service/x", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.real != "" {
			r.Header.Set("X-Real-IP", c.real)
		}
		if got := isInternalSource(r); got != c.want {
			t.Errorf("remote=%s xff=%q real=%q: got %v want %v", c.remote, c.xff, c.real, got, c.want)
		}
	}
}

func TestServiceWS_InternalOnlyInNewPlatformMode(t *testing.T) {
	e := newAgentEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", e.hub.HandleWS)
	call := func(xff string) (int, string) {
		r := httptest.NewRequest("GET", "/ws/service/svc1?api_key=nope", nil)
		r.RemoteAddr = "127.0.0.1:4000"
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if code, _ := call("203.0.113.9"); code != 401 {
		t.Fatalf("legacy mode must not add the source gate: %d", code)
	}
	e.hub.AgentCredentialOnly = true
	if code, body := call("203.0.113.9"); code != 403 || body != `{"error":"internal_only"}` {
		t.Fatalf("public source in new-platform mode: %d %s", code, body)
	}
	if code, _ := call(""); code != 401 { // 内网来源照常走服务 key 鉴权（这里 key 是假的 ⇒ 401）
		t.Fatalf("internal source should reach key auth: %d", code)
	}
}
