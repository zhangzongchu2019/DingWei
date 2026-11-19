package mcpgw

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

type dcrResolverCounter struct {
	calls  int
	client Client
}

func (c *dcrResolverCounter) Resolve(context.Context, string) (Client, error) {
	c.calls++
	return c.client, nil
}

func TestDCRAuthorizeIsolationActualRedirectAndClosedSnapshots(t *testing.T) {
	s, p, _ := dcrPG(t, false)
	ctx := context.Background()
	g, _, _ := fixture(t)
	d, e := NewDCR(DCRPolicy{Enabled: true}, s)
	if e != nil {
		t.Fatal(e)
	}
	g.cfg.DCR = d
	resolver := &dcrResolverCounter{client: Client{ID: clientID, Name: "Existing CIMD", Redirects: []string{clientRedirect}, AuthMethod: "none", Grants: []string{"authorization_code"}, Responses: []string{"code"}}}
	g.cfg.Clients = resolver
	mux := http.NewServeMux()
	g.Register(mux)
	c, _, _, e := s.Register(ctx, "source", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	q := authorizationQuery()
	q.Set("client_id", c.ID)
	request := func() int {
		return serve(mux, httptest.NewRequest("GET", testOrigin+"/oauth/authorize?"+q.Encode(), nil)).Code
	}
	if request() != 302 {
		t.Fatal("persisted public client was not usable")
	}
	q.Set("redirect_uri", "http://127.0.0.1:54321/callback")
	if request() != 302 {
		t.Fatal("native dynamic port was rejected")
	}
	for _, uri := range []string{"http://127.0.0.1:/callback", "http://127.0.0.1:0/callback", "http://127.0.0.1:54321/callback#", "http://localhost:54321/callback", "http://127.0.0.1:54321/else"} {
		q.Set("redirect_uri", uri)
		if request() != 400 {
			t.Errorf("unsafe actual redirect accepted %s", uri)
		}
	}
	q.Set("redirect_uri", clientRedirect)
	q.Set("client_id", dcrPrefix+"missing")
	if request() != 400 || resolver.calls != 0 {
		t.Fatal("unknown opaque client fetched CIMD")
	}
	q.Set("client_id", c.ID)
	dcrExec(t, p, `UPDATE mcpgw_dcr_client SET expires_ns=0 WHERE client_id=$1`, c.ID)
	if request() != 400 || resolver.calls != 0 {
		t.Fatal("expired client revived or fell through CIMD")
	}
	offLive, _, _, e := s.Register(ctx, "source", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	d.Policy.Enabled = false
	q.Set("client_id", clientID)
	if request() != 302 || resolver.calls != 1 {
		t.Fatal("DCR shutdown disabled existing CIMD")
	}
	q.Set("client_id", offLive.ID)
	if request() != 400 {
		t.Fatal("DCR shutdown allowed new authorization")
	}
	metadata := serve(mux, httptest.NewRequest("GET", testOrigin+"/.well-known/oauth-authorization-server", nil))
	if strings.Contains(metadata.Body.String(), "registration_endpoint") || !strings.Contains(metadata.Body.String(), `"client_id_metadata_document_supported":true`) {
		t.Fatal("DCR shutdown advertised registration or disabled CIMD discovery")
	}
	d.Policy.Enabled = true
	d.Policy.HTTPSRedirects = []string{"https://vendor.example/cb"}
	allowed := dcrClient(t)
	allowed.Redirects = []string{"https://vendor.example/cb"}
	https, _, _, e := s.Register(ctx, "source", allowed)
	if e != nil {
		t.Fatal(e)
	}
	q.Set("client_id", https.ID)
	q.Set("redirect_uri", allowed.Redirects[0])
	if request() != 302 {
		t.Fatal("exact listed HTTPS redirect rejected")
	}
	d.Policy.HTTPSRedirects = nil
	if request() != 400 {
		t.Fatal("authorization ignored current HTTPS allowlist")
	}
}

func TestDCRDBDeadlineAndCancelledAdmission(t *testing.T) {
	s, p, _ := dcrPG(t, false)
	ctx := context.Background()
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- p.InTx(ctx, testAccess(t), nil, func(tx repo.Tx) error {
			if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "mcpgw-dcr/"+s.resource); e != nil {
				return e
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case e := <-done:
		t.Fatal("could not acquire fixture lock", e)
	case <-time.After(3 * time.Second):
		t.Fatal("fixture lock timed out")
	}
	start := time.Now()
	admission := make(chan error, 1)
	go func() { admission <- s.Admit(ctx, "source") }()
	var e error
	released := false
	select {
	case e = <-admission:
	case <-time.After(3200 * time.Millisecond):
		close(release)
		released = true
		e = <-admission
	}
	elapsed := time.Since(start)
	if !released {
		close(release)
	}
	if x := <-done; x != nil {
		t.Fatal(x)
	}
	if e == nil || errors.Is(e, errDCRRate) || elapsed > 3500*time.Millisecond || elapsed < 1500*time.Millisecond {
		t.Fatalf("DCR DB work was not bounded: elapsed=%v err=%v", elapsed, e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_bucket`) != 0 {
		t.Fatal("timed-out registration committed partial admission")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if e = s.Admit(cancelled, "source"); e == nil {
		t.Fatal("cancelled request committed quota")
	}
}

func TestDCRHTTPBoundedSlotsAndSlowBody(t *testing.T) {
	s, _, _ := dcrPG(t, false)
	g, _, _ := fixture(t)
	d, _ := NewDCR(DCRPolicy{Enabled: true}, s)
	g.cfg.DCR = d
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	for i := 0; i < 32; i++ {
		d.slots <- struct{}{}
	}
	resp, _ := dcrRequest(t, server.Client(), "POST", server.URL+"/oauth/register", publicRegistration, "application/json")
	if resp.StatusCode != 503 {
		t.Fatal("DCR exceeded bounded processing slots")
	}
	for i := 0; i < 32; i++ {
		<-d.slots
	}
	u := strings.TrimPrefix(server.URL, "http://")
	early, e := net.Dial("tcp", u)
	if e != nil {
		t.Fatal(e)
	}
	early.SetDeadline(time.Now().Add(2 * time.Second))
	earlyStart := time.Now()
	_, e = early.Write([]byte("POST /oauth/register HTTP/1.1\r\nHost: " + u + "\r\nContent-Type: text/plain\r\nContent-Length: 100\r\n\r\n{"))
	if e != nil {
		early.Close()
		t.Fatal(e)
	}
	earlyBody := make([]byte, 4096)
	earlyN, earlyErr := early.Read(earlyBody)
	early.Close()
	if earlyErr != nil || !strings.Contains(string(earlyBody[:earlyN]), "415 Unsupported Media Type") || time.Since(earlyStart) > 1500*time.Millisecond {
		t.Fatalf("early rejection blocked draining an unread body: response=%q err=%v", earlyBody[:earlyN], earlyErr)
	}
	conn, e := net.Dial("tcp", u)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	if _, e = conn.Write([]byte("POST /oauth/register HTTP/1.1\r\nHost: " + u + "\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 4096)
	n, e := conn.Read(b)
	if e != nil || !strings.Contains(string(b[:n]), "400 Bad Request") || time.Since(start) > 4500*time.Millisecond {
		t.Fatalf("slow registration body not bounded: elapsed=%v response=%q err=%v", time.Since(start), b[:n], e)
	}
}
