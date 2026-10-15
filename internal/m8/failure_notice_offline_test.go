package m8

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func seedOfflineFailure(t *testing.T, db *store.SQLite, id string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "source", SessionName: "sender", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionEndpointDeliveryACK(ctx, "source", "sender", true); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(model.Envelope{ID: "message-" + id, From: "sender#source", To: "never#target", Body: "task"})
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: id, KeyID: "target", SessionName: "never", Envelope: string(raw), SourceMessageID: "message-" + id, SourceType: "session_inbound", CreatedAt: now.Add(-31 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
}
func TestOfflineFailureNoticeStartupAndPeriodicWithoutReconnect(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seedOfflineFailure(t, db, "startup", now)
	h := New(db, ConsoleAuthDisabled())
	defer h.Close()
	n, err := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("startup"))
	if err != nil || n == nil || n.State != "queued" {
		t.Fatalf("startup did not persist offline notice %+v %v", n, err)
	}
	seedOfflineFailure(t, db, "periodic", now)
	ticks := make(chan time.Time, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); h.runCapabilityPendingCleanup(ctx, stop, ticks) }()
	ticks <- now
	waitFor(t, time.Second, "global periodic expiry without any endpoint connection", func() bool {
		n, _ := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("periodic"))
		return n != nil
	})
	close(stop)
	<-done
	d, _ := db.GetSessionDelivery(ctx, "periodic")
	if d.State != "dead" || d.LastError != "delivery_expired" {
		t.Fatalf("normal queue not globally expired %+v", d)
	}
}

func TestOfflineFailureNoticeActualSourceReconnectAndLostACK(t *testing.T) {
	h, db, ctx := newTestHub(t)
	if err := h.UpsertService(ctx, model.RegisteredService{ID: "notice-live", Name: "notice-live", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := h.IssueAPIKey(ctx, "notice-live", "owner")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", h.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	connect := func() *websocket.Conn {
		c := dialSessionWithHeaderRaw(t, ctx, srv.URL, "source", key.ID, secret, nil)
		if err := writeEnvelope(ctx, c, model.Envelope{ID: "notice-caps", To: sessionAddress("workpulse", key.ID), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, time.Second, "source ACK capability", func() bool {
			h.mu.Lock()
			sc := h.sessionClients[key.ID]["source"]
			h.mu.Unlock()
			enabled, found, err := db.SessionEndpointDeliveryACK(ctx, key.ID, "source")
			return sc != nil && h.sessionSupportsDeliveryACK(sc) && err == nil && found && enabled
		})
		return c
	}
	first := connect()
	first.Close(websocket.StatusNormalClosure, "offline")
	waitFor(t, time.Second, "source offline", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.sessionClients[key.ID]["source"] == nil })
	now := time.Now().UTC()
	raw, _ := json.Marshal(model.Envelope{ID: "notice-live-original", From: sessionAddress("source", key.ID), To: sessionAddress("never", key.ID), Body: "task"})
	original := model.SessionDelivery{ID: "notice-live-failed", KeyID: key.ID, SessionName: "never", Envelope: string(raw), SourceMessageID: "notice-live-original", SourceType: "session_inbound", CreatedAt: now.Add(-31 * time.Minute)}
	if err := db.EnqueueSessionDelivery(ctx, original); err != nil {
		t.Fatal(err)
	}
	h.expireCapabilityPendingDeliveries(ctx, now)
	noticeID := model.FailureNoticeDeliveryID(original.ID)
	n, _ := db.GetSessionDelivery(ctx, noticeID)
	if n == nil || n.State != "queued" {
		t.Fatal("offline notice not durable")
	}
	second := connect()
	got := readEnvelopeOfType(t, ctx, second, "delivery")
	if got.ID != noticeID || metaString(got.Meta, "delivery_id") != noticeID || metaString(got.Meta, "kind") != "reply" {
		t.Fatalf("unexpected notice metadata %+v", got.Meta)
	}
	// Drop the ACK and the transport. Lease release must retry the same notice.
	second.Close(websocket.StatusNormalClosure, "lost ACK")
	waitFor(t, time.Second, "source disconnected after dropped ACK", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.sessionClients[key.ID]["source"] == nil })
	third := connect()
	defer third.Close(websocket.StatusNormalClosure, "done")
	again := readEnvelopeOfType(t, ctx, third, "delivery")
	if metaString(again.Meta, "delivery_id") != noticeID || again.ID != got.ID {
		t.Fatal("reconnect generated a second notice ID")
	}
	if err := writeEnvelope(ctx, third, model.Envelope{ID: "notice-processed", To: sessionAddress("workpulse", key.ID), Meta: map[string]any{"type": "delivery_ack", "delivery_id": noticeID, "state": "processed"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "notice processed ACK", func() bool { n, _ := db.GetSessionDelivery(ctx, noticeID); return n != nil && n.State == "processed" })
}

func TestFailureNoticeRetryExhaustionCannotNotifyAgain(t *testing.T) {
	h, _, ctx := newTestHub(t)
	client := attachAMRNoticeClient(t, h, ctx, "notice-key", "sender")
	raw, _ := json.Marshal(model.Envelope{ID: "notice", From: "sender#notice-key", Body: "notice"})
	h.notifyDeliveryRetryExhausted(ctx, model.SessionDelivery{ID: "notice", KeyID: "notice-key", SessionName: "failed-target", SourceType: "failure_notice", Provenance: model.FailureNoticeProvenance, Envelope: string(raw)}, 30, "cli_unavailable")
	readCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, _, err := client.Read(readCtx); err == nil {
		t.Fatal("failure notice generated another notice")
	}
}
