package consolemux

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A real HTTP/1.1 socket whose peer never reads must not hold the writer gate.
func TestGateRealHTTP1SlowPeerHasExecutableCutoff(t *testing.T) {
	done := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		o := Owner{Nonce: "synthetic", Generation: 1, Until: now.Add(3 * time.Second)}
		g := NewGate(o)
		g.CommitCheck(0, now, now.Add(time.Hour), o, map[string]string{"a": "1"})
		w.Header().Set("Content-Type", "text/event-stream")
		frame := []byte("data: " + strings.Repeat("x", (1<<20)-8) + "\n\n")
		for i := 0; i < 32; i++ {
			if err := g.Write(w, "a", "1", frame); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.(*net.TCPConn).SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		timeout, ok := err.(net.Error)
		if !ok || !timeout.Timeout() {
			t.Fatalf("writer did not reach real socket cutoff: %T", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked writer exceeded revocation budget")
	}
}
