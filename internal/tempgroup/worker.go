package tempgroup

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"time"
)

// Expire 分批锁组，与发送/续期/主动解散互斥；权限拒绝不依赖扫描及时性。
func (s *Service) Expire(ctx context.Context) error {
	return s.Pool.InTx(ctx, s.Access, nil, func(tx repo.Tx) error {
		now := s.now()
		rows, err := tx.Query(ctx, `SELECT g.code,CASE WHEN g.expires_at<=$1 THEN 'expired' ELSE 'revoked' END FROM np_temp_group g JOIN np_agent a ON a.id=g.external_agent_id JOIN np_agent t ON t.id=g.target_agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc JOIN np_user v ON v.user_id_lc=t.owner_lc WHERE g.dissolved_at IS NULL AND (g.expires_at<=$1 OR a.state='revoked' OR t.state='revoked' OR u.state<>'active' OR v.state<>'active') ORDER BY g.expires_at,g.code LIMIT 200 FOR UPDATE OF g SKIP LOCKED`, now)
		if err != nil {
			return err
		}
		type item struct {
			code   extshared.AGCode
			reason string
		}
		var items []item
		for rows.Next() {
			var i item
			if err = rows.Scan(&i.code, &i.reason); err != nil {
				rows.Close()
				return err
			}
			items = append(items, i)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, i := range items {
			if err = dissolve(ctx, tx, i.code, "system", i.reason, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// NotifyPending 留持久待通知标记。故障后重试，同一终态由接收者按 code 幂等收敛。
func (s *Service) NotifyPending(ctx context.Context) error {
	if s.Notifier == nil {
		return nil
	}
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	rows, err := s.Pool.Query(ctx, s.Access, `SELECT code,dissolve_reason FROM np_temp_group WHERE dissolved_at IS NOT NULL AND notified_at IS NULL ORDER BY dissolved_at LIMIT 200`)
	if err != nil {
		return err
	}
	type item struct {
		code   extshared.AGCode
		reason string
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.code, &i.reason); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		if s.Finalize != nil {
			if err = s.Finalize(ctx, i.code); err != nil {
				return err
			}
		}
		s.Notifier.TempGroupDissolved(ctx, i.code, i.reason)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err = s.Pool.Exec(ctx, s.Access, `UPDATE np_temp_group SET notified_at=$2 WHERE code=$1 AND notified_at IS NULL`, string(i.code), s.now()); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		for _, f := range []func(context.Context) error{s.Expire, s.NotifyPending} {
			if err := f(ctx); err != nil && s.OnBackgroundError != nil {
				s.OnBackgroundError(err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
