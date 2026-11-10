package mcpgw

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const claudeCodeClientID = "https://claude.ai/oauth/claude-code-client-metadata"

// The client metadata bytes were supplied with S-MCP-CLAUDE-COMPAT and are a
// test fixture only; production still resolves the exact public CIMD normally.
//
//go:embed claude_code_client.original.json
var claudeCodeMetadataFixture []byte

func TestClaudeCodeCIMDResolveFixture(t *testing.T) {
	var requested string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = r.URL.String()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(claudeCodeMetadataFixture))), Header: make(http.Header)}, nil
	})}
	resolved, err := fetchClient(context.Background(), claudeCodeClientID, client)
	if err != nil {
		t.Fatalf("CLAUDE_CIMD_RESOLVE: %v", err)
	}
	if requested != claudeCodeClientID || resolved.ID != claudeCodeClientID || resolved.Name != "Claude Code" || resolved.AuthMethod != "none" ||
		!reflect.DeepEqual(resolved.Redirects, []string{"http://localhost/callback", "http://127.0.0.1/callback"}) ||
		!reflect.DeepEqual(resolved.Grants, []string{"authorization_code", "refresh_token"}) || !reflect.DeepEqual(resolved.Responses, []string{"code"}) {
		t.Fatalf("CLAUDE_CIMD_RESOLVE: requested=%q resolved=%+v", requested, resolved)
	}
}

func TestClaudeCodeLocalhostRedirectPortAndExactPath(t *testing.T) {
	var client Client
	if err := json.Unmarshal(claudeCodeMetadataFixture, &client); err != nil || validateClient(client, claudeCodeClientID) != nil {
		t.Fatalf("CLAUDE_LOCALHOST_REDIRECT: fixture invalid: %v", err)
	}
	for _, port := range []string{"3118", "49152", "65535"} {
		if !registered(client, "http://localhost:"+port+"/callback") {
			t.Fatalf("CLAUDE_LOCALHOST_REDIRECT: random port %s rejected", port)
		}
	}
	for _, redirect := range []string{
		"http://localhost:3118/callback/extra",
		"http://localhost:3118/callback-evil",
		"http://localhost:3118/callback/",
		"http://localhost:3118/callback?next=1",
	} {
		if registered(client, redirect) {
			t.Fatalf("CLAUDE_LOCALHOST_PATH_EXACT: accepted %s", redirect)
		}
	}
	for _, host := range []string{"localhost.", "localhost.localdomain", "localhost.evil.example", "127.0.0.1.localhost"} {
		if validRedirect("http://" + host + ":3118/callback") {
			t.Fatalf("CLAUDE_LOCALHOST_HOST_EXACT: accepted hostname %q", host)
		}
	}
	if !validRedirect("http://LOCALHOST:3118/callback") || validRedirect("https://LOCALHOST:3118/callback") {
		t.Fatal("CLAUDE_LOCALHOST_SCHEME_OR_CASE")
	}
}

func TestClaudeCodeGrantTypesPermitRefreshMetadataOnly(t *testing.T) {
	var client Client
	if err := json.Unmarshal(claudeCodeMetadataFixture, &client); err != nil || validateClient(client, claudeCodeClientID) != nil {
		t.Fatalf("CLAUDE_REFRESH_GRANT: auth_code + refresh_token rejected: %v", err)
	}
	client.Grants = []string{"refresh_token"}
	if validateClient(client, claudeCodeClientID) == nil {
		t.Fatal("CLAUDE_REFRESH_GRANT: refresh-only grant accepted")
	}
	client.Grants = []string{"authorization_code", "urn:example:unknown"}
	if validateClient(client, claudeCodeClientID) == nil {
		t.Fatal("CLAUDE_UNKNOWN_GRANT: unknown grant accepted")
	}
	client.Grants = []string{"authorization_code", "refresh_token"}
	if validateClient(client, claudeCodeClientID) != nil {
		t.Fatal("CLAUDE_REFRESH_GRANT: supported grant set rejected")
	}
}

func TestClaudeRefreshTokenGrantIsNotIssued(t *testing.T) {
	_, _, mux := oauthFixture(t)
	form := tokenForm("unused")
	form.Set("grant_type", "refresh_token")
	w := redeem(mux, form)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported_grant_type") || strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("CLAUDE_REFRESH_NOT_ISSUED: status=%d body=%s", w.Code, w.Body)
	}
}
