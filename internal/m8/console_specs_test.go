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

func newConsoleSpecTestHub(t *testing.T) *Hub {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "console-spec-http.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.ConsolePrincipal{
		{ID: "owner-a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "oa", Active: true},
		{ID: "owner-b", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ob", Active: true},
		{ID: "manager", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "om", Active: true},
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

func callConsoleSpec(t *testing.T, hub *Hub, principal, action, method, path string, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principal))
	rr := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(action, handler)(rr, req)
	return rr
}

const validSpecBody = `{"title":"Spec A","content_markdown":"# A","acceptance":[{"id":"A1","text":"works"}],"input_anchors":[{"kind":"owner_message","ref":"msg:1"}]}`

func TestConsoleSpecHTTPMultipleSpecsAreVisiblePubliclyButWritesRemainControlled(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	ids := make([]string, 0, 2)
	for _, body := range []string{validSpecBody, stringsReplace(validSpecBody, "Spec A", "Spec B")} {
		rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", body, hub.HandleConsoleSpecs)
		if rr.Code != http.StatusCreated {
			t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
		}
		var got model.ConsoleSpec
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, got.ID)
	}
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionRead, http.MethodGet, "/api/console/specs", "", hub.HandleConsoleSpecs)
	if rr.Code != http.StatusOK {
		t.Fatalf("list code=%d body=%s", rr.Code, rr.Body.String())
	}
	var listed struct {
		Specs []model.ConsoleSpec `json:"specs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Specs) != 2 {
		t.Fatalf("page/API must expose both specs got=%+v", listed.Specs)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/console/specs/"+ids[0], nil)
	req.SetPathValue("specID", ids[0])
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-b"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionRead, hub.HandleConsoleSpec)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("控写不控读:cross-owner GET code=%d body=%s", rr.Code, rr.Body.String())
	}
	var public struct {
		Spec     model.ConsoleSpec         `json:"spec"`
		Revision model.ConsoleSpecRevision `json:"revision"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &public); err != nil || public.Spec.ID != ids[0] || public.Revision.ContentMarkdown != "# A" {
		t.Fatalf("public GET payload=%s err=%v", rr.Body.String(), err)
	}

	// 阳性只证明公开读；写仍须由所有者或后续能力链接授权。不能把“读放开”误写成“全放开”。
	patch := `{"title":"Spec A","content_markdown":"# stolen","acceptance":[{"id":"A1","text":"works"}],"input_anchors":[{"kind":"owner_message","ref":"msg:1"}],"expected_revision":1}`
	req = httptest.NewRequest(http.MethodPatch, "/api/console/specs/"+ids[0], bytes.NewBufferString(patch))
	req.SetPathValue("specID", ids[0])
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-b"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpec)(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-owner PATCH must remain controlled code=%d body=%s", rr.Code, rr.Body.String())
	}
	stored, err := hub.Repo.(*store.SQLite).ConsoleSpecRevision(context.Background(), "owner-a", ids[0], 1, false)
	if err != nil || stored == nil || stored.ContentMarkdown != "# A" {
		t.Fatalf("rejected PATCH changed state revision=%+v err=%v", stored, err)
	}
}

func TestConsoleSpecInputAnchorsAndManagementSideEffectsAreServerEnforced(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	bad := `{"title":"Bad","content_markdown":"x","acceptance":[{"id":"A1","text":"works"}],"input_anchors":[{"kind":"upstream_spec","ref":"spec:x"}]}`
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", bad, hub.HandleConsoleSpecs)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("upstream anchor without digest code=%d", rr.Code)
	}
	rr = callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	var spec model.ConsoleSpec
	_ = json.Unmarshal(rr.Body.Bytes(), &spec)

	// Keep the HTTP response forbidden while deliberately attempting the real
	// management action: the permanent assertion below checks state, not prose.
	reviewPath := "/api/console/specs/" + spec.ID + "/reviews"
	req := httptest.NewRequest(http.MethodPost, reviewPath, bytes.NewBufferString(`{"stage":"hard_gate","verdict":"pass"}`))
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionApprove, hub.HandleConsoleSpecReview)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("owner review code=%d", rr.Code)
	}
	stored, err := hub.Repo.(*store.SQLite).ConsoleSpec(context.Background(), "owner-a", spec.ID, false)
	if err != nil || stored == nil || stored.State != "draft" {
		t.Fatalf("403 performed side effect spec=%+v err=%v", stored, err)
	}
}

func TestConsoleSpecHTTPReviewProducesVersionedTaskProposalWithoutKFields(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	var spec model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/submit", nil)
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpecSubmit)(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("submit code=%d body=%s", rr.Code, rr.Body.String())
	}
	for _, body := range []string{`{"revision":1,"stage":"hard_gate","verdict":"pass","reason_codes":[]}`, `{"revision":1,"stage":"human_review","verdict":"pass","reason_codes":[]}`} {
		req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/reviews", bytes.NewBufferString(body))
		req.SetPathValue("specID", spec.ID)
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
		rr = httptest.NewRecorder()
		hub.RequireConsolePlatformAction(consolePlatformActionApprove, hub.HandleConsoleSpecReview)(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("review code=%d body=%s", rr.Code, rr.Body.String())
		}
	}
	rr = callConsoleSpec(t, hub, "manager", consolePlatformActionApprove, http.MethodGet, "/api/console/task-proposals/ready", "", hub.HandleConsoleTaskProposalsReady)
	if rr.Code != http.StatusOK {
		t.Fatalf("ready code=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Proposals []struct {
			SchemaVersion int    `json:"schema_version"`
			ProposalID    string `json:"proposal_id"`
			Source        struct {
				SpecID        string `json:"spec_id"`
				Revision      int    `json:"revision"`
				ContentSHA256 string `json:"content_sha256"`
			} `json:"source"`
			Acceptance   []specAcceptance  `json:"acceptance"`
			InputAnchors []specInputAnchor `json:"input_anchors"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Proposals) != 1 || payload.Proposals[0].SchemaVersion != 1 || payload.Proposals[0].Source.SpecID != spec.ID || payload.Proposals[0].Source.Revision != 1 || len(payload.Proposals[0].Acceptance) != 1 || len(payload.Proposals[0].InputAnchors) != 1 {
		t.Fatalf("proposal contract=%s", rr.Body.String())
	}
	text := rr.Body.String()
	for _, forbidden := range []string{`"assignee"`, `"reviewer"`, `"priority"`} {
		if bytes.Contains([]byte(text), []byte(forbidden)) {
			t.Fatalf("I leaked K-owned field %s in %s", forbidden, text)
		}
	}
}

func stringsReplace(value, old, new string) string {
	return bytes.NewBuffer(bytes.ReplaceAll([]byte(value), []byte(old), []byte(new))).String()
}
