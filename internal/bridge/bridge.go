// Package bridge 是 C-P0-1 契约面:飞书桥接+hub 拆分的组间接口(Go 面),
// 逐条对应接口契约 v0.3(28f09dae@10701574)§2.2 桥接↔driver 四端点与 §2.3 conformance kit。
// 本包只冻接口与类型,零实现——C-P0-2(WS/driver 解耦)/C-P0-3(hub 独立起停+stub)/
// C-P0-4(飞书 unlock mock)/C-P0-5(集成自测)按此面实现,张宗楚确认细案后推进。
//
// 拆分目标(Type B 根治):WS 连接与 driver 执行独立——driver 挂死≠WS 关(契约 O3a);
// driver 侧 health 端点非仅 WS ping(O3b);idle 超时仅在有 driver health 前提触发(O3c)。
package bridge

import (
	"context"
	"time"
)

// ---- 错误分类(契约 §2.2,集中枚举,消费者禁解析字符串,§1.5) ----

// ErrClass 桥接错误分类。
type ErrClass string

const (
	ErrOffline        ErrClass = "offline"         // 对端不在线/不可达
	ErrTimeout        ErrClass = "timeout"         // 超时(ctx deadline 或对端无响应)
	ErrBackpressure   ErrClass = "backpressure"    // 对端/信道背压,应退避重试
	ErrProtocolReject ErrClass = "protocol_reject" // 线格式/协议层显式拒(不可重试)
	ErrInternal       ErrClass = "internal"        // 桥接内部错误
)

// Error 桥接层统一错误载体(实现须返回本类型,不返回裸 error 字符串)。
type Error struct {
	Class  ErrClass
	Reason string // 人读原因,不作程序判据
}

func (e *Error) Error() string { return string(e.Class) + ": " + e.Reason }

// Retryable 是否可重试:仅 offline/timeout/backpressure 可;protocol_reject/internal 不可。
func (e *Error) Retryable() bool {
	switch e.Class {
	case ErrOffline, ErrTimeout, ErrBackpressure:
		return true
	}
	return false
}

// ---- 端点1:Connect/Close(契约 §2.2) ----

// CloseReason 关闭原因(close_reason,入事件账供取证)。
type CloseReason string

const (
	CloseShutdown   CloseReason = "shutdown"   // 主动停机(hub 独立起停,C-P0-3)
	ClosePeerGone   CloseReason = "peer_gone"  // 对端消失
	CloseProtocol   CloseReason = "protocol"   // 协议错误
	CloseIdle       CloseReason = "idle"       // idle 超时——仅当 driver health 可用时才允许(O3c)
	CloseSuperseded CloseReason = "superseded" // 新连接取代旧连接
)

// Conn 桥接连接生命周期。WS 连接状态与 driver 执行状态互相独立(O3a):
// 实现不得因 driver 无响应而关 WS,也不得因 WS 断而判 driver 死。
type Conn interface {
	Connect(ctx context.Context) error
	Close(ctx context.Context, reason CloseReason) error
}

// ---- 端点2:Deliver(契约 §2.2,delivery/request identity) ----

// Delivery 一次投递(身份二元:DeliveryID=本次投递,RequestID=业务请求;
// 重投同 RequestID 换 DeliveryID,幂等归 board O1 闸,桥接不去重)。
type Delivery struct {
	DeliveryID string
	RequestID  string // = 信封 rid
	To         string // 收件全名(短名拒发,契约0)
	Body       []byte // mcs_dispatch_v1 信封原文(线格式 §2.1,桥接不拆包)
	Deadline   time.Time
}

// DeliverOutcome 投递结果(送达 hub≠送达对端,契约诚实条款;
// 对端收讫以 Receipts 流的 ack 链为准,§2.3 injected→processed)。
type DeliverOutcome struct {
	DeliveryID string
	Accepted   bool   // hub 已受理(非对端已收)
	Err        *Error // Accepted=false 时必填
}

// Deliverer 投递端点。
type Deliverer interface {
	Deliver(ctx context.Context, d Delivery) DeliverOutcome
}

// ---- 端点3:Events/Receipts(契约 §2.2,有序+ack) ----

// EventKind 桥接事件类型(集中枚举)。
type EventKind string

const (
	EvReceipt   EventKind = "receipt"   // mcs_receipt_v1 到达(Body=原文,ParseReceipt 在消费侧)
	EvInjected  EventKind = "injected"  // 投递已注入对端会话(ack 链第一段,§2.3)
	EvProcessed EventKind = "processed" // 对端已处理(ack 链第二段)
	EvConnState EventKind = "conn"      // 连接状态变化(close_reason 附带)
)

// Event 桥接上行事件。Seq 单调(per-conn),消费者以 Ack(seq) 确认;
// 未 ack 的事件在重连后重放(at-least-once,消费侧按 Seq 去重)。
type Event struct {
	Seq  uint64
	Kind EventKind
	Ref  string // DeliveryID/连接标识
	Body []byte
	At   time.Time
}

// EventStream 有序事件流。
type EventStream interface {
	// Next 阻塞取下一事件;ctx 取消返回 ctx.Err()。
	Next(ctx context.Context) (Event, error)
	// Ack 确认已消费到 seq(含);之前的事件不再重放。
	Ack(ctx context.Context, seq uint64) error
}

// ---- 端点4:Interrupt(契约 §2.2,目标 turn/delivery→outcome) ----

// InterruptTarget 中断目标:二选一,零值字段=不限定。
type InterruptTarget struct {
	DeliveryID string
	TurnID     string // 对端会话 turn(hub V2 terminal_interrupt 管道,main a5f7c25 侦察留档)
}

// InterruptOutcome 中断结果(具名,禁裸 bool)。
type InterruptOutcome struct {
	Applied bool
	Err     *Error // Applied=false 时必填
}

// Interrupter 中断端点。
type Interrupter interface {
	Interrupt(ctx context.Context, t InterruptTarget) InterruptOutcome
}

// ---- driver 健康(O3b/O3c,与 WS ping 独立) ----

// HealthStatus driver 健康三态。
type HealthStatus string

const (
	HealthOK       HealthStatus = "ok"
	HealthDegraded HealthStatus = "degraded"
	HealthUnknown  HealthStatus = "unknown" // 探测不到≠死,禁 presence 判活(契约 §3.1)
)

// HealthReport driver 侧健康报告。
type HealthReport struct {
	Status     HealthStatus
	Detail     string
	ObservedAt time.Time
}

// DriverHealth driver 健康端点(非 WS ping;WS 活着但本端点 unknown 时,
// idle 关闭不得触发——O3c:idle timeout 仅在有 driver health 前提下生效)。
type DriverHealth interface {
	Health(ctx context.Context) HealthReport
}

// ---- §2.3 conformance kit 面(owner=信道组随仓供,禁各写一套) ----

// UnlockOutcome mock 飞书 unlock 结果(code→granted/reason,不连真飞书)。
type UnlockOutcome struct {
	Granted bool
	Reason  string
}

// Unlocker 鉴权解锁面(C-P0-4:生产=飞书;测试=mock,同一接口)。
type Unlocker interface {
	Unlock(ctx context.Context, code string) UnlockOutcome
}

// Clock 确定性时钟注入(契约 §1.5 时钟语义:mock 注入 deterministic clock)。
type Clock interface {
	Now() time.Time
}

// FaultOp stub 故障脚本操作(§2.3:脚本化 connect/disconnect+背压/重复/乱序/丢包)。
type FaultOp string

const (
	FaultDisconnect   FaultOp = "disconnect"
	FaultBackpressure FaultOp = "backpressure"
	FaultDuplicate    FaultOp = "duplicate"
	FaultReorder      FaultOp = "reorder"
	FaultDrop         FaultOp = "drop"
)

// StubHub 测试 stub 面:真实现与 stub 跑同一套契约测试(§2.3);
// E门自动化夹具直接复用本面不另起(O4,A 组消费)。
type StubHub interface {
	Deliverer
	EventStream
	Interrupter
	DriverHealth
	// Inject 注入故障脚本步骤(确定性,按调用序生效)。
	Inject(op FaultOp, ref string)
	// AckChain 断言用:返回某 DeliveryID 已走到的 ack 链位置(""/injected/processed)。
	AckChain(deliveryID string) EventKind
}
