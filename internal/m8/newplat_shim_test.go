package m8

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// R-V2-SPEC-TASK-SHIM-IMPL-20261002164524 身份垫片的测试格（实施方案第四节；编号与方案一致）。
// 夹具：newViewEnv（用户 zzc 与 lbl 均 active；zzc 名下 Opus50TS、Kimi，lbl 名下 kimi；SQLite 旧库；consoleAuthEnabled=false）。

type shimEnv struct {
	*viewEnv
	db  *store.SQLite
	srv *httptest.Server
}

func newShimEnv(t *testing.T) *shimEnv {
	t.Helper()
	v := newViewEnv(t)
	e := &shimEnv{viewEnv: v, db: v.hub.Repo.(*store.SQLite)}
	// 与 cmd/workpulse wireNewPlatform 同样的钩子。
	v.svc.OnRevoked = v.hub.NewPlatformAgentGone
	v.svc.OnSuspended = v.hub.NewPlatformAgentGone
	v.svc.OnUserChanged = v.hub.NewPlatformUserChanged
	h := v.hub
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/specs", h.RequireConsolePrincipal(h.RequireConsolePlatformAction("read", h.HandleConsoleSpecs)))
	mux.HandleFunc("POST /api/console/specs", h.RequireConsolePrincipal(h.RequireConsolePlatformAction("submit", h.HandleConsoleSpecs)))
	mux.HandleFunc("GET /api/console/spec-editors", h.RequireConsolePrincipal(h.HandleConsoleSpecEditors))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/capability", h.RequireConsoleAgent("interact", h.HandleConsoleCapability))
	mux.HandleFunc("POST /api/agent/console/specs", h.RequireConsoleSpecAgentCapability(func(*http.Request) string { return ConsoleAgentCapabilitySpecsNew }, h.HandleConsoleSpecs))
	mux.HandleFunc("POST /api/console/files/{fileID}/archive", h.RequireConsolePrincipal(h.HandleConsoleFileArchive))
	mux.HandleFunc("GET /console/admin/probe", h.RequireConsolePrincipal(h.RequireConsoleManagement(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "door-b") })))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

// do 发请求；tok 为新平台会话 cookie，dwa 为 Agent 专属凭据，capability 为能力令牌。写请求一律带 Origin。
func (e *shimEnv) do(method, path, tok, dwa, capability, body string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("Origin", npOrigin)
	}
	if tok != "" {
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
	}
	if dwa != "" {
		req.Header.Set("Authorization", "Bearer "+dwa)
	}
	if capability != "" {
		req.Header.Set("X-DW-Console-Capability", capability)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(b))
}

// issue 人（tok）对 full_name 的 Agent 签 pageID 的能力令牌；返回状态码与令牌。
func (e *shimEnv) issue(tok, fullName, pageID string) (int, string) {
	e.t.Helper()
	st, body := e.do("POST", "/api/console/agents/"+fullName+"/capability", tok, "", "", `{"page_id":"`+pageID+`"}`)
	var out struct {
		Capability string `json:"capability"`
		PageID     string `json:"page_id"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if st == 200 && out.PageID != pageID {
		e.t.Fatalf("issued page_id=%q, want %q", out.PageID, pageID)
	}
	return st, out.Capability
}

func (e *shimEnv) agentID(owner, name string) string {
	e.t.Helper()
	ag, err := e.svc.Store.FindAgentByName(e.ctx, owner, identity.Lower(name))
	if err != nil {
		e.t.Fatal(err)
	}
	return ag.ID
}

func (e *shimEnv) ownedActive(principal string) map[string]model.ConsoleAgent {
	e.t.Helper()
	agents, err := e.db.ListOwnedConsoleAgents(e.ctx, principal)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]model.ConsoleAgent{}
	for _, a := range agents {
		out[a.ID] = a
	}
	return out
}

func agentSpecBody(t *testing.T, title string) string {
	t.Helper()
	return specBodyClaimingCreator(t, title, "someone-else")
}

// U1 垫片投影：旧四表行与键（principal＝用户、owner 作用域恰一个 u_<用户>、Agent 键 u_<用户>／名称小写、属主 interact）。
func TestShimU1_Projection(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", nil); err != nil {
		t.Fatal(err)
	}
	p, err := e.db.GetConsolePrincipal(e.ctx, "zzc")
	if err != nil || p == nil || p.Provider != store.NewPlatformConsoleProvider || p.DisplayName != "zzc" || !p.Active {
		t.Fatalf("principal: %+v %v", p, err)
	}
	if scope, err := e.db.ResolveConsoleOwnerScope(e.ctx, "zzc"); err != nil || scope != "u_zzc" {
		t.Fatalf("owner scope=%q err=%v, want u_zzc (exactly one)", scope, err)
	}
	if role, _ := e.db.ResolveConsolePlatformRole(e.ctx, "zzc"); role != consolePlatformRoleOwner {
		t.Fatalf("member role=%q, want owner", role)
	}
	owned := e.ownedActive("zzc")
	opus, kimi := e.agentID("zzc", "Opus50TS"), e.agentID("zzc", "Kimi")
	if len(owned) != 2 || owned[opus].KeyID != "u_zzc" || owned[opus].SessionName != "opus50ts" || owned[kimi].SessionName != "kimi" {
		t.Fatalf("owned agents: %+v", owned)
	}
	for _, id := range []string{opus, kimi} {
		if ok, _ := e.db.ConsoleAgentAllowed(e.ctx, "zzc", "u_zzc", owned[id].SessionName, "interact"); !ok {
			t.Fatalf("owner lacks interact grant on %s", id)
		}
	}
	// D4 只属主：lbl 对 zzc 的 Agent 没有授权。
	if err := e.hub.syncNewPlatformUser(e.ctx, "lbl", nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.db.ConsoleAgentAllowed(e.ctx, "lbl", "u_zzc", "kimi", "view"); ok {
		t.Fatal("lbl was granted zzc's Agent")
	}
	// 暂停（只有托管 Agent 能暂停）⇒ 投影 inactive；恢复 ⇒ 下一次同步回 active。
	iss, err := e.svc.IssueHostedAgent(e.ctx, "zzc", "Hosted1", "", "g4")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", nil); err != nil {
		t.Fatal(err)
	}
	if owned := e.ownedActive("zzc"); len(owned) != 3 || owned[iss.Agent.ID].SessionName != "hosted1" {
		t.Fatalf("hosted Agent not projected: %+v", owned)
	}
	if _, err := e.svc.Store.SetAgentSuspended(e.ctx, iss.Agent.ID, true, "zzc", e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", nil); err != nil {
		t.Fatal(err)
	}
	if owned := e.ownedActive("zzc"); len(owned) != 2 || owned[iss.Agent.ID].ID != "" {
		t.Fatalf("after suspend: %+v", owned)
	}
	if _, err := e.svc.Store.SetAgentSuspended(e.ctx, iss.Agent.ID, false, "zzc", e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", nil); err != nil {
		t.Fatal(err)
	}
	if owned := e.ownedActive("zzc"); len(owned) != 3 {
		t.Fatalf("after resume: %+v", owned)
	}
	// 同名 Agent 吊销后重新入网（新 id）：旧行改名腾位、保持停用，新行 active（UNIQUE(key_id,session_name) 不撞）。
	if _, err := e.svc.Revoke(e.ctx, "zzc", opus); err != nil {
		t.Fatal(err)
	}
	_, newOpus := e.enroll("dev-new", "Opus50TS")
	if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", nil); err != nil {
		t.Fatalf("re-enrolled same name: %v", err)
	}
	if owned := e.ownedActive("zzc"); len(owned) != 3 || owned[newOpus].SessionName != "opus50ts" || owned[opus].ID != "" {
		t.Fatalf("after re-enroll: %+v", owned)
	}
}

// U2 启动对账：旧表脏行（吊销没走钩子、用户停用没走钩子）⇒ 对账后全部 inactive；不在新平台的投影主体停用。
func TestShimU2_StartupReconcile(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	kimi := e.agentID("zzc", "Kimi")
	// 绕过钩子直接改新平台（模拟 Hub 重启前发生、钩子没赶上的变化）。
	if _, err := e.svc.Store.Revoke(e.ctx, kimi, "zzc", "zzc", e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Store.SetUserState(e.ctx, "lbl", identity.UserDisabled, e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	// 一个新平台里没有的投影主体（例如旧库里残留的）。
	if _, err := e.db.SyncNewPlatformConsoleIdentity(e.ctx, model.NewPlatformConsoleProjection{PrincipalID: "ghost", Issuer: "dingwei", DisplayName: "gh", OwnerKey: "u_ghost", Active: true, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.ownedActive("zzc")[kimi]; !ok {
		t.Fatal("precondition: dirty row should still be active before reconcile")
	}
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.ownedActive("zzc")[kimi]; ok {
		t.Fatal("revoked Agent still active after reconcile")
	}
	for _, id := range []string{"lbl", "ghost"} {
		if p, _ := e.db.GetConsolePrincipal(e.ctx, id); p == nil || p.Active {
			t.Fatalf("principal %s still active after reconcile: %+v", id, p)
		}
	}
	if p, _ := e.db.GetConsolePrincipal(e.ctx, "zzc"); p == nil || !p.Active {
		t.Fatalf("active user deactivated: %+v", p)
	}
}

// U3 即时吊销：Revoke 后不经任何请求、不经对账，投影 Agent 立即 inactive。
func TestShimU3_RevokeIsImmediate(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	kimi := e.agentID("zzc", "Kimi")
	if _, err := e.svc.Revoke(e.ctx, "zzc", kimi); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.ownedActive("zzc")[kimi]; ok {
		t.Fatal("revoked Agent still active in projection right after Revoke")
	}
}

// U4（Hub 侧）用户停用：投影 inactive、在线 Agent 以 4401 credential_suspended 断开、重连被拒（不删凭据的那个码）。
func TestShimU4_UserDisableDisconnectsAndBlocksReconnect(t *testing.T) {
	e := newShimEnv(t)
	if err := e.svc.Store.CreateUser(e.ctx, identity.User{UserID: "boss", ShortName: "bs", Type: identity.UserManager, State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	boss, _ := e.svc.Store.GetUser(e.ctx, "boss")
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	conn, st, _ := e.dial(e.zKimiCred, "")
	if st != 101 {
		t.Fatalf("dial: %d", st)
	}
	defer conn.CloseNow()
	closed := make(chan error, 1)
	go func() { // 一直读（处理控制帧、吞掉上线指南），直到连接结束
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				closed <- err
				return
			}
		}
	}()
	waitSessionOnline(t, e.hub, "u_zzc", "kimi")
	if err := e.ob.SetUserState(e.ctx, boss, "zzc", identity.UserDisabled); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.db.GetConsolePrincipal(e.ctx, "zzc"); p == nil || p.Active {
		t.Fatalf("principal still active after disable: %+v", p)
	}
	if owned := e.ownedActive("zzc"); len(owned) != 0 {
		t.Fatalf("agents still active after owner disabled: %+v", owned)
	}
	var err error
	select {
	case err = <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("connection not closed within 3s after owner disabled")
	}
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != StatusCredentialRevoked || ce.Reason != "credential_suspended" {
		t.Fatalf("close after owner disabled: %+v err=%v, want 4401 credential_suspended", ce, err)
	}
	if _, st, body := e.dial(e.zKimiCred, ""); st != 401 || !strings.Contains(body, "credential_suspended") {
		t.Fatalf("reconnect while owner disabled: %d %s", st, body)
	}
	// 恢复后凭据照旧可用（停用不是吊销）。
	if err := e.ob.SetUserState(e.ctx, boss, "zzc", identity.UserActive); err != nil {
		t.Fatal(err)
	}
	c2, st, body := e.dial(e.zKimiCred, "")
	if st != 101 {
		t.Fatalf("reconnect after re-enable: %d %s", st, body)
	}
	c2.CloseNow()
}

// U5 显示名撞名：新平台简称撞上旧主体的 display_name（UNIQUE）⇒ 投影名「简称·用户ID」，其余用户照常投影，对账不报错。
func TestShimU5_DisplayNameCollision(t *testing.T) {
	e := newShimEnv(t)
	if err := e.db.UpsertConsolePrincipal(e.ctx, model.ConsolePrincipal{ID: "legacy-lb", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ou_x", DisplayName: "lb", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatalf("reconcile with a colliding legacy name: %v", err)
	}
	if p, _ := e.db.GetConsolePrincipal(e.ctx, "lbl"); p == nil || p.DisplayName != "lb·lbl" || !p.Active {
		t.Fatalf("colliding user projection: %+v", p)
	}
	if p, _ := e.db.GetConsolePrincipal(e.ctx, "zzc"); p == nil || p.DisplayName != "zzc" {
		t.Fatalf("other user projection: %+v", p)
	}
	if p, _ := e.db.GetConsolePrincipal(e.ctx, "legacy-lb"); p == nil || p.DisplayName != "lb" || p.Provider != "feishu" {
		t.Fatalf("legacy principal touched: %+v", p)
	}
}

// H17 签发口新平台分支：本人对本人 Agent 签三类页面 id ⇒ 200；他人 Agent、终端页 id、未登录 ⇒ 拒。
func TestShimH17_CapabilityIssuance(t *testing.T) {
	e := newShimEnv(t)
	for _, page := range []string{ConsoleAgentCapabilitySpecsNew, ConsoleAgentCapabilityTasks, ConsoleAgentSpecCapabilityPage("spec-1")} {
		if st, raw := e.issue(e.tokZZC, "Kimi.zzc", page); st != 200 || raw == "" {
			t.Errorf("owner signs %s: %d %q", page, st, raw)
		}
	}
	if st, _ := e.issue(e.tokZZC, "kimi.lb", ConsoleAgentCapabilitySpecsNew); st != 404 {
		t.Errorf("sign for another user's Agent: %d, want 404", st)
	}
	if st, _ := e.issue(e.tokZZC, "Kimi.zzc", "view-page-random-id"); st != 404 {
		t.Errorf("terminal page id: %d, want 404 (view write stays on ＃unlock)", st)
	}
	if st, _ := e.issue("", "Kimi.zzc", ConsoleAgentCapabilitySpecsNew); st != 401 {
		t.Errorf("no session: %d, want 401", st)
	}
	// 暂停的 Agent 不签（存储在同一事务里核 Agent active；只有托管 Agent 能暂停）。
	iss, err := e.svc.IssueHostedAgent(e.ctx, "zzc", "Hosted1", "", "g4")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := e.issue(e.tokZZC, "Hosted1.zzc", ConsoleAgentCapabilitySpecsNew); st != 200 {
		t.Errorf("active hosted Agent: %d, want 200", st)
	}
	if _, err := e.svc.Store.SetAgentSuspended(e.ctx, iss.Agent.ID, true, "zzc", e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.issue(e.tokZZC, "Hosted1.zzc", ConsoleAgentCapabilitySpecsNew); st != 403 {
		t.Errorf("suspended Agent: %d, want 403", st)
	}
}

// H4 Agent 侧：dwa 凭据＋真签发口签出的令牌 ⇒ 建 spec 成功、署名为该 Agent；consoleAuthEnabled=false 仍通（夹具即如此）。
// 拒：无令牌；旧 service key＋旧头；他人 Agent 的凭据拿本人令牌；令牌签给别的页面。
func TestShimH4_AgentCreatesSpecWithCredentialAndCapability(t *testing.T) {
	e := newShimEnv(t)
	if e.hub.consoleAuthEnabled {
		t.Fatal("precondition: consoleAuthEnabled must be false in this cell")
	}
	st, capNew := e.issue(e.tokZZC, "Kimi.zzc", ConsoleAgentCapabilitySpecsNew)
	if st != 200 {
		t.Fatalf("issue: %d", st)
	}
	if st, body := e.do("POST", "/api/agent/console/specs", "", e.zKimiCred, "", agentSpecBody(t, "no cap")); st != 403 {
		t.Errorf("no capability: %d %s", st, body)
	}
	secret, key, err := e.hub.IssueAPIKey(e.ctx, "session-helper", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/agent/console/specs", strings.NewReader(agentSpecBody(t, "legacy")))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-DW-Agent-Key-ID", key.ID)
	req.Header.Set("X-DW-Agent-Session", "kimi")
	req.Header.Set("X-DW-Console-Capability", capNew)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 403 {
		t.Errorf("legacy service key + headers: %v %v, want 403", res.StatusCode, err)
	}
	if st, body := e.do("POST", "/api/agent/console/specs", "", e.lKimiCr, capNew, agentSpecBody(t, "other cred")); st != 403 {
		t.Errorf("another user's Agent credential with owner's capability: %d %s", st, body)
	}
	_, capTasks := e.issue(e.tokZZC, "Kimi.zzc", ConsoleAgentCapabilityTasks)
	if st, body := e.do("POST", "/api/agent/console/specs", "", e.zKimiCred, capTasks, agentSpecBody(t, "wrong page")); st != 403 {
		t.Errorf("capability for another page: %d %s", st, body)
	}
	st, body := e.do("POST", "/api/agent/console/specs", "", e.zKimiCred, capNew, agentSpecBody(t, "by agent"))
	if st != 201 {
		t.Fatalf("Agent create: %d %s", st, body)
	}
	var spec model.ConsoleSpec
	_ = json.Unmarshal([]byte(body), &spec)
	rev, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", spec.ID, 1, false)
	owned, oerr := e.db.ConsoleSpec(e.ctx, "zzc", spec.ID, false)
	if err != nil || oerr != nil || owned == nil || owned.PrincipalID != "zzc" || rev.CreatedBy != e.agentID("zzc", "Kimi") || rev.CreatedByKind != "agent" {
		t.Fatalf("spec %+v revision %+v err=%v", spec, rev, err)
	}
}

// H5／H14 令牌随人失效、不等对账：口令被重置（会话吊销、用户仍 active）⇒ 先前签的令牌拒；
// 属主被停用 ⇒ Agent 侧请求立即拒（凭据现查属主）。
func TestShimH5H14_CapabilityDiesWithSessionAndOwner(t *testing.T) {
	e := newShimEnv(t)
	_, capNew := e.issue(e.tokZZC, "Kimi.zzc", ConsoleAgentCapabilitySpecsNew)
	if err := e.svc.Store.ResetPassword(e.ctx, "zzc", "x", e.svc.Now()); err != nil { // 吊销 zzc 全部会话；用户仍 active
		t.Fatal(err)
	}
	if st, body := e.do("POST", "/api/agent/console/specs", "", e.zKimiCred, capNew, agentSpecBody(t, "after reset")); st != 403 {
		t.Errorf("capability after owner's session revoked: %d %s", st, body)
	}
	// 新会话、新令牌 ⇒ 通；随后停用属主 ⇒ 同一令牌立即拒。
	if err := e.svc.Store.CreateSession(e.ctx, identity.Session{TokenHash: identity.HashSecret("tok-zzc-2"), UserIDLC: "zzc", ExpiresAt: e.svc.Now().Add(identity.SessionTTL)}); err != nil {
		t.Fatal(err)
	}
	_ = e.svc.Store.SetPassword(e.ctx, "zzc", "", false)
	_, cap2 := e.issue("tok-zzc-2", "Kimi.zzc", ConsoleAgentCapabilitySpecsNew)
	if st, body := e.do("POST", "/api/agent/console/specs", "", e.zKimiCred, cap2, agentSpecBody(t, "ok")); st != 201 {
		t.Fatalf("fresh capability: %d %s", st, body)
	}
	_, cap3 := e.issue("tok-zzc-2", "Kimi.zzc", ConsoleAgentCapabilitySpecsNew)
	if err := e.svc.Store.CreateUser(e.ctx, identity.User{UserID: "boss", ShortName: "bs", Type: identity.UserManager, State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	boss, _ := e.svc.Store.GetUser(e.ctx, "boss")
	if err := e.ob.SetUserState(e.ctx, boss, "zzc", identity.UserDisabled); err != nil {
		t.Fatal(err)
	}
	if st, body := e.do("POST", "/api/agent/console/specs", "", e.zKimiCred, cap3, agentSpecBody(t, "after disable")); st != 403 {
		t.Errorf("owner disabled: %d %s", st, body)
	}
}

// H3 修改人候选：只列新平台用户、显示简称、不下发 u_ 作用域；停用者不在；旧主体不在。
func TestShimH3_SpecEditors(t *testing.T) {
	e := newShimEnv(t)
	if err := e.db.UpsertConsolePrincipal(e.ctx, model.ConsolePrincipal{ID: "legacy", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ou_l", DisplayName: "旧人", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.db.UpsertConsolePrincipalRole(e.ctx, model.ConsolePrincipalRole{PrincipalID: "legacy", Role: "owner", OwnerKey: "tenant", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	editors := func() string {
		st, body := e.do("GET", "/api/console/spec-editors", e.tokZZC, "", "", "")
		if st != 200 {
			t.Fatalf("editors: %d %s", st, body)
		}
		var out struct {
			Editors []map[string]string `json:"editors"`
		}
		_ = json.Unmarshal([]byte(body), &out)
		var names []string
		for _, ed := range out.Editors {
			if ed["short_name"] != "" {
				t.Errorf("short_name leaked: %v", ed)
			}
			names = append(names, ed["value"])
		}
		return strings.Join(names, ",")
	}
	if got := editors(); got != "lb,zzc" {
		t.Fatalf("editors=%q, want lb,zzc", got)
	}
	if err := e.svc.Store.SetUserState(e.ctx, "lbl", identity.UserDisabled, e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := editors(); got != "zzc" {
		t.Fatalf("after lbl disabled editors=%q, want zzc", got)
	}
}

// H10 跨属主隔离：B 看不到 A 的 spec；B 的 Agent 用不了 A 签的令牌（H4 已钉）；B 不能给 A 的 Agent 签令牌（H17 已钉）。
func TestShimH10_CrossOwnerIsolation(t *testing.T) {
	e := newShimEnv(t)
	if st, body := e.do("POST", "/api/console/specs", e.tokZZC, "", "", agentSpecBody(t, "zzc-secret-spec")); st != 201 {
		t.Fatalf("zzc create: %d %s", st, body)
	}
	_, mine := e.do("GET", "/api/console/specs", e.tokZZC, "", "", "")
	_, theirs := e.do("GET", "/api/console/specs", e.tokLBL, "", "", "")
	if !strings.Contains(mine, "zzc-secret-spec") || strings.Contains(theirs, "zzc-secret-spec") {
		t.Fatalf("isolation: zzc sees=%v lbl sees=%v", strings.Contains(mine, "zzc-secret-spec"), strings.Contains(theirs, "zzc-secret-spec"))
	}
}

// H12 文件归档（C15）：新平台登录用户归档 ⇒ 到达归档客户端，属主作用域 u_<用户>、操作者＝用户；未登录 401。
func TestShimH12_FileArchive(t *testing.T) {
	e := newShimEnv(t)
	var got []FileArchiveRequest
	e.hub.SetFileArchiveClient(fileArchiveFunc(func(_ context.Context, req FileArchiveRequest) error {
		got = append(got, req)
		if req.OwnerKey != "u_zzc" {
			return errors.New("not your file")
		}
		return nil
	}))
	body := `{"archived":true,"reason":"done"}`
	if st, b := e.do("POST", "/api/console/files/0123456789abcdef0123456789abcdef/archive", e.tokZZC, "", "", body); st != 204 {
		t.Fatalf("owner archive: %d %s", st, b)
	}
	if len(got) != 1 || got[0].OwnerKey != "u_zzc" || got[0].ActorPrincipal != "zzc" {
		t.Fatalf("archive request: %+v", got)
	}
	if st, _ := e.do("POST", "/api/console/files/0123456789abcdef0123456789abcdef/archive", e.tokLBL, "", "", body); st != 403 {
		t.Fatalf("other user's file: %d, want 403", st)
	}
	if st, _ := e.do("POST", "/api/console/files/0123456789abcdef0123456789abcdef/archive", "", "", "", body); st != 401 {
		t.Fatalf("no session: %d, want 401", st)
	}
}

type fileArchiveFunc func(context.Context, FileArchiveRequest) error

func (f fileArchiveFunc) SetFileArchived(ctx context.Context, req FileArchiveRequest) error {
	return f(ctx, req)
}

// H13 门 B（C16，组长 16:55:17Z）：新平台管理用户过门；普通用户 403；降级后即 403；旧模式未开 auth 仍 404。
func TestShimH13_DoorB(t *testing.T) {
	e := newShimEnv(t)
	if err := e.svc.Store.CreateUser(e.ctx, identity.User{UserID: "boss", ShortName: "bs", Type: identity.UserManager, State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Store.CreateSession(e.ctx, identity.Session{TokenHash: identity.HashSecret("tok-boss"), UserIDLC: "boss", ExpiresAt: e.svc.Now().Add(identity.SessionTTL)}); err != nil {
		t.Fatal(err)
	}
	if st, body := e.do("GET", "/console/admin/probe", "tok-boss", "", "", ""); st != 200 || body != "door-b" {
		t.Errorf("manager through door B: %d %s", st, body)
	}
	if st, _ := e.do("GET", "/console/admin/probe", e.tokZZC, "", "", ""); st != 403 {
		t.Errorf("member at door B: %d, want 403", st)
	}
	if role, _ := e.db.ResolveConsolePlatformRole(e.ctx, "boss"); role != consolePlatformRoleManagement {
		t.Errorf("projected role=%q, want management", role)
	}
	if err := e.svc.Store.SetUserType(e.ctx, "boss", identity.UserMember); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.do("GET", "/console/admin/probe", "tok-boss", "", "", ""); st != 403 {
		t.Errorf("demoted manager at door B: %d, want 403", st)
	}
	// 旧模式（无 Identity）、未开 auth：原样 404。
	old, _, _ := newTestHub(t)
	rr := httptest.NewRecorder()
	old.RequireConsoleManagement(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "door-b") })(rr, httptest.NewRequest("GET", "/console/admin/probe", nil))
	if rr.Code != 404 {
		t.Errorf("legacy mode, auth off: %d, want 404", rr.Code)
	}
}

// failingShim 包住真实存储，写投影失败（H15）。
type failingShim struct{ newPlatformShimRepository }

func (failingShim) SyncNewPlatformConsoleIdentity(context.Context, model.NewPlatformConsoleProjection) (string, error) {
	return "", errors.New("injected projection failure")
}

// H15 同步失败闭合：写投影失败 ⇒ 人侧与 Agent 侧请求都 503，不放行。
func TestShimH15_SyncFailureFailsClosed(t *testing.T) {
	e := newShimEnv(t)
	_, capNew := e.issue(e.tokZZC, "Kimi.zzc", ConsoleAgentCapabilitySpecsNew)
	e.hub.newPlatformShimRepo = failingShim{e.db}
	if st, body := e.do("GET", "/api/console/specs", e.tokZZC, "", "", ""); st != 503 {
		t.Errorf("console request with failing shim: %d %s", st, body)
	}
	if st, body := e.do("POST", "/api/agent/console/specs", "", e.zKimiCred, capNew, agentSpecBody(t, "x")); st != 503 {
		t.Errorf("Agent request with failing shim: %d %s", st, body)
	}
	if st, _ := e.issue(e.tokZZC, "Kimi.zzc", ConsoleAgentCapabilitySpecsNew); st != 503 {
		t.Errorf("capability issue with failing shim: %d", st)
	}
}

// H16 并发同步：同一属主 32 路并发同步幂等，行数不重复、不报错（SQLite 单连接串行；-race 下跑）。
func TestShimH16_ConcurrentSync(t *testing.T) {
	e := newShimEnv(t)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- e.hub.syncNewPlatformUser(context.Background(), "zzc", nil)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if owned := e.ownedActive("zzc"); len(owned) != 2 {
		t.Fatalf("owned after concurrent sync: %d, want 2", len(owned))
	}
	if scope, err := e.db.ResolveConsoleOwnerScope(e.ctx, "zzc"); err != nil || scope != "u_zzc" {
		t.Fatalf("owner scope after concurrent sync: %q %v", scope, err)
	}
}
