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
	"time"

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
		{ID: "owner-a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "oa", DisplayName: "甲", Active: true},
		{ID: "owner-b", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ob", DisplayName: "乙", Active: true},
		{ID: "manager", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "om", DisplayName: "丙", Active: true},
	} {
		if err := db.UpsertConsolePrincipal(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	for _, principal := range []string{"owner-a", "owner-b", "manager"} {
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
	db := hub.Repo.(*store.SQLite)
	if err := db.TransferConsoleSpec(context.Background(), created.ID, "owner-a", "丙", "review", hub.now()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeConsoleSpec(context.Background(), created.ID, "manager", hub.now()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+created.ID+"/tasks", strings.NewReader(`{"title":"Implement","content":"Do it","definition_of_done":"tests pass","estimated_hours":16,"depends_on":[]}`))
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionSubmit, hub.HandleConsoleSpecTasks)(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("split code=%d body=%s", rr.Code, rr.Body.String())
	}
	tasks, err := hub.Repo.(*store.SQLite).ListConsoleTaskProposals(context.Background(), created.ID)
	if err != nil || len(tasks) != 1 || tasks[0].Title != "Implement" || tasks[0].EstimatedHours != 16 || !strings.Contains(tasks[0].AcceptanceJSON, "tests pass") {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
}

func TestConsoleTasksAreTopologicalAndParallelIsDerived(t *testing.T) {
	chain := []model.ConsoleTaskProposal{
		{ID: "c", DependsOnJSON: `["b"]`}, {ID: "b", DependsOnJSON: `["a"]`}, {ID: "a", DependsOnJSON: `[]`},
	}
	ordered := orderConsoleTasks(chain, chain)
	if got := []string{ordered[0].ID, ordered[1].ID, ordered[2].ID}; fmt.Sprint(got) != "[a b c]" {
		t.Fatalf("topological order=%v want [a b c]", got)
	}
	if ordered[0].DependencyLevel != 0 || ordered[1].DependencyLevel != 1 || ordered[2].DependencyLevel != 2 {
		t.Fatalf("levels=%+v", ordered)
	}
	peers := []model.ConsoleTaskProposal{{ID: "a", DependsOnJSON: `[]`}, {ID: "d", DependsOnJSON: `[]`}}
	peers = orderConsoleTasks(peers, peers)
	if !peers[0].Parallel || !peers[1].Parallel {
		t.Fatalf("same-level peers must be parallel: %+v", peers)
	}
	peers[1].DependsOnJSON = `["a"]`
	peers = orderConsoleTasks(peers, peers)
	if peers[0].Parallel || peers[1].Parallel {
		t.Fatalf("dependency must remove parallel marker: %+v", peers)
	}
}

func TestConsoleSpecDetailReturnsTopologicalTasksDespiteReverseCreatedTimes(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	rr := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rr.Code, rr.Body.String())
	}
	var created model.ConsoleSpec
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	db := hub.Repo.(*store.SQLite)
	if err := db.TransferConsoleSpec(context.Background(), created.ID, "owner-a", "丙", "review", hub.now()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeConsoleSpec(context.Background(), created.ID, "manager", hub.now()); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	for _, p := range []model.ConsoleTaskProposal{
		{ID: "a", SpecID: created.ID, Title: "A", TaskContent: "A", Status: "planned", DependsOnJSON: `[]`, AcceptanceJSON: `[]`, ReviewedBy: "owner-a", CreatedAt: base.Add(3 * time.Second), ReviewedAt: base.Add(3 * time.Second)},
		{ID: "b", SpecID: created.ID, Title: "B", TaskContent: "B", Status: "planned", DependsOnJSON: `["a"]`, AcceptanceJSON: `[]`, ReviewedBy: "owner-a", CreatedAt: base.Add(2 * time.Second), ReviewedAt: base.Add(2 * time.Second)},
		{ID: "c", SpecID: created.ID, Title: "C", TaskContent: "C", Status: "planned", DependsOnJSON: `["b"]`, AcceptanceJSON: `[]`, ReviewedBy: "owner-a", CreatedAt: base.Add(time.Second), ReviewedAt: base.Add(time.Second)},
	} {
		if err := db.CreateConsoleTaskProposal(context.Background(), p, "owner-a", false); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/console/specs/"+created.ID, nil)
	req.SetPathValue("specID", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpec(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail code=%d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Tasks []model.ConsoleTaskProposal `json:"tasks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := []string{out.Tasks[0].ID, out.Tasks[1].ID, out.Tasks[2].ID}
	if fmt.Sprint(got) != "[a b c]" {
		t.Fatalf("handler task order=%v want [a b c]", got)
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
	if rr.Code != http.StatusForbidden {
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
		{name: "management role does not grant another owners write", id: foreign.ID, want: http.StatusForbidden},
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

	// Approval permission remains distinct from write permission; a forbidden
	// management action must not mutate the spec.
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/archive", nil)
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.RequireConsolePlatformAction(consolePlatformActionApprove, hub.HandleConsoleSpecArchive)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("owner review code=%d", rr.Code)
	}
	stored, err := hub.Repo.(*store.SQLite).ConsoleSpec(context.Background(), "owner-a", spec.ID, false)
	if err != nil || stored == nil || stored.State != "editing" {
		t.Fatalf("403 performed side effect spec=%+v err=%v", stored, err)
	}
}

func TestConsoleSpecEditorsHTTPExcludesActiveNonOwners(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	if err := db.UpsertConsolePrincipal(context.Background(), model.ConsolePrincipal{ID: "nonowner", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "nonowner", DisplayName: "非业主", Active: true}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/console/spec-editors", nil)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr := httptest.NewRecorder()
	hub.HandleConsoleSpecEditors(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("editors code=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "非业主") || !strings.Contains(rr.Body.String(), "甲") || !strings.Contains(rr.Body.String(), "乙") {
		t.Fatalf("owner-only editor response=%s", rr.Body.String())
	}
	created := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, hub.HandleConsoleSpecs)
	var spec model.ConsoleSpec
	if err := json.Unmarshal(created.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/workflow/transfer", bytes.NewBufferString(`{"target_short_name":"非业主","state":"editing"}`))
	req.SetPathValue("specID", spec.ID)
	req.SetPathValue("action", "transfer")
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpecWorkflow(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "permission denied") {
		t.Fatalf("non-owner transfer code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestConsoleSpecSharedCapabilityIsReadOnly(t *testing.T) {
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
	token := strings.SplitN(created.EditURL, "#edit=", 2)[1]
	sharedPatch := strings.TrimSuffix(validSpecBody, "}") + `,"expected_revision":1}`
	req := httptest.NewRequest(http.MethodPatch, "/api/specs/shared/"+created.ID, bytes.NewBufferString(sharedPatch))
	req.SetPathValue("specID", created.ID)
	req.Header.Set("X-DW-Spec-Edit-Capability", token)
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpec(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "editing closed or invalid capability") {
		t.Fatalf("shared PATCH code=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/specs/shared/"+created.ID, nil)
	req.SetPathValue("specID", created.ID)
	rr = httptest.NewRecorder()
	hub.HandleConsoleSpec(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "principal_id") {
		t.Fatalf("shared GET code=%d body=%s", rr.Code, rr.Body.String())
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

// Legacy dual-gate HTTP coverage was replaced by the three-state workflow tests.

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
	if strings.Contains(rr.Body.String(), "principal_id") {
		t.Fatalf("read API leaked principal_id: %s", rr.Body.String())
	}
	for _, spec := range payload.Specs {
		if spec.OwnerShortName != "甲" || spec.Title == "foreign" {
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
