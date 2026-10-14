package m8

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// blockingSpecGrantRepo holds the first signing transaction at the repository
// boundary so a second owner approval exercises the in-memory reservation,
// not timing luck in SQLite.
type blockingSpecGrantRepo struct {
	store.Repository
	entered     chan struct{}
	release     chan struct{}
	rotateErr   error
	once        sync.Once
	mu          sync.Mutex
	revokeCalls int
}

var _ consoleAuthRepository = (*blockingSpecGrantRepo)(nil)

func (r *blockingSpecGrantRepo) auth() consoleAuthRepository {
	return r.Repository.(consoleAuthRepository)
}

func (r *blockingSpecGrantRepo) ResolveConsoleSession(ctx context.Context, tokenHash string, now time.Time) (*model.ConsoleSession, error) {
	return r.auth().ResolveConsoleSession(ctx, tokenHash, now)
}

func (r *blockingSpecGrantRepo) TouchConsoleSession(ctx context.Context, sessionID string, expiry, now time.Time) error {
	return r.auth().TouchConsoleSession(ctx, sessionID, expiry, now)
}

func (r *blockingSpecGrantRepo) ResolveGrantedConsoleAgent(ctx context.Context, principalID, agentID, permission string) (*model.ConsoleAgent, error) {
	return r.auth().ResolveGrantedConsoleAgent(ctx, principalID, agentID, permission)
}

func (r *blockingSpecGrantRepo) ListConsoleAgents(ctx context.Context, principalID string) ([]model.ConsoleAgent, error) {
	return r.auth().ListConsoleAgents(ctx, principalID)
}

func (r *blockingSpecGrantRepo) RotateConsolePageCapability(ctx context.Context, c model.ConsolePageCapability, keyID, sessionName string, now time.Time) error {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	if r.rotateErr != nil {
		return r.rotateErr
	}
	return r.auth().RotateConsolePageCapability(ctx, c, keyID, sessionName, now)
}

func (r *blockingSpecGrantRepo) RevokeConsolePageCapability(ctx context.Context, id, sessionID, principalID, agentID, pageID string, now time.Time) (bool, error) {
	r.mu.Lock()
	r.revokeCalls++
	r.mu.Unlock()
	return r.auth().RevokeConsolePageCapability(ctx, id, sessionID, principalID, agentID, pageID, now)
}

func (r *blockingSpecGrantRepo) revocations() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revokeCalls
}

func (r *blockingSpecGrantRepo) ResolveConsolePageCapability(ctx context.Context, tokenHash, agentID, pageID string, now time.Time) (*model.ConsolePageCapability, error) {
	return r.auth().ResolveConsolePageCapability(ctx, tokenHash, agentID, pageID, now)
}

func TestSpecGrantRevokeInvalidatesIssuedCapability(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	session, ok := e.hub.newPlatformSessionProjection(req)
	if !ok {
		t.Fatal("new-platform session projection unavailable")
	}
	if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", session); err != nil {
		t.Fatal(err)
	}
	const pageID = "console-specs:new"
	const oldCapability = "capability-that-must-be-revoked"
	if err := e.db.RotateConsolePageCapability(e.ctx, model.ConsolePageCapability{
		ID: "spec-grant-old-capability", SessionID: session.ID, PrincipalID: "zzc", AgentID: ag.ID,
		PageID: pageID, TokenHash: hashConsoleToken(oldCapability), ExpiresAt: e.hub.now().Add(newPlatformCapabilityTTL),
	}, AgentKeyID("zzc"), ag.NameLC(), e.hub.now()); err != nil {
		t.Fatal(err)
	}
	e.hub.specGrantRequests["grant-revoke"] = &specGrantRequest{
		ID: "grant-revoke", OwnerKey: AgentKeyID("zzc"), SessionName: ag.NameLC(), AgentID: ag.ID,
		PrincipalID: "zzc", Action: "create", PageID: pageID, State: "consumed",
		CapabilitySessionID: session.ID, CapabilityID: "spec-grant-old-capability",
	}
	// A later approval for the same page has its own capability generation.
	// Revoking the old grant must leave this newer token usable.
	const siblingCapability = "later-capability-that-must-survive"
	if err := e.db.RotateConsolePageCapability(e.ctx, model.ConsolePageCapability{
		ID: "spec-grant-later-capability", SessionID: session.ID, PrincipalID: "zzc", AgentID: ag.ID,
		PageID: pageID, TokenHash: hashConsoleToken(siblingCapability), ExpiresAt: e.hub.now().Add(newPlatformCapabilityTTL),
	}, AgentKeyID("zzc"), ag.NameLC(), e.hub.now()); err != nil {
		t.Fatal(err)
	}
	if got, err := e.db.ResolveConsolePageCapability(e.ctx, hashConsoleToken(siblingCapability), ag.ID, pageID, e.hub.now()); err != nil || got == nil {
		t.Fatalf("later capability unavailable before old revoke: capability=%+v err=%v", got, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantDecision))
	oldServer := e.srv
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
	if status, body := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/grant-revoke/revoke", e.tokZZC, "", "", ""); status != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", status, body)
	}
	resolved, err := e.db.ResolveConsolePageCapability(e.ctx, hashConsoleToken(oldCapability), ag.ID, pageID, e.hub.now())
	if err != nil || resolved != nil {
		t.Fatalf("old capability survived revoke: capability=%+v err=%v", resolved, err)
	}
	if state := e.hub.specGrantRequests["grant-revoke"].State; state != "revoked" {
		t.Fatalf("grant state=%q, want revoked", state)
	}
	if status, body := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/grant-revoke/revoke", e.tokZZC, "", "", ""); status != http.StatusNoContent {
		t.Fatalf("repeat revoke status=%d body=%s", status, body)
	}
	if got, err := e.db.ResolveConsolePageCapability(e.ctx, hashConsoleToken(siblingCapability), ag.ID, pageID, e.hub.now()); err != nil || got == nil {
		t.Fatalf("later capability was revoked with the old grant: capability=%+v err=%v", got, err)
	}
}

func TestSpecGrantActionBindsPageScope(t *testing.T) {
	if page, ok := validSpecGrantAction("create", ""); !ok || page != ConsoleAgentCapabilitySpecsNew {
		t.Fatalf("create page=%q ok=%v", page, ok)
	}
	if page, ok := validSpecGrantAction("update", "spec-a"); !ok || page != ConsoleAgentSpecCapabilityPage("spec-a") {
		t.Fatalf("update page=%q ok=%v", page, ok)
	}
	for _, in := range [][2]string{{"create", "spec-a"}, {"update", ""}, {"delete", "spec-a"}} {
		if _, ok := validSpecGrantAction(in[0], in[1]); ok {
			t.Fatalf("unexpected valid action=%q spec=%q", in[0], in[1])
		}
	}
}

func TestSpecGrantRevokeDeniesAgentWriteForGrantedAndConsumed(t *testing.T) {
	for _, state := range []string{"granted", "consumed"} {
		t.Run(state, func(t *testing.T) {
			e := newShimEnv(t)
			if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
				t.Fatal(err)
			}
			ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
			session, ok := e.hub.newPlatformSessionProjection(req)
			if !ok {
				t.Fatal("new-platform session projection unavailable")
			}
			if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", session); err != nil {
				t.Fatal(err)
			}
			const pageID = "console-specs:new"
			raw := "revoked-agent-write-" + state
			capID := "grant-agent-write-" + state
			if err := e.db.RotateConsolePageCapability(e.ctx, model.ConsolePageCapability{
				ID: capID, SessionID: session.ID, PrincipalID: "zzc", AgentID: ag.ID, PageID: pageID,
				TokenHash: hashConsoleToken(raw), ExpiresAt: e.hub.now().Add(newPlatformCapabilityTTL),
			}, AgentKeyID("zzc"), ag.NameLC(), e.hub.now()); err != nil {
				t.Fatal(err)
			}
			e.hub.specGrantRequests["grant-agent-write"] = &specGrantRequest{
				ID: "grant-agent-write", OwnerKey: AgentKeyID("zzc"), SessionName: ag.NameLC(), AgentID: ag.ID,
				PrincipalID: "zzc", Action: "create", PageID: pageID, State: state,
				CapabilitySessionID: session.ID, CapabilityID: capID,
			}
			mux := http.NewServeMux()
			mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantDecision))
			mux.HandleFunc("POST /api/agent/console/specs", e.hub.RequireConsoleSpecAgentCapability(func(*http.Request) string { return ConsoleAgentCapabilitySpecsNew }, e.hub.HandleConsoleSpecs))
			oldServer := e.srv
			e.srv = httptest.NewServer(mux)
			t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
			if status, body := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/grant-agent-write/revoke", e.tokZZC, "", "", ""); status != http.StatusNoContent {
				t.Fatalf("revoke status=%d body=%s", status, body)
			}
			if status, body := e.do(http.MethodPost, "/api/agent/console/specs", "", e.zKimiCred, raw, agentSpecBody(t, "revoked "+state)); status != http.StatusForbidden {
				t.Fatalf("revoked %s Agent write status=%d body=%s, want 403", state, status, body)
			}
		})
	}
}

func TestSpecGrantApproveSynchronizesFreshOwnerSession(t *testing.T) {
	e := newShimEnv(t)
	// Deliberately do not call ReconcileNewPlatformConsole or syncNewPlatformUser:
	// this is the first owner request after login, the path that used to 403.
	ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	e.hub.specGrantRequests["fresh-owner-grant"] = &specGrantRequest{
		ID: "fresh-owner-grant", OwnerKey: AgentKeyID("zzc"), SessionName: ag.NameLC(), AgentID: ag.ID,
		PrincipalID: "zzc", Action: "create", PageID: ConsoleAgentCapabilitySpecsNew, State: "pending",
		ExpiresAt: e.hub.now().Add(specGrantRequestTTL),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantDecision))
	oldServer := e.srv
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
	if status, body := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/fresh-owner-grant/approve", e.tokZZC, "", "", ""); status != http.StatusNoContent {
		t.Fatalf("fresh owner direct approve status=%d body=%s, want 204", status, body)
	}
	if got := e.hub.specGrantRequests["fresh-owner-grant"]; got.State != "granted" || got.CapabilityID == "" || got.CapabilitySessionID == "" {
		t.Fatalf("fresh owner grant=%+v, want granted capability", got)
	}
}

func TestSpecGrantListShowsBothDeadlinesAndExpiresEachWindow(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	now := e.hub.now()
	pendingDeadline := now.Add(10 * time.Minute)
	capabilityDeadline := now.Add(5 * time.Minute)
	e.hub.specGrantRequests = map[string]*specGrantRequest{
		"pending":                 {ID: "pending", SessionName: ag.NameLC(), PrincipalID: "zzc", State: "pending", ExpiresAt: pendingDeadline},
		"granted":                 {ID: "granted", SessionName: ag.NameLC(), PrincipalID: "zzc", State: "granted", ExpiresAt: pendingDeadline, CapabilityExpiresAt: capabilityDeadline, capability: "not-in-output"},
		"request-expired":         {ID: "request-expired", SessionName: ag.NameLC(), PrincipalID: "zzc", State: "pending", ExpiresAt: now.Add(-time.Second)},
		"granted-request-expired": {ID: "granted-request-expired", SessionName: ag.NameLC(), PrincipalID: "zzc", State: "granted", ExpiresAt: now.Add(-time.Second), CapabilityExpiresAt: now.Add(time.Minute), capability: "must-be-cleared"},
		"capability-expired":      {ID: "capability-expired", SessionName: ag.NameLC(), PrincipalID: "zzc", State: "granted", ExpiresAt: pendingDeadline, CapabilityExpiresAt: now.Add(-time.Second), capability: "must-be-cleared"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/agents/{sessionName}/spec-grant-requests", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantRequests))
	oldServer := e.srv
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
	status, body := e.do(http.MethodGet, "/api/console/agents/Kimi.zzc/spec-grant-requests", e.tokZZC, "", "", "")
	if status != http.StatusOK {
		t.Fatalf("list status=%d body=%s", status, body)
	}
	var got struct {
		Requests []struct {
			ID                  string  `json:"id"`
			Status              string  `json:"status"`
			ExpiresAt           string  `json:"expires_at"`
			RequestExpiresAt    string  `json:"request_expires_at"`
			CapabilityExpiresAt *string `json:"capability_expires_at"`
		}
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	rows := map[string]struct {
		Status              string
		RequestExpiresAt    string
		CapabilityExpiresAt *string
	}{}
	for _, row := range got.Requests {
		if row.ExpiresAt != row.RequestExpiresAt {
			t.Fatalf("legacy expires_at and request deadline diverged for %s", row.ID)
		}
		rows[row.ID] = struct {
			Status              string
			RequestExpiresAt    string
			CapabilityExpiresAt *string
		}{row.Status, row.RequestExpiresAt, row.CapabilityExpiresAt}
	}
	if row := rows["pending"]; row.Status != "pending" || row.RequestExpiresAt != pendingDeadline.UTC().Format(time.RFC3339) || row.CapabilityExpiresAt != nil {
		t.Fatalf("pending row=%+v", row)
	}
	if row := rows["granted"]; row.Status != "granted" || row.CapabilityExpiresAt == nil || *row.CapabilityExpiresAt != capabilityDeadline.UTC().Format(time.RFC3339) {
		t.Fatalf("granted row=%+v", row)
	}
	if row := rows["request-expired"]; row.Status != "expired" {
		t.Fatalf("request expiry row=%+v", row)
	}
	if row := rows["granted-request-expired"]; row.Status != "expired" {
		t.Fatalf("granted request expiry row=%+v", row)
	}
	if raw := e.hub.specGrantRequests["granted-request-expired"].capability; raw != "" {
		t.Fatal("request-expired grant remained in Hub memory")
	}
	if row := rows["capability-expired"]; row.Status != "expired" || row.CapabilityExpiresAt == nil {
		t.Fatalf("capability expiry row=%+v", row)
	}
	if raw := e.hub.specGrantRequests["capability-expired"].capability; raw != "" {
		t.Fatal("expired capability remained in Hub memory")
	}
}

func TestSpecGrantApprovalFailureAfterRequestExpiryStaysExpired(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	start := e.hub.now()
	current := start
	e.hub.NowFunc = func() time.Time { return current }
	e.hub.specGrantRequests["signing-expiry"] = &specGrantRequest{
		ID: "signing-expiry", OwnerKey: AgentKeyID("zzc"), SessionName: ag.NameLC(), AgentID: ag.ID,
		PrincipalID: "zzc", Action: "create", PageID: ConsoleAgentCapabilitySpecsNew,
		State: "pending", ExpiresAt: start.Add(time.Second),
	}
	oldRepo := e.hub.Repo
	repo := &blockingSpecGrantRepo{Repository: oldRepo, entered: make(chan struct{}), release: make(chan struct{}), rotateErr: errors.New("signing failed")}
	e.hub.newPlatformShimRepo = e.db
	e.hub.Repo = repo
	t.Cleanup(func() { e.hub.Repo, e.hub.newPlatformShimRepo = oldRepo, nil })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantDecision))
	oldServer := e.srv
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
	result := make(chan int, 1)
	go func() {
		status, _ := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/signing-expiry/approve", e.tokZZC, "", "", "")
		result <- status
	}()
	select {
	case <-repo.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("approval did not reach signing boundary")
	}
	current = start.Add(2 * time.Second)
	close(repo.release)
	if status := <-result; status != http.StatusForbidden {
		t.Fatalf("signing failure status=%d, want 403", status)
	}
	g := e.hub.specGrantRequests["signing-expiry"]
	if g.State != "expired" || g.capability != "" {
		t.Fatalf("expired signing failure grant=%+v", g)
	}
}

func TestSpecGrantApprovalFailureInsideRequestWindowReturnsPendingAndCanRetry(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	start := e.hub.now()
	e.hub.NowFunc = func() time.Time { return start }
	e.hub.specGrantRequests["retry-after-failure"] = &specGrantRequest{
		ID: "retry-after-failure", OwnerKey: AgentKeyID("zzc"), SessionName: ag.NameLC(), AgentID: ag.ID,
		PrincipalID: "zzc", Action: "create", PageID: ConsoleAgentCapabilitySpecsNew,
		State: "pending", ExpiresAt: start.Add(time.Minute),
	}
	oldRepo := e.hub.Repo
	repo := &blockingSpecGrantRepo{Repository: oldRepo, entered: make(chan struct{}), release: make(chan struct{}), rotateErr: errors.New("signing failed")}
	e.hub.newPlatformShimRepo = e.db
	e.hub.Repo = repo
	t.Cleanup(func() { e.hub.Repo, e.hub.newPlatformShimRepo = oldRepo, nil })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantDecision))
	oldServer := e.srv
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
	result := make(chan int, 1)
	go func() {
		status, _ := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/retry-after-failure/approve", e.tokZZC, "", "", "")
		result <- status
	}()
	select {
	case <-repo.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("approval did not reach signing boundary")
	}
	close(repo.release)
	if status := <-result; status != http.StatusForbidden {
		t.Fatalf("signing failure status=%d, want 403", status)
	}
	if state := e.hub.specGrantRequests["retry-after-failure"].State; state != "pending" {
		t.Fatalf("in-window signing failure state=%q, want pending", state)
	}
	repo.rotateErr = nil
	if status, body := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/retry-after-failure/approve", e.tokZZC, "", "", ""); status != http.StatusNoContent {
		t.Fatalf("retry approval status=%d body=%s, want 204", status, body)
	}
	if g := e.hub.specGrantRequests["retry-after-failure"]; g.State != "granted" || g.capability == "" {
		t.Fatalf("retry grant=%+v, want granted capability", g)
	}
}

func TestSpecGrantListExpiryDuringSigningRevokesPersistedCapability(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	start := e.hub.now()
	current := start
	e.hub.NowFunc = func() time.Time { return current }
	e.hub.specGrantRequests["list-expiry-during-signing"] = &specGrantRequest{
		ID: "list-expiry-during-signing", OwnerKey: AgentKeyID("zzc"), SessionName: ag.NameLC(), AgentID: ag.ID,
		PrincipalID: "zzc", Action: "create", PageID: ConsoleAgentCapabilitySpecsNew,
		State: "pending", ExpiresAt: start.Add(time.Second),
	}
	oldRepo := e.hub.Repo
	repo := &blockingSpecGrantRepo{Repository: oldRepo, entered: make(chan struct{}), release: make(chan struct{})}
	e.hub.newPlatformShimRepo = e.db
	e.hub.Repo = repo
	t.Cleanup(func() { e.hub.Repo, e.hub.newPlatformShimRepo = oldRepo, nil })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/agents/{sessionName}/spec-grant-requests", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantRequests))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantDecision))
	oldServer := e.srv
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
	result := make(chan int, 1)
	go func() {
		status, _ := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/list-expiry-during-signing/approve", e.tokZZC, "", "", "")
		result <- status
	}()
	select {
	case <-repo.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("approval did not reach signing boundary")
	}
	current = start.Add(2 * time.Second)
	if status, body := e.do(http.MethodGet, "/api/console/agents/Kimi.zzc/spec-grant-requests", e.tokZZC, "", "", ""); status != http.StatusOK || !strings.Contains(body, `"status":"expired"`) {
		t.Fatalf("owner list did not expire in-flight grant: status=%d body=%s", status, body)
	}
	close(repo.release)
	if status := <-result; status != http.StatusNotFound {
		t.Fatalf("expired signing completion status=%d, want 404", status)
	}
	g := e.hub.specGrantRequests["list-expiry-during-signing"]
	if g.State != "expired" || g.capability != "" {
		t.Fatalf("in-flight expired grant=%+v", g)
	}
	if calls := repo.revocations(); calls != 1 {
		t.Fatalf("persisted capability revoke calls=%d, want 1", calls)
	}
}

func TestSpecGrantConcurrentApproveSignsOnlyOnce(t *testing.T) {
	e := newShimEnv(t)
	if err := e.hub.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	ag, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	e.hub.specGrantRequests["concurrent"] = &specGrantRequest{
		ID: "concurrent", OwnerKey: AgentKeyID("zzc"), SessionName: ag.NameLC(), AgentID: ag.ID,
		PrincipalID: "zzc", Action: "create", PageID: ConsoleAgentCapabilitySpecsNew,
		State: "pending", ExpiresAt: e.hub.now().Add(specGrantRequestTTL),
	}
	oldRepo := e.hub.Repo
	repo := &blockingSpecGrantRepo{Repository: oldRepo, entered: make(chan struct{}), release: make(chan struct{})}
	// The wrapper blocks only capability signing; keep the real shim repository
	// available for the fresh-owner projection that precedes it.
	e.hub.newPlatformShimRepo = e.db
	e.hub.Repo = repo
	t.Cleanup(func() { e.hub.Repo, e.hub.newPlatformShimRepo = oldRepo, nil })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleSpecGrantDecision))
	oldServer := e.srv
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() { e.srv.Close(); e.srv = oldServer })
	type response struct {
		status int
		body   string
	}
	first := make(chan response, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/console/agents/Kimi.zzc/spec-grant-requests/concurrent/approve", nil)
		req.Header.Set("Origin", npOrigin)
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			first <- response{status: -1, body: err.Error()}
			return
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		first <- response{status: res.StatusCode, body: string(body)}
	}()
	select {
	case <-repo.entered:
	case result := <-first:
		t.Fatalf("first approval ended before signing boundary: status=%d body=%s", result.status, result.body)
	case <-time.After(5 * time.Second):
		t.Fatal("first approval never reached signing boundary")
	}
	if status, body := e.do(http.MethodPost, "/api/console/agents/Kimi.zzc/spec-grant-requests/concurrent/approve", e.tokZZC, "", "", ""); status != http.StatusNotFound {
		t.Fatalf("second concurrent approval status=%d body=%s, want 404", status, body)
	}
	close(repo.release)
	if result := <-first; result.status != http.StatusNoContent {
		t.Fatalf("first approval status=%d body=%s, want 204", result.status, result.body)
	}
	g := e.hub.specGrantRequests["concurrent"]
	if g.State != "granted" || g.CapabilityID == "" || g.capability == "" {
		t.Fatalf("first approval did not produce the sole grant: state=%q capID=%q", g.State, g.CapabilityID)
	}
}
