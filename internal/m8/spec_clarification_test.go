package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func clarificationHTTP(e *shimEnv, method, qid, spec, user, cred, body, query string, policy bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/console/questions/clarifications"+query, strings.NewReader(body))
	r.SetPathValue("questionID", qid)
	r.SetPathValue("specID", spec)
	if user != "" {
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, user))
	}
	if cred != "" {
		r.Header.Set("Authorization", "Bearer "+cred)
	}
	w := httptest.NewRecorder()
	fn := e.hub.HandleClarification
	if policy {
		fn = e.hub.HandleClarificationPolicy
	}
	if cred != "" {
		e.hub.RequireAgentBlocker(fn)(w, r)
	} else {
		fn(w, r)
	}
	return w
}
func clarificationHTTPFixture(t *testing.T) (*shimEnv, *identity.Group, store.TaskFlowState, store.PilotQuestion) {
	t.Helper()
	e, g, s := r32FlowEnv(t)
	q := questionHTTPCreate(t, e, s)
	e.hub.SpecClarificationEnabled = true
	w := clarificationHTTP(e, "POST", "", q.SpecID, "zzc", "", `{"expected_version":0,"pm_user":"LBL"}`, "", true)
	if w.Code != 200 {
		t.Fatal("CLARIFICATION_HTTP_CONFIGURE", w.Code, w.Body)
	}
	return e, g, s, q
}
func clarificationHTTPBody(id, kind string) string {
	return fmt.Sprintf(`{"request_id":%q,"kind":%q,"entry_refs":["A1"],"body":"原范围的边界解释"}`, id, kind)
}

func TestClarificationHTTPHumanAgentAuthorityAndReplay(t *testing.T) {
	e, _, _, q := clarificationHTTPFixture(t)
	body := clarificationHTTPBody("pm-one", "explanation")
	for _, who := range []struct{ user, cred string }{{"zzc", ""}, {"", e.zKimiCred}, {"", e.lKimiCr}, {"", ""}} {
		w := clarificationHTTP(e, "POST", q.ID, "", who.user, who.cred, body, "", false)
		if w.Code != 403 && w.Code != 401 {
			t.Fatal("CLARIFICATION_HTTP_AUTHORITY_BYPASS", who.user, w.Code, w.Body)
		}
	}
	w := clarificationHTTP(e, "POST", q.ID, "", "lbl", "", body, "", false)
	if w.Code != 200 {
		t.Fatal("CLARIFICATION_HTTP_PM_REPLY", w.Code, w.Body)
	}
	var first store.ClarificationResult
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	w = clarificationHTTP(e, "POST", q.ID, "", "lbl", "", body, "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"replayed":true`) {
		t.Fatal("CLARIFICATION_HTTP_REPLAY_FAILED", w.Code, w.Body)
	}
	w = clarificationHTTP(e, "POST", q.ID, "", "lbl", "", clarificationHTTPBody("pm-two", "explanation"), "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"floor":2`) {
		t.Fatal("CLARIFICATION_HTTP_SAME_TEXT_LOST", w.Code, w.Body)
	}
	w = clarificationHTTP(e, "POST", q.ID, "", "", e.zKimiCred, clarificationHTTPBody("agent-material", "technical"), "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"technical"`) {
		t.Fatal("CLARIFICATION_HTTP_TECHNICAL_INPUT", w.Code, w.Body)
	}
	w = clarificationHTTP(e, "GET", q.ID, "", "lbl", "", "", "?limit=2", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"next_before":2`) {
		t.Fatal("CLARIFICATION_HTTP_PAGE", w.Code, w.Body)
	}
	if first.ExecutionAuthorized || first.Post.Role != "pm" {
		t.Fatal("CLARIFICATION_HTTP_REPLY_AUTHORIZED_EXECUTION", first)
	}
}

func TestClarificationHTTPDeputyRevocationAndDisabledLegacyBoundary(t *testing.T) {
	e, _, _, q := clarificationHTTPFixture(t)
	w := clarificationHTTP(e, "POST", "", q.SpecID, "zzc", "", `{"expected_version":1,"pm_user":"lbl","deputy_user":"zzc"}`, "", true)
	if w.Code != 403 {
		t.Fatal("CLARIFICATION_HTTP_OWNER_APPOINTED_DEPUTY", w.Code, w.Body)
	}
	w = clarificationHTTP(e, "POST", "", q.SpecID, "lbl", "", `{"expected_version":1,"pm_user":"lbl","deputy_user":"zzc"}`, "", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	body := clarificationHTTPBody("deputy-one", "explanation")
	w = clarificationHTTP(e, "POST", q.ID, "", "zzc", "", body, "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"deputy"`) {
		t.Fatal("CLARIFICATION_HTTP_DEPUTY_REPLY", w.Code, w.Body)
	}
	w = clarificationHTTP(e, "POST", "", q.SpecID, "lbl", "", `{"expected_version":2,"pm_user":"lbl"}`, "", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = clarificationHTTP(e, "POST", q.ID, "", "zzc", "", body, "", false)
	if w.Code != 403 {
		t.Fatal("CLARIFICATION_HTTP_REVOKED_DEPUTY_WRITE", w.Code, w.Body)
	}
	e.hub.SpecClarificationEnabled = false
	w = clarificationHTTP(e, "POST", q.ID, "", "lbl", "", clarificationHTTPBody("disabled", "explanation"), "", false)
	if w.Code != 503 {
		t.Fatal("CLARIFICATION_HTTP_DISABLED_WRITE", w.Code, w.Body)
	}
	ref, err := e.db.QuestionReferences(e.ctx, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := fmt.Sprintf(`{"request_id":"legacy-disabled","operation":"answer","expected_version":%d,"event_id":"legacy","answer_kind":"substantive","body":"不能恢复旧授权","evidence_ref":"evidence"}`, ref.Version)
	w = questionHTTP(e, "POST", q.ID, "", "zzc", old, "")
	if w.Code != 403 {
		t.Fatal("CLARIFICATION_HTTP_DISABLED_RESTORED_LEGACY_GRANT", w.Code, w.Body)
	}
	w = clarificationHTTP(e, "GET", q.ID, "", "lbl", "", "", "", false)
	if w.Code != 200 {
		t.Fatal("CLARIFICATION_HTTP_DISABLED_READ_FROZEN", w.Code, w.Body)
	}
}

func TestClarificationHTTPMemberRevocationAndInactiveHuman(t *testing.T) {
	e, g, state, q := clarificationHTTPFixture(t)
	if err := e.svc.Store.LeaveGroup(e.ctx, g.Code, state.Developer, "zzc"); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		w := clarificationHTTP(e, method, q.ID, "", "", e.zKimiCred, clarificationHTTPBody("left", "technical"), "", false)
		if w.Code != 403 || strings.Contains(w.Body.String(), "原范围的边界解释") {
			t.Fatal("CLARIFICATION_HTTP_LEFT_GROUP_DATA", method, w.Code, w.Body)
		}
	}
	if err := e.svc.Store.SetUserState(e.ctx, "lbl", identity.UserDisabled, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	w := clarificationHTTP(e, "POST", q.ID, "", "lbl", "", clarificationHTTPBody("inactive", "explanation"), "", false)
	if w.Code != 403 {
		t.Fatal("CLARIFICATION_HTTP_INACTIVE_PM", w.Code, w.Body)
	}
}

func TestClarificationHTTPStrictBodyAndSelectors(t *testing.T) {
	e, _, _, q := clarificationHTTPFixture(t)
	for _, query := range []string{"?owner=lbl", "?before=0", "?limit=101", "?limit=2&limit=3", "?before=-1"} {
		w := clarificationHTTP(e, "GET", q.ID, "", "lbl", "", "", query, false)
		if w.Code != 400 {
			t.Fatal("CLARIFICATION_HTTP_FORGED_SELECTOR", query, w.Code, w.Body)
		}
	}
	for _, extra := range []string{`,"actor":"lbl"`, `,"source":"feishu"`, `,"sender_open_id":"forged"`, `,"role":"pm"`} {
		body := strings.TrimSuffix(clarificationHTTPBody("bad", "explanation"), "}") + extra + "}"
		w := clarificationHTTP(e, "POST", q.ID, "", "lbl", "", body, "", false)
		if w.Code != 400 {
			t.Fatal("CLARIFICATION_HTTP_FORGED_IDENTITY", w.Code, w.Body)
		}
	}
}
