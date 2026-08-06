package datad

// MemStore 的 A-lite Assign 实现:mu.Lock 串行=原子事务(PG 侧靠事务+advisory 锁,见 assignment_pg.go)。

import (
	"context"
	"encoding/json"
	"time"
)

func (m *MemStore) GetFirstResult(_ context.Context, requestID string) (*FirstResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fr := m.firstResults[requestID]
	if fr == nil {
		return nil, nil
	}
	// R1⑥读边界深拷贝:返副本(含 Body 字节),防调用方改污染内存账(PG 侧 Scan 天然新副本)。
	cp := *fr
	if fr.Body != nil {
		cp.Body = append(json.RawMessage(nil), fr.Body...)
	}
	return &cp, nil
}

// recordFirst 写首果账(§2 步骤5:任何路径含失败先记首果再返)。
// R2-6 写边界深拷贝:传入 body 复制后入账——调用方(或其上游)事后改原 slice 不得污染权威账。
func (m *MemStore) recordFirst(req *AssignRequest, hash, taskID, outcome string, http int, body json.RawMessage) {
	var cp json.RawMessage
	if body != nil {
		cp = append(json.RawMessage(nil), body...)
	}
	m.firstResults[req.RequestID] = &FirstResult{
		RequestID: req.RequestID, Outcome: outcome, HTTPCode: http,
		RequestHash: hash, Body: cp, TaskID: taskID, CreatedAt: time.Now().UTC(),
	}
}

func (m *MemStore) activeGen(taskID, role string) (int64, bool) {
	var g int64
	found := false
	for _, a := range m.assignments {
		if a.TaskID == taskID && a.Role == role && a.Active {
			g, found = a.Generation, true
		}
	}
	return g, found
}

func (m *MemStore) maxGen(taskID string) int64 {
	var g int64
	for _, a := range m.assignments {
		if a.TaskID == taskID && a.Generation > g {
			g = a.Generation
		}
	}
	return g
}

// Assign 原子 AssignGeneration(§2)。tokenSessions=注册表(unknown_assignee 校验);
// actor=真实调用方(R2-4:audit 与业务同锁原子落账,不再由 handler 事务外补记)。
func (m *MemStore) Assign(_ context.Context, taskID string, req *AssignRequest, tokenSessions map[string]bool, actor string) (*AssignResult, *AssignErr) {
	// request_id 形门=步骤0(v1.8r2 N1):非法幂等键不查账不记账,422 直拒;
	// 通过后小写规范化(M1:幂等键/hash 用规范形,大小写两写=同一把键)。
	if e := validRequestID(req.RequestID); e != nil {
		return nil, e
	}
	req.RequestID = normalizeRequestID(req.RequestID)
	hash, herr := canonicalRequestHash(req)
	if herr != nil { // R2-1 fail-closed:canonical 错绝不吞错空 hash
		return nil, herr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 幂等(§3 N4):命中首果→比 request_hash;相同=原样返,不同=409 request_conflict
	if prev, ok := m.firstResults[req.RequestID]; ok {
		if prev.RequestHash != hash {
			e := aerr(ErrRequestConflict, "同 request_id 换参数(N4)")
			m.audit = append(m.audit, assignAuditRow(actor, taskID, req, nil, e.Code, e.Sub))
			return nil, e
		}
		if prev.Outcome == "success" {
			var r AssignResult
			_ = json.Unmarshal(prev.Body, &r)
			return &r, nil
		}
		return nil, replayErr(prev.Outcome, prev.HTTPCode, prev.Body) // R1②重放保真(错误 DTO)
	}

	// fail:首果+audit 同锁原子落账再返(§2 步骤5/6,R2-4)。
	fail := func(e *AssignErr) (*AssignResult, *AssignErr) {
		m.recordFirst(req, hash, taskID, e.Code, e.HTTP, failFirstBody(e)) // R1③失败首果存错误 DTO
		m.audit = append(m.audit, assignAuditRow(actor, taskID, req, nil, e.Code, e.Sub))
		return nil, e
	}

	if e := validAssignRequest(req); e != nil {
		return fail(e)
	}
	if !tokenSessions[req.Executor] || !tokenSessions[req.Reviewer] {
		return fail(aerr(ErrUnknownAssignee, "assignee 不在 token 注册表"))
	}
	t, ok := m.tasks[taskID]
	if !ok {
		return fail(aerr(ErrWrongStateA, "task 不存在"))
	}
	repl, e := assignStateGate(t.Status, req.Force, req.ExpectedGeneration != nil)
	if e != nil {
		return fail(e)
	}
	if !repl && req.ExpectedGeneration != nil {
		// v1.8r2 N2:首派带 expected=请求形非法(客户端首派必须省略;stale_generation 语义
		// 不拉伸——首派竞争败者[任务已 dispatched 后的 CAS 失败]走下方 412)。
		return fail(aerrSub(ErrInvalidRequest, SubExpectedGenOnFirst, "首派必须省略 expected_generation(无当代行)"))
	}
	// expected_generation CAS(§4):重派须匹配现当代代次
	if repl {
		cur, has := m.activeGen(taskID, RoleExecutor)
		if req.ExpectedGeneration == nil || !has || *req.ExpectedGeneration != cur {
			return fail(aerr(ErrStaleGen, "expected_generation 不符现当代"))
		}
	}
	// 原子:旧当代 active=false;新 generation=max+1 双角色;partial-unique 由此程序保证(唯一写路径)
	newGen := m.maxGen(taskID) + 1
	for i := range m.assignments {
		if m.assignments[i].TaskID == taskID && m.assignments[i].Active {
			m.assignments[i].Active = false
		}
	}
	now := time.Now().UTC()
	m.assignments = append(m.assignments,
		Assignment{TaskID: taskID, Role: RoleExecutor, Assignee: req.Executor, Generation: newGen, Active: true, RequestID: req.RequestID, CreatedAt: now},
		Assignment{TaskID: taskID, Role: RoleReviewer, Assignee: req.Reviewer, Generation: newGen, Active: true, RequestID: req.RequestID, CreatedAt: now})
	// 单权威(§2 步骤3/A-MC3):owner_session=executor;status→dispatched
	t.OwnerSession = req.Executor
	t.Status = "dispatched"
	t.UpdatedAt = now
	m.tasks[taskID] = t

	res := &AssignResult{TaskID: taskID, Generation: newGen, Executor: req.Executor, Reviewer: req.Reviewer, Status: "dispatched"}
	body, _ := json.Marshal(res)
	m.recordFirst(req, hash, taskID, "success", 200, body)
	// R2-4:audit 与 assignment/task/首果同锁边界=同原子(真 actor+完整 detail 含新代)。
	m.audit = append(m.audit, assignAuditRow(actor, taskID, req, &newGen, "success", ""))
	return res, nil
}
