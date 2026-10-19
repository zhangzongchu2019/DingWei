package m8

import (
	"net/http/httptest"
	"testing"
)

func TestR31AgentSourceIPTrustBoundary(t *testing.T) {
	for _, c := range []struct {
		name, remote, real, xff, want string
		trust                         bool
	}{
		{"direct forged", "203.0.113.9:123", "198.51.100.2", "198.51.100.3", "203.0.113.9", true},
		{"proxy disabled", "127.0.0.1:1", "198.51.100.2", "", "127.0.0.1", false},
		{"trusted nginx", "127.0.0.1:1", "198.51.100.2", "198.51.100.3", "198.51.100.2", true},
		{"ipv6 proxy", "[::1]:1", "2001:db8::2", "", "2001:db8::2", true},
		{"invalid single", "127.0.0.1:1", "evil.example", "", "127.0.0.1", true},
		{"list injection", "127.0.0.1:1", "198.51.100.2, 198.51.100.3", "", "127.0.0.1", true},
		{"forwarded only", "127.0.0.1:1", "", "198.51.100.3", "127.0.0.1", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := Hub{TrustProxy: c.trust}
			r := httptest.NewRequest("GET", "/ws/agent", nil)
			r.RemoteAddr = c.remote
			if c.real != "" {
				r.Header.Set("X-Real-IP", c.real)
			}
			r.Header.Set("X-Forwarded-For", c.xff)
			if got := h.agentSourceIP(r); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
	h := Hub{TrustProxy: true}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Add("X-Real-IP", "198.51.100.1")
	r.Header.Add("X-Real-IP", "198.51.100.2")
	if h.agentSourceIP(r) != "127.0.0.1" {
		t.Fatal("accepted duplicate trusted headers")
	}
}
