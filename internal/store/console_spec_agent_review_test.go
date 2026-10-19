package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"strings"
	"sync"
	"testing"
	"time"
)

func r8Fixture(t *testing.T, kind string, state ...string) (*SQLite, time.Time, *SpecAgentRequest, model.ConsoleAgent) {
	t.Helper()
	db := newConsoleSpecStore(t)
	now := time.Now().UTC()
	createSpec(t, db, "s", "owner quote", now)
	a := model.ConsoleAgent{ID: "reviewer", KeyID: "key", SessionName: "review", OwnerPrincipal: "owner-a", Active: true}
	if e := db.UpsertConsoleAgent(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	if len(state) > 0 && state[0] == "review" {
		if e := db.SubmitConsoleSpecReview(context.Background(), "s", "owner-a", now); e != nil {
			t.Fatal(e)
		}
		if e := db.SaveConsoleSpecGateReview(context.Background(), "s", "owner-a", model.ConsoleSpecGateReview{ReviewAgentID: a.ID, IssuesJSON: `[]`}, now); e != nil {
			t.Fatal(e)
		}
	}
	rev, e := db.ConsoleSpecRevision(context.Background(), "owner-a", "s", 1, false)
	if e != nil {
		t.Fatal(e)
	}
	req := SpecAgentRequest{ID: "specreview_one", ClientID: "one", SpecID: "s", AgentID: a.ID, Actor: "owner-a", Kind: kind, Revision: 1, SHA: rev.ContentSHA256, Boot: "boot"}
	q, e := db.StartSpecAgentRequest(context.Background(), req, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	return db, now, q, a
}
func r8Result(q *SpecAgentRequest) SpecAgentResult {
	r := SpecAgentResult{Revision: q.Revision, SHA: q.SHA}
	if q.Kind == "review" {
		r.Issues = []SpecAgentIssue{{"I1", "check owner quote"}}
	} else {
		r.Criteria = []SpecAgentCriterion{{ID: "A1", Text: "it works", OwnerRef: "owner quote", Cases: []string{"case1"}}}
	}
	return r
}
func TestR8PendingLockCoversAllStoreWrites(t *testing.T) {
	db, now, q, _ := r8Fixture(t, "review")
	ctx := context.Background()
	paths := map[string]func() error{
		"append": func() error {
			_, e := db.AppendConsoleSpecRevision(ctx, "owner-a", "s", 1, specRevision("s", 2, "changed"), now)
			return e
		},
		"open": func() error {
			_, e := db.OpenConsoleSpecRevision(ctx, "owner-a", "s", 1, specRevision("s", 2, "changed"), now)
			return e
		},
		"transfer": func() error { return db.TransferConsoleSpec(ctx, "s", "owner-a", "乙", "editing", now) },
		"archive":  func() error { return db.ArchiveConsoleSpec(ctx, "s", "owner-a", now) },
		"close":    func() error { return db.CloseConsoleSpecEditing(ctx, "owner-a", "s", now) },
		"review": func() error {
			return db.SaveConsoleSpecGateReview(ctx, "s", "owner-a", model.ConsoleSpecGateReview{ReviewAgentID: "reviewer", IssuesJSON: `[]`}, now)
		},
		"submit": func() error { return db.SubmitConsoleSpecReview(ctx, "s", "owner-a", now) },
	}
	for name, fn := range paths {
		if e := fn(); e == nil {
			t.Fatalf("R8_LOCK_BYPASSED path=%s", name)
		}
	}
	// Separate active request is rejected rather than silently replacing the lease.
	other := *q
	other.ID = "specreview_two"
	other.ClientID = "two"
	other.Kind = "criteria"
	if _, e := db.StartSpecAgentRequest(ctx, other, nil, now); e == nil {
		t.Fatal("R8_PARALLEL_REQUEST_ACCEPTED")
	}
	if e := db.CancelSpecAgentRequest(ctx, q.ID, "owner-a", now); e != nil {
		t.Fatal(e)
	}
	if _, e := db.AppendConsoleSpecRevision(ctx, "owner-a", "s", 1, specRevision("s", 2, "changed"), now); e != nil {
		t.Fatal("R8_CANCEL_LOCK_NOT_RELEASED", e)
	}
}
func TestR8ActualReviewerPendingHumanGateAndReplay(t *testing.T) {
	db, now, q, a := r8Fixture(t, "review")
	ctx := context.Background()
	r := r8Result(q)
	forged := a
	forged.KeyID = "forged"
	if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, forged, r, nil, now); e == nil {
		t.Fatal("R8_FORGED_CALLBACK_ACCEPTED")
	}
	if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r, nil, now); e != nil {
		t.Fatal(e)
	}
	gate, e := db.ConsoleSpecGateReview(ctx, "s", 1)
	if e != nil || gate == nil || gate.ReviewAgentID != a.ID || !strings.Contains(gate.IssuesJSON, `"pending"`) {
		t.Fatal("R8_PENDING_PROVENANCE_LOST", gate, e)
	}
	if e = db.FinalizeConsoleSpec(ctx, "s", "owner-a", now); e == nil {
		t.Fatal("R8_PENDING_FINALIZED")
	}
	same, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r, nil, now)
	if e != nil || same.Status != "completed" {
		t.Fatal("R8_REPLAY_NOT_IDEMPOTENT", e)
	}
	r.Issues[0].Text = "different"
	if _, e = db.CompleteSpecAgentRequest(ctx, q.ID, a, r, nil, now); e == nil {
		t.Fatal("R8_CHANGED_REPLAY_ACCEPTED")
	}
	gate.IssuesJSON = `[{"id":"I1","text":"forged issue","response":"checked","outcome":"false_positive"}]`
	if e = db.SaveConsoleSpecGateReview(ctx, "s", "owner-a", *gate, now); e == nil {
		t.Fatal("R8_HUMAN_REPLACED_ISSUE")
	}
	gate.IssuesJSON = `[{"id":"I1","text":"check owner quote","response":"checked","outcome":"false_positive"}]`
	if e = db.SaveConsoleSpecGateReview(ctx, "s", "owner-a", *gate, now); e != nil {
		t.Fatal(e)
	}
	if e = db.FinalizeConsoleSpec(ctx, "s", "owner-a", now); e != nil {
		t.Fatal(e)
	}
}
func TestR8CriteriaConfirmationFreshRevisionAndOldGateHistory(t *testing.T) {
	for _, state := range []string{"editing", "review"} {
		t.Run(state, func(t *testing.T) {
			db, now, q, a := r8Fixture(t, "criteria", state)
			ctx := context.Background()
			// The request snapshot remains unchanged until human confirmation.
			if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); e != nil {
				t.Fatal(e)
			}
			before, e := db.ConsoleSpec(ctx, "owner-a", "s", false)
			if e != nil || before.CurrentRevision != 1 {
				t.Fatal("R8_UNCONFIRMED_OVERWRITE", e)
			}
			if _, e = db.ConfirmSpecAgentCriteria(ctx, q.ID, "owner-b", 1, q.SHA, nil, now); e == nil {
				t.Fatal("R8_OTHER_HUMAN_CONFIRMED")
			}
			if _, e = db.ConfirmSpecAgentCriteria(ctx, q.ID, "owner-a", 2, q.SHA, nil, now); e == nil {
				t.Fatal("R8_STALE_CRITERIA_CONFIRMED")
			}
			next, e := db.ConfirmSpecAgentCriteria(ctx, q.ID, "owner-a", 1, q.SHA, nil, now)
			if e != nil || next != 2 {
				t.Fatal(e)
			}
			after, e := db.ConsoleSpec(ctx, "owner-a", "s", false)
			if e != nil || after.State != "editing" || after.CurrentRevision != 2 {
				t.Fatal("R8_CRITERIA_STATE_WRONG", after, e)
			}
			old, _ := db.ConsoleSpecRevision(ctx, "owner-a", "s", 1, false)
			fresh, _ := db.ConsoleSpecRevision(ctx, "owner-a", "s", 2, false)
			if old.ContentMarkdown != fresh.ContentMarkdown || old.AcceptanceJSON == fresh.AcceptanceJSON {
				t.Fatal("R8_REVISION_HISTORY_OVERWRITTEN")
			}
			gate, _ := db.ConsoleSpecGateReview(ctx, "s", 2)
			if gate != nil {
				t.Fatal("R8_OLD_GATE_INHERITED")
			}
			if e = db.SubmitConsoleSpecReview(ctx, "s", "owner-a", now); e != nil {
				t.Fatal(e)
			}
			// A manual fabricated empty gate cannot substitute for fresh authenticated review.
			if e = db.SaveConsoleSpecGateReview(ctx, "s", "owner-a", model.ConsoleSpecGateReview{ReviewAgentID: a.ID, IssuesJSON: `[{"id":"fake","text":"fabricated","response":"checked","outcome":"false_positive"}]`}, now); e != nil {
				t.Fatal(e)
			}
			if e = db.FinalizeConsoleSpec(ctx, "s", "owner-a", now); e == nil {
				t.Fatal("R8_FRESH_REVIEW_BYPASSED")
			}
			n, e := db.ConfirmSpecAgentCriteria(ctx, q.ID, "owner-a", 1, q.SHA, nil, now)
			if e != nil || n != 2 {
				t.Fatal("R8_CONFIRM_REPLAY_APPENDED", e)
			}
			if state == "review" {
				history, e := db.ConsoleSpecGateReview(ctx, "s", 1)
				if e != nil || history == nil {
					t.Fatal("R8_OLD_GATE_HISTORY_REMOVED", e)
				}
			}
			freshRequest := SpecAgentRequest{ID: "specreview_fresh", ClientID: "fresh", SpecID: "s", Actor: "owner-a", AgentID: a.ID, Kind: "review", Revision: 2, SHA: q.SHA, Boot: "boot"}
			q2, e := db.StartSpecAgentRequest(ctx, freshRequest, nil, now.Add(time.Second))
			if e != nil {
				t.Fatal(e)
			}
			result := r8Result(q2)
			result.Issues = []SpecAgentIssue{}
			if _, e = db.CompleteSpecAgentRequest(ctx, q2.ID, a, result, nil, now.Add(time.Second)); e != nil {
				t.Fatal(e)
			}
			if e = db.FinalizeConsoleSpec(ctx, "s", "owner-a", now); e == nil {
				t.Fatal("R8_EMPTY_FRESH_REVIEW_AUTO_FINALIZED")
			}
			if e = db.SaveConsoleSpecGateReview(ctx, "s", "owner-a", model.ConsoleSpecGateReview{ReviewAgentID: a.ID, IssuesJSON: `[]`}, now); e != nil {
				t.Fatal(e)
			}
			if e = db.FinalizeConsoleSpec(ctx, "s", "owner-a", now); e != nil {
				t.Fatal("R8_FRESH_REVIEW_FINALIZE_FAILED", e)
			}
			invalid := SpecAgentRequest{ID: "specreview_final", ClientID: "final", SpecID: "s", Actor: "owner-a", AgentID: a.ID, Kind: "criteria", Revision: 2, SHA: q.SHA, Boot: "boot"}
			if _, e = db.StartSpecAgentRequest(ctx, invalid, nil, now); e == nil {
				t.Fatal("R8_FINAL_CRITERIA_REQUEST_ACCEPTED")
			}

		})
	}
}
func TestR8CriteriaRejectsQuotesAndSpoofedConfirmation(t *testing.T) {
	for _, bad := range []string{"quote", "checked_by", "mixed", "revision", "sha"} {
		t.Run(bad, func(t *testing.T) {
			db, now, q, a := r8Fixture(t, "criteria")
			r := r8Result(q)
			switch bad {
			case "quote":
				r.Criteria[0].OwnerRef = "absent"
			case "checked_by":
				r.Criteria[0].CheckedBy = "human"
			case "mixed":
				r.Issues = []SpecAgentIssue{}
			case "revision":
				r.Revision++
			case "sha":
				r.SHA = "stale"
			}
			if _, e := db.CompleteSpecAgentRequest(context.Background(), q.ID, a, r, nil, now); e == nil {
				t.Fatal("R8_INVALID_CRITERIA_ACCEPTED", bad)
			}
		})
	}
}
func TestR8CancelTransferIsAtomicAndRejectsLateReply(t *testing.T) {
	for _, stage := range []string{"pending", "awaiting_confirmation"} {
		t.Run(stage, func(t *testing.T) {
			db, now, q, a := r8Fixture(t, "criteria")
			ctx := context.Background()
			if stage == "awaiting_confirmation" {
				if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); e != nil {
					t.Fatal(e)
				}
			}
			if e := db.CancelTransferSpecAgent(ctx, "s", "owner-a", "missing", "editing", q.ID, 1, q.SHA, now); e == nil {
				t.Fatal("invalid target")
			}
			still, _ := db.SpecAgentRequest(ctx, q.ID)
			if still.Status != stage {
				t.Fatal("R8_FAILED_TRANSFER_CANCELLED_REQUEST")
			}
			if e := db.CancelTransferSpecAgent(ctx, "s", "owner-a", "乙", "editing", q.ID, 1, q.SHA, now); e != nil {
				t.Fatal(e)
			}
			after, _ := db.ConsoleSpec(ctx, "owner-b", "s", false)
			if after.CurrentEditorID != "owner-b" {
				t.Fatal("R8_CANCEL_WITHOUT_TRANSFER")
			}
			if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); e == nil {
				t.Fatal("R8_LATE_REPLY_PERSISTED")
			}
			if _, e := db.ConfirmSpecAgentCriteria(ctx, q.ID, "owner-a", 1, q.SHA, nil, now); e == nil {
				t.Fatal("R8_OLD_EDITOR_CONFIRMED")
			}
			if _, e := db.AppendConsoleSpecRevision(ctx, "owner-b", "s", 1, func() model.ConsoleSpecRevision {
				r := specRevision("s", 2, "new owner")
				r.CreatedBy = "owner-b"
				return r
			}(), now); e != nil {
				t.Fatal("R8_TRANSFER_LOCK_NOT_RELEASED", e)
			}
		})
	}
}
func TestR8DeadlineRestartAndIdentityPrecommitFailClosed(t *testing.T) {
	for _, mode := range []string{"timeout", "restart", "precommit"} {
		t.Run(mode, func(t *testing.T) {
			db, now, q, a := r8Fixture(t, "review")
			ctx := context.Background()
			switch mode {
			case "timeout":
				if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now.Add(181*time.Second)); e == nil {
					t.Fatal("R8_TIMEOUT_REPLY_ACCEPTED")
				}
			case "restart":
				if e := db.ReconcileSpecAgentRequests(ctx, "newboot", now); e != nil {
					t.Fatal(e)
				}
				if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); e == nil {
					t.Fatal("R8_RESTART_REPLY_ACCEPTED")
				}
			case "precommit":
				if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), func() error { return errors.New("PG lost") }, now); e == nil {
					t.Fatal("R8_PRECOMMIT_ERROR_IGNORED")
				}
				gate, _ := db.ConsoleSpecGateReview(ctx, "s", 1)
				if gate != nil {
					t.Fatal("R8_FAILED_PRECOMMIT_LEFT_GATE")
				}
			}
		})
	}
}
func TestR8TransferReplyConcurrency(t *testing.T) {
	for _, kind := range []string{"review", "criteria"} {
		t.Run(kind, func(t *testing.T) {
			db, now, q, a := r8Fixture(t, kind)
			ctx := context.Background()
			var wg sync.WaitGroup
			start := make(chan struct{})
			var reply, transfer error
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, reply = db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now)
			}()
			go func() {
				defer wg.Done()
				<-start
				transfer = db.CancelTransferSpecAgent(ctx, "s", "owner-a", "乙", "editing", q.ID, 1, q.SHA, now)
			}()
			close(start)
			wg.Wait()
			row, _ := db.SpecAgentRequest(ctx, q.ID)
			gate, _ := db.ConsoleSpecGateReview(ctx, "s", 1)
			if kind == "review" && reply == nil && transfer == nil {
				t.Fatal("R8_REPLY_TRANSFER_BOTH_COMMITTED")
			}
			if row.Status == "cancelled" && kind == "review" && gate != nil {
				t.Fatal("R8_CANCELLED_REPLY_GATE_PRESENT")
			}
			if reply != nil && transfer != nil {
				t.Fatalf("both failed: %v %v", reply, transfer)
			}
		})
	}
}
func TestR8EmptyIssueListStillRequiresHuman(t *testing.T) {
	db, now, q, a := r8Fixture(t, "review")
	r := r8Result(q)
	r.Issues = []SpecAgentIssue{}
	if _, e := db.CompleteSpecAgentRequest(context.Background(), q.ID, a, r, nil, now); e != nil {
		t.Fatal(e)
	}
	var human int
	if e := db.db.QueryRow(`SELECT human_confirmed FROM console_spec_agent_gate WHERE spec_id='s'`).Scan(&human); e != nil || human != 0 {
		t.Fatal("R8_EMPTY_ISSUES_AUTO_CONFIRMED")
	}
	raw, _ := json.Marshal(r)
	if len(raw) == 0 {
		t.Fatal("missing")
	}
}

func TestR8CancelledSameEditorLateReply(t *testing.T) {
	db, now, q, a := r8Fixture(t, "review")
	ctx := context.Background()
	if e := db.CancelSpecAgentRequest(ctx, q.ID, "owner-a", now); e != nil {
		t.Fatal(e)
	}
	if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); e == nil {
		t.Fatal("R8_CANCELLED_LATE_RESULT_ACCEPTED")
	}
	gate, _ := db.ConsoleSpecGateReview(ctx, "s", 1)
	if gate != nil {
		t.Fatal("R8_CANCELLED_LATE_RESULT_PERSISTED")
	}
}
func TestR8CriteriaCandidateRequiresConfirmationStatus(t *testing.T) {
	db, now, q, a := r8Fixture(t, "criteria")
	ctx := context.Background()
	out, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now)
	if e != nil {
		t.Fatal(e)
	}
	if out.Status != "awaiting_confirmation" {
		t.Fatal("R8_CANDIDATE_NOT_PENDING_CONFIRMATION")
	}
	spec, _ := db.ConsoleSpec(ctx, "owner-a", "s", false)
	if spec.CurrentRevision != 1 {
		t.Fatal("R8_UNCONFIRMED_OVERWRITE")
	}
}
func TestR8FreshReviewRequirementFollowsHumanRevision(t *testing.T) {
	db, now, q, a := r8Fixture(t, "criteria")
	ctx := context.Background()
	if _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ConfirmSpecAgentCriteria(ctx, q.ID, "owner-a", 1, q.SHA, nil, now); e != nil {
		t.Fatal(e)
	}
	old, e := db.ConsoleSpecRevision(ctx, "owner-a", "s", 2, false)
	if e != nil {
		t.Fatal(e)
	}
	next := *old
	next.Revision = 3
	next.CreatedBy = "owner-a"
	if _, e = db.AppendConsoleSpecRevision(ctx, "owner-a", "s", 2, next, now); e != nil {
		t.Fatal(e)
	}
	if e = db.SubmitConsoleSpecReview(ctx, "s", "owner-a", now); e != nil {
		t.Fatal(e)
	}
	if e = db.SaveConsoleSpecGateReview(ctx, "s", "owner-a", model.ConsoleSpecGateReview{ReviewAgentID: a.ID, IssuesJSON: `[{"id":"fake","text":"fabricated","response":"checked","outcome":"false_positive"}]`}, now); e != nil {
		t.Fatal(e)
	}
	if e = db.FinalizeConsoleSpec(ctx, "s", "owner-a", now); e == nil {
		t.Fatal("R8_FRESH_REVIEW_LOST_ON_EDIT")
	}
}
