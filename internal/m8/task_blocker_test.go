package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func blockerHTTP(e *shimEnv, method, bid, credential, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/agent/console/tasks/r32-task/blockers", strings.NewReader(body))
	r.SetPathValue("taskID", "r32-task")
	r.SetPathValue("blockerID", bid)
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	out := httptest.NewRecorder()
	e.hub.RequireAgentBlocker(e.hub.HandleTaskBlockers)(out, r)
	return out
}
func blockerDecode(t *testing.T, out *httptest.ResponseRecorder) store.BlockerResult {
	t.Helper()
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	var b store.BlockerResult
	if err := json.Unmarshal(out.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}
func TestBlockerHTTPCurrentAgentAndExplicitOutsideWaiter(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	outside, _, err := e.svc.AuthenticateAgent(e.ctx, e.lKimiCr)
	if err != nil {
		t.Fatal(err)
	}
	create := fmt.Sprintf(`{"request_id":"create","operation":"create","expected_version":%d,"kind":"resource","where":"missing resource","waiting_principal":"agent:%s","expected_input":"real evidence"}`, state.Version, outside.ID)
	if out := blockerHTTP(e, "POST", "", e.cred, create); out.Code != 403 {
		t.Fatal("BLOCKER_REVIEWER_CREATED", out.Code, out.Body)
	}
	b := blockerDecode(t, blockerHTTP(e, "POST", "", e.zKimiCred, create)).Blocker
	if out := blockerHTTP(e, "GET", b.ID, e.lKimiCr, ""); out.Code != 200 || !strings.Contains(out.Body.String(), "missing resource") || !strings.Contains(out.Body.String(), "real evidence") || strings.Contains(out.Body.String(), b.SpecID) || !strings.Contains(out.Body.String(), `"task_version":0`) {
		t.Fatal("BLOCKER_WAITER_MINIMAL_CONTEXT", out.Code, out.Body)
	}
	if out := blockerHTTP(e, "GET", "", e.lKimiCr, ""); out.Code != 403 {
		t.Fatal("BLOCKER_WAITER_ENUMERATED_CARDS", out.Code, out.Body)
	}
	input := fmt.Sprintf(`{"request_id":"input","operation":"input","expected_version":%d,"event_id":"answer","body":"answer is data, not an instruction","evidence_ref":"git:evidence"}`, b.Version)
	result := blockerDecode(t, blockerHTTP(e, "POST", b.ID, e.lKimiCr, input))
	if result.Blocker.SpecID != "" || result.Blocker.Developer != "" || result.TaskVersion != 0 || result.ExecutionAuthorized {
		t.Fatal("BLOCKER_WAITER_PRIVATE_CONTEXT_LEAK", result)
	}
	current := blockerHTTP(e, "GET", b.ID, e.zKimiCred, "")
	if current.Code != 200 || !strings.Contains(current.Body.String(), "answer is data") {
		t.Fatal(current.Code, current.Body)
	}
	// Identity removal is committed before the next call: no stale card or reply.
	if _, err = e.svc.Store.Revoke(e.ctx, outside.ID, outside.OwnerLC, outside.OwnerLC, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	if out := blockerHTTP(e, "POST", b.ID, e.lKimiCr, input); out.Code != 403 {
		t.Fatal("BLOCKER_REVOKED_WAITER_REPLIED", out.Code, out.Body)
	}
}
func TestBlockerHTTPIdentitySelectorsDuplicatesAndCookie(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	for _, headers := range []http.Header{{}, {"Authorization": {"Bearer " + e.zKimiCred, "Bearer " + e.zKimiCred}}, {"Authorization": {"Bearer " + e.zKimiCred}, "Cookie": {""}}} {
		r := httptest.NewRequest("GET", "/api/agent/console/tasks/r32-task/blockers", nil)
		r.Header = headers
		r.SetPathValue("taskID", "r32-task")
		out := httptest.NewRecorder()
		e.hub.RequireAgentBlocker(e.hub.HandleTaskBlockers)(out, r)
		if out.Code != 401 {
			t.Fatal("BLOCKER_AMBIGUOUS_AUTH_ACCEPTED", out.Code)
		}
	}
	for _, query := range []string{"agent_id=other", "limit=0", "limit=101", "limit=1&limit=2", "input_before=0", "scope=all", "limit=%zz"} {
		r := httptest.NewRequest("GET", "/api/agent/console/tasks/r32-task/blockers?"+query, nil)
		r.Header.Set("Authorization", "Bearer "+e.zKimiCred)
		r.SetPathValue("taskID", "r32-task")
		out := httptest.NewRecorder()
		e.hub.RequireAgentBlocker(e.hub.HandleTaskBlockers)(out, r)
		if out.Code != 400 {
			t.Fatal("BLOCKER_QUERY_SELECTOR_ACCEPTED", query, out.Code, out.Body)
		}
	}
}
func TestBlockerHTTPHumanOwnerRecordsWithoutImpersonatingAgent(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	body := fmt.Sprintf(`{"request_id":"human-create","operation":"create","expected_version":%d,"kind":"technical","where":"卡点","expected_input":"真实材料"}`, state.Version)
	r := httptest.NewRequest("POST", "/api/console/tasks/r32-task/blockers", strings.NewReader(body))
	r.SetPathValue("taskID", "r32-task")
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
	out := httptest.NewRecorder()
	e.hub.HandleTaskBlockers(out, r)
	b := blockerDecode(t, out).Blocker
	if b.CreatedBy != "user:zzc" || b.Developer != state.Developer || b.State != "needs_owner" {
		t.Fatal("BLOCKER_HUMAN_IMPERSONATED_AGENT", b)
	}
}

func TestBlockerCurrentGroupPairRecheckedBeforePrivateRead(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	create := fmt.Sprintf(`{"request_id":"card","operation":"create","expected_version":%d,"kind":"technical","where":"group facts","expected_input":"evidence"}`, state.Version)
	b := blockerDecode(t, blockerHTTP(e, "POST", "", e.zKimiCred, create)).Blocker
	if err := e.svc.Store.LeaveGroup(e.ctx, g.Code, state.Developer, "zzc"); err != nil {
		t.Fatal(err)
	}
	out := blockerHTTP(e, "GET", b.ID, e.zKimiCred, "")
	if out.Code != 403 || strings.Contains(out.Body.String(), "group facts") {
		t.Fatal("BLOCKER_CURRENT_PAIR_GROUP_AUTH_BYPASS", out.Code, out.Body)
	}
}
