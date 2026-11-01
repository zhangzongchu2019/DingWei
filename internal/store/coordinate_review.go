package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

func (s *SQLite) CoordinateReview(ctx context.Context, id string) (r cr.Record, e error) {
	var b string
	e = s.db.QueryRowContext(ctx, `SELECT body FROM coordinate_review WHERE id=?`, id).Scan(&b)
	if e == sql.ErrNoRows {
		return r, cr.ErrForbidden
	}
	if e != nil {
		return
	}
	if json.Unmarshal([]byte(b), &r) != nil || r.ID != id || r.Validate() != nil {
		return cr.Record{}, cr.ErrUnavailable
	}
	return
}
func (s *SQLite) CreateCoordinateReview(ctx context.Context, r cr.Record, check func() error) (out cr.Record, e error) {
	if r.Validate() != nil || check == nil {
		return out, cr.ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return
	}
	defer tx.Rollback()
	if e = check(); e != nil {
		return
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO notification_event_owner(event_id,semantic_owner,assignment) VALUES(?,'r6',?) ON CONFLICT(event_id) DO NOTHING`, r.EventID, r.ID); e != nil {
		return
	}
	var owner, assignment string
	if e = tx.QueryRowContext(ctx, `SELECT semantic_owner,assignment FROM notification_event_owner WHERE event_id=?`, r.EventID).Scan(&owner, &assignment); e != nil {
		return
	}
	if owner != "r6" || assignment != r.ID {
		return out, cr.ErrConflict
	}
	body, _ := json.Marshal(r)
	_, e = tx.ExecContext(ctx, `INSERT INTO coordinate_review(id,revision,body) VALUES(?,1,?) ON CONFLICT(id) DO NOTHING`, r.ID, string(body))
	if e != nil {
		return
	}
	var b string
	e = tx.QueryRowContext(ctx, `SELECT body FROM coordinate_review WHERE id=?`, r.ID).Scan(&b)
	if e != nil {
		return
	}
	if json.Unmarshal([]byte(b), &out) != nil || !r.SameAssignment(out) {
		return cr.Record{}, cr.ErrConflict
	}
	e = tx.Commit()
	return
}
func (s *SQLite) CoordinateReviewIDs(ctx context.Context, after string, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		return nil, cr.ErrInvalid
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM coordinate_review WHERE id>? ORDER BY id LIMIT ?`, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (s *SQLite) MutateCoordinateReview(ctx context.Context, id string, fn func(*cr.Record) error) (out cr.Record, e error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return
	}
	defer tx.Rollback()
	var body string
	var version int
	e = tx.QueryRowContext(ctx, `SELECT revision,body FROM coordinate_review WHERE id=?`, id).Scan(&version, &body)
	if e == sql.ErrNoRows {
		return out, cr.ErrForbidden
	}
	if e != nil {
		return
	}
	var before cr.Record
	if json.Unmarshal([]byte(body), &before) != nil {
		return out, cr.ErrUnavailable
	}
	if json.Unmarshal([]byte(body), &out) != nil {
		return out, cr.ErrUnavailable
	}
	if e = fn(&out); e != nil {
		return
	}
	if out.ReadStage < before.ReadStage || out.ReadStage > 4 || len(out.Alerts) < len(before.Alerts) {
		return cr.Record{}, cr.ErrConflict
	}
	for i, a := range before.Alerts {
		if out.Alerts[i].Current != a.Current || (a.Dispatch != "queued" && a.Dispatch != "attempting" && out.Alerts[i] != a) {
			return cr.Record{}, cr.ErrConflict
		}
	}
	if !out.SameAssignment(before) || out.Created != before.Created || out.Validate() != nil {
		return cr.Record{}, cr.ErrConflict
	}
	if len(out.Results) < len(before.Results) || !reflect.DeepEqual(out.Results[:len(before.Results)], before.Results) || len(out.Comparisons) < len(before.Comparisons) || !reflect.DeepEqual(out.Comparisons[:len(before.Comparisons)], before.Comparisons) || len(out.Notices) < len(before.Notices) || len(out.Notices) > 3 {
		return cr.Record{}, cr.ErrConflict
	}
	for _, pair := range [][2]bool{{!before.Unable.IsZero(), before.Unable.Equal(out.Unable)}, {!before.Opened.IsZero(), before.Opened.Equal(out.Opened)}, {!before.Completed.IsZero(), before.Completed.Equal(out.Completed)}, {!before.Cancelled.IsZero(), before.Cancelled.Equal(out.Cancelled)}, {!before.Superseded.IsZero(), before.Superseded.Equal(out.Superseded)}} {
		if pair[0] && !pair[1] {
			return cr.Record{}, cr.ErrConflict
		}
	}
	for i, n := range before.Notices {
		if out.Notices[i].Slot != n.Slot || !out.Notices[i].At.Equal(n.At) || (n.State != "attempting" && !reflect.DeepEqual(out.Notices[i], n)) {
			return cr.Record{}, cr.ErrConflict
		}
	}
	b, x := json.Marshal(out)
	if x != nil || len(b) > 1<<20 {
		return cr.Record{}, cr.ErrUnavailable
	}
	result, e := tx.ExecContext(ctx, `UPDATE coordinate_review SET revision=revision+1,body=? WHERE id=? AND revision=?`, string(b), id, version)
	if e != nil {
		return out, e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return cr.Record{}, cr.ErrConflict
	}
	e = tx.Commit()
	return
}

// Mapping is independently administered; an Agent never inherits its owner's Git account.
func (s *SQLite) CoordinateReviewAgentBinding(ctx context.Context, id string) (*specread.Binding, error) {
	b := specread.Binding{Principal: cr.AgentActor(id), Provider: "github", Instance: "github.com"}
	e := s.db.QueryRowContext(ctx, `SELECT account_id,login FROM coordinate_review_agent_binding WHERE agent_id=?`, id).Scan(&b.AccountID, &b.Login)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !b.Valid() {
		return nil, cr.ErrUnavailable
	}
	return &b, nil
}
func (s *SQLite) SetCoordinateReviewAgentBinding(ctx context.Context, actor, id string, b *specread.Binding) error {
	if actor == "" || id == "" || len(id) > 128 || strings.ContainsAny(id, "\x00\r\n") || (b != nil && (!b.Valid() || b.Principal != cr.AgentActor(id))) {
		return cr.ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	op := "delete"
	if b == nil {
		_, e = tx.ExecContext(ctx, `DELETE FROM coordinate_review_agent_binding WHERE agent_id=?`, id)
	} else {
		op = "set"
		_, e = tx.ExecContext(ctx, `INSERT INTO coordinate_review_agent_binding(agent_id,account_id,login,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET account_id=excluded.account_id,login=excluded.login,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, id, b.AccountID, b.Login, actor, s.now())
	}
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO coordinate_review_binding_audit(agent_id,actor,operation,created_at) VALUES(?,?,?,?)`, id, actor, op, s.now())
	if e != nil {
		return e
	}
	return tx.Commit()
}

var _ cr.Store = (*SQLite)(nil)

// ClaimNotificationEvent is the common R6/R9 semantic arbitration point.
// Ownership never changes on retries or opening a channel's UI.
func (s *SQLite) ClaimNotificationEvent(ctx context.Context, event, owner, assignment string) error {
	if len(event) < 16 || len(event) > 128 || assignment == "" || (owner != "r6" && owner != "r9") {
		return cr.ErrInvalid
	}
	_, e := s.db.ExecContext(ctx, `INSERT INTO notification_event_owner(event_id,semantic_owner,assignment) VALUES(?,?,?) ON CONFLICT(event_id) DO NOTHING`, event, owner, assignment)
	if e != nil {
		return e
	}
	var o, a string
	if e = s.db.QueryRowContext(ctx, `SELECT semantic_owner,assignment FROM notification_event_owner WHERE event_id=?`, event).Scan(&o, &a); e != nil {
		return e
	}
	if o != owner || a != assignment {
		return cr.ErrConflict
	}
	return nil
}

// Same canonical DB identity as the existing freeze lock, a separate namespace.
// Never holds SQLite's sole connection while invoking source/transport callbacks.
func (s *SQLite) BeginCoordinateReview(ctx context.Context) (func(), error) {
	if s.legacySpecLockPath != "" {
		return lockLegacySpecFile(ctx, s.legacySpecLockPath+".coordinate-review", true)
	}
	for !s.coordinateReviewMu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	return s.coordinateReviewMu.Unlock, nil
}
