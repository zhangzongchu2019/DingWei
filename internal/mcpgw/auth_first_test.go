package mcpgw

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

func proposedInitialize(version string) string {
	return fmt.Sprintf(`{"protocolVersion":%q,"capabilities":{"tasks":{}},"clientInfo":{"name":"external-test","version":"1","description":"synthetic","websiteUrl":"https://example.test","icons":[]}}`, version)
}

func TestMCPAuthFirstThreeVersionsAndMissingHeaders(t *testing.T) {
	for _, version := range []string{"2025-03-26", "2025-06-18", "2025-11-25"} {
		for _, method := range []string{"initialize", "tools/list", "ping"} {
			for _, header := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/header=%t", version, method, header), func(t *testing.T) {
					_, s, mux := fixture(t)
					p := "{}"
					if method == "initialize" {
						p = proposedInitialize(version)
					}
					r := requestCompat(method, p)
					r.Header.Del("Authorization")
					r.Header.Del("MCP-Protocol-Version")
					if header {
						r.Header.Set("MCP-Protocol-Version", version)
					}
					w := serve(mux, r)
					want := `Bearer resource_metadata="` + testOrigin + `/.well-known/oauth-protected-resource/mcp", scope="` + extshared.ScopeString() + `"`
					if w.Code != 401 || w.Header().Get("WWW-Authenticate") != want {
						t.Fatalf("AUTH_BEFORE_PROTOCOL: %d %s challenge=%s", w.Code, w.Body, w.Header().Get("WWW-Authenticate"))
					}
					if s.calls != 0 || s.authCalls != 0 {
						t.Fatalf("missing bearer reached seats: auth=%d business=%d", s.authCalls, s.calls)
					}
					if strings.Contains(w.Body.String(), "HeaderMismatch") || strings.Contains(w.Body.String(), "tools") {
						t.Fatal("unauthorized response leaked protocol/resources")
					}
				})
			}
		}
	}
}

type unreadBody struct{ reads int }

func (b *unreadBody) Read([]byte) (int, error) { b.reads++; return 0, io.ErrUnexpectedEOF }
func (*unreadBody) Close() error               { return nil }

func TestMCPAuthFirstDoesNotReadInvalidBodyAndHandlesAllHTTPMethods(t *testing.T) {
	for _, method := range []string{"POST", "GET", "DELETE", "OPTIONS", "PATCH", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			_, s, mux := fixture(t)
			body := &unreadBody{}
			r := httptest.NewRequest(method, testOrigin+"/mcp", nil)
			r.Body = body
			r.Header.Add("MCP-Protocol-Version", "invalid")
			r.Header.Add("MCP-Protocol-Version", "duplicate")
			r.Header.Set("Origin", "https://foreign.example")
			r.Header.Set("Content-Type", "text/plain")
			w := serve(mux, r)
			if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" || body.reads != 0 || s.calls != 0 {
				t.Fatalf("AUTH_BEFORE_READ: method=%s code=%d reads=%d", method, w.Code, body.reads)
			}
		})
	}
	for _, err := range []error{extshared.ErrUnauthenticated, extshared.ErrTokenExpired, extshared.ErrTokenRevoked} {
		_, s, mux := fixture(t)
		s.authErr = err
		r := httptest.NewRequest("POST", testOrigin+"/mcp", strings.NewReader("not json"))
		r.Header.Set("Authorization", "Bearer local-token")
		w := serve(mux, r)
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" || s.authCalls != 1 || s.calls != 0 {
			t.Fatalf("invalid bearer not challenged first: %v %d", err, w.Code)
		}
	}
}

func TestMCPInitializeNegotiatesSupportedVersionAndUsesItForTools(t *testing.T) {
	for _, version := range []string{"2025-03-26", "2025-06-18", "2025-11-25", "2099-01-01"} {
		t.Run(version, func(t *testing.T) {
			_, s, mux := fixture(t)
			r := requestCompat("initialize", proposedInitialize(version))
			r.Header.Set("MCP-Protocol-Version", version)
			w := serve(mux, r)
			var d struct {
				Result struct {
					Version      string                     `json:"protocolVersion"`
					Capabilities map[string]json.RawMessage `json:"capabilities"`
				} `json:"result"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Result.Version != CompatProtocolVersion {
				t.Fatalf("VERSION_NEGOTIATION: proposed=%s code=%d body=%s", version, w.Code, w.Body)
			}
			if len(d.Result.Capabilities) != 1 || d.Result.Capabilities["tools"] == nil {
				t.Fatal("unimplemented tasks/resources capability advertised")
			}
			for _, method := range []string{"notifications/initialized", "ping", "tools/list", "tools/call"} {
				params := "{}"
				if method == "tools/call" {
					params = `{"name":"list_online","arguments":{}}`
				}
				q := requestCompat(method, params)
				q.Header.Set("MCP-Protocol-Version", d.Result.Version)
				next := serve(mux, q)
				want := 200
				if method == "notifications/initialized" {
					want = 202
				}
				if next.Code != want {
					t.Fatalf("NEGOTIATED_CHAIN: method=%s code=%d body=%s", method, next.Code, next.Body)
				}
			}
			if s.authCalls != 5 || s.calls != 1 {
				t.Fatalf("per request auth lost: auth=%d business=%d", s.authCalls, s.calls)
			}
			s.authErr = extshared.ErrTokenRevoked
			if denied := serve(mux, requestCompat("tools/list", "")); denied.Code != 401 {
				t.Fatal("handshake cached authorization")
			}
		})
	}
}

func TestMCPMissingVersionMeansMarchNotPrivateEnvelope(t *testing.T) {
	_, s, mux := fixture(t)
	for _, method := range []string{"tools/list", "ping"} {
		r := requestCompat(method, "")
		r.Header.Del("MCP-Protocol-Version")
		w := serve(mux, r)
		var d struct{ Error wireError }
		if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &d) != nil || d.Error.Code != -32022 || strings.Contains(w.Body.String(), "HeaderMismatch") {
			t.Fatalf("DEFAULT_MARCH_VERSION: method=%s code=%d body=%s", method, w.Code, w.Body)
		}
	}
	// Authenticated invalid protocol, transport and Origin still fail closed.
	r := requestCompat("tools/list", "")
	r.Header.Set("MCP-Protocol-Version", "2025-11-25")
	if w := serve(mux, r); w.Code != 400 {
		t.Fatal("unsupported operating version accepted")
	}
	r = requestCompat("tools/list", "")
	r.Header.Set("Origin", "https://foreign.example")
	if w := serve(mux, r); w.Code != 403 {
		t.Fatal("authenticated Origin guard bypassed")
	}
	r = requestCompat("tools/list", "")
	r.Header.Set("Content-Type", "text/plain")
	if w := serve(mux, r); w.Code != 415 {
		t.Fatal("authenticated content type guard bypassed")
	}
	if s.calls != 0 {
		t.Fatal("invalid request executed business")
	}
}

func TestMCPAuthFirstRealHTTPDiscoveryAndNegotiation(t *testing.T) {
	_, _, mux := fixture(t)
	server := httptest.NewServer(mux)
	defer server.Close()
	for _, version := range []string{"2025-03-26", "2025-06-18", "2025-11-25"} {
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` + proposedInitialize(version) + `}`
		req, _ := http.NewRequest("POST", server.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("MCP-Protocol-Version", version)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), testOrigin+"/.well-known/oauth-protected-resource/mcp") {
			t.Fatal("HTTP OAuth discovery blocked", resp.StatusCode)
		}
		req, _ = http.NewRequest("POST", server.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer local-token")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", version)
		resp, err = server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(data), `"protocolVersion":"2025-06-18"`) {
			t.Fatalf("HTTP authenticated negotiation blocked: %d %s", resp.StatusCode, data)
		}
	}
}

func TestMCPExplicitJuneCodecStillIgnoresUnrelatedVendorHeaders(t *testing.T) {
	_, _, mux := fixture(t)
	r := requestCompat("tools/list", "")
	r.Header.Set("Mcp-Method", "unrelated-vendor-value")
	if w := serve(mux, r); w.Code != 200 {
		t.Fatalf("existing explicit June codec selection regressed: %d %s", w.Code, w.Body)
	}
}
