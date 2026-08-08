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

type viewV2InterruptRequest struct {
	Session    string `json:"session"`
	PageID     string `json:"page_id"`
	RequestID  string `json:"request_id"`
	DeliveryID string `json:"delivery_id"`
	TurnID     string `json:"turn_id"`
}

// HandleInternalViewV2Interrupt authenticates a View Service interrupt and
// forwards it to the unique online session client.
func (h *Hub) HandleInternalViewV2Interrupt(w http.ResponseWriter, r *http.Request) {
	session := strings.TrimSpace(r.PathValue("session"))
	if session == "" || strings.Contains(session, "#") {
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
	var req viewV2InterruptRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Session != session || strings.TrimSpace(req.RequestID) == "" ||
		strings.TrimSpace(req.DeliveryID) == "" || strings.TrimSpace(req.TurnID) == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(digest[:])
	if status, duplicate := h.reserveViewV2Request(targetName, req.RequestID, bodyHash, now); duplicate {
		if status == 0 {
			status = http.StatusConflict
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"ok":true,"duplicate":true}`))
		} else {
			_, _ = w.Write([]byte(`{"ok":false,"duplicate":true}`))
		}
		return
	}

	status, err := h.writeInterruptFrame(r.Context(), session, req.DeliveryID, req.TurnID)
	h.completeViewV2Request(targetName, req.RequestID, bodyHash, status, now)
	if status == http.StatusConflict {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":false}`))
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"state":"forwarded"}`))
}
