package consolemux

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGateThreePhaseOutputCutoff(t *testing.T) {
	for _, phase := range []string{"before_check", "after_check_before_write"} {
		t.Run(phase, func(t *testing.T) {
			start := time.Now()
			o := Owner{Nonce: "test", Generation: 1, Until: start.Add(3 * time.Second)}
			g := NewGate(o)
			v := g.Version()
			w := &deadlineWriter{}
			if phase == "after_check_before_write" && !g.CommitCheck(v, start, start.Add(time.Hour), o, map[string]string{"a": "1"}) {
				t.Fatal("check")
			}
			cut := time.Now()
			g.Revoke("a")
			if g.CommitCheck(v, start, start.Add(time.Hour), o, map[string]string{"a": "1"}) {
				t.Fatal("stale authorization resurrected")
			}
			if e := g.Write(w, "a", "1", []byte("data: sensitive\n\n")); !errors.Is(e, ErrDenied) || w.wrote != 0 {
				t.Fatal("queued output crossed revoke", e, w.wrote)
			}
			t.Logf("phase=%s revoke_to_denied=%s physical_writes=0", phase, time.Since(cut))
		})
	}
	t.Run("in_physical_write", func(t *testing.T) {
		var entered atomic.Int64
		var active atomic.Bool
		result := make(chan error, 1)
		cutDone := make(chan time.Duration, 1)
		var g *Gate
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			o := Owner{Nonce: "test", Generation: 1, Until: now.Add(3 * time.Second)}
			g = NewGate(o)
			g.CommitCheck(0, now, now.Add(time.Hour), o, map[string]string{"a": "1"})
			tap := &physicalCutWriter{ResponseWriter: w, enter: func() { entered.Store(time.Now().UnixNano()); active.Store(true) }, leave: func() { active.Store(false) }}
			for i := 0; i < 32; i++ {
				if e := g.Write(tap, "a", "1", []byte("data: "+strings.Repeat("x", (1<<20)-8)+"\n\n")); e != nil {
					result <- e
					return
				}
			}
			result <- nil
		}))
		defer srv.Close()
		c, e := net.Dial("tcp", srv.Listener.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		c.(*net.TCPConn).SetReadBuffer(1024)
		fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
		deadline := time.Now().Add(time.Second)
		for !(active.Load() && time.Since(time.Unix(0, entered.Load())) > 50*time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("socket never blocked inside physical write")
			}
			time.Sleep(time.Millisecond)
		}
		cut := time.Now()
		go func() { g.Revoke("a"); cutDone <- time.Since(cut) }()
		select {
		case d := <-cutDone:
			if d > 2*time.Second {
				t.Fatal("writer blocked revoke", d)
			}
			t.Logf("phase=in_physical_write revoke_gate_wait=%s deadline=%s", d, WriteLimit)
		case <-time.After(2 * time.Second):
			t.Fatal("revoke exceeded 2s")
		}
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("writer did not terminate")
		}
		if e := g.Write(&deadlineWriter{}, "a", "1", nil); !errors.Is(e, ErrDenied) {
			t.Fatal("later output admitted", e)
		}
	})
}

type physicalCutWriter struct {
	http.ResponseWriter
	enter func()
	leave func()
}

func (w *physicalCutWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *physicalCutWriter) Write(p []byte) (int, error) { w.enter(); return w.ResponseWriter.Write(p) }
