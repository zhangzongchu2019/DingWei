package main

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestR31HostedAccessModes(t *testing.T) {
	for _, mode := range []string{"", "allowlist", "all_active_users"} {
		p, err := parseHostedAccessMode(mode, "owner")
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"owner", "member", "disabled"} {
			p.active = func(_ *http.Request, id string) bool { return id != "disabled" }
			actor := func(*http.Request) (string, error) { return id, nil }
			r := httptest.NewRequest("GET", "/", nil)
			_, ok := p.human(r, actor)
			want := id != "disabled" && (id == "owner" || mode == "all_active_users")
			if ok != want {
				t.Fatalf("mode=%s actor=%s allowed=%v want=%v", mode, id, ok, want)
			}
			r.Header.Set("Authorization", "Bearer fixture")
			if _, ok = p.human(r, actor); ok {
				t.Fatal("bearer inherited human authority")
			}
		}
	}
	if _, err := parseHostedAccessMode("all", "owner"); err == nil {
		t.Fatal("unknown mode accepted")
	}
	p, _ := parseHostedAccessMode("all_active_users", "")
	if _, ok := p.human(httptest.NewRequest("GET", "/", nil), func(*http.Request) (string, error) { return "", errors.New("invalid session") }); ok {
		t.Fatal("invalid session admitted")
	}
}

// Exercise the HTTP fence and business handler together, using real N3 sessions.
func TestR31AllActiveHTTPReachesQuotaAndDisableIsLive(t *testing.T) {
	d, st, _, _, _ := mkHostedDeps(t)
	users, actor := fenceIdentity(t)
	d.actor = actor
	p, err := parseHostedAccessMode("all_active_users", "owner")
	if err != nil {
		t.Fatal(err)
	}
	p.active = func(r *http.Request, id string) bool {
		u, e := users.GetUser(r.Context(), id)
		return e == nil && u.State == identity.UserActive
	}
	d.access = p
	mux := http.NewServeMux()
	registerHostedRoutes(mux, d)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, fenceRequest(fenceRoute{method, path}, "fixture-member", "", strings.NewReader(body)))
		return w
	}
	if w := call("GET", "/api/console/hosted/options", ""); w.Code != 200 {
		t.Fatalf("active nonlisted options: %d %s", w.Code, w.Body)
	}
	accounts, err := st.ListUsableAccounts(context.Background(), "zzc", "", "")
	if err != nil || len(accounts) != 1 {
		t.Fatal("fixture account unavailable")
	}
	if err := st.PutQuota(context.Background(), hosted.Quota{UserLC: "member"}, "admin"); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"MemberAgent","node_id":"auto","cli":"codex","llm":"gpt","account_id":"` + accounts[0].ID + `","plan_id":"1c1.5g40"}`
	if w := call("POST", "/api/console/hosted/agents", body); w.Code != 409 || !strings.Contains(w.Body.String(), "quota_exceeded") {
		t.Fatalf("nonlisted creation did not reach quota: %d %s", w.Code, w.Body)
	}
	if err := users.SetUserState(context.Background(), "member", identity.UserDisabled, time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/console/hosted/options", ""); w.Code != 403 {
		t.Fatalf("disabled existing cookie admitted: %d", w.Code)
	}
}
