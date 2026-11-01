package m8

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"net/http"
	"strings"
)

//go:embed console_openspec_read.html
var specReadHTML string

//go:embed console_openspec_read.js
var specReadJS string

func (h *Hub) HandleOpenSpecReadPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.SpecReader == nil {
		specReadError(w, specread.ErrUnavailable, specread.Result{})
		return
	}
	sum := sha256.Sum256([]byte(specReadJS))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(strings.Replace(specReadHTML, "/*READ_SCRIPT*/", specReadJS, 1)))
}
