package main

import "time"

// parentPollInterval 是「父进程还在不在」的轮询周期。
//
// 取 200ms 的依据：这条路径救的是【测试进程被杀】这种事故场景，不是正常收尾
// （正常收尾由 stdin 关闭那一路立刻触发，见 main.go）。事故场景下晚 200ms 收场
// 没有任何代价，而轮询再密只会白烧 CPU。
const parentPollInterval = 200 * time.Millisecond

// watchParent 盯「父进程还在不在」：本进程被过继（getppid 变了）⇒ 关掉返回的 channel。
//
// 🔴 为什么不用 PR_SET_PDEATHSIG（组长 2026-10-01T18:36:20Z 点名的那个机制）：
//
//	那个信号在【父线程】退出时就发，而 Go 的运行时随时可能让创建子进程的那个线程退出
//	⇒ 会在【正常运行中】被误触发，把还在用的库停掉。那是拿一个概率性误红
//	换一个概率性残留，方向不对。轮询 getppid 判的是「真的被过继了」，
//	代价只是最多晚一个轮询周期。等价机制、更强的判据 —— 这是取舍的理由，不是省事。
//
// 🔴 它补的是哪一格：stdin 关闭本来就能覆盖「测试进程正常退出/被杀」，但那条依赖
//
//	stdin 是管道（助手用 StdinPipe 起本工具，成立）。本条覆盖的是【stdin 那一路失效】
//	的形状：管道被别的进程继承而没关、或本工具被人手工用继承的 stdin 跑起来。
//
// 🔴 覆盖不到的（如实写，不夸大）：整个进程组被 SIGKILL 打死时，本工具自己也没机会
//
//	跑收尾 —— 那种现场由【下一次启动时的前缀清理】兜（stale.go），不是由本函数。
//	另外，若本工具的父进程本来就是 PID 1，被过继后 getppid 仍是 1 ⇒ 判不出，
//	这一格同样落在 stale.go 那条事后路径上。
//
// getppid 与 sleep 都注入，是为了能确定性反测（watchdog_test.go）：真等一个父进程死
// 既慢又依赖调度。返回的 channel 只在【确认父进程没了】时才关；本函数不自己退出，
// 进程收尾时随进程一起消失（不为它加停止信号：加了就要处理「停止」与「父没了」两种
// 语义在同一个 channel 上互相冒充的问题，那是本函数要防的病）。
func watchParent(initial int, interval time.Duration, getppid func() int, sleep func(time.Duration)) <-chan struct{} {
	gone := make(chan struct{})
	go func() {
		for {
			if getppid() != initial {
				close(gone)
				return
			}
			sleep(interval)
		}
	}()
	return gone
}
