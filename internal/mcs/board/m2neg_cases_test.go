package board

// M2 负例组用例对号实测(board 面):tester 冻结件 docs/contract-cases/m2-negative-cases.md
// (blob 0e029ffd@43dfd5ef,sha256 头 1254868e)。测试名=TC 编号,判据逐字落断言。
// N1a/N1c(需真 worker 链)在 mcsd 包;N4/N5a 工人侧半边在 m2neg_wkr_check.py。

import (
	"context"
	"strings"
	"testing"
)

// TC-N1b:budget 耗尽→DeliverFail(deaTerminal=true)→failed(delivery_exhausted)。
func TestTCN1b_DeliveryBudgetExhaustedToFailed(t *testing.T) {
	b, id, ctx := rig(t)
	mustOutcome(t, must(b.Send(ctx, "n1", 1, "rid-1", "w0", id)), OutcomeApplied, "send")
	// delivery_attempt 0→3(=max_delivery 默认 3):三次 applied,第四次预算尽=cas_miss
	for i := 0; i < 3; i++ {
		mustOutcome(t, must(b.Redeliver(ctx, "n1", 1, "rid-1", id)), OutcomeApplied, "redeliver")
	}
	mustOutcome(t, must(b.Redeliver(ctx, "n1", 1, "rid-1", id)), OutcomeCASMiss, "redeliver 预算尽")
	mustOutcome(t, must(b.DeliverFail(ctx, "n1", 1, "rid-1", true, id)), OutcomeApplied, "deliver_fail")
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "failed" {
		t.Fatalf("status=%s want failed", s.Status)
	}
	// 判据:reason=delivery_exhausted(事件账为准)
	var data string
	if err := b.db.QueryRowContext(ctx,
		`SELECT data FROM board_event WHERE kind='node_failed' AND node_id='n1'`).Scan(&data); err != nil {
		t.Fatalf("node_failed 事件缺: %v", err)
	}
	if !strings.Contains(data, "delivery_exhausted") {
		t.Fatalf("事件 data=%s 缺 delivery_exhausted", data)
	}
}

// TC-N2a:running 超时→Sweep 注记 timeout,不迁移不 reassign。
func TestTCN2a_RunningTimeoutAnnotateOnly(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	if _, err := b.db.ExecContext(ctx,
		`UPDATE node SET sla_deadline=? WHERE node_id='n1'`, b.now()-1); err != nil {
		t.Fatal(err)
	}
	rep, err := b.Sweep(ctx, id, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TimeoutAnnotated != 1 {
		t.Fatalf("TimeoutAnnotated=%d want 1", rep.TimeoutAnnotated)
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "running" || s.BlockerReason.String != "timeout" {
		t.Fatalf("want running+timeout 注记,got %+v", s)
	}
	if s.Generation != 1 || s.AssignedAgent.String != "w0" {
		t.Fatalf("不得自动 reassign:%+v", s)
	}
}

// TC-N2b:超时注记后 worker 恢复发 done→node=done,注记同事务清除。
func TestTCN2b_TimeoutThenDoneClearsAnnotation(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	mustOutcome(t, must(b.SetBlocker(ctx, "n1", 1, "timeout", id)), OutcomeApplied, "annotate")
	mustOutcome(t, must(b.Terminal(ctx, "n1", 1, "rid-1", true, "ref://r", id)), OutcomeApplied, "done")
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "done" || s.BlockerReason.Valid {
		t.Fatalf("want done+注记 NULL,got %+v", s)
	}
	if n, _ := b.Events(ctx, "n1", "node_annotation_cleared"); n != 1 {
		t.Fatalf("annotation_cleared 事件=%d want 1(同事务清证)", n)
	}
}

// TC-N3a:done 先于 processing 直达 dispatched(隐式 ack),started_ts COALESCE 补。
func TestTCN3a_DoneDirectFromDispatched(t *testing.T) {
	b, id, ctx := rig(t)
	mustOutcome(t, must(b.Send(ctx, "n1", 1, "rid-1", "w0", id)), OutcomeApplied, "send")
	mustOutcome(t, must(b.Terminal(ctx, "n1", 1, "rid-1", true, "ref://r", id)), OutcomeApplied, "done 直达")
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "done" {
		t.Fatalf("status=%s want done", s.Status)
	}
	var started int64
	if err := b.db.QueryRowContext(ctx,
		`SELECT COALESCE(started_ts,0) FROM node WHERE node_id='n1'`).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if started == 0 {
		t.Fatal("started_ts 未 COALESCE 填充")
	}
}

// TC-N3b:processing 迟到(node 已 done)→cas_miss,零副作用无新事件。
func TestTCN3b_LateProcessingCASMiss(t *testing.T) {
	b, id, ctx := rig(t)
	mustOutcome(t, must(b.Send(ctx, "n1", 1, "rid-1", "w0", id)), OutcomeApplied, "send")
	mustOutcome(t, must(b.Terminal(ctx, "n1", 1, "rid-1", true, "", id)), OutcomeApplied, "done")
	before := eventCount(t, b, ctx)
	mustOutcome(t, must(b.Processing(ctx, "n1", 1, "rid-1", id)), OutcomeCASMiss, "processing 迟到")
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "done" {
		t.Fatalf("status=%s want done(零副作用)", s.Status)
	}
	if after := eventCount(t, b, ctx); after != before {
		t.Fatalf("事件数 %d→%d,迟到 processing 不得写事件", before, after)
	}
}

// TC-N5a(board 切面):同 rid 重复 done 回执→cas_miss 零副作用(执行点恰一的账面半边;
// 工人侧"不重复执行 payload"=会话协议义务,wkr 侧重发 done 见 m2neg_wkr_check.py W3)。
func TestTCN5a_DuplicateDoneReceiptIdempotent(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	mustOutcome(t, must(b.Terminal(ctx, "n1", 1, "rid-1", true, "ref://r", id)), OutcomeApplied, "首 done")
	before := eventCount(t, b, ctx)
	mustOutcome(t, must(b.Terminal(ctx, "n1", 1, "rid-1", true, "ref://r", id)), OutcomeCASMiss, "重复 done")
	if after := eventCount(t, b, ctx); after != before {
		t.Fatalf("重复 done 写了事件(%d→%d)", before, after)
	}
	if n, _ := b.Events(ctx, "n1", "node_done"); n != 1 {
		t.Fatalf("node_done=%d want 1(恰一)", n)
	}
}

// TC-N5b:同 rid 异 payload→idempotency_conflict。当前基线 Send 不携 payload/digest,
// priorOutcome 仅按 rid 重放——契约 v0.3 §1.3 O1(digest 冲突拒写+事件)属 board vNext 实现域。
// 本测试记录基线现状后 Skip,用例保留作 vNext 验收判据(CT-O1-02 同源)。
func TestTCN5b_SameRidDifferentPayloadConflict(t *testing.T) {
	b, id, ctx := rig(t)
	mustOutcome(t, must(b.Send(ctx, "n1", 1, "rid-1", "w0", id)), OutcomeApplied, "send")
	// 基线现状:同 rid 重放(即使"payload 不同"board 也无从见)→返首 outcome applied
	mustOutcome(t, must(b.Send(ctx, "n1", 1, "rid-1", "w1", id)), OutcomeApplied, "同 rid 重放=首 outcome")
	t.Skip("N5b digest 冲突检测未实现:基线 Send 无 payload/digest 参数(O1=board vNext 域)," +
		"契约锚 §1.3@28f09dae,vNext 落地后启用本判据(期望 idempotency_conflict+冲突事件)")
}

// ---- 小工具 ----

func must(oc Outcome, err error) Outcome {
	if err != nil {
		panic(err)
	}
	return oc
}

func eventCount(t *testing.T, b *Board, ctx context.Context) int {
	t.Helper()
	var n int
	if err := b.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM board_event`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
