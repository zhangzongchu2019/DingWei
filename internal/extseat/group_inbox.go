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

type routedMessage struct {
	d                                Delivery
	source, kind, ref, state, reason string
	routeGeneration                  int64
	bytes                            int
}

// 先只读元数据，在原路由重核之后才查询正文，不能以别的有效组领取旧件。
func (s *Service) routedInbox(ctx context.Context, a Seat, operation, id string, generation int, since *time.Time) ([]Delivery, []InboxSummary, []string, string, string, error) {
	var deliveries []Delivery
	summaries := []InboxSummary{}
	ids := []string{}
	state, reason := "", ""
	readonly := operation == "check" || operation == "receipt"
	var opts *sql.TxOptions
	if readonly {
		opts = &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}
	}
	err := s.Pool.InTx(ctx, s.Access, opts, func(tx repo.Tx) error {
		fresh, e := s.routedIdentity(ctx, tx, a)
		if e != nil {
			return e
		}
		if _, e = s.grants(ctx, tx, fresh, readonly); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id,generation,source_agent_id,route_kind,COALESCE(route_ref,ag_code),route_generation,state,dead_reason,source_name,target_name,created_at,expires_at,octet_length(body) FROM np_extseat_inbox WHERE seat_id=$1 AND ($2='' OR id=$2) AND ($2<>'' OR (state IN ('queued','leased') AND expires_at>$3 AND ($4::timestamptz IS NULL OR created_at>=$4))) ORDER BY created_at,id`, fresh.AgentID, id, s.now(), since)
		if e != nil {
			return e
		}
		var candidates []routedMessage
		for rows.Next() {
			var item routedMessage
			e = rows.Scan(&item.d.ID, &item.d.Generation, &item.source, &item.kind, &item.ref, &item.routeGeneration, &item.state, &item.reason, &item.d.From, &item.d.To, &item.d.CreatedAt, &item.d.ExpiresAt, &item.bytes)
			if e != nil {
				break
			}
			candidates = append(candidates, item)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return e
		}
		for _, item := range candidates {
			choice := extshared.RoutingFrom(ctx)
			if choice.Mode == "group" && (choice.Kind != item.kind || choice.Ref != item.ref) {
				continue
			}
			old := identity.CommunicationGrant{Kind: item.kind, Ref: item.ref, Generation: item.routeGeneration, Source: identity.Agent{ID: item.source}, Target: identity.Agent{ID: fresh.AgentID}}
			grant, e := identity.RecheckCommunicationTx(ctx, tx, old, s.TenantKey, s.now(), readonly)
			if errors.Is(e, extshared.ErrNotPermitted) {
				continue
			}
			if e != nil {
				return e
			}
			if e = s.currentGrant(grant); e != nil {
				continue
			}
			if _, e = s.routedIdentity(ctx, tx, a); e != nil {
				return e
			}
			if operation == "check" {
				summaries = append(summaries, InboxSummary{DeliveryID: item.d.ID, From: item.d.From, CreatedAt: item.d.CreatedAt, BodyBytes: item.bytes})
				continue
			}
			if operation == "receipt" {
				state, reason = item.state, item.reason
				return nil
			}
			// 操作/确认时重新锁行，以数据库当前状态防重复 lease、终态倒退和并发 clear。
			var lease sql.NullTime
			e = tx.QueryRow(ctx, `SELECT state,generation,expires_at,lease_until FROM np_extseat_inbox WHERE id=$1 AND seat_id=$2 FOR UPDATE`, item.d.ID, fresh.AgentID).Scan(&item.state, &item.d.Generation, &item.d.ExpiresAt, &lease)
			if e != nil {
				return e
			}
			now := s.now()
			if operation == "ack" {
				if item.d.Generation != int64(generation) {
					return extshared.ErrNotPermitted
				}
				if item.state == "received" {
					return s.seen(ctx, tx, fresh, now)
				}
				if item.state != "leased" {
					return extshared.ErrNotPermitted
				}
			} else if item.state != "queued" && item.state != "leased" {
				continue
			}
			if !now.Before(item.d.ExpiresAt) {
				if operation == "ack" {
					if _, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='dead',dead_reason='message_expired',lease_until=NULL WHERE id=$1`, item.d.ID); e != nil {
						return e
					}
					return &repo.CommitErr{Err: extshared.ErrNotPermitted}
				}
				continue
			}
			if operation == "claim" {
				if item.state == "leased" && lease.Valid && now.Before(lease.Time) {
					continue
				}
				if e = tx.QueryRow(ctx, `SELECT body FROM np_extseat_inbox WHERE id=$1`, item.d.ID).Scan(&item.d.Body); e != nil {
					return e
				}
				item.d.LeaseUntil = now.Add(5 * time.Second)
				if _, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='leased',lease_until=$2 WHERE id=$1`, item.d.ID, item.d.LeaseUntil); e != nil {
					return e
				}
				if item.kind == "temporary" {
					item.d.Group = extshared.AGCode(item.ref)
				}
				deliveries = append(deliveries, item.d)
				return nil
			}
			if operation != "ack" && operation != "clear" {
				return extshared.ErrNotPermitted
			}
			if _, e = tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='received',received_at=$2,lease_until=NULL WHERE id=$1`, item.d.ID, now); e != nil {
				return e
			}
			if operation == "ack" {
				if e = s.touchGrant(ctx, tx, grant, item.d.ID+":received", extshared.UseFirstReceivedAck); e != nil {
					return e
				}
				if e = s.seen(ctx, tx, fresh, now); e != nil {
					return e
				}
			}
			ids = append(ids, item.d.ID)
		}
		if id != "" && len(ids) == 0 && state == "" {
			return extshared.ErrNotPermitted
		}
		return nil
	})
	return deliveries, summaries, ids, state, reason, err
}
func (s *Service) routedOnline(ctx context.Context, a Seat) ([]OnlineAgent, error) {
	var helpers []OnlineAgent
	var err error
	if s.Helpers != nil {
		helpers, err = s.Helpers.Online(ctx)
		if err != nil {
			return nil, err
		}
	}
	result := []OnlineAgent{}
	err = s.tx(ctx, func(tx repo.Tx) error {
		fresh, e := s.routedIdentity(ctx, tx, a)
		if e != nil {
			return e
		}
		grants, e := s.grants(ctx, tx, fresh)
		if e != nil {
			return e
		}
		for _, g := range grants {
			if g.Target.Origin != identity.OriginExternal {
				for _, h := range helpers {
					if h.Agent.ID == g.Target.ID {
						result = append(result, h)
					}
				}
				continue
			}
			var seen sql.NullTime
			var alive bool
			e = tx.QueryRow(ctx, `SELECT s.last_seen_at,EXISTS(SELECT 1 FROM np_extseat_token t WHERE t.seat_id=s.agent_id AND t.revoked_at IS NULL AND t.expires_at>$2 AND t.auth_generation=s.auth_generation) FROM np_extseat s WHERE s.agent_id=$1`, g.Target.ID, s.now()).Scan(&seen, &alive)
			if e != nil {
				return e
			}
			if alive && seen.Valid && seen.Time.After(s.now().Add(-extshared.PresenceTimeout)) {
				result = append(result, OnlineAgent{Agent: g.Target, FullName: g.TargetName, External: true, LastSeenAt: seen.Time})
			}
		}
		return s.seen(ctx, tx, fresh, s.now())
	})
	return result, err
}
