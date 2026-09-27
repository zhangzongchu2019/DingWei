package m8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestConsoleSpecInferenceAssistSelectsSecondAgentAndTokenOwnsSignature(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	hub.consoleAuthEnabled = true
	db := hub.Repo.(*store.SQLite)
	ctx, now := context.Background(), time.Now()
	created := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", specBodyClaimingCreator(t, "Assist", "owner-b"), hub.HandleConsoleSpecs)
	if created.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", created.Code, created.Body.String())
	}
	var spec model.ConsoleSpec
	if err := json.Unmarshal(created.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "assist-human", PrincipalID: "owner-a", TokenHash: hashConsoleToken("human"), ExpiresAt: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	type credential struct {
		agent  model.ConsoleAgent
		secret string
		key    model.ServiceAPIKey
	}
	credentials := make([]credential, 0, 2)
	for _, id := range []string{"agent-a", "agent-b"} {
		secret, key, err := hub.IssueAPIKey(ctx, "assist-"+id, id)
		if err != nil {
			t.Fatal(err)
		}
		agent := model.ConsoleAgent{ID: id, KeyID: key.ID, SessionName: id + "-2642", OwnerPrincipal: "owner-a", Active: true}
		if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertConsoleAgentGrant(ctx, model.ConsoleAgentGrant{PrincipalID: "owner-a", AgentID: agent.ID, Permission: "interact", Active: true}); err != nil {
			t.Fatal(err)
		}
		hub.mu.Lock()
		if hub.sessionClients[agent.KeyID] == nil {
			hub.sessionClients[agent.KeyID] = map[string]*sessionClient{}
		}
		hub.sessionClients[agent.KeyID][agent.SessionName] = &sessionClient{keyID: agent.KeyID, sessionName: agent.SessionName}
		hub.mu.Unlock()
		credentials = append(credentials, credential{agent, secret, key})
	}
	var dispatched model.ConsoleAgent
	hub.specInferenceDispatch = func(_ context.Context, agent model.ConsoleAgent, _ model.Envelope) error {
		dispatched = agent
		return nil
	}
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+spec.ID+"/assist/inference", strings.NewReader(`{"agent_id":"agent-b"}`))
	req.SetPathValue("specID", spec.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	req = req.WithContext(context.WithValue(req.Context(), consoleSessionContextKey{}, model.ConsoleSession{ID: "assist-human", PrincipalID: "owner-a"}))
	rr := httptest.NewRecorder()
	hub.HandleConsoleSpecInferenceAssist(rr, req)
	if rr.Code != http.StatusAccepted || dispatched.ID != "agent-b" {
		t.Fatalf("second selected Agent not dispatched: code=%d agent=%q body=%s", rr.Code, dispatched.ID, rr.Body.String())
	}

	// The front end selected A, but the actual write capability belongs to B.
	const capB = "capability-b"
	if err := db.RotateConsolePageCapability(ctx, model.ConsolePageCapability{ID: "cap-b", SessionID: "assist-human", PrincipalID: "owner-a", AgentID: "agent-b", PageID: "console-spec:" + spec.ID, TokenHash: hashConsoleToken(capB), ExpiresAt: now.Add(time.Hour)}, credentials[1].agent.KeyID, credentials[1].agent.SessionName, now); err != nil {
		t.Fatal(err)
	}
	handler := hub.RequireConsoleSpecAgentCapability(func(r *http.Request) string { return "console-spec:" + r.PathValue("specID") }, hub.HandleConsoleSpecAgentInference)
	write := httptest.NewRequest(http.MethodPost, "/api/agent/console/specs/"+spec.ID+"/inferences", strings.NewReader(`{"text":"agent finding","claimed_agent_id":"agent-a"}`))
	write.SetPathValue("specID", spec.ID)
	write.Header.Set("X-DW-Agent-Key-ID", credentials[1].agent.KeyID)
	write.Header.Set("X-DW-Agent-Session", credentials[1].agent.SessionName)
	write.Header.Set("Authorization", "Bearer "+credentials[1].secret)
	write.Header.Set("X-DW-Console-Capability", capB)
	written := httptest.NewRecorder()
	handler(written, write)
	if written.Code != http.StatusCreated {
		t.Fatalf("Agent inference write code=%d body=%s", written.Code, written.Body.String())
	}
	revision, err := db.ConsoleSpecRevision(ctx, "owner-a", spec.ID, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	var bundle specAcceptanceBundle
	if err := json.Unmarshal([]byte(revision.AcceptanceJSON), &bundle); err != nil {
		t.Fatal(err)
	}
	if revision.CreatedBy != "agent-b" || revision.CreatedByKind != "agent" || len(bundle.Inferences) != 1 || bundle.Inferences[0].GeneratedBy != "agent-b" {
		t.Fatalf("dropdown claim overrode token identity: revision=%+v inferences=%+v", revision, bundle.Inferences)
	}
	t.Logf("selected dispatch=%s; claimed=agent-a; token creator=%s", dispatched.ID, revision.CreatedBy)
}
