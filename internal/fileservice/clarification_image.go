package fileservice

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/clarificationimage"
	"io"
	"net/http"
	"regexp"
	"time"
)

// These private bytes are in the file-service metadata database, never in
// objects, files or spec_permanent_object and never reachable via /f/. Hub
// proxies each read after current question ACL revalidation; there is no public
// bearer download URL. No automatic deletion of linked evidence is performed.
type ClarificationImageHandler struct {
	Repo  *Repository
	Token string
}

var clarificationImageID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (h ClarificationImageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Repo == nil || h.Token == "" || !sameSecret(bearer(r), h.Token) {
		hide(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch r.Method {
	case "POST":
		if r.URL.RawQuery != "" {
			hide(w)
			return
		}
		b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, clarificationimage.MaxBytes+1))
		if e != nil || len(b) > clarificationimage.MaxBytes {
			http.Error(w, "image too large", 413)
			return
		}
		media, e := clarificationimage.Validate(b)
		if e != nil {
			http.Error(w, "unsupported image", 415)
			return
		}
		tx, e := h.Repo.db.BeginTx(r.Context(), nil)
		if e != nil {
			http.Error(w, "private storage unavailable", 503)
			return
		}
		defer tx.Rollback()
		var size int64
		if e = tx.QueryRowContext(r.Context(), `SELECT COALESCE(sum(length(body)),0) FROM clarification_private_image`).Scan(&size); e != nil || size+int64(len(b)) > 1<<30 {
			http.Error(w, "private storage unavailable", 503)
			return
		}
		var nonce [32]byte
		if _, e = rand.Read(nonce[:]); e != nil {
			http.Error(w, "private storage unavailable", 503)
			return
		}
		id := hex.EncodeToString(nonce[:])
		sum := sha256.Sum256(b)
		sha := hex.EncodeToString(sum[:])
		if _, e = tx.ExecContext(r.Context(), `INSERT INTO clarification_private_image(id,media_type,sha256,body,created_at) VALUES(?,?,?,?,?)`, id, media, sha, b, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
			http.Error(w, "private storage unavailable", 503)
			return
		}
		if e = tx.Commit(); e != nil {
			http.Error(w, "private storage unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"file_id": id, "media_type": media, "sha256": sha, "size": len(b)})
	case "GET":
		id := r.URL.Query().Get("file_id")
		if len(r.URL.Query()) != 1 || len(r.URL.Query()["file_id"]) != 1 || !clarificationImageID.MatchString(id) {
			hide(w)
			return
		}
		var b []byte
		var media string
		if e := h.Repo.db.QueryRowContext(r.Context(), `SELECT media_type,body FROM clarification_private_image WHERE id=?`, id).Scan(&media, &b); e != nil {
			hide(w)
			return
		}
		w.Header().Set("Content-Type", media)
		w.Header().Set("Content-Disposition", "inline; filename=clarification-image")
		w.Write(b)
	default:
		hide(w)
	}
}
