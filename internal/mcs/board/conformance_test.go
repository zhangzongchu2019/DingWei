// board conformance suite —— §4.5 要求的「先固化、再判保留或替换」的那一份。
//
// 🔴 与同目录既有 47 个测试的关键差别，先写在最前面：
//
//	既有测试是 `package board`（内部包）：它们能摸 `b.db`、跑裸 SQL、调未导出的 `b.now()`。
//	它们**确实固化了语义**，但**验不了一个重写后的实现** —— 换掉存储层，它们会整片变红，
//	而红的原因与语义无关，是耦合。§4.5 要的不是「有测试」，是
//	「既有并发与代际语义须由 conformance suite 先固化，**再逐项判保留或替换**」。
//	⇒ 本文件是 `package board_test`（外部包），**只用导出 API**，不碰任何内部状态。
//	   任何一个满足同一导出契约的实现，都应当能原样跑过本文件。
//
// ⚠️ 一处刻意的「弱化」，不是疏漏：
//
//	既有测试常断言「board_event 总数不变」。那对 conformance 是**过度规定** ——
//	重写后的实现完全可以合法地多记一条别的簿记事件。契约级的说法是
//	**「不得新增 <某一 kind> 的事件」**，故本文件一律按 kind 断言，不按总数。
//
// ⚠️ 本文件当前覆盖 §4.5 点名的五族中的五族各一条主干用例，**不是全量**。
//
//	它是「可用来验重写」的地基与样板，不是「已把既有语义全部搬完」的声明。
//	剩余用例逐条从既有测试移植（移植即：把裸 SQL 断言改写成按 kind / 按导出读模型断言）。
package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
)

func roles() board.RoleAssignments {
	rb := func(w string) board.RoleBinding {
		return board.RoleBinding{Who: w, CLI: "claude", LLM: "claude-opus-4-8"}
	}
	return board.RoleAssignments{
		Designer: rb("devo48"), Implementer: rb("devo48"),
		Tester: rb("tester"), QAOwner: rb("shence"),
	}
}

// newRig 只用导出 API 建场：接管 → 建 task → 两节点（n1 ready，n2 依赖 n1）。
func newRig(t *testing.T) (*board.Board, board.Identity, context.Context) {
	t.Helper()
	ctx := context.Background()
	b, err := board.Open(":memory:", &clock.Fake{T: time.Unix(10_000, 0)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	epoch, err := b.Takeover(ctx, "board1", "orch1", 3600)
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	id := board.Identity{Owner: "orch1", Epoch: epoch}
	if err := b.CreateTask(ctx, "t1", "board1", "任务一", roles(), id); err != nil {
		t.Fatalf("create task: %v", err)
	}
	for _, a := range []string{"w0", "w1", "w-new"} {
		if err := b.RegisterAgent(ctx, a, "claude"); err != nil {
			t.Fatalf("register agent %s: %v", a, err)
		}
	}
	if err := b.AddNode(ctx, "n1", "t1", 1, "ready", nil); err != nil {
		t.Fatalf("add n1: %v", err)
	}
	if err := b.AddNode(ctx, "n2", "t1", 2, "pending", []string{"n1"}); err != nil {
		t.Fatalf("add n2: %v", err)
	}
	return b, id, ctx
}

// chk 返回一个只收 (Outcome, error) 的检查器 —— Go 只在「多值调用是唯一实参」时才允许展开，
// 故写成 chk(t, want, what)(b.Send(...)) 这种两段式。
func chk(t *testing.T, want board.Outcome, what string) func(board.Outcome, error) {
	t.Helper()
	return func(got board.Outcome, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if got != want {
			t.Fatalf("%s outcome=%s want %s", what, got, want)
		}
	}
}

func kindCount(t *testing.T, b *board.Board, ctx context.Context, node, kind string) int {
	t.Helper()
	n, err := b.Events(ctx, node, kind)
	if err != nil {
		t.Fatalf("events %s/%s: %v", node, kind, err)
	}
	return n
}

// —— 族① CAS：迟到的 processing 必须 cas_miss，且不产生该 kind 的事件 ——
func TestConformance_CAS_LateProcessingIsCASMiss(t *testing.T) {
	b, id, ctx := newRig(t)
	chk(t, board.OutcomeApplied, "send")(b.Send(ctx, "n1", 1, "rid-1", "w0", id))
	chk(t, board.OutcomeApplied, "done")(b.Terminal(ctx, "n1", 1, "rid-1", true, "", id))

	before := kindCount(t, b, ctx, "n1", "node_processing")
	chk(t, board.OutcomeCASMiss, "迟到 processing")(b.Processing(ctx, "n1", 1, "rid-1", id))

	if after := kindCount(t, b, ctx, "n1", "node_processing"); after != before {
		t.Fatalf("迟到 processing 写了 node_processing 事件 %d→%d（须零副作用）", before, after)
	}
	s, err := b.GetNode(ctx, "n1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if s.Status != "done" {
		t.Fatalf("status=%s want done —— 迟到回执不得改终态", s.Status)
	}
}

// —— 族② 代际：旧代际的回执必须被拒 ——
func TestConformance_Generation_StaleGenerationRejected(t *testing.T) {
	b, id, ctx := newRig(t)
	chk(t, board.OutcomeApplied, "send gen1")(b.Send(ctx, "n1", 1, "rid-1", "w0", id))
	chk(t, board.OutcomeApplied, "reassign→gen2")(b.Reassign(ctx, "n1", 1, "w1", id))

	s, _ := b.GetNode(ctx, "n1")
	if s.Generation != 2 {
		t.Fatalf("代际=%d want 2", s.Generation)
	}
	// 用旧代际 1 汇报，必须被拒
	oc, err := b.Terminal(ctx, "n1", 1, "rid-1", true, "", id)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if oc == board.OutcomeApplied {
		t.Fatal("旧代际回执被接受 —— 代际语义已丢失")
	}
	if s2, _ := b.GetNode(ctx, "n1"); s2.Status == "done" {
		t.Fatal("旧代际回执改了终态")
	}
}

// —— 族③ 迟到结果处置：同 rid 重复 done 幂等，node_done 恰一 ——
func TestConformance_LateResult_DuplicateDoneIsIdempotent(t *testing.T) {
	b, id, ctx := newRig(t)
	chk(t, board.OutcomeApplied, "send")(b.Send(ctx, "n1", 1, "rid-1", "w0", id))
	chk(t, board.OutcomeApplied, "processing")(b.Processing(ctx, "n1", 1, "rid-1", id))
	chk(t, board.OutcomeApplied, "首 done")(b.Terminal(ctx, "n1", 1, "rid-1", true, "ref://r", id))
	chk(t, board.OutcomeCASMiss, "重复 done")(b.Terminal(ctx, "n1", 1, "rid-1", true, "ref://r", id))

	if n := kindCount(t, b, ctx, "n1", "node_done"); n != 1 {
		t.Fatalf("node_done=%d want 1（恰一）", n)
	}
}

// —— 族④ fan-out：n1 done 之后，依赖它的 n2 必须变为可取 ——
func TestConformance_FanOut_DependentBecomesReady(t *testing.T) {
	b, id, ctx := newRig(t)
	if ready, _ := b.ReadyNodes(ctx); contains(ready, "n2") {
		t.Fatal("n2 依赖未满足时就已 ready —— fan-out 前置条件被破坏")
	}
	chk(t, board.OutcomeApplied, "send")(b.Send(ctx, "n1", 1, "rid-1", "w0", id))
	chk(t, board.OutcomeApplied, "done")(b.Terminal(ctx, "n1", 1, "rid-1", true, "", id))

	ready, err := b.ReadyNodes(ctx)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if !contains(ready, "n2") {
		t.Fatalf("n1 完成后 n2 未变可取，ready=%v —— fan-out 语义已丢失", ready)
	}
}

// —— 族⑤ reconciler 谓词：Sweep 对【未到重投时限】的在途节点不得误判 ——
//
// 🔴 本用例第一版是错的，记在这里当反面教材：
//
//	第一版把节点推到 processing 再 Sweep，而 sweep 的重投谓词只扫 status='dispatched' ——
//	**那个节点根本不在被扫集合里**，于是我把重投谓词的时限条件改坏（now- 改成 now+），
//	套件照样全绿。测试通过，但它测的不是它以为的东西。
//	⇒ 停在 dispatched（只 Send，不 Processing），节点才真正落在谓词的作用域内。
//
//	第二版**仍然是错的**：改对了作用域，断言对象却还是 status/generation ——
//	而重投谓词【只产候选、不改状态】，于是同一个变异照样全绿。第三版才断言 SweepReport 本身。
//	⚠️ 两条教训，都是变异测试逼出来的、不是读代码读出来的：
//	   ① 被测对象须落在被测谓词的**作用域**内；② 断言须落在该谓词真正**产生效果**的那个出口上。
//	   作用域对了而出口错了，测试照样恒绿。
func TestConformance_Reconciler_SweepDoesNotTouchFreshInFlight(t *testing.T) {
	b, id, ctx := newRig(t)
	// 只 Send，停在 dispatched —— 这才是重投谓词真正扫描的状态。
	chk(t, board.OutcomeApplied, "send")(b.Send(ctx, "n1", 1, "rid-1", "w0", id))

	before, _ := b.GetNode(ctx, "n1")
	if before.Status != "dispatched" {
		t.Fatalf("前置不成立：status=%s want dispatched —— 本用例必须停在谓词作用域内", before.Status)
	}
	rep, err := b.Sweep(ctx, id, 3600)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	// 🔴 断言对象必须是 SweepReport，不是节点状态：重投谓词【只产候选、不改状态】，
	//    断言 status/generation 不变永远抓不到谓词被改坏 —— 这是本用例的第二次修正。
	if len(rep.RedeliverCandidates) != 0 {
		t.Fatalf("未到重投时限却被列为候选：%v —— reconciler 谓词的时限条件已失效", rep.RedeliverCandidates)
	}
	after, _ := b.GetNode(ctx, "n1")
	if after.Status != before.Status || after.Generation != before.Generation {
		t.Fatalf("Sweep 动了未超时的在途节点：%s/gen%d → %s/gen%d",
			before.Status, before.Generation, after.Status, after.Generation)
	}
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
