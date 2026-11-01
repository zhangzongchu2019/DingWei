package m8

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

// configureSpecRead never logs paths, credentials, API errors, or source content.
func (h *Hub) configureSpecRead() {
	if os.Getenv("DW_OPENSPEC_READ_ENABLED") != "1" {
		return
	}
	bindings, ok := h.Repo.(specread.Bindings)
	if !ok {
		return
	}
	github := specread.NewGitHub(os.Getenv("DW_OPENSPEC_GITHUB_CREDENTIAL_FILE"))
	if !github.Ready() {
		return
	}
	repos := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("DW_OPENSPEC_GITHUB_REPOSITORY_IDS"), ",") {
		id = strings.TrimSpace(id)
		if id == "" || strings.Trim(id, "0123456789") != "" || id[0] == '0' {
			return
		}
		repos["github:github.com:"+id] = true
	}
	if len(repos) == 0 {
		return
	}
	h.SpecReader = &specread.Service{Bindings: bindings, Repositories: repos, Providers: map[string]specread.Provider{"github:github.com": github, "gongfeng:git.code.tencent.com": specread.GongFeng{}}}
}
func specReadError(w http.ResponseWriter, err error, out specread.Result) {
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, specread.ErrTimeout):
		status = 504
	case errors.Is(err, specread.ErrForbidden):
		status = 403
	case errors.Is(err, specread.ErrInvalid):
		status = 400
	case errors.Is(err, specread.ErrNotFound):
		status = 404
	case errors.Is(err, specread.ErrLarge):
		status = 413
	case errors.Is(err, specread.ErrEncoding):
		status = 422
	case errors.Is(err, specread.ErrLimit):
		status = 429
	}
	// Only closed, local error messages reach the browser.
	message := specread.ErrUnavailable.Error()
	for _, safe := range []error{specread.ErrTimeout, specread.ErrForbidden, specread.ErrInvalid, specread.ErrNotFound, specread.ErrLarge, specread.ErrEncoding, specread.ErrLimit} {
		if errors.Is(err, safe) {
			message = safe.Error()
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "coordinate": out.Coordinate, "source": out.Source})
}
func (h *Hub) HandleOpenSpecRead(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if h.SpecReader == nil {
		specReadError(w, specread.ErrUnavailable, specread.Result{})
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		specReadError(w, specread.ErrInvalid, specread.Result{})
		return
	}
	for k, v := range q {
		if len(v) != 1 || !(k == "provider" || k == "instance" || k == "repository" || k == "commit" || k == "path" || k == "ref") {
			specReadError(w, specread.ErrInvalid, specread.Result{})
			return
		}
	}
	c := specread.Coordinate{Provider: q.Get("provider"), Instance: q.Get("instance"), Repository: q.Get("repository"), Commit: q.Get("commit"), Path: q.Get("path")}
	out, err := h.SpecReader.Read(r.Context(), platformPrincipal(r), c, q.Get("ref"))
	if err != nil {
		specReadError(w, err, out)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}
func (h *Hub) HandleOpenSpecBinding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.SpecReader == nil {
		specReadError(w, specread.ErrUnavailable, specread.Result{})
		return
	}
	actor := platformPrincipal(r)
	if actor == "" {
		specReadError(w, specread.ErrForbidden, specread.Result{})
		return
	}
	if r.Method == "GET" {
		q := r.URL.Query()
		b, err := h.SpecReader.Bindings.SpecReadBinding(r.Context(), q.Get("principal"), q.Get("provider"), q.Get("instance"))
		if err != nil {
			specReadError(w, specread.ErrUnavailable, specread.Result{})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(b)
		return
	}
	if r.Method != "PUT" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	// Require JSON + a matching Origin for administrative mapping mutations.
	origin, e := url.Parse(r.Header.Get("Origin"))
	if e != nil || origin.Host != r.Host || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "https" && origin.Scheme != "http") || (h.PublicOrigin != "" && r.Header.Get("Origin") != h.PublicOrigin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		specReadError(w, specread.ErrForbidden, specread.Result{})
		return
	}
	var b specread.Binding
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || d.Decode(&struct{}{}) != io.EOF || !b.Valid() {
		specReadError(w, specread.ErrInvalid, specread.Result{})
		return
	}
	if locker, ok := h.Repo.(interface {
		BeginCoordinateReview(context.Context) (func(), error)
	}); ok {
		release, e := locker.BeginCoordinateReview(r.Context())
		if e != nil {
			specReadError(w, specread.ErrUnavailable, specread.Result{})
			return
		}
		defer release()
	}
	h.coordinateReviewAuthMu.Lock()
	defer h.coordinateReviewAuthMu.Unlock()
	var err error
	if r.Method == "PUT" {
		err = h.SpecReader.Bindings.SetSpecReadBinding(r.Context(), actor, b)
	} else {
		err = h.SpecReader.Bindings.DeleteSpecReadBinding(r.Context(), actor, b.Principal, b.Provider, b.Instance)
	}
	if err != nil {
		specReadError(w, specread.ErrUnavailable, specread.Result{})
		return
	}
	w.WriteHeader(204)
}
