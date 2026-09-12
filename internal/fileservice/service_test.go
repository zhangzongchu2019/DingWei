package fileservice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServiceSeparatesPublicAndInternalHandlers(t *testing.T) {
	m, _, _ := testManager(t, 100)
	s := &Service{Manager: m, Capabilities: NewGrantStore(m.Clock, time.Minute), UploadTickets: NewUploadTicketStore(m.Clock, time.Minute), IssuerToken: "issuer", AuditToken: "audit"}
	for _, tc := range []struct {
		handler http.Handler
		path    string
		want    int
	}{
		{s.PublicHandler(), "/internal/files/issue/upload", http.StatusNotFound},
		{s.InternalHandler(), "/readyz", http.StatusNoContent},
		{s.InternalHandler(), "/healthz", http.StatusNoContent},
	} {
		w := httptest.NewRecorder()
		tc.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("path=%s status=%d", tc.path, w.Code)
		}
	}
}

func TestServiceSweeperRunsAndStopsWithContext(t *testing.T) {
	m, clock, _ := testManager(t, 100)
	f, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: "x", ContentLength: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	clock.Time = clock.Time.Add(61 * 24 * time.Hour)
	s := &Service{Manager: m}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunSweeper(ctx, time.Millisecond, nil); close(done) }()
	deadline := time.Now().Add(time.Second)
	for {
		got, err := m.Repo.Get(context.Background(), f.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == StateExpired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sweeper did not expire file: %+v", got)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sweeper ignored cancellation")
	}
}
