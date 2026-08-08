// Package dea —— P3 冻结 Driver 执行权威接口(DEA)签名(M0 冻结件 §3.7,两签@6f1b071 家族;
// 真驱动实现与 Q1-Q4 能力实测留 P4,本包只含接口+内存 Stub)。
//
// DEA=外部副作用的唯一事实源。签名照抄 §3.7 接口三原语,verdict=三态
// {APPLIED | NOT_APPLIED_FENCED | UNKNOWN}(五核#1;criteria §5③ 的
// "NOT_APPLIED|FENCED" 写法是同一终局态的缩写,以冻结件三态为准)。
//
// 语义规则(§3.7 规则1-4,调用方义务):
//  1. "rid 执行了没有"的结论只能来自 execute 返回/query/可验证 receipt,禁由超时、
//     rowcount=0、缺回执推断;
//  2. rejected/释放安全栅栏仅接受 NOT_APPLIED_FENCED(TerminalForRelease);UNKNOWN 恒
//     uncertain 不放行;
//  3. fail-closed:driver 无 query/fence 能力(Capabilities 格未置✓)→ reservation 类 op
//     只有 applied 或 uncertain→blocked/manual,不存在自动 rejected;
//  4. 四 op 引用:send=Execute/steer=reserve→Execute→Query·receipt finalize/
//     interrupt=intent→Execute→Query·receipt·Fence finalize/result 回投=Execute(ACK≡APPLIED)。
package dea

import (
	"context"
	"errors"
	"sync"
)

// Verdict query/fence 判定域(★冻结三态,§3.7)。
type Verdict string

const (
	// Applied 确证已产生外部副作用。
	Applied Verdict = "APPLIED"
	// NotAppliedFenced 终局未执行且该 rid 未来也不能执行(唯一的释放依据,规则2)。
	NotAppliedFenced Verdict = "NOT_APPLIED_FENCED"
	// Unknown 当前给不出终局(含"未查到")——恒 uncertain,不放行。
	Unknown Verdict = "UNKNOWN"
)

// TerminalForRelease 规则2 的唯一判据:只有 NOT_APPLIED_FENCED 允许 rejected/释放栅栏。
func TerminalForRelease(v Verdict) bool { return v == NotAppliedFenced }

// Capabilities per-driver 能力登记(§3.7:各格默认【待验】=false;置✓唯一路径=Q1/Q2/Q3 实测,P4)。
// 格为 false 时对应原语不可用(ErrNoCapability),调用方按规则3 fail-closed。
type Capabilities struct {
	ExecuteIdempotent bool // Q1:execute 执行点幂等(同 rid 恰一次外部副作用)
	Query             bool // Q2:权威三态查询
	Fence             bool // Q3:原子取消(拒该 rid+确认未执行,终局)
}

// 错误域(冻结:能力缺失与执行边界拒绝是两类不同事实)。
var (
	// ErrNoCapability 该 driver 能力格未置✓(规则3:调用方 fail-closed,不得推断绕过)。
	ErrNoCapability = errors.New("mcs/dea: capability not verified (fail-closed, §3.7 规则3)")
	// ErrFenced 执行边界拒绝该 rid(fence 生效后 execute 到达=被拒,零外部副作用)。
	ErrFenced = errors.New("mcs/dea: rid fenced — execution refused")
)

// Authority Driver 执行权威接口(★冻结签名,§3.7 三原语)。
type Authority interface {
	// Execute 执行点幂等:同 rid 恰一次外部副作用;fence 过的 rid → ErrFenced 零副作用。
	Execute(ctx context.Context, rid string, payload []byte) error
	// Query 权威幂等查询(三态;能力未置✓→ErrNoCapability)。
	Query(ctx context.Context, rid string) (Verdict, error)
	// Fence 原子取消:成立→NOT_APPLIED_FENCED(该 rid 未来也不能执行,幂等);
	// 已执行→APPLIED(fence 不成立,如实报终局,不谎报未执行);能力未置✓→ErrNoCapability。
	Fence(ctx context.Context, rid string) (Verdict, error)
	// Caps 能力登记快照(调用方据此走规则3 fail-closed 分支)。
	Caps() Capabilities
}

// Stub 内存权威(P3 集成测试/P4 sim 台用;全能力默认开——测 fail-closed 用 SetCaps 关格)。
// 并发安全;side effect 用计数模拟(SideEffects 断言"恰一次")。
type Stub struct {
	mu      sync.Mutex
	caps    Capabilities
	applied map[string]bool
	fenced  map[string]bool
	effects map[string]int // rid → 实际外部副作用次数(幂等断言用)
}

// NewStub 全能力开的内存权威。
func NewStub() *Stub {
	return &Stub{
		caps:    Capabilities{ExecuteIdempotent: true, Query: true, Fence: true},
		applied: map[string]bool{},
		fenced:  map[string]bool{},
		effects: map[string]int{},
	}
}

// SetCaps 覆写能力登记(测规则3 fail-closed 分支)。
func (s *Stub) SetCaps(c Capabilities) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caps = c
}

func (s *Stub) Caps() Capabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps
}

func (s *Stub) Execute(ctx context.Context, rid string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fenced[rid] {
		return ErrFenced // fence 终局性:该 rid 未来也不能执行
	}
	if s.applied[rid] {
		return nil // 执行点幂等:恰一次副作用,重投 no-op
	}
	s.applied[rid] = true
	s.effects[rid]++
	return nil
}

func (s *Stub) Query(ctx context.Context, rid string) (Verdict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.caps.Query {
		return Unknown, ErrNoCapability
	}
	switch {
	case s.applied[rid]:
		return Applied, nil
	case s.fenced[rid]:
		return NotAppliedFenced, nil
	default:
		return Unknown, nil // "未查到"不是终局(四核#1)
	}
}

func (s *Stub) Fence(ctx context.Context, rid string) (Verdict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.caps.Fence {
		return Unknown, ErrNoCapability
	}
	if s.applied[rid] {
		return Applied, nil // 已执行:fence 不成立,如实报终局(不谎报"未执行")
	}
	s.fenced[rid] = true // 幂等:重复 fence 仍 NOT_APPLIED_FENCED
	return NotAppliedFenced, nil
}

// SideEffects rid 的实际外部副作用次数(测试断言 Q1"恰一次"形态)。
func (s *Stub) SideEffects(rid string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effects[rid]
}

// 编译期断言:Stub 实现冻结接口。
var _ Authority = (*Stub)(nil)
