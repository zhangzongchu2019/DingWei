package hubdriver

import (
	"context"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
)

func boardRig(t *testing.T) (*board.Board, board.Identity, context.Context) {
	t.Helper()
	ctx := context.Background()
	b, err := board.Open(":memory:", &clock.Fake{T: time.Unix(10_000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	epoch, err := b.Takeover(ctx, "board1", "orch1", 3600)
	if err != nil {
		t.Fatal(err)
	}
	id := board.Identity{Owner: "orch1", Epoch: epoch}
	rb := func(w string) board.RoleBinding { return board.RoleBinding{Who: w, CLI: "claude", LLM: "claude-opus-4-8"} }
	roles := board.RoleAssignments{Designer: rb("devo48"), Implementer: rb("devo48"), Tester: rb("tester"), QAOwner: rb("shence")}
	if err := b.CreateTask(ctx, "t1", "board1", "任务", roles, id); err != nil {
		t.Fatal(err)
	}
	if err := b.AddNode(ctx, "n1", "t1", 1, "ready", nil); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterAgent(ctx, "w1", "claude"); err != nil {
		t.Fatal(err)
	}
	return b, id, ctx
}

// E2E-lite 正例:Send 记账→processing 回执→done 回执,全 applied。
func TestReceiptE2ELite(t *testing.T) {
	b, id, ctx := boardRig(t)
	if out, err := b.Send(ctx, "n1", 1, "rid-1", "w1", id); err != nil || out != board.OutcomeApplied {
		t.Fatalf("send: %v %v", out, err)
	}
	sink := &ReceiptSink{B: b, ID: id}
	out, err := sink.Handle(ctx, WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n1", Generation: 1, RID: "rid-1", Phase: "processing", From: "w1"})
	if err != nil || out != board.OutcomeApplied {
		t.Fatalf("processing: %v %v", out, err)
	}
	out, err = sink.Handle(ctx, WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n1", Generation: 1, RID: "rid-1", Phase: "done", OK: true, ResultRef: "ref-1", From: "w1"})
	if err != nil || out != board.OutcomeApplied {
		t.Fatalf("done: %v %v", out, err)
	}
	s, err := b.GetNode(ctx, "n1")
	if err != nil || s.Status != "done" {
		t.Fatalf("终态: %+v %v", s, err)
	}
}

// 负例:未知 kind 拒(入口闭合)。
func TestReceiptUnknownKindRejected(t *testing.T) {
	b, id, ctx := boardRig(t)
	sink := &ReceiptSink{B: b, ID: id}
	if out, err := sink.Handle(ctx, WireReceipt{Kind: "evil_v9", NodeID: "n1", RID: "r", Phase: "done"}); err == nil || out != board.OutcomeRejected {
		t.Fatalf("未知 kind 应拒: %v %v", out, err)
	}
}

// 负例:未知 phase 拒(入口闭合,不猜语义)。
func TestReceiptUnknownPhaseRejected(t *testing.T) {
	b, id, ctx := boardRig(t)
	sink := &ReceiptSink{B: b, ID: id}
	if out, err := sink.Handle(ctx, WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n1", RID: "r", Phase: "half-done", From: "w1"}); err == nil || out != board.OutcomeRejected {
		t.Fatalf("未知 phase 应拒: %v %v", out, err)
	}
}

// 负例:缺 rid 拒(幂等键不可缺省)。
func TestReceiptMissingRIDRejected(t *testing.T) {
	b, id, ctx := boardRig(t)
	sink := &ReceiptSink{B: b, ID: id}
	if _, err := sink.Handle(ctx, WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n1", Phase: "done"}); err == nil {
		t.Fatal("缺 rid 应拒")
	}
}

// 负例:错代回执(gen 已换代)→board guard 拦,不入权威账(迟到回执域)。
func TestReceiptStaleGenerationGuarded(t *testing.T) {
	b, id, ctx := boardRig(t)
	if _, err := b.Send(ctx, "n1", 1, "rid-1", "w1", id); err != nil {
		t.Fatal(err)
	}
	sink := &ReceiptSink{B: b, ID: id}
	out, err := sink.Handle(ctx, WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n1", Generation: 99, RID: "rid-1", Phase: "done", OK: true, From: "w1"})
	if err == nil && out == board.OutcomeApplied {
		t.Fatalf("错代回执不得 applied: %v %v", out, err)
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.Status == "done" {
		t.Fatal("错代回执不得改权威终态")
	}
}

// 负例:JSON 损坏→显式解析错。
func TestReceiptBadJSON(t *testing.T) {
	b, id, ctx := boardRig(t)
	sink := &ReceiptSink{B: b, ID: id}
	if _, err := sink.HandleBody(ctx, []byte("{not json")); err == nil {
		t.Fatal("坏 JSON 应显式报错")
	}
}

// 负例(tester G2):回执先于派发记账(rid 从未 Send)→显式拒/不 applied,不改权威态。
// 记账先行不变量(M0 3.1)使正常时序不可能出现此况;出现即异常来路,须显式可见。
func TestReceiptBeforeDispatchRejectedExplicit(t *testing.T) {
	b, id, ctx := boardRig(t)
	sink := &ReceiptSink{B: b, ID: id}
	out, err := sink.Handle(ctx, WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n1", Generation: 1, RID: "rid-ghost", Phase: "done", OK: true, From: "w1"})
	if err == nil && out == board.OutcomeApplied {
		t.Fatalf("未派发 rid 的回执不得 applied: %v %v", out, err)
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.Status == "done" {
		t.Fatal("幽灵回执不得改权威终态")
	}
}

// 负例(张宗楚 07-25 令):回执无明确发件人→拒收。
func TestReceiptMissingFromRejected(t *testing.T) {
	b, id, ctx := boardRig(t)
	sink := &ReceiptSink{B: b, ID: id}
	if _, err := sink.Handle(ctx, WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n1", RID: "r", Phase: "done"}); err == nil {
		t.Fatal("无 from 应拒收")
	}
}

// M2 真信道 fixture:2026-07-25 16:23Z 经真实 DingWei hub 自环回流的原始回执样本
// (派发→hub→工人 wkr.py 消费→回执→hub→本会话,全链真实传输)。字节级保真。
func TestRealHubTransitFixtureParses(t *testing.T) {
	body := []byte(`{"kind": "mcs_receipt_v1", "node_id": "m2-selftest-n1", "generation": 1, "rid": "m2-selftest-rid-001", "phase": "processing", "ok": false, "result_ref": "", "from": "devo48"}`)
	r, err := ParseReceipt(body)
	if err != nil {
		t.Fatalf("真信道样本应可解析: %v", err)
	}
	if r.NodeID != "m2-selftest-n1" || r.RID != "m2-selftest-rid-001" || r.Phase != "processing" {
		t.Fatalf("字段错: %+v", r)
	}
}

// N12(tester wkr.py 审查抓获):回执 JSON 尾部带时戳行(旧版 wkr.py 实际线上格式)
// →首对象解析容忍,字段无损。双侧修:wkr.py 已改纯 JSON 出线,此为纵深防御。
func TestReceiptTrailingTimestampTolerated(t *testing.T) {
	body := []byte("{\"kind\": \"mcs_receipt_v1\", \"node_id\": \"n1\", \"generation\": 1, \"rid\": \"r-1\", \"phase\": \"done\", \"ok\": true, \"result_ref\": \"ref\", \"from\": \"w1\"}\n[2026-07-26 04:20 GMT]\n")
	r, err := ParseReceipt(body)
	if err != nil {
		t.Fatalf("尾部时戳应被容忍: %v", err)
	}
	if r.NodeID != "n1" || r.Phase != "done" || !r.OK {
		t.Fatalf("字段损: %+v", r)
	}
}
