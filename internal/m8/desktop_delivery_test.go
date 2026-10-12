package m8

import (
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDesktopDurableReceiptNeverPublishesCLIInjection(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "desktop-fixture", Name: "desktop", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "desktop-fixture", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, home, model.Envelope{ID: "caps", To: "workpulse#" + key.ID, Meta: map[string]any{
		"type": "session_capabilities", "delivery_ack_v1": true, "desktop_inbox_v1": true, "processed_semantics": "saved_to_desktop_inbox"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "desktop capability", func() bool {
		hub.mu.Lock()
		c := hub.sessionClients[key.ID]["home"]
		hub.mu.Unlock()
		if c == nil {
			return false
		}
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		return c.desktopInboxV1
	})
	_, events, unsubscribe := hub.subscribeSessionEvents(key.ID, "home", "desktop-receipt")
	defer unsubscribe()
	if err := hub.RouteEnvelope(ctx, model.Envelope{ID: "desktop-message", To: "home#" + key.ID, From: "peer#" + key.ID, Body: "fixture", TS: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	delivery := readEnvelope(t, ctx, home)
	id := metaString(delivery.Meta, "delivery_id")
	<-events // input_posted
	// Even an injected acknowledgement cannot manufacture desktop model admission.
	for _, state := range []string{"injected", "processed"} {
		if err := writeEnvelope(ctx, home, model.Envelope{ID: "ack-" + state, To: "workpulse#" + key.ID, Meta: map[string]any{"type": "delivery_ack", "delivery_id": id, "state": state}}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case raw := <-events:
		if strings.Contains(raw, "cli_injected") || !strings.Contains(raw, `"delivery_stage":"helper_received"`) || !strings.Contains(raw, "desktop_inbox_saved") {
			t.Fatalf("false desktop execution evidence: %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("no durable receipt event")
	}
	waitFor(t, time.Second, "transport processed", func() bool { d, _ := db.GetSessionDelivery(ctx, id); return d != nil && d.State == "processed" })
	select {
	case raw := <-events:
		t.Fatalf("unexpected extra event: %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}
