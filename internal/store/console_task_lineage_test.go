package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func lineageFixture(t *testing.T, specID string) (*SQLite, time.Time) {
	t.Helper()
	db, now := taskAcceptanceGateSpec(t, specID)
	if _, err := db.db.ExecContext(context.Background(), `UPDATE console_spec_revision SET acceptance_json='[{"id":"A1","text":"first"},{"id":"A2","text":"second"}]' WHERE spec_id=? AND revision=1`, specID); err != nil {
		t.Fatal(err)
	}
	return db, now
}

func createLineageTask(t *testing.T, db *SQLite, now time.Time, specID, taskID, dod string) {
	t.Helper()
	p := taskAcceptanceProposal(taskID, specID, `[]`, now)
	p.DefinitionOfDone = dod
	if err := db.CreateConsoleTaskProposal(context.Background(), p, "owner-a", false); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleTaskAcceptanceLinksCoverManyToManyAndUnlinkedQuery(t *testing.T) {
	ctx := context.Background()
	db, now := lineageFixture(t, "lineage")
	createLineageTask(t, db, now, "lineage", "task-1", "dod one")
	createLineageTask(t, db, now, "lineage", "task-2", "dod two")

	if err := db.SetConsoleSpecTaskAcceptances(ctx, "task-1", "owner-a", []string{"A1", "A2"}); err != nil {
		t.Fatalf("K14 one task linking multiple criteria: %v", err)
	}
	if err := db.SetConsoleSpecTaskAcceptances(ctx, "task-2", "owner-a", []string{"A1"}); err != nil {
		t.Fatalf("K14 one criterion linking multiple tasks: %v", err)
	}
	if got, err := db.ListConsoleSpecTaskAcceptanceIDs(ctx, "task-1"); err != nil || !reflect.DeepEqual(got, []string{"A1", "A2"}) {
		t.Fatalf("K14 read links=%v err=%v", got, err)
	}
	if err := db.SetConsoleSpecTaskAcceptances(ctx, "task-1", "owner-a", []string{"A2"}); err != nil {
		t.Fatalf("K14 unlink A1: %v", err)
	}
	if got, _ := db.ListConsoleSpecTaskAcceptanceIDs(ctx, "task-1"); !reflect.DeepEqual(got, []string{"A2"}) {
		t.Fatalf("K14 unlink result=%v want [A2]", got)
	}

	if err := db.SetConsoleSpecTaskAcceptances(ctx, "task-1", "owner-a", []string{"A99"}); !errors.Is(err, ErrConsoleTaskAcceptance) {
		t.Fatalf("K16 unknown criterion err=%v want ErrConsoleTaskAcceptance", err)
	}
	if got, err := db.ListUnlinkedConsoleSpecAcceptanceIDs(ctx, "lineage", 1); err != nil || len(got) != 0 {
		t.Fatalf("K15 all criteria linked got=%v err=%v want empty", got, err)
	}
	if err := db.SetConsoleSpecTaskAcceptances(ctx, "task-1", "owner-a", nil); err != nil {
		t.Fatalf("owner ruling permits zero links: %v", err)
	}
	if got, err := db.ListUnlinkedConsoleSpecAcceptanceIDs(ctx, "lineage", 1); err != nil || !reflect.DeepEqual(got, []string{"A2"}) {
		t.Fatalf("K15 unlinked got=%v err=%v want [A2]", got, err)
	}
}

func assertTaskDefinitionSeparated(t *testing.T, db *SQLite, taskID, want string) {
	t.Helper()
	var acceptance, dod string
	if err := db.db.QueryRow(`SELECT acceptance_json,definition_of_done FROM console_spec_task WHERE id=?`, taskID).Scan(&acceptance, &dod); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(acceptance, "DOD") || acceptance != `[]` || dod != want {
		t.Fatalf("K17 task=%s acceptance=%s definition_of_done=%q want [] / %q", taskID, acceptance, dod, want)
	}
}

func TestConsoleTaskDefinitionOfDoneIsSeparateOnAllThreeWritePaths(t *testing.T) {
	ctx := context.Background()
	db, now := lineageFixture(t, "dod-paths")
	createLineageTask(t, db, now, "dod-paths", "created-task", "create DoD")
	assertTaskDefinitionSeparated(t, db, "created-task", "create DoD")
	if err := db.UpdateConsoleTaskProposalPlan(ctx, "created-task", "owner-a", false, "updated", "planned", 1, nil, "update DoD"); err != nil {
		t.Fatal(err)
	}
	assertTaskDefinitionSeparated(t, db, "created-task", "update DoD")

	agent := model.ConsoleAgent{ID: "presplit-agent", KeyID: "key", SessionName: "session", OwnerPrincipal: "owner-a", Active: true}
	if err := db.UpsertConsoleAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(time.Hour)
	if _, err := db.db.ExecContext(ctx, `INSERT INTO console_spec_presplit_request(id,spec_id,revision,agent_id,owner_principal,status,failure_reason,deadline_at,created_at) VALUES('request','dod-paths',1,?,'owner-a','pending','',?,?)`, agent.ID, deadline.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteConsoleSpecPresplit(ctx, "request", agent, []ConsoleSpecPresplitTask{{ID: "presplit-task", Title: "pre", Content: "pre", DefinitionOfDone: "presplit DoD"}}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertTaskDefinitionSeparated(t, db, "presplit-task", "presplit DoD")
}

func TestConsoleTaskAcceptanceLinksCascadeOnTaskDelete(t *testing.T) {
	ctx := context.Background()
	db, now := lineageFixture(t, "cascade")
	createLineageTask(t, db, now, "cascade", "cascade-task", "dod")
	if err := db.SetConsoleSpecTaskAcceptances(ctx, "cascade-task", "owner-a", []string{"A1"}); err != nil {
		t.Fatal(err)
	}
	if taskAcceptanceDisableCascade {
		if _, err := db.db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeleteConsoleSpecTask(ctx, "cascade-task", "owner-a"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_spec_task_acceptance WHERE task_id='cascade-task'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("K18 cascade left %d acceptance links after task delete", n)
	}
}

func TestConsoleTaskLegacyDODMigrationPendingProductionCount(t *testing.T) {
	t.Skip(`K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision`)
}

func TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling(t *testing.T) {
	t.Skip("future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed")
}

func TestConsoleTaskProposalRejectsDefinitionOfDoneWithoutPartialWrite(t *testing.T) {
	ctx := context.Background()
	db, now := lineageFixture(t, "proposal-dod")
	stamp := now.Format(time.RFC3339Nano)
	if _, err := db.db.ExecContext(ctx, `INSERT INTO console_spec_review(id,spec_id,revision,stage,verdict,reason_codes_json,detail,reviewed_by,reviewed_at) VALUES('review','proposal-dod',1,'human_review','pass','[]','','owner-a',?)`, stamp); err != nil {
		t.Fatal(err)
	}
	const original = `[{"id":"legacy","text":"unchanged"}]`
	if _, err := db.db.ExecContext(ctx, `INSERT INTO console_task_proposal(id,schema_version,spec_id,revision,content_sha256,title,task_content,status,depends_on_json,acceptance_json,input_anchors_json,review_id,reviewed_by,reviewed_at,created_at) VALUES('proposal',1,'proposal-dod',1,'hash','title','before','planned','[]',?,'[]','review','owner-a',?,?)`, original, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	err := db.UpdateConsoleTaskProposalPlan(ctx, "proposal", "owner-a", false, "after", "planned", 1, nil, "proposal DoD")
	if !errors.Is(err, ErrConsoleTaskDefinition) {
		t.Fatalf("K19 proposal DoD update err=%v want ErrConsoleTaskDefinition", err)
	}
	var content, acceptance string
	if err := db.db.QueryRowContext(ctx, `SELECT task_content,acceptance_json FROM console_task_proposal WHERE id='proposal'`).Scan(&content, &acceptance); err != nil {
		t.Fatal(err)
	}
	if content != "before" || acceptance != original {
		t.Fatalf("K19 partial write content=%q acceptance=%s", content, acceptance)
	}
}
