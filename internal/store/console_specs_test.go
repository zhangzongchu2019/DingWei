package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func newConsoleSpecStore(t *testing.T) *SQLite {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "console-specs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.ConsolePrincipal{
		{ID: "owner-a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "oa", Active: true},
		{ID: "owner-b", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ob", Active: true},
		{ID: "manager", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "om", Active: true},
	} {
		if err := db.UpsertConsolePrincipal(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func specRevision(specID string, revision int, content string) model.ConsoleSpecRevision {
	sum := sha256.Sum256([]byte(content))
	return model.ConsoleSpecRevision{
		SpecID: specID, Revision: revision, ContentMarkdown: content,
		AcceptanceJSON:   `[{"id":"A1","text":"two specs remain independently editable"}]`,
		InputAnchorsJSON: `[{"kind":"owner_message","ref":"msg:20260920:1"}]`,
		ContentSHA256:    hex.EncodeToString(sum[:]), CreatedBy: "owner-a",
	}
}

func createSpec(t *testing.T, db *SQLite, id, title string, now time.Time) {
	t.Helper()
	capability := sha256.Sum256([]byte("edit:" + id))
	spec := model.ConsoleSpec{ID: id, PrincipalID: "owner-a", Title: title, EditCapabilityHash: hex.EncodeToString(capability[:])}
	if err := db.CreateConsoleSpec(context.Background(), spec, specRevision(id, 1, "# "+title), now); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleSpecsSupportMultipleIndependentSpecsWithPublicReadAndControlledWrite(t *testing.T) {
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	createSpec(t, db, "spec-a", "A", now)
	createSpec(t, db, "spec-b", "B", now.Add(time.Second))

	items, err := db.ListConsoleSpecs(context.Background(), "owner-a", false)
	if err != nil || len(items) != 2 {
		t.Fatalf("owner-a specs=%+v err=%v", items, err)
	}
	if got, err := db.ConsoleSpec(context.Background(), "owner-b", "spec-a", false); err != nil || got == nil || got.ID != "spec-a" {
		t.Fatalf("控写不控读:cross-owner read got=%+v err=%v", got, err)
	}
	if _, err := db.AppendConsoleSpecRevision(context.Background(), "owner-b", "spec-a", 1,
		specRevision("spec-a", 2, "stolen"), now.Add(2*time.Second)); err != ErrConsoleSpecNotFound {
		t.Fatalf("cross-owner write err=%v want ErrConsoleSpecNotFound", err)
	}
	after, err := db.ConsoleSpec(context.Background(), "owner-a", "spec-a", false)
	if err != nil || after == nil || after.CurrentRevision != 1 {
		t.Fatalf("cross-owner attempt changed state got=%+v err=%v", after, err)
	}
}

func TestConsoleSpecRevisionCASAndClaimedRevisionReturnsToNeedsImprovement(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC)
	createSpec(t, db, "spec-a", "A", now)
	if err := db.SubmitConsoleSpec(ctx, "owner-a", "spec-a", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "rh1", SpecID: "spec-a", Revision: 1, Stage: "hard_gate", Verdict: "pass", ReviewedBy: "manager"}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "ru1", SpecID: "spec-a", Revision: 1, Stage: "human_review", Verdict: "pass", ReviewedBy: "manager"}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimConsoleSpec(ctx, "spec-a", "manager", now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendConsoleSpecRevision(ctx, "owner-a", "spec-a", 1,
		specRevision("spec-a", 2, "# changed"), now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := db.ConsoleSpec(ctx, "owner-a", "spec-a", false)
	if err != nil || got == nil || got.State != "needs_improvement" || got.CurrentRevision != 2 || got.ClaimedBy != "" {
		t.Fatalf("new revision must clear current claim and return to pool got=%+v err=%v", got, err)
	}
	if _, err := db.AppendConsoleSpecRevision(ctx, "owner-a", "spec-a", 1,
		specRevision("spec-a", 3, "# stale tab"), now.Add(6*time.Second)); err != ErrConsoleSpecConflict {
		t.Fatalf("stale expected revision err=%v want conflict", err)
	}
	events, err := db.ListConsoleSpecEvents(ctx, "spec-a")
	if err != nil || len(events) == 0 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	foundClaim := false
	for _, event := range events {
		if event.Action == "claim" && event.Actor == "manager" && event.Revision == 1 {
			foundClaim = true
		}
	}
	if !foundClaim {
		t.Fatal("new revision erased the historical claim event")
	}
}

func TestConsoleSpecRequiresBothGatesOnTheSameRevisionAndReadyProposalTracksCurrentRevision(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	createSpec(t, db, "spec-a", "A", now)
	if err := db.SubmitConsoleSpec(ctx, "owner-a", "spec-a", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// A human PASS alone must not substitute for the hard gate.
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "u-alone", SpecID: "spec-a", Revision: 1, Stage: "human_review", Verdict: "pass", ReviewedBy: "manager"}, now.Add(2*time.Second)); err != ErrConsoleSpecGateOrder {
		t.Fatalf("human-only review err=%v want gate-order rejection", err)
	}
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "h1", SpecID: "spec-a", Revision: 1, Stage: "hard_gate", Verdict: "pass", ReviewedBy: "manager"}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if proposals, err := db.ListReadyConsoleTaskProposals(ctx); err != nil || len(proposals) != 0 {
		t.Fatalf("hard-only proposal leaked proposals=%+v err=%v", proposals, err)
	}
	if proposals, err := db.ListConsoleTaskProposals(ctx, "spec-a"); err != nil || len(proposals) != 0 {
		t.Fatalf("hard-only review created a hidden proposal proposals=%+v err=%v", proposals, err)
	}
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "u1", SpecID: "spec-a", Revision: 1, Stage: "human_review", Verdict: "pass", ReviewedBy: "manager"}, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	ready, err := db.ListReadyConsoleTaskProposals(ctx)
	if err != nil || len(ready) != 1 || ready[0].Revision != 1 {
		t.Fatalf("ready rev1=%+v err=%v", ready, err)
	}
	firstID := ready[0].ID
	// Retrying the same review cannot make a second proposal.
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "u1-retry", SpecID: "spec-a", Revision: 1, Stage: "human_review", Verdict: "pass", ReviewedBy: "manager"}, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	history, err := db.ListConsoleTaskProposals(ctx, "spec-a")
	if err != nil || len(history) != 1 || history[0].ID != firstID {
		t.Fatalf("proposal idempotency history=%+v err=%v", history, err)
	}
	if _, err := db.AppendConsoleSpecRevision(ctx, "owner-a", "spec-a", 1,
		specRevision("spec-a", 2, "# revision two"), now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if ready, err := db.ListReadyConsoleTaskProposals(ctx); err != nil || len(ready) != 0 {
		t.Fatalf("old revision remained ready after new revision ready=%+v err=%v", ready, err)
	}
	if err := db.SubmitConsoleSpec(ctx, "owner-a", "spec-a", now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	// rev1 hard PASS must not combine with rev2 human PASS.
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "u2", SpecID: "spec-a", Revision: 2, Stage: "human_review", Verdict: "pass", ReviewedBy: "manager"}, now.Add(8*time.Second)); err != ErrConsoleSpecGateOrder {
		t.Fatalf("cross-revision gate splice err=%v want rejection", err)
	}
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "h2", SpecID: "spec-a", Revision: 2, Stage: "hard_gate", Verdict: "pass", ReviewedBy: "manager"}, now.Add(9*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{ID: "u2-ok", SpecID: "spec-a", Revision: 2, Stage: "human_review", Verdict: "pass", ReviewedBy: "manager"}, now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	ready, err = db.ListReadyConsoleTaskProposals(ctx)
	if err != nil || len(ready) != 1 || ready[0].Revision != 2 {
		t.Fatalf("ready rev2=%+v err=%v", ready, err)
	}
	history, err = db.ListConsoleTaskProposals(ctx, "spec-a")
	if err != nil || len(history) != 2 {
		t.Fatalf("historical proposals must remain history=%+v err=%v", history, err)
	}
}

func TestConsoleSpecFailedReviewRequiresStructuredReasonCode(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC)
	createSpec(t, db, "spec-a", "A", now)
	if err := db.SubmitConsoleSpec(ctx, "owner-a", "spec-a", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, reasons := range []string{"", "[]", `[""]`, `{"not":"an-array"}`} {
		err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{
			ID: "fail-empty", SpecID: "spec-a", Revision: 1, Stage: "hard_gate", Verdict: "fail",
			ReasonCodes: reasons, ReviewedBy: "manager",
		}, now.Add(2*time.Second))
		if err == nil {
			t.Fatalf("empty or malformed reason codes %q were accepted", reasons)
		}
	}
	if err := db.ReviewConsoleSpec(ctx, model.ConsoleSpecReview{
		ID: "fail-reasoned", SpecID: "spec-a", Revision: 1, Stage: "hard_gate", Verdict: "fail",
		ReasonCodes: `["HARD_GATE_FAILED"]`, ReviewedBy: "manager",
	}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("structured failure was rejected: %v", err)
	}
	spec, err := db.ConsoleSpec(ctx, "owner-a", "spec-a", false)
	if err != nil || spec == nil || spec.State != "needs_improvement" {
		t.Fatalf("reasoned failure did not return spec to needs_improvement: spec=%+v err=%v", spec, err)
	}
}
