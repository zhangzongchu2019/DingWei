package m8

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestR52RetryEnvelopePreservesBodyAndAttachments(t *testing.T) {
	attachments := []any{map[string]any{"file_id": "file-1", "name": "note.txt"}}
	stored, _ := json.Marshal(model.Envelope{ID: "source", From: "workpulse#owner", To: "agent#owner", Body: "original body", Meta: map[string]any{"type": "delivery", "delivery_id": "old", "attachments": attachments}})
	retry, err := r52RetryEnvelope(model.SessionDelivery{ID: "old", Envelope: string(stored)}, "owner", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if retry.Body != "original body" || retry.ID == "source" || retry.To != "agent#owner" || retry.Meta["type"] != "" || !reflect.DeepEqual(retry.Meta["attachments"], attachments) {
		t.Fatalf("retry envelope did not preserve original content and attachments: %+v", retry)
	}
}

func configureViewRetry(t *testing.T, session string) []byte {
	t.Helper()
	secret := []byte("abcdef0123456789abcdef0123456789")
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "vs-to-hub.key")
	if err := os.WriteFile(secretFile, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "routes.json")
	config, _ := json.Marshal(map[string]any{"sessions": map[string]string{session: "canary"}, "targets": map[string]any{"canary": map[string]any{
		"url": "http://127.0.0.1:19321", "view_unlock_v2": true, "vs_to_hub_secret_file": secretFile,
	}}})
	if err := os.WriteFile(configFile, config, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", configFile)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)
	return secret
}

func signedRetryRequest(t *testing.T, h *Hub, secret []byte, requestID, deliveryID string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(viewV2RetryRequest{Session: "Opus50TS.zzc", PageID: "page-r", RequestID: requestID, DeliveryID: deliveryID})
	path := "/internal/view-v2/Opus50TS.zzc/retry"
	ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), randomHex(16)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.SetPathValue("session", "Opus50TS.zzc")
	req.Header.Set("X-DW-VS-Target", "canary")
	req.Header.Set("X-DW-Timestamp", ts)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(secret, vsToHubDirection, http.MethodPost, path, ts, nonce, body))
	rr := httptest.NewRecorder()
	h.HandleInternalViewV2Retry(rr, req)
	return rr
}

func TestViewRetryV2ReplaysExactEnvelopeOnce(t *testing.T) {
	f := newACKV2Fixture(t, true)
	f.sendAndConfirm("queued", "", "", f.stamp)
	f.sendAndConfirm("admission_unknown", "", "", f.stamp)
	secret := configureViewRetry(t, "Opus50TS.zzc")
	first := signedRetryRequest(t, f.hub, secret, "1123456789abcdef0123456789abcdef", "ack-wire")
	if first.Code != http.StatusOK || !bytes.Contains(first.Body.Bytes(), []byte(`"ok":true`)) {
		t.Fatalf("retry response=%d %s", first.Code, first.Body.String())
	}
	retryMessageID := r52RetryEnvelopeID("ack-wire")
	retryID := ""
	var replayObservations int
	f.hub.mu.Lock()
	ring := append([]string(nil), f.hub.terminals[terminalKey(f.client.keyID, f.client.sessionName)].eventRing...)
	f.hub.mu.Unlock()
	for _, raw := range ring {
		var event map[string]any
		if json.Unmarshal([]byte(raw), &event) == nil && event["kind"] == "input_posted" && event["message_id"] == retryMessageID {
			replayObservations++
			retryID, _ = event["delivery_id"].(string)
		}
	}
	if replayObservations != 1 || retryID == "" {
		t.Fatalf("first retry should project exactly one delivery, got count=%d id=%q", replayObservations, retryID)
	}
	row, err := f.hub.Repo.GetSessionDelivery(f.ctx, retryID)
	if err != nil || row == nil {
		t.Fatalf("retry row=%+v err=%v response=%s updates=%+v", row, err, first.Body.String(), f.updates())
	}
	var original, replay model.Envelope
	if err := json.Unmarshal([]byte(f.record().Envelope), &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(row.Envelope), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Body != original.Body || replay.Meta["r52_retry_of"] != "ack-wire" || replay.ID == original.ID {
		t.Fatalf("retry did not preserve payload with a new identity: original=%+v replay=%+v", original, replay)
	}
	second := signedRetryRequest(t, f.hub, secret, "2123456789abcdef0123456789abcdef", "ack-wire")
	if second.Code != http.StatusOK || !bytes.Contains(second.Body.Bytes(), []byte(`"already_retried":true`)) {
		t.Fatalf("second retry=%d %s", second.Code, second.Body.String())
	}
	if _, err := f.hub.Repo.GetSessionDelivery(f.ctx, retryID); err != nil {
		t.Fatal(err)
	}
	f.hub.mu.Lock()
	ring = append([]string(nil), f.hub.terminals[terminalKey(f.client.keyID, f.client.sessionName)].eventRing...)
	f.hub.mu.Unlock()
	replayObservations = 0
	for _, raw := range ring {
		var event map[string]any
		if json.Unmarshal([]byte(raw), &event) == nil && event["kind"] == "input_posted" && event["message_id"] == retryMessageID {
			replayObservations++
		}
	}
	if replayObservations != 1 {
		t.Fatalf("original should project exactly one retry, got %d observations", replayObservations)
	}
}

func TestConvergeAdmissionUnknownOnNextSessionTurnCompletion(t *testing.T) {
	f := newACKV2Fixture(t, true)
	f.sendAndConfirm("queued", "", "", f.stamp)
	f.sendAndConfirm("admission_unknown", "", "", f.stamp)
	_, events, unsub := f.hub.subscribeSessionEvents(f.client.keyID, f.client.sessionName, "converge-test")
	defer unsub()
	unrelated, _ := json.Marshal(amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed"}})
	f.hub.convergeAdmissionUnknownAfterTurn("other-key", f.client.sessionName, string(unrelated))
	select {
	case got := <-events:
		t.Fatalf("different session owner converged delivery: %s", got)
	default:
	}
	f.hub.convergeAdmissionUnknownAfterTurn(f.client.keyID, f.client.sessionName, string(unrelated))
	select {
	case got := <-events:
		var event map[string]any
		if err := json.Unmarshal([]byte(got), &event); err != nil {
			t.Fatal(err)
		}
		if event["delivery_status"] != "unconfirmed_retryable" || event["reason"] != "unconfirmed_retryable" || event["cancellable"] != false {
			t.Fatalf("bad terminal projection: %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("matching terminal event did not converge unknown delivery")
	}
	if row := f.record(); row.State != "admission_unknown" {
		t.Fatalf("helper ledger projection mutated Hub row: %+v", row)
	}
}
