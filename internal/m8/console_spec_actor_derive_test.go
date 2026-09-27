package m8

import (
	"bytes"
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

func specBodyClaimingCreator(t *testing.T, title, claimed string) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(validSpecBody), &body); err != nil {
		t.Fatal(err)
	}
	body["title"] = title
	body["created_by"] = claimed
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestConsoleSpecAgentCapabilityAttestsActorAndRejectsPublicIdentity(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	hub.consoleAuthEnabled = true
	db := hub.Repo.(*store.SQLite)
	ctx := context.Background()
	now := time.Now()
	agent := model.ConsoleAgent{ID: "agent-real", KeyID: "pending", SessionName: "agent-real-2642", OwnerPrincipal: "owner-a", Active: true}
	secret, key, err := hub.IssueAPIKey(ctx, "session-helper", "spec-agent")
	if err != nil {
		t.Fatal(err)
	}
	agent.KeyID = key.ID
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsoleAgentGrant(ctx, model.ConsoleAgentGrant{PrincipalID: "owner-a", AgentID: agent.ID, Permission: "interact", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "human-session", PrincipalID: "owner-a", TokenHash: hashConsoleToken("human-login"), ExpiresAt: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	const capabilityRaw = "human-issued-agent-capability"
	if err := db.RotateConsolePageCapability(ctx, model.ConsolePageCapability{ID: "cap-spec-create", SessionID: "human-session", PrincipalID: "owner-a", AgentID: agent.ID, PageID: "console-specs:new", TokenHash: hashConsoleToken(capabilityRaw), ExpiresAt: now.Add(time.Hour)}, key.ID, agent.SessionName, now); err != nil {
		t.Fatal(err)
	}

	handler := hub.RequireConsoleSpecAgentCapability(func(*http.Request) string { return "console-specs:new" }, hub.HandleConsoleSpecs)
	body := specBodyClaimingCreator(t, "Agent Spec", "agent-forged")
	request := func(includeServiceSecret, includeHumanCapability bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/console/specs", strings.NewReader(body))
		req.Header.Set("X-DW-Agent-Key-ID", key.ID)
		req.Header.Set("X-DW-Agent-Session", agent.SessionName)
		if includeServiceSecret {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
		if includeHumanCapability {
			req.Header.Set("X-DW-Console-Capability", capabilityRaw)
		}
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}
	if public := request(false, false); public.Code != http.StatusForbidden {
		t.Fatalf("public key/session/agent identity wrote spec: code=%d body=%s", public.Code, public.Body.String())
	}
	if noHumanGrant := request(true, false); noHumanGrant.Code != http.StatusForbidden {
		t.Fatalf("service credential without human capability wrote spec: code=%d body=%s", noHumanGrant.Code, noHumanGrant.Body.String())
	}
	created := request(true, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("attested Agent create code=%d body=%s", created.Code, created.Body.String())
	}
	var spec model.ConsoleSpec
	if err := json.Unmarshal(created.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	revision, err := db.ConsoleSpecRevision(ctx, "owner-a", spec.ID, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if revision.CreatedBy != agent.ID || revision.CreatedByKind != "agent" {
		t.Fatalf("creator followed request claim instead of token: %+v", revision)
	}
	var bundle specAcceptanceBundle
	if err := json.Unmarshal([]byte(revision.AcceptanceJSON), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Criteria[0].GeneratedBy != agent.ID || bundle.Criteria[0].Source != "ai" {
		t.Fatalf("generated_by not server-attested: %+v", bundle.Criteria[0])
	}
	t.Logf("public identity code=403; capability creator=%s kind=%s; forged claim ignored", revision.CreatedBy, revision.CreatedByKind)
}

func TestConsoleSpecHumanActorParticipantsAndDerivePreserveFinalSource(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	created := callConsoleSpec(t, hub, "owner-a", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", specBodyClaimingCreator(t, "Human Spec", "owner-b"), hub.HandleConsoleSpecs)
	if created.Code != http.StatusCreated {
		t.Fatalf("human create code=%d body=%s", created.Code, created.Body.String())
	}
	var source model.ConsoleSpec
	if err := json.Unmarshal(created.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	first, err := db.ConsoleSpecRevision(context.Background(), "owner-a", source.ID, 1, false)
	if err != nil || first.CreatedBy != "owner-a" || first.CreatedByKind != "principal" {
		t.Fatalf("human creator trusted body claim: revision=%+v err=%v", first, err)
	}
	if err := db.TransferConsoleSpec(context.Background(), source.ID, "owner-a", "乙", "editing", hub.now()); err != nil {
		t.Fatal(err)
	}
	second := *first
	second.Revision = 2
	second.CreatedBy = "owner-b"
	second.CreatedByKind = "principal"
	if _, err := db.AppendConsoleSpecRevision(context.Background(), "owner-b", source.ID, 1, second, hub.now()); err != nil {
		t.Fatal(err)
	}
	attribution, err := db.ConsoleSpecAttribution(context.Background(), source.ID)
	if err != nil || attribution.Creator != "甲" || len(attribution.Participants) != 1 || attribution.Participants[0] != "乙" {
		t.Fatalf("attribution=%+v err=%v", attribution, err)
	}
	if err := db.TransferConsoleSpec(context.Background(), source.ID, "owner-b", "丙", "review", hub.now()); err != nil {
		t.Fatal(err)
	}
	completeSpecGateForTest(t, db, source.ID, "manager", hub.now())
	if err := db.FinalizeConsoleSpec(context.Background(), source.ID, "manager", hub.now()); err != nil {
		t.Fatal(err)
	}
	before, _ := db.ConsoleSpecRevision(context.Background(), "owner-a", source.ID, 2, false)
	req := httptest.NewRequest(http.MethodPost, "/api/console/specs/"+source.ID+"/derive", bytes.NewReader(nil))
	req.SetPathValue("specID", source.ID)
	req = req.WithContext(context.WithValue(req.Context(), consolePrincipalContextKey{}, "owner-a"))
	rr := httptest.NewRecorder()
	hub.HandleConsoleSpecDerive(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("derive code=%d body=%s", rr.Code, rr.Body.String())
	}
	var derived struct {
		Spec model.ConsoleSpec `json:"spec"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &derived); err != nil {
		t.Fatal(err)
	}
	if derived.Spec.DerivedFrom != source.ID {
		t.Fatalf("derived_from=%q want %q", derived.Spec.DerivedFrom, source.ID)
	}
	after, _ := db.ConsoleSpecRevision(context.Background(), "owner-a", source.ID, 2, false)
	if before.ContentSHA256 != after.ContentSHA256 || before.ContentMarkdown != after.ContentMarkdown {
		t.Fatalf("derive mutated final source: before=%s after=%s", before.ContentSHA256, after.ContentSHA256)
	}
	t.Logf("creator=%s participants=%v derived_from=%s source_sha=%s", attribution.Creator, attribution.Participants, derived.Spec.DerivedFrom, after.ContentSHA256)
}
