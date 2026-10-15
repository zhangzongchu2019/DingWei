package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestAgentDirectRevisionOneOwnershipAndAudit(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Now()
	for _, id := range []string{"first-actor", "other-agent"} {
		if err := db.UpsertConsoleAgent(ctx, model.ConsoleAgent{ID: id, KeyID: "synthetic", SessionName: id, OwnerPrincipal: "owner-a", Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"principal", "agent", "unknown"} {
		id := "direct-" + kind
		rev := specRevision(id, 1, "initial")
		rev.CreatedByKind = kind
		if kind == "unknown" {
			rev.CreatedByKind = "principal"
		}
		rev.CreatedBy = "first-actor"
		if kind == "principal" || kind == "unknown" {
			rev.CreatedBy = "owner-a"
		}
		spec := model.ConsoleSpec{ID: id, PrincipalID: "owner-a", Title: id, EditCapabilityHash: "hash"}
		if err := db.CreateConsoleSpec(ctx, spec, rev, now); err != nil {
			t.Fatal(err)
		}
		if kind == "unknown" {
			if _, err := db.db.Exec(`UPDATE console_spec_revision SET created_by_kind='unknown' WHERE spec_id=? AND revision=1`, id); err != nil {
				t.Fatal(err)
			}
		}
		update := specRevision(id, 2, "synthetic no private content")
		update.CreatedByKind = "agent"
		update.CreatedBy = "first-actor"
		_, err := db.AppendConsoleSpecRevisionByAgent(ctx, "owner-a", id, 1, update, now)
		if kind != "agent" {
			if !errors.Is(err, ErrConsoleSpecAgentOwnership) {
				t.Fatalf("kind=%s error=%v", kind, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		wrong := specRevision(id, 3, "wrong agent")
		wrong.CreatedByKind = "agent"
		wrong.CreatedBy = "other-agent"
		if _, err := db.AppendConsoleSpecRevisionByAgent(ctx, "owner-a", id, 2, wrong, now); !errors.Is(err, ErrConsoleSpecAgentOwnership) {
			t.Fatal("latest/current owner used instead of revision1 identity")
		}
		rows, err := db.db.QueryContext(ctx, `SELECT actor,action,detail FROM console_spec_event WHERE spec_id=? ORDER BY revision`, id)
		if err != nil {
			t.Fatal(err)
		}
		var actions []string
		for rows.Next() {
			var actor, action, detail string
			if rows.Scan(&actor, &action, &detail) != nil {
				t.Fatal("bad event")
			}
			if actor != "first-actor" {
				t.Fatal("untrusted audit actor")
			}
			actions = append(actions, action)
		}
		rows.Close()
		if len(actions) != 2 || actions[0] != "agent_create" || actions[1] != "agent_update" {
			t.Fatalf("audit actions=%v", actions)
		}
	}
}

func TestSpecArchiveBatchAllOrNothingPermissionAndRestore(t *testing.T) {
	db := newConsoleSpecStore(t)
	ctx := context.Background()
	now := time.Now()
	for _, id := range []string{"archive-a", "archive-b"} {
		createSpec(t, db, id, id, now)
	}
	for _, ids := range [][]string{{"archive-a", "missing"}, {"archive-a", "archive-a"}} {
		if err := db.SetConsoleSpecsArchived(ctx, ids, "owner-a", false, true, now); err == nil {
			t.Fatal("invalid batch accepted")
		}
		got, _ := db.ConsoleSpec(ctx, "owner-a", "archive-a", false)
		if got.Archived {
			t.Fatal("invalid batch partially deleted")
		}
	}
	if err := db.SetConsoleSpecsArchived(ctx, []string{"archive-a"}, "owner-b", false, true, now); !errors.Is(err, ErrConsoleSpecPermission) {
		t.Fatal("non-owner deleted spec")
	}
	// A failure while writing the SECOND event must roll back the first archive.
	_, err := db.db.Exec(`CREATE TRIGGER synthetic_archive_failure BEFORE INSERT ON console_spec_event WHEN NEW.spec_id='archive-b' AND NEW.action='archive' BEGIN SELECT RAISE(ABORT,'synthetic'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConsoleSpecsArchived(ctx, []string{"archive-a", "archive-b"}, "owner-a", false, true, now); err == nil {
		t.Fatal("injected event failure ignored")
	}
	got, _ := db.ConsoleSpec(ctx, "owner-a", "archive-a", false)
	if got.Archived {
		t.Fatal("event failure committed partial archive")
	}
	if _, err = db.db.Exec(`DROP TRIGGER synthetic_archive_failure`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConsoleSpecsArchived(ctx, []string{"archive-a", "archive-b"}, "owner-a", false, true, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM console_spec_event WHERE action='archive'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("archive events=%d error=%v", n, err)
	}
	if err := db.SetConsoleSpecsArchived(ctx, []string{"archive-a"}, "owner-a", false, false, now); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ConsoleSpec(ctx, "owner-a", "archive-a", false)
	if got.Archived {
		t.Fatal("restore failed")
	}
	still, _ := db.ConsoleSpec(ctx, "owner-a", "archive-b", false)
	if !still.Archived {
		t.Fatal("restore changed another row")
	}
}
