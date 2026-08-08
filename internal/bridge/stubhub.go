// stubhub.go —— C-P0-3a:StubHub 参考实现(契约 §2.3 conformance kit,BR 用例的可执行载体)。
// 真 hub 实现(C-P0-2/3b)与本 stub 跑同一套契约测试;E门夹具直接消费本件(O4,禁另起)。
// 确定性:注入 Clock,故障脚本按 Inject 调用序逐次消耗,无真实网络/时间依赖。
package bridge

import (
	"context"
	"sync"
	"time"
)

// FakeClock 确定性时钟(契约 §1.5)。
type FakeClock struct {
	mu sync.Mutex
	T  time.Time
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.T
}

// Advance 前进 d。
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.T = c.T.Add(d)
	c.mu.Unlock()
}

type faultStep struct {
	op  FaultOp
	ref string
}

// Stub 实现 StubHub。零真实 I/O;事件流 at-least-once:Ack 水位前的事件不再重放,
// 断连(FaultDisconnect)后游标回退到水位=未 ack 事件重放(BR-06)。
type Stub struct {
	Clock Clock

	mu        sync.Mutex
	notify    chan struct{}
	connected bool
	closedFor CloseReason
	seq       uint64
	events    []Event // 全量留存;cursor/acked 定读窗
	cursor    int     // 下一次 Next 读 events[cursor]
	acked     uint64  // Ack 水位(≤acked 不再重放)
	chain     map[string]EventKind
	faults    []faultStep
	health    HealthReport
}

// NewStub 起一个已连接、health=ok 的 stub。
func NewStub(clk Clock) *Stub {
	if clk == nil {
		clk = &FakeClock{T: time.Unix(10_000, 0)}
	}
	return &Stub{
		Clock:     clk,
		notify:    make(chan struct{}, 1),
		connected: true,
		chain:     map[string]EventKind{},
		health:    HealthReport{Status: HealthOK, ObservedAt: clk.Now()},
	}
}

// ---- Conn ----

// Connect 重连:游标回退到 Ack 水位(未 ack 事件重放,at-least-once)。
func (s *Stub) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = true
	s.closedFor = ""
	s.cursor = s.replayStartLocked()
	s.wake()
	return nil
}

// Close 关闭并记 close_reason(conn 事件入流,BR-01)。
func (s *Stub) Close(_ context.Context, reason CloseReason) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// O3c:idle 关闭仅当 driver health 可用且非 ok 时允许
	if reason == CloseIdle && s.health.Status != HealthDegraded {
		return &Error{Class: ErrProtocolReject,
			Reason: "idle close 仅在 driver health 可用且 degraded 时允许(O3c)"}
	}
	s.connected = false
	s.closedFor = reason
	s.emitLocked(EvConnState, string(reason), []byte(reason))
	return nil
}

// replayStartLocked 返回水位后的首个事件下标。
func (s *Stub) replayStartLocked() int {
	for i, e := range s.events {
		if e.Seq > s.acked {
			return i
		}
	}
	return len(s.events)
}

// ---- Deliverer ----

func (s *Stub) Deliver(_ context.Context, d Delivery) DeliverOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.takeFaultLocked(FaultDisconnect, d.DeliveryID); ok {
		_ = f
		s.connected = false
		s.closedFor = ClosePeerGone
		return DeliverOutcome{DeliveryID: d.DeliveryID,
			Err: &Error{Class: ErrOffline, Reason: "fault: disconnect"}}
	}
	if !s.connected {
		return DeliverOutcome{DeliveryID: d.DeliveryID,
			Err: &Error{Class: ErrOffline, Reason: "not connected"}}
	}
	if _, ok := s.takeFaultLocked(FaultBackpressure, d.DeliveryID); ok {
		return DeliverOutcome{DeliveryID: d.DeliveryID,
			Err: &Error{Class: ErrBackpressure, Reason: "fault: backpressure"}}
	}
	if d.To == "" || d.RequestID == "" {
		return DeliverOutcome{DeliveryID: d.DeliveryID,
			Err: &Error{Class: ErrProtocolReject, Reason: "缺 to/rid(收发人必填)"}}
	}
	if _, ok := s.takeFaultLocked(FaultDrop, d.DeliveryID); ok {
		// hub 受理但对端永不收:ack 链停留 ""(诚实条款可观测,BR-03 反面)
		s.chain[d.DeliveryID] = ""
		return DeliverOutcome{DeliveryID: d.DeliveryID, Accepted: true}
	}
	if _, ok := s.takeFaultLocked(FaultReorder, d.DeliveryID); ok {
		s.emitLocked(EvProcessed, d.DeliveryID, nil)
		s.emitLocked(EvInjected, d.DeliveryID, nil)
		s.chain[d.DeliveryID] = EvProcessed
		return DeliverOutcome{DeliveryID: d.DeliveryID, Accepted: true}
	}
	dup := 1
	if _, ok := s.takeFaultLocked(FaultDuplicate, d.DeliveryID); ok {
		dup = 2
	}
	for i := 0; i < dup; i++ {
		s.emitLocked(EvInjected, d.DeliveryID, nil)
		s.chain[d.DeliveryID] = EvInjected // ack 链两段推进可观测(BR-13)
		s.emitLocked(EvProcessed, d.DeliveryID, nil)
		s.chain[d.DeliveryID] = EvProcessed
	}
	return DeliverOutcome{DeliveryID: d.DeliveryID, Accepted: true}
}

// ---- EventStream ----

func (s *Stub) Next(ctx context.Context) (Event, error) {
	for {
		s.mu.Lock()
		if s.cursor < len(s.events) {
			e := s.events[s.cursor]
			s.cursor++
			s.mu.Unlock()
			return e, nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-s.notify:
		}
	}
}

func (s *Stub) Ack(_ context.Context, seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq > s.acked {
		s.acked = seq
	}
	return nil
}

// ---- Interrupter ----

func (s *Stub) Interrupt(_ context.Context, t InterruptTarget) InterruptOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.DeliveryID == "" && t.TurnID == "" {
		return InterruptOutcome{Err: &Error{Class: ErrProtocolReject, Reason: "空目标"}}
	}
	if t.DeliveryID != "" {
		if _, ok := s.chain[t.DeliveryID]; !ok {
			return InterruptOutcome{Err: &Error{Class: ErrProtocolReject,
				Reason: "目标 delivery 不存在"}}
		}
	}
	return InterruptOutcome{Applied: true}
}

// ---- DriverHealth ----

func (s *Stub) Health(context.Context) HealthReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.health
}

// SetHealth 测试注入 driver 健康(O3b:与连接状态无关)。
func (s *Stub) SetHealth(st HealthStatus, detail string) {
	s.mu.Lock()
	s.health = HealthReport{Status: st, Detail: detail, ObservedAt: s.Clock.Now()}
	s.mu.Unlock()
}

// ---- kit 面 ----

func (s *Stub) Inject(op FaultOp, ref string) {
	s.mu.Lock()
	s.faults = append(s.faults, faultStep{op: op, ref: ref})
	s.mu.Unlock()
}

func (s *Stub) AckChain(deliveryID string) EventKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chain[deliveryID]
}

// Connected 观测(BR-02/09:WS 状态与 driver 状态互不推断)。
func (s *Stub) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

// CloseReasonSeen 观测最近一次关闭原因。
func (s *Stub) CloseReasonSeen() CloseReason {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closedFor
}

// ---- 内部 ----

func (s *Stub) takeFaultLocked(op FaultOp, ref string) (faultStep, bool) {
	for i, f := range s.faults {
		if f.op == op && (f.ref == "" || f.ref == ref) {
			s.faults = append(s.faults[:i], s.faults[i+1:]...)
			return f, true
		}
	}
	return faultStep{}, false
}

func (s *Stub) emitLocked(kind EventKind, ref string, body []byte) {
	s.seq++
	s.events = append(s.events, Event{
		Seq: s.seq, Kind: kind, Ref: ref, Body: body, At: s.Clock.Now(),
	})
	s.wake()
}

func (s *Stub) wake() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// 编译期断言:Stub 满足 StubHub 契约面。
var _ StubHub = (*Stub)(nil)
