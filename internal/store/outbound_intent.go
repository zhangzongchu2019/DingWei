package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var ErrOutboundConflict = errors.New("outbound idempotency_conflict")
var ErrOutboundCapacity = errors.New("outbound ledger capacity")

// The helper stops automatic recovery at 24h. Two extra hours cover the
// documented clock-skew/restart/in-flight margin; production defaults to 7d.
const OutboundHelperRecoveryWindow = 24 * time.Hour
const OutboundRetentionSafetyMargin = 2 * time.Hour
const DefaultOutboundIntentRetention = 7 * 24 * time.Hour
const MaxOutboundIntentRetention = 30 * 24 * time.Hour
const OutboundIntentCleanupBatch = 256
const outboundIntentStamp = "2006-01-02T15:04:05.000000000Z"

func (s *SQLite) ConfigureOutboundIntentRetention(raw string) error {
	retention := DefaultOutboundIntentRetention
	if strings.TrimSpace(raw) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return errors.New("WP_OUTBOUND_INTENT_RETENTION must be a duration greater than 26h and at most 720h")
		}
		retention = parsed
	}
	if retention <= OutboundHelperRecoveryWindow+OutboundRetentionSafetyMargin || retention > MaxOutboundIntentRetention {
		return errors.New("WP_OUTBOUND_INTENT_RETENTION must exceed the 24h helper window plus 2h safety margin and not exceed 720h")
	}
	s.outboundRetentionNS.Store(int64(retention))
	return nil
}
func (s *SQLite) OutboundIntentRetention() time.Duration {
	if retention := s.outboundRetentionNS.Load(); retention > 0 {
		return time.Duration(retention)
	}
	return DefaultOutboundIntentRetention
}

// A fence is also the authority for stable identity at physical delivery and
// for an associated pending HUB failure notice. Do not erase that authority
// merely because a paused/unknown delivery outlived the automatic replay window.
func cleanupOutboundIntentsTx(ctx context.Context, tx *sql.Tx, cutoff string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM outbound_intent WHERE rowid IN (
  SELECT o.rowid FROM outbound_intent o WHERE o.created_at < ?
  AND NOT EXISTS (SELECT 1 FROM session_delivery d WHERE d.id=o.delivery_id AND d.state NOT IN ('processed','dead','cancelled','legacy_received'))
  AND NOT EXISTS (SELECT 1 FROM session_delivery n WHERE n.source_type='failure_notice' AND n.source_message_id=o.delivery_id AND n.state NOT IN ('processed','dead','cancelled','legacy_received'))
  ORDER BY o.created_at,o.sender_id,o.source_message_id LIMIT ?
 )`, cutoff, OutboundIntentCleanupBatch)
	return err
}

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
// concurrent connections, or delivery payload expiry during fence retention.
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
	// Use one server acceptance clock and one cutoff throughout this transaction.
	// Admission only counts fences still inside the retention window. Cleanup is
	// bounded and atomic with acceptance; an unrelated queue failure rolls it back.
	acceptedAt := s.now().UTC()
	cutoff := acceptedAt.Add(-s.OutboundIntentRetention()).Format(outboundIntentStamp)
	if err = cleanupOutboundIntentsTx(ctx, tx, cutoff); err != nil {
		return nil, false, err
	}
	var own, total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM outbound_intent WHERE sender_id=? AND created_at>=?`, v.SenderID, cutoff).Scan(&own); err != nil {
		return nil, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM outbound_intent WHERE created_at>=?`, cutoff).Scan(&total); err != nil {
		return nil, false, err
	}
	if own >= 4096 || total >= 65536 {
		return nil, false, ErrOutboundCapacity
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = acceptedAt
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
	v.CreatedAt = acceptedAt
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbound_intent (`+outboundIntentColumns+`) VALUES(?,?,?,?,?,?)`, v.SenderID, v.SourceMessageID, v.TargetID, v.Digest, v.DeliveryID, v.CreatedAt.UTC().Format(outboundIntentStamp)); err != nil {
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
