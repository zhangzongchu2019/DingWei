package extseat

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// Send 原子受理＋续组＋outbox；SQLite结果不参与accepted的定义。
func (s *Service) Send(ctx context.Context, a Seat, to, body, clientID string) (SendReceipt, error) {
	var result SendReceipt
	if clientID == "" || len(clientID) > 256 || len(body) > 1024*1024 {
		return result, extshared.ErrNotPermitted
	}
	now := s.now()
	requestHash := digest([]string{to, body})
	id, e := s.random(16)
	if e != nil {
		return result, e
	}
	id = "dw-ext_" + id
	e = s.tx(ctx, func(tx repo.Tx) error {
		fresh, g, e := s.authTx(ctx, tx, a, now)
		if e != nil {
			return e
		}
		now = fresh.checkedAt
		var targetName string
		if e = tx.QueryRow(ctx, `SELECT a.name||'.'||u.short_name FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.id=$1 AND a.state='active' AND u.state='active' AND a.origin<>'external' FOR UPDATE OF a,u`, g.Target.ID).Scan(&targetName); e != nil {
			return extshared.ErrTargetNotInAG
		}
		now, e = s.current(fresh, g)
		if e != nil {
			return e
		}
		if to != targetName {
			return extshared.ErrTargetNotInAG
		}
		var previous string
		var receipt SendReceipt
		e = tx.QueryRow(ctx, `SELECT request_hash,delivery_id,created_at,expires_at FROM np_extseat_send_key WHERE seat_id=$1 AND client_message_id=$2`, fresh.AgentID, clientID).Scan(&previous, &receipt.DeliveryID, &receipt.CreatedAt, &receipt.ExpiresAt)
		if e == nil {
			if previous != requestHash {
				return extshared.ErrIdempotencyKey
			}
			receipt.Stage = extshared.StageAccepted
			receipt.Generation = 1
			result = receipt
			return s.seen(ctx, tx, fresh, now)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var n int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM np_extseat_outbox WHERE seat_id=$1 AND state='queued'`, fresh.AgentID).Scan(&n); e != nil {
			return e
		}
		if n >= s.limit() {
			return extshared.ErrQueueFull
		}
		expiry := now.Add(extshared.MessageTTL)
		if _, e = tx.Exec(ctx, `INSERT INTO np_extseat_send_key(seat_id,client_message_id,request_hash,delivery_id,ag_code,ag_generation,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, fresh.AgentID, clientID, requestHash, id, string(g.Code), g.Generation, now, expiry); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO np_extseat_outbox(delivery_id,seat_id,target_agent_id,source_name,target_name,body,ag_code,ag_generation,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, fresh.AgentID, g.Target.ID, fresh.FullName, to, body, string(g.Code), g.Generation, now, expiry); e != nil {
			return e
		}
		if e = s.TouchGroup(ctx, tx, g, id, extshared.UseAcceptedSend, now); e != nil {
			return e
		}
		result = SendReceipt{DeliveryID: id, Generation: 1, Stage: extshared.StageAccepted, CreatedAt: now, ExpiresAt: expiry}
		return s.seen(ctx, tx, fresh, now)
	})
	if e != nil {
		return SendReceipt{}, e
	}
	return result, nil
}

// EnqueueFromHelper 由Hub认证后的helper身份调用；不从消息正文接受sender/AG。
func (s *Service) EnqueueFromHelper(ctx context.Context, sourceAgentID, targetAgentID, messageID, body string) (SendReceipt, error) {
	var result SendReceipt
	if sourceAgentID == "" || messageID == "" || len(messageID) > 256 || len(body) > 1024*1024 {
		return result, extshared.ErrNotPermitted
	}
	now := s.now()
	id := "dw-ext_" + digest([]string{sourceAgentID, targetAgentID, messageID})
	e := s.tx(ctx, func(tx repo.Tx) error {
		var a Seat
		var revoked sql.NullTime
		e := tx.QueryRow(ctx, `SELECT s.agent_id,s.ag_code,s.ag_generation,s.revoked_at,a.owner_lc,a.name,u.short_name FROM np_extseat s JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE s.agent_id=$1 AND a.state='active' AND u.state='active' FOR UPDATE OF s,a,u`, targetAgentID).Scan(&a.AgentID, &a.Group, &a.Generation, &revoked, &a.OwnerLC, &a.Name, &a.ownerShort)
		if errors.Is(e, sql.ErrNoRows) || revoked.Valid {
			return extshared.ErrTargetNotInAG
		}
		if e != nil {
			return e
		}
		a.ID = a.AgentID
		a.FullName = a.Name + "." + a.ownerShort
		g, e := s.Groups.RecheckGroup(ctx, tx, a, now)
		if e != nil {
			return e
		}
		if g.Target.ID != sourceAgentID {
			return extshared.ErrTargetNotInAG
		}
		var from string
		if e = tx.QueryRow(ctx, `SELECT a.name||'.'||u.short_name FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.id=$1 AND a.origin<>'external' AND a.state='active' AND u.state='active' FOR UPDATE OF a,u`, sourceAgentID).Scan(&from); e != nil {
			return extshared.ErrTargetNotInAG
		}
		now = s.now()
		if !now.Before(g.ExpiresAt) {
			return extshared.ErrGroupDissolved
		}
		var previous string
		e = tx.QueryRow(ctx, `SELECT body,created_at,expires_at FROM np_extseat_inbox WHERE id=$1`, id).Scan(&previous, &result.CreatedAt, &result.ExpiresAt)
		if e == nil {
			if previous != body {
				return extshared.ErrIdempotencyKey
			}
			result.DeliveryID = id
			result.Generation = 1
			result.Stage = extshared.StageAccepted
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var n int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM np_extseat_inbox WHERE seat_id=$1 AND state IN ('queued','leased') AND expires_at>$2`, a.AgentID, now).Scan(&n); e != nil {
			return e
		}
		if n >= s.limit() {
			return extshared.ErrQueueFull
		}
		expiry := now.Add(extshared.MessageTTL)
		if _, e = tx.Exec(ctx, `INSERT INTO np_extseat_inbox(id,seat_id,ag_code,ag_generation,source_agent_id,source_message_id,source_name,target_name,body,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, a.AgentID, string(g.Code), g.Generation, sourceAgentID, messageID, from, a.FullName, body, now, expiry); e != nil {
			return e
		}
		if e = s.TouchGroup(ctx, tx, g, id, extshared.UseAcceptedSend, now); e != nil {
			return e
		}
		result = SendReceipt{DeliveryID: id, Generation: 1, Stage: extshared.StageAccepted, CreatedAt: now, ExpiresAt: expiry}
		return nil
	})
	if e != nil {
		return SendReceipt{}, e
	}
	return result, nil
}

func (s *Service) claim(ctx context.Context, a Seat) ([]Delivery, error) {
	var result []Delivery
	now := s.now()
	e := s.tx(ctx, func(tx repo.Tx) error {
		fresh, g, e := s.authTx(ctx, tx, a, now)
		if e != nil {
			return e
		}
		now = fresh.checkedAt
		if _, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='dead',dead_reason='message_expired',lease_until=NULL WHERE seat_id=$1 AND state IN ('queued','leased') AND expires_at<=$2`, fresh.AgentID, now); e != nil {
			return e
		}
		var d Delivery
		e = tx.QueryRow(ctx, `SELECT id,generation,source_name,target_name,body,created_at,expires_at FROM np_extseat_inbox WHERE seat_id=$1 AND ag_code=$2 AND ag_generation=$3 AND expires_at>$4 AND (state='queued' OR (state='leased' AND lease_until<=$4)) ORDER BY created_at,id LIMIT 1 FOR UPDATE`, fresh.AgentID, string(g.Code), g.Generation, now).Scan(&d.ID, &d.Generation, &d.From, &d.To, &d.Body, &d.CreatedAt, &d.ExpiresAt)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		now, e = s.current(fresh, g)
		if e != nil {
			return e
		}
		if !now.Before(d.ExpiresAt) {
			_, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='dead',dead_reason='message_expired',lease_until=NULL WHERE id=$1`, d.ID)
			return e
		}
		d.Group = g.Code
		d.LeaseUntil = now.Add(5 * time.Second)
		if _, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='leased',lease_until=$2 WHERE id=$1`, d.ID, d.LeaseUntil); e != nil {
			return e
		}
		result = []Delivery{d}
		return nil
	})
	return result, e
}
func (s *Service) Wait(ctx context.Context, a Seat, max time.Duration) ([]Delivery, error) {
	if max < 0 || max > extshared.WaitMax {
		return nil, extshared.ErrNotPermitted
	}
	deadline := time.NewTimer(max)
	defer deadline.Stop()
	for {
		d, e := s.claim(ctx, a)
		if e != nil {
			return nil, e
		}
		if len(d) > 0 {
			if e = s.presence(ctx, a); e != nil {
				return nil, e
			}
			return d, nil
		}
		if max == 0 {
			if e = s.presence(ctx, a); e != nil {
				return nil, e
			}
			return []Delivery{}, nil
		}
		tick := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			tick.Stop()
			return nil, ctx.Err()
		case <-deadline.C:
			tick.Stop()
			if e = s.presence(ctx, a); e != nil {
				return nil, e
			}
			return []Delivery{}, nil
		case <-tick.C:
		}
	}
}
func (s *Service) presence(ctx context.Context, a Seat) error {
	now := s.now()
	return s.tx(ctx, func(tx repo.Tx) error {
		fresh, _, e := s.authTx(ctx, tx, a, now)
		if e != nil {
			return e
		}
		now = fresh.checkedAt
		return s.seen(ctx, tx, fresh, now)
	})
}
func (s *Service) Ack(ctx context.Context, a Seat, id string, gen int) error {
	now := s.now()
	return s.tx(ctx, func(tx repo.Tx) error {
		fresh, g, e := s.authTx(ctx, tx, a, now)
		if e != nil {
			return e
		}
		now = fresh.checkedAt
		var state string
		var expiry time.Time
		var generation int64
		e = tx.QueryRow(ctx, `SELECT state,expires_at,generation FROM np_extseat_inbox WHERE id=$1 AND seat_id=$2 AND ag_code=$3 AND ag_generation=$4 FOR UPDATE`, id, fresh.AgentID, string(g.Code), g.Generation).Scan(&state, &expiry, &generation)
		if errors.Is(e, sql.ErrNoRows) {
			return extshared.ErrNotPermitted
		}
		if e != nil {
			return e
		}
		now, e = s.current(fresh, g)
		if e != nil {
			return e
		}
		if generation != int64(gen) {
			return extshared.ErrNotPermitted
		}
		if state == "received" {
			return s.seen(ctx, tx, fresh, now)
		}
		if !now.Before(expiry) {
			if _, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='dead',dead_reason='message_expired',lease_until=NULL WHERE id=$1 AND state IN ('queued','leased')`, id); e != nil {
				return e
			}
			return &repo.CommitErr{Err: extshared.ErrNotPermitted}
		}
		if state != "leased" {
			return extshared.ErrNotPermitted
		}
		if _, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state=$2,received_at=$3,lease_until=NULL WHERE id=$1`, id, string(extshared.PhaseReceived), now); e != nil {
			return e
		}
		if e = s.TouchGroup(ctx, tx, g, id+":received", extshared.UseFirstReceivedAck, now); e != nil {
			return e
		}
		return s.seen(ctx, tx, fresh, now)
	})
}

// Expire 定时补扫，无人轮询时也收敛未确认消息；received不回退。
func (s *Service) Expire(ctx context.Context) error {
	now := s.now()
	return s.tx(ctx, func(tx repo.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='dead',dead_reason='message_expired',lease_until=NULL WHERE state IN ('queued','leased') AND expires_at<=$1`, now)
		return e
	})
}

// Receipt 只提供本席位的收件状态与可查死因，不泄露别人消息。
func (s *Service) Receipt(ctx context.Context, a Seat, id string) (state, reason string, err error) {
	err = s.tx(ctx, func(tx repo.Tx) error {
		fresh, _, e := s.authTx(ctx, tx, a, s.now())
		if e != nil {
			return e
		}
		e = tx.QueryRow(ctx, `SELECT state,dead_reason FROM np_extseat_inbox WHERE id=$1 AND seat_id=$2`, id, fresh.AgentID).Scan(&state, &reason)
		if errors.Is(e, sql.ErrNoRows) {
			return extshared.ErrNotPermitted
		}
		return e
	})
	return
}

func (s *Service) ListOnline(ctx context.Context, a Seat) ([]OnlineAgent, error) {
	var helpers []OnlineAgent
	var e error
	if s.Helpers != nil {
		helpers, e = s.Helpers.Online(ctx)
		if e != nil {
			return nil, e
		}
	}
	var result []OnlineAgent
	now := s.now()
	e = s.tx(ctx, func(tx repo.Tx) error {
		fresh, g, e := s.authTx(ctx, tx, a, now)
		if e != nil {
			return e
		}
		now = fresh.checkedAt
		for _, h := range helpers {
			if h.Agent.ID == g.Target.ID && h.Agent.State == identity.AgentActive && !h.External {
				result = append(result, h)
			}
		}
		return s.seen(ctx, tx, fresh, now)
	})
	return result, e
}

// Online 供Hub目录合并使用；外部presence不伪装成WebSocket。
func (s *Service) Online(ctx context.Context) ([]OnlineAgent, error) {
	var out []OnlineAgent
	now := s.now()
	e := s.tx(ctx, func(tx repo.Tx) error {
		rows, e := tx.Query(ctx, `SELECT s.agent_id,s.ag_code,s.ag_generation,a.owner_lc,a.name,u.short_name,s.last_seen_at FROM np_extseat s JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE s.revoked_at IS NULL AND a.state='active' AND u.state='active' AND s.last_seen_at>$1 AND EXISTS(SELECT 1 FROM np_extseat_token t WHERE t.seat_id=s.agent_id AND t.revoked_at IS NULL AND t.expires_at>$2 AND t.auth_generation=s.auth_generation) ORDER BY a.id`, now.Add(-extshared.PresenceTimeout), now)
		if e != nil {
			return e
		}
		type candidate struct {
			a    Seat
			last time.Time
		}
		var items []candidate
		for rows.Next() {
			var c candidate
			if e = rows.Scan(&c.a.AgentID, &c.a.Group, &c.a.Generation, &c.a.OwnerLC, &c.a.Name, &c.a.ownerShort, &c.last); e != nil {
				rows.Close()
				return e
			}
			items = append(items, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, c := range items {
			if _, e = s.Groups.RecheckGroup(ctx, tx, c.a, now); e != nil {
				if errors.Is(e, extshared.ErrGroupDissolved) || errors.Is(e, extshared.ErrTargetNotInAG) {
					continue
				}
				return e
			}
			out = append(out, OnlineAgent{Agent: identity.Agent{ID: c.a.AgentID, OwnerLC: c.a.OwnerLC, Name: c.a.Name, Origin: identity.OriginExternal, State: identity.AgentActive}, FullName: c.a.Name + "." + c.a.ownerShort, LastSeenAt: c.last, External: true})
		}
		return nil
	})
	return out, e
}
