package datad

// A-lite 验收(契约 §6 A1-A12+delta v1.8;Mem 载体)。PG 同套语义见 assignment_pg_integration_test.go
// (DATAD_TEST_PG_DSN 门控,真并发/ACL/GC 归 PG 侧,两层互不替代——dev56 R2-5)。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// uid 生成 UUID 形 request_id(delta v1.8③:冻结格式=RFC 4122 形;测试确定性编号)。
func uid(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

const (
	sExec  = "zzc-devo48-2642"
	sRev   = "zzc-claudek3-2642"
	sThird = "zzc-shencedeepseek-2642"
	sCoord = "zzc-coordinator-2642"
)

func aliteWorld(t *testing.T) (*MemStore, map[string]bool) {
	t.Helper()
	m := NewMemStore()
	_, err := m.PutTask(context.Background(),
		Task{ID: "T1", Title: "t", OwnerSession: sCoord, Status: "proposed"}, timeZero(), true)
	if err != nil {
		t.Fatal(err)
	}
	tok := map[string]bool{sExec: true, sRev: true, sThird: true}
	return m, tok
}

func req(exec, rev, rid string) *AssignRequest {
	return &AssignRequest{Executor: exec, Reviewer: rev, RequestID: rid}
}

// mustHash canonicalRequestHash 测试便捷(合法输入不应报错)。
func mustHash(t *testing.T, r *AssignRequest) string {
	t.Helper()
	h, e := canonicalRequestHash(r)
	if e != nil {
		t.Fatalf("canonicalRequestHash: %v", e)
	}
	return h
}

// A1 原子双角色(半件拒)+成功首派→dispatched+owner_session=executor(A7 前提)。
func TestA1AtomicDualRole(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	if _, e := m.Assign(ctx, "T1", &AssignRequest{Executor: sExec, RequestID: uid(900)}, tok, sCoord); e == nil || e.Code != ErrHalfAssignment {
		t.Fatalf("半件应 half_assignment: %v", e)
	}
	r, e := m.Assign(ctx, "T1", req(sExec, sRev, uid(1)), tok, sCoord)
	if e != nil || r.Status != "dispatched" || r.Generation != 1 {
		t.Fatalf("首派: %v %+v", e, r)
	}
	tk, _ := m.GetTask(ctx, "T1")
	if tk.OwnerSession != sExec || tk.Status != "dispatched" {
		t.Fatalf("owner_session=executor/status 未落(A7): %+v", tk)
	}
}

// A2 并发唯一(R2-5 修:①不改名降级——goroutine 真并发调用,Mem 层并发语义;②loser 断言完整:
// 恰一赢家 gen1;每个 loser 断言冻结的精确错误码 stale_generation+HTTP 412+首果账 body=统一
// ErrorDTO 字节;③终态断言完整:active 恰 2 行、同 generation=1、角色一 executor 一 reviewer、
// assignee 正确、owner_session=executor、status=dispatched。DB 真并发(advisory+partial-unique)
// =PG 侧 TestPGA2ConcurrentUnique 另证,两层不互代)。
func TestA2ConcurrentUnique(t *testing.T) {
	m, tok := aliteWorld(t)
	const K = 8
	res := make([]*AssignResult, K)
	errs := make([]*AssignErr, K)
	var wg sync.WaitGroup
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res[i], errs[i] = m.Assign(context.Background(), "T1", req(sExec, sRev, uid(i)), tok, sCoord)
		}(i)
	}
	wg.Wait()
	winners, losers := 0, 0
	for i := 0; i < K; i++ {
		rid := uid(i)
		if errs[i] == nil {
			winners++
			if res[i] == nil || res[i].Generation != 1 {
				t.Fatalf("赢家 %s 应 gen1 成功: %+v", rid, res[i])
			}
			continue
		}
		losers++
		e := errs[i]
		if res[i] != nil {
			t.Fatalf("loser %s 有错误却也返 result: %+v", rid, res[i])
		}
		// 精确冻结判据:首派竞争败者=stale_generation/412(状态已 dispatched=重派面,无 expected)。
		if e.Code != ErrStaleGen || e.HTTP != 412 {
			t.Fatalf("loser %s 应 stale_generation/412,got %s/%d", rid, e.Code, e.HTTP)
		}
		fr, _ := m.GetFirstResult(context.Background(), rid)
		if fr == nil || fr.Outcome != ErrStaleGen || fr.HTTPCode != 412 {
			t.Fatalf("loser %s 首果未落账或码不符: %+v", rid, fr)
		}
		if string(fr.Body) != string(failFirstBody(e)) {
			t.Fatalf("loser %s 首果 body≠统一 ErrorDTO: %s vs %s", rid, fr.Body, failFirstBody(e))
		}
	}
	if winners != 1 || losers != K-1 {
		t.Fatalf("应恰 1 赢家+%d loser,got %d/%d", K-1, winners, losers)
	}
	// 终态:active 恰 2、同 gen=1、角色各一、assignee 正确。
	var ex, rv int
	for _, a := range m.assignments {
		if a.TaskID != "T1" || !a.Active {
			continue
		}
		if a.Generation != 1 {
			t.Fatalf("active 行 gen 应 1: %+v", a)
		}
		switch a.Role {
		case RoleExecutor:
			ex++
			if a.Assignee != sExec {
				t.Fatalf("executor assignee: %+v", a)
			}
		case RoleReviewer:
			rv++
			if a.Assignee != sRev {
				t.Fatalf("reviewer assignee: %+v", a)
			}
		}
	}
	if ex != 1 || rv != 1 {
		t.Fatalf("active 应 executor/reviewer 各恰 1,got %d/%d", ex, rv)
	}
	tk, _ := m.GetTask(context.Background(), "T1")
	if tk.OwnerSession != sExec || tk.Status != "dispatched" {
		t.Fatalf("owner/status 终态: %+v", tk)
	}
}

// A3 重派 generation+1 旧代保留。
func TestA3GenerationMonotonic(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	m.Assign(ctx, "T1", req(sExec, sRev, uid(1)), tok, sCoord)
	g1 := int64(1)
	r2, e := m.Assign(ctx, "T1", &AssignRequest{Executor: sThird, Reviewer: sRev, RequestID: uid(2), ExpectedGeneration: &g1}, tok, sCoord)
	if e != nil || r2.Generation != 2 {
		t.Fatalf("重派 gen=2: %v %+v", e, r2)
	}
	total := 0
	for _, a := range m.assignments {
		if a.TaskID == "T1" {
			total++
		}
	}
	if total != 4 { // gen1 双角色(旧,保留)+gen2 双角色
		t.Fatalf("历史代保留:应 4 行,got %d", total)
	}
}

// A4/A9/A12 request_id 重放三型+request_hash 对拍。
func TestA4A9A12Idempotent(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	r1req := req(sExec, sRev, uid(1))
	r1, _ := m.Assign(ctx, "T1", r1req, tok, sCoord)
	// A4/A12 成功重放:原样返 body
	r1b, e := m.Assign(ctx, "T1", req(sExec, sRev, uid(1)), tok, sCoord)
	if e != nil || r1b.Generation != r1.Generation {
		t.Fatalf("成功重放原样返: %v %+v", e, r1b)
	}
	// A12 request_hash=请求体 canonical hash(对拍请求体,非响应体)
	fr, _ := m.GetFirstResult(ctx, uid(1))
	if fr == nil || fr.RequestHash != mustHash(t, r1req) {
		t.Fatalf("A12 request_hash 应=请求体 canonical hash: %+v", fr)
	}
	var got AssignResult
	if err := json.Unmarshal(fr.Body, &got); err != nil || got.Generation != r1.Generation || got.Executor != sExec {
		t.Fatalf("A12 成功首果 body 原样: %s", fr.Body)
	}
	// A9 同 request_id 换参→409 request_conflict
	if _, e := m.Assign(ctx, "T1", req(sThird, sRev, uid(1)), tok, sCoord); e == nil || e.Code != ErrRequestConflict || e.HTTP != 409 {
		t.Fatalf("换参应 request_conflict/409: %v", e)
	}
}

// A4 第三型(条件已变仍原样重放,不重算)。
func TestA4ThirdType(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	if _, e := m.Assign(ctx, "T1", req(sExec, sExec, uid(10)), tok, sCoord); e == nil || e.Code != ErrSelfReview {
		t.Fatalf("首次应 self_review: %v", e)
	}
	m.Assign(ctx, "T1", req(sExec, sRev, uid(11)), tok, sCoord) // 世界变
	if _, e := m.Assign(ctx, "T1", req(sExec, sExec, uid(10)), tok, sCoord); e == nil || e.Code != ErrSelfReview {
		t.Fatalf("条件已变仍原样重放错误首果(非重算): %v", e)
	}
}

// A5 expected_generation 412+首派带 expected 亦 412(N1/R2-2)。
func TestA5CAS(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	// 首派带 expected_generation=invalid_request(v1.8r2 N2:客户端首派必须省略;
	// stale_generation 语义不拉伸,首派竞争败者另证[A2 losers=412])
	g0 := int64(0)
	if _, e := m.Assign(ctx, "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(20), ExpectedGeneration: &g0}, tok, sCoord); e == nil || e.Code != ErrInvalidRequest || e.Sub != SubExpectedGenOnFirst {
		t.Fatalf("首派带 expected_generation 应 invalid_request/expected_generation_on_first: %v", e)
	}
	m.Assign(ctx, "T1", req(sExec, sRev, uid(21)), tok, sCoord)
	bad := int64(99)
	if _, e := m.Assign(ctx, "T1", &AssignRequest{Executor: sThird, Reviewer: sRev, RequestID: uid(22), ExpectedGeneration: &bad}, tok, sCoord); e == nil || e.Code != ErrStaleGen || e.HTTP != 412 {
		t.Fatalf("CAS 不符应 stale_generation/412: %v", e)
	}
}

// A6 force 越 wrong_state 且 audit 记因(reason/force 完整入 detail,R2-4)。
func TestA6ForceOverWrongStateAudited(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	tk, _ := m.GetTask(ctx, "T1")
	tk.Status = "blocked"
	m.tasks["T1"] = *tk
	// 无 force=wrong_state
	if _, e := m.Assign(ctx, "T1", req(sExec, sRev, uid(30)), tok, sCoord); e == nil || e.Code != ErrWrongStateA {
		t.Fatalf("blocked 无 force 应 wrong_state: %v", e)
	}
	// force+reason=越闸成功
	r, e := m.Assign(ctx, "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(31), Force: true, Reason: "紧急改派"}, tok, sCoord)
	if e != nil || r.Generation != 1 {
		t.Fatalf("force 越 wrong_state: %v %+v", e, r)
	}
	// audit 记因:成功行 detail 含 force=true+reason+真 actor
	entries, _ := m.ListAudit(ctx, time.Time{}, 100)
	var hit *AuditEntry
	for i := range entries {
		if entries[i].Op == "assign" && strings.Contains(string(entries[i].Detail), uid(31)) {
			hit = &entries[i]
		}
	}
	if hit == nil {
		t.Fatal("force 成功应有 audit 行")
	}
	var d assignAuditDetail
	if err := json.Unmarshal(hit.Detail, &d); err != nil || !d.Force || d.Reason != "紧急改派" || d.Outcome != "success" || d.Generation == nil || *d.Generation != 1 {
		t.Fatalf("A6 audit detail 须完整绑定 force/reason/gen: %s", hit.Detail)
	}
	if hit.Actor != sCoord {
		t.Fatalf("audit 须真 actor(%s),got %s", sCoord, hit.Actor)
	}
}

// R2-4 audit 与业务同原子边界:成功/业务失败各有 audit(真 actor+完整 detail);重放不追加。
func TestAuditInStoreAtomic(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	// 业务失败:audit op=assign.<code>
	m.Assign(ctx, "T1", req(sExec, sExec, uid(40)), tok, sCoord)
	// 成功
	m.Assign(ctx, "T1", req(sExec, sRev, uid(41)), tok, sCoord)
	// 重放两型(不追加 audit)
	m.Assign(ctx, "T1", req(sExec, sExec, uid(40)), tok, sCoord)
	m.Assign(ctx, "T1", req(sExec, sRev, uid(41)), tok, sCoord)
	entries, _ := m.ListAudit(ctx, time.Time{}, 100)
	var failN, okN int
	for _, e := range entries {
		switch {
		case e.Op == "assign."+ErrSelfReview:
			failN++
			var d assignAuditDetail
			if err := json.Unmarshal(e.Detail, &d); err != nil || d.RequestID != uid(40) || d.Outcome != ErrSelfReview || d.Executor != sExec {
				t.Fatalf("失败 audit detail: %s", e.Detail)
			}
		case e.Op == "assign":
			okN++
		}
	}
	if failN != 1 || okN != 1 {
		t.Fatalf("audit 应失败 1+成功 1(重放不追加),got fail=%d ok=%d", failN, okN)
	}
}

// A8+R2-2 全错误码双射表驱动:每码逐一在真实调用路径触发,断言机器码+HTTP+首果账三者一致。
func TestA8ErrorBijectionRealPath(t *testing.T) {
	type tc struct {
		name     string
		code     string
		http     int
		firstRes bool // 该错误应入首果账
		run      func(m *MemStore, tok map[string]bool) *AssignErr
	}
	g1 := int64(1)
	neg := int64(-1)
	cases := []tc{
		{"half_assignment", ErrHalfAssignment, 422, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			_, e := m.Assign(context.Background(), "T1", &AssignRequest{Executor: sExec, RequestID: uid(50)}, tok, sCoord)
			return e
		}},
		{"self_review", ErrSelfReview, 422, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			_, e := m.Assign(context.Background(), "T1", req(sExec, sExec, uid(51)), tok, sCoord)
			return e
		}},
		{"unknown_assignee", ErrUnknownAssignee, 422, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			_, e := m.Assign(context.Background(), "T1", req("nobody", sRev, uid(52)), tok, sCoord)
			return e
		}},
		{"wrong_state", ErrWrongStateA, 409, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			tk, _ := m.GetTask(context.Background(), "T1")
			tk.Status = "done"
			m.tasks["T1"] = *tk
			_, e := m.Assign(context.Background(), "T1", req(sExec, sRev, uid(53)), tok, sCoord)
			return e
		}},
		{"stale_generation", ErrStaleGen, 412, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			m.Assign(context.Background(), "T1", req(sExec, sRev, uid(54)), tok, sCoord)
			bad := int64(9)
			_, e := m.Assign(context.Background(), "T1", &AssignRequest{Executor: sThird, Reviewer: sRev, RequestID: uid(55), ExpectedGeneration: &bad}, tok, sCoord)
			return e
		}},
		{"requeue_state", ErrRequeueState, 409, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			m.Assign(context.Background(), "T1", req(sExec, sRev, uid(56)), tok, sCoord)
			tk, _ := m.GetTask(context.Background(), "T1")
			tk.Status = "in_progress"
			m.tasks["T1"] = *tk
			_, e := m.Assign(context.Background(), "T1", &AssignRequest{Executor: sThird, Reviewer: sRev, RequestID: uid(57), ExpectedGeneration: &g1}, tok, sCoord)
			return e
		}},
		{"request_conflict", ErrRequestConflict, 409, false, func(m *MemStore, tok map[string]bool) *AssignErr {
			m.Assign(context.Background(), "T1", req(sExec, sRev, uid(58)), tok, sCoord)
			_, e := m.Assign(context.Background(), "T1", req(sThird, sRev, uid(58)), tok, sCoord)
			return e
		}},
		{"invalid_request(force空reason)", ErrInvalidRequest, 422, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			_, e := m.Assign(context.Background(), "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(59), Force: true}, tok, sCoord)
			return e
		}},
		{"invalid_request(负gen)", ErrInvalidRequest, 422, true, func(m *MemStore, tok map[string]bool) *AssignErr {
			_, e := m.Assign(context.Background(), "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(60), ExpectedGeneration: &neg}, tok, sCoord)
			return e
		}},
		{"invalid_request(request_id非UUID形,不入账)", ErrInvalidRequest, 422, false, func(m *MemStore, tok map[string]bool) *AssignErr {
			_, e := m.Assign(context.Background(), "T1", req(sExec, sRev, "r-not-uuid"), tok, sCoord)
			return e
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, tok := aliteWorld(t)
			e := c.run(m, tok)
			if e == nil || e.Code != c.code || e.HTTP != c.http {
				t.Fatalf("应 %s/%d,got %+v", c.code, c.http, e)
			}
			if assignCodeHTTP[e.Code] != c.http {
				t.Fatalf("双射表 %s→%d 与实际 %d 不符", e.Code, assignCodeHTTP[e.Code], c.http)
			}
			// 首果账一致性(应入账的错误:outcome/HTTP/body 三同)
			if c.firstRes {
				fr, _ := m.GetFirstResult(context.Background(), lastReqID(c.name))
				if fr == nil || fr.Outcome != c.code || fr.HTTPCode != c.http || string(fr.Body) != string(failFirstBody(e)) {
					t.Fatalf("首果账应与响应同码同体: %+v", fr)
				}
			}
		})
	}
	// store_down:双射表映射存在(Mem 无法真触发,PG 侧断连可证;此处证映射面)。
	if assignCodeHTTP[ErrStoreDownA] != 503 {
		t.Fatal("store_down→503 映射缺失")
	}
}

// lastReqID 表驱动辅助:各 case 的触发 request_id(与上表 uid 编号一一对应)。
func lastReqID(name string) string {
	switch {
	case strings.HasPrefix(name, "half"):
		return uid(50)
	case strings.HasPrefix(name, "self"):
		return uid(51)
	case strings.HasPrefix(name, "unknown"):
		return uid(52)
	case strings.HasPrefix(name, "wrong"):
		return uid(53)
	case strings.HasPrefix(name, "stale"):
		return uid(55)
	case strings.HasPrefix(name, "requeue"):
		return uid(57)
	case strings.HasPrefix(name, "invalid_request(force"):
		return uid(59)
	case strings.HasPrefix(name, "invalid_request(负"):
		return uid(60)
	}
	return ""
}

// R1② 双射表完备:每码有非零 HTTP、条数对称(无孤儿码/重复映射由 map 键唯一+条数对称证)。
func TestR1ErrorBijection(t *testing.T) {
	codes := []string{ErrHalfAssignment, ErrSelfReview, ErrWrongStateA, ErrStaleGen,
		ErrUnknownAssignee, ErrRequeueState, ErrStoreDownA, ErrOwnerConflict, ErrRequestConflict, ErrInvalidRequest}
	for _, c := range codes {
		if assignCodeHTTP[c] == 0 {
			t.Fatalf("错误码 %s 无 HTTP 映射(孤儿码)", c)
		}
	}
	if len(assignCodeHTTP) != len(codes) {
		t.Fatalf("assignCodeHTTP 条数 %d≠错误码 %d(双射不对称)", len(assignCodeHTTP), len(codes))
	}
}

// R1②③ 失败首果 body 存错误 DTO,重放保真(原 Code+HTTP+Msg 原样)。
func TestR1FailedFirstResultFaithful(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	_, e1 := m.Assign(ctx, "T1", req(sExec, sExec, uid(70)), tok, sCoord)
	if e1 == nil || e1.Code != ErrSelfReview {
		t.Fatalf("首次 self_review: %v", e1)
	}
	_, e2 := m.Assign(ctx, "T1", req(sExec, sExec, uid(70)), tok, sCoord)
	if e2 == nil || e2.Code != ErrSelfReview || e2.HTTP != e1.HTTP || e2.Msg != e1.Msg {
		t.Fatalf("失败重放须保真: first=%+v replay=%+v", e1, e2)
	}
}

// A11 force 不越 requeue_state:in_progress 在途 force 仍拒。
func TestA11ForceNotOverRequeue(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	m.Assign(ctx, "T1", req(sExec, sRev, uid(80)), tok, sCoord)
	tk, _ := m.GetTask(ctx, "T1")
	tk.Status = "in_progress"
	m.tasks["T1"] = *tk
	g1 := int64(1)
	if _, e := m.Assign(ctx, "T1", &AssignRequest{Executor: sThird, Reviewer: sRev, RequestID: uid(81), ExpectedGeneration: &g1, Force: true, Reason: "越闸"}, tok, sCoord); e == nil || e.Code != ErrRequeueState {
		t.Fatalf("force 不越 requeue_state: %v", e)
	}
}

// A10 N-A2 owner_conflict(PUT 路径,Mem)。
func TestA10OwnerConflict(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	cur, _ := m.GetTask(ctx, "T1")
	cur.OwnerSession = sThird
	if _, err := m.PutTask(ctx, *cur, cur.UpdatedAt, false); err != nil {
		t.Fatalf("无 active assignment PUT 改 owner 应放行: %v", err)
	}
	if _, e := m.Assign(ctx, "T1", req(sExec, sRev, uid(90)), tok, sCoord); e != nil {
		t.Fatalf("assign: %v", e)
	}
	tk, _ := m.GetTask(ctx, "T1")
	bad := *tk
	bad.OwnerSession = sRev
	if _, err := m.PutTask(ctx, bad, tk.UpdatedAt, false); !errors.Is(err, ErrOwnerConflictPut) {
		t.Fatalf("PUT owner≠executor 应 owner_conflict: %v", err)
	}
	if _, err := m.PutTask(ctx, bad, tk.UpdatedAt, true); !errors.Is(err, ErrOwnerConflictPut) {
		t.Fatalf("force PUT 仍应 owner_conflict: %v", err)
	}
	ok := *tk
	ok.OwnerSession = sExec
	ok.Status = "review"
	if _, err := m.PutTask(ctx, ok, tk.UpdatedAt, false); err != nil {
		t.Fatalf("PUT owner=executor 应放行: %v", err)
	}
}

// R2-1:request_hash 覆盖完整请求体含 request_id——仅 request_id 异=hash 必异(漏含则同)。
func TestHashCoversRequestID(t *testing.T) {
	a := req(sExec, sRev, uid(100))
	b := req(sExec, sRev, uid(101))
	if mustHash(t, a) == mustHash(t, b) {
		t.Fatal("request_hash 未覆盖 request_id(仅 id 异应 hash 异)")
	}
	// 同请求同 hash(幂等对拍稳定)
	if mustHash(t, a) != mustHash(t, req(sExec, sRev, uid(100))) {
		t.Fatal("同请求 hash 应稳定")
	}
}

// delta v1.8④:request_id 缺失/非法=422 invalid_request 且不入首果账。
func TestInvalidRequestIDNotAccounted(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	for _, rid := range []string{"", "r1", "00000000-0000-4000-8000-zzzzzzzzzzzz", "00000000-0000-4000-8000-00000000000"} {
		_, e := m.Assign(ctx, "T1", req(sExec, sRev, rid), tok, sCoord)
		if e == nil || e.Code != ErrInvalidRequest || e.HTTP != 422 {
			t.Fatalf("rid=%q 应 invalid_request/422: %v", rid, e)
		}
		if fr, _ := m.GetFirstResult(ctx, rid); fr != nil {
			t.Fatalf("非法 request_id 不应入首果账: %+v", fr)
		}
	}
	// 大小写不敏感:大写 hex 合法
	if _, e := m.Assign(ctx, "T1", req(sExec, sRev, "00000000-0000-4000-8000-0000000000AB"), tok, sCoord); e != nil {
		t.Fatalf("大写 hex UUID 应合法: %v", e)
	}
}

// R2-6 写边界深拷贝:recordFirst 后改原 slice 不污染账;读边界(R1⑥)双向。
func TestFirstResultDeepCopyBothWays(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	// 写边界:直接调 recordFirst,事后改原 body
	r := req(sExec, sRev, uid(110))
	body := json.RawMessage(`{"k":"orig"}`)
	m.mu.Lock()
	m.recordFirst(r, "h", "T1", "success", 200, body)
	m.mu.Unlock()
	body[6] = 'X' // 篡改调用方原 slice
	fr, _ := m.GetFirstResult(ctx, uid(110))
	if string(fr.Body) != `{"k":"orig"}` {
		t.Fatalf("写边界未深拷贝,账被外部改污: %s", fr.Body)
	}
	// 读边界:改返回值不污染账
	m.Assign(ctx, "T1", req(sExec, sRev, uid(111)), tok, sCoord)
	fr1, _ := m.GetFirstResult(ctx, uid(111))
	fr1.Body[0] = 'X'
	fr2, _ := m.GetFirstResult(ctx, uid(111))
	if fr2.Body[0] == 'X' {
		t.Fatal("读边界未深拷贝")
	}
}

func timeZero() time.Time { return time.Time{} }

// A13 invalid_request 逐子因(契约 v1.8 D6):①request_id_malformed 不入账②negative_generation
// 入账③force_reason_missing 入账;DTO 与首果 body 均带 sub。
func TestA13InvalidRequestSubCauses(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	// ① request_id_malformed:422+sub+不入账
	_, e := m.Assign(ctx, "T1", req(sExec, sRev, "not-a-uuid"), tok, sCoord)
	if e == nil || e.Code != ErrInvalidRequest || e.HTTP != 422 || e.Sub != SubRequestIDMalformed {
		t.Fatalf("①应 invalid_request/422/sub=request_id_malformed: %+v", e)
	}
	if fr, _ := m.GetFirstResult(ctx, "not-a-uuid"); fr != nil {
		t.Fatal("①不应入首果账")
	}
	// ② negative_generation:422+sub+入账
	neg := int64(-1)
	_, e = m.Assign(ctx, "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(120), ExpectedGeneration: &neg}, tok, sCoord)
	if e == nil || e.Sub != SubNegativeGeneration {
		t.Fatalf("②sub=negative_generation: %+v", e)
	}
	fr, _ := m.GetFirstResult(ctx, uid(120))
	if fr == nil || fr.Outcome != ErrInvalidRequest || !strings.Contains(string(fr.Body), SubNegativeGeneration) {
		t.Fatalf("②应入账且 body 带 sub: %+v", fr)
	}
	// ③ force_reason_missing:422+sub+入账;重放保真含 sub
	_, e = m.Assign(ctx, "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(121), Force: true}, tok, sCoord)
	if e == nil || e.Sub != SubForceReasonMissing {
		t.Fatalf("③sub=force_reason_missing: %+v", e)
	}
	_, e2 := m.Assign(ctx, "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(121), Force: true}, tok, sCoord)
	if e2 == nil || e2.Sub != SubForceReasonMissing || e2.Msg != e.Msg {
		t.Fatalf("③重放保真须含 sub: %+v", e2)
	}
	// ④ 首派带 expected_generation:422+sub+入账(v1.8r2 N2)
	g0 := int64(0)
	_, e = m.Assign(ctx, "T1", &AssignRequest{Executor: sExec, Reviewer: sRev, RequestID: uid(122), ExpectedGeneration: &g0}, tok, sCoord)
	if e == nil || e.Sub != SubExpectedGenOnFirst {
		t.Fatalf("④sub=expected_generation_on_first: %+v", e)
	}
	if fr4, _ := m.GetFirstResult(ctx, uid(122)); fr4 == nil || fr4.Outcome != ErrInvalidRequest {
		t.Fatalf("④应入账: %+v", fr4)
	}
	// ⑤ 大小写幂等(M1):大写形与小写形=同一把幂等键,原样重放不重执行
	up := strings.ToUpper(uid(123))
	r1, e5 := m.Assign(ctx, "T1", req(sExec, sRev, uid(123)), tok, sCoord)
	if e5 != nil {
		t.Fatalf("⑤首派: %v", e5)
	}
	r2, e5b := m.Assign(ctx, "T1", req(sExec, sRev, up), tok, sCoord)
	if e5b != nil || r2.Generation != r1.Generation {
		t.Fatalf("⑤大写形重放应同键原样返(gen 同): %v %+v vs %+v", e5b, r2, r1)
	}
	if fr5, _ := m.GetFirstResult(ctx, strings.ToLower(up)); fr5 == nil {
		t.Fatal("⑤幂等键应为小写规范形单键")
	}
}
