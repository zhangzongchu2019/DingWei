package m8

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

type clarificationLoginRepository interface {
	CreateClarificationLogin(context.Context, string, string, string, time.Time) error
	ConsumeClarificationLogin(context.Context, string, string, time.Time) (string, error)
}

const clarificationLoginCookie = "dw_clarification_return"

func clarificationLoginHash(s string) string {
	v := sha256.Sum256([]byte(s))
	return hex.EncodeToString(v[:])
}
func clarificationLoginToken(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func clarificationNonce() (string, error) {
	var b [32]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}

// Only this page gets a bounded login continuation. All data endpoints still
// require the normal Console cookie and current object-level authorization.
func (h *Hub) RequireClarificationPage(next http.HandlerFunc) http.HandlerFunc {
	protected := h.RequireConsolePrincipal(h.RequireConsolePlatformAction("read", next))
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Onboarding == nil || !newPlatformWantsPage(r) {
			protected(w, r)
			return
		}
		if c, e := r.Cookie(httpapi.SessionCookie); e == nil {
			if _, e = h.Onboarding.ResolveSession(r.Context(), c.Value); e == nil {
				protected(w, r)
				return
			}
		}
		values, e := url.ParseQuery(r.URL.RawQuery)
		qid := values.Get("question_id")
		if e != nil || len(values) != 1 || len(values["question_id"]) != 1 || qid == "" || len(qid) > 256 || strings.ContainsAny(qid, "\x00\r\n") {
			taskJSON(w, 400, map[string]any{"error": "invalid_question_link"})
			return
		}
		repo, ok := h.Repo.(clarificationLoginRepository)
		if !ok {
			taskJSON(w, 503, map[string]any{"error": "login_continuation_unavailable"})
			return
		}
		state, e := clarificationNonce()
		if e != nil {
			taskJSON(w, 503, map[string]any{"error": "login_continuation_unavailable"})
			return
		}
		cookie, e := clarificationNonce()
		if e != nil {
			taskJSON(w, 503, map[string]any{"error": "login_continuation_unavailable"})
			return
		}
		if e = repo.CreateClarificationLogin(r.Context(), clarificationLoginHash(state), clarificationLoginHash(cookie), qid, h.now()); e != nil {
			taskError(w, e)
			return
		}
		secure := r.TLS != nil || strings.HasPrefix(h.PublicOrigin, "https://")
		http.SetCookie(w, &http.Cookie{Name: clarificationLoginCookie, Value: cookie, Path: "/console/clarification-return", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: 600})
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, "/console/login?clarification_state="+state, http.StatusFound)
	}
}
func (h *Hub) HandleClarificationReturn(w http.ResponseWriter, r *http.Request) {
	// RequireConsolePrincipal wraps this endpoint. A return state never supplies
	// a user, session or role, and never grants access to the selected question.
	values, e := url.ParseQuery(r.URL.RawQuery)
	state := values.Get("state")
	cookie, ce := r.Cookie(clarificationLoginCookie)
	repo, ok := h.Repo.(clarificationLoginRepository)
	if e != nil || len(values) != 1 || len(values["state"]) != 1 || !clarificationLoginToken(state) || ce != nil || !clarificationLoginToken(cookie.Value) || !ok {
		taskJSON(w, 400, map[string]any{"error": "invalid_login_continuation"})
		return
	}
	qid, e := repo.ConsumeClarificationLogin(r.Context(), clarificationLoginHash(state), clarificationLoginHash(cookie.Value), h.now())
	if e != nil {
		taskError(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: clarificationLoginCookie, Value: "", Path: "/console/clarification-return", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(h.PublicOrigin, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, "/console/spec-clarifications?question_id="+url.QueryEscape(qid), http.StatusFound)
}
