package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
)

type recordingFileIssuer struct {
	req       FileTicketRequest
	calls     int
	err       error
	expiresAt time.Time
}

type recordingArchiveClient struct {
	req   FileArchiveRequest
	calls int
	err   error
}

func (c *recordingArchiveClient) SetFileArchived(_ context.Context, req FileArchiveRequest) error {
	c.req, c.calls = req, c.calls+1
	return c.err
}

func (f *recordingFileIssuer) IssueFileTicket(_ context.Context, req FileTicketRequest) (FileTicket, error) {
	f.req, f.calls = req, f.calls+1
	expiresAt := f.expiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(time.Minute)
	}
	return FileTicket{Token: "opaque-ticket", ExpiresAt: expiresAt, PublicBaseURL: "https://files.example"}, f.err
}

func TestFileRetrievalCapabilityIsFrozen(t *testing.T) {
	for _, operation := range []string{"get", "head"} {
		issuer := &recordingFileIssuer{}
		h := &Hub{fileIssuer: issuer}
		c := &sessionClient{keyID: "key-a", ownerKey: "owner-a", sessionName: "session-a"}
		env := model.Envelope{From: "session-a#key-a", Meta: map[string]any{
			"type": fileCapabilityRequestType, "request_id": "request-1", "operation": operation,
			"file_ref": "dwfile:0123456789abcdef0123456789abcdef", "range": "bytes=0-15",
			"owner_key": "attacker", "session_name": "other",
		}}
		got := h.fileCapabilityResponse(context.Background(), c, env)
		if issuer.calls != 0 {
			t.Fatalf("operation=%s issuer calls=%d request=%+v", operation, issuer.calls, issuer.req)
		}
		if metaBool(got.Meta, "ok") || metaString(got.Meta, "token") != "" || metaString(got.Meta, "error") != "file retrieval capability frozen" || got.Body != "" {
			t.Fatalf("operation=%s response=%+v", operation, got)
		}
		if got.Meta["owner_key"] != nil || got.Meta["session_name"] != nil {
			t.Fatalf("operation=%s response reflected identity claims: %+v", operation, got.Meta)
		}
	}
}

func TestUploadTicketPublicBaseUsesOnlyTrustedNonCanonicalOrigin(t *testing.T) {
	trusted, err := trustedorigin.ParseList("https://dw.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		trusted []trustedorigin.Origin
		values  []string
		want    string
	}{
		{name: "canonical with empty list falls back", values: []string{"https://dw.cmjrt.com:55055"}, want: "https://files.example"},
		{name: "canonical with trusted list falls back", trusted: trusted, values: []string{"https://dw.cmjrt.com:55055"}, want: "https://files.example"},
		{name: "trusted exact origin", trusted: trusted, values: []string{"https://dw.cmjrt.com:50443"}, want: "https://dw.cmjrt.com:50443"},
		{name: "untrusted origin falls back", trusted: trusted, values: []string{"https://evil.example"}, want: "https://files.example"},
		{name: "empty list noncanonical falls back", values: []string{"https://dw.cmjrt.com:50443"}, want: "https://files.example"},
		{name: "duplicate Origin headers fall back", trusted: trusted, values: []string{"https://dw.cmjrt.com:50443", "https://dw.cmjrt.com:50443"}, want: "https://files.example"},
		{name: "comma Origin falls back", trusted: trusted, values: []string{"https://dw.cmjrt.com:50443, https://evil.example"}, want: "https://files.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Hub{PublicOrigin: "https://dw.cmjrt.com:55055", TrustedOrigins: tc.trusted}
			r := httptest.NewRequest(http.MethodPost, "/api/console/agents/kimi.zzc/file-upload-ticket", nil)
			for _, value := range tc.values {
				r.Header.Add("Origin", value)
			}
			if got := h.uploadTicketPublicBase(r, "https://files.example"); got != tc.want {
				t.Fatalf("uploadTicketPublicBase = %q, want %q", got, tc.want)
			}
		})
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

func TestFileTicketIssuerRejectsURLCredentialsQueryAndFragment(t *testing.T) {
	for _, raw := range []string{
		"https://user:pw@files.example",
		"https://files.example?token=must-not-be-in-url",
		"https://files.example#credential-fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NewHTTPFileTicketIssuer(raw, "https://public.example", "issuer-credential"); err == nil {
				t.Fatalf("unsafe internal URL accepted: %s", raw)
			}
			if _, err := NewHTTPFileTicketIssuer("https://internal.example", raw, "issuer-credential"); err == nil {
				t.Fatalf("unsafe public URL accepted: %s", raw)
			}
		})
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

func TestConsoleUploadTicketUsesGrantedOnlineAgentIdentity(t *testing.T) {
	issuer := &recordingFileIssuer{}
	h := &Hub{fileIssuer: issuer, sessionClients: map[string]map[string]*sessionClient{
		"shared": {"agent-a": {keyID: "shared", ownerKey: "owner-a", sessionName: "agent-a"}},
	}}
	body := bytes.NewBufferString(`{"max_bytes":1234}`)
	req := httptest.NewRequest(http.MethodPost, "/api/console/agents/agent-a/file-upload-ticket", body)
	req = req.WithContext(context.WithValue(req.Context(), consoleAgentContextKey{}, model.ConsoleAgent{KeyID: "shared", SessionName: "agent-a"}))
	w := httptest.NewRecorder()
	h.HandleConsoleFileUploadTicket(w, req)
	if w.Code != http.StatusOK || issuer.calls != 1 || issuer.req.OwnerKey != "owner-a" || issuer.req.SessionName != "agent-a" || issuer.req.MaxBytes != 1234 {
		t.Fatalf("code=%d request=%+v body=%q", w.Code, issuer.req, w.Body.String())
	}
	var response map[string]any
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response["ticket"] != "opaque-ticket" || response["session"] != "agent-a" {
		t.Fatalf("response=%q", w.Body.String())
	}
}

func TestConsoleUploadTicketOriginSelection(t *testing.T) {
	trusted, err := trustedorigin.ParseList("https://dw123.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		publicOrigin  string
		trusted       []trustedorigin.Origin
		origins       []string
		wantUploadURL string
		wantBody      string
	}{
		{name: "empty list preserves canonical 443 response bytes", publicOrigin: "https://dw123.cmjrt.com", origins: []string{"https://dw123.cmjrt.com"}, wantUploadURL: "https://files.example/files/upload", wantBody: "{\"expires_at\":\"2030-01-02T03:04:05Z\",\"session\":\"agent-a\",\"ticket\":\"opaque-ticket\",\"upload_url\":\"https://files.example/files/upload\"}\n"},
		{name: "canonical public origin falls back to file service", publicOrigin: "https://dw123.cmjrt.com", trusted: trusted, origins: []string{"https://dw123.cmjrt.com"}, wantUploadURL: "https://files.example/files/upload"},
		{name: "trusted 50443 origin", publicOrigin: "https://dw123.cmjrt.com", trusted: trusted, origins: []string{"https://dw123.cmjrt.com:50443"}, wantUploadURL: "https://dw123.cmjrt.com:50443/files/upload"},
		{name: "unlisted origin falls back", publicOrigin: "https://dw123.cmjrt.com", trusted: trusted, origins: []string{"https://evil.example"}, wantUploadURL: "https://files.example/files/upload"},
		{name: "duplicate origin header falls back", publicOrigin: "https://dw123.cmjrt.com", trusted: trusted, origins: []string{"https://dw123.cmjrt.com:50443", "https://dw123.cmjrt.com:50443"}, wantUploadURL: "https://files.example/files/upload"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issuer := &recordingFileIssuer{}
			if tc.wantBody != "" {
				issuer.expiresAt = time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
			}
			h := &Hub{fileIssuer: issuer, PublicOrigin: tc.publicOrigin, TrustedOrigins: tc.trusted, sessionClients: map[string]map[string]*sessionClient{
				"shared": {"agent-a": {keyID: "shared", ownerKey: "owner-a", sessionName: "agent-a"}},
			}}
			req := httptest.NewRequest(http.MethodPost, "/api/console/agents/agent-a/file-upload-ticket", bytes.NewBufferString(`{"max_bytes":1234}`))
			for _, origin := range tc.origins {
				req.Header.Add("Origin", origin)
			}
			req = req.WithContext(context.WithValue(req.Context(), consoleAgentContextKey{}, model.ConsoleAgent{KeyID: "shared", SessionName: "agent-a"}))
			w := httptest.NewRecorder()
			h.HandleConsoleFileUploadTicket(w, req)
			var response map[string]any
			if json.Unmarshal(w.Body.Bytes(), &response) != nil || response["upload_url"] != tc.wantUploadURL {
				t.Fatalf("code=%d upload_url=%v want=%q body=%q", w.Code, response["upload_url"], tc.wantUploadURL, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("empty trusted-list response changed byte-for-byte:\n got %q\nwant %q", w.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestConsoleUploadTicketRejectsOfflineAgentBeforeIssuer(t *testing.T) {
	issuer := &recordingFileIssuer{}
	h := &Hub{fileIssuer: issuer, sessionClients: map[string]map[string]*sessionClient{}}
	req := httptest.NewRequest(http.MethodPost, "/api/console/agents/agent-a/file-upload-ticket", bytes.NewBufferString(`{"max_bytes":1}`))
	req = req.WithContext(context.WithValue(req.Context(), consoleAgentContextKey{}, model.ConsoleAgent{KeyID: "shared", SessionName: "agent-a"}))
	w := httptest.NewRecorder()
	h.HandleConsoleFileUploadTicket(w, req)
	if w.Code != http.StatusConflict || issuer.calls != 0 {
		t.Fatalf("code=%d calls=%d", w.Code, issuer.calls)
	}
}

func TestConsoleArchiveRequiresExplicitOwnerRoleAndPassesAuditFields(t *testing.T) {
	h, db, ctx := newTestHub(t)
	h.consoleAuthEnabled = true
	client := &recordingArchiveClient{}
	h.fileArchive = client
	p := model.ConsolePrincipal{ID: "owner-p", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "owner-open-id", DisplayName: "owner-p", Active: true}
	if err := db.UpsertConsolePrincipal(ctx, p); err != nil {
		t.Fatal(err)
	}
	session := model.ConsoleSession{ID: "login", PrincipalID: p.ID, ExpiresAt: time.Now().Add(time.Hour)}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/console/files/0123456789abcdef0123456789abcdef/archive", bytes.NewBufferString(`{"archived":true,"reason":"owner retention"}`))
		r.SetPathValue("fileID", "0123456789abcdef0123456789abcdef")
		r = r.WithContext(context.WithValue(r.Context(), consoleSessionContextKey{}, session))
		w := httptest.NewRecorder()
		h.HandleConsoleFileArchive(w, r)
		return w
	}
	if w := request(); w.Code != http.StatusForbidden || client.calls != 0 {
		t.Fatalf("roleless code=%d calls=%d", w.Code, client.calls)
	}
	if err := db.UpsertConsolePrincipalRole(ctx, model.ConsolePrincipalRole{PrincipalID: p.ID, Role: "owner", OwnerKey: "tenant-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	if w := request(); w.Code != http.StatusNoContent || client.calls != 1 {
		t.Fatalf("owner code=%d calls=%d body=%q", w.Code, client.calls, w.Body.String())
	}
	if client.req.ActorPrincipal != p.ID || client.req.OwnerKey != "tenant-a" || client.req.Reason != "owner retention" || !client.req.Archived {
		t.Fatalf("request=%+v", client.req)
	}
}
