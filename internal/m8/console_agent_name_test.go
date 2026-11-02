package m8

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

func TestR14S023PGNameCheckAuthorizationAndUniqueIndex(t *testing.T) {
	e := newShimEnv(t)
	pg := r32UsePGIdentity(t, e)
	for owner, tok := range map[string]string{"zzc": e.tokZZC, "lbl": e.tokLBL} {
		if err := pg.CreateSession(e.ctx, identity.Session{TokenHash: identity.HashSecret(tok), UserIDLC: owner, ExpiresAt: e.svc.Now().Add(identity.SessionTTL)}); err != nil {
			t.Fatal(err)
		}
	}
	made, err := e.svc.IssueHostedAgent(e.ctx, "zzc", "Sleeping", "", "zzc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pg.SetAgentSuspended(e.ctx, made.Agent.ID, true, "zzc", e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.IssueHostedAgent(e.ctx, "lbl", "ForeignOnly", "", "lbl"); err != nil {
		t.Fatal(err)
	}
	call := func(tok, query string) (int, string) {
		req := httptest.NewRequest("GET", "/api/console/agent-name-used?"+query, nil)
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		}
		w := httptest.NewRecorder()
		e.hub.RequireConsolePrincipal(e.hub.HandleConsoleAgentNameUsed)(w, req)
		return w.Code, strings.TrimSpace(w.Body.String())
	}
	for _, tc := range []struct {
		name, tok, q string
		status       int
		body         string
	}{
		{"own-active", e.tokZZC, "name_lc=kimi", 200, "true"},
		{"own-suspended", e.tokZZC, "name_lc=sleeping", 200, "true"},
		{"other-owner-same-name", e.tokZZC, "name_lc=foreignonly", 200, "false"},
		{"foreign-owner-checks-own", e.tokLBL, "name_lc=sleeping", 200, "false"},
		{"cross-owner-input", e.tokLBL, "name_lc=sleeping&owner_lc=zzc", 400, ""},
		{"unauthenticated", "", "name_lc=kimi", 401, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := call(tc.tok, tc.q)
			r14Assert(t, code == tc.status && (tc.body == "" || body == tc.body), "S023_NAME_CHECK_ISOLATION", code, body)
		})
	}
	// Both state classes are enforced by the existing PG unique index when the UI
	// preflight is bypassed or races another create. No new PG schema is added.
	for _, name := range []string{"Kimi", "Sleeping"} {
		_, err := e.svc.IssueHostedAgent(e.ctx, "zzc", name, "", "zzc")
		r14Assert(t, errors.Is(err, identity.ErrNameTaken), "S023_DB_UNIQUE_BYPASSED", name, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.svc.IssueHostedAgent(e.ctx, "zzc", "Concurrent", "", "zzc")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, identity.ErrNameTaken) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	r14Assert(t, success == 1 && conflict == 1, "S023_CONCURRENT_DUPLICATE", success, conflict)
}
