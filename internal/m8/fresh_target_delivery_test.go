package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestFreshTargetAdmissionQueuesBeforeCapabilityAndPromotesExactlyOnce(t *testing.T) {
	h, db, ctx := newTestHub(t)
	if err := h.UpsertService(ctx, model.RegisteredService{ID: "svc-fresh-target", Name: "fresh-target", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := h.IssueAPIKey(ctx, "svc-fresh-target", "person")
	if err != nil {
		t.Fatal(err)
	}
	const session = "fresh"
	registered := make(chan struct{})
	release := make(chan struct{})
	h.sessionBeforePublishHook = func(keyID, sessionName string) {
		if keyID == key.ID && sessionName == session {
			close(registered)
			<-release
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	type dialResult struct {
		conn *websocket.Conn
		err  error
	}
	dialed := make(chan dialResult, 1)
	go func() {
		conn := dialSessionWithHeaderRaw(t, ctx, srv.URL, session, key.ID, secret, nil)
		dialed <- dialResult{conn: conn}
	}()
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("fresh target did not reach registered-but-unpublished window")
	}
	keyID := key.ID
	env := model.Envelope{ID: "fresh-before-caps", From: "source#source-key#UnifiedRobot", To: session + "#" + keyID, Body: "deliver once", TS: time.Now().Unix()}
	if err := h.routeToSession(ctx, keyID, session, env, false); err != nil {
		t.Fatalf("registered-but-unpublished fresh target must queue: %v", err)
	}
	// Current and rollback-era claim filters both exclude capability_pending.
	if preCap, err := db.ClaimSessionDelivery(ctx, keyID, session, time.Now().UTC()); err != nil || preCap != nil {
		t.Fatalf("capability_pending must not be claimed before capability: %+v err=%v", preCap, err)
	}
	close(release)
	target := (<-dialed).conn
	defer target.Close(websocket.StatusNormalClosure, "done")
	_ = readEnvelopeIncludingSystem(t, ctx, target)
	if err := writeEnvelope(ctx, target, model.Envelope{ID: "caps-fresh", To: "workpulse#" + keyID, Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, target)
	if got.ID != env.ID || got.Body != env.Body || metaString(got.Meta, "type") != "delivery" || metaString(got.Meta, "delivery_id") == "" {
		t.Fatalf("fresh target delivery=%+v", got)
	}
	if err := writeEnvelope(ctx, target, model.Envelope{ID: "ack-fresh", To: "workpulse#" + keyID, Meta: map[string]any{"type": "delivery_ack", "delivery_id": metaString(got.Meta, "delivery_id"), "state": "processed"}}); err != nil {
		t.Fatal(err)
	}
	deliveryID := metaString(got.Meta, "delivery_id")
	waitFor(t, time.Second, "fresh target delivery processed once", func() bool {
		d, _ := db.GetSessionDelivery(ctx, deliveryID)
		return d != nil && d.State == "processed" && d.Attempts == 1
	})
}

func TestFreshTargetAdmissionDoesNotAdmitOrdinaryOfflineEndpoint(t *testing.T) {
	h, db, ctx := newTestHub(t)
	const keyID, session = "fresh-key", "offline"
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: keyID, SessionName: session, LastSeenAt: time.Now().UTC(), Active: false}); err != nil {
		t.Fatal(err)
	}
	err := h.routeToSession(context.Background(), keyID, session, model.Envelope{ID: "ordinary-offline", From: "source#source-key#UnifiedRobot", To: session + "#" + keyID, Body: "must fail", TS: time.Now().Unix()}, false)
	if err == nil {
		t.Fatal("ordinary offline endpoint unexpectedly admitted")
	}
}
