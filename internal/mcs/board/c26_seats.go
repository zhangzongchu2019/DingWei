// c26_seats.go —— P3 C2.6 2-Agent 守门(契约 C2.6/P1 §4.2·§4.2a,dev56①/D4=a)。
//
// 执行席=implementer+tester(占席);designer/qa_owner=轻角色不占席(§4.2a)。
// 席位数口径:活跃执行席 = 该 task 上 (implementer/tester ∪ promoted=1 轻角色) 的 DISTINCT who,
// 豁免(role_exemption)中的 who 不计入。
// 安全属性:
//   - 升格(PromoteExecSeat)=调度者 guarded 原语("持续产出执行中间产物"的判定在调度者/评审侧,
//     Board 只执行升格本身);升格触发【活跃执行席≤2】校验,第 3 席→rejected 零写入
//     (换席或显式豁免),不静默突破;
//   - 豁免(GrantRoleExemption)=调度者 guarded 写+role_exemption{task,who,reason} 事件落账
//     (可审计,不可匿名突破;reason 必填);
//   - 建单(CreateTask)/角色变更(SetTaskRoles)同受 ≤2 校验(变更不得使非豁免席位>2);
//   - promoted 不随 who 变更隐式释放(fail-closed:换人不换席位占用,释放须走换席+变更校验)。
package board

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// promotableRoles 可升格轻角色集(执行席角色天然占席,无升格语义;其余角色名 default-deny)。
var promotableRoles = map[string]bool{"designer": true, "qa_owner": true}

// ErrExecSeatsExceeded 建单使活跃执行席>2(C2.6 建单守门:换席或调度者显式豁免,不静默突破)。
var ErrExecSeatsExceeded = errors.New("mcs/board: 活跃执行席>2 — 换席或调度者显式豁免(C2.6)")

// execSeatCount 事务内数活跃执行席:DISTINCT who over (implementer/tester ∪ promoted=1 ∪ extraRole),
// 豁免 who 不计。extraRole 用于升格预演("若该角色也占席"),不预演传 ""。
func execSeatCount(ctx context.Context, tx *sql.Tx, taskID, extraRole string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT r.who) FROM task_role r
 WHERE r.task_id=?
   AND (r.role IN ('implementer','tester') OR r.promoted=1 OR (?<>'' AND r.role=?))
   AND NOT EXISTS (SELECT 1 FROM role_exemption e WHERE e.task_id=r.task_id AND e.who=r.who)`,
		taskID, extraRole, extraRole).Scan(&n)
	return n, err
}

// orchGuardTask 事务内调度者 guard(owner/lease_expiry>=now/epoch,授权行由 task→board 推导,3.3 通则)。
func orchGuardTask(ctx context.Context, tx *sql.Tx, taskID string, id Identity, now int64) (bool, error) {
	var ok int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM task t JOIN orchestrator o ON o.board_id=t.board_id
 WHERE t.task_id=? AND o.owner=? AND o.lease_expiry>=? AND o.epoch=?`,
		taskID, id.Owner, now, id.Epoch).Scan(&ok); err != nil {
		return false, err
	}
	return ok == 1, nil
}

// insertTaskEvent task 级事件(强制 actor,禁匿名——与 insertEvent 同则,落 task_id 列)。
func insertTaskEvent(ctx context.Context, tx *sql.Tx, kind, taskID string, ts int64, actor string, data map[string]any) error {
	if actor == "" {
		return errors.New("mcs/board: task event without actor (禁匿名事件)")
	}
	payload := "{}"
	if data != nil {
		bs, err := json.Marshal(data)
		if err != nil {
			return err
		}
		payload = string(bs)
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO board_event(kind,task_id,agent_id,ts,data) VALUES(?,?,?,?,?)`,
		kind, taskID, actor, ts, payload)
	return err
}

// PromoteExecSeat 轻角色升格执行席(★C2.6 防绕过:名义轻角色持续产出执行中间产物→升格占席)。
// guarded(调度者);第 3 非豁免席→rejected 零写入(换席或豁免);已升格→幂等 no-op applied。
func (b *Board) PromoteExecSeat(ctx context.Context, taskID, role string, id Identity) (Outcome, error) {
	if !promotableRoles[role] { // default-deny:执行席角色/未知角色名均无升格语义
		return OutcomeRejected, nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	if ok, err := orchGuardTask(ctx, tx, taskID, id, now); err != nil {
		return "", err
	} else if !ok {
		return OutcomeCASMiss, tx.Commit() // 非调度者/租约过期/旧 epoch
	}
	var who string
	var promoted int
	err = tx.QueryRowContext(ctx, `SELECT who, promoted FROM task_role WHERE task_id=? AND role=?`, taskID, role).
		Scan(&who, &promoted)
	if errors.Is(err, sql.ErrNoRows) {
		return OutcomeRejected, tx.Commit() // 角色行不存在(建单必填全,不存在=非法目标)
	}
	if err != nil {
		return "", err
	}
	if promoted == 1 {
		return OutcomeApplied, tx.Commit() // 幂等 no-op:不二次落事件
	}
	// ★≤2 校验(升格预演):该角色计入后非豁免 DISTINCT who>2 → 拒,零写入(换席或豁免,不静默突破)
	if n, err := execSeatCount(ctx, tx, taskID, role); err != nil {
		return "", err
	} else if n > 2 {
		return OutcomeRejected, tx.Commit()
	}
	// ★F-C6-1 通则:条件 UPDATE 必检 RowsAffected==1,不假记 applied
	res, err := tx.ExecContext(ctx,
		`UPDATE task_role SET promoted=1, updated_ts=? WHERE task_id=? AND role=? AND promoted=0`,
		now, taskID, role)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return OutcomeCASMiss, tx.Commit()
	}
	if err := insertTaskEvent(ctx, tx, "role_promoted", taskID, now, id.Owner,
		map[string]any{"role": role, "who": who}); err != nil {
		return "", err
	}
	return OutcomeApplied, tx.Commit()
}

// GrantRoleExemption 执行席豁免(★C2.6:调度者 guarded 写+role_exemption{task,who,reason} 事件,
// 可审计不可匿名突破;reason 必填)。同 (task,who) 重授→幂等 no-op applied(不重复落事件)。
func (b *Board) GrantRoleExemption(ctx context.Context, taskID, who, reason string, id Identity) (Outcome, error) {
	if who == "" || reason == "" { // 豁免必须指名+带 reason(空 reason=不可审计,拒)
		return OutcomeRejected, nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	if ok, err := orchGuardTask(ctx, tx, taskID, id, now); err != nil {
		return "", err
	} else if !ok {
		return OutcomeCASMiss, tx.Commit()
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO role_exemption(task_id,who,reason,granted_by,created_ts) VALUES(?,?,?,?,?)
ON CONFLICT(task_id,who) DO NOTHING`, taskID, who, reason, id.Owner, now)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeApplied, tx.Commit() // 幂等 no-op:已有豁免不重复落事件
	}
	if err := insertTaskEvent(ctx, tx, "role_exemption", taskID, now, id.Owner,
		map[string]any{"who": who, "reason": reason}); err != nil {
		return "", err
	}
	return OutcomeApplied, tx.Commit()
}
