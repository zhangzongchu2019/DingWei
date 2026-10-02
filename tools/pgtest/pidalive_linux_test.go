//go:build linux

package main

// 🔴 这一格钉的是 pidAlive 对【僵尸】的判法。
//
// kill(pid,0) 对僵尸也返回成功 —— 僵尸的 PID 在被人 wait 掉之前一直存在。只按 kill 判，
// 就会把「早就不在跑、只是没人收」读成「还活着」，于是 stale.go 对它走 SIGTERM＋等 5 秒
// 那条路：白等 5 秒，而且如实报出去的 Terminated 名单里会混进根本没在跑的东西。
// 这不是假想形状：跑测试的容器里 PID 1 是 `sleep infinity`，永不收尸，
// 本席现场实测躺着几十个这种 postgres 僵尸（其中一个正是本席手工 SIGTERM 掉
// 上一次被杀留下的孤儿 postmaster 之后留下的）。

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// psState 用【外部命令】独立核一个 pid 的状态位，不用生产代码那套解析 ——
// 前提核对若与被测函数同源，就等于自己给自己作证。
func psState(pid int) (string, bool) {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func TestPidAliveRejectsZombieButAcceptsLiveChild(t *testing.T) {
	// 起子进程与收尸必须在同一个 OS 线程上（wait4 看的是调用线程的子进程）。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	live := exec.Command("sleep", "5")
	if err := live.Start(); err != nil {
		t.Fatalf("起活替身失败: %v", err)
	}
	defer func() {
		_ = live.Process.Kill()
		_, _ = live.Process.Wait()
	}()
	if !pidAlive(live.Process.Pid) {
		t.Fatalf("活着的子进程 %d 被判成不活着 ⇒ 判据反了", live.Process.Pid)
	}

	late := exec.Command("sleep", "0.05")
	if err := late.Start(); err != nil {
		t.Fatalf("起僵尸替身失败: %v", err)
	}
	zpid := late.Process.Pid
	_ = late.Process.Release() // 故意不 Wait：它退出后就是本进程的僵尸，与被 PID 1 挂着同型

	// 等它真变成僵尸（前提），最多 3 秒。
	deadline := time.Now().Add(3 * time.Second)
	var st string
	var ok bool
	for time.Now().Before(deadline) {
		if st, ok = psState(zpid); ok && strings.HasPrefix(st, "Z") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ok && !strings.HasPrefix(st, "Z") {
		t.Fatalf("前提没成立: pid %d 的状态是 %q，不是僵尸，这一格测不到东西", zpid, st)
	}

	if pidAlive(zpid) {
		t.Errorf("🔴 僵尸 %d 被判成活着 ⇒ stale.go 会对一个早就不在跑的 postmaster 发 SIGTERM 并白等 5 秒，"+
			"还会把它写进 Terminated 名单冒充读数", zpid)
	}
	// 阴性对照：同一时刻 kill(pid,0) 仍然成功 —— 这就是「只按 kill 判」为什么不够。
	if err := syscall.Kill(zpid, 0); err != nil {
		t.Errorf("对照失效: 僵尸 %d 上 kill(pid,0) 本该成功（僵尸的 PID 仍在）, err=%v", zpid, err)
	}
	// 自己造的僵尸自己收，别留给下一次跑（本容器 PID 1 不收）。
	if n, err := reapOrphansOnce(); n < 1 {
		t.Errorf("自己造的僵尸没收到: n=%d err=%v", n, err)
	}
	if pidAlive(zpid) {
		t.Errorf("收掉之后 %d 还在（应收尸后 PID 消失）", zpid)
	}
}
