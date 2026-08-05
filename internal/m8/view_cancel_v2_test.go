package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestViewV2CancelAuthenticatesCancelsAndDeduplicates(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{
		ID: "dlv_pending", KeyID: "key", SessionName: "alice-dev55-2642", Envelope: `{}`,
		SourceMessageID: "msg_pending", SourceType: "view",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{
		ID: "dlv_other_session", KeyID: "other-key", SessionName: "other-session", Envelope: `{}`,
		SourceMessageID: "msg_other", SourceType: "view",
	}); err != nil {
		t.Fatal(err)
	}

	secret := []byte("abcdef0123456789abcdef0123456789")
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "vs-to-hub.key")
	if err := os.WriteFile(secretFile, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "routes.json")
	config := map[string]any{
		"sessions": map[string]string{"alice-dev55-2642": "canary"},
		"targets": map[string]any{"canary": map[string]any{
			"url": "http://127.0.0.1:19321", "view_unlock_v2": true,
			"vs_to_hub_secret_file": secretFile,
		}},
	}
	rawConfig, _ := json.Marshal(config)
	if err := os.WriteFile(configFile, rawConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", configFile)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/view-v2/{session}/cancel", hub.HandleInternalViewV2Cancel)
	path := "/internal/view-v2/alice-dev55-2642/cancel"
	body, _ := json.Marshal(viewV2CancelRequest{
		Session: "alice-dev55-2642", PageID: "page-1", RequestID: "0123456789abcdef0123456789abcdef",
		MessageID: "msg_pending", DeliveryID: "dlv_pending",
	})
	do := func(nonce string, requestBody []byte, direction string) *httptest.ResponseRecorder {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(requestBody))
		req.Header.Set("X-DW-VS-Target", "canary")
		req.Header.Set("X-DW-Timestamp", ts)
		req.Header.Set("X-DW-Nonce", nonce)
		req.Header.Set("X-DW-Signature", signViewServiceRequest(secret, direction, http.MethodPost, path, ts, nonce, requestBody))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}
	wrongMessageBody, _ := json.Marshal(viewV2CancelRequest{
		Session: "alice-dev55-2642", PageID: "page-1", RequestID: "6123456789abcdef0123456789abcdef",
		MessageID: "wrong", DeliveryID: "dlv_pending",
	})
	if got := do("66778899001122334455aabbccddeeff", wrongMessageBody, vsToHubDirection); got.Code != http.StatusConflict {
		t.Fatalf("wrong source message status=%d body=%s", got.Code, got.Body.String())
	}
	wrongSessionBody, _ := json.Marshal(viewV2CancelRequest{
		Session: "alice-dev55-2642", PageID: "page-1", RequestID: "7123456789abcdef0123456789abcdef",
		MessageID: "msg_other", DeliveryID: "dlv_other_session",
	})
	if got := do("77889900112233445566aabbccddeeff", wrongSessionBody, vsToHubDirection); got.Code != http.StatusConflict {
		t.Fatalf("wrong delivery session status=%d body=%s", got.Code, got.Body.String())
	}

	first := do("00112233445566778899aabbccddeeff", body, vsToHubDirection)
	if first.Code != http.StatusOK || !bytes.Contains(first.Body.Bytes(), []byte(`"state":"cancelled"`)) {
		t.Fatalf("first cancel status=%d body=%s", first.Code, first.Body.String())
	}
	delivery, err := db.GetSessionDelivery(ctx, "dlv_pending")
	if err != nil || delivery == nil || delivery.State != "cancelled" {
		t.Fatalf("delivery after cancel=%+v err=%v", delivery, err)
	}

	replay := do("00112233445566778899aabbccddeeff", body, vsToHubDirection)
	if replay.Code != http.StatusConflict {
		t.Fatalf("nonce replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	idempotent := do("ffeeddccbbaa99887766554433221100", body, vsToHubDirection)
	if idempotent.Code != http.StatusOK || !bytes.Contains(idempotent.Body.Bytes(), []byte(`"duplicate":true`)) {
		t.Fatalf("idempotent cancel status=%d body=%s", idempotent.Code, idempotent.Body.String())
	}

	changedBody, _ := json.Marshal(viewV2CancelRequest{
		Session: "alice-dev55-2642", PageID: "page-1", RequestID: "0123456789abcdef0123456789abcdef",
		MessageID: "msg_pending", DeliveryID: "dlv_other",
	})
	changed := do("11223344556677889900aabbccddeeff", changedBody, vsToHubDirection)
	if changed.Code != http.StatusConflict {
		t.Fatalf("same request id with changed body status=%d body=%s", changed.Code, changed.Body.String())
	}
	wrongDirection := do("1234567890abcdef1234567890abcdef", body, hubToVSDirection)
	if wrongDirection.Code != http.StatusUnauthorized {
		t.Fatalf("wrong direction status=%d body=%s", wrongDirection.Code, wrongDirection.Body.String())
	}

	invalidBodies := []viewV2CancelRequest{
		{Session: "alice-dev55-2642", RequestID: "1123456789abcdef0123456789abcdef", MessageID: "m1", DeliveryID: "d1"},
		{Session: "alice-dev55-2642", PageID: "p1", RequestID: "request-1", MessageID: "m1", DeliveryID: "d1"},
		{Session: "alice-dev55-2642", PageID: "p1", RequestID: "2123456789abcdef0123456789abcdef", DeliveryID: "d1"},
		{Session: "alice-dev55-2642", PageID: "p1", RequestID: "3123456789abcdef0123456789abcdef", MessageID: "m1"},
	}
	nonces := []string{
		"22334455667788990011aabbccddeeff", "33445566778899001122aabbccddeeff",
		"44556677889900112233aabbccddeeff", "55667788990011223344aabbccddeeff",
	}
	for i, invalid := range invalidBodies {
		raw, _ := json.Marshal(invalid)
		got := do(nonces[i], raw, vsToHubDirection)
		if got.Code != http.StatusBadRequest {
			t.Fatalf("invalid body %d status=%d body=%s", i, got.Code, got.Body.String())
		}
	}
}

func TestCancelSessionDeliveryCoreForwardsSentDelivery(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{
		ID: "dlv_sent", KeyID: "key", SessionName: "s1", Envelope: `{}`,
		SourceMessageID: "msg_sent", SourceType: "view",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSessionDelivery(ctx, "dlv_sent", "sent", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	accepted := make(chan *websocket.Conn, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-release
	}))
	defer server.Close()
	defer close(release)
	clientConn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.CloseNow()
	serverConn := <-accepted
	defer serverConn.CloseNow()

	hub.mu.Lock()
	hub.sessionClients["key"] = map[string]*sessionClient{"s1": {
		conn: serverConn, keyID: "key", sessionName: "s1",
	}}
	hub.mu.Unlock()
	result := hub.cancelSessionDeliveryCore(ctx, "", "s1", "msg_sent", "dlv_sent")
	if result.Status != http.StatusOK || result.State != "forwarded" {
		t.Fatalf("core result=%+v", result)
	}
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, raw, err := clientConn.Read(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	var envelope model.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Meta["type"] != "delivery_cancel" || envelope.Meta["message_id"] != "msg_sent" || envelope.Meta["delivery_id"] != "dlv_sent" {
		t.Fatalf("cancel envelope=%s", raw)
	}
}
