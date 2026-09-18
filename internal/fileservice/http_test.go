package fileservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fixedUploadVerifier struct{ grant UploadGrant }

type fixedCapabilityVerifier struct{ capability Capability }

func (v fixedCapabilityVerifier) Verify(raw, method, fileID, byteRange string, _ time.Time) (Capability, error) {
	if raw == "synthetic" && method == v.capability.Method && fileID == v.capability.FileID && byteRange == v.capability.Range {
		return v.capability, nil
	}
	return Capability{}, ErrCapabilityDenied
}

func (v fixedUploadVerifier) VerifyUpload(raw, session string, n int64, _ time.Time) (UploadGrant, error) {
	if raw != "upload-ticket" || session != v.grant.SessionName || n < 0 {
		return UploadGrant{}, ErrAccessDenied
	}
	return v.grant, nil
}

func uploadRequest(t *testing.T, h http.Handler, body string) File {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/files/upload", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer upload-ticket")
	r.Header.Set("X-DingWei-Session", "session")
	r.Header.Set("X-File-Name", "../bad\r\nname.html")
	r.Header.Set("Content-Type", "text/html")
	r.Header.Set("X-Content-SHA256", hex.EncodeToString(sum[:]))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%q", w.Code, w.Body.String())
	}
	var files []File
	m := h.(HTTPHandler).Manager
	files, err := m.Repo.ListStable(context.Background(), StateReady, 10)
	if err != nil || len(files) != 1 {
		t.Fatalf("ready files=%d err=%v", len(files), err)
	}
	return files[0]
}

func legacyHTTPUploadHeadAndSingleRanges(t *testing.T) {
	m, _, _ := testManager(t, 100)
	grants := NewGrantStore(m.Clock, 2*time.Minute)
	h := HTTPHandler{Manager: m, Capabilities: grants, Uploads: fixedUploadVerifier{UploadGrant{OwnerKey: "owner", SessionName: "session", MaxBytes: 32}}}
	f := uploadRequest(t, h, "abcdef")

	headToken, _, err := grants.Issue("owner/session", f.ID, http.MethodHead, "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	head := httptest.NewRequest(http.MethodHead, "/files/ref/"+f.ID, nil)
	head.Header.Set("Authorization", "Bearer "+headToken)
	hw := httptest.NewRecorder()
	h.ServeHTTP(hw, head)
	if hw.Code != http.StatusOK || hw.Header().Get("Content-Length") != "6" || hw.Header().Get("Content-Disposition") == "" {
		t.Fatalf("HEAD status=%d headers=%v", hw.Code, hw.Header())
	}
	if hw.Header().Get("Content-Type") != "application/octet-stream" || hw.Header().Get("X-Content-Type-Options") != "nosniff" || strings.ContainsAny(hw.Header().Get("Content-Disposition"), "\r\n") {
		t.Fatalf("unsafe download headers: %v", hw.Header())
	}

	for _, tc := range []struct{ raw, body, contentRange string }{
		{"bytes=0-0", "a", "bytes 0-0/6"},
		{"bytes=4-", "ef", "bytes 4-5/6"},
		{"bytes=-2", "ef", "bytes 4-5/6"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			token, _, issueErr := grants.Issue("owner/session", f.ID, http.MethodGet, tc.raw, time.Minute)
			if issueErr != nil {
				t.Fatal(issueErr)
			}
			r := httptest.NewRequest(http.MethodGet, "/files/ref/"+f.ID, nil)
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Range", tc.raw)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusPartialContent || w.Body.String() != tc.body || w.Header().Get("Content-Range") != tc.contentRange {
				t.Fatalf("status=%d body=%q range=%q", w.Code, w.Body.String(), w.Header().Get("Content-Range"))
			}
		})
	}
}

func legacyHTTPRetrievalDenialsDoNotRevealExistence(t *testing.T) {
	m, _, _ := testManager(t, 100)
	grants := NewGrantStore(m.Clock, 2*time.Minute)
	h := HTTPHandler{Manager: m, Capabilities: grants, Uploads: fixedUploadVerifier{UploadGrant{OwnerKey: "owner", SessionName: "session", MaxBytes: 32}}}
	f := uploadRequest(t, h, "abcdef")
	for _, id := range []string{f.ID, "unknown-id"} {
		r := httptest.NewRequest(http.MethodHead, "/files/ref/"+id, nil)
		r.Header.Set("Authorization", "Bearer invalid")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || w.Body.String() != "not found\n" {
			t.Fatalf("id=%s status=%d body=%q", id, w.Code, w.Body.String())
		}
	}
	wrongSubject, _, _ := grants.Issue("other/session", f.ID, http.MethodHead, "", time.Minute)
	r := httptest.NewRequest(http.MethodHead, "/files/ref/"+f.ID, nil)
	r.Header.Set("Authorization", "Bearer "+wrongSubject)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-session status=%d", w.Code)
	}

	removedAt := m.Clock.Now()
	if err := m.Repo.Tombstone(context.Background(), f.ID, StateEvicted, removedAt, "quota_fifo"); err != nil {
		t.Fatal(err)
	}
	valid, _, _ := grants.Issue("owner/session", f.ID, http.MethodHead, "", time.Minute)
	r = httptest.NewRequest(http.MethodHead, "/files/ref/"+f.ID, nil)
	r.Header.Set("Authorization", "Bearer "+valid)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusGone || w.Body.String() != "gone\n" {
		t.Fatalf("tombstone status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestHTTPUploadErrorsAreJSONWhileRetrievalStaysHidden(t *testing.T) {
	m, _, _ := testManager(t, 4)
	m.MaxFileBytes = 4
	h := HTTPHandler{Manager: m, Uploads: fixedUploadVerifier{UploadGrant{OwnerKey: "owner", SessionName: "session", MaxBytes: 4}}}
	r := httptest.NewRequest(http.MethodPost, "/files/upload", strings.NewReader("abcde"))
	r.Header.Set("Authorization", "Bearer upload-ticket")
	r.Header.Set("X-DingWei-Session", "session")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || w.Header().Get("Content-Type") != "application/json" || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("upload status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}

	get := httptest.NewRequest(http.MethodHead, "/files/ref/unknown", nil)
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, get)
	if gw.Code != http.StatusNotFound || gw.Body.String() != "not found\n" {
		t.Fatalf("retrieval status=%d body=%q", gw.Code, gw.Body.String())
	}
}

func TestUploadReturnsAbsoluteDirectURLAndWhitelistedObjectKey(t *testing.T) {
	m, _, _ := testManager(t, 100)
	h := HTTPHandler{Manager: m, Uploads: fixedUploadVerifier{UploadGrant{OwnerKey: "owner", SessionName: "session", MaxBytes: 32}}, PublicBaseURL: "https://files.example"}
	r := httptest.NewRequest(http.MethodPost, "/files/upload", strings.NewReader("data"))
	r.Header.Set("Authorization", "Bearer upload-ticket")
	r.Header.Set("X-DingWei-Session", "session")
	r.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	var got map[string]any
	if json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal("invalid json")
	}
	u, _ := got["url"].(string)
	if !strings.HasPrefix(u, "https://files.example/f/owner/session/26/0910/") || !strings.HasSuffix(u, ".png") || got["media_type"] != "image/png" {
		t.Fatalf("response=%v", got)
	}
}

func TestRetrievalRouteIsFrozen(t *testing.T) {
	h := HTTPHandler{}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/files/ref/0123456789abcdef0123456789abcdef", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("method=%s status=%d", method, w.Code)
		}
	}
}

func legacyHTTPRangeAndObjectFailureSemantics(t *testing.T) {
	m, _, objects := testManager(t, 100)
	grants := NewGrantStore(m.Clock, 2*time.Minute)
	h := HTTPHandler{Manager: m, Capabilities: grants, Uploads: fixedUploadVerifier{UploadGrant{OwnerKey: "owner", SessionName: "session", MaxBytes: 32}}}
	f := uploadRequest(t, h, "abcdef")
	for _, raw := range []string{"bytes=8-9", "bytes=0-1,3-4"} {
		token, _, _ := grants.Issue("owner/session", f.ID, http.MethodGet, raw, time.Minute)
		capabilities := CapabilityVerifier(grants)
		if token == "" {
			token = "synthetic"
			capabilities = fixedCapabilityVerifier{Capability{Subject: "owner/session", FileID: f.ID, Method: http.MethodGet, Range: raw}}
		}
		r := httptest.NewRequest(http.MethodGet, "/files/ref/"+f.ID, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Range", raw)
		w := httptest.NewRecorder()
		testHandler := h
		testHandler.Capabilities = capabilities
		testHandler.ServeHTTP(w, r)
		if w.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Fatalf("range=%s status=%d", raw, w.Code)
		}
	}
	objects.mu.Lock()
	delete(objects.data, f.ObjectKey)
	objects.mu.Unlock()
	token, _, _ := grants.Issue("owner/session", f.ID, http.MethodGet, "bytes=0-0", time.Minute)
	r := httptest.NewRequest(http.MethodGet, "/files/ref/"+f.ID, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Range", "bytes=0-0")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("object failure status=%d", w.Code)
	}
}

type failingPutObjects struct{ memoryObjects }

func (s *failingPutObjects) Put(context.Context, string, io.Reader, int64, string) error {
	return errors.New("injected put failure")
}

func TestHTTPUploadFailuresNeverReturnReference(t *testing.T) {
	m, _, _ := testManager(t, 100)
	m.Objects = &failingPutObjects{memoryObjects{data: map[string][]byte{}}}
	h := HTTPHandler{Manager: m, Uploads: fixedUploadVerifier{UploadGrant{OwnerKey: "owner", SessionName: "session", MaxBytes: 32}}}
	r := httptest.NewRequest(http.MethodPost, "/files/upload", bytes.NewReader([]byte("abc")))
	r.Header.Set("Authorization", "Bearer upload-ticket")
	r.Header.Set("X-DingWei-Session", "session")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "dwfile:") {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	files, err := m.Repo.ListStable(context.Background(), StateReady, 10)
	if err != nil || len(files) != 0 {
		t.Fatalf("ready=%d err=%v", len(files), err)
	}
}
