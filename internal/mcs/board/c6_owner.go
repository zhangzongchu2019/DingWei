// c6_owner.go —— P3 C6 应急原语(★新原语,C1 1.4b;非引擎既有 guarded 原语)。
//
// owner_takeover(业主夺调度权:epoch e→e+1,owner=to_owner)/owner_override(业主单指令急停:不改 epoch)。
// 安全属性(契约 C0.2/C6/C0.5):
//   - 授权=owner_principal∈业主集(C0.1 鉴权,非自述);actor=业主 principal【不冒充/不注入调度者 Identity】(dev56③/A-4);
//   - epoch-CAS:expected_epoch==当前 epoch 才执行;陈旧≠当前=cas_miss【不自动重取/不跨代】(dev56④);
//   - 幂等单键(dev56 MC-b):idempotency_id 唯一键;重投【principal 匹配时】返原 outcome no-op;
//     跨 principal 复用同 id→拒(★shence E,不返他人 outcome);
//   - 过渡期硬禁用(C6.3):emergencyEnabled=false(默认)→两原语 fail-closed 不可启用(P5 上线原子共切开);
//   - 原子:状态变更+owner_* 事件同一事务(C0.5)。
package board

import (
	"context"
	"database/sql"
	"fmt"
)

// c6Config C6 应急原语配置。默认 emergencyEnabled=false(★C6.3 过渡期硬禁用,fail-safe);
// ownerPrincipals=业主集(C0.1 鉴权后的合法业主 principal)。P5 上线由部署侧 SetEmergencyEnabled(true)+SetOwnerPrincipals。
type c6Config struct {
	emergencyEnabled bool
	ownerPrincipals  map[string]bool
}

// SetEmergencyEnabled 启用/禁用 C6 应急原语(★C6.3:P5 上线硬门=principal 就绪∧清单清零∧本开关,原子共切)。
func (b *Board) SetEmergencyEnabled(on bool) {
	b.c6mu.Lock()
	defer b.c6mu.Unlock()
	b.c6cfg.emergencyEnabled = on
}

// SetOwnerPrincipals 配置业主集(C0.1;仅这些 principal 可发 C6 应急原语)。
func (b *Board) SetOwnerPrincipals(owners map[string]bool) {
	b.c6mu.Lock()
	defer b.c6mu.Unlock()
	cp := make(map[string]bool, len(owners))
	for k, v := range owners {
		cp[k] = v
	}
	b.c6cfg.ownerPrincipals = cp
}

func (b *Board) c6Ready(principal string) (enabled, isOwner bool) {
	b.c6mu.Lock()
	defer b.c6mu.Unlock()
	return b.c6cfg.emergencyEnabled, b.c6cfg.ownerPrincipals[principal]
}

// OwnerOutcome C6 结果(含过渡期禁用态)。
type OwnerOutcome string

const (
	OwnerApplied  OwnerOutcome = "applied"
	OwnerCASMiss  OwnerOutcome = "cas_miss"  // 陈旧 expected_epoch/expected_state
	OwnerRejected OwnerOutcome = "rejected"  // 授权/schema/白名单/跨principal劫持
	OwnerDisabled OwnerOutcome = "disabled"  // ★C6.3 过渡期硬禁用(不执行,fail-closed)
)

// OwnerTakeoverReq owner_takeover 请求 schema(★dev56④⑤:显式字段,含 to_owner)。
type OwnerTakeoverReq struct {
	OwnerPrincipal string
	ToOwner        string // 指定新调度者身份(合法 zzc-*)
	ExpectedEpoch  int64
	IdempotencyID  string
	Reason         string
}

// OwnerOverrideReq owner_override 请求 schema(★dev56④:显式字段+expected_state)。
type OwnerOverrideReq struct {
	OwnerPrincipal string
	Target         string // 目标 node_id
	Directive      string // ∈白名单(C0.6)
	ExpectedEpoch  int64
	ExpectedState  string // 目标态不符=拒(防陈旧急停作用于已变更节点)
	IdempotencyID  string
	Reason         string
}

// overrideDirectives C0.6 白名单:directive→施加的 node 状态(单条既有控制语义,非自由文本)。
var overrideDirectives = map[string]string{
	"pause":        "blocked", // 急停:节点转 blocked(awaiting)
	"steer-cancel": "blocked", // 纠偏取消:同上转 blocked 待重指
}

// checkOwnerIdem 幂等单键+★shence E principal 守卫。返回:(命中原结果?, 原 outcome, 原 result_json, err)。
// 跨 principal 复用同 id → 命中但 outcome=rejected(不返他人 outcome)。
func checkOwnerIdem(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, idemID, principal string) (hit bool, oc OwnerOutcome, rj string, err error) {
	var owner string
	var outcome, rjson string
	e := q.QueryRowContext(ctx, `SELECT owner_principal, outcome, COALESCE(result_json,'') FROM owner_op WHERE idempotency_id=?`, idemID).
		Scan(&owner, &outcome, &rjson)
	if e == sql.ErrNoRows {
		return false, "", "", nil
	}
	if e != nil {
		return false, "", "", e
	}
	if owner != principal { // ★shence E:跨 principal 复用同 idempotency_id → 拒,不返他人 outcome
		return true, OwnerRejected, "", nil
	}
	return true, OwnerOutcome(outcome), rjson, nil
}

// OwnerTakeover 业主夺调度权(★C6 6.1)。
func (b *Board) OwnerTakeover(ctx context.Context, boardID string, req OwnerTakeoverReq) (OwnerOutcome, error) {
	// schema 校验:显式字段+to_owner 合法 zzc-*(dev56④⑤)
	if req.OwnerPrincipal == "" || req.IdempotencyID == "" || req.ToOwner == "" || !IdentityPattern.MatchString(req.ToOwner) {
		return OwnerRejected, nil
	}
	enabled, isOwner := b.c6Ready(req.OwnerPrincipal)
	if !enabled { // ★C6.3 过渡期硬禁用
		return OwnerDisabled, nil
	}
	if !isOwner { // 授权:principal∈业主集(C0.1)
		return OwnerRejected, nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// 幂等(事务内,SQLite 单写者串行化):命中→返原 outcome(no-op)
	if hit, oc, _, e := checkOwnerIdem(ctx, tx, req.IdempotencyID, req.OwnerPrincipal); e != nil {
		return "", e
	} else if hit {
		return oc, nil
	}
	// epoch-CAS:读当前 epoch,expected 不符=cas_miss(★dev56④陈旧不跨代/不自动重取)
	var curEpoch int64
	var curOwner string
	if e := tx.QueryRowContext(ctx, `SELECT epoch, COALESCE(owner,'') FROM orchestrator WHERE board_id=?`, boardID).
		Scan(&curEpoch, &curOwner); e != nil {
		return "", e
	}
	if curEpoch != req.ExpectedEpoch {
		return b.recordOwnerOp(ctx, tx, req.IdempotencyID, req.OwnerPrincipal, "owner_takeover", OwnerCASMiss, "")
	}
	// 应用:epoch e→e+1,owner=to_owner(同事务)。★F-C6-1:检 RowsAffected==1 让 CAS 自足——
	// 条件 UPDATE WHERE epoch=curEpoch 若匹配 0 行(读检查后被并发改=TOCTOU),不假记 applied,退 cas_miss。
	res, e := tx.ExecContext(ctx, `UPDATE orchestrator SET epoch=epoch+1, owner=? WHERE board_id=? AND epoch=?`,
		req.ToOwner, boardID, curEpoch)
	if e != nil {
		return "", e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return b.recordOwnerOp(ctx, tx, req.IdempotencyID, req.OwnerPrincipal, "owner_takeover", OwnerCASMiss, "")
	}
	// 事件:actor=业主 principal(★不冒充调度者 Identity);记 from/to owner+epoch
	data := fmt.Sprintf(`json_object('from_owner',?,'to_owner',?,'from_epoch',?,'to_epoch',?,'reason',?)`)
	if _, e := tx.ExecContext(ctx, `INSERT INTO board_event(kind,agent_id,ts,data) VALUES('owner_takeover',?,?,`+data+`)`,
		req.OwnerPrincipal, b.now(), curOwner, req.ToOwner, curEpoch, curEpoch+1, req.Reason); e != nil {
		return "", e
	}
	rj := fmt.Sprintf(`{"to_epoch":%d,"to_owner":%q}`, curEpoch+1, req.ToOwner)
	return b.recordOwnerOp(ctx, tx, req.IdempotencyID, req.OwnerPrincipal, "owner_takeover", OwnerApplied, rj)
}

// OwnerOverride 业主单指令急停(★C6 6.2;不改 epoch/owner)。
func (b *Board) OwnerOverride(ctx context.Context, req OwnerOverrideReq) (OwnerOutcome, error) {
	if req.OwnerPrincipal == "" || req.IdempotencyID == "" || req.Target == "" {
		return OwnerRejected, nil
	}
	newState, ok := overrideDirectives[req.Directive] // ★C0.6 白名单:directive 不在白名单=拒
	if !ok {
		return OwnerRejected, nil
	}
	enabled, isOwner := b.c6Ready(req.OwnerPrincipal)
	if !enabled {
		return OwnerDisabled, nil
	}
	if !isOwner {
		return OwnerRejected, nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if hit, oc, _, e := checkOwnerIdem(ctx, tx, req.IdempotencyID, req.OwnerPrincipal); e != nil {
		return "", e
	} else if hit {
		return oc, nil
	}
	// guard:epoch-CAS + expected_state(目标 node 当前态==expected;不符=cas_miss,防陈旧急停作用于已变更节点)
	var curEpoch int64
	if e := tx.QueryRowContext(ctx, `SELECT epoch FROM orchestrator WHERE board_id=(SELECT board_id FROM task WHERE task_id=(SELECT task_id FROM node WHERE node_id=?))`, req.Target).
		Scan(&curEpoch); e != nil {
		if e == sql.ErrNoRows {
			return OwnerRejected, nil // 目标不存在
		}
		return "", e
	}
	var curState string
	if e := tx.QueryRowContext(ctx, `SELECT status FROM node WHERE node_id=?`, req.Target).Scan(&curState); e != nil {
		return "", e
	}
	if curEpoch != req.ExpectedEpoch || curState != req.ExpectedState {
		return b.recordOwnerOp(ctx, tx, req.IdempotencyID, req.OwnerPrincipal, "owner_override", OwnerCASMiss, "")
	}
	// 应用:白名单 directive→node 状态变更(同事务)。★F-C6-1:检 RowsAffected==1(TOCTOU 兜底,退 cas_miss)。
	res, e := tx.ExecContext(ctx, `UPDATE node SET status=? WHERE node_id=? AND status=?`, newState, req.Target, curState)
	if e != nil {
		return "", e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return b.recordOwnerOp(ctx, tx, req.IdempotencyID, req.OwnerPrincipal, "owner_override", OwnerCASMiss, "")
	}
	// 事件:actor=业主 principal(★不冒充调度者);acting_epoch=当前 epoch(不改)
	data := `json_object('target',?,'directive',?,'idempotency_id',?,'reason',?,'acting_epoch',?)`
	if _, e := tx.ExecContext(ctx, `INSERT INTO board_event(kind,node_id,agent_id,ts,data) VALUES('owner_override',?,?,?,`+data+`)`,
		req.Target, req.OwnerPrincipal, b.now(), req.Target, req.Directive, req.IdempotencyID, req.Reason, curEpoch); e != nil {
		return "", e
	}
	return b.recordOwnerOp(ctx, tx, req.IdempotencyID, req.OwnerPrincipal, "owner_override", OwnerApplied, "")
}

// recordOwnerOp 记幂等账并提交事务;返回该 outcome。
func (b *Board) recordOwnerOp(ctx context.Context, tx *sql.Tx, idemID, principal, op string, oc OwnerOutcome, resultJSON string) (OwnerOutcome, error) {
	var rj any
	if resultJSON != "" {
		rj = resultJSON
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO owner_op(idempotency_id,owner_principal,op,outcome,result_json,applied_ts) VALUES(?,?,?,?,?,?)`,
		idemID, principal, op, string(oc), rj, b.now()); e != nil {
		return "", e
	}
	if e := tx.Commit(); e != nil {
		return "", e
	}
	return oc, nil
}
