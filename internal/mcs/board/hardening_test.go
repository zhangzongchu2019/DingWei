package board

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

// toRunning 把 n1 推到 running(steer/interrupt 预留只存在于 running)。
func toRunning(t *testing.T, b *Board, id Identity, ctx context.Context, rid string) {
	t.Helper()
	if oc, _ := b.Send(ctx, "n1", 1, rid, "w0", id); oc != OutcomeApplied {
		t.Fatalf("send=%s", oc)
	}
	if oc, _ := b.Processing(ctx, "n1", 1, rid, id); oc != OutcomeApplied {
		t.Fatalf("processing=%s", oc)
	}
}

// ---- steer(O10/O12/O17 家族) ----

// steer 预留未决(含过期 uncertain)拦 terminal/reassign;NOT_APPLIED_FENCED 终局才 rejected 放行。
func TestSteer_ReservationBarrierUntilFinalize(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	if oc, _ := b.SteerReserve(ctx, "n1", 1, "steer-1", 60, id); oc != "pending" {
		t.Fatalf("reserve=%s", oc)
	}
	// 未决期 terminal/reassign 必拒(栅栏)
	if oc, _ := b.Terminal(ctx, "n1", 1, "rid-1", true, "", id); oc != OutcomeCASMiss {
		t.Fatalf("terminal 应被预留拦:%s", oc)
	}
	if oc, _ := b.Reassign(ctx, "n1", 1, "w1", id); oc != OutcomeCASMiss {
		t.Fatalf("reassign 应被预留拦:%s", oc)
	}
	// 过期→uncertain(降级非放行,★四核#1):栅栏继续
	b.clk.(*clock.Fake).Advance(2 * time.Minute)
	rep, err := b.Sweep(ctx, id, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExpiredReservations != 1 {
		t.Fatalf("expired=%d want 1", rep.ExpiredReservations)
	}
	if oc, _ := b.Terminal(ctx, "n1", 1, "rid-1", true, "", id); oc != OutcomeCASMiss {
		t.Fatalf("uncertain 仍须拦 terminal:%s", oc)
	}
	// driver 终局 NOT_APPLIED_FENCED → rejected,栅栏释放
	if oc, _ := b.SteerFinalize(ctx, "steer-1", false, id); oc != OutcomeRejected {
		t.Fatalf("finalize rejected=%s", oc)
	}
	if oc, _ := b.Terminal(ctx, "n1", 1, "rid-1", true, "", id); oc != OutcomeApplied {
		t.Fatalf("释放后 terminal=%s", oc)
	}
}

// steer applied:确证注入→applied+node_steered 恰一条;重复 finalize 零写入(恰一次)。
func TestSteer_FinalizeAppliedExactlyOnce(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	b.SteerReserve(ctx, "n1", 1, "steer-1", 60, id)
	if oc, _ := b.SteerFinalize(ctx, "steer-1", true, id); oc != OutcomeApplied {
		t.Fatalf("applied=%s", oc)
	}
	if oc, _ := b.SteerFinalize(ctx, "steer-1", true, id); oc != OutcomeCASMiss {
		t.Fatalf("重复 finalize 应零写入:%s", oc)
	}
	if n, _ := b.Events(ctx, "n1", "node_steered"); n != 1 {
		t.Fatalf("node_steered=%d want 恰 1", n)
	}
}

// per-(node,generation) 互斥:同代第二个预留拒(busy);steer×interrupt 双向同代亦互斥(O33)。
func TestReservation_PerGenerationMutex(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	b.SteerReserve(ctx, "n1", 1, "steer-1", 60, id)
	if oc, _ := b.SteerReserve(ctx, "n1", 1, "steer-2", 60, id); oc != OutcomeCASMiss {
		t.Fatalf("同代二预留应拒:%s", oc)
	}
	if oc, _ := b.InterruptIntent(ctx, "n1", 1, "int-1", 60, id); oc != OutcomeCASMiss {
		t.Fatalf("steer 未决时 interrupt 同代应拒(O33):%s", oc)
	}
}

// ---- interrupt(O31/O32 家族:B 模型) ----

// interrupt 未决【不】拦 reassign/terminal(防死锁);换代穿越后旧代 intent 仍可确定终结,
// 事件记预留代际(旧代);旧代 uncertain 不堵新代新预留(按代分域)。
func TestInterrupt_BModelCrossGeneration(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	if oc, _ := b.InterruptIntent(ctx, "n1", 1, "int-1", 60, id); oc != "pending" {
		t.Fatalf("intent=%s", oc)
	}
	// interrupt 未决不拦 reassign(★六核#1 只拦 steer)
	if oc, _ := b.Reassign(ctx, "n1", 1, "w1", id); oc != OutcomeApplied {
		t.Fatalf("interrupt 未决不得拦 reassign:%s", oc)
	}
	// 换代后:旧代 intent 仍可 finalize applied,事件记旧代(gen=1)
	if oc, _ := b.InterruptFinalize(ctx, "int-1", true, id); oc != OutcomeApplied {
		t.Fatalf("旧代 intent finalize=%s", oc)
	}
	if n, _ := b.Events(ctx, "n1", "node_interrupted"); n != 1 {
		t.Fatalf("node_interrupted=%d", n)
	}
	// 新代(gen2)可建新预留:旧代已终结不堵;先推 running
	if oc, _ := b.Send(ctx, "n1", 2, "rid-2", "w1", id); oc != OutcomeApplied {
		t.Fatalf("send g2=%s", oc)
	}
	if oc, _ := b.Processing(ctx, "n1", 2, "rid-2", id); oc != OutcomeApplied {
		t.Fatalf("processing g2=%s", oc)
	}
	if oc, _ := b.InterruptIntent(ctx, "n1", 2, "int-2", 60, id); oc != "pending" {
		t.Fatalf("新代 intent=%s(旧代不得堵新代,O32)", oc)
	}
}

// ---- redeliver / deliver_fail(V21/V38 家族) ----

// V21:redeliver 链 delivery_attempt 递增至 max_delivery 耗尽→拒;执行 attempt 不受扰(计数分离)。
func TestV21_RedeliverBudgetExhaustion(t *testing.T) {
	b, id, ctx := rig(t)
	b.Send(ctx, "n1", 1, "rid-1", "w0", id) // delivery_attempt=0,max=3
	for i := 1; i <= 3; i++ {
		if oc, _ := b.Redeliver(ctx, "n1", 1, "rid-1", id); oc != OutcomeApplied {
			t.Fatalf("redeliver#%d=%s", i, oc)
		}
	}
	if oc, _ := b.Redeliver(ctx, "n1", 1, "rid-1", id); oc != OutcomeCASMiss {
		t.Fatalf("耗尽后应拒:%s", oc)
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.DeliveryAttempt != 3 || s.Attempt != 0 {
		t.Fatalf("计数错:delivery=%d attempt=%d(须分离)", s.DeliveryAttempt, s.Attempt)
	}
	if n, _ := b.Events(ctx, "n1", "node_redelivered"); n != 3 {
		t.Fatalf("redelivered 事件=%d want 3(逐次)", n)
	}
}

// V38 家族:UNKNOWN→保持 dispatched+delivery_unresolved(不迁 blocked/failed);
// 迟到 processing 正常收敛+清注记;终局 FENCED+耗尽→failed(delivery_exhausted)。
func TestV38_DeliverFailUnknownVsTerminal(t *testing.T) {
	b, id, ctx := rig(t)
	b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	// UNKNOWN:注记不迁移
	if oc, _ := b.DeliverFail(ctx, "n1", 1, "rid-1", false, id); oc != OutcomeApplied {
		t.Fatalf("unknown=%s", oc)
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "dispatched" || s.BlockerReason.String != "delivery_unresolved" {
		t.Fatalf("UNKNOWN 处置错:%+v", s)
	}
	// 迟到 processing:正常 running+注记清('有副作用不认账'不可达)
	if oc, _ := b.Processing(ctx, "n1", 1, "rid-1", id); oc != OutcomeApplied {
		t.Fatalf("迟到 processing=%s", oc)
	}
	s, _ = b.GetNode(ctx, "n1")
	if s.Status != "running" || s.BlockerReason.Valid {
		t.Fatalf("收敛错:%+v", s)
	}
	// 另一节点:终局 FENCED+耗尽→failed
	if err := b.AddNode(ctx, "n3", "t1", 5, "ready", nil); err != nil {
		t.Fatal(err)
	}
	b.Send(ctx, "n3", 1, "rid-3", "w1", id)
	for i := 0; i < 3; i++ {
		b.Redeliver(ctx, "n3", 1, "rid-3", id)
	}
	// 未耗尽前终局 FENCED 也不迁(guard: attempt>=max)——先验负例
	if err := b.AddNode(ctx, "n4", "t1", 6, "ready", nil); err != nil {
		t.Fatal(err)
	}
	b.Send(ctx, "n4", 1, "rid-4", "w1", id)
	if oc, _ := b.DeliverFail(ctx, "n4", 1, "rid-4", true, id); oc != OutcomeCASMiss {
		t.Fatalf("未耗尽 FENCED 应拒迁 failed:%s", oc)
	}
	if oc, _ := b.DeliverFail(ctx, "n3", 1, "rid-3", true, id); oc != OutcomeApplied {
		t.Fatalf("耗尽+FENCED=%s", oc)
	}
	if s, _ := b.GetNode(ctx, "n3"); s.Status != "failed" {
		t.Fatalf("n3=%+v", s)
	}
}

// ---- result_outbox(V31 家族) ----

// 每版恰一次:同 key 幂等;task_reopened 版本+1=新 key 不相交;ack 恰一次。
func TestV31_ResultOutboxPerVersionExactlyOnce(t *testing.T) {
	b, id, ctx := rig(t)
	_ = id
	k1 := ResultKey("t1", "env-1", 1)
	if ok, _ := b.EnqueueResult(ctx, k1, "t1", "addr", "ref1"); !ok {
		t.Fatal("first enqueue")
	}
	if ok, _ := b.EnqueueResult(ctx, k1, "t1", "addr", "ref1"); ok {
		t.Fatal("同 key 重复 enqueue 应幂等 no-op")
	}
	if ok, _ := b.AckResult(ctx, k1); !ok {
		t.Fatal("ack")
	}
	if ok, _ := b.AckResult(ctx, k1); ok {
		t.Fatal("重复 ack 应 no-op(恰一次)")
	}
	// 版本+1=新 key,旧 ACK 不销新版
	k2 := ResultKey("t1", "env-1", 2)
	if ok, _ := b.EnqueueResult(ctx, k2, "t1", "addr", "ref2"); !ok {
		t.Fatal("v2 enqueue")
	}
	pend, _ := b.PendingResults(ctx)
	if len(pend) != 1 || pend[0] != k2 {
		t.Fatalf("pending=%v want [%s]", pend, k2)
	}
}

// ---- reconciler ----

// timeout 注记(running+SLA 过)+#15 双集兜底(stray 修+告警事件)+redeliver 候选。
func TestSweep_TimeoutStrayAndRedeliverCandidates(t *testing.T) {
	b, id, ctx := rig(t)
	// running+SLA 已过
	toRunning(t, b, id, ctx, "rid-1")
	if _, err := b.db.Exec(`UPDATE node SET sla_deadline=9000 WHERE node_id='n1'`); err != nil {
		t.Fatal(err)
	}
	// stray:n2 本应随 fan-out 提升,人工制造漏网(pending 且 dep n1 置 done 绕过 fan-out)
	if _, err := b.db.Exec(`UPDATE node SET status='done' WHERE node_id='n1'`); err != nil {
		t.Fatal(err)
	}
	// dispatched 无回执:n5
	if err := b.AddNode(ctx, "n5", "t1", 7, "ready", nil); err != nil {
		t.Fatal(err)
	}
	b.Send(ctx, "n5", 1, "rid-5", "w1", id)
	b.clk.(*clock.Fake).Advance(10 * time.Minute)
	rep, err := b.Sweep(ctx, id, 60) // T_redeliver=60s
	if err != nil {
		t.Fatal(err)
	}
	// n1 已被直写 done(绕原语),timeout 注记应 0(不再 running);stray n2 应被兜底提升
	if rep.StrayPromoted != 1 {
		t.Fatalf("stray promoted=%d want 1", rep.StrayPromoted)
	}
	if s, _ := b.GetNode(ctx, "n2"); s.Status != "ready" {
		t.Fatalf("n2 未被兜底提升:%+v", s)
	}
	found := false
	for _, c := range rep.RedeliverCandidates {
		if c == "n5" {
			found = true
		}
	}
	if !found {
		t.Fatalf("redeliver 候选缺 n5:%v", rep.RedeliverCandidates)
	}
	// 告警语义:兜底事件带 invariant15_stray 标记(主路径缺陷可观测)
	if n, _ := b.Events(ctx, "n2", "node_ready"); n != 1 {
		t.Fatalf("兜底 node_ready=%d", n)
	}
}

// timeout 注记路径单测(running+SLA 过→annotate;terminal 到→正常终结+注记清=V13)。
func TestSweep_TimeoutAnnotateThenTerminalClears(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	if _, err := b.db.Exec(`UPDATE node SET sla_deadline=9000 WHERE node_id='n1'`); err != nil {
		t.Fatal(err)
	}
	rep, err := b.Sweep(ctx, id, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TimeoutAnnotated != 1 {
		t.Fatalf("timeout=%d want 1", rep.TimeoutAnnotated)
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "running" || s.BlockerReason.String != "timeout" {
		t.Fatalf("注记型不得迁移:%+v", s)
	}
	// V13:真实 terminal 正常收敛+注记清
	if oc, _ := b.Terminal(ctx, "n1", 1, "rid-1", true, "", id); oc != OutcomeApplied {
		t.Fatalf("terminal=%s", oc)
	}
	s, _ = b.GetNode(ctx, "n1")
	if s.Status != "done" || s.BlockerReason.Valid {
		t.Fatalf("V13 违例:%+v", s)
	}
}

// ---- 崩溃恢复:文件库重开,预留/账全在,sweep 收敛 ----

func TestCrashRecovery_ReopenAndConverge(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "board.db")
	fc := &clock.Fake{T: time.Unix(10_000, 0)}
	b, err := Open(dbPath, fc)
	if err != nil {
		t.Fatal(err)
	}
	epoch, _ := b.Takeover(ctx, "board1", "orch1", 3600)
	id := Identity{Owner: "orch1", Epoch: epoch}
	b.CreateTask(ctx, "t1", "board1", "x")
	b.RegisterAgent(ctx, "w0", "claude")
	b.AddNode(ctx, "n1", "t1", 1, "ready", nil)
	b.Send(ctx, "n1", 1, "rid-1", "w0", id)
	b.Processing(ctx, "n1", 1, "rid-1", id)
	if oc, _ := b.SteerReserve(ctx, "n1", 1, "steer-1", 60, id); oc != "pending" {
		t.Fatal("reserve")
	}
	b.Close() // 崩溃点:预留 pending 在盘

	fc2 := &clock.Fake{T: time.Unix(10_000+3600, 0)} // 重启于 1h 后
	b2, err := Open(dbPath, fc2)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()
	// 新任接管(epoch+1);旧身份被 fence
	e2, _ := b2.Takeover(ctx, "board1", "orch2", 3600)
	id2 := Identity{Owner: "orch2", Epoch: e2}
	if oc, _ := b2.Terminal(ctx, "n1", 1, "rid-1", true, "", id); oc != OutcomeCASMiss {
		t.Fatalf("旧 epoch 应被拒:%s", oc)
	}
	// 预留行=恢复凭据:仍拦 terminal;sweep 降级 pending→uncertain 仍拦;终局 FENCED→rejected 后收敛
	if oc, _ := b2.Terminal(ctx, "n1", 1, "rid-1", true, "", id2); oc != OutcomeCASMiss {
		t.Fatalf("预留应仍拦:%s", oc)
	}
	rep, _ := b2.Sweep(ctx, id2, 3600)
	if rep.ExpiredReservations != 1 {
		t.Fatalf("expired=%d", rep.ExpiredReservations)
	}
	if oc, _ := b2.SteerFinalize(ctx, "steer-1", false, id2); oc != OutcomeRejected {
		t.Fatalf("finalize=%s", oc)
	}
	if oc, _ := b2.Terminal(ctx, "n1", 1, "rid-1", true, "", id2); oc != OutcomeApplied {
		t.Fatalf("恢复后 terminal=%s", oc)
	}
}
