package store

import (
	"context"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

const confirmationColumns = `id,operation_id,git_instance,repository,change_dir,commit_sha,confirmed_by,confirmed_at,meaning`

func scanSpecConfirmation(row interface{ Scan(...any) error }) (specconfirm.Confirmation, error) {
	var c specconfirm.Confirmation
	var stamp string
	err := row.Scan(&c.ID, &c.OperationID, &c.Instance, &c.Repository, &c.ChangeDir, &c.Commit, &c.ConfirmedBy, &stamp, &c.Meaning)
	if err == nil {
		c.ConfirmedAt, err = time.Parse(time.RFC3339Nano, stamp)
	}
	return c, err
}

// SaveSpecConfirmation is a persistence boundary, not an authorization source.
// Callers must enter through the service's trusted Authority fence.
func (s *SQLite) SaveSpecConfirmation(ctx context.Context, c specconfirm.Confirmation) (specconfirm.Confirmation, error) {
	if err := c.Validate(); err != nil {
		return specconfirm.Confirmation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return specconfirm.Confirmation{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO openspec_commit_confirmation (`+confirmationColumns+`) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, c.ID, c.OperationID, c.Instance, c.Repository, c.ChangeDir, c.Commit, c.ConfirmedBy, c.ConfirmedAt.UTC().Format(time.RFC3339Nano), c.Meaning)
	if err != nil {
		return specconfirm.Confirmation{}, err
	}
	saved, err := scanSpecConfirmation(tx.QueryRowContext(ctx, `SELECT `+confirmationColumns+` FROM openspec_commit_confirmation WHERE id=?`, c.ID))
	if err != nil {
		return specconfirm.Confirmation{}, err
	}
	if !saved.SameFact(c) {
		return specconfirm.Confirmation{}, specconfirm.ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return specconfirm.Confirmation{}, err
	}
	return saved, nil
}
func (s *SQLite) ListSpecConfirmations(ctx context.Context, c specconfirm.Coordinate) ([]specconfirm.Confirmation, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+confirmationColumns+` FROM openspec_commit_confirmation WHERE git_instance=? AND repository=? AND change_dir=? ORDER BY confirmed_at,id LIMIT 501`, c.Instance, c.Repository, c.ChangeDir)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]specconfirm.Confirmation, 0)
	for rows.Next() {
		fact, e := scanSpecConfirmation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, fact)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Do not silently present a truncated audit history as complete.
	if len(out) > 500 {
		return nil, specconfirm.ErrUnavailable
	}
	return out, nil
}

var _ specconfirm.Store = (*SQLite)(nil)
