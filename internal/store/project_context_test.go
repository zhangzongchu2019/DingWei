package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestProjectContextSQLiteProjectionReadOnlyAndExactRevision(t *testing.T) {
	db, _, state := taskFlowFixture(t)
	ctx := context.Background()
	// Two criteria in the exact task revision, only one explicitly linked.
	criteria := `[{"id":"A1","text":"allowed","cases":["one"]},{"id":"PRIVATE","text":"UNLINKED_SECRET_CASE"}]`
	if _, err := db.db.Exec(`UPDATE console_spec_revision SET acceptance_json=? WHERE spec_id='r32'`, criteria); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO console_spec_task_acceptance(task_id,acceptance_id) VALUES('task','A1')`); err != nil {
		t.Fatal(err)
	}
	page, err := db.ProjectContextCandidates(ctx, "dev", "", 101)
	if err != nil || len(page) != 1 {
		t.Fatal(err, page)
	}
	var changes int
	if err = db.db.QueryRow(`SELECT total_changes()`).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	err = db.WithProjectContextSnapshot(ctx, "dev", page, func(snap *ContextSnapshot) error {
		task, spec, e := snap.Project(page[0])
		if e != nil {
			return e
		}
		raw, _ := json.Marshal([]any{task, spec})
		if strings.Contains(string(raw), "UNLINKED_SECRET_CASE") || len(task.Acceptance) != 1 {
			t.Fatal("PC_SQLITE_PRIVATE_CRITERION_LEAK", string(raw))
		}
		if task.Role != "developer" || task.ID != state.ID || spec.Revision != page[0].Revision {
			t.Fatal("PC_SQLITE_WRONG_TASK_REVISION")
		}
		_, e = snap.Fingerprint()
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	var after int
	if err = db.db.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if changes != after {
		t.Fatal("PC_CONTEXT_WROTE_SQLITE", changes, after)
	}
	// All public entry points enforce bounded warehouse pages.
	if _, err = db.ProjectContextCandidates(ctx, "dev", "", 102); !errors.Is(err, ErrTaskFlowInvalid) {
		t.Fatal("PC_UNBOUNDED_CANDIDATE_QUERY")
	}
}
func TestProjectContextSQLiteCandidateMetadataAndRoleRace(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	ctx := context.Background()
	old, err := db.ProjectContextCandidates(ctx, "dev", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AssignConsoleTaskFlow(ctx, state.ID, state.Owner, state.GroupCode, "other", "review", state.Version, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = db.WithProjectContextSnapshot(ctx, "dev", old, func(*ContextSnapshot) error { called = true; return nil })
	if !errors.Is(err, ErrTaskFlowConflict) || called {
		t.Fatal("PC_SQLITE_STALE_CURRENT_ROLE", err)
	}
	page, err := db.ProjectContextCandidates(ctx, "other", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	page[0].Revision++
	err = db.WithProjectContextSnapshot(ctx, "other", page, func(*ContextSnapshot) error { t.Fatal("PC_SQLITE_STALE_REVISION_PROJECTED"); return nil })
	if !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal(err)
	}
}
func TestProjectContextSQLiteReadSnapshotKeepsOneAssignment(t *testing.T) {
	db, now, state := taskFlowFixture(t)
	ctx := context.Background()
	old, err := db.ProjectContextCandidates(ctx, "dev", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	var filename string
	if err = db.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&filename); err != nil {
		t.Fatal(err)
	}
	writer, err := OpenSQLite(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	err = db.WithProjectContextSnapshot(ctx, "dev", old, func(snap *ContextSnapshot) error {
		before, e := snap.Fingerprint()
		if e != nil {
			return e
		}
		_, e = writer.AssignConsoleTaskFlow(ctx, state.ID, state.Owner, state.GroupCode, "other", "review", state.Version, func() error { return nil }, now)
		if e != nil {
			return e
		}
		task, _, e := snap.Project(old[0])
		if e != nil {
			return e
		}
		after, e := snap.Fingerprint()
		if e != nil {
			return e
		}
		if before != after || task.Role != "developer" {
			t.Fatal("PC_SQLITE_MIXED_SNAPSHOT")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := db.ProjectContextCandidates(ctx, "dev", "", 2)
	if err != nil || len(next) != 0 {
		t.Fatal("PC_SQLITE_REASSIGNED_DATA_VISIBLE", err, next)
	}
}
