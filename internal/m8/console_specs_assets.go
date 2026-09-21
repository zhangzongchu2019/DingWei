package m8

import (
	"embed"
	"net/http"
)

//go:embed static/easymde/easymde.min.js static/easymde/easymde.min.css static/easymde/LICENSE
var consoleSpecAssets embed.FS

var consoleSpecAssetTypes = map[string]string{
	"easymde.min.js":  "text/javascript; charset=utf-8",
	"easymde.min.css": "text/css; charset=utf-8",
	"LICENSE":         "text/plain; charset=utf-8",
}

func (h *Hub) HandleConsoleSpecAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	contentType, allowed := consoleSpecAssetTypes[name]
	if !allowed {
		http.NotFound(w, r)
		return
	}
	body, err := consoleSpecAssets.ReadFile("static/easymde/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(body)
}
