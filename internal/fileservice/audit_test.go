package fileservice

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestAuditHandlerIsFailClosed(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	h := AuditHandler{Repo: repo, Token: "internal-secret"}
	for i, token := range []string{"", "wrong"} {
		r := httptest.NewRequest(http.MethodGet, "/internal/files", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("audit auth case=%d status=%d", i, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/internal/files?limit=10", nil)
	r.Header.Set("Authorization", "Bearer internal-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
