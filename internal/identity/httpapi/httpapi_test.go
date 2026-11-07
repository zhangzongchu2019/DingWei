package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
)

const (
	origin     = "https://dw.cmjrt.com:55055"
	saltHex    = "000102030405060708090a0b0c0d0e0f"
	code       = "123456"
	codeDigest = "99c90773ea9ce66a355bd6a2a6e55122c3caf5a164785c90b1d58c2d63bb8680"
)

type env struct {
	t     *testing.T
	clk   *clock.Fake
	store *identity.MemStore
	svc   *identity.Service
	api   *httpapi.API
	srv   *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, clk: &clock.Fake{T: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}, store: identity.NewMemStore()}
	for _, u := range []identity.User{
		{UserID: "zzc", ShortName: "zzc", State: identity.UserActive},
		{UserID: "lbl", ShortName: "lb", State: identity.UserActive},
	} {
		if err := e.store.CreateUser(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	e.svc = identity.NewService(e.store, e.clk.Now, nil)
	e.api = &httpapi.API{
		Svc:          e.svc,
		PublicOrigin: origin,
		// 测试桩：用请求头冒充 console 会话。生产由 Hub 注入真会话解析。
		Actor: func(r *http.Request) (string, error) {
			if a := r.Header.Get("X-Test-Actor"); a != "" {
				return a, nil
			}
			return "", identity.ErrNotFound
		},
	}
	mux := http.NewServeMux()
	e.api.Register(mux)
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
}

func (e *env) do(method, path string, body any, hdr map[string]string) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	out := resp{status: res.StatusCode, header: res.Header, body: map[string]any{}}
	_ = json.NewDecoder(res.Body).Decode(&out.body)
	return out
}

func owner(actor string) map[string]string {
	return map[string]string{"X-Test-Actor": actor, "Origin": origin}
}

func keys(m map[string]any) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

func applyBody(ownerID string) map[string]any {
	return map[string]any{"owner_user_id": ownerID, "pairing_salt": saltHex, "pairing_digest": codeDigest,
		"device_fingerprint": "fp-0123456789abcdef", "client_version": "0.3.0"}
}

// 字段名逐字对齐客户端 tosonnet50ts/g3-enroll-client dbfc9f14（设计件 2.1、2.2）。
func TestClientContractFieldNames(t *testing.T) {
	e := newEnv(t)
	r := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil)
	if r.status != 201 || keys(r.body) != "application_id,expires_at,poll_token" {
		t.Fatalf("apply: %d %v", r.status, r.body)
	}
	if r.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("apply must be no-store, got %q", r.header.Get("Cache-Control"))
	}
	if r.body["expires_at"] != "2026-10-02T09:00:00Z" {
		t.Fatalf("expires_at=%v", r.body["expires_at"])
	}
	id, tok := r.body["application_id"].(string), r.body["poll_token"].(string)
	p := e.do("GET", "/api/enroll/applications/"+id, nil, map[string]string{"Authorization": "Bearer " + tok})
	if p.status != 200 || keys(p.body) != "state" || p.body["state"] != "pending" {
		t.Fatalf("poll pending: %d %v", p.status, p.body)
	}
}

func TestFullFlowOverHTTP(t *testing.T) {
	e := newEnv(t)
	a := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil)
	id, tok := a.body["application_id"].(string), a.body["poll_token"].(string)
	auth := map[string]string{"Authorization": "Bearer " + tok}

	l := e.do("GET", "/api/console/enroll/applications", nil, map[string]string{"X-Test-Actor": "zzc"})
	apps, _ := l.body["applications"].([]any)
	if l.status != 200 || len(apps) != 1 {
		t.Fatalf("V1 A1 list: %d %v", l.status, l.body)
	}
	row := apps[0].(map[string]any)
	if row["application_id"] != id || row["source_ip"] != "127.0.0.1" || row["device_fingerprint"] != "fp-012345678" ||
		row["requested_at"] != "2026-10-01T09:00:00Z" || row["same_device_count"] != float64(1) {
		t.Fatalf("V1 A1 row: %v", row)
	}
	if l2 := e.do("GET", "/api/console/enroll/applications", nil, map[string]string{"X-Test-Actor": "lbl"}); len(l2.body["applications"].([]any)) != 0 {
		t.Fatalf("V1 A6: lbl sees %v", l2.body)
	}

	bad := e.do("POST", "/api/console/enroll/applications/"+id+"/approve",
		map[string]any{"pairing_code": "999999", "agent_name": "Opus50ts", "new_group_name": tg()}, owner("zzc"))
	if bad.status != 422 || bad.body["error"] != "pairing_mismatch" || bad.body["remaining"] != float64(4) {
		t.Fatalf("V1 A2 wrong code: %d %v", bad.status, bad.body)
	}
	ok := e.do("POST", "/api/console/enroll/applications/"+id+"/approve",
		map[string]any{"pairing_code": code, "agent_name": "Opus50ts", "new_group_name": tg()}, owner("zzc"))
	if ok.status != 200 || ok.body["agent_full_name"] != "Opus50ts.zzc" {
		t.Fatalf("approve: %d %v", ok.status, ok.body)
	}
	agentID := ok.body["agent_id"].(string)

	e.clk.Advance(identity.PollMinInterval)
	p := e.do("GET", "/api/enroll/applications/"+id, nil, auth)
	if p.status != 200 || keys(p.body) != "agent_full_name,agent_name,credential,state" || p.body["state"] != "approved" ||
		p.body["agent_name"] != "Opus50ts" || !strings.HasPrefix(p.body["credential"].(string), "dwa_") {
		t.Fatalf("poll approved: %d %v", p.status, p.body)
	}
	cred := p.body["credential"].(string)
	e.clk.Advance(identity.PollMinInterval)
	if p2 := e.do("GET", "/api/enroll/applications/"+id, nil, auth); p2.body["credential"] != "" {
		t.Fatalf("credential returned twice: %v", p2.body)
	}

	ag := e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": "zzc"})
	list := ag.body["agents"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["full_name"] != "Opus50ts.zzc" || list[0].(map[string]any)["origin"] != "selfhost" ||
		list[0].(map[string]any)["session"] != "opus50ts" || list[0].(map[string]any)["state"] != "active" {
		t.Fatalf("agents: %v", ag.body)
	}
	if rv := e.do("POST", "/api/console/agents/"+agentID+"/revoke", nil, owner("lbl")); rv.status != 403 || rv.body["error"] != "forbidden" {
		t.Fatalf("V2 A6: %d %v", rv.status, rv.body)
	}
	rv := e.do("POST", "/api/console/agents/"+agentID+"/revoke", nil, owner("zzc"))
	if rv.status != 200 || rv.body["revoked_at"] != "2026-10-01T09:00:04Z" {
		t.Fatalf("revoke: %d %v", rv.status, rv.body)
	}
	if rv2 := e.do("POST", "/api/console/agents/"+agentID+"/revoke", nil, owner("zzc")); rv2.status != 200 || rv2.body["already_revoked"] != true {
		t.Fatalf("V2 A10: %d %v", rv2.status, rv2.body)
	}
	if _, _, err := e.svc.AuthenticateAgent(context.Background(), cred); err != identity.ErrCredentialRevoked {
		t.Fatalf("V2 A2: %v", err)
	}
	if ag2 := e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": "zzc"}); len(ag2.body["agents"].([]any)) != 0 {
		t.Fatalf("V2 A3: revoked agent still listed: %v", ag2.body)
	}
}

func TestPollDoesNotLeakExistence(t *testing.T) {
	e := newEnv(t)
	a := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil)
	id := a.body["application_id"].(string)
	cases := map[string]map[string]string{
		"no header":        nil,
		"wrong token":      {"Authorization": "Bearer nope"},
		"basic not bearer": {"Authorization": "Basic " + a.body["poll_token"].(string)},
	}
	for name, h := range cases {
		r := e.do("GET", "/api/enroll/applications/"+id, nil, h)
		if r.status != 404 || keys(r.body) != "error" || r.body["error"] != "not_found" {
			t.Errorf("%s: %d %v", name, r.status, r.body)
		}
	}
	r := e.do("GET", "/api/enroll/applications/doesnotexist", nil, map[string]string{"Authorization": "Bearer nope"})
	if r.status != 404 || r.body["error"] != "not_found" {
		t.Fatalf("unknown id: %d %v", r.status, r.body)
	}
}

func TestConsoleAuthAndOrigin(t *testing.T) {
	e := newEnv(t)
	if r := e.do("GET", "/api/console/agents", nil, nil); r.status != 401 || r.body["error"] != "unauthenticated" {
		t.Fatalf("no session: %d %v", r.status, r.body)
	}
	if r := e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": "zzc"}); r.status != 200 {
		t.Fatalf("GET without Origin should pass: %d", r.status)
	}
	for name, h := range map[string]map[string]string{
		"no origin":    {"X-Test-Actor": "zzc"},
		"wrong origin": {"X-Test-Actor": "zzc", "Origin": "https://evil.example"},
	} {
		r := e.do("POST", "/api/console/agents/x/revoke", nil, h)
		if r.status != 403 || r.body["error"] != "origin_mismatch" {
			t.Errorf("%s: %d %v", name, r.status, r.body)
		}
	}
}

func TestConsoleWriteUsesTrustedOriginPolicy(t *testing.T) {
	e := newEnv(t)
	list, err := trustedorigin.ParseList("https://dw.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	e.api.TrustedOrigins = list
	for _, tc := range []struct {
		name, origin string
		wantStatus   int
	}{
		{"canonical origin", "https://dw.cmjrt.com:55055", 404},
		{"extra origin", "HTTPS://DW.CMJRT.COM:50443", 404},
		{"wildcard subdomain rejected", "https://evil.dw.cmjrt.com:50443", 403},
		{"different port rejected", "https://dw.cmjrt.com:50444", 403},
		{"http rejected", "http://dw.cmjrt.com:50443", 403},
		{"missing origin rejected", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{"X-Test-Actor": "zzc"}
			if tc.origin != "" {
				headers["Origin"] = tc.origin
			}
			r := e.do("POST", "/api/console/agents/x/revoke", map[string]any{}, headers)
			if r.status != tc.wantStatus {
				t.Fatalf("status=%d body=%v want=%d", r.status, r.body, tc.wantStatus)
			}
		})
	}
	r := httptest.NewRequest(http.MethodPost, "/api/console/agents/x/revoke", strings.NewReader(`{}`))
	r.Header.Set("X-Test-Actor", "zzc")
	r.Header[http.CanonicalHeaderKey("Origin")] = []string{"https://dw.cmjrt.com:50443", "https://evil.example"}
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	e.api.Register(mux)
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("multiple Origin header values status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestErrorMapping(t *testing.T) {
	e := newEnv(t)
	b := applyBody("nosuch")
	if r := e.do("POST", "/api/enroll/applications", b, nil); r.status != 404 || r.body["error"] != "unknown_owner" {
		t.Errorf("unknown owner: %d %v", r.status, r.body)
	}
	b = applyBody("zzc")
	b["extra"] = 1
	if r := e.do("POST", "/api/enroll/applications", b, nil); r.status != 400 || r.body["error"] != "bad_request" {
		t.Errorf("unknown field: %d %v", r.status, r.body)
	}
	var id1, id2 string
	for i := 0; i < identity.ApplyPerIPPerHour; i++ {
		r := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil)
		if i == 0 {
			id1 = r.body["application_id"].(string)
		}
		if i == 1 {
			id2 = r.body["application_id"].(string)
		}
	}
	r := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil)
	if r.status != 429 || r.body["error"] != "rate_limited" || r.header.Get("Retry-After") == "" || r.body["retry_after_s"] == nil {
		t.Errorf("rate limit: %d %v %v", r.status, r.body, r.header)
	}
	approve := func(id, name string) resp {
		return e.do("POST", "/api/console/enroll/applications/"+id+"/approve",
			map[string]any{"pairing_code": code, "agent_name": name, "new_group_name": tg()}, owner("zzc"))
	}
	if r := approve(id1, "bad.name"); r.status != 422 || r.body["error"] != "name_invalid" {
		t.Errorf("name_invalid: %d %v", r.status, r.body)
	}
	if r := approve(id1, "kimi"); r.status != 200 {
		t.Fatalf("approve id1: %d %v", r.status, r.body)
	}
	// id2 与 id1 同一设备指纹 ⇒ 批了 id1 后 id2 已 superseded（V1 A10）
	if r := approve(id2, "Kimi"); r.status != 410 || r.body["error"] != "not_pending" {
		t.Errorf("superseded: %d %v", r.status, r.body)
	}
	if r := approve("nosuch", "x1"); r.status != 404 || r.body["error"] != "not_found" {
		t.Errorf("not_found: %d %v", r.status, r.body)
	}
}

func TestNameTakenAndExpiredOverHTTP(t *testing.T) {
	e := newEnv(t)
	mk := func(fp string) string {
		b := applyBody("zzc")
		b["device_fingerprint"] = fp
		return e.do("POST", "/api/enroll/applications", b, nil).body["application_id"].(string)
	}
	a1, a2, a3 := mk("dev-a"), mk("dev-b"), mk("dev-c")
	approve := func(id, name string) resp {
		return e.do("POST", "/api/console/enroll/applications/"+id+"/approve",
			map[string]any{"pairing_code": code, "agent_name": name, "new_group_name": tg()}, owner("zzc"))
	}
	if r := approve(a1, "opus50ts"); r.status != 200 {
		t.Fatal(r.body)
	}
	if r := approve(a2, "Opus50TS"); r.status != 409 || r.body["error"] != "name_taken" {
		t.Errorf("V1 A11: %d %v", r.status, r.body)
	}
	e.clk.Advance(identity.ApplicationTTL)
	if r := approve(a3, "fresh"); r.status != 410 || r.body["error"] != "expired" {
		t.Errorf("V1 A7: %d %v", r.status, r.body)
	}
}

func TestSourceIPTrustProxy(t *testing.T) {
	e := newEnv(t)
	hdr := map[string]string{"X-Real-IP": "203.0.113.7"}
	e.do("POST", "/api/enroll/applications", applyBody("zzc"), hdr)
	e.api.TrustProxy = true
	b := applyBody("zzc")
	b["device_fingerprint"] = "other-device"
	e.do("POST", "/api/enroll/applications", b, hdr)
	l := e.do("GET", "/api/console/enroll/applications", nil, map[string]string{"X-Test-Actor": "zzc"})
	var ips []string
	for _, a := range l.body["applications"].([]any) {
		ips = append(ips, a.(map[string]any)["source_ip"].(string))
	}
	sort.Strings(ips) // 要验的是采信了哪个来源，不是顺序（两条申请同一时刻）
	if strings.Join(ips, ",") != "127.0.0.1,203.0.113.7" {
		t.Fatalf("source ips = %v (untrusted header must be ignored, trusted one used)", ips)
	}
}

// 非回环直连方即使带 X-Real-IP 也不采信（否则任何人都能伪造来源 IP、绕过按 IP 限频）。
// httptest.Server 永远是回环连接，测不到这一格，所以直接调 handler 并伪造 RemoteAddr。
func TestSourceIPIgnoresHeaderFromNonLoopback(t *testing.T) {
	e := newEnv(t)
	e.api.TrustProxy = true
	mux := http.NewServeMux()
	e.api.Register(mux)
	b, _ := json.Marshal(applyBody("zzc"))
	req := httptest.NewRequest("POST", "/api/enroll/applications", bytes.NewReader(b))
	req.RemoteAddr = "198.51.100.9:40000"
	req.Header.Set("X-Real-IP", "203.0.113.7")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	l := e.do("GET", "/api/console/enroll/applications", nil, map[string]string{"X-Test-Actor": "zzc"})
	apps := l.body["applications"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["source_ip"] != "198.51.100.9" {
		t.Fatalf("source_ip = %v, want 198.51.100.9 (header from non-loopback peer must be ignored)", apps)
	}
}

// 业主令（15:34:17Z，板 8582acc4）：编组不是必须的 —— 三键都不给即合法、不入组。
func TestApproveWithoutGroupOverHTTP(t *testing.T) {
	e := newEnv(t)
	id := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil).body["application_id"].(string)
	if r := e.do("POST", "/api/console/enroll/applications/"+id+"/approve",
		map[string]any{"pairing_code": code, "agent_name": "Worker", "group_id": "g"}, owner("zzc")); r.status != 400 {
		t.Fatalf("old field name group_id must be rejected as unknown: %d %v", r.status, r.body)
	}
	r := e.do("POST", "/api/console/enroll/applications/"+id+"/approve",
		map[string]any{"pairing_code": code, "agent_name": "Worker"}, owner("zzc"))
	if r.status != 200 {
		t.Fatalf("approve without group: %d %v", r.status, r.body)
	}
	ag, err := e.store.GetAgent(context.Background(), r.body["agent_id"].(string))
	if err != nil || ag.GroupID != "" {
		t.Fatalf("agent without group: %+v %v", ag, err)
	}
	if codes, _ := e.store.ListAgentGroups(context.Background(), ag.ID); len(codes) != 0 {
		t.Fatalf("agent joined groups %v", codes)
	}
	if gs, _ := e.store.ListGroups(context.Background()); len(gs) != 0 {
		t.Fatalf("a group was created: %+v", gs)
	}
}

// GET /api/console/agents 只列【当前会话本人】的 Agent：别的业主的 Agent 一条都不出现（属主名大小写不同的会话仍是同一人）。
// 这是 console 左栏「别人的编组成员不在自己的 Agent 列表里、只能信编组快照自带的 online」的事实依据
// （见 view-service/console-agent-tree-regression.js 夹具）。
func TestAgentsListOwnerScopedOverHTTP(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, c := range []struct{ owner, name string }{{"zzc", "Alpha"}, {"zzc", "Beta"}, {"lbl", "Gamma"}} {
		if _, err := e.svc.IssueHostedAgentWithNewGroup(ctx, c.owner, c.name, tg(), c.owner); err != nil {
			t.Fatal(err)
		}
	}
	names := func(actor string) string {
		r := e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": actor})
		if r.status != 200 {
			t.Fatalf("%s: %d %v", actor, r.status, r.body)
		}
		var out []string
		for _, it := range r.body["agents"].([]any) {
			out = append(out, it.(map[string]any)["name"].(string))
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	if got := names("zzc"); got != "Alpha,Beta" {
		t.Fatalf("zzc sees [%s], want [Alpha,Beta]", got)
	}
	if got := names("ZZC"); got != "Alpha,Beta" {
		t.Fatalf("session owner in another case sees [%s], want [Alpha,Beta]", got)
	}
	if got := names("lbl"); got != "Gamma" {
		t.Fatalf("lbl sees [%s], want [Gamma] (never zzc's agents)", got)
	}
}

// G4 注入点：只给 origin=hosted 的项出 hosted 子对象，且只在注入函数返回 true 时出；自备项与无数据时不出。
func TestAgentsListHostedInjection(t *testing.T) {
	e := newEnv(t)
	hi, err := e.svc.IssueHostedAgentWithNewGroup(context.Background(), "zzc", "Codex1", tg(), "zzc")
	if err != nil {
		t.Fatal(err)
	}
	self := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil).body["application_id"].(string)
	if r := e.do("POST", "/api/console/enroll/applications/"+self+"/approve", map[string]any{"pairing_code": code, "agent_name": "Selfie", "new_group_name": tg()}, owner("zzc")); r.status != 200 {
		t.Fatal(r.body)
	}
	// 未接 G4（Hosted 为空）而库里已有托管行：列表照常 200，托管行不出 hosted 子对象（不得对空函数调用而 500）。
	if r := e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": "zzc"}); r.status != 200 {
		t.Fatalf("list without Hosted provider: %d %v", r.status, r.body)
	} else {
		for _, it := range r.body["agents"].([]any) {
			if _, has := it.(map[string]any)["hosted"]; has {
				t.Fatalf("no provider ⇒ no hosted object: %v", it)
			}
		}
	}
	calls := map[string]int{}
	e.api.Hosted = func(_ context.Context, id string) (map[string]any, bool) {
		calls[id]++
		if id != hi.Agent.ID {
			return nil, false
		}
		return map[string]any{"node_name": "node-a", "state": "running", "account_last4": "9z8y"}, true
	}
	for _, check := range []func(*http.Request) bool{nil, func(*http.Request) bool { return false }} {
		e.api.HostedAccess = check
		r := e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": "zzc"})
		if r.status != 200 || len(calls) != 0 {
			t.Fatalf("fenced list read hosted provider: %d %v", r.status, calls)
		}
		for _, item := range r.body["agents"].([]any) {
			if _, has := item.(map[string]any)["hosted"]; has {
				t.Fatal("fenced hosted metadata disclosed")
			}
		}
	}
	e.api.HostedAccess = func(*http.Request) bool { return true } // Explicitly allow this legacy projection contract fixture.
	list := e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": "zzc"}).body["agents"].([]any)
	if len(list) != 2 {
		t.Fatalf("agents: %v", list)
	}
	for _, it := range list {
		m := it.(map[string]any)
		h, has := m["hosted"].(map[string]any)
		switch m["name"] {
		case "Codex1":
			if !has || h["node_name"] != "node-a" || h["state"] != "running" || m["state"] != "active" {
				t.Fatalf("hosted item: %v", m)
			}
		case "Selfie":
			if has {
				t.Fatalf("selfhost item must not carry hosted: %v", m)
			}
		}
	}
	if calls[hi.Agent.ID] != 1 || len(calls) != 1 {
		t.Fatalf("Hosted must be called once and only for hosted items: %v", calls)
	}
	e.api.Hosted = func(context.Context, string) (map[string]any, bool) { return nil, false }
	for _, it := range e.do("GET", "/api/console/agents", nil, map[string]string{"X-Test-Actor": "zzc"}).body["agents"].([]any) {
		if _, has := it.(map[string]any)["hosted"]; has {
			t.Fatalf("no data ⇒ no hosted object: %v", it)
		}
	}
}

var tgSeq atomic.Int64

// tg 测试用的新组名：每次不同（组名全局唯一、1～24 个字母、数字或下划线）。批准与托管新建都必须带编组（GroupAllowed）。
func tg() string { return fmt.Sprintf("t%d", tgSeq.Add(1)) }

// 批准时的编组（GroupAllowed）错误码：二选一、已有组须存在且未关闭添加、新组名合规且不撞。
func TestApproveGroupErrorsOverHTTP(t *testing.T) {
	e := newEnv(t)
	mk := func(fp string) string {
		b := applyBody("zzc")
		b["device_fingerprint"] = fp
		return e.do("POST", "/api/enroll/applications", b, nil).body["application_id"].(string)
	}
	a1, a2 := mk("dev-a"), mk("dev-b")
	approve := func(id string, extra map[string]any) resp {
		body := map[string]any{"pairing_code": code, "agent_name": "Worker" + id[:4]}
		for k, v := range extra {
			body[k] = v
		}
		return e.do("POST", "/api/console/enroll/applications/"+id+"/approve", body, owner("zzc"))
	}
	for _, c := range []struct {
		name   string
		extra  map[string]any
		status int
		code   string
	}{
		{"blank new name", map[string]any{"new_group_name": "   "}, 422, "group_name_invalid"}, // 只有空白不当作「没给」
		{"both", map[string]any{"group_code": "x", "new_group_name": "开发组"}, 422, "group_ambiguous"},
		{"missing group", map[string]any{"group_code": "nope"}, 422, "group_not_found"},
		{"bad new name", map[string]any{"new_group_name": "二十五个字二十五个字二十五个字二十五个字二十五个字"}, 422, "group_name_invalid"},
	} {
		if r := approve(a1, c.extra); r.status != c.status || r.body["error"] != c.code {
			t.Errorf("%s: %d %v, want %d %s", c.name, r.status, r.body, c.status, c.code)
		}
	}
	r := approve(a1, map[string]any{"new_group_name": " 开发组 "})
	if r.status != 200 {
		t.Fatalf("approve with new group: %d %v", r.status, r.body)
	}
	ag, err := e.store.GetAgent(context.Background(), r.body["agent_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	g, err := e.store.GetGroup(context.Background(), ag.GroupID)
	if err != nil || g.ShortName != "开发组" || g.LeaderAgentID != ag.ID {
		t.Fatalf("new group: %+v %v", g, err)
	}
	if r := approve(a2, map[string]any{"new_group_name": "开发组"}); r.status != 409 || r.body["error"] != "group_name_taken" {
		t.Errorf("name taken: %d %v", r.status, r.body)
	}
	if err := e.svc.SetGroupAdditionsClosed(context.Background(), "zzc", g.Code, true); err != nil {
		t.Fatal(err)
	}
	if r := approve(a2, map[string]any{"group_code": g.Code}); r.status != 409 || r.body["error"] != "group_closed" {
		t.Errorf("closed: %d %v", r.status, r.body)
	}
	if err := e.svc.SetGroupAdditionsClosed(context.Background(), "zzc", g.Code, false); err != nil {
		t.Fatal(err)
	}
	// 别人的组（本人既未建、也无成员在内）⇒ 与不存在同一答复 422 group_not_found
	other, err := e.svc.IssueHostedAgentWithNewGroup(context.Background(), "lbl", "Lee", "李组", "lbl")
	if err != nil {
		t.Fatal(err)
	}
	if r := approve(a2, map[string]any{"group_code": other.Agent.GroupID}); r.status != 422 || r.body["error"] != "group_not_found" {
		t.Errorf("someone else's group: %d %v, want 422 group_not_found", r.status, r.body)
	}
	if r := approve(a2, map[string]any{"group_code": g.Code}); r.status != 200 {
		t.Fatalf("approve into existing group: %d %v", r.status, r.body)
	}
}
