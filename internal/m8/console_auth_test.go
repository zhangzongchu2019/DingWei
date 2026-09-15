package m8

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestUnifiedConsoleAuthCoversEveryViewRouteAndSeparatesSharedKey(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.consoleAuthEnabled = true
	now := time.Now()
	for _, p := range []model.ConsolePrincipal{{ID: "a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ua", Active: true}, {ID: "b", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ub", Active: true}} {
		if err := db.UpsertConsolePrincipal(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []model.ConsoleAgent{{ID: "agent-a", KeyID: "shared", SessionName: "session-a", OwnerPrincipal: "a", Active: true}, {ID: "agent-b", KeyID: "shared", SessionName: "session-b", OwnerPrincipal: "b", Active: true}} {
		if err := db.UpsertConsoleAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []model.ConsoleAgentGrant{{PrincipalID: "a", AgentID: "agent-a", Permission: "interact", Active: true}, {PrincipalID: "b", AgentID: "agent-b", Permission: "interact", Active: true}} {
		if err := db.UpsertConsoleAgentGrant(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "login-a", PrincipalID: "a", TokenHash: hashConsoleToken("raw-a"), ExpiresAt: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /view/{sessionName}", hub.RequireConsoleAgent("view", ok))
	mux.HandleFunc("GET /ws/view/{sessionName}", hub.RequireConsoleAgent("view", ok))
	mux.HandleFunc("/view/{sessionName}/{rest...}", hub.RequireConsoleViewSubPath(ok))
	paths := []struct{ method, path string }{{"GET", "/view/session-b"}, {"GET", "/ws/view/session-b"}, {"GET", "/view/session-b/events"}, {"GET", "/view/session-b/status"}, {"POST", "/view/session-b/input"}, {"POST", "/view/session-b/page/resume"}, {"POST", "/view/session-b/page"}}
	for _, tc := range paths {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: consoleSessionCookie, Value: "raw-a"})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s code=%d", tc.method, tc.path, rr.Code)
		}
	}
	req := httptest.NewRequest("POST", "/view/session-a/input", nil)
	req.AddCookie(&http.Cookie{Name: consoleSessionCookie, Value: "raw-a"})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("own input without page capability code=%d", rr.Code)
	}
}

func TestConsoleCapabilityIsConsumedForExactSessionBeforeInput(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.consoleAuthEnabled = true
	now := time.Now()
	for _, p := range []model.ConsolePrincipal{{ID: "a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ua", Active: true}, {ID: "b", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ub", Active: true}} {
		if err := db.UpsertConsolePrincipal(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []model.ConsoleAgent{{ID: "agent-a", KeyID: "shared", SessionName: "session-a", OwnerPrincipal: "a", Active: true}, {ID: "agent-b", KeyID: "shared", SessionName: "session-b", OwnerPrincipal: "b", Active: true}} {
		if err := db.UpsertConsoleAgent(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []model.ConsoleAgentGrant{{PrincipalID: "a", AgentID: "agent-a", Permission: "interact", Active: true}, {PrincipalID: "a", AgentID: "agent-b", Permission: "interact", Active: true}} {
		if err := db.UpsertConsoleAgentGrant(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "login-a", PrincipalID: "a", TokenHash: hashConsoleToken("raw-a"), ExpiresAt: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.RotateConsolePageCapability(ctx, model.ConsolePageCapability{ID: "cap-a", SessionID: "login-a", PrincipalID: "a", AgentID: "agent-a", PageID: "page-a", TokenHash: hashConsoleToken("secret-a"), ExpiresAt: now.Add(time.Minute)}, "shared", "session-a", now); err != nil {
		t.Fatal(err)
	}
	reached := 0
	next := hub.RequireConsoleViewSubPath(func(w http.ResponseWriter, r *http.Request) {
		reached++
		if r.Header.Get("X-DW-Console-Capability-Verified") != "1" {
			t.Fatal("trusted marker missing")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := func(session string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/view/"+session+"/input", nil)
		req.SetPathValue("sessionName", session)
		req.SetPathValue("rest", "input")
		req.AddCookie(&http.Cookie{Name: consoleSessionCookie, Value: "raw-a"})
		req.Header.Set("X-DW-Console-Page", "page-a")
		req.Header.Set("X-DW-Console-Capability", "secret-a")
		req.Header.Set("X-DW-Console-Capability-Verified", "1")
		rr := httptest.NewRecorder()
		next(rr, req)
		return rr
	}
	if rr := request("session-a"); rr.Code != http.StatusNoContent || reached != 1 {
		t.Fatalf("own session code=%d reached=%d", rr.Code, reached)
	}
	// The same valid capability must not cross the session boundary. Removing
	// agentID from ResolveConsolePageCapability makes this mutation turn green.
	if rr := request("session-b"); rr.Code != http.StatusForbidden || reached != 1 {
		t.Fatalf("cross session code=%d reached=%d", rr.Code, reached)
	}
}

func TestDisabledConsoleAuthStripsCallerSuppliedCapabilityMarker(t *testing.T) {
	hub, _, _ := newTestHub(t)
	hub.consoleAuthEnabled = false
	next := hub.RequireConsoleViewSubPath(func(w http.ResponseWriter, r *http.Request) {
		if marker := r.Header.Get("X-DW-Console-Capability-Verified"); marker != "" {
			t.Fatalf("caller-supplied trust marker reached view service: %q", marker)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest("POST", "/view/session-a/input", nil)
	req.SetPathValue("sessionName", "session-a")
	req.SetPathValue("rest", "input")
	req.Header.Set("X-DW-Console-Capability-Verified", "1")
	rr := httptest.NewRecorder()
	next(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("compatibility path code=%d", rr.Code)
	}
}

func TestUnifiedConsoleAuthRejectsMissingAndExpiredSession(t *testing.T) {
	hub, _, _ := newTestHub(t)
	hub.consoleAuthEnabled = true
	next := hub.RequireConsoleAgent("view", func(w http.ResponseWriter, r *http.Request) { t.Fatal("reached protected handler") })
	for _, cookie := range []string{"", "unknown"} {
		req := httptest.NewRequest("GET", "/view/a", nil)
		req.SetPathValue("sessionName", "a")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: consoleSessionCookie, Value: cookie})
		}
		rr := httptest.NewRecorder()
		next(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("cookie=%q code=%d", cookie, rr.Code)
		}
	}
}

func TestParseConsoleAuthEnabled(t *testing.T) {
	for _, value := range []string{"1", "true", "YES", "y", "on"} {
		mode, err := ParseConsoleAuthEnabled(value)
		if err != nil || !mode.Enabled() {
			t.Fatalf("value=%q enabled=%v err=%v", value, mode.Enabled(), err)
		}
	}
	for _, value := range []string{"", "0", "false", "NO", "n", "off"} {
		mode, err := ParseConsoleAuthEnabled(value)
		if err != nil || mode.Enabled() {
			t.Fatalf("value=%q enabled=%v err=%v", value, mode.Enabled(), err)
		}
	}
	if _, err := ParseConsoleAuthEnabled("tru"); err == nil {
		t.Fatal("unknown non-empty flag accepted")
	}
}
