package m8

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestViewV2InputAuthenticatesReplaysAndIdempotency(t *testing.T) {
	hub, _, _ := newTestHub(t)
	secret := []byte("abcdef0123456789abcdef0123456789")
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "vs-to-hub.key")
	if err := os.WriteFile(secretFile, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "routes.json")
	config := map[string]any{
		"sessions": map[string]string{"alice-developer-0000": "canary"},
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
	mux.HandleFunc("POST /internal/view-v2/{session}/input", hub.HandleInternalViewV2Input)
	body, _ := json.Marshal(viewV2InputRequest{
		Session: "alice-developer-0000", PageID: "page-1", RequestID: "0123456789abcdef0123456789abcdef", Text: "line1\nline2\r",
	})

	do := func(nonce, direction string) *httptest.ResponseRecorder {
		path := "/internal/view-v2/alice-developer-0000/input"
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("X-DW-VS-Target", "canary")
		req.Header.Set("X-DW-Timestamp", ts)
		req.Header.Set("X-DW-Nonce", nonce)
		req.Header.Set("X-DW-Signature", signViewServiceRequest(secret, direction, http.MethodPost, path, ts, nonce, body))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}

	first := do("00112233445566778899aabbccddeeff", vsToHubDirection)
	if first.Code != http.StatusNotFound { // auth passed; target session is intentionally offline
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	replay := do("00112233445566778899aabbccddeeff", vsToHubDirection)
	if replay.Code != http.StatusConflict {
		t.Fatalf("nonce replay status=%d", replay.Code)
	}
	idempotent := do("ffeeddccbbaa99887766554433221100", vsToHubDirection)
	if idempotent.Code != http.StatusNotFound || !bytes.Contains(idempotent.Body.Bytes(), []byte("duplicate")) {
		t.Fatalf("idempotent status=%d body=%s", idempotent.Code, idempotent.Body.String())
	}
	wrongDirection := do("1234567890abcdef1234567890abcdef", hubToVSDirection)
	if wrongDirection.Code != http.StatusUnauthorized {
		t.Fatalf("wrong direction status=%d", wrongDirection.Code)
	}
}

func TestViewV2RequestIDValidation(t *testing.T) {
	for _, valid := range []string{
		"0123456789abcdef0123456789abcdef",
		"01234567-89ab-cdef-0123-456789abcdef",
	} {
		if !viewV2RequestIDPattern.MatchString(valid) {
			t.Fatalf("rejected valid request id %q", valid)
		}
	}
	for _, invalid := range []string{"", "request-1", strings.Repeat("a", 129), "../../same", "ABCDEF0123456789ABCDEF0123456789"} {
		if viewV2RequestIDPattern.MatchString(invalid) {
			t.Fatalf("accepted invalid request id %q", invalid)
		}
	}
}

func TestViewV2TerminalSubmissionPreservesMultilineAndSubmitsOnce(t *testing.T) {
	if got, want := terminalSubmission("line1\r\nline2\rline3"), "line1\nline2\nline3\r"; got != want {
		t.Fatalf("terminal submission=%q want=%q", got, want)
	}
}

func TestViewV2ReplayCachesAreBounded(t *testing.T) {
	hub, _, _ := newTestHub(t)
	now := time.Now()
	for i := 0; i < viewV2ReplayMax+5; i++ {
		hub.claimViewV2Nonce("canary", strconv.Itoa(i), now)
		hub.reserveViewV2Request("canary", strconv.Itoa(i), strconv.Itoa(i), now)
	}
	if len(hub.viewV2Nonces) > viewV2ReplayMax || len(hub.viewV2Requests) > viewV2ReplayMax {
		t.Fatalf("unbounded caches nonces=%d requests=%d", len(hub.viewV2Nonces), len(hub.viewV2Requests))
	}
}
