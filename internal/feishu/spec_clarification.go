package feishu

// ReplyApplication exposes only the configured non-secret application ID.
// FCS/proxy adapters without a verified inbound service proof intentionally do
// not implement this capability; their D3 notices remain Console-only.
func (g *LarkGateway) ReplyApplication(issuer string) (string, bool) {
	if g == nil || issuer == "" || issuer != g.botChannelID || g.appID == "" {
		return "", false
	}
	return g.appID, true
}
func (m *MultiGateway) ReplyApplication(issuer string) (string, bool) {
	if m == nil || issuer == "" {
		return "", false
	}
	m.mu.RLock()
	g := m.gateways[issuer]
	m.mu.RUnlock()
	if g == nil {
		return "", false
	}
	return g.ReplyApplication(issuer)
}
