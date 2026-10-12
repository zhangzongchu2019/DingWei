package httpapi_test

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type revokeFailStore struct{ identity.Store }

func (s revokeFailStore) RevokeSession(context.Context, string, time.Time) error {
	return errors.New("test unavailable")
}

func TestLogoutRevokesCurrentCookieAndKeepsOtherSession(t *testing.T) {
	e := newN3Env(t)
	raw := "synthetic-current"
	for _, v := range []string{raw, "synthetic-other"} {
		if err := e.ob.Store.CreateSession(context.Background(), identity.Session{TokenHash: identity.HashSecret(v), UserIDLC: "root", ExpiresAt: e.clk.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	hooked := false
	e.ob.OnSessionRevoked = func(ctx context.Context, hash string) error {
		if _, _, err := e.ob.Store.ResolveSession(ctx, hash, e.clk.Now()); !errors.Is(err, identity.ErrNotFound) {
			t.Fatal("hook before authoritative revocation")
		}
		hooked = true
		return nil
	}
	client := e.browser()
	u, err := url.Parse(e.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar.SetCookies(u, []*http.Cookie{{Name: httpapi.SessionCookie, Value: raw}})
	if code, _ := e.call(client, "POST", "/console/logout", nil, true); code != 200 || !hooked {
		t.Fatal("logout did not complete revoke hook", code)
	}
	if _, _, err := e.ob.Store.ResolveSession(context.Background(), identity.HashSecret(raw), e.clk.Now()); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("old cookie reusable")
	}
	if _, _, err := e.ob.Store.ResolveSession(context.Background(), identity.HashSecret("synthetic-other"), e.clk.Now()); err != nil {
		t.Fatal("other session lost", err)
	}
}

func TestLogoutFailureDoesNotClearCookieOrClaimSuccess(t *testing.T) {
	for _, mode := range []string{"storage", "hook", "origin"} {
		t.Run(mode, func(t *testing.T) {
			e := newN3Env(t)
			if mode == "storage" {
				e.ob.Store = revokeFailStore{e.ob.Store}
			}
			if mode == "hook" {
				e.ob.OnSessionRevoked = func(context.Context, string) error { return errors.New("test hook failed") }
			}
			req := httptest.NewRequest("POST", "/console/logout", nil)
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "synthetic"})
			if mode != "origin" {
				req.Header.Set("Origin", origin)
			}
			mux := http.NewServeMux()
			(&httpapi.N3{OB: e.ob, PublicOrigin: origin}).Register(mux)
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			want := 503
			if mode == "origin" {
				want = 403
			}
			if res.Code != want || len(res.Result().Cookies()) != 0 {
				t.Fatal("failure masked as cookie-clearing success", res.Code)
			}
		})
	}
}
