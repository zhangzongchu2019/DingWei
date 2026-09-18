package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	fileCapabilityRequestType  = "file_capability_request"
	fileCapabilityResponseType = "file_capability_response"
	maxFileUploadBytes         = int64(50 << 20)
)

var fileIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type FileTicketRequest struct {
	Operation   string
	Subject     string
	OwnerKey    string
	SessionName string
	FileID      string
	Method      string
	ByteRange   string
	MaxBytes    int64
	TTL         time.Duration
}

type FileTicket struct {
	Token         string
	ExpiresAt     time.Time
	PublicBaseURL string
}

type FileTicketIssuer interface {
	IssueFileTicket(context.Context, FileTicketRequest) (FileTicket, error)
}

type FileArchiveRequest struct {
	FileID, ActorPrincipal, OwnerKey, Reason string
	Archived                                 bool
}

type FileArchiveClient interface {
	SetFileArchived(context.Context, FileArchiveRequest) error
}

type HTTPFileTicketIssuer struct {
	InternalBaseURL string
	PublicBaseURL   string
	Credential      string
	Client          *http.Client
}

func NewHTTPFileTicketIssuer(internalBaseURL, publicBaseURL, credential string) (*HTTPFileTicketIssuer, error) {
	internalBaseURL = strings.TrimRight(strings.TrimSpace(internalBaseURL), "/")
	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	credential = strings.TrimSpace(credential)
	for _, raw := range []string{internalBaseURL, publicBaseURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid file-service URL")
		}
	}
	if credential == "" {
		return nil, errors.New("file-service issuer credential is required")
	}
	return &HTTPFileTicketIssuer{
		InternalBaseURL: internalBaseURL,
		PublicBaseURL:   publicBaseURL,
		Credential:      credential,
		Client:          &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (i *HTTPFileTicketIssuer) IssueFileTicket(ctx context.Context, req FileTicketRequest) (FileTicket, error) {
	if i == nil || i.Client == nil {
		return FileTicket{}, errors.New("file-service issuer unavailable")
	}
	path := ""
	body := map[string]any{"ttl_seconds": int64(req.TTL / time.Second)}
	switch req.Operation {
	case "upload":
		path = "/internal/files/issue/upload"
		body["owner_key"], body["session_name"], body["max_bytes"] = req.OwnerKey, req.SessionName, req.MaxBytes
	case "head", "get":
		path = "/internal/files/issue/retrieval"
		body["subject"], body["file_id"], body["method"], body["range"] = req.Subject, req.FileID, req.Method, req.ByteRange
	default:
		return FileTicket{}, errors.New("unsupported file ticket operation")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return FileTicket{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, i.InternalBaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return FileTicket{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+i.Credential)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := i.Client.Do(httpReq)
	if err != nil {
		return FileTicket{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return FileTicket{}, fmt.Errorf("file-service issuer status %d", resp.StatusCode)
	}
	var decoded struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 16<<10))
	if err := decoder.Decode(&decoded); err != nil || decoded.Token == "" || !decoded.ExpiresAt.After(time.Now()) {
		return FileTicket{}, errors.New("invalid file-service issuer response")
	}
	return FileTicket{Token: decoded.Token, ExpiresAt: decoded.ExpiresAt, PublicBaseURL: i.PublicBaseURL}, nil
}

func (i *HTTPFileTicketIssuer) SetFileArchived(ctx context.Context, req FileArchiveRequest) error {
	if i == nil || i.Client == nil || !fileIDPattern.MatchString(req.FileID) || strings.TrimSpace(req.ActorPrincipal) == "" || strings.TrimSpace(req.OwnerKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return errors.New("invalid file archive request")
	}
	payload, err := json.Marshal(map[string]any{"file_id": req.FileID, "actor_principal": req.ActorPrincipal, "owner_key": req.OwnerKey, "reason": req.Reason, "archived": req.Archived})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, i.InternalBaseURL+"/internal/files/archive", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+i.Credential)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := i.Client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("file archive status %d", resp.StatusCode)
	}
	return nil
}

func (h *Hub) SetFileTicketIssuer(issuer FileTicketIssuer)   { h.fileIssuer = issuer }
func (h *Hub) SetFileArchiveClient(client FileArchiveClient) { h.fileArchive = client }

type consoleOwnerScopeRepository interface {
	ResolveConsoleOwnerScope(context.Context, string) (string, error)
}

func (h *Hub) HandleConsoleFileArchive(w http.ResponseWriter, r *http.Request) {
	session, sessionOK := r.Context().Value(consoleSessionContextKey{}).(model.ConsoleSession)
	repo, repoOK := h.Repo.(consoleOwnerScopeRepository)
	fileID := strings.TrimSpace(r.PathValue("fileID"))
	if !h.consoleAuthEnabled || !sessionOK || !repoOK || h.fileArchive == nil || !fileIDPattern.MatchString(fileID) {
		http.NotFound(w, r)
		return
	}
	var input struct {
		Archived *bool  `json:"archived"`
		Reason   string `json:"reason"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || input.Archived == nil || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 {
		http.Error(w, "invalid archive request", http.StatusBadRequest)
		return
	}
	ownerKey, err := repo.ResolveConsoleOwnerScope(r.Context(), session.PrincipalID)
	if err != nil {
		http.Error(w, "archive unavailable", http.StatusServiceUnavailable)
		return
	}
	if ownerKey == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := h.fileArchive.SetFileArchived(r.Context(), FileArchiveRequest{FileID: fileID, ActorPrincipal: session.PrincipalID, OwnerKey: ownerKey, Reason: strings.TrimSpace(input.Reason), Archived: *input.Archived}); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleFileUploadTicket(w http.ResponseWriter, r *http.Request) {
	agent, ok := r.Context().Value(consoleAgentContextKey{}).(model.ConsoleAgent)
	if !ok || h.fileIssuer == nil {
		http.Error(w, "file upload unavailable", http.StatusServiceUnavailable)
		return
	}
	var input struct {
		MaxBytes int64 `json:"max_bytes"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.MaxBytes <= 0 || input.MaxBytes > maxFileUploadBytes {
		http.Error(w, "invalid upload request", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	c := h.sessionClients[agent.KeyID][agent.SessionName]
	ownerKey := ""
	if c != nil {
		ownerKey = c.ownerKey
	}
	h.mu.Unlock()
	if ownerKey == "" {
		http.Error(w, "agent offline", http.StatusConflict)
		return
	}
	ticket, err := h.fileIssuer.IssueFileTicket(r.Context(), FileTicketRequest{
		Operation: "upload", OwnerKey: ownerKey, SessionName: agent.SessionName,
		MaxBytes: input.MaxBytes, TTL: time.Minute,
	})
	if err != nil {
		http.Error(w, "file upload unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"upload_url": ticket.PublicBaseURL + "/files/upload", "ticket": ticket.Token,
		"expires_at": ticket.ExpiresAt.UTC().Format(time.RFC3339Nano), "session": agent.SessionName,
	})
}

func (h *Hub) handleFileCapabilityRequest(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != fileCapabilityRequestType {
		return false
	}
	response := h.fileCapabilityResponse(ctx, c, env)
	if c != nil && c.conn != nil {
		_ = c.write(ctx, response)
	}
	return true
}

func (h *Hub) fileCapabilityResponse(ctx context.Context, c *sessionClient, env model.Envelope) model.Envelope {
	requestID := metaString(env.Meta, "request_id")
	meta := map[string]any{
		"type":       fileCapabilityResponseType,
		"request_id": requestID,
		"ok":         false,
		"system":     true,
		"no_mirror":  true,
	}
	response := model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, firstNonEmpty(cKeyID(c), "unknown")), To: env.From, TS: time.Now().Unix(), Meta: meta}
	if c == nil || h.fileIssuer == nil || requestID == "" || len(requestID) > 128 || c.ownerKey == "" || c.sessionName == "" {
		meta["error"] = "file capability unavailable"
		return response
	}
	operation := strings.ToLower(strings.TrimSpace(metaString(env.Meta, "operation")))
	req := FileTicketRequest{Operation: operation, OwnerKey: c.ownerKey, SessionName: c.sessionName, Subject: c.ownerKey + "/" + c.sessionName, TTL: time.Minute}
	switch operation {
	case "upload":
		req.MaxBytes = metaInt64(env.Meta, "max_bytes")
		if req.MaxBytes <= 0 || req.MaxBytes > maxFileUploadBytes {
			meta["error"] = "invalid file capability request"
			return response
		}
	case "head", "get":
		// Frozen by owner decision 2026-09-18. Restore only together with the
		// file-service retrieval issuer, GrantStore verification, /files/ref/
		// HTTP route and nginx proxy.
		meta["error"] = "file retrieval capability frozen"
		return response
	default:
		meta["error"] = "invalid file capability request"
		return response
	}
	ticket, err := h.fileIssuer.IssueFileTicket(ctx, req)
	if err != nil {
		meta["error"] = "file capability unavailable"
		return response
	}
	meta["ok"], meta["operation"], meta["token"] = true, operation, ticket.Token
	meta["expires_at"], meta["public_base_url"] = ticket.ExpiresAt.UTC().Format(time.RFC3339Nano), ticket.PublicBaseURL
	return response
}

func validFileByteRange(raw string) bool {
	if !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(raw, "bytes="), "-")
	if len(parts) != 2 || (parts[0] == "" && parts[1] == "") {
		return false
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func metaInt64(meta map[string]any, key string) int64 {
	switch v := meta[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func cKeyID(c *sessionClient) string {
	if c == nil {
		return ""
	}
	return c.keyID
}
