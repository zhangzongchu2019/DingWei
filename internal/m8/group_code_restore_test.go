package m8

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// Recorder-only: synthetic memory identities and a temporary Hub repository; no server/listener or real enrollment.
func TestGroupCodeRestoreAuthorization(t *testing.T) {
	h, _, ctx := newTestHub(t)
	mem := identity.NewMemStore()
	svc := identity.NewService(mem, nil, nil)
	h.Identity, h.Onboarding, h.PublicOrigin = svc, identity.NewOnboarding(svc), npOrigin
	now := time.Now()
	for _, owner := range []string{"alice", "bob"} {
		if err := mem.CreateUser(ctx, identity.User{UserID: owner, ShortName: owner, Type: identity.UserMember, State: identity.UserActive, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := mem.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret(owner + "-token"), UserIDLC: owner, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct{ id, owner string }{{"own", "alice"}, {"revoked", "alice"}, {"foreign", "bob"}, {"leader", "bob"}} {
		if err := mem.CreateHostedAgent(ctx, identity.Agent{ID: item.id, OwnerLC: item.owner, Name: item.id, CreatedAt: now}, identity.Application{ID: "app-" + item.id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mem.RetireAgent(ctx, "revoked", "alice", now); err != nil {
		t.Fatal(err)
	}
	code := "AbCdEf0123456789_-AbCdEf01234567"
	if err := mem.CreateGroup(ctx, identity.Group{Code: code, ShortName: "Team", CreatorLC: "bob", LeaderAgentID: "leader", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agent-groups/{code}/members", h.RequireConsolePrincipal(h.HandleConsoleAgentGroup))
	call := func(code, id, token, origin string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/console/agent-groups/"+code+"/members", strings.NewReader(`{"agent_id":"`+id+`"}`))
		if token != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		}
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("got %d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	t.Run("missing_cookie", func(t *testing.T) { call(code, "own", "", npOrigin, 401) })
	t.Run("wrong_origin", func(t *testing.T) { call(code, "own", "alice-token", "https://wrong.invalid", 403) })
	t.Run("foreign_agent", func(t *testing.T) { call(code, "foreign", "alice-token", npOrigin, 409) })
	t.Run("revoked_agent", func(t *testing.T) { call(code, "revoked", "alice-token", npOrigin, 409) })
	t.Run("wrong_code_case", func(t *testing.T) { call(strings.ToLower(code), "own", "alice-token", npOrigin, 404) })
	t.Run("closed_group", func(t *testing.T) {
		if err := mem.SetGroupAdditionsClosed(ctx, code, "bob", true, now); err != nil {
			t.Fatal(err)
		}
		call(code, "own", "alice-token", npOrigin, 409)
		if err := mem.SetGroupAdditionsClosed(ctx, code, "bob", false, now); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("own_agent_other_owner_open_group", func(t *testing.T) { call(code, "own", "alice-token", npOrigin, 204) })
	t.Run("duplicate_rejected", func(t *testing.T) { call(code, "own", "alice-token", npOrigin, 409) })
}
