package identity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// CommunicationGrant 是事务中按稳定双方身份核得的路由，必须随消息持久化。
// 不允许把普通组码冒充 AG；Kind/Ref/Generation 三者一起判别。
type CommunicationGrant struct {
	Kind, Ref              string
	Generation             int64
	Source, Target         Agent
	SourceName, TargetName string
	ExpiresAt              time.Time
}

// AuthorizeCommunicationTx 在调用方已有事务内完成，不另借池连接。
// tenantKey 只能来自受信配置，组成员关系不代替跨租户准入。
func AuthorizeCommunicationTx(ctx context.Context, tx repo.Tx, sourceID, targetID string, choice extshared.Routing, tenantKey string, at time.Time, readOnly ...bool) (CommunicationGrant, error) {
	agentLock, groupLock := " FOR UPDATE OF a,u", " FOR SHARE OF g,s,t"
	if len(readOnly) > 0 && readOnly[0] {
		agentLock, groupLock = "", ""
	}
	var out CommunicationGrant
	deny := extshared.ErrNotPermitted
	if sourceID == "" || targetID == "" || sourceID == targetID || !choice.Valid() || choice.Mode == "" {
		return out, deny
	}
	// 两端的锁序固定，外部互发时不因相反的发送方向反转锁序。
	rows, err := tx.Query(ctx, `SELECT a.id,a.owner_lc,a.name,a.origin,a.state,u.short_name FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.id IN ($1,$2) AND a.state='active' AND u.state='active' ORDER BY a.id`+agentLock, sourceID, targetID)
	if err != nil {
		return out, err
	}
	count := 0
	for rows.Next() {
		var a Agent
		var short string
		if err = rows.Scan(&a.ID, &a.OwnerLC, &a.Name, &a.Origin, &a.State, &short); err != nil {
			break
		}
		if a.ID == sourceID {
			out.Source, out.SourceName = a, a.Name+"."+short
		} else {
			out.Target, out.TargetName = a, a.Name+"."+short
		}
		count++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	if count != 2 {
		return out, deny
	}
	// 外部席的权威主体验证，不根据地址、AG 镜像或客户端标签推断。
	for _, a := range []Agent{out.Source, out.Target} {
		if a.Origin != OriginExternal {
			continue
		}
		if tenantKey == "" {
			return out, deny
		}
		var allowed bool
		seatLock := " FOR SHARE OF s,p"
		if len(readOnly) > 0 && readOnly[0] {
			seatLock = ""
		}
		err = tx.QueryRow(ctx, `SELECT p.tenant_key=$2 AND s.revoked_at IS NULL FROM np_extseat s JOIN np_external_principal p ON p.id=s.principal_id WHERE s.agent_id=$1`+seatLock, a.ID, tenantKey).Scan(&allowed)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !allowed) {
			return out, deny
		}
		if err != nil {
			return out, err
		}
	}
	{
		rows, err = tx.Query(ctx, `SELECT g.code,g.leader_only,g.leader_agent_id FROM np_group g JOIN np_group_member s ON s.code=g.code AND s.agent_id=$1 JOIN np_group_member t ON t.code=g.code AND t.agent_id=$2 WHERE NOT EXISTS(SELECT 1 FROM np_temp_group ag WHERE ag.code=g.code) ORDER BY g.code`+groupLock, sourceID, targetID)
		if err != nil {
			return out, err
		}
		selected := ""
		blocked := false
		for rows.Next() {
			var ref, leader string
			var leaderOnly bool
			if err = rows.Scan(&ref, &leaderOnly, &leader); err != nil {
				break
			}
			if leaderOnly && sourceID != leader && targetID != leader {
				blocked = true
			}
			if selected == "" && (choice.Mode == "direct" || choice.Ref == ref) {
				selected = ref
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return out, err
		}
		if blocked {
			return out, deny
		}
		if selected != "" && (choice.Mode == "direct" || choice.Kind == "ordinary") {
			out.Kind, out.Ref, out.Generation = "ordinary", selected, 1
			return out, nil
		}
		if choice.Mode == "group" && choice.Kind == "ordinary" {
			return out, deny
		}
	}
	// 真实双成员配对才可授权；过期的镜像 np_group 永远不是替代授权。
	err = tx.QueryRow(ctx, `SELECT g.code,g.generation,g.expires_at FROM np_temp_group g JOIN np_temp_group_member s ON s.code=g.code AND s.agent_id=$1 JOIN np_temp_group_member t ON t.code=g.code AND t.agent_id=$2 WHERE g.dissolved_at IS NULL AND g.expires_at>$3 AND ((g.external_agent_id=$1 AND g.target_agent_id=$2) OR (g.external_agent_id=$2 AND g.target_agent_id=$1)) AND ($4='' OR g.code=$4) AND s.role=CASE WHEN g.external_agent_id=$1 THEN 'external' ELSE 'internal' END AND t.role=CASE WHEN g.external_agent_id=$2 THEN 'external' ELSE 'internal' END AND EXISTS(SELECT 1 FROM np_extseat pair WHERE pair.agent_id=g.external_agent_id AND pair.ag_code=g.code AND pair.ag_generation=g.generation AND pair.revoked_at IS NULL) AND (SELECT count(*) FROM np_temp_group_member m WHERE m.code=g.code)=2 ORDER BY g.code LIMIT 1`+groupLock, sourceID, targetID, at, choice.Ref).Scan(&out.Ref, &out.Generation, &out.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, deny
	}
	if err != nil {
		return out, err
	}
	out.Kind = "temporary"
	return out, nil
}

// RecheckCommunicationTx 不进行 direct 路由重新选择，丢组即拒绝。
func RecheckCommunicationTx(ctx context.Context, tx repo.Tx, previous CommunicationGrant, tenantKey string, at time.Time, readOnly ...bool) (CommunicationGrant, error) {
	grant, err := AuthorizeCommunicationTx(ctx, tx, previous.Source.ID, previous.Target.ID, extshared.Routing{Mode: "group", Kind: previous.Kind, Ref: previous.Ref}, tenantKey, at, readOnly...)
	if err == nil && grant.Generation != previous.Generation {
		err = extshared.ErrNotPermitted
	}
	return grant, err
}

func (s *PGStore) ExternalAgentsCanCommunicate(ctx context.Context, source, target, tenant string, at time.Time) (bool, error) {
	allowed := false
	err := s.inTx(ctx, func(tx repo.Tx) error {
		_, err := AuthorizeCommunicationTx(ctx, tx, source, target, extshared.Routing{Mode: "direct"}, tenant, at)
		if errors.Is(err, extshared.ErrNotPermitted) {
			return nil
		}
		allowed = err == nil
		return err
	})
	return allowed, err
}

func (s *PGStore) JoinExternalGroup(ctx context.Context, code, agentID, actor, tenant string, at time.Time) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		g, err := lockGroup(ctx, tx, code, true)
		if err != nil {
			return err
		}
		if g.AdditionsClosed {
			return ErrGroupClosed
		}
		var mirror bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM np_temp_group WHERE code=$1)`, code).Scan(&mirror); err != nil {
			return err
		}
		if mirror {
			return ErrGroupAgentUnavailable
		}
		var allowed bool
		err = tx.QueryRow(ctx, `SELECT (a.owner_lc=$2 OR actor.type='platform_admin') AND s.revoked_at IS NULL AND p.tenant_key=$3 FROM np_agent a JOIN np_extseat s ON s.agent_id=a.id JOIN np_external_principal p ON p.id=s.principal_id JOIN np_user u ON u.user_id_lc=a.owner_lc JOIN np_user actor ON actor.user_id_lc=$2 WHERE a.id=$1 AND a.origin='external' AND a.state='active' AND u.state='active' AND actor.state='active' FOR SHARE OF a,s,p,u,actor`, agentID, actor, tenant).Scan(&allowed)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (!allowed || tenant == "")) {
			return ErrGroupAgentUnavailable
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE np_extseat SET routing_enabled=true WHERE agent_id=$1`, agentID); err != nil {
			return err
		}
		res, err := tx.Exec(ctx, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, code, agentID, at)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err == nil && n == 0 {
			return ErrGroupMemberExists
		}
		return err
	})
}

func (s *PGStore) ExternalGroupCandidates(ctx context.Context, actor, tenant string) ([]Agent, error) {
	var out []Agent
	err := s.inTx(ctx, func(tx repo.Tx) error {
		rows, e := tx.Query(ctx, `SELECT a.id,a.owner_lc,a.name,a.origin,a.state,a.created_at FROM np_agent a JOIN np_extseat s ON s.agent_id=a.id JOIN np_external_principal p ON p.id=s.principal_id JOIN np_user u ON u.user_id_lc=a.owner_lc JOIN np_user actor ON actor.user_id_lc=$1 WHERE a.origin='external' AND a.state='active' AND u.state='active' AND actor.state='active' AND (a.owner_lc=$1 OR actor.type='platform_admin') AND s.revoked_at IS NULL AND p.tenant_key=$2 ORDER BY a.id`, actor, tenant)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var a Agent
			if e = rows.Scan(&a.ID, &a.OwnerLC, &a.Name, &a.Origin, &a.State, &a.CreatedAt); e != nil {
				return e
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (s *PGStore) CreateExternalOwnedGroup(ctx context.Context, g Group, tenant string) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		var valid bool
		e := tx.QueryRow(ctx, `SELECT p.tenant_key=$3 AND s.revoked_at IS NULL FROM np_agent a JOIN np_extseat s ON s.agent_id=a.id JOIN np_external_principal p ON p.id=s.principal_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.id=$1 AND a.owner_lc=$2 AND a.origin='external' AND a.state='active' AND u.state='active' FOR SHARE OF a,s,p,u`, g.LeaderAgentID, g.CreatorLC, tenant).Scan(&valid)
		if errors.Is(e, sql.ErrNoRows) || (e == nil && (!valid || tenant == "")) {
			return ErrGroupAgentUnavailable
		}
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO np_group (`+groupCols+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, g.Code, g.ShortName, g.CreatorLC, g.LeaderAgentID, g.AdditionsClosed, g.CreatedAt, g.UpdatedAt, g.LeaderOnly)
		if c, k := pgCode(e); c == pgUniqueViolation {
			if k == "np_group_pkey" {
				return ErrConflict
			}
			return ErrGroupNameTaken
		}
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE np_extseat SET routing_enabled=true WHERE agent_id=$1`, g.LeaderAgentID); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,$2,$3)`, g.Code, g.LeaderAgentID, g.CreatedAt)
		return e
	})
}
