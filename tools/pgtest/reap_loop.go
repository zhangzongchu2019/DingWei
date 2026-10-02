package main

import (
	"errors"
	"time"
)

// 收尸节奏。
//
// 🔴 终止条件是【已无后代】，不是「静默窗」—— 这是本文件唯一的要点。
//
//	上一版（分支 qwen38maxkhovvob4/pgtest-reap-retry，tip c9969181；裁定
//	ruling-2026-10-01T174744Z 二判为【不合入】）用的是「连续 30 轮没有收获就收手」：
//	那是【猜一个窗】。猜窗有两种错法，都实测到了 ——
//	  窗太短：晚退出的孤儿收不到，留成永久僵尸（这就是本函数最初要修的缺陷）；
//	  窗太长：正常路径上白等，每次起停都要付两遍（主扫＋收尾），
//	         A/B 实测每次起停由 3.75–4.07s 涨到 8.4–13.4s（2.55 倍）。
//	换成「还有没有后代」这个【确定判据】之后，正常路径的成本 ＝ 最后一个后代真正退出
//	的那一刻，不多等一轮；收手时还说得出【为什么】收手（真没了 / 上限到了 / 还剩几个）。
//
//	reapInterval  每轮之间睡多久（5ms：比 postmaster 子进程退出的典型间隔小一个量级）
//	reapTimeout   时间上限兜底（5s）。上限触发时把「还有几个后代没退出」打进输出，
//	            便于下次排障，而不是静默收手（裁定 ruling-2026-10-01T174744Z 二明写此点）。
const (
	reapInterval = 5 * time.Millisecond
	reapTimeout  = 5 * time.Second
)

// errNoChildren 表示 wait4 报了 ECHILD ⇒ 调用线程已无子进程。
var errNoChildren = errors.New("pgtest: wait4 报 ECHILD（已无子进程）")

// reapReport 是一次收尸的【全部】结果，不只是「收了几条」。
//
// 🔴 为什么要带 Remaining／Capped／Elapsed：上一版收手时说不出自己为什么收手，
//
//	「上限到了」与「真没了」在输出里长一个样，而它的耗时又只能从整包测试时间反推
//	⇒ 于是「变慢 2.55 倍」这个结论只能靠 A/B 两棵树对跑才敢写。
//	现在三个数都直接报出来（REAP_SECONDS／REAP_LEFTOVER），读数不必再靠推断。
//
// Remaining ＝ -1 表示这台机器判不出「还有几个」（/proc 不可用且没走到 ECHILD）。
type reapReport struct {
	Reaped    int           // 累计收了几条
	Rounds    int           // 跑了几轮
	Remaining int           // 收手时还没退出的后代数；-1＝判不出
	Capped    bool          // 是否因时间上限收手（而不是「已无后代」）
	Elapsed   time.Duration // 这一次收尸花了多久（自证，不靠外部推算）
	Err       error         // 非 ECHILD 的错，如实交回
}

// reapUntilGone 收尸，直到【已无后代】或时间上限。
//
//	reap      收掉当前所有「已退出但还没人 wait」的后代，返回收了几条；
//	          err＝errNoChildren 表示 wait4 报 ECHILD。别的错【不吞】，交回调用方。
//	remaining 数「还有几个后代」；第二返回值 false ⇒ 判不出（退回 ECHILD 判）。
//	sleep／now 都注入，是为了让这段逻辑能被**确定性**反测（见 reap_loop_test.go）：
//	          「晚一点才退出的孤儿」正是本函数存在的理由，用真 sleep 去测它既慢又不稳定。
func reapUntilGone(reap func() (int, error), remaining func() (int, bool),
	sleep func(time.Duration), now func() time.Time, interval, timeout time.Duration) reapReport {
	start := now()
	rep := reapReport{Remaining: -1}
	finish := func() reapReport {
		rep.Elapsed = now().Sub(start)
		return rep
	}
	for {
		n, err := reap()
		rep.Reaped += n
		rep.Rounds++
		if n > 0 {
			continue // 有收获就立刻再收一次，不白等一轮
		}
		if err != nil && !errors.Is(err, errNoChildren) {
			rep.Err = err // 「没后代」以外的错不吞：吞了就是把「判不出」冒充成「没有」
			return finish()
		}
		// 「已无后代」的两个判据：/proc 优先，ECHILD 兜底。
		// 🔴 为什么不只用 ECHILD：Linux 的 wait 看的是【调用线程】的子进程，
		//    而孤儿的过继目标是 subreaper 线程组里的某个线程 —— 两者可能不是同一个，
		//    于是 ECHILD 可能在别处还挂着未退出后代时就先为真。/proc 数的是整个进程。
		if cnt, ok := remaining(); ok {
			rep.Remaining = cnt
			if cnt == 0 {
				return finish()
			}
		} else if errors.Is(err, errNoChildren) {
			rep.Remaining = 0
			return finish()
		}
		if now().Sub(start) >= timeout {
			rep.Capped = true
			return finish()
		}
		sleep(interval)
	}
}

// reapConstantsSane 是常量自检（用例调它，生产不调）：
// 上限必须真能兜住，且不得大到把「每次起停」拖回上一版那个量级。
func reapConstantsSane() error {
	if reapInterval <= 0 {
		return errors.New("reapInterval 必须是正数")
	}
	if reapTimeout <= 0 {
		return errors.New("reapTimeout 必须是正数（上限是兜底，不能没有）")
	}
	if reapTimeout > 10*time.Second {
		return errors.New("reapTimeout 过大：每次起停都要付这笔时间，会拖慢全部 PG 用例")
	}
	return nil
}
