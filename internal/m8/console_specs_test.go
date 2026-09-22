package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	for _, principal := range []string{"owner-a", "owner-b"} {
		if err := db.UpsertConsolePrincipalRole(context.Background(), model.ConsolePrincipalRole{PrincipalID: principal, Role: "owner", OwnerKey: "tenant-a", Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertConsolePlatformRole(context.Background(), model.ConsolePlatformRole{PrincipalID: "manager", Role: "management", Active: true}); err != nil {
		t.Fatal(err)
	}
	return New(db, ConsoleAuthMode{enabled: true})
}

type recordingSpecPermanentClient struct {
	requests []FilePermanentRequest
	url      string
}

func (c *recordingSpecPermanentClient) PersistSpecFile(_ context.Context, req FilePermanentRequest) (string, error) {
	c.requests = append(c.requests, req)
	return c.url, nil
}

func callConsoleSpec(t *testing.T, hub *Hub, principal, action, method, path string, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principal))
	rr := httptest.NewRecorder()
	hub.RequireConsolePlatformAction(action, handler)(rr, req)
	return rr
}

func TestConsoleSpecTaskSplitEndpointCreatesVisibleTaskAndLocksRevision(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var created model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+created.ID+"/tasks", strings.NewReader(`{"title":"Implement","content":"Do it","estimated_hours":4,"depends_on":[]}`))
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpecTasks)(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("split code=%d body=%s", rr.Code, rr.Body.String())
	}
	tasks, err := hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), created.ID)
	if err != nil || len(tasks) != 1 || tasks[0].Title != "Implement" || tasks[0].EstimatedHours != 4 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
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

func TestConsoleSpecManagementOwnerCanEditOwnSpecButNotAnotherOwners(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	createdBy := func(principal string) model.ConsoleSpec {
		rr := callConsoleSpec(t, hub, principal, consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
		if rr.Code != http.StatusCreated {
			t.Fatalf("create principal=%s code=%d body=%s", principal, rr.Code, rr.Body.String())
		}
		var spec model.ConsoleSpec
		if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
			t.Fatal(err)
		}
		return spec
	}
	owned := createdBy("manager")
	foreign := createdBy("owner-a")
	patch := `{"title":"Spec A","content_markdown":"# edited","acceptance":[{"id":"A1","text":"works"}],"input_anchors":[{"kind":"owner_message","ref":"msg:1"}],"expected_revision":1}`

	for _, tc := range []struct {
		name string
		id   string
		want int
	}{
		{name: "management owner may edit", id: owned.ID, want: http.StatusOK},
		{name: "management role does not grant another owners write", id: foreign.ID, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/console/specs/"+tc.id, bytes.NewBufferString(patch))
			req.SetPathValue("specID", tc.id)
			req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "manager"))
			rr := httptest.NewRecorder()
			hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpec)(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("PATCH code=%d want=%d body=%s", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
	stored, err := hub.Repo.(*store.SQLite).ConsoleSpecRevision(context.Background(), "owner-a", foreign.ID, 1, false)
	if err != nil || stored == nil || stored.ContentMarkdown != "# A" {
		t.Fatalf("rejected management cross-owner PATCH changed state revision=%+v err=%v", stored, err)
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

func TestConsoleSpecSharedCapabilityWritesUntilOwnerClosesEditing(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID      string `json:"id"`
		EditURL string `json:"edit_url"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(strings.SplitN(created.EditURL, "#edit=", 2)[1], " ")
	patchBody := `{"title":"Spec A","content_markdown":"# shared","acceptance":[{"id":"A1","text":"works"}],"input_anchors":[{"kind":"owner_message","ref":"msg:1"}],"expected_revision":1}`
	req := httptest.NewRequest(http.MethodPatch, "/api/specs/shared/"+created.ID, bytes.NewBufferString(patchBody))
	req.SetPathValue("specID", created.ID)
	req.Header.Set("X-DW-Spec-Edit-Capability", token)
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpec(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("shared PATCH code=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+created.ID+"/close-editing", nil)
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpecCloseEditing)(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("close editing code=%d body=%s", rr.Code, rr.Body.String())
	}

	patchBody = strings.Replace(patchBody, `"expected_revision":1`, `"expected_revision":2`, 1)
	req = httptest.NewRequest(http.MethodPatch, "/api/specs/shared/"+created.ID, bytes.NewBufferString(patchBody))
	req.SetPathValue("specID", created.ID)
	req.Header.Set("X-DW-Spec-Edit-Capability", token)
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpec(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "editing closed") {
		t.Fatalf("closed shared PATCH code=%d body=%s", rr.Code, rr.Body.String())
	}

	// Closing is one reversible owner-controlled brake. It must stop the owner
	// write path too; otherwise UI and storage disagree about the same state.
	req = httptest.NewRequest(http.MethodPatch, "/api/console/specs/"+created.ID, bytes.NewBufferString(patchBody))
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpec)(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "editing closed") {
		t.Fatalf("closed owner PATCH code=%d body=%s", rr.Code, rr.Body.String())
	}
	closed, err := hub.Repo.(*store.SQLite).ConsoleSpec(context.Background(), "owner-a", created.ID, false)
	if err != nil || closed == nil || closed.CurrentRevision != 2 || closed.State != "needs_improvement" {
		t.Fatalf("closed rejected write changed state spec=%+v err=%v", closed, err)
	}

	for _, principal := range []string{"owner-b", ""} {
		req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+created.ID+"/reopen-editing", nil)
		req.SetPathValue("specID", created.ID)
		req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, principal))
		rr = httptest.NewRecorder()
		hub.HandleConsoleSpecReopenEditing(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("non-owner %q reopened editing code=%d body=%s", principal, rr.Code, rr.Body.String())
		}
	}
	req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+created.ID+"/reopen-editing", nil)
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpecReopenEditing)(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("owner reopen code=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, "/api/console/specs/"+created.ID, bytes.NewBufferString(patchBody))
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpec)(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"revision":3`) {
		t.Fatalf("reopened owner PATCH code=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/specs/shared/"+created.ID, nil)
	req.SetPathValue("specID", created.ID)
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpec(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"current_revision":3`) {
		t.Fatalf("closing writes must not close public read code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestConsoleSpecSavePersistsAndRewritesUploadedImageURL(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	client := &recordingSpecPermanentClient{url: "https://files.example/f/spec-assets/global/00/0000/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"}
	hub.SetFilePermanentClient(client)
	temporary := "https://files.example/f/tenant/agent/26/0920/0123456789abcdef0123456789abcdef.png"
	body := strings.Replace(validSpecBody, `# A`, `# A\n![diagram](`+temporary+`)`, 1)
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", body, hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var created model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	revision, err := hub.Repo.(*store.SQLite).ConsoleSpecRevision(context.Background(), "owner-b", created.ID, 1, false)
	if err != nil || revision == nil || strings.Contains(revision.ContentMarkdown, temporary) || !strings.Contains(revision.ContentMarkdown, client.url) {
		t.Fatalf("stored markdown=%q err=%v", revision.ContentMarkdown, err)
	}
	if len(client.requests) != 1 || client.requests[0].FileID != "0123456789abcdef0123456789abcdef" || client.requests[0].OwnerKey != "tenant-a" {
		t.Fatalf("permanent requests=%+v", client.requests)
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
	newBody := `{"title":"Spec A","content_markdown":"# version two","acceptance":[{"id":"A1","text":"works"}],"input_anchors":[{"kind":"owner_message","ref":"msg:1"}],"expected_revision":1}`
	req = httptest.NewRequest(http.MethodPatch, "/api/console/specs/"+spec.ID, bytes.NewBufferString(newBody))
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpec)(rr, req)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "revision locked") {
		t.Fatalf("locked revision PATCH must be observably rejected code=%d body=%s", rr.Code, rr.Body.String())
	}
	// Reopening editing is not a task/proposal unlock. The same locked revision
	// remains immutable; only the explicit new-version path below may advance it.
	req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/close-editing", nil)
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpecCloseEditing(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("close locked spec code=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/reopen-editing", nil)
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpecReopenEditing(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("reopen locked spec code=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, "/api/console/specs/"+spec.ID, bytes.NewBufferString(newBody))
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpec)(rr, req)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "revision locked") {
		t.Fatalf("reopen must not unlock proposed revision code=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/versions", bytes.NewBufferString(newBody))
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpecNewVersion)(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"revision":2`) {
		t.Fatalf("explicit new version code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestConsoleSpecsListIsMineOnlyAndPaginatesTwenty(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("spec-mine-%02d", i)
		rev, err := revisionFromRequest(id, "owner-a", 1, consoleSpecWriteRequest{Title: id, ContentMarkdown: "# " + id, Acceptance: []specAcceptance{{ID: "A1", Text: "ok"}}, InputAnchors: []specInputAnchor{{Kind: "owner_message", Ref: "m"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CreateConsoleSpec(ctx, model.ConsoleSpec{ID: id, PrincipalID: "owner-a", Title: id, EditCapabilityHash: fmt.Sprintf("%064x", i+1)}, rev, hub.now()); err != nil {
			t.Fatal(err)
		}
	}
	foreignRev, _ := revisionFromRequest("spec-foreign", "owner-b", 1, consoleSpecWriteRequest{Title: "foreign", ContentMarkdown: "# foreign", Acceptance: []specAcceptance{{ID: "A1", Text: "ok"}}, InputAnchors: []specInputAnchor{{Kind: "owner_message", Ref: "m"}}})
	if err := db.CreateConsoleSpec(ctx, model.ConsoleSpec{ID: "spec-foreign", PrincipalID: "owner-b", Title: "foreign", EditCapabilityHash: strings.Repeat("b", 64)}, foreignRev, hub.now()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/console/specs?page=1", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr := httptest.NewRecorder()
	hub.HandleConsoleSpecs(rr, req)
	var payload struct {
		Specs      []model.ConsoleSpec `json:"specs"`
		Page       int                 `json:"page"`
		PageSize   int                 `json:"page_size"`
		Total      int                 `json:"total"`
		TotalPages int                 `json:"total_pages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || len(payload.Specs) != 20 || payload.PageSize != 20 || payload.Total != 21 || payload.TotalPages != 2 {
		t.Fatalf("page1 code=%d payload=%+v", rr.Code, payload)
	}
	for _, spec := range payload.Specs {
		if spec.PrincipalID != "owner-a" {
			t.Fatalf("foreign spec leaked: %+v", spec)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/console/specs?page=2", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	req = req.WithContext(context.WithValue(req.Context(), consolePlatformRoleContextKey{}, consolePlatformRoleManagement))
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpecs(rr, req)
	payload.Specs = nil
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil || len(payload.Specs) != 1 {
		t.Fatalf("page2 payload=%+v err=%v", payload, err)
	}
}

func stringsReplace(value, old, new string) string {
	return bytes.NewBuffer(bytes.ReplaceAll([]byte(value), []byte(old), []byte(new))).String()
}
