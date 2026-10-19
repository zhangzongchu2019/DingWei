package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

type adminViewEnv struct {
	st  *identity.MemStore
	api *httpapi.API
	mux *http.ServeMux
	at  time.Time
}

func newAdminViewEnv(t *testing.T) *adminViewEnv {
	t.Helper()
	ctx := context.Background()
	st := identity.NewMemStore()
	now := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)
	for _, u := range []identity.User{{UserID: "admin", ShortName: "ad", Type: identity.UserPlatformAdmin, State: identity.UserActive}, {UserID: "alice", ShortName: "al", Type: identity.UserMember, State: identity.UserActive}, {UserID: "bob", ShortName: "bo", Type: identity.UserMember, State: identity.UserActive}, {UserID: "manager", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive}, {UserID: "disabled", ShortName: "ds", Type: identity.UserPlatformAdmin, State: identity.UserDisabled}, {UserID: "mustchange", ShortName: "mc", Type: identity.UserPlatformAdmin, State: identity.UserActive, MustChangePassword: true}} {
		u.CreatedAt = now
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("session-" + u.UserID), UserIDLC: u.UserID, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		id, owner, name string
		hosted          bool
	}{{"ag-a", "alice", "Alpha", false}, {"ag-b", "alice", "Beta", true}, {"ag-c", "bob", "Gamma", false}, {"ag-d", "admin", "Delta", false}} {
		app := identity.Application{ID: "app-" + row.id, OwnerLC: row.owner, PairingSalt: "salt", PairingDigest: "digest", PollTokenHash: "poll-" + row.id, DeviceFingerprint: row.id, RequestedAt: now, ExpiresAt: now.Add(time.Hour), State: identity.AppPending}
		if row.hosted {
			if err := st.CreateHostedAgent(ctx, identity.Agent{ID: row.id, OwnerLC: row.owner, Name: row.name}, app, nil); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := st.CreateApplication(ctx, app); err != nil {
				t.Fatal(err)
			}
			if _, err := st.Approve(ctx, identity.ApproveInput{AppID: app.ID, OwnerLC: row.owner, AgentID: row.id, Name: row.name}, now); err != nil {
				t.Fatal(err)
			}
			if err := st.RecordAgentConnection(ctx, row.id, "203.0.113."+map[string]string{"ag-a": "44", "ag-c": "55", "ag-d": "66"}[row.id], now); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, ok, err := st.TakeCredential(ctx, "app-ag-a", identity.HashSecret("agent-fixture-credential"), now); err != nil || !ok {
		t.Fatalf("agent credential seed: %v %v", ok, err)
	}
	svc := identity.NewService(st, func() time.Time { return now }, nil)
	n3 := &httpapi.N3{OB: identity.NewOnboarding(svc)}
	api := &httpapi.API{Svc: svc, Actor: n3.Actor, Online: func(id string) bool { return id == "ag-a" }, HostedAccess: func(*http.Request) bool { return false }, Hosted: func(_ context.Context, id string) (map[string]any, bool) {
		return map[string]any{"node_name": "node-private", "credential": "fixture-must-never-leak", "account_last4": "9999"}, id == "ag-b"
	}}
	api.Svc.Store = &adminNodeFixtureStore{MemStore: st}
	mux := http.NewServeMux()
	api.Register(mux)
	return &adminViewEnv{st: st, api: api, mux: mux, at: now}
}
func (e *adminViewEnv) call(actor, bearer, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if actor != "" {
		r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "session-" + actor})
	}
	if bearer != "" {
		r.Header.Set("Authorization", bearer)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}
func (e *adminViewEnv) audits() int {
	n := 0
	for _, a := range e.st.AuditLog() {
		if a.Action == "agent.admin_list" {
			n++
		}
	}
	return n
}
func TestR31AdminAgentHTTPProjectionAndAudit(t *testing.T) {
	e := newAdminViewEnv(t)
	before := e.audits()
	w := e.call("admin", "", "/api/console/platform/agents?limit=2")
	if w.Code != 200 {
		t.Fatalf("ADMIN_ROUTE_MISSING: %d %s", w.Code, w.Body.String())
	}
	var p struct {
		Agents []struct {
			ID       string `json:"agent_id"`
			FullName string `json:"full_name"`
			Type     string `json:"agent_type"`
			Host     string `json:"host"`
			Online   bool   `json:"online"`
			Owner    struct {
				ID    string `json:"user_id"`
				Short string `json:"short_name"`
			} `json:"owner"`
		} `json:"agents"`
		Next string `json:"next_after"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Agents) != 2 || p.Agents[0].ID != "ag-a" || p.Agents[0].Host != "203.0.113.44" || p.Agents[0].FullName != "Alpha.al" || p.Agents[0].Owner.ID != "alice" || !p.Agents[0].Online {
		t.Fatalf("ADMIN_RESPONSE_SOURCE_MISSING: %s", w.Body.String())
	}
	if p.Agents[1].Type != "hosted" || p.Agents[1].Host != "node-private" {
		t.Fatalf("ADMIN_NODE_HOST_MISSING: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "credential") || strings.Contains(w.Body.String(), "9999") {
		t.Fatal("non-whitelisted hosted details leaked")
	}
	if p.Next != "ag-b" || e.audits() != before+1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("ADMIN_READ_AUDIT_OR_BOUND_MISSING")
	}
	w = e.call("admin", "", "/api/console/platform/agents?limit=2&after="+p.Next)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Gamma.bo") || strings.Contains(w.Body.String(), "Alpha.al") || e.audits() != before+2 {
		t.Fatalf("ADMIN_PAGE_REPEAT_OR_UNAUDITED: %s", w.Body.String())
	}
	for _, a := range e.st.AuditLog() {
		if a.Action == "agent.admin_list" && (strings.Contains(a.Detail, "203.0.113") || strings.Contains(a.Detail, "fixture")) {
			t.Fatal("sensitive audit detail")
		}
	}
}
func TestR31AdminAgentHTTPRejectsNonHumansAndStaleRoles(t *testing.T) {
	for _, actor := range []string{"", "alice", "bob", "manager", "disabled", "mustchange"} {
		t.Run(actor, func(t *testing.T) {
			e := newAdminViewEnv(t)
			w := e.call(actor, "", "/api/console/platform/agents")
			if w.Code == 200 || strings.Contains(w.Body.String(), "203.0.113.44") || e.audits() != 0 {
				t.Fatalf("ADMIN_HTTP_ROLE_GATE_MISSING: %s %d %s", actor, w.Code, w.Body.String())
			}
		})
	}
	for _, cookie := range []string{"", "admin"} {
		t.Run("bearer-"+cookie, func(t *testing.T) {
			e := newAdminViewEnv(t)
			w := e.call(cookie, "Bearer agent-fixture-credential", "/api/console/platform/agents")
			if w.Code != 403 || e.audits() != 0 {
				t.Fatalf("ADMIN_AGENT_BEARER_GATE_MISSING: %d", w.Code)
			}
		})
	}
	for _, change := range []string{"disable", "demote", "expire", "revoke"} {
		t.Run(change, func(t *testing.T) {
			e := newAdminViewEnv(t)
			ctx := context.Background()
			actor := "admin"
			var changeErr error
			switch change {
			case "disable":
				changeErr = e.st.SetUserState(ctx, "admin", identity.UserDisabled, e.at)
			case "demote":
				changeErr = e.st.SetUserType(ctx, "admin", identity.UserMember)
			case "expire":
				actor = "expired-admin"
				changeErr = e.st.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("session-expired-admin"), UserIDLC: "admin", ExpiresAt: e.at.Add(-time.Second)})
			case "revoke":
				changeErr = e.st.RevokeSession(ctx, identity.HashSecret("session-admin"), e.at)
			}
			if changeErr != nil {
				t.Fatal(changeErr)
			}
			w := e.call(actor, "", "/api/console/platform/agents")
			if w.Code == 200 || e.audits() != 0 {
				t.Fatalf("ADMIN_STALE_ROLE_GATE_MISSING: %s %d %s", change, w.Code, w.Body.String())
			}
		})
	}
}
func TestR31AdminAgentOrdinaryListRemainsPrivate(t *testing.T) {
	e := newAdminViewEnv(t)
	for _, actor := range []string{"alice", "bob", "admin"} {
		w := e.call(actor, "", "/api/console/agents")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var payload struct {
			Agents []struct {
				ID string `json:"agent_id"`
			} `json:"agents"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		expected := map[string]map[string]bool{"alice": {"ag-a": true, "ag-b": true}, "bob": {"ag-c": true}, "admin": {"ag-d": true}}[actor]
		if len(payload.Agents) != len(expected) {
			t.Fatalf("ORDINARY_LIST_OWNER_FENCE_MISSING: %s %s", actor, w.Body.String())
		}
		for _, agent := range payload.Agents {
			if !expected[agent.ID] {
				t.Fatalf("ORDINARY_LIST_OWNER_FENCE_MISSING: %s %s", actor, w.Body.String())
			}
		}
		if actor != "alice" && (strings.Contains(w.Body.String(), "Alpha.al") || strings.Contains(w.Body.String(), "203.0.113.44")) {
			t.Fatalf("ORDINARY_LIST_OWNER_FENCE_MISSING: %s %s", actor, w.Body.String())
		}
	}
	w := e.call("alice", "Bearer agent-fixture-credential", "/api/console/agents")
	if strings.Contains(w.Body.String(), "203.0.113.44") {
		t.Fatal("ORDINARY_BEARER_SOURCE_GATE_MISSING")
	}
	if e.audits() != 0 {
		t.Fatal("ordinary list incorrectly uses admin read")
	}
}
func TestR31AdminAgentPaginationValidationAndReadOnly(t *testing.T) {
	e := newAdminViewEnv(t)
	for _, suffix := range []string{"?limit=0", "?limit=201", "?limit=-1", "?limit=oops", "?limit=1&limit=2", "?after=x&after=y", "?after=" + strings.Repeat("x", 129)} {
		if w := e.call("admin", "", "/api/console/platform/agents"+suffix); w.Code != 400 {
			t.Fatalf("ADMIN_HTTP_BOUND_MISSING: %s %d", suffix, w.Code)
		}
	}
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		r := httptest.NewRequest(method, "/api/console/platform/agents", nil)
		r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "session-admin"})
		w := httptest.NewRecorder()
		e.mux.ServeHTTP(w, r)
		if w.Code != 405 {
			t.Fatalf("readonly method %s status%d", method, w.Code)
		}
	}
	if e.audits() != 0 {
		t.Fatal("invalid reads audited as success")
	}
}

type adminProjectionSpy struct {
	identity.Store
	calls int
	fail  bool
}

func (s *adminProjectionSpy) ListAdminAgents(_ context.Context, _, _ string, _ int, _ time.Time) (identity.AdminAgentPage, error) {
	s.calls++
	page := identity.AdminAgentPage{Agents: []identity.AdminAgentRecord{{ID: "spy", Name: "Private", OwnerID: "other", OwnerShortName: "ot", Origin: identity.OriginSelfhost, SourceIP: "198.51.100.88"}}}
	if s.fail {
		return page, identity.ErrBadRequest
	}
	return page, nil
}
func TestR31AdminAgentHTTPRoleGateBeforeProjection(t *testing.T) {
	e := newAdminViewEnv(t)
	spy := &adminProjectionSpy{Store: e.st}
	e.api.Svc.Store = spy
	for _, actor := range []string{"alice", "manager"} {
		w := e.call(actor, "", "/api/console/platform/agents")
		if w.Code != 403 || spy.calls != 0 || strings.Contains(w.Body.String(), "198.51.100.88") {
			t.Fatal("ADMIN_HTTP_ROLE_FENCE_MISSING")
		}
	}
}
func TestR31AdminAgentHTTPReadFailureNeverReturnsPartialPage(t *testing.T) {
	e := newAdminViewEnv(t)
	spy := &adminProjectionSpy{Store: e.st, fail: true}
	e.api.Svc.Store = spy
	w := e.call("admin", "", "/api/console/platform/agents")
	if w.Code == 200 || strings.Contains(w.Body.String(), "198.51.100.88") {
		t.Fatal("ADMIN_FAILED_READ_LEAKED_PARTIAL_PAGE")
	}
}
func TestR31AdminAgentBearerCannotInheritHumanCookie(t *testing.T) {
	e := newAdminViewEnv(t)
	w := e.call("admin", "Bearer agent-fixture-credential", "/api/console/platform/agents")
	if w.Code != 403 || strings.Contains(w.Body.String(), "203.0.113.44") || e.audits() != 0 {
		t.Fatalf("ADMIN_BEARER_INHERITED_HUMAN_ROLE: %d %s", w.Code, w.Body.String())
	}
}

// HTTP-only fixture projection. Production uses PGStore's joined transaction;
// the production wiring test never injects this adapter or Hosted callbacks.
type adminNodeFixtureStore struct{ *identity.MemStore }

func (s *adminNodeFixtureStore) ListAdminAgents(ctx context.Context, actor, after string, limit int, at time.Time) (identity.AdminAgentPage, error) {
	page, err := s.MemStore.ListAdminAgents(ctx, actor, after, limit, at)
	if err != nil {
		return identity.AdminAgentPage{}, err
	}
	for i := range page.Agents {
		if page.Agents[i].ID == "ag-b" {
			page.Agents[i].HostedNodeName = "node-private"
		}
	}
	return page, nil
}
