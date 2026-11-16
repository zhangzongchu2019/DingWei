package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestViewerOutputSlowLockIsolatedAndWriteDeadlineIncludesLock(t *testing.T) {
	h, _, ctx := newTestHub(t)
	viewers := make(chan *terminalViewer, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		slot := make(chan struct{}, 1)
		if r.URL.Path != "/slow" {
			slot <- struct{}{}
		}
		v := &terminalViewer{conn: conn, mu: slot}
		h.startViewerOutput(r.Context(), v)
		defer v.output.cancel()
		viewers <- v
		for {
			if _, _, err = conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	slow, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/slow", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.CloseNow()
	sv := <-viewers
	fast, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/fast", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.CloseNow()
	fv := <-viewers
	sv.output.enqueue("must not block fast viewer")
	fv.output.enqueue("healthy frame")
	readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, raw, err := fast.Read(readCtx)
	if err != nil {
		t.Fatal("healthy viewer blocked behind slow viewer", err)
	}
	var got map[string]any
	if json.Unmarshal(raw, &got) != nil || got["data"] != "healthy frame" {
		t.Fatal("output changed", string(raw))
	}
	select {
	case <-sv.output.done:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("viewer write lock wait was unbounded")
	}
	if !sv.outputGap.Load() {
		t.Fatal("lost display output lacks reconnect gap marker")
	}
	// The lock stays held: only the total write budget can release the worker.
	select {
	case <-sv.mu:
		t.Fatal("test lost held write slot")
	default:
	}
	fresh, stopFresh := context.WithTimeout(ctx, time.Second)
	defer stopFresh()
	fv.output.enqueue("still alive")
	_, _, err = fast.Read(fresh)
	if err != nil {
		t.Fatal("slow viewer closed healthy viewer", err)
	}
}

func TestViewerOutputSessionOfflineKeepsLegacyStatusAndNormalClose(t *testing.T) {
	h, _, ctx := newTestHub(t)
	if err := h.UpsertService(ctx, model.RegisteredService{ID: "offline", Name: "offline", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := h.IssueAPIKey(ctx, "offline", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
	mux.HandleFunc("GET /ws/view/{sessionName}", h.HandleTerminalViewWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	session := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer session.CloseNow()
	waitSessionOnline(t, h, key.ID, "home")
	view := dialTerminalView(t, ctx, srv.URL, "home")
	defer view.CloseNow()
	// A real registered legacy viewer, not merely a teardown-helper fixture.
	_ = readTerminalViewMessage(t, ctx, view)
	_ = session.CloseNow()
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		_, raw, err := view.Read(deadline)
		if err != nil {
			t.Fatal("Agent offline lost legacy status frame", err)
		}
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			t.Fatal("invalid terminal frame")
		}
		if frame["type"] == "status" && frame["message"] == "会话已离线" {
			if frame["readonly"] != true {
				t.Fatal("offline terminal remained writable")
			}
			break
		}
	}
	_, _, err = view.Read(deadline)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("Agent offline lost normal close code: %v", err)
	}
}

func TestViewerOutputOfflineSlowViewerDoesNotHoldHealthyOrAgent(t *testing.T) {
	h, _, ctx := newTestHub(t)
	ready := make(chan *terminalViewer, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		v := &terminalViewer{conn: conn, mu: make(chan struct{}, 1)}
		if r.URL.Path != "/slow" {
			v.mu <- struct{}{}
		}
		ready <- v
		_, _, _ = conn.Read(r.Context())
	}))
	defer srv.Close()
	slow, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/slow", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.CloseNow()
	sv := <-ready
	fast, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/fast", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.CloseNow()
	fv := <-ready
	start := time.Now()
	h.closeOfflineTerminalViewers([]*terminalViewer{sv, fv})
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("viewer teardown blocked Agent cleanup")
	}
	readCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	frame := readTerminalViewMessage(t, readCtx, fast)
	if frame["message"] != "会话已离线" {
		t.Fatal("slow viewer swallowed healthy offline status", frame)
	}
	_, _, err = fast.Read(readCtx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatal("healthy viewer did not close normally", err)
	}
	slowCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	if _, _, err = slow.Read(slowCtx); err == nil {
		t.Fatal("held-slot slow viewer was not disconnected")
	}
	if !sv.outputGap.Load() {
		t.Fatal("failed offline notification lacks visible gap")
	}
}
func TestViewerOutputSealedLifecycleClosesWithVisibleGap(t *testing.T) {
	h, _, ctx := newTestHub(t)
	h.lifetime().background.Seal()
	ready := make(chan *terminalViewer, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		v := &terminalViewer{conn: conn, mu: make(chan struct{}, 1)}
		v.mu <- struct{}{}
		h.startViewerOutput(r.Context(), v)
		ready <- v
	}))
	defer srv.Close()
	peer, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.CloseNow()
	v := <-ready
	v.output.enqueue("must not disappear without a gap")
	if !v.outputGap.Load() {
		t.Fatal("sealed background group silently discarded display output")
	}
	select {
	case <-v.output.done:
	default:
		t.Fatal("sealed lifecycle left a viewer worker running")
	}
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, _, err = peer.Read(deadline); err == nil {
		t.Fatal("sealed lifecycle left terminal connected")
	}
}

func TestViewerOutputOverflowIsBoundedAndVirtualWriteIsNoop(t *testing.T) {
	if terminalWrite(context.Background(), &terminalViewer{}, map[string]any{"data": "virtual"}) != nil {
		t.Fatal("virtual viewer attempted network write")
	}
	h, _, ctx := newTestHub(t)
	ready := make(chan *terminalViewer, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		v := &terminalViewer{conn: c, mu: make(chan struct{}, 1)}
		h.startViewerOutput(r.Context(), v)
		defer v.output.cancel()
		ready <- v
		for {
			if _, _, e = c.Read(r.Context()); e != nil {
				return
			}
		}
	}))
	defer srv.Close()
	c, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	v := <-ready
	v.output.enqueue(strings.Repeat("x", viewerOutputByteLimit+1))
	select {
	case <-v.output.done:
	case <-time.After(time.Second):
		t.Fatal("overflow left viewer worker behind")
	}
	v.output.mu.Lock()
	stopped, bytes := v.output.stopped, v.output.bytes
	v.output.mu.Unlock()
	if !v.outputGap.Load() {
		t.Fatal("overflow lost visible gap marker")
	}
	if !stopped || bytes != 0 {
		t.Fatal("overflow memory not reclaimed", stopped, bytes)
	}
}
func TestSessionWriteWaitHasTotalCancellationBudget(t *testing.T) {
	c := &sessionClient{}
	c.writeGateOnce.Do(func() { c.writeGate = make(chan struct{}, 1) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.write(ctx, errorEnvelope("fixture", "fixture", "", "test")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("session lock wait ignored caller deadline", err)
	}
}

func TestSessionWriteDefaultBudgetIncludesContendedSlot(t *testing.T) {
	h, _, ctx := newTestHub(t)
	_ = h
	ready := make(chan *sessionClient, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		c := &sessionClient{conn: conn}
		c.writeGateOnce.Do(func() { c.writeGate = make(chan struct{}, 1) })
		ready <- c
		for {
			if _, _, err = conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	peer, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.CloseNow()
	c := <-ready
	call, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		defer close(finished)
		result <- c.write(call, errorEnvelope("fixture", "fixture", "", "bounded status"))
	}()
	defer func() {
		cancel()
		select {
		case c.writeGate <- struct{}{}:
		default:
		}
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("session writer not reclaimed")
		}
	}()
	select {
	case err = <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("missing default write deadline", err)
		}
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("default session write budget excluded lock wait")
	}
}
