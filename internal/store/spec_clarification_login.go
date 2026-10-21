package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"time"
)

func clarificationDigest(s string) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == 32 }

// A login continuation is not an authorization token. It only carries a fixed
// question selector, is bound to a same-browser HttpOnly cookie and is consumed
// after normal Console authentication. Arbitrary redirect targets are not stored.
func (s *SQLite) CreateClarificationLogin(ctx context.Context, state, cookie, question string, now time.Time) error {
	if !clarificationDigest(state) || !clarificationDigest(cookie) || !questionNonempty(question, 256) {
		return ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM spec_clarification_login_state WHERE expires_at<=?`, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM spec_clarification_login_state`).Scan(&count); err != nil {
		return err
	}
	if count >= 5000 {
		return ErrBlockerRateLimited
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_login_state(state_sha,cookie_sha,question_id,expires_at) VALUES(?,?,?,?)`, state, cookie, question, now.Add(10*time.Minute).UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLite) ConsumeClarificationLogin(ctx context.Context, state, cookie string, now time.Time) (string, error) {
	if !clarificationDigest(state) || !clarificationDigest(cookie) {
		return "", ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var question string
	err = tx.QueryRowContext(ctx, `SELECT question_id FROM spec_clarification_login_state WHERE state_sha=? AND cookie_sha=? AND consumed_at IS NULL AND expires_at>?`, state, cookie, now.UTC().Format(time.RFC3339Nano)).Scan(&question)
	if err == sql.ErrNoRows {
		return "", ErrTaskFlowConflict
	}
	if err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, `UPDATE spec_clarification_login_state SET consumed_at=? WHERE state_sha=? AND cookie_sha=? AND consumed_at IS NULL`, now.UTC().Format(time.RFC3339Nano), state, cookie)
	if err != nil {
		return "", err
	}
	if n, e := res.RowsAffected(); e != nil || n != 1 {
		return "", ErrTaskFlowConflict
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return question, nil
}
