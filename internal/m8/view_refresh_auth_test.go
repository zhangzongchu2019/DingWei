package m8

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// In-process handler recorders only: no httptest server, listener or network.
func TestViewRefreshConsoleAuthority(t *testing.T) {
	h, _, ctx := newTestHub(t)
	authority := identity.NewMemStore()
	for _, name := range []string{"zzc", "lbl"} {
		if err := authority.CreateUser(ctx, identity.User{UserID: name, ShortName: name, State: identity.UserActive}); err != nil {
			t.Fatal(err)
		}
	}
	svc := identity.NewService(authority, nil, nil)
	h.Identity = svc
	h.Onboarding = identity.NewOnboarding(svc)
	h.PublicOrigin = npOrigin
	e := &agentEnv{t: t, ctx: ctx, hub: h, svc: svc}
	_, agentID := e.enroll("refresh-fixture", "Opus50TS")
	for _, name := range []string{"zzc", "lbl"} {
		if err := authority.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("synthetic-" + name), UserIDLC: name, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	issue := func(token, origin string) (int, string) {
		req := httptest.NewRequest("POST", "/api/console/agents/opus50ts.zzc/capability", strings.NewReader(`{"page_id":"synthetic-page"}`))
		req.SetPathValue("sessionName", "opus50ts.zzc")
		req.Header.Set("Origin", origin)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		h.RequireConsoleAgent("interact", h.HandleConsoleCapability)(w, req)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		cap, _ := out["capability"].(string)
		return w.Code, cap
	}
	code, cap := issue("synthetic-zzc", npOrigin)
	if code != 200 || cap == "" {
		t.Fatalf("owner mint: %d", code)
	}
	if code, _ := issue("synthetic-lbl", npOrigin); code != 404 {
		t.Fatalf("foreign owner mint: %d", code)
	}
	if code, _ := issue("synthetic-zzc", "https://foreign.invalid"); code != 403 {
		t.Fatalf("foreign origin mint: %d", code)
	}
	consume := func(token, page, ticket, origin string) int {
		req := httptest.NewRequest("POST", "/view/opus50ts.zzc/input?console_mux=1", strings.NewReader(`{}`))
		req.SetPathValue("sessionName", "opus50ts.zzc")
		req.SetPathValue("rest", "input")
		req.Header.Set("Origin", origin)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		}
		req.Header.Set("X-DW-Console-Page", page)
		req.Header.Set("X-DW-Console-Capability", ticket)
		req.Header.Set("X-DW-Console-Capability-Verified", "1")
		req.Header.Set("X-DW-Console-Principal", "forged")
		w := httptest.NewRecorder()
		h.RequireConsoleViewSubPath(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-DW-Console-Capability-Verified") != "1" || r.Header.Get("X-DW-Console-Principal") != "zzc" {
				w.WriteHeader(403)
				return
			}
			w.WriteHeader(200)
		})(w, req)
		return w.Code
	}
	for _, tc := range []struct {
		name, token, page, ticket, origin string
		want                              int
	}{
		{"owner", "synthetic-zzc", "synthetic-page", cap, npOrigin, 200},
		{"URL and trust-header spoof only", "synthetic-zzc", "synthetic-page", "", npOrigin, 403},
		{"cross principal", "synthetic-lbl", "synthetic-page", cap, npOrigin, 404},
		{"cross page", "synthetic-zzc", "other-page", cap, npOrigin, 403},
		{"no cookie", "", "synthetic-page", cap, npOrigin, 401},
		{"foreign origin", "synthetic-zzc", "synthetic-page", cap, "https://foreign.invalid", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := consume(tc.token, tc.page, tc.ticket, tc.origin); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
	h.NowFunc = func() time.Time { return time.Now().Add(301 * time.Second) }
	if got := consume("synthetic-zzc", "synthetic-page", cap, npOrigin); got != 403 {
		t.Fatalf("expired capability: %d", got)
	}
	h.NowFunc = nil
	if err := authority.RevokeSession(ctx, identity.HashSecret("synthetic-zzc"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := consume("synthetic-zzc", "synthetic-page", cap, npOrigin); got != 401 {
		t.Fatalf("revoked session: %d", got)
	}
	if err := authority.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("synthetic-retained"), UserIDLC: "zzc", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if code, _ := issue("synthetic-retained", npOrigin); code != 200 {
		t.Fatalf("other login remains valid: %d", code)
	}
	if _, err := svc.Revoke(ctx, "zzc", agentID); err != nil {
		t.Fatal(err)
	}
	if code, _ := issue("synthetic-retained", npOrigin); code != 404 {
		t.Fatalf("revoked agent: %d", code)
	}
}
