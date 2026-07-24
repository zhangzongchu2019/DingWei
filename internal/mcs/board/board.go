// Package board 实现多CLI调度的 Board 权威存储与 guarded 写原语。
//
// 语义来源(两签冻结件,只标条目,不复述设计):
//   - multi-cli-scheduler-M0-board-draft.md §DDL / 3.1 send / 3.1b processing /
//     3.2 reassign / 3.3 mark_done|mark_failed(含 MC2-1/MC3-1 terminal 直达一般窗、
//     A案 fan-out promote+recover) / 3.4 orchestrator 租约 / 3.3 通则(guard 五项)
//   - 不变量:实体表=唯一权威;event=审计+提示;迁移必落 event 且仅 rowcount==1 同事务。
//
// 通则实现方式:每个原语=单个 SQLite 事务;guard 全谓词在 UPDATE 的 WHERE 内
// (授权行由目标 node→task→board 推导;owner/lease_expiry>=:store_now/epoch;
// rid 幂等;steer pending 互斥)。rowcount==0 ⇒ 整事务零写入。
package board

import (
	"context"
	"database/sql"
	"encoding/json"
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Outcome node_request.outcome 值域。
type Outcome string

const (
	OutcomeApplied  Outcome = "applied"
	OutcomeCASMiss  Outcome = "cas_miss"
	OutcomeRejected Outcome = "rejected"
)

// Identity 调度者身份(guard 三项:owner/epoch;store_now 由 clk 取)。
type Identity struct {
	Owner string
	Epoch int64
}

// Board 权威存储。并发安全(SQLite 单写者+事务)。
type Board struct {
	db  *sql.DB
	clk clock.Clock

	idmu  sync.Mutex
	idcfg identityConfig // P3 身份 enforce(C4;默认 warn,调度者上线开 enforce)

	c6mu  sync.Mutex
	c6cfg c6Config // P3 C6 应急原语配置(默认硬禁用=过渡期 fail-closed,P5 上线开)
}

// Open 打开(或建)Board 库并迁移 schema。path 可为 ":memory:"(测试)。
func Open(path string, clk clock.Clock) (*Board, error) {
	if clk == nil {
		clk = clock.Real{}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("mcs/board: open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 单写者;串行化=线性化点(维度5)
	for _, pragma := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("mcs/board: %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("mcs/board: migrate: %w", err)
	}
	// ★shence F3 fail-safe:idcfg.provisional 默认 true(过渡期=启动默认态,P2 保护默认开:
	// warn 不写权威 identity_warned)。与 Router.Trusted 零值 false 同向 fail-safe。
	// principal 就绪(hub per-session 注入)后由部署侧 SetIdentityProvisional(false) 关。
	return &Board{db: db, clk: clk, idcfg: identityConfig{provisional: true}}, nil
}

// DB 底层句柄(只读观测/测试断言用;业务写一律走 guarded 原语,禁裸写)。
func (b *Board) DB() *sql.DB { return b.db }

// Close 关闭底层库。
func (b *Board) Close() error { return b.db.Close() }

func (b *Board) now() int64 { return b.clk.Now().Unix() }

// ---- 3.4 orchestrator 租约 ----

// Takeover 接管/首任(所有权变更):epoch+1,同事务(§7 7.3)。
func (b *Board) Takeover(ctx context.Context, boardID, me string, ttlSec int64) (epoch int64, err error) {
	now := b.now()
	res, err := b.db.ExecContext(ctx, `
INSERT INTO orchestrator(board_id, owner, epoch, lease_expiry) VALUES(?,?,1,?)
ON CONFLICT(board_id) DO UPDATE SET owner=excluded.owner, epoch=orchestrator.epoch+1, lease_expiry=excluded.lease_expiry`,
		boardID, me, now+ttlSec)
	if err != nil {
		return 0, fmt.Errorf("mcs/board: takeover: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, errors.New("mcs/board: takeover rowcount!=1")
	}
	row := b.db.QueryRowContext(ctx, `SELECT epoch FROM orchestrator WHERE board_id=?`, boardID)
	if err := row.Scan(&epoch); err != nil {
		return 0, err
	}
	return epoch, nil
}

// Renew 活调度者续期:owner+epoch CAS 只延 expiry,不 +1(★N3;renew 不是所有权变更)。
func (b *Board) Renew(ctx context.Context, boardID string, id Identity, ttlSec int64) (bool, error) {
	res, err := b.db.ExecContext(ctx, `
UPDATE orchestrator SET lease_expiry=?+?
 WHERE board_id=? AND owner=? AND epoch=? AND lease_expiry>=?`,
		b.now(), ttlSec, boardID, id.Owner, id.Epoch, b.now())
	if err != nil {
		return false, fmt.Errorf("mcs/board: renew: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// guardSQL 是 3.3 通则的授权谓词(嵌入各原语 WHERE;授权行由【目标 node→task→board】推导,禁拼 :b)。
// 占位参数依序:owner, store_now, epoch。
const guardSQL = ` AND EXISTS (SELECT 1 FROM orchestrator o JOIN task t ON t.board_id=o.board_id
               WHERE t.task_id = node.task_id
                 AND o.owner = ? AND o.lease_expiry >= ? AND o.epoch = ?)`

// steerMutexSQL:steer 预留未决(pending/uncertain)拦 reassign/terminal(只拦 steer,interrupt 不拦=防死锁)。
const steerMutexSQL = ` AND NOT EXISTS (SELECT 1 FROM node_request r
               WHERE r.node_id = node.node_id AND r.op='steer' AND r.outcome IN ('pending','uncertain'))`

// priorOutcome 幂等查询:见过 request_id → 返旧 outcome(3.1①)。
func priorOutcome(ctx context.Context, tx *sql.Tx, rid string) (Outcome, bool, error) {
	var oc string
	err := tx.QueryRowContext(ctx, `SELECT outcome FROM node_request WHERE request_id=?`, rid).Scan(&oc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return Outcome(oc), true, nil
}

// insertEvent 每条事件强制记 actor(board_event.agent_id=执行本次 Board 写的操作者身份)——
// ★M5 治理扩展:调度者约束/可追责,"谁做的写"结构化落账,禁匿名事件。
func insertEvent(ctx context.Context, tx *sql.Tx, kind, nodeID string, gen, ts int64, actor string, data map[string]any) error {
	if actor == "" {
		return fmt.Errorf("mcs/board: event %s without actor (禁匿名事件)", kind)
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
		`INSERT INTO board_event(kind,node_id,agent_id,generation,ts,data) VALUES(?,?,?,?,?,?)`,
		kind, nodeID, actor, gen, ts, payload)
	return err
}

func insertRequest(ctx context.Context, tx *sql.Tx, rid, nodeID string, gen int64, op, expected string, oc Outcome, ts int64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO node_request(request_id,node_id,generation,op,expected_state,outcome,applied_ts) VALUES(?,?,?,?,?,?,?)`,
		rid, nodeID, gen, op, expected, string(oc), ts)
	return err
}

// blockerBefore 读 UPDATE 前的 blocker_reason(用于条件 clear 事件,R2:旧值匹配才发,无注记不发伪 clear)。
func blockerBefore(ctx context.Context, tx *sql.Tx, nodeID string) (string, error) {
	var br sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT blocker_reason FROM node WHERE node_id=?`, nodeID).Scan(&br); err != nil {
		return "", err
	}
	return br.String, nil
}

// ---- 3.1 send: ready → dispatched ----

// Send 幂等+CAS 派发记账(记账先行,外呼在调用方之后=crash cuts 维度1)。
func (b *Board) Send(ctx context.Context, nodeID string, gen int64, rid, agentID string, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	if oc, hit, err := priorOutcome(ctx, tx, rid); err != nil {
		return "", err
	} else if hit {
		return oc, nil // ①幂等:返旧 outcome,不重复施加
	}
	brBefore, err := blockerBefore(ctx, tx, nodeID)
	if err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, `
UPDATE node SET status='dispatched', assigned_agent=?, delivery_id=?, updated_ts=?,
       delivery_attempt = 0,
       blocker_reason = CASE WHEN blocker_reason='starved' THEN NULL ELSE blocker_reason END
 WHERE node_id=? AND generation=? AND status='ready'`+guardSQL,
		agentID, rid, now, nodeID, gen, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	oc := OutcomeApplied
	if n == 0 {
		oc = OutcomeCASMiss // 旧代/态不符/guard 失败 → reject(stale_node)
	}
	if err := insertRequest(ctx, tx, rid, nodeID, gen, "send", "ready", oc, now); err != nil {
		return "", err
	}
	if n == 1 {
		if err := insertEvent(ctx, tx, "node_dispatched", nodeID, gen, now, id.Owner, map[string]any{"agent": agentID, "rid": rid}); err != nil {
			return "", err
		}
		if brBefore == "starved" { // ★R2 条件 clear:旧值匹配恰一条
			if err := insertEvent(ctx, tx, "node_annotation_cleared", nodeID, gen, now, id.Owner, map[string]any{"reason": "starved", "trigger": "dispatch"}); err != nil {
				return "", err
			}
		}
	}
	return oc, tx.Commit()
}

// ---- 3.1b processing 回执: dispatched → running ----

// Processing 归属校验(当前 rid)+guard;迟到真实执行到达时同事务清 delivery_unresolved 注记(★MC1)。
func (b *Board) Processing(ctx context.Context, nodeID string, gen int64, rid string, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	brBefore, err := blockerBefore(ctx, tx, nodeID)
	if err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, `
UPDATE node SET status='running', started_ts=?, updated_ts=?,
       blocker_reason = CASE WHEN blocker_reason='delivery_unresolved' THEN NULL ELSE blocker_reason END
 WHERE node_id=? AND generation=? AND status='dispatched' AND delivery_id=?`+guardSQL,
		now, now, nodeID, gen, rid, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return OutcomeCASMiss, tx.Commit() // 旧 rid/旧代/态不符=拒,零副作用(C8 家族)
	}
	if err := insertEvent(ctx, tx, "node_started", nodeID, gen, now, id.Owner, nil); err != nil {
		return "", err
	}
	if brBefore == "delivery_unresolved" { // ★附带修:SQL 显式接线,恰一条
		if err := insertEvent(ctx, tx, "node_annotation_cleared", nodeID, gen, now, id.Owner, map[string]any{"reason": "delivery_unresolved", "trigger": "processing"}); err != nil {
			return "", err
		}
	}
	return OutcomeApplied, tx.Commit()
}

// ---- 3.3 mark_done / mark_failed ----

// Terminal 终结销账:归属(当前 rid)+代际+guard+steer 互斥;
// 接受态 = running 或 dispatched(★MC2-1/MC3-1 直达一般窗,无注记前置);
// done 时同一事务 fan-out:∀dep=done 的直接后继 pending→ready(promote)与 blocked(dep_failed)→ready(recover)。
func (b *Board) Terminal(ctx context.Context, nodeID string, gen int64, rid string, done bool, resultRef string, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	brBefore, err := blockerBefore(ctx, tx, nodeID)
	if err != nil {
		return "", err
	}
	st, evt := "failed", "node_failed"
	if done {
		st, evt = "done", "node_done"
	}
	res, err := tx.ExecContext(ctx, `
UPDATE node SET status=?, result_ref=?, ended_ts=?, updated_ts=?,
       started_ts = COALESCE(started_ts, ?),
       blocker_reason = CASE WHEN blocker_reason IN ('timeout','delivery_unresolved') THEN NULL ELSE blocker_reason END
 WHERE node_id=? AND generation=? AND delivery_id=?
   AND status IN ('running','dispatched')`+guardSQL+steerMutexSQL,
		st, resultRef, now, now, now, nodeID, gen, rid, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return OutcomeCASMiss, tx.Commit() // ★C8:旧投递/旧代/guard 任一不符=拒,零副作用
	}
	if err := insertEvent(ctx, tx, evt, nodeID, gen, now, id.Owner, map[string]any{"rid": rid}); err != nil {
		return "", err
	}
	if brBefore == "timeout" || brBefore == "delivery_unresolved" { // ★条件 clear 按 reason 恰一条
		if err := insertEvent(ctx, tx, "node_annotation_cleared", nodeID, gen, now, id.Owner, map[string]any{"reason": brBefore, "trigger": "terminal"}); err != nil {
			return "", err
		}
	}
	if done { // ★A案:提升/恢复与 terminal 同原子提交('已 done 而后继未提升'不可达)
		if err := fanOutLocked(ctx, tx, nodeID, now, id.Owner); err != nil {
			return "", err
		}
	}
	return OutcomeApplied, tx.Commit()
}

// fanOutLocked done 同事务内:对含此 dep 的同 task 兄弟节点,∀dep=done 则
// pending→ready(event node_ready) / blocked+dep_failed→ready(event node_recovered)。
func fanOutLocked(ctx context.Context, tx *sql.Tx, doneNode string, now int64, actor string) error {
	rows, err := tx.QueryContext(ctx, `
SELECT c.node_id, c.status, c.generation, c.deps, COALESCE(c.blocker_reason,'')
  FROM node c JOIN node d ON d.node_id=? AND c.task_id=d.task_id
 WHERE c.node_id<>? AND c.status IN ('pending','blocked') AND c.deps LIKE '%'||?||'%'`,
		doneNode, doneNode, doneNode)
	if err != nil {
		return err
	}
	type cand struct {
		id      string
		status  string
		gen     int64
		deps    string
		blocker string
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.status, &c.gen, &c.deps, &c.blocker); err != nil {
			rows.Close()
			return err
		}
		cands = append(cands, c)
	}
	rows.Close()
	for _, c := range cands {
		if c.status == "blocked" && c.blocker != "dep_failed" {
			continue // recover 只对 dep_failed 型(3.3 级联注)
		}
		var deps []string
		if err := json.Unmarshal([]byte(c.deps), &deps); err != nil {
			return fmt.Errorf("mcs/board: node %s deps json: %w", c.id, err)
		}
		allDone, isDep := true, false
		for _, dep := range deps {
			if dep == doneNode {
				isDep = true
				continue
			}
			var st string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM node WHERE node_id=?`, dep).Scan(&st); err != nil {
				return err
			}
			if st != "done" {
				allDone = false
				break
			}
		}
		if !isDep || !allDone { // LIKE 是粗滤;逐 dep 精核
			continue
		}
		evtKind := "node_ready"
		if c.status == "blocked" {
			evtKind = "node_recovered"
		}
		res, err := tx.ExecContext(ctx, `
UPDATE node SET status='ready', blocker_reason=NULL, updated_ts=? WHERE node_id=? AND status=?`,
			now, c.id, c.status)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 { // CAS 幂等防双提升(V30)
			if err := insertEvent(ctx, tx, evtKind, c.id, c.gen, now, actor, map[string]any{"promoted_by": doneNode}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- 3.2 reassign: gen+1 ----

// Reassign 换代(failover/人工解卡唯一入口):CAS 当前代;delivery_unresolved 期拒(★MC1);
// steer 预留未决拒;delivery_attempt 归零(★MC2-3);同事务清一切 blocker_reason+条件 clear 事件。
func (b *Board) Reassign(ctx context.Context, nodeID string, gen int64, newAgent string, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	brBefore, err := blockerBefore(ctx, tx, nodeID)
	if err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, `
UPDATE node SET generation = generation + 1,
       assigned_agent=NULLIF(?,''), status='ready', delivery_id=NULL, updated_ts=?,
       delivery_attempt = 0,
       blocker_reason = NULL
 WHERE node_id=? AND generation=?
   AND (blocker_reason IS NULL OR blocker_reason<>'delivery_unresolved')`+steerMutexSQL+guardSQL,
		newAgent, now, nodeID, gen, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return OutcomeCASMiss, tx.Commit() // 并发双 reassign 仅一成功(C4)/注记期拒/guard 拒
	}
	if err := insertEvent(ctx, tx, "node_reassigned", nodeID, gen+1, now, id.Owner, map[string]any{"old_gen": gen, "agent": newAgent}); err != nil {
		return "", err
	}
	if brBefore != "" { // ★R2:逐 reason 条件 clear 恰一条
		if err := insertEvent(ctx, tx, "node_annotation_cleared", nodeID, gen+1, now, id.Owner, map[string]any{"reason": brBefore, "trigger": "reassign"}); err != nil {
			return "", err
		}
	}
	return OutcomeApplied, tx.Commit()
}

// ---- 建图辅助(M2 测试与调度循环用;plan_nodes 全量原语属后续) ----

// RegisterAgent 登记 worker(node.assigned_agent 的 FK 目标;池起 worker 后调用)。
// ★P3 C4:身份闸——enforce 下非法名拒登记+identity_rejected 事件;warn 下放行+identity_warned。
func (b *Board) RegisterAgent(ctx context.Context, agentID, driverType string) error {
	if err := b.checkIdentity(ctx, agentID, "register_agent"); err != nil {
		return err
	}
	now := b.now()
	_, err := b.db.ExecContext(ctx, `
INSERT INTO agent(agent_id,driver_type,handle,lease_ts,created_ts,updated_ts)
VALUES(?,?,?,?,?,?) ON CONFLICT(agent_id) DO NOTHING`, agentID, driverType, agentID, now, now, now)
	return err
}

// CreateTask 建单(★M5v2 硬约束:调度者建单时【必须】填全四角色三元组 who/cli/llm,缺任一=整单拒;
// guarded:建单者身份核验;task_created 事件带 actor)。完整 task_request 幂等属 3.5b,此为最小承载。
func (b *Board) CreateTask(ctx context.Context, taskID, boardID, title string, ra RoleAssignments, id Identity) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := b.now()
	var ok int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM orchestrator WHERE board_id=? AND owner=? AND lease_expiry>=? AND epoch=?`,
		boardID, id.Owner, now, id.Epoch).Scan(&ok); err != nil {
		return err
	}
	if ok == 0 {
		return fmt.Errorf("mcs/board: create_task %s: 调度者身份核验失败(guard)", taskID)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO task(task_id,board_id,title,root_body,origin_addr,origin_envelope_id,created_ts,updated_ts)
VALUES(?,?,?,?,'','',?,?)`, taskID, boardID, title, title, now, now); err != nil {
		return err
	}
	if err := upsertRoles(ctx, tx, taskID, ra, now, true); err != nil { // requireAll:建单即填全
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO board_event(kind,task_id,agent_id,ts,data) VALUES('task_created',?,?,?,'{}')`,
		taskID, id.Owner, now); err != nil {
		return err
	}
	return tx.Commit()
}

// AddNode 建节点(born 状态由调用方按合并图快照定:pending/ready/blocked)。
func (b *Board) AddNode(ctx context.Context, nodeID, taskID string, seq int, status string, deps []string) error {
	now := b.now()
	dj, err := json.Marshal(deps)
	if err != nil {
		return err
	}
	_, err = b.db.ExecContext(ctx, `
INSERT INTO node(node_id,task_id,seq,title,body,deps,status,updated_ts)
VALUES(?,?,?,?,?,?,?,?)`, nodeID, taskID, seq, nodeID, "", string(dj), status, now)
	return err
}

// NodeState 读节点观测(测试/看板用;只读)。
type NodeState struct {
	Status          string
	Generation      int64
	DeliveryID      sql.NullString
	Attempt         int64
	DeliveryAttempt int64
	BlockerReason   sql.NullString
	AssignedAgent   sql.NullString
}

// GetNode 只读快照。
func (b *Board) GetNode(ctx context.Context, nodeID string) (NodeState, error) {
	var s NodeState
	err := b.db.QueryRowContext(ctx, `
SELECT status,generation,delivery_id,attempt,delivery_attempt,blocker_reason,assigned_agent
  FROM node WHERE node_id=?`, nodeID).
		Scan(&s.Status, &s.Generation, &s.DeliveryID, &s.Attempt, &s.DeliveryAttempt, &s.BlockerReason, &s.AssignedAgent)
	return s, err
}

// ActiveNodesOf 列出指派给某 agent 的在途节点(dispatched/running)——failover 重派的输入(J2)。
func (b *Board) ActiveNodesOf(ctx context.Context, agentID string) (map[string]int64, error) {
	rows, err := b.db.QueryContext(ctx, `
SELECT node_id, generation FROM node WHERE assigned_agent=? AND status IN ('dispatched','running')`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var gen int64
		if err := rows.Scan(&id, &gen); err != nil {
			return nil, err
		}
		out[id] = gen
	}
	return out, rows.Err()
}

// ActiveNode 在途节点观测行。
type ActiveNode struct {
	Node  string
	Gen   int64
	Agent string
}

// AllActive 全部在途节点(dispatched/running)及其指派 agent——孤儿接管扫描输入(守护崩溃恢复)。
func (b *Board) AllActive(ctx context.Context) ([]ActiveNode, error) {
	rows, err := b.db.QueryContext(ctx, `
SELECT node_id, generation, COALESCE(assigned_agent,'') FROM node WHERE status IN ('dispatched','running')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActiveNode
	for rows.Next() {
		var a ActiveNode
		if err := rows.Scan(&a.Node, &a.Gen, &a.Agent); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReadyNodes 列出 ready 节点(调度循环输入;按 seq 序)。
func (b *Board) ReadyNodes(ctx context.Context) ([]string, error) {
	rows, err := b.db.QueryContext(ctx, `SELECT node_id FROM node WHERE status='ready' ORDER BY task_id, seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Events 按 kind 过滤读事件数(测试断言"恰一条"用)。
func (b *Board) Events(ctx context.Context, nodeID, kind string) (int, error) {
	var n int
	err := b.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM board_event WHERE node_id=? AND kind=?`, nodeID, kind).Scan(&n)
	return n, err
}

// RoleBinding 角色三元组(★M5v2:who+所用 CLI+LLM,回溯优化的最小聚合键)。
type RoleBinding struct {
	Who string
	CLI string
	LLM string
}

func (r RoleBinding) complete() bool { return r.Who != "" && r.CLI != "" && r.LLM != "" }

// RoleAssignments 四角色全集(建单必填全,★张宗楚 07-23:任务单生成时调度者即填写完成)。
type RoleAssignments struct {
	Designer    RoleBinding
	Implementer RoleBinding
	Tester      RoleBinding
	QAOwner     RoleBinding
}

func (ra RoleAssignments) pairs() [][2]any {
	return [][2]any{
		{"designer", ra.Designer}, {"implementer", ra.Implementer},
		{"tester", ra.Tester}, {"qa_owner", ra.QAOwner},
	}
}

// ErrRolesIncomplete 建单时任一角色缺 who/cli/llm ⇒ 整单拒绝(结构化约束,不允许先建后补)。
var ErrRolesIncomplete = errors.New("mcs/board: roles incomplete — 建单必须填全四角色的 who/cli/llm")

func upsertRoles(ctx context.Context, tx *sql.Tx, taskID string, ra RoleAssignments, now int64, requireAll bool) error {
	for _, p := range ra.pairs() {
		role, rb := p[0].(string), p[1].(RoleBinding)
		if !rb.complete() {
			if requireAll {
				return ErrRolesIncomplete
			}
			if rb.Who == "" && rb.CLI == "" && rb.LLM == "" {
				continue // 变更场景:整组空=不动该角色
			}
			return ErrRolesIncomplete // 半填=拒(要么不动,要么三元组全给)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO task_role(task_id,role,who,cli,llm,updated_ts) VALUES(?,?,?,?,?,?)
ON CONFLICT(task_id,role) DO UPDATE SET who=excluded.who, cli=excluded.cli, llm=excluded.llm, updated_ts=excluded.updated_ts`,
			taskID, role, rb.Who, rb.CLI, rb.LLM, now); err != nil {
			return err
		}
	}
	return nil
}

// SetTaskRoles guarded 角色变更(建单后调整;每角色要么整组三元给全,要么不动)+事件。
func (b *Board) SetTaskRoles(ctx context.Context, taskID string, ra RoleAssignments, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	var ok int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM task t JOIN orchestrator o ON o.board_id=t.board_id
 WHERE t.task_id=? AND o.owner=? AND o.lease_expiry>=? AND o.epoch=?`,
		taskID, id.Owner, now, id.Epoch).Scan(&ok); err != nil {
		return "", err
	}
	if ok == 0 {
		return OutcomeCASMiss, tx.Commit()
	}
	if err := upsertRoles(ctx, tx, taskID, ra, now, false); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO board_event(kind,task_id,agent_id,ts,data) VALUES('task_roles_set',?,?,?,'{}')`,
		taskID, id.Owner, now); err != nil {
		return "", err
	}
	return OutcomeApplied, tx.Commit()
}

// LedgerEntry 任务台账(项目管理观测面;只读推导,零写)。
type LedgerEntry struct {
	TaskID     string
	Roles      map[string]RoleBinding // designer/implementer/tester/qa_owner → who+cli+llm
	CreatedTS  int64
	StartedTS  int64 // 首个 node started(0=未开始)
	EndedTS    int64 // 全部 node 终态才落(0=未收口)
	NodesTotal int
	NodesDone  int
	Dispatcher string // 当前 orchestrator owner(结构化操作问责主体)
}

// TaskLedger 只读台账:角色(含 CLI/LLM)/起止/进度/当前调度者。
func (b *Board) TaskLedger(ctx context.Context, taskID string) (LedgerEntry, error) {
	e := LedgerEntry{TaskID: taskID, Roles: map[string]RoleBinding{}}
	if err := b.db.QueryRowContext(ctx, `
SELECT t.created_ts, COALESCE(o.owner,'')
  FROM task t JOIN orchestrator o ON o.board_id=t.board_id WHERE t.task_id=?`, taskID).
		Scan(&e.CreatedTS, &e.Dispatcher); err != nil {
		return e, err
	}
	rows, err := b.db.QueryContext(ctx, `SELECT role,who,cli,llm FROM task_role WHERE task_id=?`, taskID)
	if err != nil {
		return e, err
	}
	for rows.Next() {
		var role string
		var rb RoleBinding
		if err := rows.Scan(&role, &rb.Who, &rb.CLI, &rb.LLM); err != nil {
			rows.Close()
			return e, err
		}
		e.Roles[role] = rb
	}
	rows.Close()
	if err := b.db.QueryRowContext(ctx, `
SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0),
       COALESCE(MIN(started_ts),0),
       CASE WHEN COUNT(*)>0 AND COUNT(*)=SUM(CASE WHEN status IN ('done','failed') THEN 1 ELSE 0 END)
            THEN COALESCE(MAX(ended_ts),0) ELSE 0 END
  FROM node WHERE task_id=?`, taskID).
		Scan(&e.NodesTotal, &e.NodesDone, &e.StartedTS, &e.EndedTS); err != nil {
		return e, err
	}
	return e, nil
}

// RoleStat 回溯优化聚合行:某(角色,cli,llm)组合参与的任务量与完成量。
type RoleStat struct {
	Role, CLI, LLM string
	Tasks, DoneTasks int
}

// RoleStats 回溯优化查询:按(role,cli,llm)聚合任务参与/完成——"哪种 CLI×LLM 干哪类角色最好"的数据面。
func (b *Board) RoleStats(ctx context.Context) ([]RoleStat, error) {
	rows, err := b.db.QueryContext(ctx, `
SELECT r.role, r.cli, r.llm, COUNT(*),
       SUM(CASE WHEN t.status='done' THEN 1 ELSE 0 END)
  FROM task_role r JOIN task t ON t.task_id=r.task_id
 GROUP BY r.role, r.cli, r.llm ORDER BY r.role, r.cli, r.llm`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoleStat
	for rows.Next() {
		var s RoleStat
		if err := rows.Scan(&s.Role, &s.CLI, &s.LLM, &s.Tasks, &s.DoneTasks); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetBlocker 注记原语(3.5 annotate 行最小版:guarded 置 blocker_reason,不迁 status)。
func (b *Board) SetBlocker(ctx context.Context, nodeID string, gen int64, reason string, id Identity) (Outcome, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := b.now()
	res, err := tx.ExecContext(ctx, `
UPDATE node SET blocker_reason=?, updated_ts=? WHERE node_id=? AND generation=?`+guardSQL,
		reason, now, nodeID, gen, id.Owner, now, id.Epoch)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeCASMiss, tx.Commit()
	}
	if err := insertEvent(ctx, tx, "node_annotated", nodeID, gen, now, id.Owner, map[string]any{"reason": reason}); err != nil {
		return "", err
	}
	return OutcomeApplied, tx.Commit()
}
