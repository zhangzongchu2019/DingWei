package m8

import (
	"context"
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

type r31EndpointRecorder struct {
	store.Repository
	writes chan model.SessionEndpoint
}

func (r *r31EndpointRecorder) UpsertSessionEndpoint(ctx context.Context, ep model.SessionEndpoint) error {
	err := r.Repository.UpsertSessionEndpoint(ctx, ep)
	if err == nil && ep.KeyID == "u_zzc" && ep.Active {
		select {
		case r.writes <- ep:
		default:
		}
	}
	return err
}

// Real localhost WS admission: failed credentials and duplicate live sessions
// must not change private source metadata; disconnect retains the last value.
func TestR31SourceRecordedOnlyAfterAdmissionAndRetained(t *testing.T) {
	e := newAgentEnv(t)
	e.hub.TrustProxy = true
	if err := e.hub.Repo.UpsertSessionEndpoint(e.ctx, model.SessionEndpoint{KeyID: "u_zzc", OwnerKey: "u_zzc", SessionName: "opus50ts", FullSessionName: "Opus50TS.zzc", ClientIP: "203.0.113.77"}); err != nil {
		t.Fatal(err)
	}
	endpointsBefore, err := e.hub.Repo.ListSessionEndpoints(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range endpointsBefore {
		if endpoint.ClientIP != "" {
			t.Fatal("historical offline source leaked into directory")
		}
	}
	recorder := &r31EndpointRecorder{Repository: e.hub.Repo, writes: make(chan model.SessionEndpoint, 4)}
	e.hub.Repo = recorder
	dial := func(cred, ip string) (*websocket.Conn, int) {
		h := http.Header{"Authorization": []string{"Bearer " + cred}, "X-Real-IP": []string{ip}}
		c, r, err := websocket.Dial(e.ctx, e.wsURL("version=0.3.0"), &websocket.DialOptions{HTTPHeader: h})
		if err == nil {
			return c, http.StatusSwitchingProtocols
		}
		if r == nil {
			t.Fatal("local handshake failed without HTTP response")
		}
		r.Body.Close()
		return nil, r.StatusCode
	}
	read := func() identity.AgentConnection {
		v, err := e.svc.Store.ReadAgentConnection(e.ctx, "zzc", e.agID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if c, code := dial("invalid-fixture", "203.0.113.1"); c != nil || code != 401 {
		t.Fatalf("bad credential admitted: %d", code)
	}
	if read().IP != "" {
		t.Fatal("failed authentication wrote source IP")
	}
	c, code := dial(e.cred, "198.51.100.20")
	if c == nil {
		t.Fatalf("valid local admission: %d", code)
	}
	defer c.CloseNow()
	reader(c)
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	deadline := time.Now().Add(3 * time.Second)
	for read().IP != "198.51.100.20" {
		if time.Now().After(deadline) {
			t.Fatal("admitted source was not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	endpoints, err := e.hub.Repo.ListSessionEndpoints(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range endpoints {
		if endpoint.ClientIP != "" {
			t.Fatal("private source leaked into general session directory")
		}
	}
	select {
	case admitted := <-recorder.writes:
		if !admitted.PrivateClientIP || admitted.ClientIP != "" {
			t.Fatal("credential admission omitted private IP clear intent")
		}
	case <-time.After(time.Second):
		t.Fatal("credential admission was not observed")
	}
	before := read()
	if duplicate, code := dial(e.cred, "203.0.113.99"); duplicate != nil || code != 409 {
		t.Fatalf("duplicate admission: %d", code)
	}
	if got := read(); got != before {
		t.Fatal("rejected admission replaced source IP")
	}
	c.CloseNow()
	deadline = time.Now().Add(3 * time.Second)
	for e.hub.AgentOnline(e.agID) {
		if time.Now().After(deadline) {
			t.Fatal("local disconnect not observed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := read(); got != before {
		t.Fatal("disconnect erased source IP")
	}
	c2, code := dial(e.cred, "2001:db8::20")
	if c2 == nil {
		t.Fatalf("reconnect: %d", code)
	}
	defer c2.CloseNow()
	reader(c2)
	deadline = time.Now().Add(3 * time.Second)
	for read().IP != "2001:db8::20" {
		if time.Now().After(deadline) {
			t.Fatal("reconnect did not refresh source IP")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
