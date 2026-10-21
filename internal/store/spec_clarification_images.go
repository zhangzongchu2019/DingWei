package store

import (
	"context"
	"database/sql"
	"github.com/zhangzongchu2019/dingwei/internal/clarificationimage"
	"reflect"
	"time"
)

func (s *SQLite) RegisterClarificationImage(ctx context.Context, expected QuestionContext, a BlockerActor, qid string, f clarificationimage.File, check func() error, now time.Time) error {
	if a.Key() == "" || check == nil || !clarificationDigest(f.ID) || !clarificationDigest(f.SHA) || f.Size < 1 || f.Size > clarificationimage.MaxBytes || (f.Media != "image/png" && f.Media != "image/jpeg" && f.Media != "image/webp") {
		return ErrTaskFlowInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	c, e := questionContext(ctx, tx, "", "", qid)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(c, expected) {
		return ErrTaskFlowConflict
	}
	q, e := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
	if e != nil {
		return e
	}
	p, e := readClarificationPolicy(ctx, tx, q.SpecID)
	if e != nil {
		return e
	}
	if c.Archived || !clarificationReader(a, c, q, p) {
		return ErrTaskFlowPermission
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM spec_clarification_image i WHERE question_id=? AND actor_id=? AND NOT EXISTS(SELECT 1 FROM spec_clarification_post_image p WHERE p.file_id=i.file_id)`, qid, a.Key()).Scan(&count); e != nil {
		return e
	}
	if count >= 16 {
		return ErrBlockerRateLimited
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO spec_clarification_image(file_id,question_id,actor_id,media_type,sha256,size_bytes,created_at) VALUES(?,?,?,?,?,?,?)`, f.ID, qid, a.Key(), f.Media, f.SHA, f.Size, now.UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	if e = check(); e != nil {
		return e
	}
	return tx.Commit()
}
func clarificationPostImages(ctx context.Context, tx taskQuery, spec string, floor int64) ([]clarificationimage.File, error) {
	rows, e := tx.QueryContext(ctx, `SELECT i.file_id,i.media_type,i.sha256,i.size_bytes FROM spec_clarification_post_image p JOIN spec_clarification_image i ON i.file_id=p.file_id WHERE p.spec_id=? AND p.floor=? ORDER BY i.file_id LIMIT 5`, spec, floor)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []clarificationimage.File{}
	for rows.Next() {
		var f clarificationimage.File
		if e = rows.Scan(&f.ID, &f.Media, &f.SHA, &f.Size); e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func (s *SQLite) ReadClarificationImageMetadata(ctx context.Context, expected QuestionContext, a BlockerActor, qid, id string, check func() error) (clarificationimage.File, error) {
	var f clarificationimage.File
	if !clarificationDigest(id) || check == nil {
		return f, ErrTaskFlowInvalid
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return f, e
	}
	defer tx.Rollback()
	c, e := questionContext(ctx, tx, "", "", qid)
	if e != nil {
		return f, e
	}
	if !reflect.DeepEqual(c, expected) {
		return f, ErrTaskFlowConflict
	}
	q, e := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, qid))
	if e != nil {
		return f, e
	}
	p, e := readClarificationPolicy(ctx, tx, q.SpecID)
	if e != nil {
		return f, e
	}
	if !clarificationReader(a, c, q, p) {
		return f, ErrTaskFlowPermission
	}
	var owner string
	var linked bool
	e = tx.QueryRowContext(ctx, `SELECT i.file_id,i.media_type,i.sha256,i.size_bytes,i.actor_id,EXISTS(SELECT 1 FROM spec_clarification_post_image p WHERE p.file_id=i.file_id) FROM spec_clarification_image i WHERE i.question_id=? AND i.file_id=?`, qid, id).Scan(&f.ID, &f.Media, &f.SHA, &f.Size, &owner, &linked)
	if e != nil {
		return f, e
	}
	if !linked && owner != a.Key() {
		return f, ErrTaskFlowPermission
	}
	if e = check(); e != nil {
		return f, e
	}
	return f, tx.Commit()
}
