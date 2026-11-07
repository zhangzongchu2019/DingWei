// Package tempgroup 实现 PG 权威临时编组及其 identity 可达性镜像。
package tempgroup

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

type TempGroupNotifier interface {
	TempGroupDissolved(context.Context, extshared.AGCode, string)
}
type Service struct {
	notifyMu          sync.Mutex
	Pool              *repo.Pool
	Now               func() time.Time
	Notifier          TempGroupNotifier
	Finalize          func(context.Context, extshared.AGCode) error
	OnBackgroundError func(error)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

var _ extseat.Groups = (*Service)(nil)

func (s *Service) CreateTempGroup(ctx context.Context, tx repo.Tx, r extseat.CreateGroupRequest) (extseat.Group, error) {
	var g extseat.Group
	if r.CreateExternal == nil || r.ExternalAgentID == "" || r.Target.ID == "" || r.ExternalAgentID == r.Target.ID || r.OwnerLC == "" {
		return g, extshared.ErrNotPermitted
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT a.state='active' AND a.origin<>'external' AND u.state='active' FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.id=$1 FOR SHARE OF a,u`, r.Target.ID).Scan(&active); err != nil {
		return g, err
	}
	if !active {
		return g, extshared.ErrTargetNotInAG
	}
	var n int64
	if err := tx.QueryRow(ctx, `SELECT nextval('np_temp_group_sequence')`).Scan(&n); err != nil {
		return g, err
	}
	code, err := extshared.FormatAGCode(n)
	if err != nil {
		return g, err
	}
	g = extseat.Group{Code: code, Generation: 1, Target: r.Target, ExpiresAt: r.At.Add(extshared.GroupTTL)}
	if err = r.CreateExternal(g); err != nil {
		return g, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO np_temp_group(code,seq,created_by,created_by_union,created_by_display,owner_lc,external_agent_id,target_agent_id,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, string(code), n, r.Principal.Key(), r.Principal.UnionID, r.Principal.DisplayName, r.OwnerLC, r.ExternalAgentID, r.Target.ID, r.At, g.ExpiresAt)
	if err != nil {
		return g, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO np_temp_group_member(code,agent_id,role,created_at) VALUES($1,$2,'external',$4),($1,$3,'internal',$4)`, string(code), r.ExternalAgentID, r.Target.ID, r.At)
	if err != nil {
		return g, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,additions_closed,leader_only,created_at,updated_at) VALUES($1,$2,$3,$4,true,true,$5,$5)`, string(code), string(code)+"-temp", r.OwnerLC, r.Target.ID, r.At)
	if err != nil {
		return g, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,$2,$4),($1,$3,$4)`, string(code), r.ExternalAgentID, r.Target.ID, r.At)
	if err != nil {
		return g, err
	}
	return g, audit(ctx, tx, r.OwnerLC, "temp_group_create", code, r.At)
}

func (s *Service) RecheckGroup(ctx context.Context, tx repo.Tx, a extseat.Seat, at time.Time) (extseat.Group, error) {
	var g extseat.Group
	var target string
	var dissolved sql.NullTime
	err := tx.QueryRow(ctx, `SELECT g.code,g.generation,g.target_agent_id,g.expires_at,g.dissolved_at FROM np_temp_group g JOIN np_temp_group_member m ON m.code=g.code WHERE m.agent_id=$1 AND m.role='external' FOR UPDATE OF g`, a.AgentID).Scan(&g.Code, &g.Generation, &target, &g.ExpiresAt, &dissolved)
	if errors.Is(err, sql.ErrNoRows) {
		return g, extshared.ErrGroupDissolved
	}
	if err != nil {
		return g, err
	}
	if dissolved.Valid || !at.Before(g.ExpiresAt) || (a.Group != "" && a.Group != g.Code) || (a.Generation != 0 && a.Generation != g.Generation) {
		return g, extshared.ErrGroupDissolved
	}
	err = tx.QueryRow(ctx, `SELECT a.id,a.owner_lc,a.name,a.group_code,a.origin,a.state,a.created_at FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.id=$1 AND u.state='active' FOR SHARE OF a,u`, target).Scan(&g.Target.ID, &g.Target.OwnerLC, &g.Target.Name, &g.Target.GroupID, &g.Target.Origin, &g.Target.State, &g.Target.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return g, extshared.ErrTargetNotInAG
	}
	if err != nil {
		return g, err
	}
	if g.Target.State != identity.AgentActive || g.Target.Origin == identity.OriginExternal {
		return g, extshared.ErrTargetNotInAG
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT count(*)=2 AND count(*) FILTER(WHERE role='external' AND agent_id=$2)=1 AND count(*) FILTER(WHERE role='internal' AND agent_id=$3)=1 FROM np_temp_group_member WHERE code=$1`, string(g.Code), a.AgentID, target).Scan(&valid)
	if err != nil {
		return g, err
	}
	if !valid {
		return g, extshared.ErrTargetNotInAG
	}
	return g, nil
}
func (s *Service) TouchGroup(ctx context.Context, tx repo.Tx, g extseat.Group, key string, kind extshared.GroupUseKind, at time.Time) error {
	if key == "" || (kind != extshared.UseAcceptedSend && kind != extshared.UseFirstReceivedAck) {
		return extshared.ErrNotPermitted
	}
	var generation int64
	var expires time.Time
	var dissolved sql.NullTime
	err := tx.QueryRow(ctx, `SELECT generation,expires_at,dissolved_at FROM np_temp_group WHERE code=$1 FOR UPDATE`, string(g.Code)).Scan(&generation, &expires, &dissolved)
	if errors.Is(err, sql.ErrNoRows) {
		return extshared.ErrGroupDissolved
	}
	if err != nil {
		return err
	}
	if dissolved.Valid || generation != g.Generation || !at.Before(expires) {
		return extshared.ErrGroupDissolved
	}
	var old string
	err = tx.QueryRow(ctx, `SELECT kind FROM np_temp_group_use WHERE code=$1 AND event_key=$2`, string(g.Code), key).Scan(&old)
	if err == nil {
		if old != string(kind) {
			return extshared.ErrIdempotencyKey
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO np_temp_group_use(code,event_key,kind,at) VALUES($1,$2,$3,$4)`, string(g.Code), key, string(kind), at)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE np_temp_group SET last_used_at=GREATEST(last_used_at,$2),expires_at=GREATEST(expires_at,$3) WHERE code=$1`, string(g.Code), at, at.Add(extshared.GroupTTL))
	return err
}
func audit(ctx context.Context, tx repo.Tx, actor, action string, code extshared.AGCode, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(actor,action,target,after_json,ts) VALUES($1,$2,$3,'{}',$4)`, actor, action, string(code), at)
	return err
}
