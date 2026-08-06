package datad

// PGStore 的 A-lite Assign 实现:单事务 AssignGeneration(§2)——advisory 锁 per task 串行化
// (B 案单写,coordinator-only,低频);partial-unique(assignment_active_uniq)DB 强制硬门①(≤1),
// =1 由双角色齐校验(唯一写路径,N-A3 GRANT 收成)。首果账幂等(§3)。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func (p *PGStore) GetFirstResult(ctx context.Context, requestID string) (*FirstResult, error) {
	fr := &FirstResult{}
	var body []byte
	err := p.db.QueryRowContext(ctx,
		`SELECT request_id,outcome,http_code,request_hash,body,task_id,created_at FROM assign_first_result WHERE request_id=$1`,
		requestID).Scan(&fr.RequestID, &fr.Outcome, &fr.HTTPCode, &fr.RequestHash, &body, &fr.TaskID, &fr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fr.Body = body
	return fr, nil
}

func (p *PGStore) Assign(ctx context.Context, taskID string, req *AssignRequest, tokenSessions map[string]bool) (*AssignResult, *AssignErr) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	defer func() { _ = tx.Rollback() }()

	// advisory 锁 per task:串行化同 task 的 assign(并发下 partial-unique 兜底,advisory 串行为主)
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext('datad.assign:'||$1::text))`, taskID); err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}

	// 幂等(§3 N4):首果账命中→比 request_hash
	hash := canonicalRequestHash(req)
	var prevOutcome, prevHash string
	var prevCode int
	var prevBody []byte
	perr := tx.QueryRowContext(ctx,
		`SELECT outcome,http_code,request_hash,body FROM assign_first_result WHERE request_id=$1`,
		req.RequestID).Scan(&prevOutcome, &prevCode, &prevHash, &prevBody)
	if perr == nil {
		if prevHash != hash {
			return nil, aerr(ErrRequestConflict, "同 request_id 换参数(N4)")
		}
		if prevOutcome == "success" {
			var r AssignResult
			_ = json.Unmarshal(prevBody, &r)
			return &r, nil
		}
		return nil, replayErr(prevOutcome, prevCode, prevBody) // R1②重放保真(错误 DTO,同 Mem)
	} else if !errors.Is(perr, sql.ErrNoRows) {
		return nil, aerr(ErrStoreDownA, perr.Error())
	}

	recordFail := func(e *AssignErr) *AssignErr {
		_, _ = tx.ExecContext(ctx,
			`INSERT INTO assign_first_result(request_id,outcome,http_code,request_hash,body,task_id)
       VALUES($1,$2,$3,$4,$5::jsonb,$6) ON CONFLICT DO NOTHING`,
			req.RequestID, e.Code, e.HTTP, hash, string(failFirstBody(e)), taskID) // R1③失败首果存错误 DTO(非 null)
		_ = tx.Commit() // 首果落账(含失败)
		return e
	}

	if e := validAssignRequest(req); e != nil {
		return nil, recordFail(e)
	}
	if !tokenSessions[req.Executor] || !tokenSessions[req.Reviewer] {
		return nil, recordFail(aerr(ErrUnknownAssignee, "assignee 不在 token 注册表"))
	}
	// 现态
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM task WHERE id=$1`, taskID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return nil, recordFail(aerr(ErrWrongStateA, "task 不存在"))
	} else if err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	repl, e := assignStateGate(status, req.Force, req.ExpectedGeneration != nil)
	if e != nil {
		return nil, recordFail(e)
	}
	// CAS(§4):重派须匹配现当代 executor generation
	var curGen sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT generation FROM assignment WHERE task_id=$1 AND role='executor' AND active`, taskID).Scan(&curGen); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	if repl {
		if req.ExpectedGeneration == nil || !curGen.Valid || *req.ExpectedGeneration != curGen.Int64 {
			return nil, recordFail(aerr(ErrStaleGen, "expected_generation 不符现当代"))
		}
	}
	// 新 gen=max+1
	var maxGen sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(generation) FROM assignment WHERE task_id=$1`, taskID).Scan(&maxGen); err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	newGen := maxGen.Int64 + 1
	// 旧当代 active=false(同事务);新 gen 双角色 INSERT(partial-unique DB 强制硬门①)
	if _, err := tx.ExecContext(ctx, `UPDATE assignment SET active=false WHERE task_id=$1 AND active`, taskID); err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	for _, ra := range []struct{ role, who string }{{RoleExecutor, req.Executor}, {RoleReviewer, req.Reviewer}} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO assignment(task_id,role,assignee,generation,active,request_id) VALUES($1,$2,$3,$4,true,$5)`,
			taskID, ra.role, ra.who, newGen, req.RequestID); err != nil {
			return nil, aerr(ErrStoreDownA, "assignment INSERT(partial-unique 冲突?): "+err.Error())
		}
	}
	// 单权威(§2 步骤3):owner_session=executor;status→dispatched
	if _, err := tx.ExecContext(ctx,
		`UPDATE task SET owner_session=$2, status='dispatched', updated_at=now() WHERE id=$1`, taskID, req.Executor); err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	res := &AssignResult{TaskID: taskID, Generation: newGen, Executor: req.Executor, Reviewer: req.Reviewer, Status: "dispatched"}
	body, _ := json.Marshal(res)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO assign_first_result(request_id,outcome,http_code,request_hash,body,task_id) VALUES($1,'success',200,$2,$3::jsonb,$4)`,
		req.RequestID, hash, string(body), taskID); err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	// R1④:审计由 handler(server.handleAssign)以真调用方 id.Session+detail 落,不在事务内写
	// actor='assign' 字面(旧内部 audit 已删,避免假身份;审计非事务原子必需,写失败仅告警)。
	if err := tx.Commit(); err != nil {
		return nil, aerr(ErrStoreDownA, err.Error())
	}
	return res, nil
}
