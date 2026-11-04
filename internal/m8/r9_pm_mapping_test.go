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

func TestR9PMMappingAdminAndDisabledGuards(t *testing.T) {
	h, _, ctx := newTestHub(t)
	mem := identity.NewMemStore()
	for _, u := range []identity.User{{UserID: "root", ShortName: "root", Type: identity.UserPlatformAdmin, State: identity.UserActive}, {UserID: "manager", ShortName: "manager", Type: identity.UserManager, State: identity.UserActive}, {UserID: "pm", ShortName: "pm", Type: identity.UserMember, State: identity.UserActive}} {
		if err := mem.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"root", "manager"} {
		if err := mem.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("token-" + name), UserIDLC: name, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	svc := identity.NewService(mem, nil, nil)
	h.Identity = svc
	h.Onboarding = identity.NewOnboarding(svc)
	h.PublicOrigin = "https://console.fixture.invalid"
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	handler := h.RequireConsolePrincipal(h.RequireConsolePlatformAdmin(h.HandleR9PMResponsibility))
	invoke := func(method, token, body string) int {
		r := httptest.NewRequest(method, "https://console.fixture.invalid/api/console/openspec-pm-responsibility", strings.NewReader(body))
		if token != "" {
			r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		}
		r.Header.Set("Origin", h.PublicOrigin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Code
	}
	input := `{"provider":"github","instance":"github.com","repository_id":"1","change_directory":"openspec/changes/x","point_id":"PM01","pm_user_id_lc":"pm"}`
	if code := invoke(http.MethodPut, "token-root", input); code != 200 {
		t.Fatalf("active admin mapping set rc=%d", code)
	}
	if code := invoke(http.MethodPut, "token-manager", input); code != http.StatusForbidden {
		t.Fatalf("manager mapping write rc=%d", code)
	}
	if code := invoke(http.MethodPut, "", input); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated mapping write rc=%d", code)
	}
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "true")
	if code := invoke(http.MethodDelete, "token-root", input); code != http.StatusNotFound {
		t.Fatalf("non-exact feature flag must avoid R9 reads/writes rc=%d", code)
	}
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	var out map[string]any
	r := httptest.NewRequest(http.MethodGet, "https://console.fixture.invalid/api/console/openspec-pm-responsibility?provider=github&instance=github.com&repository_id=1&change_directory=openspec%2Fchanges%2Fx&point_id=PM01", nil)
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "token-root"})
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["pm_user_id_lc"] != "pm" {
		t.Fatalf("admin read mapping rc=%d body=%s", w.Code, w.Body.String())
	}
}
