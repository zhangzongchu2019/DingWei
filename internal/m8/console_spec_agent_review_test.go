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

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func r8HTTP(e *shimEnv, method, spec, kind, request, action, credential, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/console/specs/"+spec+"/"+kind, strings.NewReader(body))
	r.SetPathValue("specID", spec)
	r.SetPathValue("kind", kind)
	r.SetPathValue("requestID", request)
	r.SetPathValue("action", action)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", npOrigin)
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
	}
	rr := httptest.NewRecorder()
	if kind == "agent" {
		e.hub.RequireAgentTaskFlow(e.hub.HandleAgentSpecReview)(rr, r)
	} else if action != "" {
		e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandleConsoleSpecAgentConfirm))(rr, r)
	} else {
		e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandleConsoleSpecAgentRequest))(rr, r)
	}
	return rr
}
func r8HTTPFixture(t *testing.T) (*shimEnv, model.ConsoleSpec, *model.ConsoleSpecRevision, string) {
	t.Helper()
	e := newShimEnv(t)
	st, b := e.do("POST", "/api/console/specs", e.tokZZC, "", "", validSpecBody)
	if st != 201 {
		t.Fatal(st, b)
	}
	var spec model.ConsoleSpec
	if json.Unmarshal([]byte(b), &spec) != nil {
		t.Fatal(b)
	}
	rev, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", spec.ID, 1, false)
	if err != nil || rev == nil {
		t.Fatal(err)
	}
	a, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	return e, spec, rev, a.ID
}
func TestR8HTTPRealReliableReviewAndHumanOnlyFinalize(t *testing.T) {
	e, spec, rev, agent := r8HTTPFixture(t)
	body := fmt.Sprintf(`{"agent_id":%q,"expected_revision":1,"content_sha256":%q,"client_request_id":"review-one"}`, agent, rev.ContentSHA256)
	if rr := r8HTTP(e, "POST", spec.ID, "agent-review", "", "", "", e.tokZZC, body); rr.Code != 409 || !strings.Contains(rr.Body.String(), "agent_offline") {
		t.Fatalf("R8_OFFLINE_NOT_EXPLICIT: %d %s", rr.Code, rr.Body.String())
	}
	conn, st, _ := e.dial(e.zKimiCred, "")
	if st != 101 {
		t.Fatal(st)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	capability := func(enabled bool) {
		t.Helper()
		if err := writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, "spec_review_v1": enabled}}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			yes, cap := e.hub.specAgentCapable(AgentKeyID("zzc"), "kimi")
			if yes && cap == enabled {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("capability not observed")
	}
	capability(false)
	if rr := r8HTTP(e, "POST", spec.ID, "agent-review", "", "", "", e.tokZZC, body); rr.Code != 409 || !strings.Contains(rr.Body.String(), "helper_upgrade_required") {
		t.Fatalf("R8_OLD_HELPER_REQUEST_SENT: %d %s", rr.Code, rr.Body.String())
	}
	capability(true)
	rr := r8HTTP(e, "POST", spec.ID, "agent-review", "", "", "", e.tokZZC, body)
	if rr.Code != 202 {
		t.Fatalf("start %d %s", rr.Code, rr.Body.String())
	}
	var q store.SpecAgentRequest
	if err := json.Unmarshal(rr.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	for {
		env := readEnvelopeIncludingSystem(t, ctx, conn)
		if specReviewEnvelopeType(env) != "console_spec_review_request" {
			continue
		}
		if metaString(env.Meta, "type") != "delivery" || metaString(env.Meta, "delivery_id") == "" {
			t.Fatal("R8_REQUEST_NOT_RELIABLE")
		}
		if !strings.Contains(env.Body, rev.ContentSHA256) || !strings.Contains(env.Body, "acceptance") {
			t.Fatal("R8_REQUEST_SNAPSHOT_MISSING")
		}
		break
	}
	listed := r8HTTP(e, "GET", "", "agent", "", "", e.zKimiCred, "", "")
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), q.ID) {
		t.Fatalf("list %d %s", listed.Code, listed.Body.String())
	}
	mixed := r8HTTP(e, "GET", "", "agent", "", "", e.zKimiCred, e.tokZZC, "")
	if mixed.Code != 401 {
		t.Fatal("R8_MIXED_AUTH_ACCEPTED", mixed.Code)
	}
	result := fmt.Sprintf(`{"revision":1,"content_sha256":%q,"issues":[{"id":"I1","text":"review issue"}]}`, rev.ContentSHA256)
	if forged := r8HTTP(e, "POST", "", "agent", q.ID, "", e.lKimiCr, "", result); forged.Code != 403 {
		t.Fatal("R8_FORGED_HTTP_CALLBACK_ACCEPTED", forged.Code)
	}
	if got := r8HTTP(e, "POST", "", "agent", q.ID, "", e.zKimiCred, "", result); got.Code != 200 {
		t.Fatalf("reply %d %s", got.Code, got.Body.String())
	}
	gate, err := e.db.ConsoleSpecGateReview(e.ctx, spec.ID, 1)
	if err != nil || gate == nil || gate.ReviewAgentID != agent || !strings.Contains(gate.IssuesJSON, "pending") {
		t.Fatal("R8_HTTP_PENDING_SOURCE_WRONG", gate, err)
	}
	req := httptest.NewRequest("POST", "/api/console/specs/"+spec.ID+"/workflow/finalize", nil)
	req.SetPathValue("specID", spec.ID)
	req.SetPathValue("action", "finalize")
	req.Header.Set("Authorization", "Bearer "+e.zKimiCred)
	denied := httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandleConsoleSpecWorkflow))(denied, req)
	if denied.Code != 401 && denied.Code != 403 {
		t.Fatal("R8_AGENT_FINALIZED")
	}
	gate.IssuesJSON = `[{"id":"I1","text":"review issue","response":"checked","outcome":"false_positive"}]`
	if err = e.db.SaveConsoleSpecGateReview(e.ctx, spec.ID, "zzc", *gate, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	if err = e.db.FinalizeConsoleSpec(e.ctx, spec.ID, "zzc", e.hub.now()); err != nil {
		t.Fatal(err)
	}
}
func TestR8HTTPAgentCannotReadAfterCancelAndCrossOwnerRejected(t *testing.T) {
	e, spec, rev, agent := r8HTTPFixture(t)
	ctx := e.ctx
	// Store request creation here isolates current-role/read authorization from transport.
	q, err := e.db.StartSpecAgentRequest(ctx, store.SpecAgentRequest{ID: "specreview_read", SpecID: spec.ID, Actor: "zzc", AgentID: agent, Kind: "criteria", Revision: 1, SHA: rev.ContentSHA256, ClientID: "read", Boot: e.hub.specAgentBoot()}, nil, e.hub.now())
	if err != nil {
		t.Fatal(err)
	}
	if rr := r8HTTP(e, "GET", "", "agent", "", "", e.zKimiCred, "", ""); rr.Code != 200 || !strings.Contains(rr.Body.String(), q.ID) {
		t.Fatal("R8_VALID_AGENT_READ_DENIED", rr.Code, rr.Body.String())
	}
	if rr := r8HTTP(e, "GET", "", "agent", "", "", e.lKimiCr, "", ""); rr.Code != 200 || strings.Contains(rr.Body.String(), q.ID) {
		t.Fatal("R8_OTHER_AGENT_READ_LEAK", rr.Code, rr.Body.String())
	}
	if err = e.db.CancelSpecAgentRequest(ctx, q.ID, "zzc", e.hub.now()); err != nil {
		t.Fatal(err)
	}
	if rr := r8HTTP(e, "GET", "", "agent", "", "", e.zKimiCred, "", ""); rr.Code != 200 || strings.Contains(rr.Body.String(), q.ID) {
		t.Fatal("R8_CANCELLED_SNAPSHOT_LEAK", rr.Code, rr.Body.String())
	}
	foreign, err := e.svc.Store.FindAgentByName(ctx, "lbl", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"agent_id":%q,"expected_revision":1,"content_sha256":%q,"client_request_id":"cross-owner"}`, foreign.ID, rev.ContentSHA256)
	if rr := r8HTTP(e, "POST", spec.ID, "agent-criteria", "", "", "", e.tokZZC, body); rr.Code != 403 {
		t.Fatal("R8_CROSS_OWNER_AGENT_SELECTED", rr.Code, rr.Body.String())
	}
	if err = e.hub.RouteEnvelope(ctx, model.Envelope{Meta: map[string]any{"spec_review_type": "console_spec_review_request", "type": "delivery"}}); err == nil {
		t.Fatal("R8_FORGED_SYSTEM_REQUEST_ACCEPTED")
	}
}

type r8ReadRepository struct {
	*store.SQLite
	afterList func()
	readError bool
}

func (r *r8ReadRepository) ListSpecAgentRequests(ctx context.Context, a, b, s, c string) ([]store.SpecAgentRequest, error) {
	rows, e := r.SQLite.ListSpecAgentRequests(ctx, a, b, s, c)
	if r.afterList != nil {
		r.afterList()
	}
	return rows, e
}
func (r *r8ReadRepository) SpecAgentRequest(ctx context.Context, id string) (*store.SpecAgentRequest, error) {
	q, e := r.SQLite.SpecAgentRequest(ctx, id)
	if r.readError && e == nil {
		return q, fmt.Errorf("injected state read error")
	}
	return q, e
}
func TestR8HTTPReadErrorsAndCancellationBetweenSelectionAndProjection(t *testing.T) {
	for _, mode := range []string{"error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			e, spec, rev, agent := r8HTTPFixture(t)
			q, err := e.db.StartSpecAgentRequest(e.ctx, store.SpecAgentRequest{ID: "specreview_race", SpecID: spec.ID, Actor: "zzc", AgentID: agent, Kind: "criteria", Revision: 1, SHA: rev.ContentSHA256, ClientID: "race", Boot: e.hub.specAgentBoot()}, nil, e.hub.now())
			if err != nil {
				t.Fatal(err)
			}
			wrapped := &r8ReadRepository{SQLite: e.db, readError: mode == "error"}
			if mode == "cancel" {
				wrapped.afterList = func() {
					wrapped.afterList = nil
					if err := e.db.CancelSpecAgentRequest(e.ctx, q.ID, "zzc", e.hub.now()); err != nil {
						t.Fatal(err)
					}
				}
			}
			e.hub.Repo = wrapped
			got := r8HTTP(e, "GET", "", "agent", "", "", e.zKimiCred, "", "")
			if strings.Contains(got.Body.String(), q.ID) {
				t.Fatal("R8_READ_RACE_LEAKED_REQUEST", mode, got.Body.String())
			}
			if mode == "error" && got.Code != 503 {
				t.Fatal("R8_STATE_ERROR_NOT_FAIL_CLOSED", got.Code, got.Body.String())
			}
			if mode == "cancel" && got.Code != 200 {
				t.Fatal(got.Code, got.Body.String())
			}
		})
	}
}

func TestR8WSCallbackUsesActualAuthenticatedEndpoint(t *testing.T) {
	e, spec, rev, agent := r8HTTPFixture(t)
	q, err := e.db.StartSpecAgentRequest(e.ctx, store.SpecAgentRequest{ID: "specreview_ws", SpecID: spec.ID, Actor: "zzc", AgentID: agent, Kind: "review", Revision: 1, SHA: rev.ContentSHA256, ClientID: "ws", Boot: e.hub.specAgentBoot()}, nil, e.hub.now())
	if err != nil {
		t.Fatal(err)
	}
	result := fmt.Sprintf(`{"revision":1,"content_sha256":%q,"issues":[]}`, rev.ContentSHA256)
	env := model.Envelope{Body: result, Meta: map[string]any{"type": "console_spec_review_result", "request_id": q.ID}}
	// The sessionClient represents the identity established by Hub WS admission;
	// no Body/From field is treated as evidence of that identity.
	if !e.hub.handleSpecAgentReviewResult(e.ctx, &sessionClient{keyID: AgentKeyID("lbl"), sessionName: "kimi"}, env) {
		t.Fatal("not handled")
	}
	current, err := e.db.SpecAgentRequest(e.ctx, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "pending" {
		t.Fatal("R8_FORGED_WS_CALLBACK_ACCEPTED")
	}
	if !e.hub.handleSpecAgentReviewResult(e.ctx, &sessionClient{keyID: AgentKeyID("zzc"), sessionName: "kimi"}, env) {
		t.Fatal("not handled")
	}
	current, err = e.db.SpecAgentRequest(e.ctx, q.ID)
	if err != nil || current.Status != "completed" {
		t.Fatal("R8_AUTHENTIC_WS_CALLBACK_REJECTED", err)
	}
}

func TestR8RestartBeforeReconcilerRejectsWSAndCriteriaConfirmation(t *testing.T) {
	for _, kind := range []string{"review", "criteria"} {
		t.Run(kind, func(t *testing.T) {
			e, spec, rev, agent := r8HTTPFixture(t)
			q, err := e.db.StartSpecAgentRequest(e.ctx, store.SpecAgentRequest{ID: "specreview_oldboot", SpecID: spec.ID, Actor: "zzc", AgentID: agent, Kind: kind, Revision: 1, SHA: rev.ContentSHA256, ClientID: "oldboot", Boot: "previous-Hub-boot"}, nil, e.hub.now())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "review" {
				env := model.Envelope{Body: fmt.Sprintf(`{"revision":1,"content_sha256":%q,"issues":[]}`, rev.ContentSHA256), Meta: map[string]any{"type": "console_spec_review_result", "request_id": q.ID}}
				e.hub.handleSpecAgentReviewResult(e.ctx, &sessionClient{keyID: AgentKeyID("zzc"), sessionName: "kimi"}, env)
				row, _ := e.db.SpecAgentRequest(e.ctx, q.ID)
				if row.Status == "completed" {
					t.Fatal("R8_RESTARTED_WS_RESULT_ACCEPTED")
				}
			} else {
				a := model.ConsoleAgent{ID: agent, KeyID: AgentKeyID("zzc"), SessionName: "kimi", OwnerPrincipal: "zzc", Active: true}
				result := store.SpecAgentResult{Revision: 1, SHA: rev.ContentSHA256, Criteria: []store.SpecAgentCriterion{{ID: "A1", Text: "works", OwnerRef: "msg:1", Cases: []string{"case1"}}}}
				if _, err = e.db.CompleteSpecAgentRequest(e.ctx, q.ID, a, result, nil, e.hub.now()); err != nil {
					t.Fatal(err)
				}
				body := fmt.Sprintf(`{"expected_revision":1,"content_sha256":%q}`, rev.ContentSHA256)
				got := r8HTTP(e, "POST", spec.ID, "agent-criteria", q.ID, "confirm", "", e.tokZZC, body)
				if got.Code != 409 {
					t.Fatal("R8_RESTARTED_CANDIDATE_CONFIRMED", got.Code, got.Body.String())
				}
			}
		})
	}
}
