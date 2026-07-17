package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHMACViewPageUnlockerSignsDirectionBoundRequest(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	var captured struct {
		body   []byte
		header http.Header
		path   string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.header = r.Header.Clone()
		captured.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "hub-to-vs.key")
	if err := os.WriteFile(secretFile, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "routes.json")
	config := map[string]any{
		"sessions": map[string]string{"alice-developer-0000": "canary"},
		"default":  "legacy-xterm",
		"targets": map[string]any{
			"legacy-xterm": map[string]any{"builtin": true},
			"canary":       map[string]any{"url": server.URL, "view_unlock_v2": true, "hub_to_vs_secret_file": secretFile},
		},
	}
	raw, _ := json.Marshal(config)
	if err := os.WriteFile(configFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", configFile)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)

	fixed := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	client := newHMACViewPageUnlocker()
	client.now = func() time.Time { return fixed }
	client.nonce = func() (string, error) { return "00112233445566778899aabbccddeeff", nil }
	err := client.UnlockViewPage(context.Background(), ViewPageUnlockRequest{
		Code: "alice-developer-0000-a3Kf9xQ2", FullSessionName: "alice-developer-0000",
		OwnerKey: "alice", SenderOpenID: "ou_alice", MessageID: "msg-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.path != viewUnlockControlPath || captured.header.Get("X-DW-VS-Target") != "canary" || captured.header.Get("X-DW-Request-ID") != "msg-1" {
		t.Fatalf("request path=%q headers=%v", captured.path, captured.header)
	}
	want := signViewServiceRequest(secret, hubToVSDirection, http.MethodPost, captured.path,
		captured.header.Get("X-DW-Timestamp"), captured.header.Get("X-DW-Nonce"), captured.body)
	if captured.header.Get("X-DW-Signature") != want {
		t.Fatalf("signature mismatch")
	}
	reverse := signViewServiceRequest(secret, "view-service-to-hub/v1", http.MethodPost, captured.path,
		captured.header.Get("X-DW-Timestamp"), captured.header.Get("X-DW-Nonce"), captured.body)
	if reverse == want {
		t.Fatal("direction keys are not separated")
	}
	var body viewUnlockControlBody
	if err := json.Unmarshal(captured.body, &body); err != nil {
		t.Fatal(err)
	}
	if body.CommandID != "msg-1" || body.RequestID != "msg-1" || body.Code != "alice-developer-0000-a3Kf9xQ2" {
		t.Fatalf("body=%+v", body)
	}
}

func TestHMACViewPageUnlockerCapabilityFailsClosed(t *testing.T) {
	tmp := t.TempDir()
	configFile := filepath.Join(tmp, "routes.json")
	raw := []byte(`{"sessions":{"alice-developer-0000":"canary"},"targets":{"canary":{"url":"http://127.0.0.1:1"}}}`)
	if err := os.WriteFile(configFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", configFile)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)
	err := newHMACViewPageUnlocker().UnlockViewPage(context.Background(), ViewPageUnlockRequest{FullSessionName: "alice-developer-0000"})
	if err == nil {
		t.Fatal("disabled capability accepted unlock")
	}
}

func resetViewRoutingCacheForTest() {
	viewRoutingMu.Lock()
	viewRoutingCache = nil
	viewRoutingMTime = time.Time{}
	viewRoutingMu.Unlock()
}

func TestSignViewServiceRequestBindsBody(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	a := signViewServiceRequest(secret, hubToVSDirection, "POST", "/x", "1", "n", []byte("a"))
	b := signViewServiceRequest(secret, hubToVSDirection, "POST", "/x", "1", "n", []byte("b"))
	if a == b {
		t.Fatal("signature does not bind body")
	}
	decoded, err := hex.DecodeString(a)
	if err != nil || len(decoded) != sha256.Size {
		t.Fatalf("signature encoding invalid: len=%d err=%v", len(decoded), err)
	}
}
