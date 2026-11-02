package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Twenty real WS upgrades stop between registration admission and publishing
// their sockets. Closing must wait for publication; late admission is rejected.
func TestHLCRegistrationBarrierTwentyWebSockets(t *testing.T) {
	h, _, _ := newTestHub(t)
	entered := make(chan struct{}, 20)
	release := make(chan struct{})
	var releaseOnce sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /race", func(w http.ResponseWriter, r *http.Request) {
		reg, ended, ok := h.beginRegistration(w, "race_ws")
		if !ok {
			return
		}
		defer ended()
		defer reg()
		entered <- struct{}{}
		<-release
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		defer h.trackSocket(conn, "race_ws")()
		reg()
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	type result struct {
		conn *websocket.Conn
		err  error
	}
	clients := make(chan result, 20)
	for i := 0; i < 20; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/race", nil)
			clients <- result{conn, err}
		}()
	}
	for i := 0; i < 20; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("twenty registration requests not admitted")
		}
	}
	h.SealRegistrations()
	r := httptest.NewRecorder()
	reg, done, ok := h.beginRegistration(r, "late_ws")
	if ok {
		reg()
		done()
		t.Fatal("S005_REGISTRATION_AFTER_SEAL: late upgrade admitted")
	}
	if r.Code != 503 {
		t.Fatalf("late registration status=%d", r.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := h.WaitRegistrations(ctx)
	cancel()
	if err == nil {
		t.Fatal("S005_REGISTRATION_BARRIER: returned before accepted upgrades published")
	}
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < 20; i++ {
		r := <-clients
		if r.err != nil {
			t.Fatal(r.err)
		}
		reader(r.conn)
		t.Cleanup(func() { r.conn.CloseNow() })
	}
	closeCtx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	start := time.Now()
	if err := h.WaitRegistrations(closeCtx); err != nil {
		t.Fatal(err)
	}
	if err := h.CloseContext(closeCtx); err != nil {
		t.Fatal(err)
	}
	for kind, count := range h.LifecycleCounts() {
		if count != 0 {
			t.Fatalf("S005_WS_NOT_JOINED: %s=%d", kind, count)
		}
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("S005_WS_CLOSE_BUDGET")
	}
	t.Logf("S005 real_ws=20 paused_before_publish=20 late_rejected=1 late_registered=0 joined=true close_ms=%d", time.Since(start).Milliseconds())
}
