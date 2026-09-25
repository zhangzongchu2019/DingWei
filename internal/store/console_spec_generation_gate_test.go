package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestConsoleSpecFinalVerificationRequiresAllThreePartsAndAtLeastOneCase(t *testing.T) {
	base := `{"criteria":[{"owner_ref":"owner:q","source":"owner","checked_by":"checker","checked_at":"2026-09-25T01:02:03Z","cases":["case-a","case-b"]}]}`
	if !validSpecFinalAcceptance(base) {
		t.Fatal("complete verification rejected")
	}
	for _, broken := range []string{
		strings.Replace(base, `"checked_by":"checker"`, `"checked_by":""`, 1),
		strings.Replace(base, `"checked_at":"2026-09-25T01:02:03Z"`, `"checked_at":""`, 1),
		strings.Replace(base, `"cases":["case-a","case-b"]`, `"cases":[]`, 1),
	} {
		if validSpecFinalAcceptance(broken) {
			t.Fatalf("incomplete verification accepted: %s", broken)
		}
	}
}

func TestConsoleSpecGateReviewRequiresAtLeastOneIssue(t *testing.T) {
	for _, raw := range []string{`[]`, `null`} {
		if validCompletedSpecGateReview(raw) {
			t.Fatalf("empty issue list accepted: %s", raw)
		}
	}
	if !validCompletedSpecGateReview(`[{"id":"Q1","text":"疑点","response":"已答","outcome":"false_positive"}]`) {
		t.Fatal("completed non-empty issue list rejected")
	}
}

func TestConsoleSpecReviewAgentLookupFailureIsNotReportedAsInvalidAgent(t *testing.T) {
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 9, 25, 16, 52, 0, 0, time.UTC)
	createSpec(t, db, "lookup-failure", "Lookup failure", now)
	if _, err := db.db.ExecContext(context.Background(), `UPDATE console_spec SET state='review',current_editor_id='manager' WHERE id='lookup-failure'; ALTER TABLE console_agent RENAME TO console_agent_unavailable`); err != nil {
		t.Fatal(err)
	}
	err := db.SaveConsoleSpecGateReview(context.Background(), "lookup-failure", "manager", model.ConsoleSpecGateReview{
		ReviewAgentID: "reviewer",
		IssuesJSON:    `[{"id":"Q1","text":"疑点"}]`,
	}, now)
	if !errors.Is(err, ErrConsoleSpecReviewerLookup) {
		t.Fatalf("lookup failure err=%v want ErrConsoleSpecReviewerLookup", err)
	}
	if errors.Is(err, ErrConsoleSpecReviewerInvalid) {
		t.Fatalf("lookup failure collapsed into invalid reviewer: %v", err)
	}
}

func TestConsoleSpecFinalIsTheExclusiveRevisionRejectionReason(t *testing.T) {
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 9, 25, 16, 25, 0, 0, time.UTC)
	createSpec(t, db, "final-only", "Final", now)
	// The schema normally couples final with current_editor_id=NULL. Temporarily
	// disable CHECK enforcement only in this isolated fixture so every other arm
	// is deliberately valid: actor is the current editor, archive/closed are 0,
	// expected revision is exact. The sole rejecting fact is state=final.
	if _, err := db.db.ExecContext(context.Background(), `PRAGMA ignore_check_constraints=ON; UPDATE console_spec SET state='final',current_editor_id='owner-a',archived=0,editing_closed=0 WHERE id='final-only'`); err != nil {
		t.Fatal(err)
	}
	revision := specRevision("final-only", 2, "attempt after final")
	revision.CreatedBy = "owner-a"
	_, err := db.AppendConsoleSpecRevision(context.Background(), "owner-a", "final-only", 1, revision, now)
	if !errors.Is(err, ErrConsoleSpecFinal) {
		t.Fatalf("post-final revision err=%v want ErrConsoleSpecFinal", err)
	}
	final, err := db.ConsoleSpec(context.Background(), "owner-a", "final-only", false)
	if err != nil || final.State != "final" || final.CurrentRevision != 1 {
		t.Fatalf("final mutated=%+v err=%v", final, err)
	}
}
