package tempgroup

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

var ErrNotFound = errors.New("临时编组不存在或无权访问")
var ErrChanged = errors.New("临时编组已变化，请刷新后重试")

type Member struct {
	AgentID  string `json:"agent_id"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	Online   bool   `json:"online"`
}
type View struct {
	Code             extshared.AGCode `json:"code"`
	Generation       int64            `json:"generation"`
	ExternalFullName string           `json:"external_full_name"`
	CreatedByDisplay string           `json:"created_by_display"`
	OwnerLC          string           `json:"owner_lc"`
	CreatedAt        time.Time        `json:"created_at"`
	LastUsedAt       *time.Time       `json:"last_used_at"`
	ExpiresAt        time.Time        `json:"expires_at"`
	DissolvedAt      *time.Time       `json:"dissolved_at"`
	DissolveReason   string           `json:"dissolve_reason"`
	CanDissolve      bool             `json:"can_dissolve"`
	Members          []Member         `json:"members"`
}

// List 个人侧栏只按内部对端属当前登录者过滤；创建者和管理员无额外豁免。
func (s *Service) List(ctx context.Context, actor string) ([]View, error) {
	out := make([]View, 0)
	rows, err := s.Pool.Query(ctx, repo.SystemCtx(), `SELECT g.code FROM np_temp_group g JOIN np_agent a ON a.id=g.target_agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE a.owner_lc=$1 AND a.state<>'revoked' AND u.state='active' ORDER BY g.created_at,g.code`, actor)
	if err != nil {
		return nil, err
	}
	var codes []string
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			rows.Close()
			return nil, err
		}
		codes = append(codes, code)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, code := range codes {
		v, e := s.Detail(ctx, actor, extshared.AGCode(code), false)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Service) Detail(ctx context.Context, actor string, code extshared.AGCode, allowAdmin bool) (View, error) {
	var v View
	err := s.Pool.InTx(ctx, repo.SystemCtx(), nil, func(tx repo.Tx) error { var e error; v, e = detail(ctx, tx, actor, code, allowAdmin); return e })
	return v, err
}
func permitted(ctx context.Context, tx repo.Tx, actor string, code extshared.AGCode, admin bool) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM np_temp_group g JOIN np_agent a ON a.id=g.target_agent_id JOIN np_user u ON u.user_id_lc=$1 WHERE g.code=$2 AND u.state='active' AND ((a.owner_lc=$1 AND a.state<>'revoked') OR ($3 AND u.type='platform_admin')))`, actor, string(code), admin).Scan(&ok)
	return ok, err
}
func detail(ctx context.Context, tx repo.Tx, actor string, code extshared.AGCode, admin bool) (View, error) {
	var v View
	ok, err := permitted(ctx, tx, actor, code, admin)
	if err != nil {
		return v, err
	}
	if !ok {
		return v, ErrNotFound
	}
	err = tx.QueryRow(ctx, `SELECT code,generation,created_by_display,owner_lc,created_at,last_used_at,expires_at,dissolved_at,dissolve_reason FROM np_temp_group WHERE code=$1`, string(code)).Scan(&v.Code, &v.Generation, &v.CreatedByDisplay, &v.OwnerLC, &v.CreatedAt, &v.LastUsedAt, &v.ExpiresAt, &v.DissolvedAt, &v.DissolveReason)
	if err != nil {
		return v, err
	}
	rows, err := tx.Query(ctx, `SELECT a.id,a.name||'.'||u.short_name,m.role FROM np_temp_group_member m JOIN np_agent a ON a.id=m.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE m.code=$1 ORDER BY m.role`, string(code))
	if err != nil {
		return v, err
	}
	defer rows.Close()
	v.Members = make([]Member, 0, 2)
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.AgentID, &m.FullName, &m.Role); err != nil {
			return v, err
		}
		if m.Role == "external" {
			v.ExternalFullName = m.FullName
		}
		v.Members = append(v.Members, m)
	}
	v.CanDissolve = v.DissolvedAt == nil
	return v, rows.Err()
}
func (s *Service) Dissolve(ctx context.Context, actor string, code extshared.AGCode, expected time.Time) error {
	return s.Pool.InTx(ctx, repo.SystemCtx(), nil, func(tx repo.Tx) error {
		var expires time.Time
		var dissolved sql.NullTime
		err := tx.QueryRow(ctx, `SELECT expires_at,dissolved_at FROM np_temp_group WHERE code=$1 FOR UPDATE`, string(code)).Scan(&expires, &dissolved)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		ok, err := permitted(ctx, tx, actor, code, true)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if dissolved.Valid || !expires.Equal(expected) {
			return ErrChanged
		}
		reason := "owner"
		now := s.now()
		if !now.Before(expires) {
			reason = "expired"
		}
		return dissolve(ctx, tx, code, actor, reason, now)
	})
}
func dissolve(ctx context.Context, tx repo.Tx, code extshared.AGCode, actor, reason string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE np_temp_group SET dissolved_at=$2,dissolve_reason=$3 WHERE code=$1 AND dissolved_at IS NULL`, string(code), at, reason)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM np_group WHERE code=$1`, string(code))
	if err != nil {
		return err
	}
	return audit(ctx, tx, actor, "temp_group_dissolve_"+reason, code, at)
}
