package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var ErrOutboundConflict = errors.New("outbound idempotency_conflict")
var ErrOutboundCapacity = errors.New("outbound ledger capacity")

const outboundIntentColumns = `sender_id,source_message_id,target_id,digest,delivery_id,created_at`

func scanOutboundIntent(row rowScanner) (*model.OutboundIntent, error) {
	var v model.OutboundIntent
	var created string
	err := row.Scan(&v.SenderID, &v.SourceMessageID, &v.TargetID, &v.Digest, &v.DeliveryID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return &v, err
}
func (s *SQLite) GetOutboundIntent(ctx context.Context, sender, source string) (*model.OutboundIntent, error) {
	return scanOutboundIntent(s.db.QueryRowContext(ctx, `SELECT `+outboundIntentColumns+` FROM outbound_intent WHERE sender_id=? AND source_message_id=?`, sender, source))
}

// CommitOutboundSessionDelivery binds acceptance and the only recipient queue
// item in one transaction. Its uniqueness remains authoritative after restart,
// concurrent connections, or normal delivery payload retention/expiry.
func (s *SQLite) CommitOutboundSessionDelivery(ctx context.Context, v model.OutboundIntent, d model.SessionDelivery) (*model.OutboundIntent, bool, error) {
	if v.SenderID == "" || v.SourceMessageID == "" || v.TargetID == "" || len(v.Digest) != 64 || d.ID == "" || d.SubRoute != nil || d.SourceMessageID != v.SourceMessageID {
		return nil, false, errors.New("invalid outbound intent")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	old, err := scanOutboundIntent(tx.QueryRowContext(ctx, `SELECT `+outboundIntentColumns+` FROM outbound_intent WHERE sender_id=? AND source_message_id=?`, v.SenderID, v.SourceMessageID))
	if err != nil {
		return nil, false, err
	}
	if old != nil {
		if old.TargetID != v.TargetID || old.Digest != v.Digest {
			return nil, false, ErrOutboundConflict
		}
		return old, false, nil
	}
	// These fences have no automatic GC. Stop new admissions rather than erase
	// uncertainty or reopen IDs. Maintenance requires a separately reviewed plan.
	var own, total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM outbound_intent WHERE sender_id=?`, v.SenderID).Scan(&own); err != nil {
		return nil, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM outbound_intent`).Scan(&total); err != nil {
		return nil, false, err
	}
	if own >= 4096 || total >= 65536 {
		return nil, false, ErrOutboundCapacity
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = s.now().UTC()
	}
	if d.NextAttemptAt.IsZero() {
		d.NextAttemptAt = d.CreatedAt
	}
	if d.State == "" {
		d.State = "queued"
	}
	if d.SourceType == "" {
		d.SourceType = "agent_route"
	}
	v.DeliveryID = d.ID
	v.CreatedAt = d.CreatedAt
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbound_intent (`+outboundIntentColumns+`) VALUES(?,?,?,?,?,?)`, v.SenderID, v.SourceMessageID, v.TargetID, v.Digest, v.DeliveryID, v.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO session_delivery(id,key_id,session_name,envelope_json,source_message_id,source_type,provenance,state,attempts,next_attempt_at,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID, d.KeyID, d.SessionName, d.Envelope, d.SourceMessageID, d.SourceType, nullString(d.Provenance), d.State, d.Attempts, d.NextAttemptAt.UTC().Format(time.RFC3339Nano), d.LastError, d.CreatedAt.UTC().Format(time.RFC3339Nano), d.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return &v, true, nil
}

func (s *SQLite) GetOutboundIntentForDelivery(ctx context.Context, deliveryID string) (*model.OutboundIntent, error) {
	return scanOutboundIntent(s.db.QueryRowContext(ctx, `SELECT `+outboundIntentColumns+` FROM outbound_intent WHERE delivery_id=?`, deliveryID))
}
