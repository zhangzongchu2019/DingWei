package datad

// A-lite 验收(契约 §6 A1-A12;Mem 载体,PG 集成 D4c 同套跑)。

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

func aliteWorld(t *testing.T) (*MemStore, map[string]bool) {
	t.Helper()
	m := NewMemStore()
	_, err := m.PutTask(context.Background(),
		Task{ID: "T1", Title: "t", OwnerSession: "zzc-coordinator-2642", Status: "proposed"}, timeZero(), true)
	if err != nil {
		t.Fatal(err)
	}
	tok := map[string]bool{"zzc-devo48-2642": true, "zzc-claudek3-2642": true, "zzc-shencedeepseek-2642": true}
	return m, tok
}

func req(exec, rev, rid string) *AssignRequest {
	return &AssignRequest{Executor: exec, Reviewer: rev, RequestID: rid}
}

// A1 原子双角色(半件拒)+成功首派→dispatched+owner_session=executor(A7 前提)。
func TestA1AtomicDualRole(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	if _, e := m.Assign(ctx, "T1", &AssignRequest{Executor: "zzc-devo48-2642", RequestID: "r0"}, tok); e == nil || e.Code != ErrHalfAssignment {
		t.Fatalf("半件应 half_assignment: %v", e)
	}
	r, e := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	if e != nil || r.Status != "dispatched" || r.Generation != 1 {
		t.Fatalf("首派: %v %+v", e, r)
	}
	tk, _ := m.GetTask(ctx, "T1")
	if tk.OwnerSession != "zzc-devo48-2642" || tk.Status != "dispatched" {
		t.Fatalf("owner_session=executor/status 未落(A7): %+v", tk)
	}
}

// A2 程序级唯一(goroutine 争用下恰一成)。★R1⑤诚实边界:MemStore.Assign 用 mu.Lock 串行,
// 故此测证的是"唯一写路径+程序判据"在 goroutine 争用下不破,**非** DB 层真并发。真并发下的
// partial-unique(assignment_active_uniq)唯一性=PG 侧强制,由 assignment_pg.go 的 PG 集成测试
// (DATAD_TEST_PG_DSN 门控,非 assign 角色直写被拒 + K 真并发恰一成)证——Mem 载体结构性盖不住
// PG 语义面(shenceclaude/dev56 同点),此测不冒充并发唯一证据。
func TestA2SerialUniqueUnderContention(t *testing.T) {
	m, tok := aliteWorld(t)
	const K = 8
	res := make([]*AssignResult, K)
	var wg sync.WaitGroup
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, _ := m.Assign(context.Background(), "T1",
				req("zzc-devo48-2642", "zzc-claudek3-2642", "r"+strconv.Itoa(i)), tok)
			res[i] = r
		}(i)
	}
	wg.Wait()
	// 恰一首派成功(gen1);当代 active 恰 2(双角色);无双 gen1
	ok := 0
	for _, r := range res {
		if r != nil && r.Generation == 1 {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("并发首派应恰一成(gen1),got %d", ok)
	}
	active := 0
	for _, a := range m.assignments {
		if a.TaskID == "T1" && a.Active {
			active++
		}
	}
	if active != 2 {
		t.Fatalf("当代 active 恰 2(partial-unique),got %d", active)
	}
}

// A3 重派 generation+1 旧代保留。
func TestA3GenerationMonotonic(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	g1 := int64(1)
	r2, e := m.Assign(ctx, "T1", &AssignRequest{Executor: "zzc-shencedeepseek-2642", Reviewer: "zzc-claudek3-2642", RequestID: "r2", ExpectedGeneration: &g1}, tok)
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

// A4/A9/A12 request_id 重放三型。
func TestA4A9A12Idempotent(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	r1, _ := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	// A4/A12 成功重放:原样返 body
	r1b, e := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	if e != nil || r1b.Generation != r1.Generation {
		t.Fatalf("成功重放原样返: %v %+v", e, r1b)
	}
	// A9 同 request_id 换参→409 request_conflict
	if _, e := m.Assign(ctx, "T1", req("zzc-shencedeepseek-2642", "zzc-claudek3-2642", "r1"), tok); e == nil || e.Code != ErrRequestConflict {
		t.Fatalf("换参应 request_conflict: %v", e)
	}
	_ = r1
}

// A4 第三型(shenceclaude 交接:条件已变仍原样重放,不重算)——错误首果重放。
// r-bad 首次因 self_review 失败(记错误首果);task 状态/世界变了(已成功派 r1)后,
// 同 r-bad 重放仍返原 self_review 错误码(读首果账,不重算=不会因世界变了而"重派成功")。
func TestA4ThirdType(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	// r-bad 首次 self_review 失败→记错误首果
	if _, e := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-devo48-2642", "r-bad"), tok); e == nil || e.Code != ErrSelfReview {
		t.Fatalf("首次应 self_review: %v", e)
	}
	// 世界变:另一 request 成功派(条件已变)
	m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r-ok"), tok)
	// 同 r-bad 重放(载荷相同):须原样返 self_review,不重算(不会因世界变了成功)
	if _, e := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-devo48-2642", "r-bad"), tok); e == nil || e.Code != ErrSelfReview {
		t.Fatalf("条件已变仍原样重放错误首果(非重算): %v", e)
	}
}

// A5 expected_generation 412。
func TestA5CAS(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	bad := int64(99)
	if _, e := m.Assign(ctx, "T1", &AssignRequest{Executor: "zzc-shencedeepseek-2642", Reviewer: "zzc-claudek3-2642", RequestID: "r2", ExpectedGeneration: &bad}, tok); e == nil || e.Code != ErrStaleGen {
		t.Fatalf("CAS 不符应 stale_generation: %v", e)
	}
}

// A8 unknown_assignee / self_review 双射。
func TestA8ErrorCodes(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	if _, e := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-devo48-2642", "rs"), tok); e == nil || e.Code != ErrSelfReview {
		t.Fatalf("self_review: %v", e)
	}
	if _, e := m.Assign(ctx, "T1", req("nobody", "zzc-claudek3-2642", "ru"), tok); e == nil || e.Code != ErrUnknownAssignee {
		t.Fatalf("unknown_assignee: %v", e)
	}
}

// A11 force 不越 requeue_state:in_progress 在途 force 仍拒。
func TestA11ForceNotOverRequeue(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	// 手动置 in_progress(模拟 /pull 拉取后在途)
	tk, _ := m.GetTask(ctx, "T1")
	tk.Status = "in_progress"
	m.tasks["T1"] = *tk
	g1 := int64(1)
	if _, e := m.Assign(ctx, "T1", &AssignRequest{Executor: "zzc-shencedeepseek-2642", Reviewer: "zzc-claudek3-2642", RequestID: "r2", ExpectedGeneration: &g1, Force: true, Reason: "越闸"}, tok); e == nil || e.Code != ErrRequeueState {
		t.Fatalf("force 不越 requeue_state,在途应拒: %v", e)
	}
}

// A10 N-A2:active assignment 存续期,PUT 改 owner_session≠当代 executor=拒 owner_conflict;
// =executor 放行;无 active assignment 时 PUT 自由改 owner。
func TestA10OwnerConflict(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	// 无 assignment:PUT 自由改 owner
	cur, _ := m.GetTask(ctx, "T1")
	cur.OwnerSession = "zzc-shencedeepseek-2642"
	if _, err := m.PutTask(ctx, *cur, cur.UpdatedAt, false); err != nil {
		t.Fatalf("无 active assignment PUT 改 owner 应放行: %v", err)
	}
	// assign(owner_session→executor=devo48)
	if _, e := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok); e != nil {
		t.Fatalf("assign: %v", e)
	}
	// PUT owner≠executor→拒 owner_conflict
	tk, _ := m.GetTask(ctx, "T1")
	bad := *tk
	bad.OwnerSession = "zzc-claudek3-2642" // ≠当代 executor(devo48)
	if _, err := m.PutTask(ctx, bad, tk.UpdatedAt, false); !errors.Is(err, ErrOwnerConflictPut) {
		t.Fatalf("PUT owner≠executor 应 owner_conflict: %v", err)
	}
	// force 也不越(改派须走 /assign)
	if _, err := m.PutTask(ctx, bad, tk.UpdatedAt, true); !errors.Is(err, ErrOwnerConflictPut) {
		t.Fatalf("force PUT owner≠executor 仍应 owner_conflict(改派走 /assign): %v", err)
	}
	// PUT owner=executor→放行(A7 前提维持)
	ok := *tk
	ok.OwnerSession = "zzc-devo48-2642"
	ok.Status = "review"
	if _, err := m.PutTask(ctx, ok, tk.UpdatedAt, false); err != nil {
		t.Fatalf("PUT owner=executor 应放行: %v", err)
	}
}

// R1② 请求与错误双射完整:每个错误码在 assignCodeHTTP 有非零 HTTP,且条数对称(无孤儿码)。
func TestR1ErrorBijection(t *testing.T) {
	codes := []string{ErrHalfAssignment, ErrSelfReview, ErrWrongStateA, ErrStaleGen,
		ErrUnknownAssignee, ErrRequeueState, ErrStoreDownA, ErrOwnerConflict, ErrRequestConflict}
	for _, c := range codes {
		if assignCodeHTTP[c] == 0 {
			t.Fatalf("错误码 %s 无 HTTP 映射(双射不完整=孤儿码)", c)
		}
	}
	if len(assignCodeHTTP) != len(codes) {
		t.Fatalf("assignCodeHTTP 条数 %d≠错误码 %d(双射不对称)", len(assignCodeHTTP), len(codes))
	}
}

// R1②③ 失败首果 body 存错误 DTO,重放保真(原 Code+HTTP+Msg 原样,非退化"重放首果")。
func TestR1FailedFirstResultFaithful(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	_, e1 := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-devo48-2642", "r-bad"), tok)
	if e1 == nil || e1.Code != ErrSelfReview {
		t.Fatalf("首次 self_review: %v", e1)
	}
	_, e2 := m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-devo48-2642", "r-bad"), tok)
	if e2 == nil || e2.Code != ErrSelfReview || e2.HTTP != e1.HTTP || e2.Msg != e1.Msg {
		t.Fatalf("失败重放须保真(Code/HTTP/Msg 原样,非退化): first=%+v replay=%+v", e1, e2)
	}
}

// R1⑥ GetFirstResult 深拷贝:改返回值 Body 不污染内存账。
func TestR1FirstResultDeepCopy(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	fr1, _ := m.GetFirstResult(ctx, "r1")
	if fr1 == nil || len(fr1.Body) == 0 {
		t.Fatal("首果应有 body")
	}
	fr1.Body[0] = 'X' // 篡改返回副本
	fr2, _ := m.GetFirstResult(ctx, "r1")
	if fr2.Body[0] == 'X' {
		t.Fatal("GetFirstResult 未深拷贝:改返回值污染了内存账")
	}
}

func timeZero() time.Time { return time.Time{} }
