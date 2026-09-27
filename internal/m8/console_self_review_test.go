package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func callSpecWorkflow(t *testing.T, hub *Hub, specID, actor, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+specID+"/workflow/"+action, bytes.NewBufferString(body))
	req.SetPathValue("specID", specID)
	req.SetPathValue("action", action)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, actor))
	rr := httptest.NewRecorder()
	hub.HandleConsoleSpecWorkflow(rr, req)
	return rr
}

func TestConsoleSpecSingleOwnerSubmitsReviewWithoutSelfTransferAndStillPassesAllFinalizeGates(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	created := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", generationGateBody(`[{
"id":"A1","text":"owner:q must hold","owner_ref":"owner:q","source":"owner","checked_by":"owner-a","checked_at":"2026-09-27T16:00:00Z","cases":["case-a"]}]`), hub.HandleConsoleSpecs)
	if created.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", created.Code, created.Body.String())
	}
	var spec model.ConsoleSpec
	if err := json.Unmarshal(created.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	self := callSpecWorkflow(t, hub, spec.ID, "owner-a", "transfer", `{"target_short_name":"甲","state":"review"}`)
	if self.Code != http.StatusConflict || !strings.Contains(self.Body.String(), "无需转给自己") {
		t.Fatalf("self transfer code=%d body=%s", self.Code, self.Body.String())
	}
	submit := callSpecWorkflow(t, hub, spec.ID, "owner-a", "submit-review", ``)
	if submit.Code != http.StatusNoContent {
		t.Fatalf("submit review code=%d body=%s", submit.Code, submit.Body.String())
	}
	db := hub.Repo.(*store.SQLite)
	inReview, err := db.ConsoleSpec(context.Background(), "owner-a", spec.ID, false)
	if err != nil || inReview.State != "review" || inReview.CurrentEditorID != "owner-a" {
		t.Fatalf("submit review did not preserve single owner: spec=%+v err=%v", inReview, err)
	}
	completeSpecGateForTest(t, db, spec.ID, "owner-a", hub.now())
	if err := db.FinalizeConsoleSpec(context.Background(), spec.ID, "owner-a", hub.now()); err != nil {
		t.Fatalf("single-owner finalize with all existing gates satisfied: %v", err)
	}
	final, err := db.ConsoleSpec(context.Background(), "owner-a", spec.ID, false)
	if err != nil || final.State != "final" {
		t.Fatalf("single-owner flow final=%+v err=%v", final, err)
	}
}

func TestConsoleSpecPageExposesSubmitReviewOnlyForEditingHolder(t *testing.T) {
	for _, needle := range []string{`id=submit-spec-review`, `workflowAction('submit-review')`, `data.spec.state==='editing'`} {
		if !strings.Contains(consoleSpecsPageHTML, needle) {
			t.Fatalf("submit review UI disconnected: missing %s", needle)
		}
	}
}
