package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const consoleSessionCookie = "dw_console_session"

type consoleAuthRepository interface {
	ResolveConsoleSession(context.Context, string, time.Time) (*model.ConsoleSession, error)
	ResolveGrantedConsoleAgent(context.Context, string, string, string) (*model.ConsoleAgent, error)
}

type consolePrincipalContextKey struct{}
type consoleAgentContextKey struct{}

func hashConsoleToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RequireConsoleAgent protects every public view surface when unified auth is
// enabled. Authorization is derived only from the explicit principal grant;
// key_id and session_name are returned after authorization as runtime targets.
func (h *Hub) RequireConsoleAgent(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(strings.TrimSpace(os.Getenv("WP_CONSOLE_AUTH_ENABLED")), "true") {
			next(w, r)
			return
		}
		repo, ok := h.Repo.(consoleAuthRepository)
		if !ok {
			http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
			return
		}
		cookie, err := r.Cookie(consoleSessionCookie)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		session, err := repo.ResolveConsoleSession(r.Context(), hashConsoleToken(cookie.Value), time.Now())
		if err != nil {
			http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
			return
		}
		if session == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		sessionName := strings.TrimSpace(r.PathValue("sessionName"))
		agent, err := repo.ResolveGrantedConsoleAgent(r.Context(), session.PrincipalID, sessionName, permission)
		if err != nil {
			http.Error(w, "console auth unavailable", http.StatusServiceUnavailable)
			return
		}
		if agent == nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), consolePrincipalContextKey{}, session.PrincipalID)
		ctx = context.WithValue(ctx, consoleAgentContextKey{}, *agent)
		next(w, r.WithContext(ctx))
	}
}

func (h *Hub) RequireConsoleViewSubPath(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		permission := "view"
		if r.Method != http.MethodGet {
			permission = "interact"
		}
		h.RequireConsoleAgent(permission, next)(w, r)
	}
}
