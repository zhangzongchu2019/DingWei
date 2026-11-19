package mcpgw

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

// Replay the four pre-fix, credential-free production request shapes over an
// actual local TCP server. This does not contact production or a real account.
func TestR61MCPUnauthenticatedFourBaselinesOverRealHTTP(t *testing.T) {
	_, seats, mux := fixture(t)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	want := `Bearer resource_metadata="` + testOrigin + `/.well-known/oauth-protected-resource/mcp", scope="` + extshared.ScopeString() + `"`
	cases := []struct{ name, body string }{
		{"initialize_2025-06-18", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` + proposedInitialize("2025-06-18") + `}`},
		{"initialize_2025-03-26", `{"jsonrpc":"2.0","id":2,"method":"initialize","params":` + proposedInitialize("2025-03-26") + `}`},
		{"initialize_2025-11-25", `{"jsonrpc":"2.0","id":3,"method":"initialize","params":` + proposedInitialize("2025-11-25") + `}`},
		{"ping_no_version", `{"jsonrpc":"2.0","id":4,"method":"ping","params":{}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader(c.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("MCP-Protocol-Version") != "" {
				t.Fatal("fixture must have no credentials or version header")
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != want {
				t.Fatalf("REAL_HTTP_AUTH_FIRST: status=%d challenge=%q body=%s", resp.StatusCode, resp.Header.Get("WWW-Authenticate"), body)
			}
			var got struct {
				ID    json.RawMessage `json:"id"`
				Error struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &got) != nil || got.Error.Code != 1001 || !bytes.Equal(bytes.TrimSpace(got.ID), []byte("null")) {
				t.Fatalf("unauthenticated error shape changed: %s", body)
			}
			t.Logf("real local TCP, no auth/cookie/version header: HTTP=%d challenge=%q error_code=%d", resp.StatusCode, resp.Header.Get("WWW-Authenticate"), got.Error.Code)
		})
	}
	if seats.authCalls != 0 || seats.calls != 0 {
		t.Fatalf("unauthenticated four baselines touched seats: auth=%d business=%d", seats.authCalls, seats.calls)
	}
}
