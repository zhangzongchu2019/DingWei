package fileservice

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRetrievalIssuerIsFrozen(t *testing.T) {
	clock := &FixedClock{Time: time.Now()}
	h := IssuerHandler{Capabilities: NewGrantStore(clock, time.Minute), Token: "secret"}
	r := httptest.NewRequest(http.MethodPost, "/internal/files/issue/retrieval", strings.NewReader(`{"subject":"o/s","file_id":"id","method":"HEAD","ttl_seconds":60}`))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d", w.Code)
	}
}

func legacyIssuerKeepsUploadAndRetrievalAudiencesSeparate(t *testing.T) {
	clock := &FixedClock{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)}
	capabilities := NewGrantStore(clock, 2*time.Minute)
	uploads := NewUploadTicketStore(clock, 2*time.Minute)
	h := IssuerHandler{Capabilities: capabilities, Uploads: uploads, Token: "internal-only"}

	retrieval := issue(t, h, "/internal/files/issue/retrieval", map[string]any{
		"subject": "owner/session", "file_id": "f1", "method": "GET", "range": "bytes=0-3", "ttl_seconds": 60,
	})
	if _, err := capabilities.Verify(retrieval, "GET", "f1", "bytes=0-3", clock.Now()); err != nil {
		t.Fatalf("retrieval token did not verify: %v", err)
	}
	if _, err := uploads.VerifyUpload(retrieval, "session", 4, clock.Now()); err == nil {
		t.Fatal("retrieval token crossed into upload audience")
	}

	upload := issue(t, h, "/internal/files/issue/upload", map[string]any{
		"owner_key": "owner", "session_name": "session", "max_bytes": 16, "ttl_seconds": 60,
	})
	if _, err := capabilities.Verify(upload, "GET", "f1", "bytes=0-3", clock.Now()); err == nil {
		t.Fatal("upload token crossed into retrieval audience")
	}
	if _, err := uploads.VerifyUpload(upload, "session", 16, clock.Now()); err != nil {
		t.Fatalf("upload token did not verify: %v", err)
	}
}

func TestIssuerFailsClosedForAuthAndUnknownFields(t *testing.T) {
	clock := &FixedClock{Time: time.Now()}
	h := IssuerHandler{Capabilities: NewGrantStore(clock, time.Minute), Uploads: NewUploadTicketStore(clock, time.Minute), Token: "internal-only"}
	for _, tc := range []struct {
		name, auth, body string
	}{
		{"missing auth", "", `{"subject":"owner/session","file_id":"f","method":"HEAD","ttl_seconds":30}`},
		{"wrong auth", "wrong", `{"subject":"owner/session","file_id":"f","method":"HEAD","ttl_seconds":30}`},
		{"unknown field", "internal-only", `{"subject":"owner/session","file_id":"f","method":"HEAD","ttl_seconds":30,"token":"must-not-be-accepted"}`},
		{"trailing object", "internal-only", `{"subject":"owner/session","file_id":"f","method":"HEAD","ttl_seconds":30}{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/internal/files/issue/retrieval", bytes.NewBufferString(tc.body))
			if tc.auth != "" {
				r.Header.Set("Authorization", "Bearer "+tc.auth)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusNotFound || w.Body.String() != "not found\n" {
				t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
			}
		})
	}
}

func issue(t *testing.T, h http.Handler, path string, body map[string]any) string {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer internal-only")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("issue status=%d body=%q", w.Code, w.Body.String())
	}
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Token == "" {
		t.Fatalf("response=%q err=%v", w.Body.String(), err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("issuer response is cacheable")
	}
	return response.Token
}
