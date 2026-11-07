package m8

import (
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTempGroupMirrorExcludedAndOrdinaryWritesRefused(t *testing.T) {
	v := newViewEnv(t)
	ag, e := v.svc.Store.FindAgentByName(v.ctx, "zzc", "kimi")
	if e != nil {
		t.Fatal(e)
	}
	if e = v.svc.Store.CreateGroup(v.ctx, identity.Group{Code: "AG777", ShortName: "AG777-temp", CreatorLC: "zzc", LeaderAgentID: ag.ID, AdditionsClosed: true, LeaderOnly: true, CreatedAt: v.svc.Now(), UpdatedAt: v.svc.Now()}); e != nil {
		t.Fatal(e)
	}
	call := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, npOrigin+path, strings.NewReader(`{"leader_only":false}`))
		r.SetPathValue("code", "AG777")
		r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: v.tokZZC})
		r.Header.Set("Origin", npOrigin)
		w := httptest.NewRecorder()
		if method == "GET" {
			v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroups)(w, r)
		} else {
			v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroup)(w, r)
		}
		return w
	}
	w := call("GET", "/api/console/agent-groups")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var data struct {
		Groups []struct {
			Code string `json:"code"`
		} `json:"groups"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &data); e != nil {
		t.Fatal(e)
	}
	if len(data.Groups) == 0 {
		t.Fatal("ordinary groups disappeared")
	}
	for _, g := range data.Groups {
		if g.Code == "AG777" {
			t.Fatal("temporary mirror leaked into ordinary groups")
		}
	}
	for _, tc := range []struct{ method, path string }{{"DELETE", "/api/console/agent-groups/AG777"}, {"POST", "/api/console/agent-groups/AG777/communication"}, {"POST", "/api/console/agent-groups/AG777/additions"}} {
		if w = call(tc.method, tc.path); w.Code != 403 {
			t.Fatal("ordinary AG write bypassed D fence", w.Code)
		}
	}
}
func TestTempGroupsConsolePrincipalAndOriginGuard(t *testing.T) {
	v := newViewEnv(t)
	wrapped := v.hub.RequireConsolePrincipal(v.hub.HandleConsoleTempGroups)
	r := httptest.NewRequest("GET", npOrigin+"/api/console/temp-groups", nil)
	w := httptest.NewRecorder()
	wrapped(w, r)
	if w.Code != 401 {
		t.Fatal("anonymous temp group route", w.Code)
	}
	r = httptest.NewRequest("POST", npOrigin+"/api/console/temp-groups/AG1/dissolve", strings.NewReader(`{}`))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: v.tokZZC})
	r.Header.Set("Origin", "https://untrusted.invalid")
	w = httptest.NewRecorder()
	wrapped(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin temp group write", w.Code)
	}
}
