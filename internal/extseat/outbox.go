package extseat

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// ProjectOne 持有PG outbox行锁投影一个稳定ID。投影后PG提交失败时，下一轮相同ID重放。
// 失败不标projected，不把受理当成helper收件，也不改SQLite原来的重试/ACK策略。
func (s *Service) ProjectOne(ctx context.Context, p Projector) (bool, error) {
	if p == nil {
		return false, extshared.ErrNotPermitted
	}
	did := false
	now := s.now()
	err := s.tx(ctx, func(tx repo.Tx) error {
		var d Delivery
		var seatID, targetID string
		var a Seat
		e := tx.QueryRow(ctx, `SELECT delivery_id,seat_id,target_agent_id,source_name,target_name,body,ag_code,ag_generation,created_at,expires_at FROM np_extseat_outbox WHERE state='queued' ORDER BY created_at,delivery_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&d.ID, &seatID, &targetID, &d.From, &d.To, &d.Body, &a.Group, &a.Generation, &d.CreatedAt, &d.ExpiresAt)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		did = true
		dead := func(reason string) error {
			_, e := tx.Exec(ctx, `UPDATE np_extseat_outbox SET state='dead',dead_reason=$2 WHERE delivery_id=$1`, d.ID, reason)
			return e
		}
		if !now.Before(d.ExpiresAt) {
			return dead("message_expired")
		}
		a.ID = seatID
		a.AgentID = seatID
		var active bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM np_extseat s JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE s.agent_id=$1 AND s.revoked_at IS NULL AND a.state='active' AND u.state='active')`, seatID).Scan(&active); e != nil {
			return e
		}
		if !active {
			return dead("seat_revoked")
		}
		g, e := s.Groups.RecheckGroup(ctx, tx, a, now)
		if e != nil {
			if errors.Is(e, extshared.ErrGroupDissolved) || errors.Is(e, extshared.ErrTargetNotInAG) {
				return dead("group_unavailable")
			}
			return e
		}
		if g.Target.ID != targetID {
			return dead("target_not_in_ag")
		}
		d.Generation = 1
		d.Group = g.Code
		if e = p.Project(ctx, d, g.Target); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE np_extseat_outbox SET state='projected',projected_at=$2 WHERE delivery_id=$1`, d.ID, now)
		return e
	})
	return did, err
}

// Run 供集成装配为受控后台任务；ctx取消即停止。每轮限定批量，错误交调用方退避处理。
func (s *Service) Run(ctx context.Context, p Projector) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if e := s.Expire(ctx); e != nil {
			return e
		}
		for i := 0; i < 32; i++ {
			did, e := s.ProjectOne(ctx, p)
			if e != nil {
				return e
			}
			if !did {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
