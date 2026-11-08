package mcpgw

import (
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func requestCompat(method, params string) *http.Request {
	id := `"id":1,`
	if method == "notifications/initialized" {
		id = ""
	}
	if params == "" {
		params = `{}`
		if method == "initialize" {
			params = `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}`
		}
	}
	r := httptest.NewRequest("POST", testOrigin+"/mcp", strings.NewReader(`{"jsonrpc":"2.0",`+id+`"method":"`+method+`","params":`+params+`}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Authorization", "Bearer local-token")
	if method != "initialize" {
		r.Header.Set("MCP-Protocol-Version", CompatProtocolVersion)
	}
	return r
}
func TestCompat2025LifecycleAndFourTools(t *testing.T) {
	_, seats, mux := fixture(t)
	params := `{"protocolVersion":"2025-06-18","capabilities":{"elicitation":{"form":{},"url":{}}},"clientInfo":{"name":"codex-mcp-client","title":"Codex","version":"0.167.0"}}`
	w := serve(mux, requestCompat("initialize", params))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("CODEX_2025_INITIALIZE: %d %s", w.Code, w.Body)
	}
	if w.Header().Get("Mcp-Session-Id") != "" {
		t.Fatal("codec must not cache a protocol session")
	}
	w = serve(mux, requestCompat("notifications/initialized", ""))
	if w.Code != 202 || w.Body.Len() != 0 {
		t.Fatalf("notification %d %s", w.Code, w.Body)
	}
	w = serve(mux, requestCompat("ping", ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"result":{}`) {
		t.Fatal("ping", w.Code, w.Body)
	}
	w = serve(mux, requestCompat("tools/list", ""))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &listed) != nil || len(listed.Result.Tools) != 4 {
		t.Fatal("four tools only", w.Body)
	}
	for _, tc := range []struct{ name, args string }{{"list_online", `{}`}, {"send_message", `{"to":"target.zzc","body":"synthetic","client_message_id":"id"}`}, {"wait_for_messages", `{"wait_seconds":0.001}`}, {"ack", `{"delivery_id":"delivery","generation":2,"phase":"received"}`}} {
		w = serve(mux, requestCompat("tools/call", `{"name":"`+tc.name+`","arguments":`+tc.args+`}`))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"isError":false`) {
			t.Fatal(tc.name, w.Code, w.Body)
		}
	}
	if seats.authCalls != 8 {
		t.Fatalf("each request must authenticate: %d", seats.authCalls)
	}
}
func TestCompat2025EveryRequestKeepsAuthOriginAndArgumentGuards(t *testing.T) {
	_, seats, mux := fixture(t)
	for _, method := range []string{"initialize", "notifications/initialized", "ping", "tools/list", "tools/call"} {
		r := requestCompat(method, "")
		r.Header.Del("Authorization")
		w := serve(mux, r)
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("auth %s %d", method, w.Code)
		}
		r = requestCompat(method, "")
		r.Header.Set("Origin", "https://foreign.example")
		w = serve(mux, r)
		if w.Code != 403 {
			t.Fatalf("origin %s %d", method, w.Code)
		}
	}
	if seats.calls != 0 {
		t.Fatal("unauthorized business side effect")
	}
	for _, tc := range []struct{ method, params string }{{"tools/call", `{"name":"list_online","arguments":{"target":"foreign"}}`}, {"tools/call", `{"name":"list_online","arguments":{},"tenant":"override"}`}, {"tools/list", `{"cursor":"unissued"}`}, {"tools/list", `{"unexpected":true}`}, {"ping", `null`}, {"initialize", `{"protocolVersion":"2025-06-18","capabilities":null,"clientInfo":{"name":"c","version":"1"}}`}} {
		w := serve(mux, requestCompat(tc.method, tc.params))
		if w.Code != 400 {
			t.Fatalf("params accepted %s: %d %s", tc.params, w.Code, w.Body)
		}
	}
	r := requestCompat("tools/list", "")
	r.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	if w := serve(mux, r); w.Code != 400 {
		t.Fatal("2026 envelope guard bypassed")
	}
	// A successful handshake never grants a token snapshot: next request still
	// sees the seat's revocation/tenant/scope/audience/AG error from Seats.
	for _, tc := range []struct {
		token string
		code  int
	}{{"wrong", 401}, {"local-token", 200}} {
		r := requestCompat("tools/list", "")
		r.Header.Set("Authorization", "Bearer "+tc.token)
		if w := serve(mux, r); w.Code != tc.code {
			t.Fatal(w.Code, w.Body)
		}
	}
}
func TestCompat2025RechecksRevocationTenantAndGroup(t *testing.T) {
	_, seats, mux := fixture(t)
	if w := serve(mux, requestCompat("initialize", "")); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, denial := range []error{extshared.ErrTokenRevoked, extshared.ErrTokenExpired, extshared.ErrTenantRejected, extshared.ErrGroupDissolved, extshared.ErrNotPermitted} {
		seats.authErr = denial
		status, _ := mapped(denial)
		for _, method := range []string{"ping", "notifications/initialized", "tools/list", "tools/call"} {
			w := serve(mux, requestCompat(method, ""))
			if w.Code != status {
				t.Fatalf("cached authorization for %v/%s: %d", denial, method, w.Code)
			}
		}
	}
	seats.authErr = nil
	w := serve(mux, requestCompat("tools/call", `{"name":"send_message","arguments":{"to":"foreign.zzc","body":"synthetic","client_message_id":"cross-group"}}`))
	status, _ := mapped(extshared.ErrTargetNotInAG)
	if w.Code != status {
		t.Fatal("AG bypass", w.Code, w.Body)
	}
}

func TestLoopbackPortMatchingAndLocalhostRejection(t *testing.T) {
	for _, registeredURI := range []string{"http://127.0.0.1/callback", "http://[::1]/callback"} {
		c := Client{Redirects: []string{registeredURI}}
		host := "127.0.0.1"
		other := "[::1]"
		if strings.Contains(registeredURI, "[::1]") {
			host = "[::1]"
			other = "127.0.0.1"
		}
		for _, port := range []string{"43123", "43124"} {
			if !registered(c, "http://"+host+":"+port+"/callback") {
				t.Fatal("LOOPBACK_RANDOM_PORT_REJECTED", port)
			}
		}
		for _, bad := range []string{"http://localhost:43123/callback", "http://" + other + ":43123/callback", "https://" + host + ":43123/callback", "http://" + host + ":43123/other", "http://" + host + ":43123/callback?extra=1", "http://" + host + ":43123/%63allback", "http://user@" + host + ":43123/callback", "http://" + host + ":43123/callback#fragment"} {
			if registered(c, bad) {
				t.Fatal("loopback URI widened", bad)
			}
		}
	}
	id := "https://client.example/codex.json"
	c := Client{ID: id, Name: "invalid localhost", Redirects: []string{"http://localhost:43123/callback"}, AuthMethod: "none"}
	for _, local := range []string{"http://localhost:43123/callback", "https://localhost/callback", "https://LOCALHOST:43123/callback"} {
		c.Redirects = []string{local}
		if validateClient(c, id) == nil {
			t.Fatal("LOCALHOST_METADATA_MUST_BE_REJECTED", local)
		}
	}
	c.Redirects = []string{"https://client.example:443/callback"}
	if !registered(c, c.Redirects[0]) || registered(c, "https://client.example:444/callback") {
		t.Fatal("non-loopback exact match changed")
	}
}
func TestCodexStaticClientAndExactLocalResolution(t *testing.T) {
	g, _, mux := fixture(t)
	r := httptest.NewRequest("GET", testOrigin+codexClientPath, nil)
	r.Host = "attacker.example"
	w := serve(mux, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var c Client
	if json.Unmarshal(w.Body.Bytes(), &c) != nil || c.ID != testOrigin+codexClientPath || len(c.Redirects) != 2 || c.Redirects[0] != "http://127.0.0.1/callback" || c.Redirects[1] != "http://[::1]/callback" {
		t.Fatal(w.Body)
	}
	if c.AuthMethod != "none" {
		t.Fatal("public client only")
	}
	// Configured external resolver is deliberately failing: own ID resolves only
	// the server-generated doc, while any lookalike still uses the external seam.
	g.cfg.Clients = clientsFake{err: errParams}
	for _, tc := range []struct {
		id   string
		code int
	}{{testOrigin + codexClientPath, 302}, {testOrigin + codexClientPath + "?evil=1", 400}, {"https://other.example" + codexClientPath, 400}} {
		q := authorizationQuery()
		q.Set("client_id", tc.id)
		q.Set("redirect_uri", "http://127.0.0.1:49152/callback")
		w = serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil))
		if w.Code != tc.code {
			t.Fatal(tc.id, w.Code, w.Body)
		}
	}
}
