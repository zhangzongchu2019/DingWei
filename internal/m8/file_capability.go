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
	maxFileUploadBytes         = int64(16 << 20)
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

func (h *Hub) SetFileTicketIssuer(issuer FileTicketIssuer) { h.fileIssuer = issuer }

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
		ref := strings.TrimSpace(metaString(env.Meta, "file_ref"))
		req.FileID = strings.TrimPrefix(ref, "dwfile:")
		req.Method = strings.ToUpper(operation)
		req.ByteRange = strings.TrimSpace(metaString(env.Meta, "range"))
		if !strings.HasPrefix(ref, "dwfile:") || !fileIDPattern.MatchString(req.FileID) || (operation == "head" && req.ByteRange != "") || (operation == "get" && !validFileByteRange(req.ByteRange)) {
			meta["error"] = "invalid file capability request"
			return response
		}
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
