package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func newConsolePlatformTestHub(t *testing.T) *Hub {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "console-platform.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.ConsolePrincipal{
		{ID: "manager", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "manager-openid", DisplayName: "manager", Active: true},
		{ID: "owner", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "owner-openid", DisplayName: "owner", Active: true},
	} {
		if err := db.UpsertConsolePrincipal(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertConsolePlatformRole(context.Background(), model.ConsolePlatformRole{PrincipalID: "manager", Role: "management", Active: true}); err != nil {
		t.Fatal(err)
	}
	return New(db, ConsoleAuthMode{enabled: true})
}

func TestConsolePlatformRoleMatrixIsEnforcedByServer(t *testing.T) {
	hub := newConsolePlatformTestHub(t)
	actions := []string{
		consolePlatformActionRead,
		consolePlatformActionSubmit,
		consolePlatformActionAdd,
		consolePlatformActionEdit,
		consolePlatformActionDelete,
		consolePlatformActionApprove,
	}
	for _, principalID := range []string{"manager", "owner"} {
		for _, action := range actions {
			t.Run(principalID+"/"+action, func(t *testing.T) {
				called := false
				handler := hub.RequireConsolePlatformAction(action, func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				})
				req := httptest.NewRequest(http.MethodPost, "/api/console/platform", nil)
				req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principalID))
				rr := httptest.NewRecorder()
				handler(rr, req)

				wantAllowed := principalID == "manager" || action == consolePlatformActionRead || action == consolePlatformActionSubmit
				if wantAllowed && (rr.Code != http.StatusNoContent || !called) {
					t.Fatalf("allowed action rejected code=%d called=%v", rr.Code, called)
				}
				if !wantAllowed && (rr.Code != http.StatusForbidden || called) {
					t.Fatalf("owner management action escaped server guard code=%d called=%v", rr.Code, called)
				}
			})
		}
	}
}

func TestConsolePlatformUnknownPrincipalIsForbidden(t *testing.T) {
	hub := newConsolePlatformTestHub(t)
	handler := hub.RequireConsolePlatformAction(consolePlatformActionRead, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unknown principal reached platform handler")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/console/platform", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "unknown"))
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%q", rr.Code, rr.Body.String())
	}
}

func callConsolePlatform(t *testing.T, hub *Hub, principalID, action, method, path string, body []byte, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principalID))
	rr := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(action, handler)(rr, req)
	return rr
}

func TestConsolePlatformHandlersMountAgentsAndEnforceWorkflow(t *testing.T) {
	hub := newConsolePlatformTestHub(t)

	// Existing Agent workspace is a real mounted item, not an empty extension point.
	rr := callConsolePlatform(t, hub, "owner", consolePlatformActionRead, http.MethodGet,
		"/api/console/platform", nil, hub.HandleConsolePlatform)
	if rr.Code != http.StatusOK {
		t.Fatalf("owner list code=%d body=%q", rr.Code, rr.Body.String())
	}
	var listed struct {
		Role    string                      `json:"role"`
		Actions []string                    `json:"actions"`
		Items   []model.ConsolePlatformItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Role != "owner" || len(listed.Items) != 2 || listed.Items[0].ID != "agents" || listed.Items[0].Href != "#agents" || listed.Items[1].ID != "specs" {
		t.Fatalf("owner platform=%+v", listed)
	}

	payload := []byte(`{"id":"custom-specs","kind":"spec","title":"Custom Specs","href":"/console/custom-specs"}`)
	// A normal owner can submit for review...
	rr = callConsolePlatform(t, hub, "owner", consolePlatformActionSubmit, http.MethodPost,
		"/api/console/platform/submissions", payload, hub.HandleConsolePlatformSubmit)
	if rr.Code != http.StatusCreated || !bytes.Contains(rr.Body.Bytes(), []byte(`"status":"pending"`)) {
		t.Fatalf("owner submit code=%d body=%q", rr.Code, rr.Body.String())
	}
	// ...but the same identity, through the same HTTP channel, cannot add directly.
	rr = callConsolePlatform(t, hub, "owner", consolePlatformActionAdd, http.MethodPost,
		"/api/console/platform/items", []byte(`{"id":"direct","kind":"spec","title":"Direct","href":"/console/direct"}`), hub.HandleConsolePlatformAdd)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("owner direct management action code=%d body=%q", rr.Code, rr.Body.String())
	}
	direct, err := hub.Repo.(*store.SQLite).ConsolePlatformItem(context.Background(), "direct")
	if err != nil {
		t.Fatal(err)
	}
	if direct != nil {
		t.Fatalf("owner received 403 but management side effect still happened: %+v", direct)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/console/platform/items/custom-specs/approve", nil)
	approveReq.SetPathValue("itemID", "custom-specs")
	approveReq = approveReq.WithContext(context.WithValue(approveReq.Context(), consolePrincipalContextKey{}, "manager"))
	approveRR := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionApprove, hub.HandleConsolePlatformApprove)(approveRR, approveReq)
	if approveRR.Code != http.StatusNoContent {
		t.Fatalf("manager approve code=%d body=%q", approveRR.Code, approveRR.Body.String())
	}

	rr = callConsolePlatform(t, hub, "owner", consolePlatformActionRead, http.MethodGet,
		"/api/console/platform", nil, hub.HandleConsolePlatform)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"id":"custom-specs"`)) {
		t.Fatalf("owner approved list code=%d body=%q", rr.Code, rr.Body.String())
	}
}
