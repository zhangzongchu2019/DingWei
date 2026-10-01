//go:build linux

package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// prSetChildSubreaper 是 prctl(2) 的 PR_SET_CHILD_SUBREAPER。
//
// 🔴 为什么写死数值 36 而不是引常量:Go 标准库 syscall 只在【部分】架构上导出了它
// (实测 go1.22.2:zerrors_linux_mipsle/loong64/s390x 有,amd64 没有),
// 而 syscall.Prctl 在 Linux 上根本没有导出。要用就得走 RawSyscall + 自己的常量。
// 取 golang.org/x/sys/unix 当然更体面,但那会给这个独立 module 添一条直接依赖 ——
// 本工具存在的理由之一就是「依赖关在这里、主 go.mod 一个数都不动」,
// 为一个 prctl 常量把面撑大不划算。数值来源:linux/include/uapi/linux/prctl.h。
const prSetChildSubreaper = 36

// becomeSubreaper 让本进程接管【后代的孤儿】,这样它们退出后由本进程收尸,
// 而不是过继给 PID 1(跑测试的容器里 PID 1 是 `sleep infinity`,永远不收)。
func becomeSubreaper() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		return errno
	}
	return nil
}

// reapOrphansOnce 收掉「已退出但还没人 wait」的后代，返回收了几条。WNOHANG ⇒ 绝不阻塞。
//
// 🔴 与上一版的区别在【返回值】：上一版把 err 与 pid<=0 合成一个「空手而归」，
//
//	于是「已经没有后代了」（ECHILD）与「后代还在、只是这一刻没退」（pid==0）
//	在调用方看来一模一样 ⇒ 只能靠猜静默窗收手。拆开之后，终止条件才可能
//	是【已无后代】这个确定判据（见 reap_loop.go）。
//
// 🔴 只许在 embedded-postgres 的 Stop() 返回【之后】调用：库自己起的 pg_ctl/initdb
//
//	由它的 cmd.Run() 负责 wait；中途插手会把退出状态抢走，让库报
//	「wait: no child processes」这类假错 —— 那是把真问题换成噪音，比僵尸更糟。
//
// 非 ECHILD 的错误【不吞】，原样交回：吞掉就是把「判不出」冒充成「没有」。
func reapOrphansOnce() (int, error) {
	reaped := 0
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil {
			if errors.Is(err, syscall.ECHILD) {
				return reaped, errNoChildren
			}
			return reaped, err
		}
		if pid <= 0 {
			return reaped, nil // 还有后代，只是这一刻没有已退出的
		}
		reaped++
	}
}

// descendantCount 数本进程【还有几个后代】：读 /proc/self/task/＊/children 并去重求和。
//
// 🔴 /proc 给的是【直接子进程】，而那正好就是 wait4 能收的那一批 —— 过继来的孤儿
//
//	也是本进程的直接子进程，不是「孙子」。所以「直接子进程数＝0」就等价于「已无后代可收」。
//
// 🔴 为什么不靠 wait4 的 ECHILD 单独判：ECHILD 说的是【调用线程】没有子进程，
//
//	而 Go 的运行时把 goroutine 放在哪个线程上跑是不保证的，孤儿的过继目标又是
//	subreaper 线程组里的某个线程 ⇒ 两者可能错开，ECHILD 会提前为真。
//	/proc 这一路数的是整个进程，不受线程调度影响。
//
// 第二返回值 false ＝ 这台机器判不出（/proc 没挂、内核没开 CONFIG_PROC_CHILDREN、
// 任一线程的 children 读不到）⇒ 调用方退回 ECHILD 判，🔴 不假装数过一个偏小的值。
func descendantCount() (int, bool) {
	tasks, err := os.ReadDir("/proc/self/task")
	if err != nil || len(tasks) == 0 {
		return 0, false
	}
	seen := map[string]struct{}{}
	for _, t := range tasks {
		b, err := os.ReadFile("/proc/self/task/" + t.Name() + "/children")
		if err != nil {
			return 0, false // 读不全就判不出：宁可退回 ECHILD，不报一个偏小的数
		}
		for _, f := range strings.Fields(string(b)) {
			seen[f] = struct{}{}
		}
	}
	return len(seen), true
}

// pidAlive 判一个 PID 还在不在。
//
// 🔴 不能只用 kill(pid, 0)：它对【僵尸】也返回成功 —— 僵尸的 PID 在被人 wait 掉之前一直存在，
//
//	而本工具要清的现场恰恰是「父进程被杀、没人 wait」的僵尸（容器 PID 1 是
//	`sleep infinity`，永不收尸）。只按 kill 判就会把「已经死了但没被收」读成「还活着」，
//	于是走 SIGTERM ＋ 等 5 秒那条路：白等 5 秒，而且如实报出去的 Terminated 名单里
//	会混进根本没在跑的东西 —— 那又是一处「读数看着像那么回事，其实指错了对象」。
//
// ⇒ Linux 上再读一眼 /proc/<pid>/stat 的状态位，'Z' 一律算【不活着】；
//
//	/proc 读不出时退回 kill(pid,0) 的口径（保守当活着 ⇒ 只报不杀，不误杀）。
func pidAlive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // 判不出是不是僵尸 ⇒ 保守当活着
	}
	// stat 的形状是「pid (comm) state …」，而 comm 里可以有空格与括号
	// ⇒ 从【最后一个】右括号之后取状态位，不从第一个之后取。
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 >= len(s) {
		return true // 读出来却不是预期形状 ⇒ 同上，保守当活着
	}
	return s[i+2] != 'Z'
}

// terminatePID 给孤儿 postmaster 发 SIGTERM（不是 SIGKILL：让它走自己的快速关停，
// 把 WAL 收干净；杀急了下次起库要恢复，那是把「残留」换成更难查的东西）。
func terminatePID(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// waitGonePID 等一个 PID 消失（最多 d）。它只是给 SIGTERM 一点时间，等不到也不阻塞收尾。
func waitGonePID(pid int, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
