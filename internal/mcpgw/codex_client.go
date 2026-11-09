package mcpgw

import (
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const codexClientPath = "/mcp/clients/codex.json"

const codexDefaultClientID = "https://chatgpt.com/oauth/codex/client.json"

// Fetched anonymously 2026-11-09T02:14:27Z, HTTP 200; byte-exact upstream
// SHA256 88c1677cc7dc907c07184571bba9ecaa342904424823d883270f9c8f62940004.
// Keep provenance intact. Effective policy below intersects the original with
// our native public-client, IP-loopback, authorization-code-only policy.
//
//go:embed codex_client.original.json
var codexOriginalMetadata []byte

// Ruling ㊿-补20: native desktop callbacks may carry one opaque path segment.
// Only the two exact, locally configured Codex identities use this matcher.
var codexCallbackPath = regexp.MustCompile(`^/callback(?:/[A-Za-z0-9_-]{1,64})?$`)

func registeredCodexRedirect(c Client, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !validRedirect(raw) || !codexCallbackPath.MatchString(u.EscapedPath()) {
		return false
	}
	// Reuse the registered IP/scheme/port/query/fragment/userinfo guards. Only
	// the permitted callback path is normalized for registration matching;
	// authorization and token exchange retain and compare the ORIGINAL URI.
	u.Path, u.RawPath = "/callback", ""
	// url.Parse lowercases schemes; retain the original scheme spelling so the
	// existing exact-scheme guard cannot be bypassed through normalization.
	return registered(c, strings.SplitN(raw, ":", 2)[0]+":"+strings.SplitN(u.String(), ":", 2)[1])
}

func defaultCodexClient() (Client, error) {
	var c Client
	if json.Unmarshal(codexOriginalMetadata, &c) != nil || c.ID != codexDefaultClientID || c.AuthMethod != "none" {
		return Client{}, errParams
	}
	var redirects []string
	for _, raw := range c.Redirects {
		u, err := url.Parse(raw)
		if err != nil || !validRedirect(raw) {
			continue
		}
		ip := net.ParseIP(u.Hostname())
		if ip != nil && ip.IsLoopback() && u.Scheme == "http" {
			redirects = append(redirects, raw)
		}
	}
	c.Redirects = redirects
	var grants, responses []string
	for _, grant := range c.Grants {
		if grant == "authorization_code" {
			grants = append(grants, grant)
		}
	}
	for _, response := range c.Responses {
		if response == "code" {
			responses = append(responses, response)
		}
	}
	c.Grants, c.Responses = grants, responses
	if len(grants) != 1 || len(responses) != 1 || validateClient(c, codexDefaultClientID) != nil {
		return Client{}, errParams
	}
	return c, nil
}

// This one document is server-owned configuration, not a user-supplied fetch
// target. Resolving it locally avoids hairpin DNS and never permits arbitrary
// private CIMD URLs. Other client IDs still use the configured external resolver.
func (g *Gateway) codexClient() Client {
	return Client{ID: g.cfg.PublicOrigin + codexClientPath, Name: "DingWei Codex public client", Redirects: []string{"http://127.0.0.1/callback", "http://[::1]/callback"}, AuthMethod: "none", Grants: []string{"authorization_code"}, Responses: []string{"code"}}
}
func (g *Gateway) codexClientMetadata(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	writeJSON(w, 200, g.codexClient())
}
