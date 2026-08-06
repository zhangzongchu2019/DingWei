package datad

// A-lite 验收(契约 §6 A1-A12;Mem 载体,PG 集成 D4c 同套跑)。

import (
	"context"
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

// A2 当代唯一:重派后旧代 active=false,当代恰一双角色。
func TestA2ActiveUnique(t *testing.T) {
	m, tok := aliteWorld(t)
	ctx := context.Background()
	m.Assign(ctx, "T1", req("zzc-devo48-2642", "zzc-claudek3-2642", "r1"), tok)
	g1 := int64(1)
	m.Assign(ctx, "T1", &AssignRequest{Executor: "zzc-shencedeepseek-2642", Reviewer: "zzc-claudek3-2642", RequestID: "r2", ExpectedGeneration: &g1}, tok)
	active := 0
	for _, a := range m.assignments {
		if a.TaskID == "T1" && a.Active {
			active++
		}
	}
	if active != 2 { // 当代恰一 executor+一 reviewer
		t.Fatalf("当代 active 应=2(双角色),got %d", active)
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

func timeZero() time.Time { return time.Time{} }
