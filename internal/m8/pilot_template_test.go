package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
)

func templateHTTP(e *shimEnv, method, id, cred, user, body, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.SetPathValue("draftID", id)
	if user != "" {
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, user))
	}
	out := httptest.NewRecorder()
	if cred != "" {
		r.Header.Set("Authorization", "Bearer "+cred)
		e.hub.RequireAgentBlocker(e.hub.HandleTemplateDraft)(out, r)
	} else {
		e.hub.HandleTemplateDraft(out, r)
	}
	return out
}
func templateHTTPFixture(t *testing.T) (*shimEnv, store.TemplateView) {
	t.Helper()
	e := newShimEnv(t)
	if err := e.hub.syncNewPlatformUser(e.ctx, "zzc", nil); err != nil {
		t.Fatal(err)
	}
	dev, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.db.SaveTemplateSource(e.ctx, "zzc", "source", store.TemplateSource{URL: "https://fixture.feishu.cn/docx/Document", Revision: "v1", Kind: "manual_export", Content: "R1 PRIVATE_RAW_SOURCE"}, func() error { return nil }, e.hub.now())
	if err != nil {
		t.Fatal(err)
	}
	d := store.TemplateDocument{Kind: "parent", Title: "PRIVATE_TEMPLATE", Goal: "bounded", Scope: "fixture", SourceID: source.ID, TeamMode: "single", Defaults: store.TemplateFields{Acceptance: "exact", Method: "local fixture", Deliverable: "artifact", Environment: "isolated", Executor: "agent:" + dev.ID, Reviewer: "agent:" + e.agID, Signoff: "user:zzc"}, Entries: []store.TemplateEntry{{ID: "R1", Kind: "requirement", Title: "bounded", OwnerRef: "R1"}}}
	raw, _ := json.Marshal(store.TemplateOperation{RequestID: "create", Operation: "save", Document: &d})
	out := templateHTTP(e, "POST", "", "", "zzc", string(raw), "/api/console/spec-templates")
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	var b struct {
		Draft store.TemplateView `json:"draft"`
	}
	if err = json.Unmarshal(out.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return e, b.Draft
}
func TestPilotTemplateHTTPPrivateInvitationsAndHumanOnlyConfirmation(t *testing.T) {
	e, v := templateHTTPFixture(t)
	for _, who := range []struct{ cred, user string }{{e.lKimiCr, ""}, {"", "lbl"}} {
		out := templateHTTP(e, "GET", v.ID, who.cred, who.user, "", "/api/console/spec-templates/x")
		if out.Code != 403 || strings.Contains(out.Body.String(), "PRIVATE_TEMPLATE") {
			t.Fatal("TEMPLATE_FOREIGN_INVITATION_LEAK", out.Code, out.Body)
		}
	}
	out := templateHTTP(e, "GET", v.ID, e.zKimiCred, "", "", "/api/agent/console/spec-templates/x")
	if out.Code != 200 || !strings.Contains(out.Body.String(), "PRIVATE_TEMPLATE") || strings.Contains(out.Body.String(), "PRIVATE_RAW_SOURCE") {
		t.Fatal("TEMPLATE_AGENT_GRANT_WRONG", out.Code, out.Body)
	}
	for _, body := range []string{`{"request_id":"confirm","operation":"confirm","confirm_human":true}`, `{"request_id":"save","operation":"save","document":{}}`, `{"request_id":"sign","operation":"responsibility","role":"signoff"}`, `{"request_id":"full","operation":"responsibility","role":"full_review","confirm_human":true}`} {
		out = templateHTTP(e, "POST", v.ID, e.zKimiCred, "", body, "/api/agent/console/spec-templates/x")
		if out.Code != 403 {
			t.Fatal("TEMPLATE_AGENT_HUMAN_CONFIRMATION", out.Code, out.Body)
		}
	}
	u := store.TemplateOperation{RequestID: "accept", ExpectedVersion: v.Version, Operation: "responsibility", PlanSHA: v.PlanSHA, EntryIDs: []string{"R1"}, Role: "executor", Decision: "accepted"}
	raw, _ := json.Marshal(u)
	out = templateHTTP(e, "POST", v.ID, e.zKimiCred, "", string(raw), "/api/agent/console/spec-templates/x")
	if out.Code != 200 || strings.Contains(out.Body.String(), `"execution_authorized":true`) {
		t.Fatal("TEMPLATE_ACTUAL_AGENT_RECEIPT", out.Code, out.Body)
	}
	out = templateHTTP(e, "POST", v.ID, "", "zzc", string(raw), "/api/console/spec-templates/x")
	if out.Code != 403 {
		t.Fatal("TEMPLATE_HUMAN_PROXY_AGENT", out.Code, out.Body)
	}
}
func TestPilotTemplateHTTPStaleAndRemovedInvitation(t *testing.T) {
	e, v := templateHTTPFixture(t)
	body := fmt.Sprintf(`{"request_id":"stale","operation":"responsibility","expected_version":99,"plan_sha256":%q,"entry_ids":["R1"],"role":"executor","decision":"accepted"}`, v.PlanSHA)
	if out := templateHTTP(e, "POST", v.ID, e.zKimiCred, "", body, "/api/agent/console/spec-templates/x"); out.Code != 409 {
		t.Fatal("TEMPLATE_STALE_RECEIPT_ACCEPTED", out.Code, out.Body)
	}
	d := v.Document
	d.Defaults.Executor = "user:zzc"
	d.Entries[0].Fields.Executor = "user:zzc"
	raw, _ := json.Marshal(store.TemplateOperation{RequestID: "reassign", Operation: "save", ExpectedVersion: 1, Document: &d})
	if out := templateHTTP(e, "POST", v.ID, "", "zzc", string(raw), "/api/console/spec-templates/x"); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	out := templateHTTP(e, "GET", v.ID, e.zKimiCred, "", "", "/api/agent/console/spec-templates/x")
	if out.Code != 403 || strings.Contains(out.Body.String(), "PRIVATE_TEMPLATE") {
		t.Fatal("TEMPLATE_REMOVED_ROLE_READ_LEAK", out.Code, out.Body)
	}
}
func TestPilotTemplateHTTPSourceNoFetchAndForgedKind(t *testing.T) {
	e := newShimEnv(t)
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/console/spec-template-sources", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
		w := httptest.NewRecorder()
		e.hub.HandleTemplateSource(w, r)
		return w
	}
	out := call(`{"request_id":"pending","mode":"connector","source_url":"https://fixture.feishu.cn/docx/Doc"}`)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "pending_config") {
		t.Fatal("TEMPLATE_UNCONFIGURED_IMPORT_FAKED", out.Code, out.Body)
	}
	for _, body := range []string{`{"request_id":"forged","mode":"trusted_connector","source_url":"https://fixture.feishu.cn/docx/Doc","source_revision":"v1","content_markdown":"fake"}`, `{"request_id":"forged2","mode":"connector","source_url":"https://fixture.feishu.cn/docx/Doc","content_markdown":"fake"}`, `{"request_id":"ssrf","mode":"connector","source_url":"http://127.0.0.1/private"}`, `{"request_id":"cookie","mode":"connector","source_url":"https://fixture.feishu.cn/docx/Doc","cookie":"private"}`} {
		if out := call(body); out.Code != 400 {
			t.Fatal("TEMPLATE_SOURCE_IDENTITY_FORGED", out.Code, out.Body)
		}
	}
}
func TestPilotTemplateHTTPActualConfirmRemainsEditing(t *testing.T) {
	e, v := templateHTTPFixture(t)
	for _, r := range v.Roles {
		cred, user := "", ""
		switch r.Role {
		case "executor":
			cred = e.zKimiCred
		case "reviewer":
			cred = e.cred
		case "signoff":
			user = "zzc"
		}
		u := store.TemplateOperation{RequestID: "accept-" + r.Role, Operation: "responsibility", ExpectedVersion: v.Version, PlanSHA: v.PlanSHA, EntryIDs: []string{r.EntryID}, Role: r.Role, Decision: "accepted"}
		raw, _ := json.Marshal(u)
		out := templateHTTP(e, "POST", v.ID, cred, user, string(raw), "/api/console/spec-templates/x")
		if out.Code != 200 {
			t.Fatal("TEMPLATE_HTTP_ACCEPT_PATH", r.Role, out.Code, out.Body)
		}
	}
	u := store.TemplateOperation{RequestID: "human-confirm", Operation: "confirm", ExpectedVersion: v.Version, PlanSHA: v.PlanSHA, ConfirmHuman: true}
	raw, _ := json.Marshal(u)
	out := templateHTTP(e, "POST", v.ID, "", "zzc", string(raw), "/api/console/spec-templates/x")
	if out.Code != 200 {
		t.Fatal("TEMPLATE_HTTP_CONFIRM_PATH", out.Code, out.Body)
	}
	var b struct {
		Draft store.TemplateView `json:"draft"`
	}
	json.Unmarshal(out.Body.Bytes(), &b)
	spec, err := e.db.ConsoleSpec(e.ctx, "zzc", b.Draft.SpecID, false)
	if err != nil || spec.State != "editing" || b.Draft.BusinessSignedOff || b.Draft.ExecutionAuthorized {
		t.Fatal("TEMPLATE_HTTP_CONFIRM_FINAL_OR_EXECUTED", spec, err)
	}
}
func TestPilotTemplateListSummariesFitBoundedHelperRead(t *testing.T) {
	e, v := templateHTTPFixture(t)
	d := v.Document
	d.Defaults.Method = strings.Repeat("m", 6000)
	d.Entries = nil
	for i := 0; i < 40; i++ {
		d.Entries = append(d.Entries, store.TemplateEntry{ID: fmt.Sprintf("R%d", i+1), Kind: "requirement", Title: "bounded large contract", OwnerRef: "R1"})
	}
	var id string
	for i := 0; i < 10; i++ {
		d.Title = fmt.Sprintf("large-%d", i)
		var saved store.TemplateView
		err := e.hub.withTemplateIdentity(e.ctx, blockerCredential{actor: store.BlockerActor{UserID: "zzc"}}, store.TemplateReferences{Owner: "zzc"}, &d, func(snap store.TemplatePrincipalSnapshot, check func() error) error {
			var x error
			saved, x = e.db.SaveTemplateDraft(e.ctx, "zzc", "", store.TemplateOperation{RequestID: fmt.Sprintf("large-%d", i), Document: &d}, snap, check, e.hub.now())
			return x
		})
		if err != nil {
			t.Fatal(err)
		}
		id = saved.ID
	}
	out := templateHTTP(e, "GET", "", e.zKimiCred, "", "", "/api/agent/console/spec-templates")
	if out.Code != 200 || out.Body.Len() > 64<<10 {
		t.Fatal("TEMPLATE_LIST_EXCEEDS_HELPER_READ_BOUND", out.Code, out.Body.Len())
	}
	if strings.Contains(out.Body.String(), strings.Repeat("m", 50)) || strings.Contains(out.Body.String(), "PRIVATE_RAW_SOURCE") {
		t.Fatal("TEMPLATE_LIST_REPEATS_PRIVATE_CONTRACTS")
	}
	detail := templateHTTP(e, "GET", id, e.zKimiCred, "", "", "/api/agent/console/spec-templates/x")
	if detail.Code != 200 || detail.Body.Len() < 200<<10 {
		t.Fatal("TEMPLATE_DETAIL_CONTRACT_LOST", detail.Code, detail.Body.Len())
	}
}
