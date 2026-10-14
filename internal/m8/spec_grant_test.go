package m8

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

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
