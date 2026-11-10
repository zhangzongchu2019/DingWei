package mcpgw

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
	_ "modernc.org/sqlite"
)

const testOrigin = "https://hub.example"
const clientID = "https://client.example/metadata.json"
const clientRedirect = "http://127.0.0.1:43123/callback"

type seatsFake struct {
	mu                     sync.Mutex
	authErr, errorBusiness error
	issueErr               error
	calls, authCalls       int
	wait                   time.Duration
	expectReply            bool
	issued                 []feishulogin.IssueRequest
	now                    time.Time
}

func (s *seatsFake) Authenticate(_ context.Context, token string) (extseat.Seat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authCalls++
	if s.authErr != nil {
		return extseat.Seat{}, s.authErr
	}
	if token != "local-token" {
		return extseat.Seat{}, extshared.ErrUnauthenticated
	}
	return extseat.Seat{ID: "seat", ExpiresAt: s.now.Add(extshared.TokenTTL)}, nil
}
func (s *seatsFake) business() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.errorBusiness
}
func (s *seatsFake) ListOnline(context.Context, extseat.Seat) ([]extseat.OnlineAgent, error) {
	e := s.business()
	return []extseat.OnlineAgent{{FullName: "target.zzc", LastSeenAt: s.now}}, e
}
func (s *seatsFake) Send(_ context.Context, _ extseat.Seat, to, body, id string, expect ...bool) (extseat.SendReceipt, error) {
	e := s.business()
	if len(expect) > 0 {
		s.mu.Lock()
		s.expectReply = expect[0]
		s.mu.Unlock()
	}
	if to != "target.zzc" {
		e = extshared.ErrTargetNotInAG
	}
	return extseat.SendReceipt{DeliveryID: "delivery", Generation: 1, Stage: extshared.StageAccepted}, e
}
func (s *seatsFake) Wait(ctx context.Context, _ extseat.Seat, d time.Duration) ([]extseat.Delivery, error) {
	s.mu.Lock()
	s.wait = d
	s.mu.Unlock()
	e := s.business()
	if ctx.Err() != nil {
		e = ctx.Err()
	}
	return []extseat.Delivery{{ID: "delivery", Generation: 2, From: "target.zzc", To: "bot_AG1.zzc", Body: "literal https://example.test", Group: "AG1"}}, e
}
func (s *seatsFake) Ack(_ context.Context, _ extseat.Seat, id string, gen int) error {
	e := s.business()
	if id != "delivery" || gen != 2 {
		return extshared.ErrNotPermitted
	}
	return e
}

// 实际 extwire.Issuer 在 C 回调签发，替身只替代 A 的 PG 边界。
func (s *seatsFake) IssueToken(_ context.Context, p extshared.Principal, short, target string) (string, extseat.Seat, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issued = append(s.issued, feishulogin.IssueRequest{Principal: p, ShortName: short, TargetAgent: target})
	if s.issueErr != nil {
		return "", extseat.Seat{}, "", s.issueErr
	}
	return "local-token", extseat.Seat{ID: "seat", FullName: "bot_AG1.zzc", ExpiresAt: s.now.Add(extshared.TokenTTL)}, "AG1", nil
}

type clientsFake struct {
	client Client
	err    error
}

func (c clientsFake) Resolve(context.Context, string) (Client, error) { return c.client, c.err }

type loginFake struct{}

func (loginFake) ServeConnectPage(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
func (loginFake) Callback(context.Context, feishulogin.CallbackRequest) (feishulogin.CallbackResult, error) {
	return feishulogin.CallbackResult{}, errors.New("not used")
}
func testDB(t *testing.T) (*repo.Pool, *SQLStore) { return testDBKey(t, randomKey(t)) }
func randomKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		t.Fatal(e)
	}
	return key
}
func testDBKey(t *testing.T, key []byte) (*repo.Pool, *SQLStore) {
	t.Helper()
	db, e := repo.OpenSQLite(context.Background(), t.TempDir()+"/oauth.db?_pragma=busy_timeout(10000)")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	ddl, e := os.ReadFile("../store/migrate/migrations/0107_mcpgw_oauth.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(context.Background(), testAccess(t), string(ddl)); e != nil {
		t.Fatal(e)
	}
	s, e := NewSQLStore(context.Background(), db, testAccess(t), testOrigin+"/mcp", key)
	if e != nil {
		t.Fatal(e)
	}
	return db, s
}
func fixture(t *testing.T) (*Gateway, *seatsFake, *http.ServeMux) {
	t.Helper()
	_, store := testDB(t)
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	seats := &seatsFake{now: now}
	g, e := New(Config{PublicOrigin: testOrigin, Resource: testOrigin + "/mcp", Seats: seats, Login: loginFake{}, Clients: clientsFake{client: Client{ID: clientID, Name: "test client", Redirects: []string{clientRedirect}, AuthMethod: "none", Grants: []string{"authorization_code"}, Responses: []string{"code"}}}, Store: store, Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	return g, seats, mux
}
func rpcRequestFor(method, name, args string) *http.Request {
	if args == "" {
		args = `{}`
	}
	body := `{"jsonrpc":"2.0","id":"fresh-rpc","method":"` + method + `","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}},"name":"` + name + `","arguments":` + args + `}}`
	r := httptest.NewRequest("POST", testOrigin+"/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Authorization", "Bearer local-token")
	r.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	r.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		r.Header.Set("Mcp-Name", name)
	}
	return r
}
func restRequest(name, args string) *http.Request {
	r := httptest.NewRequest("POST", testOrigin+"/api/external/"+name, strings.NewReader(args))
	r.Header.Set("Authorization", "Bearer local-token")
	r.Header.Set("Content-Type", "application/json")
	return r
}
func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestFourToolsRESTParity(t *testing.T) {
	for _, tc := range []struct{ name, args, contains string }{
		{"list_online", `{}`, `target.zzc`},
		{"send_message", `{"to":"target.zzc","body":"literal","client_message_id":"retry-id"}`, `accepted`},
		{"wait_for_messages", `{"wait_seconds":0}`, `delivery`},
		{"ack", `{"delivery_id":"delivery","generation":2,"phase":"received"}`, `received`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s, mux := fixture(t)
			for _, rest := range []bool{false, true} {
				r := rpcRequestFor("tools/call", tc.name, tc.args)
				if rest {
					r = restRequest(tc.name, tc.args)
				}
				w := serve(mux, r)
				if w.Code != 200 || !strings.Contains(w.Body.String(), tc.contains) {
					t.Fatalf("rest=%v status=%d body=%s", rest, w.Code, w.Body)
				}
				if !rest && !strings.Contains(w.Body.String(), `"resultType":"complete"`) {
					t.Fatal("missing resultType")
				}
				if w.Header().Get("Mcp-Session-Id") != "" {
					t.Fatal("session minted")
				}
			}
			if s.calls != 2 || s.authCalls != 2 {
				t.Fatalf("not independently authenticated/dispatched %d/%d", s.authCalls, s.calls)
			}
		})
	}
}

func TestSendMessageExpectReplyMCPAndREST(t *testing.T) {
	for _, rest := range []bool{false, true} {
		_, s, mux := fixture(t)
		r := rpcRequestFor("tools/call", "send_message", `{"to":"target.zzc","body":"x","client_message_id":"expect","expect_reply":true}`)
		if rest {
			r = restRequest("send_message", `{"to":"target.zzc","body":"x","client_message_id":"expect","expect_reply":true}`)
		}
		if w := serve(mux, r); w.Code != 200 {
			t.Fatalf("rest=%t status=%d body=%s", rest, w.Code, w.Body)
		}
		if !s.expectReply {
			t.Fatalf("rest=%t expect_reply not passed to service", rest)
		}
	}
	defs := toolDefinitions()
	for _, raw := range defs {
		def := raw.(map[string]any)
		if def["name"] != "send_message" {
			continue
		}
		schema := def["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		if props["expect_reply"].(map[string]any)["type"] != "boolean" {
			t.Fatalf("bad expect_reply schema: %#v", props)
		}
		return
	}
	t.Fatal("send_message schema missing")
}

func TestToolBoundaryMatrix(t *testing.T) {
	tools := map[string]string{"list_online": `{}`, "send_message": `{"to":"target.zzc","body":"x","client_message_id":"stable"}`, "wait_for_messages": `{"wait_seconds":0}`, "ack": `{"delivery_id":"delivery","generation":2,"phase":"received"}`}
	for name, args := range tools {
		for _, rest := range []bool{false, true} {
			for _, tc := range []struct {
				name      string
				auth, biz error
				status    int
			}{
				{"revoked", extshared.ErrTokenRevoked, nil, 401}, {"expired", extshared.ErrTokenExpired, nil, 401}, {"unauthenticated", extshared.ErrUnauthenticated, nil, 401}, {"group_dissolved", nil, extshared.ErrGroupDissolved, 403}, {"not_permitted", nil, extshared.ErrNotPermitted, 403},
			} {
				t.Run(name+"/"+map[bool]string{true: "rest", false: "mcp"}[rest]+"/"+tc.name, func(t *testing.T) {
					_, s, mux := fixture(t)
					s.authErr = tc.auth
					s.errorBusiness = tc.biz
					r := rpcRequestFor("tools/call", name, args)
					if rest {
						r = restRequest(name, args)
					}
					w := serve(mux, r)
					if w.Code != tc.status {
						t.Fatalf("status %d body %s", w.Code, w.Body)
					}
					if tc.auth != nil && s.calls != 0 {
						t.Fatal("unauthorized business call")
					}
					if w.Code == 401 && !strings.Contains(w.Header().Get("WWW-Authenticate"), "resource_metadata=") {
						t.Fatal("missing OAuth discovery challenge")
					}
				})
			}
		}
	}
}
func TestDiscoveryWithoutInitializeAndNoExtraCapabilities(t *testing.T) {
	_, _, mux := fixture(t)
	for _, method := range []string{"server/discover", "tools/list"} {
		w := serve(mux, rpcRequestFor(method, "", `{}`))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"resultType":"complete"`) {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		if method == "tools/list" {
			var data struct {
				Result struct{ Tools []struct{ Name string } }
			}
			if json.Unmarshal(w.Body.Bytes(), &data) != nil || len(data.Result.Tools) != 4 {
				t.Fatal("not exactly four tools")
			}
			for _, d := range data.Result.Tools {
				if !knownTool(d.Name) {
					t.Fatalf("unexpected tool %s", d.Name)
				}
			}
		}
	}
	for _, name := range []string{"execute", "upload_file", "heartbeat_report", "list_online/extra"} {
		for _, r := range []*http.Request{rpcRequestFor("tools/call", name, `{}`), restRequest(name, `{}`)} {
			if w := serve(mux, r); w.Code != 404 {
				t.Fatalf("extra capability %s allowed: %d", name, w.Code)
			}
		}
	}
	if w := serve(mux, rpcRequestFor("initialize", "", `{}`)); w.Code != 404 {
		t.Fatal("initialize supported")
	}
	for _, m := range []string{"GET", "DELETE"} {
		if w := serve(mux, httptest.NewRequest(m, testOrigin+"/mcp", nil)); w.Code != 405 {
			t.Fatal("legacy stream/session accepted")
		}
	}
}
func TestRequestMetadataAndOriginGuards(t *testing.T) {
	for _, tc := range []struct {
		name         string
		change       func(*http.Request)
		status, code int
	}{
		{"missing_version", func(r *http.Request) { r.Header.Del("MCP-Protocol-Version") }, 400, -32020},
		{"method_mismatch", func(r *http.Request) { r.Header.Set("Mcp-Method", "tools/list") }, 400, -32020},
		{"name_mismatch", func(r *http.Request) { r.Header.Set("Mcp-Name", "ack") }, 400, -32020},
		{"missing_name", func(r *http.Request) { r.Header.Del("Mcp-Name") }, 400, -32020},
		{"duplicate_header", func(r *http.Request) { r.Header.Add("Mcp-Name", "list_online") }, 400, -32020},
		{"invalid_origin", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") }, 403, 0},
		{"null_origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403, 0},
		{"duplicate_origin", func(r *http.Request) { r.Header.Add("Origin", testOrigin); r.Header.Add("Origin", testOrigin) }, 403, 0},
		{"missing_accept", func(r *http.Request) { r.Header.Del("Accept") }, 406, -32600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s, mux := fixture(t)
			r := rpcRequestFor("tools/call", "list_online", `{}`)
			tc.change(r)
			w := serve(mux, r)
			if w.Code != tc.status {
				t.Fatalf("guard missing: status=%d body=%s", w.Code, w.Body)
			}
			if s.authCalls != 0 || s.calls != 0 {
				t.Fatal("invalid request reached A")
			}
			if tc.code != 0 {
				var d struct{ Error wireError }
				json.Unmarshal(w.Body.Bytes(), &d)
				if d.Error.Code != tc.code {
					t.Fatalf("wrong protocol error %d", d.Error.Code)
				}
			}
		})
	}
	_, _, mux := fixture(t)
	for _, patch := range []struct {
		from, to     string
		status, code int
	}{
		{ProtocolVersion, "2025-11-25", 400, -32022},
		{`"io.modelcontextprotocol/clientCapabilities":{}`, `"io.modelcontextprotocol/clientCapabilities":null`, 400, -32602},
		{`"io.modelcontextprotocol/clientCapabilities":{}`, `"io.modelcontextprotocol/clientCapabilities":{"roots":true}`, 400, -32602},
	} {
		r := rpcRequestFor("tools/call", "list_online", `{}`)
		raw, _ := ioRead(r)
		r.Body = http.NoBody
		raw = strings.ReplaceAll(raw, patch.from, patch.to)
		r = httptest.NewRequest("POST", testOrigin+"/mcp", strings.NewReader(raw))
		r.Header = rpcRequestFor("tools/call", "list_online", `{}`).Header
		if patch.from == ProtocolVersion {
			r.Header.Set("MCP-Protocol-Version", patch.to)
		}
		w := serve(mux, r)
		var d struct{ Error wireError }
		json.Unmarshal(w.Body.Bytes(), &d)
		if w.Code != patch.status || d.Error.Code != patch.code {
			t.Fatalf("bad metadata response %d %s", w.Code, w.Body)
		}
	}
	r := rpcRequestFor("tools/call", "list_online", `{}`)
	r.Header.Set("Mcp-Name", "=?base64?bGlzdF9vbmxpbmU=?=")
	r.Header.Set("Origin", testOrigin)
	if w := serve(mux, r); w.Code != 200 {
		t.Fatalf("encoded valid name rejected %d %s", w.Code, w.Body)
	}
}
func ioRead(r *http.Request) (string, error) { b, e := io.ReadAll(r.Body); return string(b), e }
func TestArgumentsAndCrossAG(t *testing.T) {
	for _, tc := range []struct {
		name, args string
		status     int
	}{
		{"send_message", `{"to":"other.zzc","body":"x","client_message_id":"id"}`, 403},
		{"list_online", `{"agent_id":"other"}`, 400},
		{"wait_for_messages", `{"wait_seconds":25.01}`, 400},
		{"wait_for_messages", `{"wait_seconds":-1}`, 400},
		{"ack", `{"delivery_id":"other","generation":2,"phase":"received"}`, 403},
		{"ack", `{"delivery_id":"delivery","generation":1,"phase":"received"}`, 403},
		{"ack", `{"delivery_id":"delivery","generation":2,"phase":"processed"}`, 400},
		{"send_message", `{"to":"target.zzc","body":"x"}`, 400},
		{"list_online", `null`, 400},
	} {
		for _, rest := range []bool{false, true} {
			t.Run(tc.name+tc.args+map[bool]string{true: "rest", false: "mcp"}[rest], func(t *testing.T) {
				_, _, mux := fixture(t)
				r := rpcRequestFor("tools/call", tc.name, tc.args)
				if rest {
					r = restRequest(tc.name, tc.args)
				}
				w := serve(mux, r)
				if w.Code != tc.status {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
			})
		}
	}
	_, s, mux := fixture(t)
	if w := serve(mux, rpcRequestFor("tools/call", "wait_for_messages", `{}`)); w.Code != 200 || s.wait != extshared.WaitMax {
		t.Fatalf("default wait not capped %v", s.wait)
	}
}
func TestAllSharedErrorsSanitizedAndMapped(t *testing.T) {
	for _, rule := range errorRules {
		st, we := mapped(errors.Join(rule.err, errors.New("private credential")))
		if st != rule.status || we.Code != rule.code || strings.Contains(we.Message, "private") {
			t.Fatalf("bad map %v", we)
		}
	}
	st, we := mapped(errors.New("private token"))
	if st != 500 || we.Message != "internal_error" {
		t.Fatal("internal error leak")
	}
}

func testAccess(t *testing.T) repo.AccessCtx {
	t.Helper()
	a, e := repo.NewAccessCtx("test-team", "oauth-gateway")
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func TestApplicationErrorsAvoidMCPReservedRange(t *testing.T) {
	for _, r := range errorRules {
		if r.err == errParams || r.err == errMethod {
			continue
		}
		if r.code >= -32768 && r.code <= -32000 {
			t.Fatalf("application error uses reserved MCP code %d", r.code)
		}
	}
}
func TestRequestIDMustBeStringOrInteger(t *testing.T) {
	_, _, mux := fixture(t)
	for _, id := range []string{"null", "1.5", "true", "{}"} {
		r := rpcRequestFor("tools/list", "", `{}`)
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(strings.Replace(string(raw), `"fresh-rpc"`, id, 1)))
		if w := serve(mux, r); w.Code != 400 {
			t.Fatalf("invalid ID %s accepted", id)
		}
	}
}
func TestAcceptZeroQualityRejected(t *testing.T) {
	_, _, mux := fixture(t)
	r := rpcRequestFor("tools/list", "", `{}`)
	r.Header.Set("Accept", "application/json; q=0.0, text/event-stream")
	if w := serve(mux, r); w.Code != 406 {
		t.Fatal("zero quality accepted")
	}
}
