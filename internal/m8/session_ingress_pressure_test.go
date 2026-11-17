package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// One Hub, 22 loopback WS clients. These are load generators, not 22 model or
// identity services. Real legacy-helper semantics are tested separately.
func TestSessionIngressTwentyTwoConnectionsBoundedWindow(t *testing.T) {
	h, _, ctx := newTestHub(t)
	h.sessionJournal = func([]byte) bool { return true }
	useTestSessionKeepalive(t, h, 100*time.Millisecond, 5*time.Second)
	if err := h.UpsertService(ctx, model.RegisteredService{ID: "load", Name: "load", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := h.IssueAPIKey(ctx, "load", "loaduser")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var peers []*websocket.Conn
	var clients []*sessionClient
	defer func() {
		for _, p := range peers {
			p.CloseNow()
		}
	}()
	for i := 0; i < 22; i++ {
		name := fmt.Sprintf("load%02d", i)
		p := dialSession(t, ctx, srv.URL, name, key.ID, secret)
		peers = append(peers, p)
		go func(p *websocket.Conn) {
			for {
				if _, _, err := p.Read(ctx); err != nil {
					return
				}
			}
		}(p)
		var c *sessionClient
		waitFor(t, time.Second, "load registration", func() bool { h.mu.Lock(); defer h.mu.Unlock(); c = h.sessionClients[key.ID][name]; return c != nil })
		clients = append(clients, c)
		raw, _ := json.Marshal(model.Envelope{ID: "load-caps", From: sessionAddress(name, key.ID), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}})
		if err := p.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
	}
	var before, during runtime.MemStats
	runtime.ReadMemStats(&before)
	h.mu.Lock()
	locked := true
	defer func() {
		if locked {
			h.mu.Unlock()
		}
	}()
	var wg sync.WaitGroup
	errs := make(chan error, len(peers))
	start := time.Now()
	for i, p := range peers {
		wg.Add(1)
		go func(i int, p *websocket.Conn) {
			defer wg.Done()
			raw, _ := json.Marshal(model.Envelope{ID: "load-event", From: sessionAddress(fmt.Sprintf("load%02d", i), key.ID), Body: `{"kind":"assistant_text","text":"` + strings.Repeat("x", 4096) + `"}`, Meta: map[string]any{"type": "session_event"}})
			for n := 0; n < 1000; n++ {
				if err := p.Write(ctx, websocket.MessageText, raw); err != nil {
					errs <- err
					return
				}
			}
		}(i, p)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("load writers stalled before approved window")
	}
	select {
	case err := <-errs:
		t.Fatal("load generator failed", err)
	default:
	}
	// Reader must continue accepting while all business workers wait for h.mu.
	waitFor(t, 10*time.Second, "all 22 approved windows resident", func() bool {
		h.ingressBudgetMu.Lock()
		defer h.ingressBudgetMu.Unlock()
		return h.ingressBudgetItems >= 22000
	})
	h.ingressBudgetMu.Lock()
	items, bytes := h.ingressBudgetItems, h.ingressBudgetBytes
	h.ingressBudgetMu.Unlock()
	runtime.ReadMemStats(&during)
	if items > 65536 || bytes > 256<<20 {
		t.Fatal("global pressure budget exceeded", items, bytes)
	}
	h.mu.Unlock()
	locked = false
	waitFor(t, 20*time.Second, "pressure budget released after drain", func() bool {
		h.ingressBudgetMu.Lock()
		defer h.ingressBudgetMu.Unlock()
		return h.ingressBudgetItems == 0 && h.ingressBudgetBytes == 0
	})
	for _, c := range clients {
		c.ingress.mu.Lock()
		peak := c.ingress.peakItems
		c.ingress.mu.Unlock()
		if peak < 1000 || peak > 2048 {
			t.Fatal("per-client compatible window not exercised", peak)
		}
	}
	t.Logf("22x1000x4KiB raw_items=%d raw_bytes=%d heap_before=%d heap_resident=%d heap_sys=%d elapsed=%s (Go clients; not 22 real helpers; raw quota is not RSS)", items, bytes, before.HeapAlloc, during.HeapAlloc, during.HeapSys, time.Since(start))
}

// Explicit parent cancellation stays stronger than the wider physical budget.
func TestSessionWriteCancelledBeforePhysicalDoesNotCloseConnection(t *testing.T) {
	f := newACKV2Fixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.client.write(ctx, model.Envelope{Body: "must never hit wire"}); err == nil {
		t.Fatal("cancelled caller was written")
	}
	if err := f.client.write(f.ctx, model.Envelope{ID: "after-cancel", Meta: map[string]any{"type": "delivery_ack_confirm"}}); err != nil {
		t.Fatal("pre-write cancellation closed healthy socket", err)
	}
	if f.readEnvUntil(f.conn, time.Second, func(e model.Envelope) bool { return e.ID == "after-cancel" }) == nil {
		t.Fatal("healthy connection did not survive cancelled caller")
	}
}
