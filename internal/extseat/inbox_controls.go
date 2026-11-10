package extseat

import (
	"context"
	"database/sql"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"time"
)

type InboxSummary struct {
	DeliveryID string    `json:"delivery_id"`
	From       string    `json:"from"`
	CreatedAt  time.Time `json:"created_at"`
	BodyBytes  int       `json:"body_bytes"`
}

// readPending is enforced read-only by PG, including group/auth callbacks.
// No body, lease, presence, token renewal or TouchGroup is written here.
func (s *Service) readPending(ctx context.Context, a Seat, since *time.Time, eligible bool) ([]InboxSummary, error) {
	out := []InboxSummary{}
	reader, ok := s.Groups.(interface {
		ReadGroup(context.Context, repo.Tx, Seat, time.Time) (Group, error)
	})
	if !ok {
		return nil, extshared.ErrNotPermitted
	}
	err := s.Pool.InTx(ctx, s.Access, &sql.TxOptions{ReadOnly: true}, func(tx repo.Tx) error {
		fresh, e := s.load(ctx, tx, a.TokenID, a.proof, s.now(), false)
		if e != nil {
			return e
		}
		if fresh.AgentID != a.AgentID || fresh.AuthGeneration != a.AuthGeneration || fresh.Generation != a.Generation {
			return extshared.ErrUnauthenticated
		}
		g, e := reader.ReadGroup(ctx, tx, fresh, fresh.checkedAt)
		if e != nil {
			return e
		}
		now, e := s.current(fresh, g)
		if e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id,source_name,created_at,octet_length(body) FROM np_extseat_inbox WHERE seat_id=$1 AND ag_code=$2 AND ag_generation=$3 AND expires_at>$4 AND state IN ('queued','leased') AND ($5::timestamptz IS NULL OR created_at>=$5) AND source_agent_id=$6 AND (NOT $7::boolean OR state='queued' OR lease_until<=$4) ORDER BY created_at,id`, fresh.AgentID, string(g.Code), g.Generation, now, since, g.Target.ID, eligible)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v InboxSummary
			if e = rows.Scan(&v.DeliveryID, &v.From, &v.CreatedAt, &v.BodyBytes); e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Service) Check(ctx context.Context, a Seat) ([]InboxSummary, error) {
	return s.readPending(ctx, a, nil, false)
}

// WaitReply ignores old inbox rows and other group members. Polls never lock or
// write. Only a ready candidate enters the existing short claim transaction.
func (s *Service) WaitReply(ctx context.Context, a Seat, since time.Time, max time.Duration) ([]Delivery, error) {
	if max < 0 || max > extshared.SendReplyMax {
		return nil, extshared.ErrNotPermitted
	}
	timer := time.NewTimer(max)
	defer timer.Stop()
	for {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		ready, e := s.readPending(ctx, a, &since, true)
		if e != nil {
			return nil, e
		}
		if len(ready) > 0 {
			ds, e := s.claimSince(ctx, a, &since)
			if e != nil || len(ds) > 0 {
				return ds, e
			}
		}
		if max == 0 {
			return []Delivery{}, nil
		}
		tick := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			tick.Stop()
			return nil, ctx.Err()
		case <-timer.C:
			tick.Stop()
			return []Delivery{}, nil
		case <-tick.C:
		}
	}
}

// Clear has a request-level confirmation gate plus a service-level confirmation
// gate, settles only this seat's arrived direction and never renews the AG.
func (s *Service) Clear(ctx context.Context, a Seat, confirm bool) ([]string, error) {
	if !confirm {
		return nil, extshared.ErrNotPermitted
	}
	out := []string{}
	e := s.tx(ctx, func(tx repo.Tx) error {
		fresh, g, e := s.authTx(ctx, tx, a, s.now())
		if e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `UPDATE np_extseat_inbox SET state='received',received_at=$4,lease_until=NULL WHERE seat_id=$1 AND ag_code=$2 AND ag_generation=$3 AND state IN ('queued','leased') RETURNING id`, fresh.AgentID, string(g.Code), g.Generation, fresh.checkedAt)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, e
}
