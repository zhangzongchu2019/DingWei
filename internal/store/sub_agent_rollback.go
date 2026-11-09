package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/zhangzongchu2019/dingwei/internal/subagent"
	"time"
)

type SubAgentRollbackResult struct{ Returned, Unknown, Terminal int }

// Offline maintenance only. Callers must stop writers, disable capability and
// seal/drain helper lanes first. A durable marker blocks new tree connections.
// Unknown delivery rows retain state/ACK and move out of all legacy root queues.
func (s *SQLite) PrepareSubAgentRollback(ctx context.Context, root string) (SubAgentRollbackResult, error) {
	out := SubAgentRollbackResult{}
	if root == "" {
		return out, errors.New("root required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT d.id,r.route_json FROM session_delivery d JOIN session_delivery_sub_route r ON r.delivery_id=d.id`)
	if err != nil {
		return out, err
	}
	ids := []string{}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return out, err
		}
		var route subagent.DeliveryRoute
		if json.Unmarshal([]byte(raw), &route) != nil {
			rows.Close()
			return out, errors.New("invalid retained route")
		}
		if route.To != nil && route.To.RootID == root {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		var prior int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sub_agent_rollback_delivery WHERE delivery_id=?`, id).Scan(&prior); err != nil {
			return out, err
		}
		if prior != 0 {
			continue
		}
		d, e := scanSessionDelivery(tx.QueryRowContext(ctx, sessionDeliverySelect+` WHERE id=?`, id))
		if e != nil {
			return out, e
		}
		class := "unknown"
		switch d.State {
		case "processed", "dead", "cancelled", "legacy_received":
			class = "terminal"
			out.Terminal++
		default:
			if d.State == "queued" && d.Attempts == 0 {
				class = "not_executed"
				sent, e := enqueueDeliveryFailureNoticeTx(ctx, tx, *d, "sub_agent_ended", s.now())
				if sent {
					out.Returned++
				}
				err = e
				if err != nil {
					return out, err
				}
				if _, err = tx.ExecContext(ctx, `UPDATE session_delivery SET state='dead',last_error='sub_agent_ended',lease_until=NULL WHERE id=?`, id); err != nil {
					return out, err
				}
			} else {
				out.Unknown++
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sub_agent_rollback_delivery VALUES(?,?,?,?,?)`, id, root, d.KeyID, d.SessionName, class); err != nil {
			return out, err
		}
		// Even terminal rows stay isolated: a legacy retry/maintenance query cannot
		// accidentally turn a child row back into a root delivery.
		if _, err = tx.ExecContext(ctx, `UPDATE session_delivery SET session_name=? WHERE id=?`, d.SessionName+"\x1fsub-quarantine:"+root, id); err != nil {
			return out, err
		}
	}
	// A return can itself target another child of the same sealed root. Retain
	// that notice as isolated audit, never hand it to the old root claimer.
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO sub_agent_rollback_delivery
        SELECT d.id,?,d.key_id,d.session_name,'unknown' FROM session_delivery d
        JOIN session_delivery_sub_route r ON r.delivery_id=d.id
        WHERE json_extract(r.route_json,'$.to.root_agent_id')=?`, root, root); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_delivery SET session_name=(SELECT original_session_name FROM sub_agent_rollback_delivery a WHERE a.delivery_id=session_delivery.id)||? WHERE id IN (SELECT delivery_id FROM sub_agent_rollback_delivery WHERE root_agent_id=?)`, "\x1fsub-quarantine:"+root, root); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sub_agent_registry SET state='draining',reason='operator_closed' WHERE root_agent_id=? AND node_id<>'' AND state NOT IN ('draining','ended')`, root); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO sub_agent_rollback VALUES(?,?)`, root, s.now().UTC().Format(time.RFC3339Nano)); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
