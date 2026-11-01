package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/zhangzongchu2019/dingwei/internal/changeflow"
)

func (s *SQLite) Read(ctx context.Context, c changeflow.Coordinate) (changeflow.State, error) {
	st := changeflow.NewState(c)
	if c.Validate() != nil {
		return st, changeflow.ErrInvalid
	}
	var b string
	e := s.db.QueryRowContext(ctx, `SELECT body FROM changeflow_evidence WHERE coordinate_key=?`, c.Key()).Scan(&b)
	if e == sql.ErrNoRows {
		return st, nil
	}
	if e != nil {
		return st, e
	}
	if json.Unmarshal([]byte(b), &st) != nil || st.Coordinate != c {
		return changeflow.State{}, changeflow.ErrUnavailable
	}
	return st, nil
}
func (s *SQLite) Transact(ctx context.Context, c changeflow.Coordinate, fn func(*changeflow.State) error) error {
	if c.Validate() != nil {
		return changeflow.ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var b string
	var version int64
	st := changeflow.NewState(c)
	e = tx.QueryRowContext(ctx, `SELECT revision,body FROM changeflow_evidence WHERE coordinate_key=?`, c.Key()).Scan(&version, &b)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if e == nil && (json.Unmarshal([]byte(b), &st) != nil || st.Coordinate != c) {
		return changeflow.ErrUnavailable
	}
	before, _ := json.Marshal(st)
	if e = fn(&st); e != nil {
		return e
	}
	if st.Coordinate != c {
		return changeflow.ErrConflict
	}
	var original changeflow.State
	if json.Unmarshal(before, &original) != nil || !changeHistoryPreserved(original, st) {
		return changeflow.ErrConflict
	}
	after, e := json.Marshal(st)
	if e != nil || len(after) > 8<<20 {
		return changeflow.ErrUnavailable
	}
	// Read-only/idempotent callbacks do not update the revision or audit timestamp.
	if string(before) == string(after) {
		return nil
	}
	var result sql.Result
	if version == 0 {
		result, e = tx.ExecContext(ctx, `INSERT INTO changeflow_evidence(coordinate_key,revision,body) VALUES(?,1,?) ON CONFLICT(coordinate_key) DO NOTHING`, c.Key(), string(after))
	} else {
		result, e = tx.ExecContext(ctx, `UPDATE changeflow_evidence SET revision=revision+1,body=? WHERE coordinate_key=? AND revision=?`, string(after), c.Key(), version)
	}
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return changeflow.ErrConflict
	}
	return tx.Commit()
}

var _ changeflow.Store = (*SQLite)(nil)

// Preserve historical prefixes even if a future caller accidentally mutates an
// old result/event/revision in place. Mutable delivery/knowledge belong only to
// the current reminder revision; earlier revisions cannot be rewritten.
func changeHistoryPreserved(old, next changeflow.State) bool {
	if !jsonPrefix(old.Results, next.Results) || !jsonPrefix(old.Events, next.Events) || !jsonPrefix(old.Tools, next.Tools) {
		return false
	}
	for key, op := range old.Operations {
		if next.Operations[key] != op {
			return false
		}
	}
	if old.Reminder == nil {
		return true
	}
	if next.Reminder == nil || old.Reminder.ID != next.Reminder.ID || old.Reminder.Coordinate != next.Reminder.Coordinate {
		return false
	}
	if !old.Reminder.MergedAt.IsZero() && (!old.Reminder.MergedAt.Equal(next.Reminder.MergedAt) || old.Reminder.MergeEvidence != next.Reminder.MergeEvidence) {
		return false
	}
	a, b := old.Reminder.Revisions, next.Reminder.Revisions
	if len(b) < len(a) {
		return false
	}
	if len(b) > len(a) {
		return jsonPrefix(a, b)
	}
	if len(a) == 0 {
		return true
	}
	if !jsonPrefix(a[:len(a)-1], b[:len(b)-1]) {
		return false
	}
	before, after := a[len(a)-1], b[len(b)-1]
	if !jsonPrefix(before.Attempts, after.Attempts) {
		return false
	}
	if before.Acknowledged != nil && !jsonSame(before.Acknowledged, after.Acknowledged) {
		return false
	}
	before.Acknowledged = nil
	after.Acknowledged = nil
	before.MR = changeflow.Delivery{}
	after.MR = changeflow.Delivery{}
	before.Attempts = nil
	after.Attempts = nil
	return jsonSame(before, after)
}
func jsonSame(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func jsonPrefix[T any](a, b []T) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if !jsonSame(a[i], b[i]) {
			return false
		}
	}
	return true
}
