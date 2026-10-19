package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
