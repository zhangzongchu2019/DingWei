package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func pmDraftFixture(id, body string, version int, sha string) PMSpecDraftInput {
	return PMSpecDraftInput{SpecID: id, Title: "试点 spec", ContentMarkdown: body, AcceptanceJSON: `{"criteria":[]}`, InputAnchorsJSON: `[]`, SourceRefsJSON: `[]`, NormAnchorsJSON: `[]`, ExpectedVersion: version, ExpectedSHA256: sha}
}

func TestPMSpecWorkingDraftCASPreservesBothConflictPayloadsAndCreatesNoRevision(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	draft, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-new", "first", 0, ""), now)
	if err != nil || draft == nil || draft.Version != 1 {
		t.Fatalf("first save: draft=%+v err=%v", draft, err)
	}
	if got, err := db.ConsoleSpec(ctx, "owner-a", "pm-new", false); err != nil || got != nil {
		t.Fatalf("autosave created immutable spec: got=%+v err=%v", got, err)
	}
	updated, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-new", "server bytes", 1, draft.ContentSHA256), now.Add(time.Second))
	if err != nil || updated.Version != 2 {
		t.Fatalf("second save: draft=%+v err=%v", updated, err)
	}
	_, err = db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-new", "client bytes", 1, draft.ContentSHA256), now.Add(2*time.Second))
	if !errors.Is(err, ErrPMSpecFlowConflict) {
		t.Fatalf("stale CAS err=%v", err)
	}
	var submitted, current string
	if err = db.db.QueryRowContext(ctx, `SELECT submitted_json,current_json FROM console_spec_working_draft_conflict WHERE spec_id='pm-new'`).Scan(&submitted, &current); err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(submitted)) || !json.Valid([]byte(current)) {
		t.Fatal("conflict did not retain both valid payloads")
	}
	if !strings.Contains(submitted, "client bytes") || !strings.Contains(current, "server bytes") {
		t.Fatalf("conflict bytes missing: submitted=%s current=%s", submitted, current)
	}
	var revisions int
	if err = db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_revision WHERE spec_id='pm-new'`).Scan(&revisions); err != nil || revisions != 0 {
		t.Fatalf("revision count=%d err=%v", revisions, err)
	}
}

func TestPMSpecTransferFreezesDraftAtomicallyAndBlocksLegacyWritesAndFinalize(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)
	draft, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-transfer", "approved body", 0, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	gotRevision, err := db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "transfer-1", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecTransferReview, TargetPrincipal: "owner-b"}, now.Add(time.Second))
	if err != nil || gotRevision != 1 {
		t.Fatalf("freeze transfer revision=%d err=%v", gotRevision, err)
	}
	spec, err := db.ConsoleSpec(ctx, "owner-a", "pm-transfer", false)
	if err != nil || spec == nil || spec.CurrentEditorID != "owner-b" || spec.State != "review" {
		t.Fatalf("transferred spec=%+v err=%v", spec, err)
	}
	rev, err := db.ConsoleSpecRevision(ctx, "owner-a", "pm-transfer", 1, false)
	if err != nil || rev == nil || rev.ContentMarkdown != "approved body" || rev.SourceRefsJSON != "[]" {
		t.Fatalf("frozen revision=%+v err=%v", rev, err)
	}
	if _, err = db.AppendConsoleSpecRevision(ctx, "owner-b", "pm-transfer", 1, specRevision("pm-transfer", 2, "legacy write"), now.Add(2*time.Second)); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("legacy revision write err=%v", err)
	}
	if err = db.FinalizeConsoleSpec(ctx, "pm-transfer", "owner-b", now.Add(3*time.Second)); !errors.Is(err, ErrConsoleSpecGate) {
		t.Fatalf("recipient finalized without completion: %v", err)
	}
	if err = db.SubmitConsoleSpecReview(ctx, "pm-transfer", "owner-b", now.Add(4*time.Second)); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("legacy review route err=%v", err)
	}
	if err = db.CloseConsoleSpecEditing(ctx, "owner-a", "pm-transfer", now.Add(5*time.Second)); err == nil {
		t.Fatal("legacy close-editing route changed a PM-flow spec")
	}
	if err = db.ReopenConsoleSpecEditing(ctx, "owner-a", "pm-transfer", now.Add(6*time.Second)); err == nil {
		t.Fatal("legacy reopen-editing route changed a PM-flow spec")
	}
	proposal := model.ConsoleTaskProposal{ID: "task-before-review", SpecID: "pm-transfer", Title: "task", TaskContent: "content", Status: "planned", DependsOnJSON: `[]`, CreatedAt: now}
	if err = db.CreateConsoleTaskProposal(ctx, proposal, "owner-a", false); !errors.Is(err, ErrConsoleSpecGate) {
		t.Fatalf("task proposal before PM review completion err=%v", err)
	}
}

func TestPMSpecFreezeOnlyAllowsThreeExplicitActions(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: "pm-action-agent", KeyID: "pm-action-key", SessionName: "pm-action-agent", OwnerPrincipal: "owner-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	draft, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-actions", "body", 0, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StartPMSpecAssist(ctx, draft.SpecID, "owner-a", "pm-action-agent", "valid-empty-run", "boot-actions", draft.Version, draft.ContentSHA256, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = db.CompletePMSpecAssist(ctx, "valid-empty-run", "pm-action-agent", json.RawMessage(`{"suggestions":[],"review_questions":[]}`), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "arbitrary-with-valid-assist", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: "arbitrary", AssistRequestID: "valid-empty-run"}, now.Add(3*time.Second)); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("arbitrary freeze action err=%v", err)
	}
	var revisions, eventsBefore, eventsAfter int
	if err = db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_revision WHERE spec_id=?`, draft.SpecID).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if err = db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_flow_event WHERE spec_id=?`, draft.SpecID).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatalf("rejected action created immutable revision: revisions=%d", revisions)
	}
	if err = db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_flow_event WHERE spec_id=?`, draft.SpecID).Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if eventsAfter != eventsBefore {
		t.Fatalf("rejected action appended workflow event: before=%d after=%d", eventsBefore, eventsAfter)
	}
	if _, err = db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "stale", ExpectedVersion: draft.Version + 1, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecAcceptSuggestions}, now); !errors.Is(err, ErrPMSpecFlowConflict) {
		t.Fatalf("stale freeze err=%v", err)
	}
}

func TestPMSpecReviewCompletionRequiresFreshAssistAndEveryDecisionThenExistingFinalGateRemains(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: "pm-review-agent", KeyID: "pm-key", SessionName: "pm-review-agent", OwnerPrincipal: "owner-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	draftInput := pmDraftFixture("pm-self-review", "# Review me", 0, "")
	draftInput.AcceptanceJSON = `{"criteria":[{"owner_ref":"REQ-1","source":"owner","checked_by":"owner-a","checked_at":"2026-10-22T15:00:00Z","cases":["accept/reject"]}]}`
	draft, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", draftInput, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StartPMSpecAssist(ctx, draft.SpecID, "owner-a", "pm-review-agent", "run-1", "boot-1", draft.Version, draft.ContentSHA256, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = db.CompletePMSpecAssist(ctx, "run-1", "pm-review-agent", json.RawMessage(`{"suggestions":[{"id":"S1","kind":"acceptance","payload":{"text":"require valid input"}}],"review_questions":[{"id":"I1","question":"Is this safe?"}]}`), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "complete-1", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecCompleteReview, AssistRequestID: "run-1"}, now.Add(3*time.Second)); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("completion without decision err=%v", err)
	}
	if err = db.DecidePMSpecAssist(ctx, "run-1", "owner-a", []PMSpecFlowDecision{{ItemID: "I1", Outcome: "reject", Reason: "not applicable"}}, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "complete-1", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecCompleteReview, AssistRequestID: "run-1"}, now.Add(4*time.Second)); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("completion after partial saved decision err=%v", err)
	}
	if err = db.DecidePMSpecAssist(ctx, "run-1", "owner-a", []PMSpecFlowDecision{{ItemID: "S1", Outcome: "reject"}}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("partial decision completion: %v", err)
	}
	if err = db.DecidePMSpecAssist(ctx, "run-1", "owner-a", []PMSpecFlowDecision{{ItemID: "I1", Outcome: "reject", Reason: "not applicable"}}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("exact decision retry was not idempotent: %v", err)
	}
	if err = db.DecidePMSpecAssist(ctx, "run-1", "owner-a", []PMSpecFlowDecision{{ItemID: "I1", Outcome: "accept"}}, now.Add(4*time.Second)); !errors.Is(err, ErrPMSpecFlowConflict) {
		t.Fatalf("changed decision retry err=%v", err)
	}
	revision, err := db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "complete-1", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecCompleteReview, AssistRequestID: "run-1"}, now.Add(5*time.Second))
	if err != nil || revision != 1 {
		t.Fatalf("complete review revision=%d err=%v", revision, err)
	}
	if err = db.FinalizeConsoleSpec(ctx, draft.SpecID, "owner-a", now.Add(6*time.Second)); !errors.Is(err, ErrConsoleSpecGate) {
		t.Fatalf("existing template/final gate was bypassed or not enforced: %v", err)
	}
	var flowState string
	if err = db.db.QueryRowContext(ctx, `SELECT state FROM console_spec_flow WHERE spec_id=?`, draft.SpecID).Scan(&flowState); err != nil || flowState != "review_complete" {
		t.Fatalf("flow state=%s err=%v", flowState, err)
	}
}

func TestPMSpecAssistRejectsMalformedStructuredSuggestionPayloads(t *testing.T) {
	tests := []struct {
		name string
		kind string
		body string
	}{
		{name: "acceptance missing text", kind: "acceptance", body: `{"criteria":[]}`},
		{name: "source missing uri", kind: "source_ref", body: `{"title":"source"}`},
		{name: "source relative uri", kind: "source_ref", body: `{"uri":"docs/spec.md"}`},
		{name: "norm missing section", kind: "norm_anchor", body: `{"standard":"DW","version":"3"}`},
		{name: "payload not object", kind: "acceptance", body: `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"suggestions":[{"id":"x","kind":"` + tt.kind + `","payload":` + tt.body + `}],"review_questions":[]}`
			if _, err := decodePMSpecAssistResult([]byte(raw)); !errors.Is(err, ErrSpecAgentInvalid) {
				t.Fatalf("malformed suggestion accepted: err=%v", err)
			}
		})
	}
}

func TestPMSpecAcceptSuggestionsAppliesSelectedStructureAndFreezesOnce(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 16, 0, 0, 0, time.UTC)
	if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: "pm-agent", KeyID: "pm-key", SessionName: "pm-agent", OwnerPrincipal: "owner-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	draft, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-accept", "body", 0, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StartPMSpecAssist(ctx, draft.SpecID, "owner-a", "pm-agent", "accept-run", "boot", draft.Version, draft.ContentSHA256, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	result := json.RawMessage(`{"suggestions":[{"id":"a1","kind":"acceptance","payload":{"text":"must reject invalid input"}},{"id":"s1","kind":"source_ref","payload":{"uri":"https://example.test/spec","title":"Source"}},{"id":"n1","kind":"norm_anchor","payload":{"standard":"DW","version":"3","section":"4.1"}}],"review_questions":[{"id":"q1","question":"Which role owns this?"}]}`)
	if err = db.CompletePMSpecAssist(ctx, "accept-run", "pm-agent", result, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	decisions := []PMSpecFlowDecision{{ItemID: "a1", Outcome: "accept"}, {ItemID: "s1", Outcome: "accept"}, {ItemID: "n1", Outcome: "reject"}, {ItemID: "q1", Outcome: "reject", Reason: "already covered"}}
	if err = db.DecidePMSpecAssist(ctx, "accept-run", "owner-a", decisions, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	revision, err := db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "accept-1", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecAcceptSuggestions, AssistRequestID: "accept-run"}, now.Add(4*time.Second))
	if err != nil || revision != 1 {
		t.Fatalf("revision=%d err=%v", revision, err)
	}
	stored, err := db.ConsoleSpecRevision(ctx, "owner-a", draft.SpecID, 1, false)
	if err != nil || stored == nil {
		t.Fatalf("revision=%+v err=%v", stored, err)
	}
	var acceptance struct {
		Criteria []map[string]any `json:"criteria"`
	}
	if err = json.Unmarshal([]byte(stored.AcceptanceJSON), &acceptance); err != nil || len(acceptance.Criteria) != 1 || acceptance.Criteria[0]["text"] != "must reject invalid input" {
		t.Fatalf("accepted criteria=%s err=%v", stored.AcceptanceJSON, err)
	}
	var refs []map[string]any
	if err = json.Unmarshal([]byte(stored.SourceRefsJSON), &refs); err != nil || len(refs) != 1 {
		t.Fatalf("source refs=%s err=%v", stored.SourceRefsJSON, err)
	}
	var anchors []any
	if err = json.Unmarshal([]byte(stored.NormAnchorsJSON), &anchors); err != nil || len(anchors) != 0 {
		t.Fatalf("rejected norm anchor applied: %s", stored.NormAnchorsJSON)
	}
	updated, err := db.GetPMSpecWorkingDraft(ctx, draft.SpecID)
	if err != nil || updated.Version != draft.Version+1 || updated.ContentSHA256 == draft.ContentSHA256 || updated.BaseRevision != 1 {
		t.Fatalf("updated draft=%+v err=%v", updated, err)
	}
	if replay, err := db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "accept-1", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecAcceptSuggestions, AssistRequestID: "accept-run"}, now.Add(5*time.Second)); err != nil || replay != revision {
		t.Fatalf("idempotent replay revision=%d err=%v", replay, err)
	}
}

func TestPMSpecAssistResultStalesAfterWorkingDraftEdit(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)
	if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: "pm-stale-agent", KeyID: "pm-stale-key", SessionName: "pm-stale-agent", OwnerPrincipal: "owner-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	draft, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-stale-assist", "original", 0, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StartPMSpecAssist(ctx, draft.SpecID, "owner-a", "pm-stale-agent", "stale-run", "boot-stale", draft.Version, draft.ContentSHA256, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = db.CompletePMSpecAssist(ctx, "stale-run", "pm-stale-agent", json.RawMessage(`{"suggestions":[{"id":"S1","kind":"acceptance","payload":{"text":"must validate"}}],"review_questions":[]}`), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = db.DecidePMSpecAssist(ctx, "stale-run", "owner-a", []PMSpecFlowDecision{{ItemID: "S1", Outcome: "accept"}}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	updated, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture(draft.SpecID, "edited after assist", draft.Version, draft.ContentSHA256), now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "stale-freeze", ExpectedVersion: updated.Version, ExpectedSHA256: updated.ContentSHA256, Action: PMSpecAcceptSuggestions, AssistRequestID: "stale-run"}, now.Add(5*time.Second))
	if !errors.Is(err, ErrPMSpecFlowStale) {
		t.Fatalf("assist result for old working draft was not rejected as stale: %v", err)
	}
	var revisions int
	if queryErr := db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_revision WHERE spec_id=?`, draft.SpecID).Scan(&revisions); queryErr != nil || revisions != 0 {
		t.Fatalf("stale assist created revision count=%d err=%v", revisions, queryErr)
	}
}

func TestPMSpecReviewCompletionBlocksIncompleteAcceptanceBeforeFreeze(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)
	if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: "pm-agent-gate", KeyID: "pm-key", SessionName: "pm-agent-gate", OwnerPrincipal: "owner-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	draft, err := db.SavePMSpecWorkingDraft(ctx, "owner-a", pmDraftFixture("pm-acceptance-gate", "body", 0, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StartPMSpecAssist(ctx, draft.SpecID, "owner-a", "pm-agent-gate", "gate-run", "boot", draft.Version, draft.ContentSHA256, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = db.CompletePMSpecAssist(ctx, "gate-run", "pm-agent-gate", json.RawMessage(`{"suggestions":[],"review_questions":[]}`), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = db.FreezePMSpecWorkingDraft(ctx, draft.SpecID, "owner-a", PMSpecFreezeInput{OperationID: "gate-complete", ExpectedVersion: draft.Version, ExpectedSHA256: draft.ContentSHA256, Action: PMSpecCompleteReview, AssistRequestID: "gate-run"}, now.Add(3*time.Second))
	if !errors.Is(err, ErrConsoleSpecGate) {
		t.Fatalf("incomplete acceptance passed review completion: %v", err)
	}
	var revisions int
	if err = db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_revision WHERE spec_id=?`, draft.SpecID).Scan(&revisions); err != nil || revisions != 0 {
		t.Fatalf("blocked completion froze %d revisions, err=%v", revisions, err)
	}
}
