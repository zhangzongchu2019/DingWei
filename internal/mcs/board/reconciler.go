// reconciler.go —— M4:崩溃恢复/兜底扫描(权威兜底,非主路径)。
//
// 语义来源(冻结件):M0 3.1a ②'(pending 过期→uncertain 降级,【非】放行)/3.5 timeout 注记行
// (仅 running 候选;annotate 不迁移 status)/不变量 #15(双集恒空:pending∧∀dep=done 与
// blocked(dep_failed)∧∀dep=done 均非稳定可观测态——主路径=fan-out,本扫描=兜底,发现即修+告警)/
// send crash cuts(dispatched 无回执超 T_redeliver→redeliver;耗尽→DeliverFail 判定,由驱动 DEA 结果驱动)。
package board

import (
	"context"
	"encoding/json"
)

// SweepReport 一轮兜底扫描的动作账(观测/告警用)。
type SweepReport struct {
	ExpiredReservations int // pending→uncertain 降级数
	TimeoutAnnotated    int // running 超时注记数
	RedeliverCandidates []string // dispatched 超 T 无回执的 (node) —— 由调用方对每个执行 Redeliver+重投外呼
	StrayPromoted       int // #15 兜底:pending∧∀dep=done → ready(本应恒空,>0=主路径缺陷告警)
	StrayRecovered      int // #15 兜底:blocked(dep_failed)∧∀dep=done → ready(同上)
}

// Sweep 执行一轮兜底扫描。redeliverAfterSec/timeoutSlaCheck 由调用方给(D15 数值域)。
func (b *Board) Sweep(ctx context.Context, id Identity, redeliverAfterSec int64) (SweepReport, error) {
	var rep SweepReport
	now := b.now()

	// ① steer/interrupt 预留过期降级:pending→uncertain(栅栏继续,绝非放行,★四核#1)
	res, err := b.db.ExecContext(ctx,
		`UPDATE node_request SET outcome='uncertain' WHERE outcome='pending' AND expires_ts <= ?`, now)
	if err != nil {
		return rep, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		rep.ExpiredReservations = int(n)
	}

	// ② timeout 注记(仅 running 候选;注记型不迁移 status)
	rows, err := b.db.QueryContext(ctx, `
SELECT node_id, generation FROM node
 WHERE status='running' AND sla_deadline IS NOT NULL AND sla_deadline < ? AND blocker_reason IS NULL`, now)
	if err != nil {
		return rep, err
	}
	type ng struct {
		n string
		g int64
	}
	var timeouts []ng
	for rows.Next() {
		var x ng
		if err := rows.Scan(&x.n, &x.g); err != nil {
			rows.Close()
			return rep, err
		}
		timeouts = append(timeouts, x)
	}
	rows.Close()
	for _, x := range timeouts {
		if oc, err := b.SetBlocker(ctx, x.n, x.g, "timeout", id); err != nil {
			return rep, err
		} else if oc == OutcomeApplied {
			rep.TimeoutAnnotated++
		}
	}

	// ③ redeliver 候选:dispatched 持续无 processing 回执(updated_ts 早于 T_redeliver 窗)
	rows, err = b.db.QueryContext(ctx, `
SELECT node_id FROM node WHERE status='dispatched' AND updated_ts <= ?
   AND (blocker_reason IS NULL)`, now-redeliverAfterSec)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var nid string
		if err := rows.Scan(&nid); err != nil {
			rows.Close()
			return rep, err
		}
		rep.RedeliverCandidates = append(rep.RedeliverCandidates, nid)
	}
	rows.Close()

	// ④ 不变量 #15 双集兜底(本应恒空;发现=主路径缺陷,修+告警事件)
	strayP, err := b.sweepStray(ctx, "pending", "node_ready", id.Owner)
	if err != nil {
		return rep, err
	}
	rep.StrayPromoted = strayP
	strayR, err := b.sweepStray(ctx, "blocked", "node_recovered", id.Owner)
	if err != nil {
		return rep, err
	}
	rep.StrayRecovered = strayR
	return rep, nil
}

// sweepStray 扫描 status∈{pending,blocked(dep_failed)} 且 ∀dep=done 的漏网节点→ready+事件+告警标记。
func (b *Board) sweepStray(ctx context.Context, status, evtKind, actor string) (int, error) {
	q := `SELECT node_id, generation, deps FROM node WHERE status=?`
	if status == "blocked" {
		q += ` AND blocker_reason='dep_failed'`
	}
	rows, err := b.db.QueryContext(ctx, q, status)
	if err != nil {
		return 0, err
	}
	type cand struct {
		id   string
		gen  int64
		deps string
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.gen, &c.deps); err != nil {
			rows.Close()
			return 0, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	fixed := 0
	now := b.now()
	for _, c := range cands {
		var deps []string
		if err := json.Unmarshal([]byte(c.deps), &deps); err != nil {
			return fixed, err
		}
		if len(deps) == 0 {
			if status == "pending" { // born 无 dep 却停 pending=建图缺陷,兜底提升
				deps = nil
			} else {
				continue
			}
		}
		allDone := true
		for _, dep := range deps {
			var st string
			if err := b.db.QueryRowContext(ctx, `SELECT status FROM node WHERE node_id=?`, dep).Scan(&st); err != nil {
				return fixed, err
			}
			if st != "done" {
				allDone = false
				break
			}
		}
		if !allDone {
			continue
		}
		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			return fixed, err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE node SET status='ready', blocker_reason=NULL, updated_ts=? WHERE node_id=? AND status=?`,
			now, c.id, status)
		if err != nil {
			tx.Rollback()
			return fixed, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			if err := insertEvent(ctx, tx, evtKind, c.id, c.gen, now, actor,
				map[string]any{"by": "reconciler", "alert": "invariant15_stray"}); err != nil {
				tx.Rollback()
				return fixed, err
			}
			fixed++
		}
		if err := tx.Commit(); err != nil {
			return fixed, err
		}
	}
	return fixed, nil
}
