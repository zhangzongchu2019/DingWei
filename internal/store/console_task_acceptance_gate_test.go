package store

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var taskAcceptanceMutation = flag.String("mutate", "", "task acceptance gate mutation")
var taskAssignmentDisableForeignKeys bool

func TestMain(m *testing.M) {
	flag.Parse()
	switch *taskAcceptanceMutation {
	case "":
	case "k7-accept-empty":
		parseTaskAcceptanceIDs = func(string) ([]string, error) { return nil, nil }
	case "k8-accept-unknown":
		checkTaskAcceptanceMember = func([]string, []string) error { return nil }
	case "drop-insert-acceptance-check":
		validateTaskAcceptanceForInsert = func(string, string) error { return nil }
	case "drop-update-acceptance-check":
		validateTaskAcceptanceForUpdate = func(string, string) error { return nil }
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

func TestConsoleTaskAcceptanceGateInsert(t *testing.T) {
	db, now := taskAcceptanceGateSpec(t, "acceptance-insert")
	for i, raw := range []string{`[]`, `null`, `[{"id":"","text":"x"}]`} {
		err := db.CreateConsoleTaskProposal(context.Background(), taskAcceptanceProposal("invalid-insert-"+string(rune('a'+i)), "acceptance-insert", raw, now), "owner-a", false)
		if !errors.Is(err, ErrConsoleTaskAcceptance) {
			t.Errorf("K7 INSERT acceptance=%s err=%v want ErrConsoleTaskAcceptance", raw, err)
		}
	}
	for id, raw := range map[string]string{
		"dod-insert":     `[{"id":"DOD","text":"task definition"}]`,
		"unknown-insert": `[{"id":"A99","text":"not yet a checked lineage"}]`,
	} {
		if err := db.CreateConsoleTaskProposal(context.Background(), taskAcceptanceProposal(id, "acceptance-insert", raw, now), "owner-a", false); err != nil {
			t.Fatalf("K7 non-empty boundary INSERT %s rejected: %v", id, err)
		}
	}
}

func TestConsoleTaskAcceptanceGateUpdate(t *testing.T) {
	db, now := taskAcceptanceGateSpec(t, "acceptance-update")
	valid := `[{"id":"A1","text":"two specs remain independently editable"}]`
	if err := db.CreateConsoleTaskProposal(context.Background(), taskAcceptanceProposal("update-target", "acceptance-update", valid, now), "owner-a", false); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`[]`, `null`, `[{"id":"","text":"x"}]`} {
		err := db.UpdateConsoleTaskProposalPlan(context.Background(), "update-target", "owner-a", false, "content", "planned", 1, nil, raw)
		if !errors.Is(err, ErrConsoleTaskAcceptance) {
			t.Errorf("K7 UPDATE acceptance=%s err=%v want ErrConsoleTaskAcceptance", raw, err)
		}
	}
	for _, raw := range []string{`[{"id":"DOD","text":"task definition"}]`, `[{"id":"A99","text":"not yet a checked lineage"}]`, valid} {
		if err := db.UpdateConsoleTaskProposalPlan(context.Background(), "update-target", "owner-a", false, "content", "planned", 1, nil, raw); err != nil {
			t.Fatalf("K7 non-empty boundary UPDATE %s rejected: %v", raw, err)
		}
	}
}

func TestConsoleTaskAcceptanceK8MembershipPendingOwnerRuling(t *testing.T) {
	t.Skip("K8 is retained but deliberately not connected: acceptance_json currently stores task DoD")
	if err := validateTaskAcceptanceAgainstSpec(`[{"id":"A99"}]`, `[{"id":"A1"}]`); !errors.Is(err, ErrConsoleTaskAcceptance) {
		t.Fatalf("future K8 helper lost membership behavior: %v", err)
	}
}
