package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type pmDeliveryAuthStore struct {
	identity.Store
	agent  identity.Agent
	denied bool
}

func (s *pmDeliveryAuthStore) WithOwnedAgentAuthorization(ctx context.Context, owner, id string, fn func(identity.Agent, func() error) error) error {
	if s.denied || owner != s.agent.OwnerLC || id != s.agent.ID || s.agent.State != identity.AgentActive {
		return identity.ErrForbidden
	}
	return fn(s.agent, func() error { return ctx.Err() })
}

func pmAssistConsoleHTTP(e *shimEnv, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	r.Header.Set("Origin", npOrigin)
	if strings.Contains(path, "/assist/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/console/pm-spec-flow/drafts/"), "/")
		if len(parts) >= 1 {
			r.SetPathValue("specID", parts[0])
		}
		if len(parts) >= 3 {
			r.SetPathValue("requestID", parts[2])
		}
		if strings.HasSuffix(path, "/decisions") {
			r.SetPathValue("action", "decide")
		} else {
			r.SetPathValue("action", "get")
		}
	} else if strings.HasSuffix(path, "/assist") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/console/pm-spec-flow/drafts/"), "/")
		if len(parts) > 0 {
			r.SetPathValue("specID", parts[0])
		}
		r.SetPathValue("action", "start")
	}
	w := httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandlePMSpecAssistAction))(w, r)
	return w
}

func TestPMSpecFlowDefaultOffAndCASConflictIsHTTP409(t *testing.T) {
	e := newShimEnv(t)
	body := `{"spec_id":"pm-api","title":"PM flow","content_markdown":"draft","acceptance_json":"{\"criteria\":[]}","input_anchors_json":"[]","source_refs_json":"[]","norm_anchors_json":"[]","expected_version":0,"expected_sha256":""}`
	r := httptest.NewRequest(http.MethodPost, "/api/console/pm-spec-flow/drafts", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	r.Header.Set("Origin", npOrigin)
	w := httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandlePMSpecWorkingDraft))(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("default-off status=%d body=%s", w.Code, w.Body.String())
	}
	e.hub.pmSpecFlowEnabled = true
	r = httptest.NewRequest(http.MethodPost, "/api/console/pm-spec-flow/drafts", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	r.Header.Set("Origin", npOrigin)
	w = httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandlePMSpecWorkingDraft))(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"version":1`) {
		t.Fatalf("first autosave status=%d body=%s", w.Code, w.Body.String())
	}
	if spec, err := e.db.ConsoleSpec(context.Background(), "zzc", "pm-api", false); err != nil || spec != nil {
		t.Fatalf("autosave created immutable console spec: %+v %v", spec, err)
	}
	conflictBody := strings.Replace(body, `"draft"`, `"stale client bytes"`, 1)
	r = httptest.NewRequest(http.MethodPost, "/api/console/pm-spec-flow/drafts", strings.NewReader(conflictBody))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	r.Header.Set("Origin", npOrigin)
	w = httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandlePMSpecWorkingDraft))(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale autosave status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPMSpecFlowPageIsFeatureGatedAndBindsAuthenticatedPrincipal(t *testing.T) {
	e := newShimEnv(t)
	r := httptest.NewRequest(http.MethodGet, "/console/pm-specs", nil)
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
	w := httptest.NewRecorder()
	e.hub.HandlePMSpecFlowPage(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled page status=%d", w.Code)
	}
	e.hub.pmSpecFlowEnabled = true
	r = httptest.NewRequest(http.MethodGet, "/console/pm-specs", nil)
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
	w = httptest.NewRecorder()
	e.hub.HandlePMSpecFlowPage(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `window.pmPrincipal="zzc"`) || !strings.Contains(w.Body.String(), "@media(max-width:600px)") {
		t.Fatalf("enabled page status=%d principal/mobile marker missing", w.Code)
	}
}

func TestPMSpecAssistUsesNegotiatedHelperCapabilityAndAuthenticatedHTTPReply(t *testing.T) {
	e := newShimEnv(t)
	e.hub.pmSpecFlowEnabled = true
	ctx := context.Background()
	now := e.hub.now()
	if err := e.hub.syncNewPlatformUser(ctx, "zzc", nil); err != nil {
		t.Fatal(err)
	}
	draft, err := e.db.SavePMSpecWorkingDraft(ctx, "zzc", store.PMSpecDraftInput{SpecID: "pm-http-assist", Title: "PM", ContentMarkdown: "body", AcceptanceJSON: `{"criteria":[]}`, InputAnchorsJSON: `[]`, SourceRefsJSON: `[]`, NormAnchorsJSON: `[]`, ExpectedVersion: 0}, now)
	if err != nil {
		t.Fatalf("create working draft: %v", err)
	}
	agent, err := e.svc.Store.FindAgentByName(ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	startBody := fmt.Sprintf(`{"agent_id":%q,"working_version":%d,"content_sha256":%q}`, agent.ID, draft.Version, draft.ContentSHA256)
	if rr := pmAssistConsoleHTTP(e, http.MethodPost, "/api/console/pm-spec-flow/drafts/pm-http-assist/assist", startBody); rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "upgrade_required") {
		t.Fatalf("old helper was not denied before dispatch: %d %s", rr.Code, rr.Body.String())
	}
	conn, status, _ := e.dial(e.zKimiCred, "")
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("helper WS status=%d", status)
	}
	defer conn.CloseNow()
	wsCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err = writeEnvelope(wsCtx, conn, model.Envelope{To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, "spec_review_v1": true, "pm_spec_flow_v1": true}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.hub.mu.Lock()
		client := e.hub.sessionClients[AgentKeyID("zzc")]["kimi"]
		e.hub.mu.Unlock()
		if client != nil {
			client.stateMu.Lock()
			capable := client.pmSpecFlowV1
			client.stateMu.Unlock()
			if capable {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	startBody = fmt.Sprintf(`{"agent_id":%q,"working_version":%d,"content_sha256":%q}`, agent.ID, draft.Version, draft.ContentSHA256)
	started := pmAssistConsoleHTTP(e, http.MethodPost, "/api/console/pm-spec-flow/drafts/pm-http-assist/assist", startBody)
	if started.Code != http.StatusAccepted {
		t.Fatalf("start assist: %d %s", started.Code, started.Body.String())
	}
	var start struct {
		RequestID string `json:"request_id"`
	}
	if err = json.Unmarshal(started.Body.Bytes(), &start); err != nil || start.RequestID == "" {
		t.Fatalf("start response: %+v %v", start, err)
	}
	e.hub.mu.Lock()
	client := e.hub.sessionClients[AgentKeyID("zzc")]["kimi"]
	e.hub.mu.Unlock()
	if client == nil {
		t.Fatal("PM Agent session disappeared")
	}
	client.stateMu.Lock()
	capSnapshot := []bool{client.deliveryACKV1, client.specReviewV1, client.pmSpecFlowV1}
	client.stateMu.Unlock()
	if len(capSnapshot) != 3 || !capSnapshot[0] || !capSnapshot[1] || !capSnapshot[2] {
		t.Fatalf("PM Agent capabilities not negotiated: %v", capSnapshot)
	}
	// Wake the durable dispatcher explicitly so this test does not depend on
	// the asynchronous delivery retry timer after the onboarding frame is acked.
	e.hub.drainSessionDeliveries(wsCtx, client)
	var env model.Envelope
	for {
		env = readEnvelopeIncludingSystem(t, wsCtx, conn)
		if metaString(env.Meta, "type") == "delivery" && metaString(env.Meta, "spec_review_type") == "console_spec_pm_assist_request" {
			break
		}
		if deliveryID := metaString(env.Meta, "delivery_id"); deliveryID != "" && metaBool(env.Meta, "ack_required") {
			if err = writeEnvelope(wsCtx, conn, model.Envelope{ID: "pm-test-ack-" + deliveryID, To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": "delivery_ack", "delivery_id": deliveryID, "state": "processed"}}); err != nil {
				t.Fatalf("ack prerequisite delivery: %v", err)
			}
		}
		if metaString(env.Meta, "type") == "agent_network_skill" {
			if err = writeEnvelope(wsCtx, conn, model.Envelope{ID: "pm-test-onboard-ack", To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": agentNetworkSkillAckType}}); err != nil {
				t.Fatalf("ack onboarding delivery: %v", err)
			}
			continue
		}
		if metaString(env.Meta, "type") != "delivery" {
			t.Fatalf("unexpected prerequisite delivery: %#v", env.Meta)
		}
	}
	if metaString(env.Meta, "type") != "delivery" || metaString(env.Meta, "spec_review_type") != "console_spec_pm_assist_request" || metaString(env.Meta, "reply_policy") != "no_reply" || !strings.Contains(env.Body, draft.ContentSHA256) {
		t.Fatalf("unexpected PM assist delivery: %#v %s", env.Meta, env.Body)
	}
	agentGet := httptest.NewRequest(http.MethodGet, "/api/agent/console/pm-spec-assist-requests/"+start.RequestID, nil)
	agentGet.SetPathValue("requestID", start.RequestID)
	agentGet.Header.Set("Authorization", "Bearer "+e.zKimiCred)
	agentGetW := httptest.NewRecorder()
	e.hub.RequireAgentTaskFlow(e.hub.HandleAgentPMSpecAssistRequest)(agentGetW, agentGet)
	if agentGetW.Code != http.StatusOK || !strings.Contains(agentGetW.Body.String(), draft.ContentSHA256) {
		t.Fatalf("Agent request GET: %d %s", agentGetW.Code, agentGetW.Body.String())
	}
	result := `{"suggestions":[{"id":"A1","kind":"acceptance","payload":{"text":"reject invalid input"}}],"review_questions":[{"id":"Q1","question":"What if the input is empty?"}]}`
	callback := httptest.NewRequest(http.MethodPost, "/api/agent/console/pm-spec-assist-requests/"+start.RequestID+"/result", strings.NewReader(fmt.Sprintf(`{"result":%s}`, result)))
	callback.SetPathValue("requestID", start.RequestID)
	callback.Header.Set("Authorization", "Bearer "+e.lKimiCr)
	callbackW := httptest.NewRecorder()
	e.hub.RequireAgentTaskFlow(e.hub.HandleAgentPMSpecAssistResult)(callbackW, callback)
	if callbackW.Code != http.StatusConflict {
		t.Fatalf("wrong Agent callback accepted: %d %s", callbackW.Code, callbackW.Body.String())
	}
	callback = httptest.NewRequest(http.MethodPost, "/api/agent/console/pm-spec-assist-requests/"+start.RequestID+"/result", strings.NewReader(fmt.Sprintf(`{"result":%s}`, result)))
	callback.SetPathValue("requestID", start.RequestID)
	callback.Header.Set("Authorization", "Bearer "+e.zKimiCred)
	callbackW = httptest.NewRecorder()
	e.hub.RequireAgentTaskFlow(e.hub.HandleAgentPMSpecAssistResult)(callbackW, callback)
	if callbackW.Code != http.StatusOK {
		t.Fatalf("valid Agent callback rejected: %d %s", callbackW.Code, callbackW.Body.String())
	}
	statusRead := httptest.NewRequest(http.MethodGet, "/api/console/pm-spec-flow/drafts/pm-http-assist/assist/"+start.RequestID, nil)
	statusRead.SetPathValue("specID", "pm-http-assist")
	statusRead.SetPathValue("requestID", start.RequestID)
	statusRead.SetPathValue("action", "get")
	statusRead.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	statusRead.Header.Set("Origin", npOrigin)
	statusW := httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("read", e.hub.HandlePMSpecAssistAction))(statusW, statusRead)
	if statusW.Code != http.StatusOK || !strings.Contains(statusW.Body.String(), `"status":"completed"`) {
		t.Fatalf("PM assist result unavailable: %d %s", statusW.Code, statusW.Body.String())
	}
}

func TestPMSpecAssistQueuedDeliveryRechecksCurrentAgentAuthorization(t *testing.T) {
	a := identity.Agent{ID: "agent-current", OwnerLC: "zzc", Name: "kimi", State: identity.AgentActive}
	auth := &pmDeliveryAuthStore{agent: a}
	h := &Hub{Identity: &identity.Service{Store: auth}, pmSpecFlowEnabled: true}
	q := &store.PMSpecAssistRequest{ActorID: "zzc", AgentID: a.ID}
	c := &sessionClient{keyID: AgentKeyID("zzc"), sessionName: "kimi"}
	if !h.pmSpecAssistDeliveryAuthorized(context.Background(), q, c) {
		t.Fatal("active current Agent authorization was rejected")
	}
	auth.denied = true
	if h.pmSpecAssistDeliveryAuthorized(context.Background(), q, c) {
		t.Fatal("queued PM spec content remained deliverable after Agent authorization was revoked")
	}
}
