package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const FeishuStickyTTL = 30 * time.Minute
const FeishuStickyPendingTTL = 10 * time.Minute
const FeishuStickyCleanupBatch = 200

var ErrStickyGeneration = errors.New("sticky generation changed")

type FeishuStickyKey struct{ UserIDLC, BotChannelID string }
type FeishuStickyState struct {
	FeishuStickyKey
	AgentID                          string
	Generation                       int64
	SelectedAt, ExpiresAt, UpdatedAt time.Time
	LastSourceMessageID              string
}

func (s FeishuStickyState) Valid(now time.Time) bool {
	return !now.Before(s.SelectedAt) && now.Before(s.ExpiresAt)
}

type FeishuStickyOperation struct {
	FeishuStickyKey
	Seq, BaseGeneration, ResultGeneration                                                int64
	SourceMessageID, Kind, TargetAgentID, Decision, Status, DeliveryID, SourceEnvelopeID string
	CreatedAt, CompletedAt                                                               time.Time
}

// Optional capability: legacy Repository implementations need not simulate R14.
type FeishuStickyRepository interface {
	GetFeishuSticky(context.Context, FeishuStickyKey) (*FeishuStickyState, error)
	BeginFeishuSticky(context.Context, FeishuStickyKey, string, string, time.Time) (FeishuStickyOperation, bool, error)
	FinishFeishuSticky(context.Context, FeishuStickyOperation, string, string, string, time.Time) error
	ListFeishuStickyUnresolved(context.Context, FeishuStickyKey, time.Time) ([]FeishuStickyOperation, error)
	ResolveFeishuSticky(context.Context, int64, time.Time) error
	FindFeishuStickyDelivery(context.Context, FeishuStickyOperation) (string, error)
	SweepFeishuStickyPending(context.Context, time.Time, int) (int64, error)
	RecordFeishuStickyIntent(context.Context, FeishuStickyOperation, string, string, string) error
	ListFeishuStickyStates(context.Context, int, FeishuStickyKey) ([]FeishuStickyState, error)
	DeleteFeishuSticky(context.Context, FeishuStickyState) error
	CleanupFeishuSticky(context.Context, time.Time, int) (int64, error)
}

// Fixed width UTC representation preserves lexical ordering across subsecond values.
func stickyTime(t time.Time) string  { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func stickyParse(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }
func scanSticky(row interface{ Scan(...any) error }) (*FeishuStickyState, error) {
	var s FeishuStickyState
	var a, b, c string
	err := row.Scan(&s.UserIDLC, &s.BotChannelID, &s.AgentID, &s.Generation, &a, &b, &s.LastSourceMessageID, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.SelectedAt, err = time.Parse(time.RFC3339Nano, a)
	if err != nil {
		return nil, err
	}
	s.ExpiresAt, err = time.Parse(time.RFC3339Nano, b)
	if err != nil {
		return nil, err
	}
	s.UpdatedAt, err = time.Parse(time.RFC3339Nano, c)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

const stickyStateCols = "user_id_lc,bot_channel_id,agent_id,generation,selected_at_utc,expires_at_utc,last_source_message_id,updated_at_utc"

func (s *SQLite) GetFeishuSticky(ctx context.Context, k FeishuStickyKey) (*FeishuStickyState, error) {
	return scanSticky(s.db.QueryRowContext(ctx, "SELECT "+stickyStateCols+" FROM r14_feishu_sticky_state WHERE user_id_lc=? AND bot_channel_id=?", k.UserIDLC, k.BotChannelID))
}

const stickyOpCols = "user_id_lc,bot_channel_id,operation_seq,source_message_id,operation_kind,base_generation,result_generation,target_agent_id,decision,status,delivery_id,source_envelope_id,created_at_utc,completed_at_utc"

func scanStickyOp(row interface{ Scan(...any) error }) (FeishuStickyOperation, error) {
	var o FeishuStickyOperation
	var a, b string
	err := row.Scan(&o.UserIDLC, &o.BotChannelID, &o.Seq, &o.SourceMessageID, &o.Kind, &o.BaseGeneration, &o.ResultGeneration, &o.TargetAgentID, &o.Decision, &o.Status, &o.DeliveryID, &o.SourceEnvelopeID, &a, &b)
	o.CreatedAt = stickyParse(a)
	o.CompletedAt = stickyParse(b)
	return o, err
}
func (s *SQLite) BeginFeishuSticky(ctx context.Context, k FeishuStickyKey, source, kind string, now time.Time) (FeishuStickyOperation, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FeishuStickyOperation{}, false, err
	}
	defer tx.Rollback()
	// Same-source uniqueness is channel-wide, including an identity remapping.
	old, err := scanStickyOp(tx.QueryRowContext(ctx, "SELECT "+stickyOpCols+" FROM r14_feishu_sticky_operation WHERE bot_channel_id=? AND source_message_id=?", k.BotChannelID, source))
	if err == nil {
		return old, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return old, false, err
	}
	state, err := scanSticky(tx.QueryRowContext(ctx, "SELECT "+stickyStateCols+" FROM r14_feishu_sticky_state WHERE user_id_lc=? AND bot_channel_id=?", k.UserIDLC, k.BotChannelID))
	if err != nil {
		return old, false, err
	}
	o := FeishuStickyOperation{FeishuStickyKey: k, SourceMessageID: source, Kind: kind, Status: "pending", CreatedAt: now}
	if state != nil {
		o.BaseGeneration = state.Generation
	} else {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(MAX(base_generation,result_generation)),0) FROM r14_feishu_sticky_operation WHERE user_id_lc=? AND bot_channel_id=?`, k.UserIDLC, k.BotChannelID).Scan(&o.BaseGeneration); err != nil {
			return o, false, err
		}
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO r14_feishu_sticky_operation(user_id_lc,bot_channel_id,source_message_id,operation_kind,base_generation,status,created_at_utc) VALUES(?,?,?,?,?,'pending',?)`, k.UserIDLC, k.BotChannelID, source, kind, o.BaseGeneration, stickyTime(now))
	if err != nil {
		return o, false, err
	}
	o.Seq, err = r.LastInsertId()
	if err != nil {
		return o, false, err
	}
	return o, true, tx.Commit()
}
func (s *SQLite) FinishFeishuSticky(ctx context.Context, o FeishuStickyOperation, status, target, decision string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM r14_feishu_sticky_operation WHERE operation_seq=? AND user_id_lc=? AND bot_channel_id=?", o.Seq, o.UserIDLC, o.BotChannelID).Scan(&current); err != nil {
		return err
	}
	if current != "pending" {
		return ErrStickyGeneration
	}
	resultGen := int64(0)
	if status == "accepted" {
		r, e := tx.ExecContext(ctx, `INSERT INTO r14_feishu_sticky_state(`+stickyStateCols+`) SELECT ?,?,?,?,?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM r14_feishu_sticky_operation WHERE user_id_lc=? AND bot_channel_id=? AND result_generation>?) ON CONFLICT(user_id_lc,bot_channel_id) DO NOTHING`, o.UserIDLC, o.BotChannelID, target, o.BaseGeneration+1, stickyTime(now), stickyTime(now.Add(FeishuStickyTTL)), o.SourceMessageID, stickyTime(now), o.UserIDLC, o.BotChannelID, o.BaseGeneration)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			r, e = tx.ExecContext(ctx, `UPDATE r14_feishu_sticky_state SET agent_id=?,generation=generation+1,selected_at_utc=?,expires_at_utc=?,last_source_message_id=?,updated_at_utc=? WHERE user_id_lc=? AND bot_channel_id=? AND generation=?`, target, stickyTime(now), stickyTime(now.Add(FeishuStickyTTL)), o.SourceMessageID, stickyTime(now), o.UserIDLC, o.BotChannelID, o.BaseGeneration)
			if e != nil {
				return e
			}
			n, e = r.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				status = "stale_completion"
			}
		}
		if status == "accepted" {
			resultGen = o.BaseGeneration + 1
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE r14_feishu_sticky_operation SET status=?,target_agent_id=?,decision=?,result_generation=?,delivery_id=?,completed_at_utc=? WHERE operation_seq=? AND status='pending'`, status, target, decision, resultGen, o.DeliveryID, stickyTime(now), o.Seq)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if status == "stale_completion" {
		return ErrStickyGeneration
	}
	return nil
}
func (s *SQLite) ListFeishuStickyUnresolved(ctx context.Context, k FeishuStickyKey, now time.Time) ([]FeishuStickyOperation, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE r14_feishu_sticky_operation SET status='unknown',completed_at_utc=? WHERE user_id_lc=? AND bot_channel_id=? AND status='pending' AND created_at_utc<=?`, stickyTime(now), k.UserIDLC, k.BotChannelID, stickyTime(now.Add(-FeishuStickyPendingTTL)))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+stickyOpCols+" FROM r14_feishu_sticky_operation WHERE user_id_lc=? AND bot_channel_id=? AND status IN ('pending','unknown') ORDER BY operation_seq", k.UserIDLC, k.BotChannelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeishuStickyOperation
	for rows.Next() {
		o, e := scanStickyOp(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *SQLite) ResolveFeishuSticky(ctx context.Context, seq int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE r14_feishu_sticky_operation SET status='resolved',completed_at_utc=? WHERE operation_seq=? AND status IN ('pending','unknown')`, stickyTime(now), seq)
	return err
}
func (s *SQLite) ListFeishuStickyStates(ctx context.Context, limit int, after FeishuStickyKey) ([]FeishuStickyState, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+stickyStateCols+" FROM r14_feishu_sticky_state WHERE (user_id_lc,bot_channel_id)>(?,?) ORDER BY user_id_lc,bot_channel_id LIMIT ?", after.UserIDLC, after.BotChannelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeishuStickyState
	for rows.Next() {
		v, e := scanSticky(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func (s *SQLite) DeleteFeishuSticky(ctx context.Context, v FeishuStickyState) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM r14_feishu_sticky_state WHERE user_id_lc=? AND bot_channel_id=? AND generation=?`, v.UserIDLC, v.BotChannelID, v.Generation)
	return err
}
func (s *SQLite) CleanupFeishuSticky(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > FeishuStickyCleanupBatch {
		limit = FeishuStickyCleanupBatch
	}
	cutoff := stickyTime(now.Add(-30 * 24 * time.Hour))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `DELETE FROM r14_feishu_sticky_state WHERE rowid IN (SELECT rowid FROM r14_feishu_sticky_state WHERE updated_at_utc<=? ORDER BY updated_at_utc LIMIT ?)`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	n, _ := r.RowsAffected()
	r, err = tx.ExecContext(ctx, `DELETE FROM r14_feishu_sticky_operation WHERE operation_seq IN (SELECT operation_seq FROM r14_feishu_sticky_operation WHERE status NOT IN ('pending','unknown') AND completed_at_utc<=? ORDER BY completed_at_utc LIMIT ?)`, cutoff, int64(limit)-n)
	if err != nil {
		return 0, err
	}
	m, _ := r.RowsAffected()
	return n + m, tx.Commit()
}

// Metadata checkpoints never make a binding active. A crash leaves a blocked key.
func (s *SQLite) RecordFeishuStickyIntent(ctx context.Context, o FeishuStickyOperation, target, decision, delivery string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE r14_feishu_sticky_operation SET target_agent_id=?,decision=?,delivery_id=?,source_envelope_id=? WHERE operation_seq=? AND status='pending'`, target, decision, delivery, o.SourceEnvelopeID, o.Seq)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStickyGeneration
	}
	return nil
}

// Recover the enqueue-before-checkpoint crash window using the preserved
// source envelope identity and both R14 authority metadata fields.
func (s *SQLite) FindFeishuStickyDelivery(ctx context.Context, o FeishuStickyOperation) (string, error) {
	if o.SourceEnvelopeID == "" || o.TargetAgentID == "" {
		return "", nil
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM session_delivery WHERE source_message_id=? AND json_extract(envelope_json,'$.meta.r14_target_agent_id')=? AND json_extract(envelope_json,'$.meta.r14_principal_lc')=? ORDER BY created_at LIMIT 1`, o.SourceEnvelopeID, o.TargetAgentID, o.UserIDLC).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
func (s *SQLite) SweepFeishuStickyPending(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > FeishuStickyCleanupBatch {
		limit = FeishuStickyCleanupBatch
	}
	r, err := s.db.ExecContext(ctx, `UPDATE r14_feishu_sticky_operation SET status='unknown',completed_at_utc=? WHERE operation_seq IN (SELECT operation_seq FROM r14_feishu_sticky_operation WHERE status='pending' AND created_at_utc<=? ORDER BY operation_seq LIMIT ?)`, stickyTime(now), stickyTime(now.Add(-FeishuStickyPendingTTL)), limit)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
