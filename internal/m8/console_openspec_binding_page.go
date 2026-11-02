package m8

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"net/http"
	"strings"
)

//go:embed console_openspec_binding.html
var specBindingHTML string

//go:embed console_openspec_binding.js
var specBindingJS string

// HandleOpenSpecBindingPage is registered behind the existing principal/admin guards.
func (h *Hub) HandleOpenSpecBindingPage(w http.ResponseWriter, r *http.Request) {
	sum := sha256.Sum256([]byte(specBindingJS))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'none'; frame-src 'none'; frame-ancestors 'self'; object-src 'none'; base-uri 'none'; form-action 'none'")
	_, _ = w.Write([]byte(strings.Replace(specBindingHTML, "/*BINDING_SCRIPT*/", specBindingJS, 1)))
}
