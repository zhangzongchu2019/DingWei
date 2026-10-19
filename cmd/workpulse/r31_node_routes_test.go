package main

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestR31AdminNodeAPIAndOneTimeCredential(t *testing.T) {
	d, st, _, _, _ := mkHostedDeps(t)
	users, actor := fenceIdentity(t)
	d.actor = actor
	d.store = st
	d.access = testHostedAccess(t, "") // administrators are not on the hosted allowlist.
	d.isAdmin = func(r *http.Request, id string) bool {
		u, err := users.GetUser(r.Context(), id)
		return err == nil && u.State == identity.UserActive && u.Type == identity.UserPlatformAdmin
	}
	mux := http.NewServeMux()
	registerHostedRoutes(mux, d)
	call := func(method, path, token, bearer, origin, body string) *httptest.ResponseRecorder {
		r := fenceRequest(fenceRoute{method, path}, token, bearer, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	body := `{"id":"nd_new","name":"new","pool":"shared","quota_mode":"hard","quota_policy_revision":"` + strings.Repeat("a", 64) + `","access":{"scope":"public"}}`
	for _, token := range []string{"", "fixture-manager", "fixture-member"} {
		if w := call("POST", "/api/console/admin/hosted/nodes", token, "", testRouteOrigin, body); w.Code != 403 {
			t.Fatalf("nonadmin %s: %d", token, w.Code)
		}
	}
	if w := call("POST", "/api/console/admin/hosted/nodes", "fixture-admin", "", "https://evil.invalid", body); w.Code != 403 {
		t.Fatal("cross origin admitted")
	}
	if w := call("POST", "/api/console/admin/hosted/nodes", "fixture-admin", "Bearer fixture", testRouteOrigin, body); w.Code != 403 {
		t.Fatal("bearer mixed with admin cookie")
	}
	w := call("POST", "/api/console/admin/hosted/nodes", "fixture-admin", "", testRouteOrigin, body)
	if w.Code != 201 {
		t.Fatalf("register %d: %s", w.Code, w.Body.String())
	}
	var reply map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	secret := reply["credential"]
	if secret == "" {
		t.Fatal("missing one-time credential")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response cacheable")
	}
	if err := st.AuthenticateNode(context.Background(), "nd_new", secret); err != nil {
		t.Fatal(err)
	}
	if w = call("GET", "/api/console/admin/hosted/nodes", "fixture-admin", "", "", ""); w.Code != 200 || strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "secret_hash") {
		t.Fatal("list leaked credential")
	}
	if w = call("POST", "/api/console/admin/hosted/nodes", "fixture-admin", "", testRouteOrigin, body); w.Code != 409 || strings.Contains(w.Body.String(), secret) {
		t.Fatal("duplicate re-exposed credential")
	}
	if w = call("PATCH", "/api/console/admin/hosted/nodes/nd_new", "fixture-admin", "", testRouteOrigin, `{"scheduling_state":"disabled"}`); w.Code != 200 {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	for _, action := range []string{"node.register", "node.update"} {
		if count, err := st.CountAudit(context.Background(), action, "nd_new"); err != nil || count != 1 {
			t.Fatalf("node management audit missing action=%s count=%d err=%v", action, count, err)
		}
	}
	r, err := st.GetNodeRegistration(context.Background(), "nd_new", "admin")
	if err != nil || r.SchedulingState != "disabled" || r.Access.Scope != "public" {
		t.Fatalf("patch reset access: %+v %v", r, err)
	}
}
