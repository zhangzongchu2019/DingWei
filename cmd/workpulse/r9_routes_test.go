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
	call := func(token string) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, h.PublicOrigin+"/api/console/openspec-pm-responsibility", strings.NewReader(body))
		r.Header.Set("Origin", h.PublicOrigin)
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	if got := call(""); got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated route status=%d, want 401", got)
	}
	for _, token := range []string{"r9-manager", "r9-member"} {
		if got := call(token); got != http.StatusForbidden {
			t.Errorf("non-admin route token=%s status=%d, want 403", token, got)
		}
	}
	if got := call("r9-root"); got != http.StatusOK {
		t.Fatalf("active platform-admin route status=%d, want 200", got)
	}
}
