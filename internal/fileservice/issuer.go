package fileservice

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type IssuerHandler struct {
	Capabilities *GrantStore
	Uploads      *UploadTicketStore
	Token        string
}

type issueRequest struct {
	Subject     string `json:"subject"`
	OwnerKey    string `json:"owner_key"`
	SessionName string `json:"session_name"`
	FileID      string `json:"file_id"`
	Method      string `json:"method"`
	Range       string `json:"range"`
	TTLSeconds  int64  `json:"ttl_seconds"`
	MaxBytes    int64  `json:"max_bytes"`
}

func (h IssuerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || h.Token == "" || !sameSecret(bearer(r), h.Token) {
		hide(w)
		return
	}
	var req issueRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		hide(w)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		hide(w)
		return
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	var raw string
	var expiresAt time.Time
	var err error
	switch r.URL.Path {
	case "/internal/files/issue/retrieval":
		if h.Capabilities == nil || req.Subject == "" || req.FileID == "" || (strings.ToUpper(req.Method) != http.MethodHead && strings.ToUpper(req.Method) != http.MethodGet) {
			hide(w)
			return
		}
		var capability Capability
		raw, capability, err = h.Capabilities.Issue(req.Subject, req.FileID, req.Method, req.Range, ttl)
		expiresAt = capability.ExpiresAt
	case "/internal/files/issue/upload":
		if h.Uploads == nil || req.OwnerKey == "" || req.SessionName == "" || req.MaxBytes <= 0 {
			hide(w)
			return
		}
		var grant UploadGrant
		raw, grant, err = h.Uploads.Issue(req.OwnerKey, req.SessionName, req.MaxBytes, ttl)
		expiresAt = grant.ExpiresAt
	default:
		hide(w)
		return
	}
	if err != nil {
		hide(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"token": raw, "expires_at": expiresAt})
}
