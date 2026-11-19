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
func (s *Service) Send(ctx context.Context, a Seat, to, body, clientID string, expectReplyArg ...bool) (SendReceipt, error) {
	if routed(ctx) {
		return s.routedSend(ctx, a, to, body, clientID, len(expectReplyArg) > 0 && expectReplyArg[0])
	}
	var result SendReceipt
	if err := extshared.CheckMessageBody(body); err != nil {
		return result, err
	}
	expectReply := len(expectReplyArg) > 0 && expectReplyArg[0]
	if clientID == "" || len(clientID) > 256 {
		return result, extshared.ErrNotPermitted
	}
	now := s.now()
	requestHash := digest([]any{to, body, expectReply})
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
		var ownerDisplay, ownerShort string
		if e = tx.QueryRow(ctx, `SELECT display_name,short_name FROM np_user WHERE user_id_lc=$1`, fresh.OwnerLC).Scan(&ownerDisplay, &ownerShort); e != nil {
			return e
		}
		if e = extshared.CheckMessageBody(extshared.ExternalMessagePrefix(fresh.FullName, ownerDisplay, ownerShort, targetName, expectReply) + body); e != nil {
			return e
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
		if _, e = tx.Exec(ctx, `INSERT INTO np_extseat_outbox(delivery_id,seat_id,target_agent_id,source_name,target_name,body,ag_code,ag_generation,created_at,expires_at,expect_reply) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, fresh.AgentID, g.Target.ID, fresh.FullName, to, body, string(g.Code), g.Generation, now, expiry, expectReply); e != nil {
			return e
		}
		s.enqueueFeishuCopy(ctx, tx, FeishuCopyEvent{ID: id, SeatID: fresh.AgentID, Issuer: fresh.Principal.Issuer, Subject: fresh.Principal.Subject, ExternalName: fresh.FullName, TargetName: to, OwnerDisplayName: fresh.Principal.DisplayName, OwnerShortName: fresh.ownerShort, Direction: FeishuCopyExternalToInternal, Body: body, CreatedAt: now, ExpiresAt: expiry})
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
	var useOrdinary bool
	if !s.ExternalGroups && !routed(ctx) {
		if e := s.tx(ctx, func(tx repo.Tx) error {
			var err error
			useOrdinary, err = s.hasOrdinaryTx(ctx, tx, targetAgentID)
			if err == nil && !useOrdinary {
				err = tx.QueryRow(ctx, `SELECT ag_code IS NULL FROM np_extseat WHERE agent_id=$1`, targetAgentID).Scan(&useOrdinary)
				if errors.Is(err, sql.ErrNoRows) {
					return extshared.ErrNotPermitted
				}
			}
			return err
		}); e != nil {
			return SendReceipt{}, e
		}
	}
	if s.ExternalGroups || routed(ctx) || useOrdinary {
		return s.routedEnqueue(ctx, sourceAgentID, targetAgentID, messageID, body)
	}
	var result SendReceipt
	if err := extshared.CheckMessageBody(body); err != nil {
		return result, err
	}
	if sourceAgentID == "" || messageID == "" || len(messageID) > 256 {
		return result, extshared.ErrNotPermitted
	}
	now := s.now()
	id := "dw-ext_" + digest([]string{sourceAgentID, targetAgentID, messageID})
	e := s.tx(ctx, func(tx repo.Tx) error {
		var a Seat
		var revoked sql.NullTime
		e := tx.QueryRow(ctx, `SELECT s.agent_id,s.ag_code,s.ag_generation,s.revoked_at,a.owner_lc,a.name,u.short_name,p.issuer,p.subject,p.display_name FROM np_extseat s JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc JOIN np_external_principal p ON p.id=s.principal_id WHERE s.agent_id=$1 AND p.tenant_key=$2 AND a.state='active' AND u.state='active' FOR UPDATE OF s,a,u`, targetAgentID, s.TenantKey).Scan(&a.AgentID, &a.Group, &a.Generation, &revoked, &a.OwnerLC, &a.Name, &a.ownerShort, &a.Principal.Issuer, &a.Principal.Subject, &a.Principal.DisplayName)
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
		s.enqueueFeishuCopy(ctx, tx, FeishuCopyEvent{ID: id, SeatID: a.AgentID, Issuer: a.Principal.Issuer, Subject: a.Principal.Subject, ExternalName: a.FullName, TargetName: from, OwnerDisplayName: a.Principal.DisplayName, OwnerShortName: a.ownerShort, Direction: FeishuCopyInternalToExternal, Body: body, CreatedAt: now, ExpiresAt: expiry})
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

func (s *Service) enqueueFeishuCopy(ctx context.Context, tx repo.Tx, event FeishuCopyEvent) {
	if s.FeishuCopies == nil {
		return
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT extseat_feishu_copy_enqueue`); err != nil {
		if s.OnFeishuCopyEnqueueError != nil {
			s.OnFeishuCopyEnqueueError(err)
		}
		return
	}
	err := s.FeishuCopies.Enqueue(ctx, tx, event)
	if err != nil {
		if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT extseat_feishu_copy_enqueue`); rollbackErr != nil {
			if s.OnFeishuCopyEnqueueError != nil {
				s.OnFeishuCopyEnqueueError(rollbackErr)
			}
			return
		}
		if _, releaseErr := tx.Exec(ctx, `RELEASE SAVEPOINT extseat_feishu_copy_enqueue`); releaseErr != nil {
			if s.OnFeishuCopyEnqueueError != nil {
				s.OnFeishuCopyEnqueueError(releaseErr)
			}
			return
		}
		if s.OnFeishuCopyEnqueueError != nil {
			s.OnFeishuCopyEnqueueError(err)
		}
		return
	}
	if _, err = tx.Exec(ctx, `RELEASE SAVEPOINT extseat_feishu_copy_enqueue`); err != nil && s.OnFeishuCopyEnqueueError != nil {
		s.OnFeishuCopyEnqueueError(err)
	}
}

func (s *Service) claim(ctx context.Context, a Seat) ([]Delivery, error) {
	return s.claimSince(ctx, a, nil)
}
func (s *Service) claimSince(ctx context.Context, a Seat, since *time.Time) ([]Delivery, error) {
	if routed(ctx) {
		ds, _, _, _, _, e := s.routedInbox(ctx, a, "claim", "", 0, since)
		return ds, e
	}
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
		e = tx.QueryRow(ctx, `SELECT id,generation,source_name,target_name,body,created_at,expires_at FROM np_extseat_inbox WHERE seat_id=$1 AND ag_code=$2 AND ag_generation=$3 AND expires_at>$4 AND (state='queued' OR (state='leased' AND lease_until<=$4)) AND ($5::timestamptz IS NULL OR (created_at>=$5 AND source_agent_id=$6)) ORDER BY created_at,id LIMIT 1 FOR UPDATE`, fresh.AgentID, string(g.Code), g.Generation, now, since, g.Target.ID).Scan(&d.ID, &d.Generation, &d.From, &d.To, &d.Body, &d.CreatedAt, &d.ExpiresAt)
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
	if routed(ctx) {
		return s.tx(ctx, func(tx repo.Tx) error {
			fresh, e := s.routedIdentity(ctx, tx, a)
			if e != nil {
				return e
			}
			if _, e = s.grants(ctx, tx, fresh); e != nil {
				return e
			}
			return s.seen(ctx, tx, fresh, s.now())
		})
	}
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
	if routed(ctx) {
		_, _, _, _, _, e := s.routedInbox(ctx, a, "ack", id, gen, nil)
		return e
	}
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
	if routed(ctx) {
		_, _, _, state, reason, err = s.routedInbox(ctx, a, "receipt", id, 0, nil)
		return
	}
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
	if routed(ctx) {
		return s.routedOnline(ctx, a)
	}
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
		displayColumn := `''::text`
		if extshared.ExternalDisplayNamesEnabled() {
			displayColumn = `COALESCE(a.display_name,'')`
		}
		rows, e := tx.Query(ctx, `SELECT s.agent_id,COALESCE(s.ag_code,''),COALESCE(s.ag_generation,0),a.owner_lc,a.name,u.short_name,s.last_seen_at,`+displayColumn+` FROM np_extseat s JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc JOIN np_external_principal p ON p.id=s.principal_id WHERE p.tenant_key=$3 AND s.revoked_at IS NULL AND a.state='active' AND u.state='active' AND s.last_seen_at>$1 AND EXISTS(SELECT 1 FROM np_extseat_token t WHERE t.seat_id=s.agent_id AND t.revoked_at IS NULL AND t.expires_at>$2 AND t.auth_generation=s.auth_generation) ORDER BY a.id`, now.Add(-extshared.PresenceTimeout), now, s.TenantKey)
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
			if e = rows.Scan(&c.a.AgentID, &c.a.Group, &c.a.Generation, &c.a.OwnerLC, &c.a.Name, &c.a.ownerShort, &c.last, &c.a.DisplayName); e != nil {
				rows.Close()
				return e
			}
			if normalized, err := extshared.NormalizeExternalDisplayName(c.a.DisplayName); err == nil {
				c.a.DisplayName = normalized
			} else {
				c.a.DisplayName = ""
			}
			items = append(items, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, c := range items {
			ordinary, err := s.hasOrdinaryTx(ctx, tx, c.a.AgentID)
			if err != nil {
				return err
			}
			if ordinary || c.a.Group == "" {
				if _, err = s.grants(extshared.WithRouting(ctx, extshared.Routing{Mode: "direct"}), tx, c.a); err != nil {
					if errors.Is(err, extshared.ErrNotPermitted) {
						continue
					}
					return err
				}
			} else if _, e = s.Groups.RecheckGroup(ctx, tx, c.a, now); e != nil {
				if errors.Is(e, extshared.ErrGroupDissolved) || errors.Is(e, extshared.ErrTargetNotInAG) {
					continue
				}
				return e
			}
			out = append(out, OnlineAgent{Agent: identity.Agent{ID: c.a.AgentID, OwnerLC: c.a.OwnerLC, Name: c.a.Name, DisplayName: c.a.DisplayName, Origin: identity.OriginExternal, State: identity.AgentActive}, FullName: c.a.Name + "." + c.a.ownerShort, LastSeenAt: c.last, External: true})
		}
		return nil
	})
	return out, e
}

// SettleDissolvedGroup 供 D 的提交后通知 worker 调用；只结算 A 自己的未确认行。
// 已 received 或已投影的 helper 投递不回退，D 不跨域写 A 的表。
func (s *Service) SettleDissolvedGroup(ctx context.Context, code extshared.AGCode) error {
	if _, ok := extshared.ParseAGCode(string(code)); !ok {
		return extshared.ErrNotPermitted
	}
	return s.tx(ctx, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE np_extseat_inbox SET state='dead',dead_reason='group_unavailable',lease_until=NULL WHERE ag_code=$1 AND state IN ('queued','leased')`, string(code)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE np_extseat_outbox SET state='dead',dead_reason='group_unavailable' WHERE ag_code=$1 AND state='queued'`, string(code))
		return err
	})
}
