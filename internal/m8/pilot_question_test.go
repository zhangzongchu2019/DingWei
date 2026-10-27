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

func questionHTTP(e *shimEnv, method, qid, credential, user, body, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/console/questions"+query, strings.NewReader(body))
	r.SetPathValue("questionID", qid)
	r.SetPathValue("taskID", "r32-task")
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	if user != "" {
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, user))
	}
	out := httptest.NewRecorder()
	if credential != "" {
		e.hub.RequireAgentBlocker(e.hub.HandleQuestion)(out, r)
	} else {
		e.hub.HandleQuestion(out, r)
	}
	return out
}
func questionHTTPCreate(t *testing.T, e *shimEnv, state store.TaskFlowState) store.PilotQuestion {
	t.Helper()
	body := fmt.Sprintf(`{"request_id":"question-create","expected_version":%d,"kind":"requirement_unclear","title":"真实问题","observed_fact":"实际现象","question":"期待澄清","expected_answer":"可验证的明确答复","entry_refs":["A1"]}`, state.Version)
	out := questionHTTP(e, "POST", "", e.zKimiCred, "", body, "")
	if out.Code != 200 {
		t.Fatal("QUESTION_HTTP_CREATE", out.Code, out.Body)
	}
	var result store.QuestionResult
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Question
}
func TestPilotQuestionHTTPAuthorizationAndNoPMImpersonation(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	q := questionHTTPCreate(t, e, state)
	if out := questionHTTP(e, "GET", q.ID, e.lKimiCr, "", "", ""); out.Code != 403 {
		t.Fatal("QUESTION_OUTSIDE_AGENT_READ", out.Code, out.Body)
	}
	for _, body := range []string{
		fmt.Sprintf(`{"request_id":"spoof","expected_version":%d,"operation":"answer","event_id":"event","answer_kind":"substantive","body":"伪造业务答复","evidence_ref":"git:ref"}`, q.Version),
		fmt.Sprintf(`{"request_id":"spoof","expected_version":%d,"operation":"input","event_id":"event","body":"材料","evidence_ref":"git:ref","scope_change":true}`, q.Version),
		fmt.Sprintf(`{"request_id":"spoof","expected_version":%d,"operation":"assign","pm_user":"lbl"}`, q.Version),
	} {
		want := 403
		if strings.Contains(body, `"operation":"input"`) {
			want = 426
		}

		if out := questionHTTP(e, "POST", q.ID, e.zKimiCred, "", body, ""); out.Code != want {
			t.Fatal("QUESTION_AGENT_HUMAN_BOUNDARY", out.Code, out.Body)
		}
	}
	if out := questionHTTP(e, "GET", q.ID, e.zKimiCred, "", "", ""); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if err := e.svc.Store.LeaveGroup(e.ctx, g.Code, state.Developer, "zzc"); err != nil {
		t.Fatal(err)
	}
	out := questionHTTP(e, "GET", q.ID, e.zKimiCred, "", "", "")
	if out.Code != 403 || strings.Contains(out.Body.String(), "实际现象") {
		t.Fatal("QUESTION_GROUP_REMOVAL_LEAK", out.Code, out.Body)
	}
}
func TestPilotQuestionHTTPPolicyAndMinimalPMObjectGrant(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	c, err := e.db.QuestionAuthorizationContext(e.ctx, "", state.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err = e.db.QuestionAuthorizationContext(e.ctx, c.SpecID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.db.SetQuestionPolicy(e.ctx, c, store.BlockerActor{UserID: "zzc"}, store.QuestionUpdate{RequestID: "configure", PMUser: "lbl", LeaderUser: "zzc"}, func() error { return nil }, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	q := questionHTTPCreate(t, e, state)
	out := questionHTTP(e, "GET", q.ID, "", "lbl", "", "")
	if out.Code != 200 || !strings.Contains(out.Body.String(), "实际现象") || strings.Contains(out.Body.String(), `"can_manage":true`) || strings.Contains(out.Body.String(), `"can_configure_recipients":true`) || strings.Contains(out.Body.String(), `"leader_user":"zzc"`) {
		t.Fatal("QUESTION_PM_MINIMAL_OBJECT_GRANT", out.Code, out.Body)
	}
	body := fmt.Sprintf(`{"request_id":"human-answer","operation":"answer","expected_version":%d,"event_id":"answer","answer_kind":"substantive","body":"明确的范围结论","evidence_ref":"git:answer"}`, q.Version)
	out = questionHTTP(e, "POST", q.ID, "", "lbl", body, "")
	if out.Code != 426 {
		t.Fatal("QUESTION_OLD_PM_WRITE_NOT_UPGRADE_REJECTED", out.Code, out.Body)
	}
	e.hub.SpecClarificationEnabled = true
	out = clarificationHTTP(e, "POST", q.ID, "", "lbl", "", clarificationHTTPBody("human-answer", "explanation"), "", false)
	if out.Code != 200 {
		t.Fatal("QUESTION_PM_ANSWER_FAILED", out.Code, out.Body)
	}
	if other := r32HTTP(e, "GET", state.ID, e.lKimiCr, ""); other.Code != 403 && (other.Code != 200 || !strings.Contains(other.Body.String(), `"tasks":[]`)) {
		t.Fatal("QUESTION_GRANT_EXPANDED_TO_TASK", other.Code, other.Body)
	}
	if strings.Contains(out.Body.String(), `"execution_authorized":true`) {
		t.Fatal("QUESTION_REPLY_EXECUTION_AUTH")
	}
	e.hub.PublicOrigin = "https://console.fixture.invalid"
	sent := 0
	e.hub.QuestionFeishuEnabled = true
	e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) { sent++; return "provider", nil }
	e.hub.processQuestions(context.Background())
	if sent != 0 {
		t.Fatal("QUESTION_UNBOUND_USER_SENT", sent)
	}
}
func TestPilotQuestionHTTPRejectsSelectorsAndMixedCredential(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	q := questionHTTPCreate(t, e, state)
	for _, query := range []string{"?agent_id=other", "?before=0", "?before=1&before=2", "?scope=all", "?limit=2", "?change_before=-1"} {
		out := questionHTTP(e, "GET", q.ID, e.zKimiCred, "", "", query)
		if out.Code != 400 {
			t.Fatal("QUESTION_DETAIL_QUERY_FORGED", query, out.Code, out.Body)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=1&limit=2", "?owner=zzc", "?cursor=forged"} {
		out := questionHTTP(e, "GET", "", e.zKimiCred, "", "", query)
		if out.Code != 400 {
			t.Fatal("QUESTION_LIST_QUERY_FORGED", query, out.Code, out.Body)
		}
	}
	r := httptest.NewRequest("GET", "/api/agent/console/questions", nil)
	r.Header.Set("Authorization", "Bearer "+e.zKimiCred)
	r.Header.Set("Cookie", "session=synthetic")
	out := httptest.NewRecorder()
	e.hub.RequireAgentBlocker(e.hub.HandleQuestion)(out, r)
	if out.Code != 401 {
		t.Fatal("QUESTION_MIXED_AUTH_ACCEPTED", out.Code)
	}
}
func TestPilotQuestionMessageNoPrivatePayloadAndOriginCannotBeClientSelected(t *testing.T) {
	e, _, _ := r32FlowEnv(t)
	q := store.PilotQuestion{ID: "question", Kind: "technical", ObservedFact: "PRIVATE_FACT", InputAnchors: "PRIVATE_ANCHOR", Question: "PRIVATE_BODY"}
	for _, origin := range []string{"http://insecure.invalid", "https://credential:private@host.invalid", "https://host.invalid/path", "https://host.invalid/?redirect=private"} {
		e.hub.PublicOrigin = origin
		if _, ok := e.hub.questionLink(q.ID); ok {
			t.Fatal("QUESTION_UNTRUSTED_ORIGIN", origin)
		}
	}
	e.hub.PublicOrigin = "https://console.fixture.invalid"
	link, ok := e.hub.questionLink(q.ID)
	if !ok {
		t.Fatal("valid origin rejected")
	}
	body := questionMessage(q, "question", link)
	if strings.Contains(body, "PRIVATE_") || !strings.Contains(body, "直接回复此消息不会回写") {
		t.Fatal("QUESTION_FEISHU_PRIVATE_DATA_OR_CALLBACK_CONFUSION", body)
	}
}
func TestPilotQuestionRevokedResponsibleDoesNotTrapOwnerCleanup(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	q := questionHTTPCreate(t, e, state)
	outside, _, err := e.svc.AuthenticateAgent(e.ctx, e.lKimiCr)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"request_id":"outside","operation":"assign","expected_version":%d,"pm_user":"zzc","leader_user":"zzc","responsible_principal":"agent:%s"}`, q.Version, outside.ID)
	out := questionHTTP(e, "POST", q.ID, "", "zzc", body, "")
	if out.Code != 200 {
		t.Fatal("QUESTION_EXTERNAL_ASSIGN", out.Code, out.Body)
	}
	var assigned store.QuestionResult
	if err = json.Unmarshal(out.Body.Bytes(), &assigned); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Store.Revoke(e.ctx, outside.ID, outside.OwnerLC, outside.OwnerLC, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	out = questionHTTP(e, "GET", q.ID, e.lKimiCr, "", "", "")
	if out.Code != 403 {
		t.Fatal("QUESTION_REVOKED_RESPONSIBLE_READ", out.Code, out.Body)
	}
	out = questionHTTP(e, "GET", q.ID, "", "zzc", "", "")
	if out.Code != 200 {
		t.Fatal("QUESTION_OWNER_TRAPPED_BY_REVOKED_RESPONSIBLE", out.Code, out.Body)
	}
	body = fmt.Sprintf(`{"request_id":"cleanup","operation":"cancel","expected_version":%d,"reason":"责任Agent已吊销，停止旧问题等待"}`, assigned.Question.Version)
	out = questionHTTP(e, "POST", q.ID, "", "zzc", body, "")
	if out.Code != 200 {
		t.Fatal("QUESTION_OWNER_CANNOT_CANCEL_REVOKED_RESPONSIBLE", out.Code, out.Body)
	}
}

func TestPilotQuestionReminderExplicitlyDirectsReplyToOriginalNotice(t *testing.T) {
	q := store.PilotQuestion{ID: "question", Kind: "technical", ObservedFact: "PRIVATE_FACT", Question: "PRIVATE_BODY"}
	reminder := questionMessage(q, "remind_60m", "https://console.fixture.invalid/console/questions?question_id=question")
	if !strings.Contains(reminder, "请在原通知下回复") || !strings.Contains(reminder, "请勿直接回复本提醒") || !strings.Contains(reminder, "答复到件仍须重核") || strings.Contains(reminder, "PRIVATE_") {
		t.Fatalf("提醒必须明确指回原通知，不泄露正文或改变到件边界: %s", reminder)
	}
	for _, kind := range []string{"question", "assignment"} {
		body := questionMessage(q, kind, "https://console.fixture.invalid/console/questions")
		if !strings.Contains(body, "直接回复此消息不会回写问题") || strings.Contains(body, "请在原通知下回复") {
			t.Fatalf("既有非提醒通知文案回归: %s", body)
		}
	}
}
