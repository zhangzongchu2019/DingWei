package m8

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type viewV2RetryRequest struct {
	Session    string `json:"session"`
	PageID     string `json:"page_id"`
	RequestID  string `json:"request_id"`
	DeliveryID string `json:"delivery_id"`
}

func r52RetryDeliveryID(original string) string {
	sum := sha256.Sum256([]byte("r52-retry-delivery-v1\x00" + original))
	return deliveryIDPrefix + hex.EncodeToString(sum[:16])
}

func r52RetryEnvelopeID(original string) string {
	sum := sha256.Sum256([]byte("r52-retry-envelope-v1\x00" + original))
	return hex.EncodeToString(sum[:16])
}

func r52RetryEnvelope(d model.SessionDelivery, keyID, sessionName string) (model.Envelope, error) {
	var original model.Envelope
	if err := json.Unmarshal([]byte(d.Envelope), &original); err != nil {
		return model.Envelope{}, err
	}
	original.ID = r52RetryEnvelopeID(d.ID)
	original.To = sessionAddress(sessionName, keyID)
	if original.Meta == nil {
		original.Meta = map[string]any{}
	}
	original.Meta["r52_retry_of"] = d.ID
	// The stored envelope is already wrapped as a Hub delivery. Clear that
	// transport marker so routeToSession creates a fresh durable delivery.
	original.Meta["type"] = ""
	return original, nil
}

// HandleInternalViewV2Retry creates at most one durable replay of the exact
// original envelope. The deterministic delivery key is the durable idempotency
// fence; the helper's admission ledger is never edited.
func (h *Hub) HandleInternalViewV2Retry(w http.ResponseWriter, r *http.Request) {
	session := strings.TrimSpace(r.PathValue("session"))
	if !h.viewSessionNameAllowed(r.Context(), session) {
		http.Error(w, "invalid session", http.StatusBadRequest)
		return
	}
	targetName, target := loadViewRouting().resolve(session, ownerFromSessionName(session))
	if target.Builtin || !target.ViewUnlockV2 || target.VSToHubSecretFile == "" || r.Header.Get("X-DW-VS-Target") != targetName {
		http.Error(w, "capability disabled", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil || len(body) == 0 || len(body) > 1<<20 {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	secret, err := os.ReadFile(target.VSToHubSecretFile)
	if err != nil || len(bytes.TrimSpace(secret)) < 32 {
		http.Error(w, "service auth unavailable", http.StatusServiceUnavailable)
		return
	}
	timestamp, nonce := r.Header.Get("X-DW-Timestamp"), r.Header.Get("X-DW-Nonce")
	unixTime, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || !viewV2NoncePattern.MatchString(nonce) || time.Since(time.Unix(unixTime, 0)).Abs() > time.Minute {
		http.Error(w, "invalid authentication", http.StatusUnauthorized)
		return
	}
	want := signViewServiceRequest(bytes.TrimSpace(secret), vsToHubDirection, r.Method, r.URL.Path, timestamp, nonce, body)
	provided, err := hex.DecodeString(r.Header.Get("X-DW-Signature"))
	wantBytes, _ := hex.DecodeString(want)
	if err != nil || !hmac.Equal(provided, wantBytes) {
		http.Error(w, "invalid authentication", http.StatusUnauthorized)
		return
	}
	now := time.Now()
	if !h.claimViewV2Nonce(targetName, nonce, now) {
		http.Error(w, "replayed request", http.StatusConflict)
		return
	}
	var req viewV2RetryRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Session != session || strings.TrimSpace(req.PageID) == "" ||
		!viewV2RequestIDPattern.MatchString(req.RequestID) || strings.TrimSpace(req.DeliveryID) == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(digest[:])
	if status, duplicate := h.reserveViewV2Request(targetName, req.RequestID, bodyHash, now); duplicate {
		if status == 0 {
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":true,"duplicate":true}`))
		return
	}
	keyID, internalSession, ok := h.viewInternalSession(r.Context(), session)
	if !ok || h.Repo == nil {
		h.completeViewV2Request(targetName, req.RequestID, bodyHash, http.StatusNotFound, now)
		http.Error(w, "session unavailable", http.StatusNotFound)
		return
	}
	h.viewRetryMu.Lock()
	defer h.viewRetryMu.Unlock()
	d, err := h.Repo.GetSessionDelivery(r.Context(), req.DeliveryID)
	if err != nil || d == nil || d.KeyID != keyID || d.SessionName != internalSession ||
		d.State != "admission_unknown" || d.LastError != "model_admission_unknown" || d.AckProtocol != 2 || d.ProcessingMode != "driver" {
		h.completeViewV2Request(targetName, req.RequestID, bodyHash, http.StatusConflict, now)
		http.Error(w, "delivery is not retryable", http.StatusConflict)
		return
	}
	retryID := r52RetryDeliveryID(d.ID)
	if existing, err := h.Repo.GetSessionDelivery(r.Context(), retryID); err == nil && existing != nil {
		h.publishDeliveryUpdateForDelivery(*d, d.State, "helper_queued", "retried", "unconfirmed_retryable")
		h.completeViewV2Request(targetName, req.RequestID, bodyHash, http.StatusOK, now)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"already_retried":true}`))
		return
	}
	original, err := r52RetryEnvelope(*d, keyID, internalSession)
	if err != nil {
		h.completeViewV2Request(targetName, req.RequestID, bodyHash, http.StatusConflict, now)
		http.Error(w, "original envelope unavailable", http.StatusConflict)
		return
	}
	if err := h.routeToSession(r.Context(), keyID, internalSession, original, false); err != nil {
		// A durable row may have won a race/restart despite a lost HTTP response.
		if existing, getErr := h.Repo.GetSessionDelivery(r.Context(), retryID); getErr != nil || existing == nil {
			h.completeViewV2Request(targetName, req.RequestID, bodyHash, http.StatusBadGateway, now)
			http.Error(w, "retry enqueue failed", http.StatusBadGateway)
			return
		}
	}
	h.publishDeliveryUpdateForDelivery(*d, d.State, "helper_queued", "retried", "unconfirmed_retryable")
	h.completeViewV2Request(targetName, req.RequestID, bodyHash, http.StatusOK, now)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
