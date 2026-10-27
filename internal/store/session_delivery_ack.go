package store

import (
	"context"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var (
	ErrDeliveryACKIdentity   = errors.New("delivery ACK protocol, mode or generation mismatch")
	ErrDeliveryACKTransition = errors.New("delivery ACK lacks a valid lifecycle predecessor")
)

// BindSessionDeliveryACK freezes the protocol on first dispatch. Connection
// replacement never relabels an existing row, including a historical v1 row.
func (s *SQLite) BindSessionDeliveryACK(ctx context.Context, id, keyID, session string, protocol int, mode string) (*model.SessionDelivery, error) {
	if id == "" || keyID == "" || session == "" || (protocol != 1 && protocol != 2) || (protocol == 2 && mode != "driver") || (mode != "driver" && mode != "desktop" && mode != "legacy") {
		return nil, ErrDeliveryACKIdentity
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	d, err := scanSessionDelivery(tx.QueryRowContext(ctx, sessionDeliverySelect+` WHERE id=? AND key_id=? AND session_name=?`, id, keyID, session))
	if err != nil || d == nil {
		return nil, ErrDeliveryACKIdentity
	}
	if d.AckProtocol == 0 {
		if d.State != "sent" && d.State != "queued" {
			return nil, ErrDeliveryACKTransition
		}
		// A previously attempted, untagged delivery belongs to legacy semantics.
		if d.Attempts > 1 {
			protocol, mode = 1, "legacy"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE session_delivery SET ack_protocol=?,processing_mode=?,ack_generation=1 WHERE id=? AND ack_protocol IS NULL`, protocol, mode, id); err != nil {
			return nil, err
		}
		d.AckProtocol, d.ProcessingMode, d.AckGeneration = protocol, mode, 1
	}
	if d.AckGeneration < 1 || d.ProcessingMode == "" {
		return nil, ErrDeliveryACKIdentity
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}

// ApplySessionDeliveryACK commits a phase before the Hub confirms it. Replayed
// observations preserve original helper times and cannot overwrite a turn.
func (s *SQLite) ApplySessionDeliveryACK(ctx context.Context, a model.SessionDeliveryACK, now time.Time) (*model.SessionDelivery, bool, error) {
	if a.Protocol != 2 || a.ProcessingMode != "driver" || a.Generation < 1 || a.OccurredAt.IsZero() {
		return nil, false, ErrDeliveryACKIdentity
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	d, err := scanSessionDelivery(tx.QueryRowContext(ctx, sessionDeliverySelect+` WHERE id=?`, a.DeliveryID))
	if err != nil {
		return nil, false, err
	}
	if d == nil || d.KeyID != a.KeyID || d.SessionName != a.SessionName || d.AckProtocol != a.Protocol || d.ProcessingMode != a.ProcessingMode || d.AckGeneration != a.Generation {
		return nil, false, ErrDeliveryACKIdentity
	}
	if d.State == "cancelled" || d.State == "legacy_received" || (d.State == "dead" && !(a.Phase == "failed" && d.LastError == "session_unavailable")) {
		return nil, false, ErrDeliveryACKTransition
	}
	changed := false
	stamp := now.UTC().Format(time.RFC3339Nano)
	actual := a.OccurredAt.UTC().Format(time.RFC3339Nano)
	switch a.Phase {
	case "queued":
		if a.TurnID != "" || a.TurnOutcome != "" {
			return nil, false, ErrDeliveryACKIdentity
		}
		if d.State == "sent" || d.State == "queued" {
			if _, err := tx.ExecContext(ctx, `UPDATE session_delivery SET state='delivered',delivered_at=COALESCE(delivered_at,?),lease_until=NULL,last_error='',updated_at=? WHERE id=?`, stamp, stamp, d.ID); err != nil {
				return nil, false, err
			}
			changed = true
		} else if d.DeliveredAt == nil {
			return nil, false, ErrDeliveryACKTransition
		}
	case "injected":
		if a.TurnID == "" || a.TurnOutcome != "" || d.DeliveredAt == nil {
			return nil, false, ErrDeliveryACKTransition
		}
		if d.InjectedAt != nil {
			if d.TurnID != a.TurnID || !d.InjectedAt.Equal(a.OccurredAt) {
				return nil, false, ErrDeliveryACKIdentity
			}
		} else {
			if d.State != "delivered" && d.State != "admission_unknown" {
				return nil, false, ErrDeliveryACKTransition
			}
			if _, err := tx.ExecContext(ctx, `UPDATE session_delivery SET state='injected',injected_at=?,turn_id=?,last_error='',failed_count=0,lease_until=NULL,updated_at=? WHERE id=?`, actual, a.TurnID, stamp, d.ID); err != nil {
				return nil, false, err
			}
			changed = true
		}
	case "processed":
		if d.InjectedAt == nil || a.TurnID == "" || d.TurnID != a.TurnID || a.OccurredAt.Before(*d.InjectedAt) || (a.TurnOutcome != "completed" && a.TurnOutcome != "interrupted" && a.TurnOutcome != "failed") {
			return nil, false, ErrDeliveryACKTransition
		}
		if d.State == "processed" {
			if d.TurnOutcome != a.TurnOutcome || d.ProcessedAt == nil || !d.ProcessedAt.Equal(a.OccurredAt) {
				return nil, false, ErrDeliveryACKIdentity
			}
		} else {
			if d.State != "injected" {
				return nil, false, ErrDeliveryACKTransition
			}
			if _, err := tx.ExecContext(ctx, `UPDATE session_delivery SET state='processed',processed_at=?,turn_outcome=?,lease_until=NULL,last_error='',failed_count=0,updated_at=? WHERE id=?`, actual, a.TurnOutcome, stamp, d.ID); err != nil {
				return nil, false, err
			}
			changed = true
		}
	case "admission_unknown":
		if a.TurnID != "" || a.TurnOutcome != "" || d.DeliveredAt == nil {
			return nil, false, ErrDeliveryACKTransition
		}
		if d.State == "delivered" {
			if _, err := tx.ExecContext(ctx, `UPDATE session_delivery SET state='admission_unknown',last_error='model_admission_unknown',lease_until=NULL,updated_at=? WHERE id=?`, stamp, d.ID); err != nil {
				return nil, false, err
			}
			changed = true
		} else if d.State == "injected" && d.LastError != "turn_terminal_unknown" {
			// Transport watchdogs cannot erase a proven admission or invent a
			// model terminal. Keep the stage and expose only the uncertainty.
			if _, err := tx.ExecContext(ctx, `UPDATE session_delivery SET last_error='turn_terminal_unknown',updated_at=? WHERE id=?`, stamp, d.ID); err != nil {
				return nil, false, err
			}
			changed = true
		} else if d.State != "admission_unknown" && d.InjectedAt == nil {
			return nil, false, ErrDeliveryACKTransition
		}
	case "failed":
		// This is a positively rejected dispatch, never a failed model turn.
		// A turn failure must follow injected and use processed/outcome=failed.
		if d.InjectedAt != nil || d.DeliveredAt == nil || a.TurnID != "" || a.TurnOutcome != "" {
			return nil, false, ErrDeliveryACKTransition
		}
		if d.State == "delivered" {
			if _, err := tx.ExecContext(ctx, `UPDATE session_delivery SET state='dead',last_error='session_unavailable',lease_until=NULL,updated_at=? WHERE id=?`, stamp, d.ID); err != nil {
				return nil, false, err
			}
			changed = true
		} else if d.State != "dead" || d.LastError != "session_unavailable" {
			return nil, false, ErrDeliveryACKTransition
		}
	default:
		return nil, false, ErrDeliveryACKTransition
	}
	result, err := scanSessionDelivery(tx.QueryRowContext(ctx, sessionDeliverySelect+` WHERE id=?`, d.ID))
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return result, changed, nil
}
