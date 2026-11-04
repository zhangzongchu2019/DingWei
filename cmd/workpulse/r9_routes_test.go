package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

func TestR9PMResponsibilityRegisteredRouteIsPlatformAdminOnly(t *testing.T) {
	ctx := context.Background()
	h := newplatHub(t)
	mem := identity.NewMemStore()
	now := time.Now().UTC()
	for _, u := range []identity.User{
		{UserID: "root", ShortName: "root", Type: identity.UserPlatformAdmin, State: identity.UserActive, CreatedAt: now},
		{UserID: "manager", ShortName: "manager", Type: identity.UserManager, State: identity.UserActive, CreatedAt: now},
		{UserID: "member", ShortName: "member", Type: identity.UserMember, State: identity.UserActive, CreatedAt: now},
	} {
		if err := mem.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := mem.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("r9-" + u.UserID), UserIDLC: u.UserID, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	svc := identity.NewService(mem, nil, nil)
	h.Identity = svc
	h.Onboarding = identity.NewOnboarding(svc)
	h.PublicOrigin = "https://console.fixture.invalid"
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	mux := http.NewServeMux()
	registerConsoleRoutes(mux, h, true)
	body := `{"provider":"github","instance":"github.com","repository_id":"1","change_directory":"openspec/changes/demo","point_id":"R9-PM01","pm_user_id_lc":"manager"}`
	call := func(method, path, token string) int {
		t.Helper()
		r := httptest.NewRequest(method, h.PublicOrigin+path, strings.NewReader(body))
		r.Header.Set("Origin", h.PublicOrigin)
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	const responsibility = "/api/console/openspec-pm-responsibility"
	if got := call(http.MethodPut, responsibility, ""); got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated route status=%d, want 401", got)
	}
	for _, token := range []string{"r9-manager", "r9-member"} {
		if got := call(http.MethodPut, responsibility, token); got != http.StatusForbidden {
			t.Errorf("non-admin route token=%s status=%d, want 403", token, got)
		}
	}
	if got := call(http.MethodPut, responsibility, "r9-root"); got != http.StatusOK {
		t.Fatalf("active platform-admin route status=%d, want 200", got)
	}
	// With the feature flag off, these newly registered endpoints retain the
	// same externally visible 404 as an unknown route on the r50 console.
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "0")
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, responsibility},
		{http.MethodPut, responsibility},
		{http.MethodDelete, responsibility},
		{http.MethodGet, "/api/console/openspec-pm-requests"},
		{http.MethodPost, "/api/console/openspec-pm-requests"},
		{http.MethodPost, "/api/console/openspec-pm-requests/request-1/response"},
	} {
		if got := call(tc.method, tc.path, "r9-root"); got != http.StatusNotFound {
			t.Errorf("disabled route %s %s status=%d, want 404", tc.method, tc.path, got)
		}
	}
}
