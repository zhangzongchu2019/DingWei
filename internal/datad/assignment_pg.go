package datad

// PGStore 的 A-lite Assign 实现:单事务 AssignGeneration(§2)——advisory 锁 per task 串行化
// (B 案单写,coordinator-only,低频);partial-unique(assignment_active_uniq)DB 强制硬门①(≤1),
// =1 由双角色齐校验(唯一写路径,N-A3 GRANT 收成)。首果账幂等(§3)。
// N-A3:本文件所有事务走 p.assignDB(assign 专用角色池)——常规应用池对 assignment/
// assign_first_result 无写权(deploy/datad-grants.sql),生产上直写被 PG permission denied。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

func (p *PGStore) GetFirstResult(ctx context.Context, requestID string) (*FirstResult, error) {
	fr := &FirstResult{}
	var body []byte
	row, qerr := p.assignDB.QueryRow(ctx, dtx(),
		`SELECT request_id,outcome,http_code,request_hash,body,task_id,created_at FROM assign_first_result WHERE request_id=$1`,
		requestID)
	if qerr != nil {
		return nil, qerr
	}
	err := row.Scan(&fr.RequestID, &fr.Outcome, &fr.HTTPCode, &fr.RequestHash, &body, &fr.TaskID, &fr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fr.Body = body
	return fr, nil
}

// auditInTx 事务内 audit INSERT(R2-4:与 assignment/首果同事务原子;真 actor)。
func auditInTx(ctx context.Context, tx repo.Tx, e AuditEntry) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit(actor,op,target,detail,at) VALUES($1,$2,$3,$4,$5)`,
		e.Actor, e.Op, e.Target, nullableRaw(e.Detail), e.At)
	return err
}

func nullableRaw(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return string(r)
}

func (p *PGStore) Assign(ctx context.Context, taskID string, req *AssignRequest, tokenSessions map[string]bool, actor string) (*AssignResult, *AssignErr) {
	// request_id 形门=步骤0(v1.8r2 N1):非法幂等键不开事务不记账,422 直拒;
	// 通过后小写规范化(M1:幂等键/hash 用规范形,大小写两写=同一把键)。
	if e := validRequestID(req.RequestID); e != nil {
		return nil, e
	}
	req.RequestID = normalizeRequestID(req.RequestID)
	hash, herr := canonicalRequestHash(req)
	if herr != nil { // R2-1 fail-closed:canonical 错绝不吞错空 hash
		return nil, herr
	}

	// 事务改回调式(repo.InTx):*sql.Tx 因此不出仓储包(§22.2c #1 后半句)。
	// 🔴 本函数有三处「写审计 + 提交 + 仍返回业务错误」——审计属 fail-closed,拒绝也必须留痕。
	//    这三处用 &repo.CommitErr{} 表达,由 InTx 提交后把内层错误交回,语义与改造前逐字对应;
	//    其余错误路径返回普通 error = 回滚,与原 defer Rollback 一致。
	var out *AssignResult
	terr := p.assignDB.InTx(ctx, dtx(), &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx repo.Tx) error {

		// 双 advisory 锁(cycle2 C1):task 锁(串行同 task 派发)+request_id 锁(串行同幂等键的
		// 首果判定——异 task 同 request_id 并发若只有 task 锁会双双 miss 首果查,PK 竞争败方
		// 首响 503 而非稳定 409)。**固定全局取锁序=锁键数值升序**(两键先 least 后 greatest,
		// 任意事务对同一对键的获取序一致⇒无死锁环;PutTask 只取单一 task 锁、无二次获取,
		// 不参与环)。键碰撞(least==greatest)=同键重入,同会话 advisory 锁可重入,无害。
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(least(hashtext('datad.assign:'||$1::text), hashtext('datad.assign.rid:'||$2::text)))`,
			taskID, req.RequestID); err != nil {
			return aerr(ErrStoreDownA, err.Error())
		}
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(greatest(hashtext('datad.assign:'||$1::text), hashtext('datad.assign.rid:'||$2::text)))`,
			taskID, req.RequestID); err != nil {
			return aerr(ErrStoreDownA, err.Error())
		}

		// 幂等(§3 N4):首果账命中→**先比 task_id(B3:幂等绑定完整操作身份,跨 task 同
		// request_id=稳定 request_conflict,防把 A 的首果错误重放给 B)**,再比 request_hash。
		var prevOutcome, prevHash, prevTask string
		var prevCode int
		var prevBody []byte
		perr := tx.QueryRow(ctx,
			`SELECT outcome,http_code,request_hash,body,task_id FROM assign_first_result WHERE request_id=$1`,
			req.RequestID).Scan(&prevOutcome, &prevCode, &prevHash, &prevBody, &prevTask)
		if perr == nil {
			if prevTask != taskID {
				e := aerr(ErrRequestConflict, "同 request_id 跨 task 复用(B3:幂等键绑定 task)")
				if aer := auditInTx(ctx, tx, assignAuditRow(actor, taskID, req, nil, e.Code, e.Sub)); aer != nil {
					return aerr(ErrStoreDownA, "audit 写失败(fail-closed): "+aer.Error())
				}
				return &repo.CommitErr{Err: e}
			}
			if prevHash != hash {
				e := aerr(ErrRequestConflict, "同 request_id 换参数(N4)")
				// request_conflict 不改首果账(首果属原参请求),audit 单独落(同事务提交)。
				if aer := auditInTx(ctx, tx, assignAuditRow(actor, taskID, req, nil, e.Code, e.Sub)); aer != nil {
					return aerr(ErrStoreDownA, "audit 写失败(fail-closed): "+aer.Error())
				}
				return &repo.CommitErr{Err: e}
			}
			if prevOutcome == "success" {
				var r AssignResult
				_ = json.Unmarshal(prevBody, &r)
				out = &r
				return nil
			}
			return replayErr(prevOutcome, prevCode, prevBody) // R1②重放保真(错误 DTO,同 Mem)
		} else if !errors.Is(perr, sql.ErrNoRows) {
			return aerr(ErrStoreDownA, perr.Error())
		}

		// recordFail:失败首果+audit 同事务持久,任一写/commit 失败=store_down fail-closed
		// (R2-3:先持久首果再返业务错;落账失败绝不向调用方返业务错)。
		recordFail := func(e *AssignErr) error {
			if _, ierr := tx.Exec(ctx,
				`INSERT INTO assign_first_result(request_id,outcome,http_code,request_hash,body,task_id)
       VALUES($1,$2,$3,$4,$5::jsonb,$6)`,
				req.RequestID, e.Code, e.HTTP, hash, string(failFirstBody(e)), taskID); ierr != nil {
				return aerr(ErrStoreDownA, "失败首果落账失败(fail-closed): "+ierr.Error())
			}
			if aer := auditInTx(ctx, tx, assignAuditRow(actor, taskID, req, nil, e.Code, e.Sub)); aer != nil {
				return aerr(ErrStoreDownA, "audit 写失败(fail-closed): "+aer.Error())
			}
			// 首果+audit 必须落库,业务错照常返回 -> CommitErr 精确表达这一点
			return &repo.CommitErr{Err: e}
		}

		if e := validAssignRequest(req); e != nil {
			return recordFail(e)
		}
		if !tokenSessions[req.Executor] || !tokenSessions[req.Reviewer] {
			return recordFail(aerr(ErrUnknownAssignee, "assignee 不在 token 注册表"))
		}
		// 现态
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM task WHERE id=$1`, taskID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return recordFail(aerr(ErrWrongStateA, "task 不存在"))
		} else if err != nil {
			return aerr(ErrStoreDownA, err.Error())
		}
		repl, e := assignStateGate(status, req.Force, req.ExpectedGeneration != nil)
		if e != nil {
			return recordFail(e)
		}
		if !repl && req.ExpectedGeneration != nil {
			// v1.8r2 N2:首派带 expected=请求形非法(客户端首派必须省略;stale_generation 不拉伸)。
			return recordFail(aerrSub(ErrInvalidRequest, SubExpectedGenOnFirst, "首派必须省略 expected_generation(无当代行)"))
		}
		// CAS(§4):重派须匹配现当代 executor generation
		var curGen sql.NullInt64
		if err := tx.QueryRow(ctx,
			`SELECT generation FROM assignment WHERE task_id=$1 AND role='executor' AND active`, taskID).Scan(&curGen); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return aerr(ErrStoreDownA, err.Error())
		}
		if repl {
			if req.ExpectedGeneration == nil || !curGen.Valid || *req.ExpectedGeneration != curGen.Int64 {
				return recordFail(aerr(ErrStaleGen, "expected_generation 不符现当代"))
			}
		}
		// 新 gen=max+1
		var maxGen sql.NullInt64
		if err := tx.QueryRow(ctx, `SELECT max(generation) FROM assignment WHERE task_id=$1`, taskID).Scan(&maxGen); err != nil {
			return aerr(ErrStoreDownA, err.Error())
		}
		newGen := maxGen.Int64 + 1
		// 旧当代 active=false(同事务);新 gen 双角色 INSERT(partial-unique DB 强制硬门①)
		if _, err := tx.Exec(ctx, `UPDATE assignment SET active=false WHERE task_id=$1 AND active`, taskID); err != nil {
			return aerr(ErrStoreDownA, err.Error())
		}
		for _, ra := range []struct{ role, who string }{{RoleExecutor, req.Executor}, {RoleReviewer, req.Reviewer}} {
			if _, err := tx.Exec(ctx,
				`INSERT INTO assignment(task_id,role,assignee,generation,active,request_id) VALUES($1,$2,$3,$4,true,$5)`,
				taskID, ra.role, ra.who, newGen, req.RequestID); err != nil {
				return aerr(ErrStoreDownA, "assignment INSERT(partial-unique 冲突?): "+err.Error())
			}
		}
		// 单权威(§2 步骤3):owner_session=executor;status→dispatched
		if _, err := tx.Exec(ctx,
			`UPDATE task SET owner_session=$2, status='dispatched', updated_at=now() WHERE id=$1`, taskID, req.Executor); err != nil {
			return aerr(ErrStoreDownA, err.Error())
		}
		res := &AssignResult{TaskID: taskID, Generation: newGen, Executor: req.Executor, Reviewer: req.Reviewer, Status: "dispatched"}
		body, _ := json.Marshal(res)
		if _, err := tx.Exec(ctx,
			`INSERT INTO assign_first_result(request_id,outcome,http_code,request_hash,body,task_id) VALUES($1,'success',200,$2,$3::jsonb,$4)`,
			req.RequestID, hash, string(body), taskID); err != nil {
			return aerr(ErrStoreDownA, err.Error())
		}
		// R2-4:audit 与 assignment/task/首果同事务原子(真 actor+完整 detail 含新代)。
		if aer := auditInTx(ctx, tx, assignAuditRow(actor, taskID, req, &newGen, "success", "")); aer != nil {
			return aerr(ErrStoreDownA, "audit 写失败(fail-closed): "+aer.Error())
		}
		out = res
		return nil
	})
	if terr != nil {
		// 提交本身失败与业务错误分开处置:前者一律 fail-closed 归 store_down。
		var cf *repo.CommitFailed
		if errors.As(terr, &cf) {
			return nil, aerr(ErrStoreDownA, "commit 失败(fail-closed): "+cf.Err.Error())
		}
		// InTx 可能返回 BeginTx 失败这类非 *AssignErr 的裸错误,统一归 store_down;
		// 业务错误(*AssignErr)原样交回,不重新包装。
		var ae *AssignErr
		if errors.As(terr, &ae) {
			return nil, ae
		}
		return nil, aerr(ErrStoreDownA, terr.Error())
	}
	return out, nil
}

// GCFirstResults 首果账 30d GC(契约 §1 S5):仅删 created_at 早于保留窗**且**对应 task 已
// closed(done/archived)的行;未 closed task 首果永不删(重放安全)。走 p.gcDB(独立
// datad_assign_gc 角色:仅 assign_first_result DELETE/SELECT+task SELECT,对 assignment
// 零写权——N-A3 GC 门)。单独事务,不碰 assign 路径。返回删除行数。
func (p *PGStore) GCFirstResults(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := p.gcDB.Exec(ctx, dtx(),
		`DELETE FROM assign_first_result
      WHERE created_at < now() - make_interval(secs => $1)
        AND task_id IN (SELECT id FROM task WHERE status IN ('done','archived'))`,
		retention.Seconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
