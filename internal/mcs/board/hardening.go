// hardening.go —— M4:steer/interrupt 预留-终结、redeliver/deliver_fail 投递收敛、result_outbox。
//
// 语义来源(冻结件):M0 3.1a(steer reserve→finalize;uncertain 不放行;rejected 唯一前置=driver 终局
// NOT_APPLIED_FENCED)/3.1c(interrupt durable intent;B 模型 finalize 只 CAS 请求行,授权由请求行→node→task→board
// 推导;事件记预留代际)/3.5 redeliver·deliver_fail 行(delivery_attempt 预算;UNKNOWN→保持 dispatched+
// delivery_unresolved 注记,不迁 blocked/failed)/3.6 result_outbox(外呼前落盘;result_key 每版恰一次)。
package board

import (
	"context"
	"database/sql"
	"fmt"
)

// ---- 3.1a steer:reserve → (外呼) → finalize ----

// SteerReserve TX-reserve:完整 guard+per-(node,generation) 未决互斥(steer/interrupt 共用,按代分域,
// 旧代 uncertain 不堵新代)。不改 node 任何列。rowcount==0 ⇒ 拒(stale/busy/guard),零副作用。
func (b *Board) SteerReserve(ctx context.Context, nodeID string, gen int64, rid string, ttlSec int64, id Identity) (Outcome, error) {
	return b.reserve(ctx, nodeID, gen, rid, "steer", ttlSec, id)
}

// InterruptIntent 3.1c TX-intent:与 steer 同构的持久意图(外部副作用前必须先有可恢复凭据)。
func (b *Board) InterruptIntent(ctx context.Context, nodeID string, gen int64, rid string, ttlSec int64, id Identity) (Outcome, error) {
	return b.reserve(ctx, nodeID, gen, rid, "interrupt", ttlSec, id)
}

func (b *Board) reserve(ctx context.Context, nodeID string, gen int64, rid, op string, ttlSec int64, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	if oc, hit, err := priorOutcome(ctx, tx, rid); err != nil {
		return "", err
	} else if hit {
		return oc, nil
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO node_request(request_id,node_id,generation,op,expected_state,outcome,expires_ts,applied_ts)
SELECT ?,?,?,?,'running','pending',?,?
 WHERE EXISTS (SELECT 1 FROM node WHERE node_id=? AND generation=? AND status='running'
   AND EXISTS (SELECT 1 FROM orchestrator o JOIN task t ON t.board_id=o.board_id
               WHERE t.task_id=node.task_id AND o.owner=? AND o.lease_expiry>=? AND o.epoch=?))
   AND NOT EXISTS (SELECT 1 FROM node_request r
               WHERE r.node_id=? AND r.generation=? AND r.outcome IN ('pending','uncertain'))`,
		rid, nodeID, gen, op, now+ttlSec, now,
		nodeID, gen, id.Owner, now, id.Epoch,
		nodeID, gen)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeCASMiss, tx.Commit()
	}
	return "pending", tx.Commit()
}

// SteerFinalize 3.1a TX-finalize,只接受【确定】结果:
//   - applied=true(确证已注入):CAS 预留态+目标代际仍 running+guard;event node_steered 仅此刻产生;
//   - applied=false:调用方必须持 driver 权威的【终局 NOT_APPLIED/FENCED】证据(★五核#1;
//     "未查到/当前未注入"不是终局,不得调本分支)——CAS+guard→rejected,无结果事件(预留行即审计)。
//
// late/重复:CAS rowcount=0 零写入(账面防线;外部副作用由 driver fence 消,不由此论无害)。
func (b *Board) SteerFinalize(ctx context.Context, rid string, applied bool, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	var nodeID string
	var gen int64
	if err := tx.QueryRowContext(ctx, `SELECT node_id, generation FROM node_request WHERE request_id=? AND op='steer'`, rid).
		Scan(&nodeID, &gen); err != nil {
		if err == sql.ErrNoRows {
			return OutcomeCASMiss, tx.Commit()
		}
		return "", err
	}
	guardExists := `EXISTS (SELECT 1 FROM orchestrator o JOIN task t ON t.board_id=o.board_id JOIN node nd ON nd.task_id=t.task_id
       WHERE nd.node_id=node_request.node_id AND o.owner=? AND o.lease_expiry>=? AND o.epoch=?)`
	var res sql.Result
	if applied {
		res, err = tx.ExecContext(ctx, `
UPDATE node_request SET outcome='applied'
 WHERE request_id=? AND outcome IN ('pending','uncertain')
   AND EXISTS (SELECT 1 FROM node WHERE node_id=node_request.node_id
               AND generation=node_request.generation AND status='running')
   AND `+guardExists, rid, id.Owner, now, id.Epoch)
	} else {
		res, err = tx.ExecContext(ctx, `
UPDATE node_request SET outcome='rejected'
 WHERE request_id=? AND outcome IN ('pending','uncertain')
   AND `+guardExists, rid, id.Owner, now, id.Epoch)
	}
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeCASMiss, tx.Commit()
	}
	if applied {
		if err := insertEvent(ctx, tx, "node_steered", nodeID, gen, now, map[string]any{"rid": rid}); err != nil {
			return "", err
		}
		return OutcomeApplied, tx.Commit()
	}
	return OutcomeRejected, tx.Commit()
}

// InterruptFinalize 3.1c TX-finalize(★七核#1 B 模型):只 CAS【请求行自身】,不要求 node 当前
// generation/status——换代穿越后旧代 intent 仍可确定终结;授权由请求行→node→task→board 推导;
// applied 的 node_interrupted 事件记【预留代际(可为旧代)】,不冒充当前代。
func (b *Board) InterruptFinalize(ctx context.Context, rid string, applied bool, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	var nodeID string
	var resGen int64
	if err := tx.QueryRowContext(ctx, `SELECT node_id, generation FROM node_request WHERE request_id=? AND op='interrupt'`, rid).
		Scan(&nodeID, &resGen); err != nil {
		if err == sql.ErrNoRows {
			return OutcomeCASMiss, tx.Commit()
		}
		return "", err
	}
	outcome := "rejected"
	if applied {
		outcome = "applied"
	}
	res, err := tx.ExecContext(ctx, `
UPDATE node_request SET outcome=?
 WHERE request_id=? AND op='interrupt' AND outcome IN ('pending','uncertain')
   AND EXISTS (SELECT 1 FROM orchestrator o JOIN task t ON t.board_id=o.board_id JOIN node nd ON nd.task_id=t.task_id
               WHERE nd.node_id=node_request.node_id AND o.owner=? AND o.lease_expiry>=? AND o.epoch=?)`,
		outcome, rid, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeCASMiss, tx.Commit()
	}
	if applied {
		if err := insertEvent(ctx, tx, "node_interrupted", nodeID, resGen, now, map[string]any{"rid": rid, "reservation_generation": resGen}); err != nil {
			return "", err
		}
		return OutcomeApplied, tx.Commit()
	}
	return OutcomeRejected, tx.Commit()
}

// ---- 3.5 redeliver / deliver_fail ----

// Redeliver 同 rid 传输重投(guarded):delivery_attempt<max_delivery 时 +1(★MC3-2 传输预算,
// 不耗执行 attempt)+event node_redelivered;耗尽/态不符 ⇒ cas_miss(转 DeliverFail 判定)。
func (b *Board) Redeliver(ctx context.Context, nodeID string, gen int64, rid string, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	res, err := tx.ExecContext(ctx, `
UPDATE node SET delivery_attempt=delivery_attempt+1, updated_ts=?
 WHERE node_id=? AND generation=? AND status='dispatched' AND delivery_id=?
   AND delivery_attempt < max_delivery`+guardSQL,
		now, nodeID, gen, rid, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeCASMiss, tx.Commit()
	}
	if err := insertEvent(ctx, tx, "node_redelivered", nodeID, gen, now, map[string]any{"rid": rid}); err != nil {
		return "", err
	}
	return OutcomeApplied, tx.Commit()
}

// DeliverFail 投递终局判定(★MC1/R4 守 DEA):
//   - deaTerminal=true(driver 权威【终局 NOT_APPLIED/FENCED】)∧ delivery_attempt≥max_delivery
//     → failed(reason=delivery_exhausted)+event node_failed;
//   - deaTerminal=false(UNKNOWN/无终局证据)→【不得迁 failed,也不得迁 blocked】:
//     保持 dispatched + annotate(delivery_unresolved)+event node_annotated——
//     注记期拒 reassign(3.2 谓词已接),同 rid processing/terminal 正常收敛并同事务清注记。
func (b *Board) DeliverFail(ctx context.Context, nodeID string, gen int64, rid string, deaTerminal bool, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	if deaTerminal {
		res, err := tx.ExecContext(ctx, `
UPDATE node SET status='failed', ended_ts=?, updated_ts=?
 WHERE node_id=? AND generation=? AND status='dispatched' AND delivery_id=?
   AND delivery_attempt >= max_delivery`+guardSQL,
			now, now, nodeID, gen, rid, id.Owner, now, id.Epoch)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return OutcomeCASMiss, tx.Commit()
		}
		if err := insertEvent(ctx, tx, "node_failed", nodeID, gen, now, map[string]any{"reason": "delivery_exhausted", "rid": rid}); err != nil {
			return "", err
		}
		return OutcomeApplied, tx.Commit()
	}
	res, err := tx.ExecContext(ctx, `
UPDATE node SET blocker_reason='delivery_unresolved', updated_ts=?
 WHERE node_id=? AND generation=? AND status='dispatched' AND delivery_id=?
   AND (blocker_reason IS NULL OR blocker_reason='delivery_unresolved')`+guardSQL,
		now, nodeID, gen, rid, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeCASMiss, tx.Commit()
	}
	if err := insertEvent(ctx, tx, "node_annotated", nodeID, gen, now, map[string]any{"reason": "delivery_unresolved", "rid": rid}); err != nil {
		return "", err
	}
	return OutcomeApplied, tx.Commit()
}

// ---- 3.6 result_outbox ----

// ResultKey 组 result_key(★R3:task:origin_envelope:v{version}——每【版】恰一次)。
func ResultKey(taskID, originEnvelopeID string, version int64) string {
	return fmt.Sprintf("%s:%s:v%d", taskID, originEnvelopeID, version)
}

// EnqueueResult 外呼【前】落盘持久意图;同 key 幂等(已在=不重复,返 false)。
func (b *Board) EnqueueResult(ctx context.Context, resultKey, taskID, originAddr, payloadRef string) (bool, error) {
	now := b.now()
	res, err := b.db.ExecContext(ctx, `
INSERT INTO result_outbox(result_key,task_id,origin_addr,payload_ref,created_ts,updated_ts)
VALUES(?,?,?,?,?,?) ON CONFLICT(result_key) DO NOTHING`,
		resultKey, taskID, originAddr, payloadRef, now, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// AckResult 收到接收侧 ACK(等价 APPLIED):pending→acked 恰一次。
func (b *Board) AckResult(ctx context.Context, resultKey string) (bool, error) {
	res, err := b.db.ExecContext(ctx, `
UPDATE result_outbox SET state='acked', updated_ts=? WHERE result_key=? AND state='pending'`,
		b.now(), resultKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// PendingResults 待投列表(重投循环输入;未知 ACK 只表示未知→保持 pending 指数退避重投同 key)。
func (b *Board) PendingResults(ctx context.Context) ([]string, error) {
	rows, err := b.db.QueryContext(ctx, `SELECT result_key FROM result_outbox WHERE state='pending' ORDER BY created_ts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
