package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeFileBrowser struct {
	openErr error
	opens   int
}

func TestHTTPFileBrowserPreviewRequiresIssuedTickets(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	issued := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/internal/files/audit":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"files":[{"ID":%q,"OwnerKey":"owner","SessionName":"agent"}]}`, id)
		case r.URL.Path == "/internal/files/issue/retrieval":
			issued++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"token":"ticket-%d"}`, issued)
		case r.URL.Path == "/files/ref/"+id && r.Method == http.MethodHead:
			if r.Header.Get("Authorization") != "Bearer ticket-1" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Length", "4")
			w.Header().Set("X-DingWei-Media-Type", "image/png")
		case r.URL.Path == "/files/ref/"+id && r.Method == http.MethodGet:
			if r.Header.Get("Authorization") != "Bearer ticket-2" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Range", "bytes 0-3/4")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "data")
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	b := NewHTTPFileBrowser(s.URL, s.URL, "issuer", "audit")
	c, err := b.Open(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Body.Close()
	body, _ := io.ReadAll(c.Body)
	if string(body) != "data" || issued != 2 {
		t.Fatalf("body=%q issued=%d", body, issued)
	}
}

func (f *fakeFileBrowser) List(context.Context, int, int) (FilePage, error) {
	return FilePage{Files: []AdminFile{{ID: "0123456789abcdef0123456789abcdef", OriginalName: "a.png", SizeBytes: 4, SHA256: "abcd", OwnerKey: "owner", SessionName: "agent", CreatedAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)}}}, nil
}
func (f *fakeFileBrowser) Open(context.Context, string) (FileContent, error) {
	f.opens++
	if f.openErr != nil {
		return FileContent{}, f.openErr
	}
	return FileContent{Body: io.NopCloser(strings.NewReader("data")), Size: 4, MediaType: "image/png"}, nil
}

func TestAdminFilesRequireAuthAndUseBrowserCapability(t *testing.T) {
	fb := &fakeFileBrowser{}
	// Mount used the same server returned by helper; recover it through a fresh setup.
	srv, _, mux, _ := newAdminTestServer(t)
	srv.Files = fb
	srv.sessions["tok"] = "admin"
	for _, path := range []string{"/admin/files", "/admin/files/0123456789abcdef0123456789abcdef/content"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("unauth %s status=%d", path, w.Code)
		}
	}
	page := getAuth(t, mux, "/admin/files")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "0123456789abcdef0123456789abcdef") {
		t.Fatalf("page=%d %q", page.Code, page.Body.String())
	}
	content := getAuth(t, mux, "/admin/files/0123456789abcdef0123456789abcdef/content")
	if content.Code != 200 || content.Body.String() != "data" || fb.opens != 1 {
		t.Fatalf("content=%d body=%q opens=%d", content.Code, content.Body.String(), fb.opens)
	}
}

func TestAdminFilePreviewFailsClosedWhenTicketPathFails(t *testing.T) {
	srv, _, mux, _ := newAdminTestServer(t)
	fb := &fakeFileBrowser{openErr: errors.New("ticket issuance disabled")}
	srv.Files = fb
	srv.sessions["tok"] = "admin"
	req := httptest.NewRequest(http.MethodGet, "/admin/files/0123456789abcdef0123456789abcdef/content", nil)
	req.AddCookie(&http.Cookie{Name: "wp_admin", Value: "tok"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || w.Body.String() == "data" || fb.opens != 1 {
		t.Fatalf("status=%d body=%q opens=%d", w.Code, w.Body.String(), fb.opens)
	}
}
