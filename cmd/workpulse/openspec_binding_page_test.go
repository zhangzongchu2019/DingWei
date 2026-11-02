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

func TestR5BindingPageProductionRouteAuthorization(t *testing.T) {
	h := newplatHub(t)
	t.Cleanup(h.Close)
	st := identity.NewMemStore()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []identity.User{
		{UserID: "bindingadmin", ShortName: "ba", Type: identity.UserPlatformAdmin, State: identity.UserActive},
		{UserID: "bindingmanager", ShortName: "bm", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "bindingmember", ShortName: "bu", Type: identity.UserMember, State: identity.UserActive},
	} {
		if err := st.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateSession(ctx, identity.Session{UserIDLC: user.UserID, TokenHash: identity.HashSecret("fixture-" + user.UserID), ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	if err := wireNewPlatform(ctx, mux, h, newPlatformConfig{publicOrigin: "https://binding.example"}, st, nil); err != nil {
		t.Fatal(err)
	}
	registerConsoleRoutes(mux, h, true)
	for _, tc := range []struct {
		name, user string
		want       int
	}{
		{"administrator", "bindingadmin", 200}, {"manager_direct_url", "bindingmanager", 403}, {"member_direct_url", "bindingmember", 403}, {"unauthenticated", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://binding.example/console/openspec-bindings", nil)
			r.Header.Set("Accept", "application/json")
			if tc.user != "" {
				r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "fixture-" + tc.user})
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("BIND_PAGE_ADMIN_ONLY: %s status=%d want=%d", tc.name, w.Code, tc.want)
			}
			page := strings.Contains(w.Body.String(), `id="bindingForm"`)
			if page != (tc.want == 200) {
				t.Fatal("BIND_PAGE_CONTENT_NOT_EXPOSED")
			}
			if tc.want == 200 && (w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'self'") || strings.Contains(w.Body.String(), "/*BINDING_SCRIPT*/")) {
				t.Fatal("BIND_PAGE_POLICY_AND_SCRIPT")
			}
		})
	}
}
