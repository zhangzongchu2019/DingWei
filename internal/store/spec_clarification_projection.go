package store

import (
	"context"
	"database/sql"
	"reflect"
)

// Bounded posts are explicitly data, never task instructions or new authority.
// Only this task's exact revision/SHA is projected under the existing R9 PG
// membership/assignment authorization and the same SQLite read transaction.
type ContextClarifications struct {
	DataOnly   bool                `json:"data_only"`
	Posts      []ClarificationPost `json:"posts"`
	NextBefore int64               `json:"next_before,omitempty"`
}

func taskContextClarifications(ctx context.Context, db taskQuery, c ContextCandidate) (ContextClarifications, error) {
	out := ContextClarifications{DataOnly: true, Posts: []ClarificationPost{}}
	rows, e := db.QueryContext(ctx, `SELECT p.floor FROM spec_clarification_post p JOIN pilot_question q ON q.id=p.question_id WHERE q.task_id=? AND p.spec_id=? AND p.revision=? AND p.content_sha256=? ORDER BY p.floor DESC LIMIT 21`, c.State.ID, c.SpecID, c.Revision, c.SHA)
	if e != nil {
		return out, e
	}
	floors := []int64{}
	for rows.Next() {
		var f int64
		if e = rows.Scan(&f); e != nil {
			rows.Close()
			return out, e
		}
		floors = append(floors, f)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(floors) > 20 {
		floors = floors[:20]
		out.NextBefore = floors[len(floors)-1]
	}
	for _, floor := range floors {
		p, err := readClarificationPost(ctx, db, c.SpecID, floor)
		if err != nil {
			return out, err
		}
		out.Posts = append(out.Posts, p)
	}
	return out, nil
}

// Spec readers see only the new public thread body/metadata, never legacy D3
// history, private facts or recipient bindings. Pages are pinned to one revision.
func (s *SQLite) ReadSpecClarifications(ctx context.Context, expected QuestionContext, revision int, before int64, limit int, check func() error) (ContextClarifications, error) {
	out := ContextClarifications{DataOnly: true, Posts: []ClarificationPost{}}
	if expected.Task != nil || revision < 1 || before < 0 || limit < 1 || limit > 100 || check == nil {
		return out, ErrTaskFlowInvalid
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	c, e := questionContext(ctx, tx, expected.SpecID, "", "")
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(c, expected) {
		return out, ErrTaskFlowConflict
	}
	var sha string
	if e = tx.QueryRowContext(ctx, `SELECT content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=?`, c.SpecID, revision).Scan(&sha); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT floor FROM spec_clarification_post WHERE spec_id=? AND revision=? AND content_sha256=? AND (?=0 OR floor<?) ORDER BY floor DESC LIMIT ?`, c.SpecID, revision, sha, before, before, limit+1)
	if e != nil {
		return out, e
	}
	floors := []int64{}
	for rows.Next() {
		var f int64
		if e = rows.Scan(&f); e != nil {
			rows.Close()
			return out, e
		}
		floors = append(floors, f)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(floors) > limit {
		floors = floors[:limit]
		out.NextBefore = floors[len(floors)-1]
	}
	for _, floor := range floors {
		p, err := readClarificationPost(ctx, tx, c.SpecID, floor)
		if err != nil {
			return out, err
		}
		out.Posts = append(out.Posts, p)
	}
	if e = check(); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
