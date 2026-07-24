package dea

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// ★签名冻结:三态字符串值逐字=M0 §3.7(重命名/加值=破冻结,本测试即门)。
func TestDEA_VerdictDomainFrozen(t *testing.T) {
	if Applied != "APPLIED" || NotAppliedFenced != "NOT_APPLIED_FENCED" || Unknown != "UNKNOWN" {
		t.Fatalf("verdict 域漂移:%q/%q/%q", Applied, NotAppliedFenced, Unknown)
	}
}

// ★规则2:释放栅栏唯一判据=NOT_APPLIED_FENCED;APPLIED/UNKNOWN 均不放行。
func TestDEA_ReleaseOnlyOnFenced(t *testing.T) {
	if !TerminalForRelease(NotAppliedFenced) {
		t.Fatal("NOT_APPLIED_FENCED 应允许释放")
	}
	for _, v := range []Verdict{Applied, Unknown, Verdict("")} {
		if TerminalForRelease(v) {
			t.Fatalf("%q 不得作为释放依据(规则2)", v)
		}
	}
}

// Q1 形态:execute 执行点幂等——并发+重投同 rid,外部副作用恰一次。
func TestDEA_ExecuteIdempotentExactlyOnce(t *testing.T) {
	s := NewStub()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Execute(ctx, "r1", nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := s.SideEffects("r1"); n != 1 {
		t.Fatalf("同 rid 并发+重投副作用应恰一次,got %d", n)
	}
}

// Q2 形态:query 三态——未查到=UNKNOWN(非终局)/已执行=APPLIED/已 fence=NOT_APPLIED_FENCED。
func TestDEA_QueryThreeStates(t *testing.T) {
	s := NewStub()
	ctx := context.Background()
	if v, _ := s.Query(ctx, "never"); v != Unknown {
		t.Fatalf("未查到应 UNKNOWN(不是终局),got %s", v)
	}
	s.Execute(ctx, "ra", nil)
	if v, _ := s.Query(ctx, "ra"); v != Applied {
		t.Fatalf("已执行应 APPLIED,got %s", v)
	}
	s.Fence(ctx, "rf")
	if v, _ := s.Query(ctx, "rf"); v != NotAppliedFenced {
		t.Fatalf("已 fence 应 NOT_APPLIED_FENCED,got %s", v)
	}
}

// Q3 形态:fence 原子终局——fence 后 execute 被拒零副作用(该 rid 未来也不能执行);fence 幂等。
func TestDEA_FenceTerminal(t *testing.T) {
	s := NewStub()
	ctx := context.Background()
	if v, err := s.Fence(ctx, "r2"); err != nil || v != NotAppliedFenced {
		t.Fatalf("fence 应成立 NOT_APPLIED_FENCED,got %s/%v", v, err)
	}
	if err := s.Execute(ctx, "r2", nil); !errors.Is(err, ErrFenced) {
		t.Fatalf("fence 后 execute 应 ErrFenced,got %v", err)
	}
	if n := s.SideEffects("r2"); n != 0 {
		t.Fatalf("fence 后迟到执行应零副作用,got %d", n)
	}
	if v, _ := s.Fence(ctx, "r2"); v != NotAppliedFenced {
		t.Fatal("fence 应幂等")
	}
}

// ★已执行后 fence 不成立:如实报 APPLIED(不谎报"终局未执行"——五核#1:rejected 唯一前置
// =终局 NOT_APPLIED_FENCED,本例的 APPLIED 不得走 rejected)。
func TestDEA_FenceAfterExecuteReportsApplied(t *testing.T) {
	s := NewStub()
	ctx := context.Background()
	s.Execute(ctx, "r3", nil)
	v, err := s.Fence(ctx, "r3")
	if err != nil || v != Applied {
		t.Fatalf("已执行后 fence 应报 APPLIED,got %s/%v", v, err)
	}
	if TerminalForRelease(v) {
		t.Fatal("APPLIED 不得作为释放依据")
	}
}

// ★规则3 fail-closed:能力格未置✓→ErrNoCapability+UNKNOWN(调用方无终局可用,不存在自动 rejected)。
func TestDEA_NoCapabilityFailClosed(t *testing.T) {
	s := NewStub()
	s.SetCaps(Capabilities{ExecuteIdempotent: true, Query: false, Fence: false})
	ctx := context.Background()
	v, err := s.Query(ctx, "rx")
	if !errors.Is(err, ErrNoCapability) || TerminalForRelease(v) {
		t.Fatalf("无 query 能力应 ErrNoCapability 且无释放依据,got %s/%v", v, err)
	}
	v, err = s.Fence(ctx, "rx")
	if !errors.Is(err, ErrNoCapability) || TerminalForRelease(v) {
		t.Fatalf("无 fence 能力应 ErrNoCapability 且无释放依据,got %s/%v", v, err)
	}
}

// 能力登记默认形态:Capabilities 零值=全待验(false)=fail-closed 方向(§3.7 待验默认)。
func TestDEA_CapabilitiesZeroValueFailClosed(t *testing.T) {
	var c Capabilities
	if c.ExecuteIdempotent || c.Query || c.Fence {
		t.Fatal("Capabilities 零值应全 false(待验=fail-closed)")
	}
}
