package store

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
)

func clarificationReader(a BlockerActor, c QuestionContext, q PilotQuestion, p ClarificationPolicy) bool {
	if a.AgentID != "" {
		return c.Task != nil && questionCurrent(q, c) && questionReader(a, c, q)
	}
	if a.UserID == "" {
		return false
	}
	if a.UserID == c.Owner || a.UserID == c.Editor || a.UserID == p.PMUser || a.UserID == p.DeputyUser {
		return true
	}
	// A formerly assigned PM is not kept by q's historical pm_user snapshot or
	// its implicit responsible_principal. A distinct explicit waiter retains
	// only its preexisting object grant, not any authority to answer.
	return q.Responsible == a.Key() && q.Responsible != "user:"+q.PMUser
}

// ReadClarificationThread returns new public thread posts only. It does not
// expose private legacy D3 answer history under the new PM/deputy grant.
func (s *SQLite) ReadClarificationThread(ctx context.Context, expected QuestionContext, a BlockerActor, qid string, before int64, limit int, check func() error) (ClarificationThread, error) {
	out := ClarificationThread{Posts: []ClarificationPost{}}
	if a.Key() == "" || a.UserID != "" && a.AgentID != "" || before < 0 || limit < 1 || limit > 100 || check == nil {
		return out, ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	c, err := questionContext(ctx, tx, "", "", qid)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(c, expected) {
		return out, ErrTaskFlowConflict
	}
	q, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
	if err != nil {
		return out, err
	}
	p, err := readClarificationPolicy(ctx, tx, q.SpecID)
	if err != nil {
		return out, err
	}
	if !clarificationReader(a, c, q, p) {
		return out, ErrTaskFlowPermission
	}
	anchors, e := clarificationAnchors(ctx, tx, q, false)
	if e != nil {
		return out, e
	}
	refs := []string{}
	for id, n := range anchors {
		if n == 1 {
			refs = append(refs, id)
		}
	}
	sort.Strings(refs)
	truncated := len(refs) > 300
	if truncated {
		refs = refs[:300]
	}
	out.Context = ClarificationContext{SpecID: q.SpecID, Revision: q.Revision, SHA: q.SHA, QuestionID: q.ID, Title: q.Title, Question: q.Question, ExpectedAnswer: q.ExpectedAnswer, Role: clarificationRole(a, p), EntryRefs: refs, AnchorsTruncated: truncated, Archived: c.Archived, PolicyVersion: p.Version}
	// First collect bounded keys, then close rows before reading the unique
	// body join, respecting SQLite's single-connection discipline.
	rows, err := tx.QueryContext(ctx, `SELECT floor FROM spec_clarification_post WHERE question_id=? AND (?=0 OR floor<?) ORDER BY floor DESC LIMIT ?`, qid, before, before, limit+1)
	if err != nil {
		return out, err
	}
	floors := []int64{}
	for rows.Next() {
		var f int64
		if err = rows.Scan(&f); err != nil {
			rows.Close()
			return out, err
		}
		floors = append(floors, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(floors) > limit {
		floors = floors[:limit]
		out.NextBefore = floors[len(floors)-1]
	}
	for _, f := range floors {
		post, e := readClarificationPost(ctx, tx, q.SpecID, f)
		if e != nil {
			return out, e
		}
		out.Posts = append(out.Posts, post)
	}
	if err = check(); err != nil {
		return ClarificationThread{}, err
	}
	if err = tx.Commit(); err != nil {
		return ClarificationThread{}, err
	}
	return out, nil
}
