package m8

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	vsToHubDirection = "view-service-to-hub/v1"
	viewV2ReplayTTL  = 5 * time.Minute
	viewV2ReplayMax  = 10000
)

var viewV2NoncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var viewV2RequestIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

type viewV2InputRequest struct {
	Session   string `json:"session"`
	PageID    string `json:"page_id"`
	RequestID string `json:"request_id"`
	Text      string `json:"text"`
}

type viewV2RequestResult struct {
	BodyHash string
	Status   int
	Expires  time.Time
}

func (h *Hub) HandleInternalViewV2Input(w http.ResponseWriter, r *http.Request) {
	session := strings.TrimSpace(r.PathValue("session"))
	if !sessionNamePattern.MatchString(session) {
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
	var req viewV2InputRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Session != session || strings.TrimSpace(req.PageID) == "" || !viewV2RequestIDPattern.MatchString(req.RequestID) || req.Text == "" {
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
	status := http.StatusOK
	if err := h.routeViewServiceInput(r.Context(), session, req.PageID, req.RequestID, req.Text); err != nil {
		status = http.StatusNotFound
	}
	h.completeViewV2Request(targetName, req.RequestID, bodyHash, status, now)
	if status != http.StatusOK {
		http.Error(w, "session unavailable", status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (h *Hub) routeViewServiceInput(ctx context.Context, session, pageID, requestID, text string) error {
	h.mu.Lock()
	st, found := h.terminalBySessionNameLocked(session)
	var c *sessionClient
	var keyID string
	if found {
		keyID = st.keyID
		c = h.sessionClients[keyID][session]
	}
	h.mu.Unlock()
	if c == nil {
		return fmt.Errorf("session offline or ambiguous")
	}
	env := model.Envelope{
		// Stable request ID reaches the helper queue as the delivery identity, so a
		// Hub restart between response loss and retry cannot create a second turn.
		ID: requestID, To: sessionAddress(session, keyID), From: sessionAddress("workpulse", keyID),
		Body: terminalSubmission(text), TS: time.Now().Unix(),
		Meta: map[string]any{"type": terminalInputType, "system": true, "no_mirror": true,
			"auth_mode": "view_page_v2", "page_id": pageID, "request_id": requestID},
	}
	return c.write(ctx, env)
}

func terminalSubmission(text string) string {
	// The helper writes terminal_input bytes directly to the PTY. Preserve the
	// user's internal newlines, normalize CR variants, and submit exactly once.
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n") + "\r"
}

func (h *Hub) claimViewV2Nonce(target, nonce string, now time.Time) bool {
	h.viewV2Mu.Lock()
	defer h.viewV2Mu.Unlock()
	h.sweepViewV2ReplayLocked(now)
	key := target + "\x00" + nonce
	if _, exists := h.viewV2Nonces[key]; exists {
		return false
	}
	h.viewV2Nonces[key] = now.Add(viewV2ReplayTTL)
	h.trimViewV2ReplayLocked()
	return true
}

func (h *Hub) reserveViewV2Request(target, requestID, bodyHash string, now time.Time) (int, bool) {
	h.viewV2Mu.Lock()
	defer h.viewV2Mu.Unlock()
	h.sweepViewV2ReplayLocked(now)
	key := target + "\x00" + requestID
	if previous, exists := h.viewV2Requests[key]; exists {
		if !hmac.Equal([]byte(previous.BodyHash), []byte(bodyHash)) {
			return http.StatusConflict, true
		}
		return previous.Status, true
	}
	h.viewV2Requests[key] = viewV2RequestResult{BodyHash: bodyHash, Expires: now.Add(viewV2ReplayTTL)}
	h.trimViewV2ReplayLocked()
	return 0, false
}

func (h *Hub) completeViewV2Request(target, requestID, bodyHash string, status int, now time.Time) {
	h.viewV2Mu.Lock()
	h.viewV2Requests[target+"\x00"+requestID] = viewV2RequestResult{BodyHash: bodyHash, Status: status, Expires: now.Add(viewV2ReplayTTL)}
	h.viewV2Mu.Unlock()
}

func (h *Hub) sweepViewV2ReplayLocked(now time.Time) {
	for key, expires := range h.viewV2Nonces {
		if !expires.After(now) {
			delete(h.viewV2Nonces, key)
		}
	}
	for key, result := range h.viewV2Requests {
		if !result.Expires.After(now) {
			delete(h.viewV2Requests, key)
		}
	}
}

func (h *Hub) trimViewV2ReplayLocked() {
	for len(h.viewV2Nonces) > viewV2ReplayMax {
		for key := range h.viewV2Nonces {
			delete(h.viewV2Nonces, key)
			break
		}
	}
	for len(h.viewV2Requests) > viewV2ReplayMax {
		for key := range h.viewV2Requests {
			delete(h.viewV2Requests, key)
			break
		}
	}
}
