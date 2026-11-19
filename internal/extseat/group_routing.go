package extseat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

func routed(ctx context.Context) bool { return extshared.RoutingFrom(ctx).Mode != "" }
func (s *Service) routedIdentity(ctx context.Context, tx repo.Tx, a Seat) (Seat, error) {
	fresh, err := s.load(ctx, tx, a.TokenID, a.proof, s.now(), false)
	if err == nil && (fresh.AgentID != a.AgentID || fresh.AuthGeneration != a.AuthGeneration) {
		err = extshared.ErrUnauthenticated
	}
	return fresh, err
}
func targetID(ctx context.Context, tx repo.Tx, name string) (string, error) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return "", extshared.ErrNotPermitted
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT a.id FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.name_lc=lower($1) AND u.short_name_lc=lower($2) AND a.state='active' AND u.state='active'`, name[:i], name[i+1:]).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = extshared.ErrNotPermitted
	}
	return id, err
}
func peerIDs(ctx context.Context, tx repo.Tx, id string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT t.agent_id FROM np_group_member s JOIN np_group_member t ON t.code=s.code WHERE s.agent_id=$1 AND t.agent_id<>$1 ORDER BY t.agent_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var peer string
		if err = rows.Scan(&peer); err != nil {
			return nil, err
		}
		ids = append(ids, peer)
	}
	return ids, rows.Err()
}
func (s *Service) grants(ctx context.Context, tx repo.Tx, a Seat, readOnly ...bool) ([]identity.CommunicationGrant, error) {
	ids, err := peerIDs(ctx, tx, a.AgentID)
	if err != nil {
		return nil, err
	}
	grants := []identity.CommunicationGrant{}
	for _, id := range ids {
		grant, e := identity.AuthorizeCommunicationTx(ctx, tx, a.AgentID, id, extshared.RoutingFrom(ctx), s.TenantKey, s.now(), readOnly...)
		if errors.Is(e, extshared.ErrNotPermitted) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if grant.Kind == "temporary" && !s.now().Before(grant.ExpiresAt) {
			continue
		}
		grants = append(grants, grant)
	}
	if len(grants) == 0 {
		return nil, extshared.ErrNotPermitted
	}
	return grants, nil
}
func (s *Service) touchGrant(ctx context.Context, tx repo.Tx, g identity.CommunicationGrant, event string, kind extshared.GroupUseKind) error {
	if g.Kind != "temporary" {
		return nil
	}
	target := g.Target
	if target.Origin == identity.OriginExternal {
		target = g.Source
	}
	return s.TouchGroup(ctx, tx, Group{Code: extshared.AGCode(g.Ref), Generation: g.Generation, Target: target, ExpiresAt: g.ExpiresAt}, event, kind, s.now())
}
func grantAG(g identity.CommunicationGrant) (any, any) {
	if g.Kind == "temporary" {
		return g.Ref, g.Generation
	}
	return nil, nil
}
func grantBinding(g identity.CommunicationGrant) extshared.Routing {
	return extshared.Routing{Mode: "group", Kind: g.Kind, Ref: g.Ref}
}
func (s *Service) routedSend(ctx context.Context, a Seat, to, body, key string, expect bool) (SendReceipt, error) {
	var result SendReceipt
	if key == "" || len(key) > 256 {
		return result, extshared.ErrNotPermitted
	}
	if err := extshared.CheckMessageBody(body); err != nil {
		return result, err
	}
	choice := extshared.RoutingFrom(ctx)
	if !choice.Valid() {
		return result, extshared.ErrNotPermitted
	}
	hash := digest([]any{to, body, expect, choice})
	token, err := s.random(16)
	if err != nil {
		return result, err
	}
	id := "dw-ext_" + token
	err = s.tx(ctx, func(tx repo.Tx) error {
		fresh, e := s.routedIdentity(ctx, tx, a)
		if e != nil {
			return e
		}
		// 幂等行先读原绑定，重放不执行 direct 重新选择。
		var previous, target, kind, ref string
		var generation int64
		e = tx.QueryRow(ctx, `SELECT request_hash,delivery_id,created_at,expires_at,COALESCE(target_agent_id,''),route_kind,COALESCE(route_ref,ag_code),route_generation FROM np_extseat_send_key WHERE seat_id=$1 AND client_message_id=$2`, fresh.AgentID, key).Scan(&previous, &result.DeliveryID, &result.CreatedAt, &result.ExpiresAt, &target, &kind, &ref, &generation)
		if e == nil {
			if previous != hash {
				return extshared.ErrIdempotencyKey
			}
			old := identity.CommunicationGrant{Kind: kind, Ref: ref, Generation: generation, Source: identity.Agent{ID: fresh.AgentID}, Target: identity.Agent{ID: target}}
			if _, e = identity.RecheckCommunicationTx(ctx, tx, old, s.TenantKey, s.now()); e != nil {
				return e
			}
			result.Generation = 1
			result.Stage = extshared.StageAccepted
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		target, e = targetID(ctx, tx, to)
		if e != nil {
			return e
		}
		grant, e := identity.AuthorizeCommunicationTx(ctx, tx, fresh.AgentID, target, choice, s.TenantKey, s.now())
		if e != nil {
			return e
		}
		// 权威身份锁取得后再核令牌，避免锁等待期间令牌过期/撤销。
		if e = s.currentGrant(grant); e != nil {
			return e
		}
		fresh, e = s.routedIdentity(ctx, tx, a)
		if e != nil {
			return e
		}
		// 共享身份锁等待后重读幂等键，覆盖两个首次同 ID 请求的竞态。
		e = tx.QueryRow(ctx, `SELECT request_hash,delivery_id,created_at,expires_at,COALESCE(target_agent_id,''),route_kind,COALESCE(route_ref,ag_code),route_generation FROM np_extseat_send_key WHERE seat_id=$1 AND client_message_id=$2`, fresh.AgentID, key).Scan(&previous, &result.DeliveryID, &result.CreatedAt, &result.ExpiresAt, &target, &kind, &ref, &generation)
		if e == nil {
			if previous != hash {
				return extshared.ErrIdempotencyKey
			}
			old := identity.CommunicationGrant{Kind: kind, Ref: ref, Generation: generation, Source: identity.Agent{ID: fresh.AgentID}, Target: identity.Agent{ID: target}}
			if _, e = identity.RecheckCommunicationTx(ctx, tx, old, s.TenantKey, s.now()); e != nil {
				return e
			}
			result.Generation = 1
			result.Stage = extshared.StageAccepted
			return nil
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
		now := s.now()
		expiry := now.Add(extshared.MessageTTL)
		ag, gen := grantAG(grant)
		if grant.Target.Origin != identity.OriginExternal {
			var display, short string
			if e = tx.QueryRow(ctx, `SELECT display_name,short_name FROM np_user WHERE user_id_lc=$1`, fresh.OwnerLC).Scan(&display, &short); e != nil {
				return e
			}
			if e = extshared.CheckMessageBody(extshared.ExternalMessagePrefix(fresh.FullName, display, short, grant.TargetName, expect) + body); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, `INSERT INTO np_extseat_send_key(seat_id,client_message_id,request_hash,delivery_id,ag_code,ag_generation,created_at,expires_at,route_kind,route_ref,route_generation,target_agent_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, fresh.AgentID, key, hash, id, ag, gen, now, expiry, grant.Kind, grant.Ref, grant.Generation, target)
		if e != nil {
			return e
		}
		state := "queued"
		if grant.Target.Origin == identity.OriginExternal {
			state = "projected"
		}
		_, e = tx.Exec(ctx, `INSERT INTO np_extseat_outbox(delivery_id,seat_id,target_agent_id,source_name,target_name,body,ag_code,ag_generation,created_at,expires_at,expect_reply,route_kind,route_ref,route_generation,state,projected_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,CASE WHEN $15::text='projected' THEN $9::timestamptz ELSE NULL END)`, id, fresh.AgentID, target, fresh.FullName, grant.TargetName, body, ag, gen, now, expiry, expect, grant.Kind, grant.Ref, grant.Generation, state)
		if e != nil {
			return e
		}
		if state == "projected" {
			if e = s.insertRoutedInbox(ctx, tx, grant, id, key, body, now, expiry); e != nil {
				return e
			}
		}
		if e = s.touchGrant(ctx, tx, grant, id, extshared.UseAcceptedSend); e != nil {
			return e
		}
		// 不引入切片四偏好变化；现有外发抄送仍走原 sidecar。
		s.enqueueFeishuCopy(ctx, tx, FeishuCopyEvent{ID: id, SeatID: fresh.AgentID, Issuer: fresh.Principal.Issuer, Subject: fresh.Principal.Subject, ExternalName: fresh.FullName, TargetName: grant.TargetName, OwnerDisplayName: fresh.Principal.DisplayName, OwnerShortName: fresh.ownerShort, Direction: FeishuCopyExternalToInternal, Body: body, CreatedAt: now, ExpiresAt: expiry})
		result = SendReceipt{DeliveryID: id, Generation: 1, Stage: extshared.StageAccepted, CreatedAt: now, ExpiresAt: expiry}
		return s.seen(ctx, tx, fresh, now)
	})
	return result, err
}
func (s *Service) insertRoutedInbox(ctx context.Context, tx repo.Tx, g identity.CommunicationGrant, id, key, body string, now, expiry time.Time) error {
	// 稳定目标席行的锁由共享授权持有；容量核与入队串行，防并发超界。
	var seat string
	if e := tx.QueryRow(ctx, `SELECT agent_id FROM np_extseat WHERE agent_id=$1 FOR UPDATE`, g.Target.ID).Scan(&seat); e != nil {
		return e
	}
	var n int
	if e := tx.QueryRow(ctx, `SELECT count(*) FROM np_extseat_inbox WHERE seat_id=$1 AND state IN ('queued','leased') AND expires_at>$2`, seat, now).Scan(&n); e != nil {
		return e
	}
	if n >= s.limit() {
		return extshared.ErrQueueFull
	}
	ag, gen := grantAG(g)
	_, e := tx.Exec(ctx, `INSERT INTO np_extseat_inbox(id,seat_id,ag_code,ag_generation,source_agent_id,source_message_id,source_name,target_name,body,created_at,expires_at,route_kind,route_ref,route_generation) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, id, seat, ag, gen, g.Source.ID, key, g.SourceName, g.TargetName, body, now, expiry, g.Kind, g.Ref, g.Generation)
	return e
}

// routedEnqueue 从已认证 helper 的稳定身份进入，与 MCP 共用同一权威路由。
func (s *Service) routedEnqueue(ctx context.Context, source, target, key, body string) (SendReceipt, error) {
	var result SendReceipt
	if key == "" || len(key) > 256 {
		return result, extshared.ErrNotPermitted
	}
	if e := extshared.CheckMessageBody(body); e != nil {
		return result, e
	}
	id := "dw-ext_" + digest([]string{source, target, key})
	err := s.tx(ctx, func(tx repo.Tx) error {
		choice := extshared.RoutingFrom(ctx)
		if !choice.Valid() {
			return extshared.ErrNotPermitted
		}
		if choice.Mode == "" {
			choice = extshared.Routing{Mode: "direct"}
		}
		var oldBody, kind, ref string
		var gen int64
		e := tx.QueryRow(ctx, `SELECT body,route_kind,COALESCE(route_ref,ag_code),route_generation,created_at,expires_at FROM np_extseat_inbox WHERE id=$1 AND seat_id=$2`, id, target).Scan(&oldBody, &kind, &ref, &gen, &result.CreatedAt, &result.ExpiresAt)
		var g identity.CommunicationGrant
		if e == nil {
			if oldBody != body {
				return extshared.ErrIdempotencyKey
			}
			if choice.Mode == "group" && (choice.Kind != kind || choice.Ref != ref) {
				return extshared.ErrIdempotencyKey
			}
			g, e = identity.RecheckCommunicationTx(ctx, tx, identity.CommunicationGrant{Kind: kind, Ref: ref, Generation: gen, Source: identity.Agent{ID: source}, Target: identity.Agent{ID: target}}, s.TenantKey, s.now())
			if e != nil {
				return e
			}
			result.DeliveryID = id
			result.Generation = 1
			result.Stage = extshared.StageAccepted
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		g, e = identity.AuthorizeCommunicationTx(ctx, tx, source, target, choice, s.TenantKey, s.now())
		if e != nil {
			return e
		}
		if e = s.currentGrant(g); e != nil {
			return e
		}
		if g.Source.Origin == identity.OriginExternal || g.Target.Origin != identity.OriginExternal {
			return extshared.ErrNotPermitted
		}
		e = tx.QueryRow(ctx, `SELECT body,route_kind,COALESCE(route_ref,ag_code),route_generation,created_at,expires_at FROM np_extseat_inbox WHERE id=$1 AND seat_id=$2`, id, target).Scan(&oldBody, &kind, &ref, &gen, &result.CreatedAt, &result.ExpiresAt)
		if e == nil {
			if oldBody != body || (choice.Mode == "group" && (choice.Kind != kind || choice.Ref != ref)) {
				return extshared.ErrIdempotencyKey
			}
			if _, e = identity.RecheckCommunicationTx(ctx, tx, identity.CommunicationGrant{Kind: kind, Ref: ref, Generation: gen, Source: identity.Agent{ID: source}, Target: identity.Agent{ID: target}}, s.TenantKey, s.now()); e != nil {
				return e
			}
			result.DeliveryID = id
			result.Generation = 1
			result.Stage = extshared.StageAccepted
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		now := s.now()
		expiry := now.Add(extshared.MessageTTL)
		if e = s.insertRoutedInbox(ctx, tx, g, id, key, body, now, expiry); e != nil {
			return e
		}
		var issuer, subject, display, short string
		if e = tx.QueryRow(ctx, `SELECT p.issuer,p.subject,p.display_name,u.short_name FROM np_extseat s JOIN np_external_principal p ON p.id=s.principal_id JOIN np_user u ON u.user_id_lc=p.owner_lc WHERE s.agent_id=$1`, target).Scan(&issuer, &subject, &display, &short); e != nil {
			return e
		}
		s.enqueueFeishuCopy(ctx, tx, FeishuCopyEvent{ID: id, SeatID: target, Issuer: issuer, Subject: subject, ExternalName: g.TargetName, TargetName: g.SourceName, OwnerDisplayName: display, OwnerShortName: short, Direction: FeishuCopyInternalToExternal, Body: body, CreatedAt: now, ExpiresAt: expiry})
		if e = s.touchGrant(ctx, tx, g, id, extshared.UseAcceptedSend); e != nil {
			return e
		}
		result = SendReceipt{DeliveryID: id, Generation: 1, Stage: extshared.StageAccepted, CreatedAt: now, ExpiresAt: expiry}
		return nil
	})
	return result, err
}

// 升级后的身份状态由持久成员/无配对席决定，关闭新入组开关不撤销它。
func (s *Service) hasOrdinaryTx(ctx context.Context, tx repo.Tx, id string) (bool, error) {
	var present bool
	if e := tx.QueryRow(ctx, `SELECT to_regclass('np_temp_group') IS NOT NULL`).Scan(&present); e != nil {
		return false, e
	}
	if !present {
		return false, nil
	}
	var found bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM np_group_member m JOIN np_group g ON g.code=m.code WHERE m.agent_id=$1 AND NOT EXISTS(SELECT 1 FROM np_temp_group ag WHERE ag.code=g.code))`, id).Scan(&found)
	return found, e
}

func (s *Service) currentGrant(g identity.CommunicationGrant) error {
	if g.Kind == "temporary" && !s.now().Before(g.ExpiresAt) {
		return extshared.ErrNotPermitted
	}
	return nil
}

// 带普通组能力的身份，即使使用旧 AG-only 协议，也不能绕过共同权限前置检查。
func (s *Service) checkLegacyGrant(ctx context.Context, tx repo.Tx, a Seat, g Group, readOnly bool) error {
	upgraded, err := s.hasOrdinaryTx(ctx, tx, a.AgentID)
	if err != nil {
		return err
	}
	if !upgraded && !a.routingIdentity && !s.ExternalGroups {
		return nil
	}
	grant, err := identity.AuthorizeCommunicationTx(ctx, tx, a.AgentID, g.Target.ID, extshared.Routing{Mode: "group", Kind: "temporary", Ref: string(g.Code)}, s.TenantKey, s.now(), readOnly)
	if err == nil && grant.Generation != g.Generation {
		return extshared.ErrNotPermitted
	}
	if err != nil {
		return err
	}
	return s.currentGrant(grant)
}
