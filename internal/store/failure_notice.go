package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// enqueueDeliveryFailureNoticeTx commits with the original terminal transition.
// Only previously ACK-capable session senders are eligible for durable notices.
// It never copies an original body, arbitrary last_error, target or credentials.
func enqueueDeliveryFailureNoticeTx(ctx context.Context, tx *sql.Tx, d model.SessionDelivery, reason string, now time.Time) (bool, error) {
	if !model.DeliveryFailureReasonAllowed(reason) || d.SourceType != "session_inbound" || d.Provenance == model.FailureNoticeProvenance {
		return false, nil
	}
	var original model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &original) != nil {
		return false, nil
	}
	source := strings.Split(strings.TrimSpace(original.From), "#")
	if len(source) != 2 || source[0] == "" || source[1] == "" || source[0] == "workpulse" {
		return false, nil
	}
	var ack int
	err := tx.QueryRowContext(ctx, `SELECT delivery_ack_v1 FROM session_endpoint WHERE key_id=? AND session_name=?`, source[1], source[0]).Scan(&ack)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ack != 1 {
		return false, nil
	}
	id := model.FailureNoticeDeliveryID(d.ID)
	notice := model.Envelope{ID: id, From: "workpulse#" + source[1], To: original.From, TS: now.Unix(),
		Body: "【系统投递失败通知】\n原消息未能完成投递；原因码：" + reason + "。本通知不要求回复。",
		Meta: map[string]any{"type": "delivery", "delivery_id": id, "delivery_version": 1, "ack_required": true,
			"source_message_id": d.ID, "failed_message_id": original.ID, "failed_delivery_id": d.ID, "reason_code": reason,
			"kind": "reply", "reply_policy": "no_reply", "conversation_terminal": true, "suppress_reply_origin": true, "no_mirror": true}}
	raw, err := json.Marshal(notice)
	if err != nil {
		return false, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	r, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO session_delivery
 (id,key_id,session_name,envelope_json,source_message_id,source_type,provenance,state,attempts,next_attempt_at,last_error,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,'queued',0,?,'',?,?)`, id, source[1], source[0], string(raw), d.ID, "failure_notice", model.FailureNoticeProvenance, stamp, stamp, stamp)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

func collectFailureRows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]model.SessionDelivery, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.SessionDelivery
	for rows.Next() {
		d, err := scanSessionDelivery(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *d)
	}
	return result, rows.Err()
}

// SweepSessionDeliveryFailures includes every expirable nonterminal state,
// even if neither endpoint ever reconnects. Backfill is limited to explicit
// allowlisted reasons and the last 30 minutes of terminal-transition time.
// No old timestamp or ambiguous arbitrary retry error is guessed into scope.
func (s *SQLite) SweepSessionDeliveryFailures(ctx context.Context, now time.Time) ([]model.SessionDelivery, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	cutoff := now.Add(-30 * time.Minute).UTC().Format(time.RFC3339Nano)
	rows, err := collectFailureRows(ctx, tx, `UPDATE session_delivery SET state='dead',lease_until=NULL,updated_at=?,
 last_error=CASE WHEN state='capability_pending' AND provenance='capability_pending_ack_v1' THEN 'delivery_capability_unconfirmed_expired' ELSE 'delivery_expired' END
 WHERE id IN (SELECT id FROM session_delivery WHERE state NOT IN ('processed','dead','cancelled') AND (attempts>=12 OR created_at<=?) ORDER BY created_at,id LIMIT 1000)
 RETURNING id,key_id,session_name,envelope_json,COALESCE(source_message_id,''),source_type,provenance,state,attempts,next_attempt_at,lease_until,COALESCE(last_error,''),created_at,delivered_at,processed_at`, stamp, cutoff)
	if err != nil {
		return nil, err
	}
	for _, d := range rows {
		if _, err := enqueueDeliveryFailureNoticeTx(ctx, tx, d, d.LastError, now); err != nil {
			return nil, err
		}
	}
	recent, err := collectFailureRows(ctx, tx, sessionDeliverySelect+` WHERE state='dead' AND updated_at>=? AND updated_at<=? AND last_error IN ('retry_exhausted','delivery_ack_unsupported','delivery_capability_unconfirmed_expired','delivery_expired')
 AND source_type='session_inbound' AND EXISTS (SELECT 1 FROM session_endpoint ep WHERE ep.delivery_ack_v1=1 AND ep.session_name<>'workpulse' AND (ep.session_name || '#' || ep.key_id)=json_extract(CASE WHEN json_valid(envelope_json) THEN envelope_json ELSE '{}' END,'$.from'))
 AND NOT EXISTS (SELECT 1 FROM session_delivery n WHERE n.source_type='failure_notice' AND n.source_message_id=session_delivery.id) ORDER BY updated_at,id LIMIT 1000`, cutoff, stamp)
	// Dedup is the deterministic primary key in the INSERT, not an in-memory flag.
	if err != nil {
		return nil, err
	}
	for _, d := range recent {
		created, err := enqueueDeliveryFailureNoticeTx(ctx, tx, d, d.LastError, now)
		if err != nil {
			return nil, err
		}
		if created {
			rows = append(rows, d)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rows, nil
}
