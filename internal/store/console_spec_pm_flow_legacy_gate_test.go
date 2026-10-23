package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func insertPMSpecFlowStateForTest(t *testing.T, db *SQLite, specID, state string, revision int, sha string, now time.Time) {
	t.Helper()
	if _, err := db.db.ExecContext(context.Background(), `INSERT INTO console_spec_flow(spec_id,original_editor_id,review_recipient_id,state,frozen_revision,frozen_sha256,created_at,updated_at) VALUES(?, 'owner-a','owner-b',?,?,?,?,?)`, specID, state, revision, sha, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}

func makeFinalSplittingSpec(t *testing.T, db *SQLite, id string, now time.Time) string {
	t.Helper()
	createSpec(t, db, id, id, now)
	if _, err := db.db.ExecContext(context.Background(), `UPDATE console_spec SET state='final',current_editor_id=NULL,task_state='splitting' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	var sha string
	if err := db.db.QueryRowContext(context.Background(), `SELECT content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=1`, id).Scan(&sha); err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestPMSpecPresplitGateIsOptInAndRequiresFinalReviewedRevision(t *testing.T) {
	ctx := context.Background()
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	agent := model.ConsoleAgent{ID: "split-agent", KeyID: "split-key", SessionName: "split-agent", OwnerPrincipal: "owner-a", Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	sha := makeFinalSplittingSpec(t, db, "split-review-incomplete", now)
	insertPMSpecFlowStateForTest(t, db, "split-review-incomplete", "review_complete", 1, sha, now)
	req := model.ConsoleSpecPresplitRequest{ID: "legacy-off", SpecID: "split-review-incomplete", AgentID: agent.ID, DeadlineAt: now.Add(time.Minute), CreatedAt: now}
	if _, _, err := db.StartConsoleSpecPresplitWithPMSpecFlow(ctx, req, "owner-a", true); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("enabled presplit accepted non-final PM flow: %v", err)
	}
	if _, err := db.ConsoleSpecPresplit(ctx, req.SpecID, "owner-a", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	} else if state, _ := db.ConsoleSpecPresplit(ctx, req.SpecID, "owner-a", now, now.Add(time.Hour)); state != nil {
		t.Fatalf("rejected enabled start created request: %+v", state)
	}
	// The old entry point still behaves as before with the feature disabled.
	if _, _, err := db.StartConsoleSpecPresplit(ctx, req, "owner-a"); err != nil {
		t.Fatalf("feature-off legacy presplit changed: %v", err)
	}

	sha = makeFinalSplittingSpec(t, db, "split-finalized", now.Add(time.Second))
	insertPMSpecFlowStateForTest(t, db, "split-finalized", "final", 1, sha, now)
	req = model.ConsoleSpecPresplitRequest{ID: "finalized-start", SpecID: "split-finalized", AgentID: agent.ID, DeadlineAt: now.Add(time.Minute), CreatedAt: now}
	if _, _, err := db.StartConsoleSpecPresplitWithPMSpecFlow(ctx, req, "owner-a", true); err != nil {
		t.Fatalf("enabled presplit rejected reviewed finalized revision: %v", err)
	}
}

func TestPMSpecPresplitRejectsUnregisteredLegacyFinalSpecOnlyWhenEnabled(t *testing.T) {
	ctx := context.Background()
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC)
	spec := "legacy-final-no-flow"
	makeFinalSplittingSpec(t, db, spec, now)
	agent := model.ConsoleAgent{ID: "legacy-split-agent", KeyID: "legacy-split-key", SessionName: "legacy-split", OwnerPrincipal: "owner-a", Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	req := model.ConsoleSpecPresplitRequest{ID: "legacy-no-flow-request", SpecID: spec, AgentID: agent.ID, DeadlineAt: now.Add(time.Minute), CreatedAt: now}
	if _, _, err := db.StartConsoleSpecPresplitWithPMSpecFlow(ctx, req, "owner-a", true); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("enabled presplit accepted unregistered legacy final spec: %v", err)
	}
	var n int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_presplit_request WHERE spec_id=?`, spec).Scan(&n); err != nil || n != 0 {
		t.Fatalf("denied legacy presplit created requests=%d err=%v", n, err)
	}
	// Use the same spec and request to prove denial left no conflicting row and
	// that the explicit feature-off path still supports existing final specs.
	revision, selected, err := db.StartConsoleSpecPresplitWithPMSpecFlow(ctx, req, "owner-a", false)
	if err != nil {
		t.Fatalf("feature-off legacy presplit changed: %v", err)
	}
	if revision == nil || revision.SpecID != spec || selected == nil || selected.ID != agent.ID {
		t.Fatalf("feature-off legacy presplit returned wrong inputs: revision=%+v agent=%+v", revision, selected)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_presplit_request WHERE spec_id=?`, spec).Scan(&n); err != nil || n != 1 {
		t.Fatalf("feature-off legacy presplit requests=%d err=%v", n, err)
	}
}

func createCandidateForPMSpecTest(t *testing.T, db *SQLite, id, flowState string, now time.Time) (string, []ConsoleSpecPresplitTask, string) {
	t.Helper()
	ctx := context.Background()
	sha := makeFinalSplittingSpec(t, db, id, now)
	agent := model.ConsoleAgent{ID: "candidate-agent-" + id, KeyID: "key-" + id, SessionName: "session-" + id, OwnerPrincipal: "owner-a", Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	tasks := []ConsoleSpecPresplitTask{{ID: "task-" + id, Title: "task", Content: "content", DefinitionOfDone: "done", EstimatedHours: 1, DependsOn: []string{}}}
	raw, err := json.Marshal(tasks)
	if err != nil {
		t.Fatal(err)
	}
	stamp := now.Format(time.RFC3339Nano)
	if _, err = db.db.ExecContext(ctx, `INSERT INTO console_spec_presplit_request(id,spec_id,revision,agent_id,owner_principal,status,failure_reason,deadline_at,created_at,completed_at) VALUES(?,?,1,?,'owner-a','completed','',?,?,?)`, "request-"+id, id, agent.ID, now.Add(time.Hour).Format(time.RFC3339Nano), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, `INSERT INTO console_task_candidate(request_id,tasks_json,version) VALUES(?,?,1)`, "request-"+id, string(raw)); err != nil {
		t.Fatal(err)
	}
	// Empty flowState represents a legacy spec that was never enrolled.
	if flowState != "" {
		insertPMSpecFlowStateForTest(t, db, id, flowState, 1, sha, now)
	}
	return "request-" + id, tasks, sha
}

func TestPMSpecCandidateConfirmRejectsUnregisteredLegacyFinalSpecOnlyWhenEnabled(t *testing.T) {
	ctx := context.Background()
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 10, 23, 1, 1, 0, 0, time.UTC)
	spec := "legacy-candidate-no-flow"
	request, tasks, _ := createCandidateForPMSpecTest(t, db, spec, "", now)
	rechecks := 0
	recheck := func() error { rechecks++; return nil }
	if err := db.ConfirmConsoleTaskCandidatesWithPMSpecFlow(ctx, request, "owner-a", 1, 1, tasks, recheck, now, true); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("enabled candidate confirmation accepted unregistered legacy final spec: %v", err)
	}
	var n int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_task WHERE spec_id=?`, spec).Scan(&n); err != nil || n != 0 {
		t.Fatalf("denied legacy confirmation created tasks=%d err=%v", n, err)
	}
	var confirmed bool
	if err := db.db.QueryRowContext(ctx, `SELECT confirmed_at IS NOT NULL FROM console_task_candidate WHERE request_id=?`, request).Scan(&confirmed); err != nil || confirmed || rechecks != 0 {
		t.Fatalf("denied legacy confirmation mutated candidate=%v rechecks=%d err=%v", confirmed, rechecks, err)
	}
	// The same candidate remains confirmable when the feature is disabled.
	if err := db.ConfirmConsoleTaskCandidatesWithPMSpecFlow(ctx, request, "owner-a", 1, 1, tasks, recheck, now, false); err != nil {
		t.Fatalf("feature-off legacy confirmation changed: %v", err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_task WHERE spec_id=?`, spec).Scan(&n); err != nil || n != len(tasks) {
		t.Fatalf("feature-off legacy confirmation tasks=%d err=%v", n, err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT confirmed_at IS NOT NULL FROM console_task_candidate WHERE request_id=?`, request).Scan(&confirmed); err != nil || !confirmed || rechecks != 1 {
		t.Fatalf("feature-off legacy confirmation candidate=%v rechecks=%d err=%v", confirmed, rechecks, err)
	}
}

func TestPMSpecSplitGateWithoutMigrationTableKeepsLegacyBehavior(t *testing.T) {
	ctx := context.Background()
	db := newConsoleSpecStore(t)
	if _, err := db.db.ExecContext(ctx, `ALTER TABLE console_spec_flow RENAME TO console_spec_flow_not_migrated`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, enabled := range []bool{false, true} {
		if err := requireFinalizedPMSpecForSplit(ctx, tx, enabled, "legacy-no-migration", 1); err != nil {
			t.Fatalf("missing migration table changed legacy behavior enabled=%v: %v", enabled, err)
		}
	}
}

func TestPMSpecCandidateConfirmUsesSameOptInFinalizationGate(t *testing.T) {
	ctx := context.Background()
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 10, 22, 14, 30, 0, 0, time.UTC)
	request, tasks, _ := createCandidateForPMSpecTest(t, db, "candidate-review", "review_complete", now)
	if err := db.ConfirmConsoleTaskCandidatesWithPMSpecFlow(ctx, request, "owner-a", 1, 1, tasks, func() error { return nil }, now, true); !errors.Is(err, ErrPMSpecFlowDenied) {
		t.Fatalf("enabled candidate confirmation accepted non-final PM flow: %v", err)
	}
	var n int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_task WHERE spec_id='candidate-review'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rejected confirmation created tasks=%d err=%v", n, err)
	}
	// Feature-off uses the original entry point and retains legacy behavior.
	if err := db.ConfirmConsoleTaskCandidates(ctx, request, "owner-a", 1, 1, tasks, func() error { return nil }, now); err != nil {
		t.Fatalf("feature-off legacy confirmation changed: %v", err)
	}

	request, tasks, _ = createCandidateForPMSpecTest(t, db, "candidate-final", "final", now.Add(time.Second))
	if err := db.ConfirmConsoleTaskCandidatesWithPMSpecFlow(ctx, request, "owner-a", 1, 1, tasks, func() error { return nil }, now, true); err != nil {
		t.Fatalf("enabled candidate confirmation rejected finalized PM flow: %v", err)
	}
}

func TestPMSpecFinalizeRejectsTransferredFlowBeforeReviewCompletion(t *testing.T) {
	ctx := context.Background()
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	createSpec(t, db, "finalize-transfer-gate", "legacy accepted fixture", now)
	if err := db.TransferConsoleSpec(ctx, "finalize-transfer-gate", "owner-a", "乙", "review", now); err != nil {
		t.Fatal(err)
	}
	var sha string
	if err := db.db.QueryRowContext(ctx, `SELECT content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=1`, "finalize-transfer-gate").Scan(&sha); err != nil {
		t.Fatal(err)
	}
	insertPMSpecFlowStateForTest(t, db, "finalize-transfer-gate", "review", 1, sha, now)
	agent := model.ConsoleAgent{ID: "finalize-review-agent", KeyID: "finalize-review-key", SessionName: "finalize-review", OwnerPrincipal: "owner-a", Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `INSERT INTO console_spec_gate_review(id,spec_id,revision,review_agent_id,operator_principal,issues_json,created_at,updated_at) VALUES('gate','finalize-transfer-gate',1,?,'owner-b',?,?,?)`, agent.ID, `[{"id":"I1","text":"reviewed","response":"ok","outcome":"false_positive"}]`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := db.FinalizeConsoleSpec(ctx, "finalize-transfer-gate", "owner-b", now); !errors.Is(err, ErrConsoleSpecGate) {
		t.Fatalf("matching frozen revision finalized before PM review completion: %v", err)
	}
	var state string
	if err := db.db.QueryRowContext(ctx, `SELECT state FROM console_spec WHERE id=?`, "finalize-transfer-gate").Scan(&state); err != nil || state != "review" {
		t.Fatalf("rejected finalize changed state=%s err=%v", state, err)
	}
}
