package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestSpecArchiveDefaultListBoardSharedReadAndRestore(t *testing.T) {
	h := newConsoleSpecTestHub(t)
	spec := boardOwnedFixtureSpec(t, h, "owner-a", "archive final", true)
	db := h.Repo.(*store.SQLite)
	archiveCall := func(principal, method string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/console/specs/"+spec.ID, nil)
		r.SetPathValue("specID", spec.ID)
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, principal))
		out := httptest.NewRecorder()
		h.RequireConsolePlatformAction(consolePlatformActionRead, handler)(out, r)
		return out
	}
	archived := archiveCall("owner-a", "DELETE", h.HandleConsoleSpecArchive)
	if archived.Code != 204 {
		t.Fatalf("owner archive=%d %s", archived.Code, archived.Body.String())
	}
	list := callConsoleSpec(t, h, "owner-a", consolePlatformActionRead, "GET", "/api/console/specs", "", h.HandleConsoleSpecs)
	if list.Code != 200 || strings.Contains(list.Body.String(), spec.ID) {
		t.Fatal("default list leaks archive")
	}
	list = callConsoleSpec(t, h, "owner-a", consolePlatformActionRead, "GET", "/api/console/specs?include_archived=1", "", h.HandleConsoleSpecs)
	if list.Code != 200 || !strings.Contains(list.Body.String(), spec.ID) {
		t.Fatal("show archive failed")
	}
	if status, p := boardOwnedRequest(t, h, "owner-a", ""); status != 200 || len(p.Specs) != 0 || len(p.Tasks) != 0 {
		t.Fatal("task board leaks archived spec/tasks")
	}
	if status, _ := boardOwnedRequest(t, h, "owner-a", "spec_id="+spec.ID); status != 404 {
		t.Fatal("explicit task board selects archive")
	}
	r := httptest.NewRequest("GET", "/api/specs/shared/"+spec.ID, nil)
	r.SetPathValue("specID", spec.ID)
	out := httptest.NewRecorder()
	h.HandleConsoleSpec(out, r)
	if out.Code != 200 {
		t.Fatal("archive destroyed public historical read")
	}
	var detail struct {
		Spec model.ConsoleSpec `json:"spec"`
	}
	_ = json.Unmarshal(out.Body.Bytes(), &detail)
	if !detail.Spec.Archived || detail.Spec.CanEdit || detail.Spec.CanSplit {
		t.Fatal("archived shared page can mutate")
	}
	denied := archiveCall("owner-b", "POST", h.HandleConsoleSpecUnarchive)
	if denied.Code != 403 {
		t.Fatal("other owner restored archive")
	}
	restored := archiveCall("owner-a", "POST", h.HandleConsoleSpecUnarchive)
	if restored.Code != 204 {
		t.Fatalf("restore=%d", restored.Code)
	}
	if status, p := boardOwnedRequest(t, h, "owner-a", ""); status != 200 || len(p.Specs) != 1 || len(p.Tasks) != 1 {
		t.Fatal("restore did not restore board visibility")
	}
	// Invalid batch leaves the existing item readable and unarchived.
	batch := callConsoleSpec(t, h, "owner-a", consolePlatformActionRead, "POST", "/api/console/specs/archive", `{"spec_ids":["`+spec.ID+`","missing"]}`, h.HandleConsoleSpecsArchiveBatch)
	if batch.Code != 404 {
		t.Fatalf("invalid batch=%d", batch.Code)
	}
	current, err := db.ConsoleSpec(context.Background(), "owner-a", spec.ID, false)
	if err != nil || current.Archived {
		t.Fatal("invalid batch partially committed")
	}
}
