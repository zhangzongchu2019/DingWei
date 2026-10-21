package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/fileservice"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"image"
	"image/png"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestClarificationImageHTTPPrivateServiceAndCurrentQuestionACL(t *testing.T) {
	e, _, _, q := clarificationHTTPFixture(t)
	fs, err := fileservice.OpenRepository(filepath.Join(t.TempDir(), "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	server := httptest.NewServer(fileservice.ClarificationImageHandler{Repo: fs, Token: "SYNTHETIC"})
	defer server.Close()
	issuer, err := NewHTTPFileTicketIssuer(server.URL, server.URL, "SYNTHETIC")
	if err != nil {
		t.Fatal(err)
	}
	e.hub.SetFileTicketIssuer(issuer)
	var imageBytes bytes.Buffer
	png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	call := func(method, user, id string, b []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/console/questions/"+q.ID+"/clarification-images", bytes.NewReader(b))
		r.SetPathValue("questionID", q.ID)
		r.SetPathValue("fileID", id)
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, user))
		w := httptest.NewRecorder()
		e.hub.HandleClarificationImage(w, r)
		return w
	}
	w := call("POST", "lbl", "", imageBytes.Bytes())
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var result struct {
		Image struct {
			ID string `json:"file_id"`
		} `json:"image"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Image.ID == "" {
		t.Fatal(err, w.Body)
	}
	id := result.Image.ID
	w = call("GET", "zzc", id, nil)
	if w.Code != 403 {
		t.Fatal("CLARIFICATION_PRIVATE_STAGED_IMAGE_DISCLOSED", w.Code, w.Body)
	}
	post := clarificationHTTPBody("private-image", "explanation")
	post = strings.TrimSuffix(post, "}") + `,"file_ids":["` + id + `"]}`
	w = clarificationHTTP(e, "POST", q.ID, "", "lbl", "", post, "", false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = call("GET", "zzc", id, nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), imageBytes.Bytes()) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("CLARIFICATION_PRIVATE_FILE_READ_FAILED", w.Code, w.Body)
	}
	// Policy is replaced while the old PM still has the historical D3 snapshot.
	w = clarificationHTTP(e, "POST", "", q.SpecID, "zzc", "", `{"expected_version":1,"pm_user":"zzc"}`, "", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = call("GET", "lbl", id, nil)
	if w.Code != 403 {
		t.Fatal("CLARIFICATION_REVOKED_PM_IMAGE_READ", w.Code, w.Body)
	}
	w = call("POST", "zzc", "", []byte("<svg>bad</svg>"))
	if w.Code != 415 {
		t.Fatal("CLARIFICATION_IMAGE_SVG_HTTP_ACCEPTED", w.Code, w.Body)
	}
	// Direct client file IDs cannot cross the per-question staged-upload ledger.
	ctx, _ := e.db.QuestionAuthorizationContext(context.Background(), "", "", q.ID)
	_, err = e.db.AppendClarification(context.Background(), ctx, store.BlockerActor{UserID: "zzc"}, q.ID, store.ClarificationUpdate{RequestID: "forged-file", Kind: "explanation", EntryRefs: []string{"A1"}, Body: "explanation", Files: []string{strings.Repeat("a", 64)}}, true, func() error { return nil }, e.hub.now())
	if err == nil {
		t.Fatal("CLARIFICATION_UNUPLOADED_FILE_ATTACHED")
	}
}
