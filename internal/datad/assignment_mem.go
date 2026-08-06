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
	return m.firstResults[requestID], nil
}

// recordFirst 写首果账(§2 步骤5:任何路径含失败先记首果再返)。
func (m *MemStore) recordFirst(req *AssignRequest, taskID, outcome string, http int, body json.RawMessage) {
	m.firstResults[req.RequestID] = &FirstResult{
		RequestID: req.RequestID, Outcome: outcome, HTTPCode: http,
		RequestHash: canonicalRequestHash(req), Body: body, TaskID: taskID, CreatedAt: time.Now().UTC(),
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

// Assign 原子 AssignGeneration(§2)。tokenSessions=注册表(unknown_assignee 校验)。
func (m *MemStore) Assign(_ context.Context, taskID string, req *AssignRequest, tokenSessions map[string]bool) (*AssignResult, *AssignErr) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 幂等(§3 N4):命中首果→比 request_hash;相同=原样返,不同=409 request_conflict
	if prev, ok := m.firstResults[req.RequestID]; ok {
		if prev.RequestHash != canonicalRequestHash(req) {
			return nil, aerr(ErrRequestConflict, "同 request_id 换参数(N4)")
		}
		if prev.Outcome == "success" {
			var r AssignResult
			_ = json.Unmarshal(prev.Body, &r)
			return &r, nil
		}
		return nil, &AssignErr{Code: prev.Outcome, HTTP: prev.HTTPCode, Msg: "重放首果"}
	}

	fail := func(e *AssignErr) (*AssignResult, *AssignErr) {
		m.recordFirst(req, taskID, e.Code, e.HTTP, nil) // 失败也记首果
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
	// audit
	m.audit = append(m.audit, AuditEntry{Actor: "assign", Op: "assign", Target: taskID, At: now})

	res := &AssignResult{TaskID: taskID, Generation: newGen, Executor: req.Executor, Reviewer: req.Reviewer, Status: "dispatched"}
	body, _ := json.Marshal(res)
	m.recordFirst(req, taskID, "success", 200, body)
	return res, nil
}
