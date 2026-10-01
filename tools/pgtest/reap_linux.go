//go:build linux

package main

import "syscall"

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

// reapOrphans 收掉「已退出但还没人 wait」的后代,返回收了几条。WNOHANG ⇒ 绝不阻塞。
//
// 🔴 只许在 embedded-postgres 的 Stop() 返回【之后】调用:库自己起的 pg_ctl/initdb
// 由它的 cmd.Run() 负责 wait;中途插手会把退出状态抢走,让库报
// 「wait: no child processes」这类假错 —— 那是把真问题换成噪音,比僵尸更糟。
// 没有可收的后代时 Wait4 返回 ECHILD,循环随即结束。
func reapOrphans() int {
	reaped := 0
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil || pid <= 0 {
			return reaped
		}
		reaped++
	}
}
