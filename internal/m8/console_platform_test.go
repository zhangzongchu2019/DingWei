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
	h := New(db, ConsoleAuthMode{enabled: true})
	t.Cleanup(h.Close)
	return h
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

func TestConsolePlatformHandlersExposeOnlyActionableBuiltinsAndEnforceWorkflow(t *testing.T) {
	hub := newConsolePlatformTestHub(t)

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
	// 🔴 S-GROUP-ENTRY 改口径:owner 的平台项由 2 项变 **3** 项 ——
	//   agent-groups 已移出 `if role == consolePlatformRoleManagement` 块
	//   (派工 runbooks/DISPATCH-20260929-S-GROUP-ENTRY.md;
	//    背景 runbooks/DEFECT-group-entry-is-management-only-20260929.md)。
	//   🔵 **admin 仍不得下发给 owner**,那条判据由
	//   console_platform_group_entry_owner_test.go 的子格 ② 专守,本处不重复钉。
	// S-R62-CONSOLE-SLIM：owner 菜单恰为一项 agent-groups（specs/task-board 已摘）。
	if listed.Role != "owner" || len(listed.Items) != 1 || listed.Items[0].ID != "agent-groups" {
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
	// S-R62-OLDPLAT-SLIM 白名单口径（组长 2026-11-19T00:20:38Z 裁定）：已批准自定义项
	// 不再随菜单下发；批准工作流本身不动（上面 204＋下面落库状态），菜单恒只 builtin。
	if rr.Code != http.StatusOK || bytes.Contains(rr.Body.Bytes(), []byte(`"id":"custom-specs"`)) {
		t.Fatalf("owner 菜单不得下发自定义项（白名单口径）code=%d body=%q", rr.Code, rr.Body.String())
	}
	approved, err := hub.Repo.(*store.SQLite).ConsolePlatformItem(context.Background(), "custom-specs")
	if err != nil || approved == nil || approved.Status != "approved" {
		t.Fatalf("批准工作流须照旧落库 approved: %+v err=%v", approved, err)
	}
}
