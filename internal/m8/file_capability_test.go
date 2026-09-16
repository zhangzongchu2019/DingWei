package m8

import (
	"context"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type recordingFileIssuer struct {
	req   FileTicketRequest
	calls int
	err   error
}

func (f *recordingFileIssuer) IssueFileTicket(_ context.Context, req FileTicketRequest) (FileTicket, error) {
	f.req, f.calls = req, f.calls+1
	return FileTicket{Token: "opaque-ticket", ExpiresAt: time.Now().Add(time.Minute), PublicBaseURL: "https://files.example"}, f.err
}

func TestFileCapabilityUsesAuthenticatedSessionIdentity(t *testing.T) {
	issuer := &recordingFileIssuer{}
	h := &Hub{fileIssuer: issuer}
	c := &sessionClient{keyID: "key-a", ownerKey: "owner-a", sessionName: "session-a"}
	env := model.Envelope{From: "session-a#key-a", Meta: map[string]any{
		"type": fileCapabilityRequestType, "request_id": "request-1", "operation": "get",
		"file_ref": "dwfile:0123456789abcdef0123456789abcdef", "range": "bytes=0-15",
		"owner_key": "attacker", "session_name": "other",
	}}
	got := h.fileCapabilityResponse(context.Background(), c, env)
	if issuer.calls != 1 || issuer.req.OwnerKey != "owner-a" || issuer.req.SessionName != "session-a" || issuer.req.Subject != "owner-a/session-a" {
		t.Fatalf("issuer calls=%d request=%+v", issuer.calls, issuer.req)
	}
	if !metaBool(got.Meta, "ok") || metaString(got.Meta, "token") != "opaque-ticket" || got.Body != "" {
		t.Fatalf("response=%+v", got)
	}
	if got.Meta["owner_key"] != nil || got.Meta["session_name"] != nil {
		t.Fatalf("response reflected identity claims: %+v", got.Meta)
	}
}

func TestFileCapabilityRejectsInvalidRequestBeforeIssuer(t *testing.T) {
	for _, meta := range []map[string]any{
		{"type": fileCapabilityRequestType, "request_id": "r", "operation": "get", "file_ref": "dwfile:../../secret", "range": "bytes=0-1"},
		{"type": fileCapabilityRequestType, "request_id": "r", "operation": "get", "file_ref": "dwfile:0123456789abcdef0123456789abcdef", "range": "bytes=0-1,3-4"},
		{"type": fileCapabilityRequestType, "request_id": "r", "operation": "upload", "max_bytes": float64(maxFileUploadBytes + 1)},
	} {
		issuer := &recordingFileIssuer{}
		h := &Hub{fileIssuer: issuer}
		got := h.fileCapabilityResponse(context.Background(), &sessionClient{keyID: "k", ownerKey: "o", sessionName: "s"}, model.Envelope{From: "s#k", Meta: meta})
		if issuer.calls != 0 || metaBool(got.Meta, "ok") || metaString(got.Meta, "error") == "" {
			t.Fatalf("meta=%+v calls=%d response=%+v", meta, issuer.calls, got)
		}
		if got.Meta["token"] != nil {
			t.Fatalf("invalid response exposed token: %+v", got.Meta)
		}
	}
}

func TestFileCapabilityUploadIsBoundToAuthenticatedSession(t *testing.T) {
	issuer := &recordingFileIssuer{}
	h := &Hub{fileIssuer: issuer}
	meta := map[string]any{"type": fileCapabilityRequestType, "request_id": "r", "operation": "upload", "max_bytes": float64(1024)}
	got := h.fileCapabilityResponse(context.Background(), &sessionClient{keyID: "k", ownerKey: "owner", sessionName: "agent"}, model.Envelope{From: "agent#k", Meta: meta})
	if !metaBool(got.Meta, "ok") || issuer.req.Operation != "upload" || issuer.req.OwnerKey != "owner" || issuer.req.SessionName != "agent" || issuer.req.MaxBytes != 1024 {
		t.Fatalf("request=%+v response=%+v", issuer.req, got)
	}
}
