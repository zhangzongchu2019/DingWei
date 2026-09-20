package m8

import (
	"context"
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
		{ID: "manager", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "manager-openid", Active: true},
		{ID: "owner", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "owner-openid", Active: true},
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
