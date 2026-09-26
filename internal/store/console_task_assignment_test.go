package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestConsoleSpecTaskAssigneesPersistNullableAgentReferences(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)
	createSpec(t, db, "assignment-spec", "Assignment", now)
	if _, err := db.db.ExecContext(ctx, `UPDATE console_spec SET state='final',current_editor_id=NULL WHERE id='assignment-spec'`); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []model.ConsoleAgent{
		{ID: "developer-agent", KeyID: "dev-key", SessionName: "developer-session", OwnerPrincipal: "owner-a", Active: true},
		{ID: "reviewer-agent", KeyID: "review-key", SessionName: "reviewer-session", OwnerPrincipal: "owner-a", Active: true},
	} {
		if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
			t.Fatal(err)
		}
	}
	proposal := taskAcceptanceProposal("assignment-task", "assignment-spec", `[{"id":"DOD","text":"done"}]`, now)
	if err := db.CreateConsoleTaskProposal(ctx, proposal, "owner-a", false); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, proposal.ID, "owner-a", "developer-agent", "reviewer-agent"); err != nil {
		t.Fatalf("valid agent assignment rejected: %v", err)
	}
	tasks, err := db.ListConsoleTaskProposals(ctx, proposal.SpecID)
	if err != nil || len(tasks) != 1 || tasks[0].DeveloperAgentID != "developer-agent" || tasks[0].DeveloperAgent != "developer-session" || tasks[0].ReviewerAgentID != "reviewer-agent" || tasks[0].ReviewerAgent != "reviewer-session" {
		t.Fatalf("assignment did not survive reload: tasks=%+v err=%v", tasks, err)
	}
	if err := db.UpdateConsoleSpecTaskAssignees(ctx, proposal.ID, "owner-a", "", ""); err != nil {
		t.Fatalf("NULL/unassigned state rejected: %v", err)
	}
	tasks, err = db.ListConsoleTaskProposals(ctx, proposal.SpecID)
	if err != nil || tasks[0].DeveloperAgentID != "" || tasks[0].ReviewerAgentID != "" {
		t.Fatalf("assignment clear did not persist: tasks=%+v err=%v", tasks, err)
	}
	if taskAssignmentDisableForeignKeys {
		if _, err := db.db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			t.Fatal(err)
		}
	}
	err = db.UpdateConsoleSpecTaskAssignees(ctx, proposal.ID, "owner-a", "agent-BOGUS", "")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("K12 nonexistent Agent assignment err=%v want foreign key rejection", err)
	}
}
