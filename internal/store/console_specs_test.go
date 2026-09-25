package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
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
		{ID: "owner-a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "oa", DisplayName: "甲", Active: true},
		{ID: "owner-b", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ob", DisplayName: "乙", Active: true},
		{ID: "manager", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "om", DisplayName: "丙", Active: true},
	} {
		if err := db.UpsertConsolePrincipal(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertConsolePrincipalRole(context.Background(), model.ConsolePrincipalRole{PrincipalID: p.ID, Role: "owner", OwnerKey: "tenant-a", Active: true}); err != nil {
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
		specRevision("spec-a", 2, "stolen"), now.Add(2*time.Second)); err != ErrConsoleSpecEditDenied {
		t.Fatalf("cross-owner write err=%v want ErrConsoleSpecEditDenied", err)
	}
	after, err := db.ConsoleSpec(context.Background(), "owner-a", "spec-a", false)
	if err != nil || after == nil || after.CurrentRevision != 1 {
		t.Fatalf("cross-owner attempt changed state got=%+v err=%v", after, err)
	}
}

// Legacy dual-gate tests were replaced by TestConsoleSpecTwelveStepFlowSingleEditorFinalAndOwnerOnlyTasks.

func TestConsoleTaskPlanningRejectsSelfMissingAndCycles(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC)
	createSpec(t, db, "spec-a", "A", now)
	createSpec(t, db, "spec-b", "B", now)
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec SET state='final',current_editor_id=NULL`); err != nil {
		t.Fatal(err)
	}
	stamp := now.Format(time.RFC3339Nano)
	for _, id := range []string{"a", "b"} {
		specID := "spec-" + id
		reviewID := "review-" + id
		if _, err := db.db.ExecContext(ctx, `INSERT INTO console_spec_review(id,spec_id,revision,stage,verdict,reason_codes_json,detail,reviewed_by,reviewed_at) VALUES(?,?,1,'human_review','pass','[]','',?,?)`, reviewID, specID, "manager", stamp); err != nil {
			t.Fatal(err)
		}
		if _, err := db.db.ExecContext(ctx, `INSERT INTO console_task_proposal(id,schema_version,spec_id,revision,content_sha256,title,acceptance_json,input_anchors_json,review_id,reviewed_by,reviewed_at,created_at) VALUES(?,1,?,1,?,'task `+id+`','[]','[]',?,'manager',?,?)`, "task-"+id, specID, strings.Repeat(id, 64), reviewID, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpdateConsoleTaskProposalPlan(ctx, "task-a", "owner-a", false, "content", "planned", 3, []string{"task-a"}, "[]"); err == nil {
		t.Fatal("self dependency accepted")
	}
	if err := db.UpdateConsoleTaskProposalPlan(ctx, "task-a", "owner-a", false, "content", "planned", 3, []string{"not-frozen"}, "[]"); err == nil {
		t.Fatal("missing/unfrozen cross-spec dependency accepted")
	}
	if err := db.UpdateConsoleTaskProposalPlan(ctx, "task-a", "owner-b", false, "content", "planned", 3, nil, "[]"); err == nil {
		t.Fatal("foreign owner updated task plan")
	}
	if err := db.UpdateConsoleTaskProposalPlan(ctx, "task-a", "owner-a", false, "content", "planned", 16, []string{"task-b"}, `[{"id":"DOD","text":"complete"}]`); err != nil {
		t.Fatalf("frozen cross-spec dependency rejected: %v", err)
	}
	if err := db.UpdateConsoleTaskProposalPlan(ctx, "task-b", "owner-a", false, "content", "planned", 2, []string{"task-a"}, "[]"); err == nil {
		t.Fatal("dependency cycle accepted")
	}
	tasks, err := db.ListConsoleTaskProposals(ctx, "spec-a")
	if err != nil || len(tasks) != 1 || tasks[0].EstimatedHours != 16 || tasks[0].DependsOnJSON != `["task-b"]` || !strings.Contains(tasks[0].AcceptanceJSON, "complete") {
		t.Fatalf("plan not persisted tasks=%+v err=%v", tasks, err)
	}
}

func TestConsoleTaskSplitCreatesAndLocksCurrentRevision(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	createSpec(t, db, "spec-split", "Split", now)
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec SET state='final',current_editor_id=NULL WHERE id='spec-split'`); err != nil {
		t.Fatal(err)
	}
	p := model.ConsoleTaskProposal{ID: "task-split", SpecID: "spec-split", Title: "Implement", TaskContent: "Do it", Status: "planned", EstimatedHours: 3, DependsOnJSON: "[]", ReviewID: "manual", ReviewedBy: "owner-a", ReviewedAt: now, CreatedAt: now}
	if err := db.CreateConsoleTaskProposal(ctx, p, "owner-a", false); err != nil {
		t.Fatal(err)
	}
	tasks, err := db.ListConsoleTaskProposals(ctx, "spec-split")
	if err != nil || len(tasks) != 1 || tasks[0].Revision != 1 || tasks[0].EstimatedHours != 3 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	if _, err := db.AppendConsoleSpecRevision(ctx, "owner-a", "spec-split", 1, specRevision("spec-split", 2, "changed"), now.Add(time.Second)); !errors.Is(err, ErrConsoleSpecFinal) {
		t.Fatalf("finalized spec must reject edits, err=%v", err)
	}
	if err := db.CreateConsoleTaskProposal(ctx, model.ConsoleTaskProposal{ID: "bad", SpecID: "spec-split", Title: "Bad", TaskContent: "Bad", Status: "planned", DependsOnJSON: `["missing"]`, ReviewedAt: now, CreatedAt: now}, "owner-a", false); err == nil {
		t.Fatal("missing dependency must be rejected")
	}
}
