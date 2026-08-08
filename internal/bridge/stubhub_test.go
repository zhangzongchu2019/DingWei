package bridge

// BR-01~14 可执行用例(冻结骨架=docs/bridge-contract-face-v0.1.md §三,d56cf15d)。
// 测试名=BR 编号;判据逐字落断言。真 hub 实现(C-P0-2/3b)须通过同一套测试(conformance)。

import (
	"context"
	"testing"
	"time"
)

func deliver(id string) Delivery {
	return Delivery{DeliveryID: id, RequestID: "rid-" + id, To: "zzc-worker-a-2642",
		Body: []byte(`{"kind":"mcs_dispatch_v1"}`)}
}

func drain(t *testing.T, s *Stub, n int) []Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out []Event
	for len(out) < n {
		e, err := s.Next(ctx)
		if err != nil {
			t.Fatalf("Next: %v(已收 %d/%d)", err, len(out), n)
		}
		out = append(out, e)
	}
	return out
}

// BR-01:Connect→Close(shutdown),事件流见 conn 事件含 close_reason。
func TestBR01_ConnectCloseWithReason(t *testing.T) {
	s := NewStub(nil)
	if err := s.Close(context.Background(), CloseShutdown); err != nil {
		t.Fatal(err)
	}
	ev := drain(t, s, 1)[0]
	if ev.Kind != EvConnState || CloseReason(ev.Ref) != CloseShutdown {
		t.Fatalf("conn 事件=%+v want close_reason=shutdown", ev)
	}
	if s.Connected() {
		t.Fatal("close 后仍 connected")
	}
}

// BR-02:driver health unknown(挂死不可知)时 WS 不关,Close(idle) 被拒(O3a/O3c)。
func TestBR02_IdleCloseRejectedWhenHealthUnknown(t *testing.T) {
	s := NewStub(nil)
	s.SetHealth(HealthUnknown, "探测不到")
	err := s.Close(context.Background(), CloseIdle)
	if err == nil {
		t.Fatal("health unknown 时 idle close 应拒")
	}
	if be, ok := err.(*Error); !ok || be.Class != ErrProtocolReject {
		t.Fatalf("err=%v want protocol_reject", err)
	}
	if !s.Connected() {
		t.Fatal("WS 被关:driver 挂死≠WS 关(O3a)")
	}
}

// BR-03:Deliver 受理→ack 链走到 injected→processed。
func TestBR03_DeliverAckChainToProcessed(t *testing.T) {
	s := NewStub(nil)
	oc := s.Deliver(context.Background(), deliver("d1"))
	if !oc.Accepted || oc.Err != nil {
		t.Fatalf("outcome=%+v", oc)
	}
	if got := s.AckChain("d1"); got != EvProcessed {
		t.Fatalf("ack 链=%q want processed", got)
	}
	evs := drain(t, s, 2)
	if evs[0].Kind != EvInjected || evs[1].Kind != EvProcessed {
		t.Fatalf("事件序=%v,%v want injected,processed", evs[0].Kind, evs[1].Kind)
	}
}

// BR-04:离线→offline(可重试);协议拒(缺 rid)→protocol_reject(不可重试)。
func TestBR04_ErrorClassAndRetryable(t *testing.T) {
	s := NewStub(nil)
	_ = s.Close(context.Background(), CloseShutdown)
	oc := s.Deliver(context.Background(), deliver("d1"))
	if oc.Accepted || oc.Err.Class != ErrOffline || !oc.Err.Retryable() {
		t.Fatalf("offline 判定错:%+v", oc)
	}
	s2 := NewStub(nil)
	bad := deliver("d2")
	bad.RequestID = ""
	oc2 := s2.Deliver(context.Background(), bad)
	if oc2.Accepted || oc2.Err.Class != ErrProtocolReject || oc2.Err.Retryable() {
		t.Fatalf("protocol_reject 判定错:%+v", oc2)
	}
}

// BR-05:事件 Seq 单调;Ack 水位后重连不重放已确认事件。
func TestBR05_SeqMonotonicAckNoReplay(t *testing.T) {
	s := NewStub(nil)
	s.Deliver(context.Background(), deliver("d1"))
	evs := drain(t, s, 2)
	if !(evs[0].Seq < evs[1].Seq) {
		t.Fatalf("Seq 不单调:%d,%d", evs[0].Seq, evs[1].Seq)
	}
	if err := s.Ack(context.Background(), evs[1].Seq); err != nil {
		t.Fatal(err)
	}
	_ = s.Connect(context.Background()) // 重连
	s.Deliver(context.Background(), deliver("d2"))
	next := drain(t, s, 1)[0]
	if next.Seq <= evs[1].Seq {
		t.Fatalf("已 ack 事件被重放:seq=%d", next.Seq)
	}
}

// BR-06:未 ack 断连重连→重放(at-least-once);消费侧按 Seq 去重恰一。
func TestBR06_UnackedReplayedAfterReconnect(t *testing.T) {
	s := NewStub(nil)
	s.Deliver(context.Background(), deliver("d1"))
	first := drain(t, s, 2) // 读了但不 ack
	_ = s.Close(context.Background(), ClosePeerGone)
	_ = s.Connect(context.Background())
	replay := drain(t, s, 2)
	seen := map[uint64]int{}
	for _, e := range append(first, replay...) {
		if e.Kind != EvConnState {
			seen[e.Seq]++
		}
	}
	for seq, n := range seen {
		if n != 2 {
			t.Fatalf("seq=%d 出现 %d 次 want 2(重放)", seq, n)
		}
	}
	// 按 Seq 去重后恰一
	if len(seen) != 2 {
		t.Fatalf("去重后=%d want 2", len(seen))
	}
}

// BR-07:Interrupt 已知 delivery→applied;未知目标→protocol_reject;空目标→protocol_reject。
func TestBR07_InterruptTargets(t *testing.T) {
	s := NewStub(nil)
	s.Deliver(context.Background(), deliver("d1"))
	if oc := s.Interrupt(context.Background(), InterruptTarget{DeliveryID: "d1"}); !oc.Applied {
		t.Fatalf("已知目标应 applied:%+v", oc)
	}
	if oc := s.Interrupt(context.Background(), InterruptTarget{DeliveryID: "ghost"}); oc.Applied ||
		oc.Err.Class != ErrProtocolReject {
		t.Fatalf("未知目标应 protocol_reject:%+v", oc)
	}
	if oc := s.Interrupt(context.Background(), InterruptTarget{}); oc.Applied {
		t.Fatal("空目标应拒")
	}
}

// BR-08:五类 ErrClass 的 Retryable 判定表全对。
func TestBR08_RetryableTable(t *testing.T) {
	want := map[ErrClass]bool{
		ErrOffline: true, ErrTimeout: true, ErrBackpressure: true,
		ErrProtocolReject: false, ErrInternal: false,
	}
	for class, retryable := range want {
		e := &Error{Class: class}
		if e.Retryable() != retryable {
			t.Fatalf("%s retryable=%v want %v", class, e.Retryable(), retryable)
		}
	}
}

// BR-09:WS 断开不推断 driver 死;重连后事件续流。
func TestBR09_WSDownDriverNotDead(t *testing.T) {
	s := NewStub(nil)
	_ = s.Close(context.Background(), ClosePeerGone)
	if h := s.Health(context.Background()); h.Status != HealthOK {
		t.Fatalf("WS 断→driver 被判 %s(禁推断,O3a)", h.Status)
	}
	_ = s.Connect(context.Background())
	s.Deliver(context.Background(), deliver("d1"))
	evs := drain(t, s, 3) // conn 事件+injected+processed
	var got int
	for _, e := range evs {
		if e.Kind == EvInjected || e.Kind == EvProcessed {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("重连后事件未续流:%d", got)
	}
}

// BR-10:health 三态;探测不到返 unknown,连接不受牵连。
func TestBR10_HealthThreeStates(t *testing.T) {
	s := NewStub(nil)
	for _, st := range []HealthStatus{HealthOK, HealthDegraded, HealthUnknown} {
		s.SetHealth(st, "")
		if h := s.Health(context.Background()); h.Status != st {
			t.Fatalf("health=%s want %s", h.Status, st)
		}
	}
	if !s.Connected() {
		t.Fatal("health unknown 不得牵连连接")
	}
}

// BR-11:health 非 degraded(含 ok/unknown=端点不可用)时 idle 关闭不触发;degraded 才允许。
func TestBR11_IdleOnlyWithDegradedHealth(t *testing.T) {
	s := NewStub(nil) // health=ok
	if err := s.Close(context.Background(), CloseIdle); err == nil {
		t.Fatal("health=ok 时 idle close 应拒(无 idle 依据)")
	}
	s.SetHealth(HealthDegraded, "driver 无响应")
	if err := s.Close(context.Background(), CloseIdle); err != nil {
		t.Fatalf("degraded 时 idle close 应允许:%v", err)
	}
	if r := s.CloseReasonSeen(); r != CloseIdle {
		t.Fatalf("close_reason=%s want idle", r)
	}
}

// BR-13:ack 链三段(""→injected→processed)可断言;drop 故障停留 ""。
func TestBR13_AckChainAssertable(t *testing.T) {
	s := NewStub(nil)
	if got := s.AckChain("never"); got != "" {
		t.Fatalf("未投递应=空,got %q", got)
	}
	s.Inject(FaultDrop, "d1")
	oc := s.Deliver(context.Background(), deliver("d1"))
	if !oc.Accepted {
		t.Fatalf("drop=hub 受理但对端不收:%+v", oc)
	}
	if got := s.AckChain("d1"); got != "" {
		t.Fatalf("drop 后 ack 链=%q want 空(诚实条款:受理≠送达)", got)
	}
	s.Deliver(context.Background(), deliver("d2"))
	if got := s.AckChain("d2"); got != EvProcessed {
		t.Fatalf("正常链=%q want processed", got)
	}
}

// BR-14:五种故障逐一注入,行为确定(同脚本两次跑事件模式一致)。
func TestBR14_FaultsDeterministic(t *testing.T) {
	run := func() []EventKind {
		s := NewStub(nil)
		s.Inject(FaultBackpressure, "d1")
		s.Inject(FaultDuplicate, "d2")
		s.Inject(FaultReorder, "d3")
		s.Inject(FaultDrop, "d4")
		if oc := s.Deliver(context.Background(), deliver("d1")); oc.Err == nil ||
			oc.Err.Class != ErrBackpressure {
			t.Fatalf("backpressure:%+v", oc)
		}
		s.Deliver(context.Background(), deliver("d2")) // duplicate→4 事件
		s.Deliver(context.Background(), deliver("d3")) // reorder→2 事件(倒序)
		s.Deliver(context.Background(), deliver("d4")) // drop→0 事件
		var kinds []EventKind
		for _, e := range drain(t, s, 6) {
			kinds = append(kinds, e.Kind)
		}
		// disconnect 故障单验(影响连接态)
		s2 := NewStub(nil)
		s2.Inject(FaultDisconnect, "d5")
		if oc := s2.Deliver(context.Background(), deliver("d5")); oc.Err == nil ||
			oc.Err.Class != ErrOffline {
			t.Fatalf("disconnect:%+v", oc)
		}
		if s2.Connected() {
			t.Fatal("disconnect 故障后应断连")
		}
		return kinds
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("两跑长度不一:%d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("第 %d 事件不定:%s vs %s", i, a[i], b[i])
		}
	}
	// reorder 模式钉死:d3 的两事件=processed 在 injected 前
	if a[4] != EvProcessed || a[5] != EvInjected {
		t.Fatalf("reorder 模式=%v,%v want processed,injected", a[4], a[5])
	}
}
