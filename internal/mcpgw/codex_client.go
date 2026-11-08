package mcpgw

import "net/http"

const codexClientPath = "/mcp/clients/codex.json"

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
