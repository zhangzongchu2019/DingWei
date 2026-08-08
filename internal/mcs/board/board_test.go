package board

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)


// testRoles 建单角色 fixture(M5v2:建单必填全)。
func testRoles() RoleAssignments {
	rb := func(w string) RoleBinding { return RoleBinding{Who: w, CLI: "claude", LLM: "claude-opus-4-8"} }
	return RoleAssignments{Designer: rb("devo48"), Implementer: rb("devo48"), Tester: rb("tester"), QAOwner: rb("shence")}
}
// rig 起一块内存 Board:orchestrator 已接管,一 task 两节点(n1 ready,n2 pending deps=[n1])。
func rig(t *testing.T) (*Board, Identity, context.Context) {
	t.Helper()
	ctx := context.Background()
	b, err := Open(":memory:", &clock.Fake{T: time.Unix(10_000, 0)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	epoch, err := b.Takeover(ctx, "board1", "orch1", 3600)
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	id := Identity{Owner: "orch1", Epoch: epoch}
	if err := b.CreateTask(ctx, "t1", "board1", "任务一", testRoles(), id); err != nil {
		t.Fatalf("task: %v", err)
	}
	for _, a := range []string{"w0", "w1", "w-new"} {
		if err := b.RegisterAgent(ctx, a, "claude"); err != nil {
			t.Fatalf("agent %s: %v", a, err)
		}
	}
	if err := b.AddNode(ctx, "n1", "t1", 1, "ready", nil); err != nil {
		t.Fatalf("n1: %v", err)
	}
	if err := b.AddNode(ctx, "n2", "t1", 2, "pending", []string{"n1"}); err != nil {
		t.Fatalf("n2: %v", err)
	}
	return b, id, ctx
}

func mustOutcome(t *testing.T, got Outcome, want Outcome, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s outcome=%s want %s", what, got, want)
	}
}

// ---- 分派主链: send → processing → terminal(done) + fan-out ----

func TestDispatchChain_SendProcessingDoneFanOut(t *testing.T) {
	b, id, ctx := rig(t)
	oc, err := b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	if err != nil {
		t.Fatal(err)
	}
	mustOutcome(t, oc, OutcomeApplied, "send")
	if s, _ := b.GetNode(ctx, "n1"); s.Status != "dispatched" || s.DeliveryID.String != "rid-1" {
		t.Fatalf("after send: %+v", s)
	}
	oc, _ = b.Processing(ctx, "n1", 1, "rid-1", id)
	mustOutcome(t, oc, OutcomeApplied, "processing")
	oc, _ = b.Terminal(ctx, "n1", 1, "rid-1", true, "ref://r", id)
	mustOutcome(t, oc, OutcomeApplied, "terminal")
	// A案 fan-out:n2 与 done 同事务提升(V27 家族:'done 已提交而后继未提升'不可达)
	if s, _ := b.GetNode(ctx, "n2"); s.Status != "ready" {
		t.Fatalf("n2 未随 done 原子提升:%+v", s)
	}
	for kind, want := range map[string]int{"node_dispatched": 1, "node_started": 1, "node_done": 1} {
		if n, _ := b.Events(ctx, "n1", kind); n != want {
			t.Fatalf("event %s=%d want %d", kind, n, want)
		}
	}
	if n, _ := b.Events(ctx, "n2", "node_ready"); n != 1 {
		t.Fatalf("node_ready=%d want 1(提示恰一条)", n)
	}
}

// C6:同 request_id 重投 → 幂等返旧 outcome,不重复施加。
func TestC6_RequestIDIdempotent(t *testing.T) {
	b, id, ctx := rig(t)
	oc1, _ := b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	oc2, _ := b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	mustOutcome(t, oc1, OutcomeApplied, "first")
	mustOutcome(t, oc2, OutcomeApplied, "replay(返旧)")
	if n, _ := b.Events(ctx, "n1", "node_dispatched"); n != 1 {
		t.Fatalf("重投不得重复施加:events=%d", n)
	}
}

// C5:非当前代/非法态 → CAS 拒。
func TestC5_StaleGenerationRejected(t *testing.T) {
	b, id, ctx := rig(t)
	oc, _ := b.Send(ctx, "n1", 99, "rid-x", "w0", id)
	mustOutcome(t, oc, OutcomeCASMiss, "旧代 send")
	if s, _ := b.GetNode(ctx, "n1"); s.Status != "ready" {
		t.Fatalf("零副作用被破:%+v", s)
	}
}

// C8:旧投递(前 rid)迟到 terminal → 拒、零副作用、当前投递不受扰。
func TestC8_LateTerminalOldDeliveryRejected(t *testing.T) {
	b, id, ctx := rig(t)
	b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	// 换代(failover):gen2、新 rid
	oc, _ := b.Reassign(ctx, "n1", 1, "w1", id)
	mustOutcome(t, oc, OutcomeApplied, "reassign")
	b.Send(ctx, "n1", 2, "rid-2", "w1", id)
	// 旧 rid-1 的迟到终结:必须拒
	oc, _ = b.Terminal(ctx, "n1", 2, "rid-1", true, "", id)
	mustOutcome(t, oc, OutcomeCASMiss, "旧 rid terminal")
	if s, _ := b.GetNode(ctx, "n1"); s.Status != "dispatched" || s.Generation != 2 {
		t.Fatalf("当前投递被扰:%+v", s)
	}
	// 当前 rid-2 正常销账
	oc, _ = b.Terminal(ctx, "n1", 2, "rid-2", true, "", id)
	mustOutcome(t, oc, OutcomeApplied, "当前 rid terminal")
}

// C4:并发两 reassign 同代 → 仅一成功(CAS),另一 cas_miss,不双改派。
func TestC4_ConcurrentReassignOnlyOneWins(t *testing.T) {
	b, id, ctx := rig(t)
	var wg sync.WaitGroup
	results := make([]Outcome, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			oc, err := b.Reassign(ctx, "n1", 1, "w-new", id)
			if err != nil {
				t.Errorf("reassign %d: %v", i, err)
				return
			}
			results[i] = oc
		}(i)
	}
	wg.Wait()
	applied := 0
	for _, oc := range results {
		if oc == OutcomeApplied {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("并发 reassign applied=%d want 1: %v", applied, results)
	}
	if s, _ := b.GetNode(ctx, "n1"); s.Generation != 2 {
		t.Fatalf("代际=%d want 2(恰一次换代)", s.Generation)
	}
}

// guard②:lease 过期【即】失权(权威 store 时钟),不等 takeover。
func TestGuard_ExpiredLeaseLosesAuthority(t *testing.T) {
	ctx := context.Background()
	fc := &clock.Fake{T: time.Unix(10_000, 0)}
	b, err := Open(":memory:", fc)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	epoch, _ := b.Takeover(ctx, "board1", "orch1", 60) // TTL 60s
	id := Identity{Owner: "orch1", Epoch: epoch}
	b.CreateTask(ctx, "t1", "board1", "x", testRoles(), id)
	b.RegisterAgent(ctx, "w0", "claude")
	b.AddNode(ctx, "n1", "t1", 1, "ready", nil)
	fc.Advance(2 * time.Minute) // 过期
	oc, _ := b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	mustOutcome(t, oc, OutcomeCASMiss, "过期 lease send 应拒")
	// renew 也失败(过期后只能 takeover)
	if ok, _ := b.Renew(ctx, "board1", id, 60); ok {
		t.Fatal("过期 renew 应失败")
	}
	// takeover 后 epoch+1,旧 epoch 身份被 fence
	newEpoch, _ := b.Takeover(ctx, "board1", "orch2", 3600)
	if newEpoch != epoch+1 {
		t.Fatalf("epoch=%d want %d", newEpoch, epoch+1)
	}
	oc, _ = b.Send(ctx, "n1", 1, "rid-2", "w0", id) // 旧 epoch
	mustOutcome(t, oc, OutcomeCASMiss, "僵尸 epoch send 应拒")
	oc, _ = b.Send(ctx, "n1", 1, "rid-3", "w0", Identity{Owner: "orch2", Epoch: newEpoch})
	mustOutcome(t, oc, OutcomeApplied, "新任 send")
}

// ★MC2-1/MC3-1(V42a):processing 丢失 → terminal 从【普通 dispatched(无注记)】直达终态;无伪 clear 事件。
func TestV42a_TerminalDirectFromPlainDispatched(t *testing.T) {
	b, id, ctx := rig(t)
	b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	oc, _ := b.Terminal(ctx, "n1", 1, "rid-1", true, "ref", id) // 无 processing
	mustOutcome(t, oc, OutcomeApplied, "直达 terminal")
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "done" {
		t.Fatalf("status=%s want done", s.Status)
	}
	if n, _ := b.Events(ctx, "n1", "node_annotation_cleared"); n != 0 {
		t.Fatalf("无注记不得发伪 clear:%d", n)
	}
}

// ★MC1+V42b:UNKNOWN 注记(delivery_unresolved)期间 reassign 拒;注记后 terminal 直达=清注记+恰一条 clear。
func TestMC1_V42b_AnnotatedWindow(t *testing.T) {
	b, id, ctx := rig(t)
	b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	oc, _ := b.SetBlocker(ctx, "n1", 1, "delivery_unresolved", id)
	mustOutcome(t, oc, OutcomeApplied, "annotate")
	// 注记期 reassign 必拒(防旧 rid 迟到执行=双执行)
	oc, _ = b.Reassign(ctx, "n1", 1, "w1", id)
	mustOutcome(t, oc, OutcomeCASMiss, "注记期 reassign")
	// terminal 直达:原子终结+清注记+恰一条 clear 事件
	oc, _ = b.Terminal(ctx, "n1", 1, "rid-1", false, "", id)
	mustOutcome(t, oc, OutcomeApplied, "注记后 terminal 直达")
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "failed" || s.BlockerReason.Valid {
		t.Fatalf("注记未随终结清:%+v", s)
	}
	if n, _ := b.Events(ctx, "n1", "node_annotation_cleared"); n != 1 {
		t.Fatalf("clear 事件=%d want 恰 1", n)
	}
}

// ★MC2-3(V43-lite):reassign 原子归零 delivery_attempt;send 新 rid 预算从 0。
func TestMC23_ReassignZeroesDeliveryAttempt(t *testing.T) {
	b, id, ctx := rig(t)
	b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	// 模拟 redeliver 耗预算(redeliver 原语属 M4;此处直写列以验归零语义)
	if _, err := b.db.Exec(`UPDATE node SET delivery_attempt=3 WHERE node_id='n1'`); err != nil {
		t.Fatal(err)
	}
	oc, _ := b.Reassign(ctx, "n1", 1, "w1", id)
	mustOutcome(t, oc, OutcomeApplied, "reassign")
	s, _ := b.GetNode(ctx, "n1")
	if s.DeliveryAttempt != 0 {
		t.Fatalf("delivery_attempt=%d want 0(原子归零)", s.DeliveryAttempt)
	}
	if s.Generation != 2 || s.Status != "ready" || s.DeliveryID.Valid {
		t.Fatalf("换代态错:%+v", s)
	}
}

// R2:send 是 starved 注记唯一清除者,条件 clear 恰一条。
func TestR2_SendClearsStarvedWithSingleEvent(t *testing.T) {
	b, id, ctx := rig(t)
	b.SetBlocker(ctx, "n1", 1, "starved", id)
	oc, _ := b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	mustOutcome(t, oc, OutcomeApplied, "send")
	s, _ := b.GetNode(ctx, "n1")
	if s.BlockerReason.Valid {
		t.Fatalf("starved 未随 dispatch 清:%+v", s)
	}
	if n, _ := b.Events(ctx, "n1", "node_annotation_cleared"); n != 1 {
		t.Fatalf("clear=%d want 1", n)
	}
}

// fan-out recover:dep_failed 的 blocked 后继在 dep 修复 done 后同事务自动解卡(V37 家族)。
func TestFanOut_RecoverDepFailedChild(t *testing.T) {
	b, id, ctx := rig(t)
	// n2 置为 blocked(dep_failed)(建图期 born-blocked 的运行时等价)
	if _, err := b.db.Exec(`UPDATE node SET status='blocked', blocker_reason='dep_failed' WHERE node_id='n2'`); err != nil {
		t.Fatal(err)
	}
	b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	b.Processing(ctx, "n1", 1, "rid-1", id)
	oc, _ := b.Terminal(ctx, "n1", 1, "rid-1", true, "", id)
	mustOutcome(t, oc, OutcomeApplied, "terminal")
	s, _ := b.GetNode(ctx, "n2")
	if s.Status != "ready" || s.BlockerReason.Valid {
		t.Fatalf("blocked(dep_failed) 未随 dep done 自动 recover:%+v", s)
	}
	if n, _ := b.Events(ctx, "n2", "node_recovered"); n != 1 {
		t.Fatalf("recover 事件=%d want 1", n)
	}
}
