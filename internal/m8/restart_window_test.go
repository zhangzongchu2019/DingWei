package m8

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestRestartWindowSignalIsBounded(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	h := &Hub{startedAt: now.Add(-5 * time.Second), restartWindow: time.Minute}
	env := model.Envelope{Meta: map[string]any{"type": "delivery_failure"}}
	if !h.annotateRestartWindow(&env, now, "test") {
		t.Fatal("failure inside restart window was not annotated")
	}
	if env.Meta["within_restart_window"] != true || env.Meta["hub_uptime_seconds"] != int64(5) || env.Meta["restart_window_seconds"] != int64(60) {
		t.Fatalf("restart metadata=%+v", env.Meta)
	}

	h.startedAt = now.Add(-time.Minute)
	outside := model.Envelope{Meta: map[string]any{"type": "delivery_failure"}}
	if h.annotateRestartWindow(&outside, now, "test") {
		t.Fatal("failure at or outside restart boundary was annotated")
	}
	if _, exists := outside.Meta["within_restart_window"]; exists {
		t.Fatalf("outside metadata=%+v", outside.Meta)
	}
}

func TestSessionDeliverySucceedsOutsideRestartWindow(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	hub.startedAt = time.Now().Add(-2 * hub.restartWindow)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc", Name: "svc", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc", "owner")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	source := dialSession(t, ctx, srv.URL, "source", key.ID, secret)
	defer source.Close(websocket.StatusNormalClosure, "done")
	target := dialSession(t, ctx, srv.URL, "target", key.ID, secret)
	defer target.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, source, model.Envelope{ID: "outside-window", To: "target#" + key.ID, From: "source#" + key.ID, Body: "ping"}); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, ctx, target); got.Body != "ping" {
		t.Fatalf("outside-window delivery=%+v", got)
	}
}

func TestAMRFailureNotificationCarriesRestartWindowSignal(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	hub.startedAt = time.Now().Add(-5 * time.Second)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc-amr", Name: "svc-amr", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc-amr", "owner")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	source := dialSession(t, ctx, srv.URL, "source", key.ID, secret)
	defer source.Close(websocket.StatusNormalClosure, "done")

	p := testAMRProcessor()
	p.hub = hub
	p.notifySource(amrTurnKey{KeyID: key.ID, Session: "source", DeliveryID: "d", TurnID: "t"}, "#missing", "target offline", true)
	got := readEnvelope(t, ctx, source)
	if metaString(got.Meta, "type") != "delivery_failure" || !metaBool(got.Meta, "within_restart_window") {
		t.Fatalf("AMR failure notification=%+v", got)
	}
	if got.Meta["hub_uptime_seconds"] == nil || got.Meta["restart_window_seconds"] != float64(60) && got.Meta["restart_window_seconds"] != int64(60) {
		t.Fatalf("AMR restart metadata=%+v", got.Meta)
	}
}
