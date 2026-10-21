package fileservice

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestClarificationPrivateImagesNeverGetPublicObjects(t *testing.T) {
	repo, e := OpenRepository(filepath.Join(t.TempDir(), "files.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer repo.Close()
	handler := ClarificationImageHandler{Repo: repo, Token: "SYNTHETIC"}
	var body bytes.Buffer
	png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	req := httptest.NewRequest("POST", "/internal/files/clarification-images", bytes.NewReader(body.Bytes()))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("CLARIFICATION_PRIVATE_IMAGE_NO_AUTH")
	}
	req = httptest.NewRequest("POST", "/internal/files/clarification-images", bytes.NewReader(body.Bytes()))
	req.Header.Set("Authorization", "Bearer SYNTHETIC")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var result struct {
		ID  string `json:"file_id"`
		URL string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.ID == "" || result.URL != "" {
		t.Fatal("CLARIFICATION_IMAGE_PUBLIC_URL", w.Body)
	}
	for _, table := range []string{"files", "spec_permanent_object"} {
		var n int
		if e = repo.db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("CLARIFICATION_IMAGE_PUBLIC_METADATA", table, n, e)
		}
	}
	req = httptest.NewRequest("GET", "/internal/files/clarification-images?file_id="+result.ID, nil)
	req.Header.Set("Authorization", "Bearer SYNTHETIC")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body.Bytes()) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	req = httptest.NewRequest("POST", "/internal/files/clarification-images", bytes.NewBufferString("<svg>not an image</svg>"))
	req.Header.Set("Authorization", "Bearer SYNTHETIC")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 415 {
		t.Fatal("CLARIFICATION_IMAGE_SVG_ACCEPTED", w.Code)
	}
}
