package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestClarificationContextProjectionChangesCursorWithoutReopeningTask(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	// Completed task: a discussion adds no D2 input/task version, yet must still
	// invalidate a context cursor whose data snapshot no longer matches.
	if _, e := db.db.Exec(`UPDATE console_spec_task SET status='done' WHERE id='task'`); e != nil {
		t.Fatal(e)
	}
	page, e := db.ProjectContextCandidates(ctx, "dev", "", 100)
	if e != nil {
		t.Fatal(e)
	}
	var before, after string
	e = db.WithProjectContextSnapshot(ctx, "dev", page, func(s *ContextSnapshot) error { var err error; before, err = s.Fingerprint(); return err })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, clarificationUpdate("after-done")); e != nil {
		t.Fatal(e)
	}
	e = db.WithProjectContextSnapshot(ctx, "dev", page, func(s *ContextSnapshot) error {
		var err error
		after, err = s.Fingerprint()
		if err != nil {
			return err
		}
		task, spec, err := s.Project(page[0])
		if err != nil {
			return err
		}
		if !task.Clarifications.DataOnly || len(task.Clarifications.Posts) != 1 || len(spec.Clarifications.Posts) != 1 || task.Status != "done" {
			t.Fatal("CLARIFICATION_CONTEXT_NOT_DATA_OR_REOPENED", task)
		}
		raw, _ := json.Marshal(task)
		if strings.Contains(string(raw), q.ObservedFact) {
			t.Fatal("CLARIFICATION_CONTEXT_PRIVATE_D3_LEAK")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if before == after {
		t.Fatal("CLARIFICATION_CONTEXT_CURSOR_NOT_INVALIDATED")
	}
	if _, e = db.db.Exec(`UPDATE pilot_question SET task_id='' WHERE id=?`, q.ID); e != nil {
		t.Fatal(e)
	}
	e = db.WithProjectContextSnapshot(ctx, "dev", page, func(s *ContextSnapshot) error {
		task, _, err := s.Project(page[0])
		if err != nil {
			return err
		}
		if len(task.Clarifications.Posts) != 0 {
			t.Fatal("CLARIFICATION_CONTEXT_OTHER_THEME_LEAK")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
