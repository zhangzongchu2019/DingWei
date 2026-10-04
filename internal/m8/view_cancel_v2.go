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
)

type viewV2CancelRequest struct {
	Session    string `json:"session"`
	PageID     string `json:"page_id"`
	RequestID  string `json:"request_id"`
	MessageID  string `json:"message_id"`
	DeliveryID string `json:"delivery_id"`
}

// HandleInternalViewV2Cancel authenticates a View Service cancellation and
// routes it through the same cancellation core as the legacy view endpoint.
func (h *Hub) HandleInternalViewV2Cancel(w http.ResponseWriter, r *http.Request) {
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
	var req viewV2CancelRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Session != session || strings.TrimSpace(req.PageID) == "" ||
		!viewV2RequestIDPattern.MatchString(req.RequestID) || strings.TrimSpace(req.MessageID) == "" || strings.TrimSpace(req.DeliveryID) == "" {
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
	if !ok {
		h.completeViewV2Request(targetName, req.RequestID, bodyHash, http.StatusNotFound, now)
		http.Error(w, "session unavailable", http.StatusNotFound)
		return
	}
	result := h.cancelSessionDeliveryCore(r.Context(), keyID, internalSession, req.MessageID, req.DeliveryID)
	h.completeViewV2Request(targetName, req.RequestID, bodyHash, result.Status, now)
	writeSessionDeliveryCancelResult(w, req.MessageID, req.DeliveryID, result)
}
