package store

import (
	"context"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var taskAcceptanceMutation = flag.String("mutate", "", "task acceptance gate mutation")
var taskAssignmentDisableForeignKeys bool
var taskAcceptanceDisableCascade bool

func TestMain(m *testing.M) {
	flag.Parse()
	switch *taskAcceptanceMutation {
	case "":
	case "k8-accept-unknown":
		skipTaskAcceptanceMembership = true
	case "drop-task-acceptance-validation":
		skipTaskAcceptanceValidation = true
	case "drop-task-acceptance-write":
		skipTaskAcceptanceWrite = true
	case "k14-hide-acceptance-read":
		hideTaskAcceptanceRead = true
	case "k15-hide-unlinked":
		treatEveryAcceptanceAsLinked = true
	case "drop-k15-linked-lookup":
		skipLinkedAcceptanceLookup = true
	case "k17-create-dod-in-acceptance":
		createTaskDefinitionStorage = legacyTaskDefinitionInAcceptance
	case "k17-update-dod-in-acceptance":
		updateTaskDefinitionStorage = legacyTaskDefinitionInAcceptance
	case "k17-presplit-dod-in-acceptance":
		presplitTaskDefinitionStorage = legacyTaskDefinitionInAcceptance
	case "drop-k17-create-dod-storage":
		createTaskDefinitionStorage = dropTaskDefinitionStorage
	case "drop-k17-update-dod-storage":
		updateTaskDefinitionStorage = dropTaskDefinitionStorage
	case "drop-k17-presplit-dod-storage":
		presplitTaskDefinitionStorage = dropTaskDefinitionStorage
	case "k18-disable-cascade":
		taskAcceptanceDisableCascade = true
	case "k19-accept-proposal-dod":
		allowProposalTaskDefinition = true
	case "drop-k19-proposal-check":
		skipProposalDefinitionCheck = true
	case "drop-k18-task-delete":
		skipConsoleSpecTaskDelete = true
	case "k20-count-marked", "drop-k20-confirmed-filter":
		countUnconfirmedTaskCases = true
	case "k21-allow-other-marker", "drop-k21-marker-role-check":
		allowAnyTaskCaseMarker = true
	case "k22-allow-developer-confirm", "drop-k22-confirmer-role-check":
		allowAnyTaskCaseConfirmer = true
	case "k23-fixed-denominator", "drop-k23-case-expansion":
		truncateTaskCaseDenominator = true
	case "k24-allow-zero-total", "drop-k24-fallback":
		disableTaskFallbackCase = true
	case "k25-disable-cascade":
		taskAcceptanceDisableCascade = true
	case "drop-k25-task-delete":
		skipConsoleSpecTaskDelete = true
	case "k12-disable-foreign-keys":
		taskAssignmentDisableForeignKeys = true
	default:
		panic("unknown --mutate=" + *taskAcceptanceMutation)
	}
	os.Exit(m.Run())
}

func taskAcceptanceGateSpec(t *testing.T, id string) (*SQLite, time.Time) {
	t.Helper()
	db := newConsoleSpecStore(t)
	now := time.Date(2026, 9, 26, 7, 0, 0, 0, time.UTC)
	createSpec(t, db, id, id, now)
	if _, err := db.db.ExecContext(context.Background(), `UPDATE console_spec SET state='final',current_editor_id=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	return db, now
}

func taskAcceptanceProposal(id, specID, acceptance string, now time.Time) model.ConsoleTaskProposal {
	return model.ConsoleTaskProposal{
		ID: id, SpecID: specID, Title: id, TaskContent: id, Status: "planned", EstimatedHours: 1,
		DependsOnJSON: `[]`, AcceptanceJSON: acceptance, ReviewedBy: "owner-a", ReviewedAt: now, CreatedAt: now,
	}
}

func TestConsoleTaskMayStartWithNoAcceptanceLinks(t *testing.T) {
	db, now := taskAcceptanceGateSpec(t, "acceptance-insert")
	proposal := taskAcceptanceProposal("zero-links", "acceptance-insert", `[]`, now)
	proposal.DefinitionOfDone = "task definition"
	if err := db.CreateConsoleTaskProposal(context.Background(), proposal, "owner-a", false); err != nil {
		t.Fatalf("owner ruling permits a task with zero criterion links: %v", err)
	}
	ids, err := db.ListConsoleSpecTaskAcceptanceIDs(context.Background(), proposal.ID)
	if err != nil || len(ids) != 0 {
		t.Fatalf("new task links=%v err=%v want empty", ids, err)
	}
}
