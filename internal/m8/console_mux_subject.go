package m8

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (h *Hub) muxRouteOwner(r *http.Request, name string) string {
	if h.Identity != nil {
		if _, a, ok := h.newPlatformCanonicalName(r.Context(), name); ok {
			return a.OwnerLC
		}
	}
	return ownerFromSessionName(name)
}

var errMuxSubjectUnavailable = errors.New("projection_subject_unavailable")

func (h *Hub) muxProjectionSubject(req *http.Request, session string) error {
	req.Header.Del("X-DW-Mux-Subject")
	// All service authentication fields are router-owned; incoming browser values never survive.
	for _, name := range []string{"X-DW-VS-Target", "X-DW-Timestamp", "X-DW-Nonce", "X-DW-Signature"} {
		req.Header.Del(name)
	}
	if h.Identity == nil || req.Method != "GET" {
		return nil
	}
	canon, a, ok := h.newPlatformCanonicalName(req.Context(), session)
	if !ok {
		return errMuxSubjectUnavailable
	}
	targetName, target := loadViewRouting().resolve(canon, a.OwnerLC)
	if !target.ViewUnlockV2 {
		return nil
	}
	key, err := os.ReadFile(target.HubToVSSecretFile)
	if err != nil || len(bytes.TrimSpace(key)) < 32 {
		return errMuxSubjectUnavailable
	}
	body, _ := json.Marshal(map[string]string{"agent_id": a.ID, "full_name": canon})
	nonce, err := muxID()
	if err != nil {
		return errMuxSubjectUnavailable
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-DW-Mux-Subject", base64.RawURLEncoding.EncodeToString(body))
	req.Header.Set("X-DW-VS-Target", targetName)
	req.Header.Set("X-DW-Timestamp", timestamp)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(bytes.TrimSpace(key), hubToVSDirection, req.Method, req.URL.Path, timestamp, nonce, body))
	return nil
}
