package main

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// fakeClock 把「睡」与「现在几点」合成一个可推进的假时钟：
// 收尸的终止条件里有时间上限，用真时钟测它既慢又不稳（CI 上一抖就红）。
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time                      { return c.t }
func (c *fakeClock) Sleep(d time.Duration)               { c.t = c.t.Add(d) }
func (c *fakeClock) Since(start time.Time) time.Duration { return c.t.Sub(start) }

// 反测的判据是【修复前的形状必须被这些用例判红】：每一格都在同一序列上跑一次
// 「只收一轮」（＝修复前 reapOrphans() 调一次的形状），它拿不到的收获新形状必须拿到 ——
// 否则本函数只是把旧代码换了个写法（裁定 ruling-2026-10-01T174744Z 二那条反向对照）。

// singleShot 是修复前的形状：收一次就走，不等晚退出的孤儿。
func singleShot(reap func() (int, error)) int {
	n, _ := reap()
	return n
}

func TestReapUntilGoneCatchesLateOrphan(t *testing.T) {
	// 孤儿在前两轮还没退出（收不到），第三轮才退出。
	calls := 0
	reap := func() (int, error) {
		calls++
		if calls == 3 {
			return 1, nil
		}
		return 0, nil // 还有后代，只是这一刻没退出的
	}
	remaining := func() (int, bool) {
		if calls >= 3 {
			return 0, true
		}
		return 1, true
	}
	clk := &fakeClock{t: time.Unix(0, 0)}

	got := reapUntilGone(reap, remaining, clk.Sleep, clk.Now, time.Millisecond, time.Second)
	if got.Reaped != 1 {
		t.Fatalf("晚退出的孤儿没收到: %+v（calls=%d）", got, calls)
	}
	if got.Capped {
		t.Errorf("收到全部孤儿后不该是「上限触发」: %+v", got)
	}
	if got.Remaining != 0 {
		t.Errorf("收手时应当如实报「已无后代」: Remaining=%d", got.Remaining)
	}
	// 🔴 阴性对照：修复前的形状在同一序列下必然空手 —— 这就是本函数存在的理由。
	calls = 0
	if n := singleShot(reap); n != 0 {
		t.Fatalf("阴性对照失效: 只收一轮本该收不到晚退出的孤儿, got=%d", n)
	}
}

func TestReapUntilGoneDoesNotWaitWhenNoDescendants(t *testing.T) {
	// (a) 的要点：已无后代 ⇒ 【立刻】收手，不白等一个静默窗。
	sleeps := 0
	clk := &fakeClock{t: time.Unix(0, 0)}
	sleep := func(d time.Duration) { sleeps++; clk.Sleep(d) }
	rounds := 0
	reap := func() (int, error) { rounds++; return 0, nil }
	remaining := func() (int, bool) { return 0, true }

	got := reapUntilGone(reap, remaining, sleep, clk.Now, time.Millisecond, time.Second)
	if got.Reaped != 0 {
		t.Errorf("没有孤儿时不该报收获: %+v", got)
	}
	if rounds != 1 || sleeps != 0 {
		t.Fatalf("已无后代还多等了: 轮数=%d want 1、sleep=%d want 0（上一版在这里白等 30 轮＝150ms，每次起停付两遍）", rounds, sleeps)
	}
	if got.Remaining != 0 {
		t.Errorf("Remaining=%d want 0", got.Remaining)
	}
}

func TestReapUntilGoneFallsBackToECHILDWhenProcUnavailable(t *testing.T) {
	// /proc 判不出（非 Linux、内核没开 CONFIG_PROC_CHILDREN）⇒ 退回 wait4 的 ECHILD，
	// 而不是「判不出就一直等到上限」。
	rounds := 0
	reap := func() (int, error) { rounds++; return 0, errNoChildren }
	remaining := func() (int, bool) { return 0, false }
	clk := &fakeClock{t: time.Unix(0, 0)}

	got := reapUntilGone(reap, remaining, clk.Sleep, clk.Now, time.Millisecond, time.Second)
	if rounds != 1 {
		t.Fatalf("ECHILD 应当立刻收手: 轮数=%d want 1", rounds)
	}
	if got.Remaining != 0 || got.Capped {
		t.Errorf("ECHILD 这条路应当报「已无后代」: %+v", got)
	}
}

func TestReapUntilGoneCapsAndReportsLeftover(t *testing.T) {
	// 一直有后代没退出 ⇒ 必须由时间上限收手，且【说出还剩几个】（不静默收手）。
	reap := func() (int, error) { return 0, nil }
	remaining := func() (int, bool) { return 3, true }
	clk := &fakeClock{t: time.Unix(0, 0)}

	got := reapUntilGone(reap, remaining, clk.Sleep, clk.Now, 5*time.Millisecond, 50*time.Millisecond)
	if !got.Capped {
		t.Fatalf("上限没生效（会卡死收尾）: %+v", got)
	}
	if got.Remaining != 3 {
		t.Errorf("上限触发时必须报出未退出的后代数: Remaining=%d want 3", got.Remaining)
	}
	if got.Elapsed < 50*time.Millisecond {
		t.Errorf("上限是 50ms，实际只等了 %v", got.Elapsed)
	}
	if got.Rounds < 5 {
		t.Errorf("50ms／5ms 至少该跑 10 轮左右，实得 %d 轮（间隔没生效？）", got.Rounds)
	}
}

func TestReapUntilGonePropagatesUnexpectedError(t *testing.T) {
	// wait4 报了 ECHILD 以外的错 ⇒ 不吞：吞掉就是把「判不出」冒充成「没有」。
	boom := errors.New("wait4: 权限不对")
	reap := func() (int, error) { return 0, boom }
	remaining := func() (int, bool) { return 1, true }
	clk := &fakeClock{t: time.Unix(0, 0)}

	got := reapUntilGone(reap, remaining, clk.Sleep, clk.Now, time.Millisecond, time.Second)
	if !errors.Is(got.Err, boom) {
		t.Fatalf("错误被吞了: %+v", got)
	}
	if got.Capped {
		t.Errorf("出错就该停下，不是继续等到上限: %+v", got)
	}
}

func TestReapConstantsSane(t *testing.T) {
	if err := reapConstantsSane(); err != nil {
		t.Fatalf("收尸节奏常量不自洽: %v", err)
	}
}

// ── 下面两格用【真进程】验原语，不用假函数 ──────────────────────────────
//
// 🔴 为什么要真进程：descendantCount 读的是 /proc/self/task/＊/children，
// 而 wait4 看的是【调用线程】的子进程 —— 两者的口径差异正是 (a) 里「/proc 优先、
// ECHILD 兜底」那条设计的理由。只用注入的假函数测，这条差异永远测不到。
// 故这两格锁定 OS 线程（LockOSThread），保证起子进程与收尸在同一个线程上。

func TestReapPrimitivesOnRealLateChild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("只在 Linux 上判：/proc/self/task/＊/children 是 Linux 的东西")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cmd := exec.Command("sleep", "0.3") // 晚 300ms 才退出的「孤儿」替身
	if err := cmd.Start(); err != nil {
		t.Fatalf("起替身进程失败: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release() // 不让 os.Process 去 wait：这一格要自己收

	// 🔴 阴性对照（真进程版）：修复前「只收一次」在这一刻必然空手 —— 子进程还活着。
	if n, err := reapOrphansOnce(); n != 0 || errors.Is(err, errNoChildren) {
		t.Fatalf("只收一次本该空手（子进程还没退出）: n=%d err=%v", n, err)
	}
	if cnt, ok := descendantCount(); !ok || cnt < 1 {
		t.Fatalf("descendantCount 没数到活着的子进程: cnt=%d ok=%v", cnt, ok)
	}
	if !pidAlive(pid) {
		t.Fatalf("pidAlive 说 %d 不活着，而它刚起来", pid)
	}

	// 新形状：等到它真退出并收掉，且【不靠猜窗】——收完立刻收手。
	start := time.Now()
	got := reapUntilGone(reapOrphansOnce, descendantCount, time.Sleep, time.Now, reapInterval, 10*time.Second)
	if got.Err != nil {
		t.Fatalf("收尸报错: %v", got.Err)
	}
	if got.Reaped < 1 {
		t.Fatalf("晚退出的子进程没收到: %+v", got)
	}
	if got.Capped {
		t.Errorf("不该走到时间上限: %+v", got)
	}
	if got.Remaining != 0 {
		t.Errorf("收手时应已无后代: Remaining=%d", got.Remaining)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("等到一个 300ms 后退出的子进程花了 %v —— 终止条件又在白等", d)
	}
	if cnt, ok := descendantCount(); ok && cnt != 0 {
		t.Errorf("收完之后 /proc 里还剩 %d 个后代", cnt)
	}
}
