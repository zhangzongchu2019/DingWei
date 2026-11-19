package extseat

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// ExistingSeats 只对已经验证的同一四元组主体返回稳定席位选择，不按显示名合并。
func (s *Service) ExistingSeats(ctx context.Context, p extshared.Principal) ([]extshared.SeatChoice, error) {
	if !validPrincipal(p) || (extshared.SameTenantAdmission{}).Check(ctx, p, s.TenantKey) != nil {
		return nil, extshared.ErrTenantRejected
	}
	out := []extshared.SeatChoice{}
	err := s.tx(ctx, func(tx repo.Tx) error {
		rows, e := tx.Query(ctx, `SELECT s.agent_id,a.name||'.'||u.short_name FROM np_extseat s JOIN np_external_principal p ON p.id=s.principal_id JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE p.provider=$1 AND p.tenant_key=$2 AND p.issuer=$3 AND p.subject=$4 AND s.revoked_at IS NULL AND a.state='active' AND u.state='active' ORDER BY s.agent_id`, p.Provider, p.TenantKey, p.Issuer, p.Subject)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var c extshared.SeatChoice
			if e = rows.Scan(&c.ID, &c.FullName); e != nil {
				return e
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) Reauthenticate(ctx context.Context, p extshared.Principal, seatID string) (string, Seat, string, error) {
	var seat Seat
	if s.Resource == "" || seatID == "" || !validPrincipal(p) || (extshared.SameTenantAdmission{}).Check(ctx, p, s.TenantKey) != nil {
		return "", seat, "", extshared.ErrNotPermitted
	}
	id, e := s.random(16)
	if e != nil {
		return "", seat, "", e
	}
	secret, e := s.random(32)
	if e != nil {
		return "", seat, "", e
	}
	raw := id + "." + secret
	e = s.tx(ctx, func(tx repo.Tx) error {
		seat.Principal = p
		err := tx.QueryRow(ctx, `SELECT s.agent_id,a.owner_lc,a.name,u.short_name,COALESCE(s.ag_code,''),COALESCE(s.ag_generation,0),s.auth_generation FROM np_extseat s JOIN np_external_principal p ON p.id=s.principal_id JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE s.agent_id=$1 AND p.provider=$2 AND p.tenant_key=$3 AND p.issuer=$4 AND p.subject=$5 AND s.revoked_at IS NULL AND a.state='active' AND u.state='active' FOR UPDATE OF s,a,u`, seatID, p.Provider, p.TenantKey, p.Issuer, p.Subject).Scan(&seat.AgentID, &seat.OwnerLC, &seat.Name, &seat.ownerShort, &seat.Group, &seat.Generation, &seat.AuthGeneration)
		if errors.Is(err, sql.ErrNoRows) {
			return extshared.ErrNotPermitted
		}
		if err != nil {
			return err
		}
		now := s.now()
		seat.ID = seat.AgentID
		seat.FullName = seat.Name + "." + seat.ownerShort
		seat.TokenID = id
		seat.ExpiresAt = now.Add(extshared.TokenTTL)
		if _, err = tx.Exec(ctx, `UPDATE np_extseat SET routing_enabled=true WHERE agent_id=$1`, seatID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO np_extseat_token(id,token_hash,seat_id,auth_generation,created_at,last_auth_at,expires_at,audience,scopes) VALUES($1,$2,$3,$4,$5,$5,$6,$7,$8)`, id, identity.HashSecret(raw), seatID, seat.AuthGeneration, now, seat.ExpiresAt, s.Resource, extshared.ScopeString()); err != nil {
			return err
		}
		return s.audit(ctx, tx, seat.OwnerLC, "extseat.reauthenticated", seatID, now)
	})
	if e != nil {
		return "", Seat{}, "", e
	}
	seat.proof = identity.HashSecret(raw)
	return raw, seat, string(seat.Group), nil
}

// HasRoutingSeats 仅供可信进程装配决定回退后是否保留已有身份的重认证入口。
func (s *Service) HasRoutingSeats(ctx context.Context) (bool, error) {
	var found bool
	err := s.tx(ctx, func(tx repo.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM np_extseat WHERE routing_enabled)`).Scan(&found)
	})
	return found, err
}
