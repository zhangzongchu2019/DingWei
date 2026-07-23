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

func TestViewV2InterruptAuthenticatesForwardsAndDeduplicates(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	secret := []byte("abcdef0123456789abcdef0123456789")
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "vs-to-hub.key")
	if err := os.WriteFile(secretFile, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "routes.json")
	config := map[string]any{
		"sessions": map[string]string{"v2interrupt-online": "canary", "v2interrupt-offline": "canary"},
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
	hub.sessionClients["key"] = map[string]*sessionClient{"v2interrupt-online": {
		conn: serverConn, keyID: "key", sessionName: "v2interrupt-online",
	}}
	hub.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/view-v2/{session}/interrupt", hub.HandleInternalViewV2Interrupt)
	do := func(session, nonce string, body []byte, signingSecret []byte) *httptest.ResponseRecorder {
		path := "/internal/view-v2/" + session + "/interrupt"
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("X-DW-VS-Target", "canary")
		// Observability-only header: the Hub intentionally keys idempotency from
		// the signed body request_id, matching HandleInternalViewV2Input.
		req.Header.Set("X-DW-Request-ID", "different-observation-id")
		req.Header.Set("X-DW-Timestamp", ts)
		req.Header.Set("X-DW-Nonce", nonce)
		req.Header.Set("X-DW-Signature", signViewServiceRequest(signingSecret, vsToHubDirection, http.MethodPost, path, ts, nonce, body))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}
	body, _ := json.Marshal(viewV2InterruptRequest{
		Session: "v2interrupt-online", PageID: "page-1", RequestID: "rid-x",
		DeliveryID: "delivery-1", TurnID: "turn-1",
	})
	first := do("v2interrupt-online", "00112233445566778899aabbccddeeff", body, secret)
	if first.Code != http.StatusOK || !bytes.Contains(first.Body.Bytes(), []byte(`"state":"forwarded"`)) {
		t.Fatalf("first interrupt status=%d body=%s", first.Code, first.Body.String())
	}
	readCtx, cancelRead := context.WithTimeout(ctx, time.Second)
	defer cancelRead()
	_, raw, err := clientConn.Read(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	var envelope model.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Meta) != 5 || envelope.Meta["type"] != terminalInterruptType || envelope.Meta["delivery_id"] != "delivery-1" || envelope.Meta["turn_id"] != "turn-1" || envelope.Meta["system"] != true || envelope.Meta["no_mirror"] != true {
		t.Fatalf("interrupt envelope=%s", raw)
	}

	duplicate := do("v2interrupt-online", "11223344556677889900aabbccddeeff", body, secret)
	if duplicate.Code != http.StatusOK || !bytes.Contains(duplicate.Body.Bytes(), []byte(`"duplicate":true`)) {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	noFrameCtx, cancelNoFrame := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelNoFrame()
	if _, extra, err := clientConn.Read(noFrameCtx); err == nil {
		t.Fatalf("duplicate wrote extra frame=%s", extra)
	}

	offlineBody, _ := json.Marshal(viewV2InterruptRequest{
		Session: "v2interrupt-offline", PageID: "page-1", RequestID: "2123456789abcdef0123456789abcdef",
		DeliveryID: "delivery-2", TurnID: "turn-2",
	})
	offline := do("v2interrupt-offline", "22334455667788990011aabbccddeeff", offlineBody, secret)
	if offline.Code != http.StatusConflict || !bytes.Contains(offline.Body.Bytes(), []byte(`"ok":false`)) {
		t.Fatalf("offline status=%d body=%s", offline.Code, offline.Body.String())
	}
	badSignature := do("v2interrupt-online", "33445566778899001122aabbccddeeff", body, []byte("0123456789abcdef0123456789abcdef"))
	if badSignature.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature status=%d body=%s", badSignature.Code, badSignature.Body.String())
	}
	capabilityPath := "/internal/view-v2/v2interrupt-online/interrupt"
	capabilityReq := httptest.NewRequest(http.MethodPost, capabilityPath, bytes.NewReader(body))
	capabilityReq.Header.Set("X-DW-VS-Target", "wrong-target")
	capabilityRecorder := httptest.NewRecorder()
	mux.ServeHTTP(capabilityRecorder, capabilityReq)
	if capabilityRecorder.Code != http.StatusNotFound {
		t.Fatalf("capability gate status=%d body=%s", capabilityRecorder.Code, capabilityRecorder.Body.String())
	}
	missingTurn, _ := json.Marshal(viewV2InterruptRequest{
		Session: "v2interrupt-online", PageID: "page-1", RequestID: "3123456789abcdef0123456789abcdef",
		DeliveryID: "delivery-3",
	})
	invalid := do("v2interrupt-online", "44556677889900112233aabbccddeeff", missingTurn, secret)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("missing turn status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	invalidSessionReq := httptest.NewRequest(http.MethodPost, "/internal/view-v2/bad%23session/interrupt", bytes.NewReader(body))
	invalidSessionRecorder := httptest.NewRecorder()
	mux.ServeHTTP(invalidSessionRecorder, invalidSessionReq)
	if invalidSessionRecorder.Code != http.StatusBadRequest {
		t.Fatalf("hash session status=%d body=%s", invalidSessionRecorder.Code, invalidSessionRecorder.Body.String())
	}
}
