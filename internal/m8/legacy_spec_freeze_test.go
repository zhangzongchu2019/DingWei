package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestR11FrozenHTTPWritesAndUnchangedReads(t *testing.T) {
	h := newConsoleSpecTestHub(t)
	rr := callConsoleSpec(t, h, "owner-a", consolePlatformActionSubmit, "POST", "/api/console/specs", validSpecBody, h.HandleConsoleSpecs)
	if rr.Code != 201 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var spec model.ConsoleSpec
	if e := json.Unmarshal(rr.Body.Bytes(), &spec); e != nil {
		t.Fatal(e)
	}
	db := h.Repo.(*store.SQLite)
	read := func() string {
		req := httptest.NewRequest("GET", "/api/specs/shared/"+spec.ID, nil)
		req.SetPathValue("specID", spec.ID)
		rec := httptest.NewRecorder()
		h.HandleConsoleSpec(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	for _, pageHandler := range []http.HandlerFunc{h.HandleConsoleSpecsPage, h.HandleSharedConsoleSpecPage} {
		rec := httptest.NewRecorder()
		pageHandler(rec, httptest.NewRequest("GET", "/console/specs", nil))
		if strings.Contains(rec.Body.String(), "id=legacy-spec-frozen") {
			t.Fatal("default-off page marked frozen")
		}
	}
	before := read()
	if e := db.EnableLegacySpecFreeze(context.Background()); e != nil {
		t.Fatal(e)
	}
	// Each actual handler is exercised, including callbacks and both template surfaces.
	handlers := map[string]http.HandlerFunc{
		"create": h.HandleConsoleSpecs, "patch_shared_agent": h.HandleConsoleSpec,
		"versions": h.HandleConsoleSpecNewVersion, "derive": h.HandleConsoleSpecDerive,
		"close": h.HandleConsoleSpecCloseEditing, "reopen": h.HandleConsoleSpecReopenEditing,
		"archive_delete": h.HandleConsoleSpecArchive, "batch_archive": h.HandleConsoleSpecsArchiveBatch,
		"unarchive": h.HandleConsoleSpecUnarchive, "workflow": h.HandleConsoleSpecWorkflow,
		"new_tasks": h.HandleConsoleSpecTasks, "presplit": h.HandleConsoleSpecPresplit,
		"confirm_candidates":    h.HandleConsoleTaskCandidateConfirm,
		"agent_review_criteria": h.HandleConsoleSpecAgentRequest, "human_signoff": h.HandleConsoleSpecAgentConfirm,
		"agent_result": h.HandleAgentSpecReview, "inference": h.HandleConsoleSpecInferenceAssist,
		"inference_result": h.HandleConsoleSpecAgentInference,
		"pm_draft":         h.HandlePMSpecWorkingDraft, "pm_freeze": h.HandlePMSpecFlowFreeze,
		"pm_action": h.HandlePMSpecAssistAction, "pm_result": h.HandleAgentPMSpecAssistResult,
		"template_source": h.HandleTemplateSource, "template": h.HandleTemplateDraft,
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/console/specs/"+spec.ID, strings.NewReader(validSpecBody))
			req.SetPathValue("specID", spec.ID)
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"error":"legacy_spec_frozen"`) || !strings.Contains(rec.Body.String(), `"outcome":"rejected"`) {
				t.Fatalf("not explicit frozen refusal: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
	for _, pageHandler := range []http.HandlerFunc{h.HandleConsoleSpecsPage, h.HandleSharedConsoleSpecPage} {
		rec := httptest.NewRecorder()
		pageHandler(rec, httptest.NewRequest("GET", "/console/specs", nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "id=legacy-spec-frozen") {
			t.Fatal("LCS-S008 history marker missing")
		}
	}
	if after := read(); after != before {
		t.Fatal("LCS-S008 shared GET authorization/content changed")
	}
	for _, typ := range []string{"console_spec_review_result", "console_spec_criteria_result", "console_spec_pm_assist_result"} {
		if !h.handleSpecAgentReviewResult(context.Background(), nil, model.Envelope{Meta: map[string]any{"type": typ}}) {
			t.Fatal("late result not consumed")
		}
	}
	if !h.handleConsoleSpecPresplitResult(context.Background(), nil, model.Envelope{Meta: map[string]any{"type": specPresplitResultType}}) {
		t.Fatal("late presplit not consumed")
	}
	h.reconcileSpecAgent(context.Background())
}

func TestR11FrozenWriteStillRequiresIdentity(t *testing.T) {
	e := newShimEnv(t)
	if err := e.db.EnableLegacySpecFreeze(e.ctx); err != nil {
		t.Fatal(err)
	}
	status, _ := e.do("POST", "/api/console/specs", "", "", "", validSpecBody)
	if status != 401 && status != 403 {
		t.Fatal("anonymous identity check bypassed", status)
	}
	status, body := e.do("POST", "/api/console/specs", e.tokZZC, "", "", validSpecBody)
	if status != 409 || !strings.Contains(body, "legacy_spec_frozen") {
		t.Fatal(status, body)
	}
}

func TestR11FrozenDispatchAndPersistedQueue(t *testing.T) {
	h := newConsoleSpecTestHub(t)
	db := h.Repo.(*store.SQLite)
	ctx := context.Background()
	raw, _ := json.Marshal(model.Envelope{ID: "generation", Body: "private-spec", Meta: map[string]any{"type": "delivery", "spec_review_type": "console_spec_review_request"}})
	now := time.Now().UTC()
	if e := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "queued-generation", KeyID: "key", SessionName: "reviewer", SourceMessageID: "generation", Envelope: string(raw), CreatedAt: now}); e != nil {
		t.Fatal(e)
	}
	if e := db.EnableLegacySpecFreeze(ctx); e != nil {
		t.Fatal(e)
	}
	for _, typ := range []string{"console_spec_review_request", "console_spec_criteria_request", "console_spec_pm_assist_request", specPresplitRequestType, specInferenceAssistRequestType} {
		if e := h.RouteEnvelope(ctx, model.Envelope{Meta: map[string]any{"type": typ}}); !errors.Is(e, store.ErrLegacySpecFrozen) {
			t.Fatalf("generation escaped cutover %s: %v", typ, e)
		}
	}
	// A nil transport would panic on socket write: a dead queued generation must
	// instead be consumed without attempting delivery, even after reconnect.
	c := &sessionClient{keyID: "key", sessionName: "reviewer", deliveryACKV1: true, specReviewV1: true}
	h.drainSessionDeliveries(ctx, c)
	d, e := db.GetSessionDelivery(ctx, "queued-generation")
	if e != nil || d == nil || d.State != "dead" {
		t.Fatal("queued generation still deliverable", d, e)
	}
	// Ordinary messages do not acquire the old-spec gate; normal validation runs.
	if e := h.RouteEnvelope(ctx, model.Envelope{}); errors.Is(e, store.ErrLegacySpecFrozen) {
		t.Fatal("ordinary message frozen")
	}
}
