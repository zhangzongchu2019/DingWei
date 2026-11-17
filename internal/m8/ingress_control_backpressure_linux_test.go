//go:build linux

package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// A real TCP send buffer, not a sleeping Write mock. The counter witnesses the
// physical write, separately from the application write gate and WS writer.
type ingressControlBufferConn struct {
	net.Conn
	armed  *atomic.Bool
	active *atomic.Int32
}

func (c *ingressControlBufferConn) Write(p []byte) (int, error) {
	if c.armed.Load() {
		c.active.Add(1)
		defer c.active.Add(-1)
	}
	return c.Conn.Write(p)
}

type ingressControlBufferListener struct {
	net.Listener
	armed  *atomic.Bool
	active *atomic.Int32
}

func (l *ingressControlBufferListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if err = c.(*net.TCPConn).SetWriteBuffer(4096); err != nil {
		c.Close()
		return nil, err
	}
	return &ingressControlBufferConn{c, l.armed, l.active}, nil
}

func TestLegacyIngressSmallControlBlockedPhysicalWrite(t *testing.T) {
	e := newAgentEnv(t)
	e.srv.Close()
	armed := &atomic.Bool{}
	active := &atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", e.hub.HandleAgentWS)
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener = &ingressControlBufferListener{srv.Listener, armed, active}
	srv.Start()
	t.Cleanup(srv.Close)
	e.srv = srv
	logs := &sessionDiagRecorder{}
	e.hub.sessionJournal = logs.write
	secret, _ := e.enroll("rif-control-buffer", "Legacy")
	child := startLegacyIngress(t, e, secret)
	e.hub.mu.Lock()
	c := e.hub.sessionClients[AgentKeyID("zzc")]["legacy"]
	e.hub.mu.Unlock()
	if c == nil {
		t.Fatal("legacy connection absent")
	}
	child.send(t, map[string]any{"op": "pause_control_buffer"})
	child.want(t, "RIF_PAUSED", time.Second)
	// Pre-fill using a complete legal application message through the same real
	// route/write gate. Never bypass the gate or interleave application messages.
	filler := strings.Repeat("W", 96<<10)
	fillCtx, fillCancel := context.WithTimeout(e.ctx, time.Second)
	err := e.hub.routeToSession(fillCtx, c.keyID, c.sessionName, model.Envelope{ID: "rif-large", Body: filler, Meta: map[string]any{"type": "terminal_resize", "rif_fixture": true}}, false)
	fillCancel()
	if err != nil {
		t.Fatal("prefill must finish before the measured control write", err)
	}
	control := model.Envelope{ID: "rif-small-paused", From: sessionAddress("workpulse", c.keyID), To: sessionAddress(c.sessionName, c.keyID), Meta: map[string]any{"type": "terminal_resize", "rif_fixture": true, "padding": strings.Repeat("C", 60<<10)}}
	encoded, err := json.Marshal(control)
	if err != nil || len(encoded) > 64<<10 {
		t.Fatal("fixture is not a small control message", len(encoded), err)
	}
	armed.Store(true)
	call, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- e.hub.routeToSession(call, c.keyID, c.sessionName, control, false) }()
	// The peer remains paused until this explicit command; observe a real blocked
	// kernel write beyond the old two-second budget before permitting recovery.
	select {
	case err := <-done:
		t.Fatalf("small control did not survive physical backpressure beyond 2s: %v", err)
	case <-time.After(2800 * time.Millisecond):
	}
	if active.Load() != 1 {
		t.Fatalf("not blocked in underlying TCP Write: active=%d", active.Load())
	}
	time.Sleep(700 * time.Millisecond)
	child.send(t, map[string]any{"op": "resume_control_buffer"})
	child.want(t, "RIF_RESUMED", time.Second)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("5s control budget failed after real buffered pause", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("control failed to recover")
	}
	sum := sha256.Sum256([]byte(filler))
	child.want(t, "RIF_LARGE "+hex.EncodeToString(sum[:]), 3*time.Second)
	child.want(t, "RIF_SMALL", 3*time.Second)
	child.send(t, map[string]any{"op": "state"})
	child.want(t, `RIF_STATE {"alive": true}`, time.Second)
	if logs.has("session_physical_write_failed") {
		t.Fatal("control recovery recorded physical failure")
	}
	t.Logf("real TCP write blocked beyond 2.8s; resumed at3.5s control_bytes=%d elapsed=%s filler_sha256=%x", len(encoded), time.Since(started), sum)
	child.stop(t)
}
