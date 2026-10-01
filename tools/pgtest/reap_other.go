//go:build !linux

package main

import (
	"errors"
	"time"
)

// errNoKillOnPlatform 见 terminatePID。
var errNoKillOnPlatform = errors.New("pgtest: 本平台不终止进程（判不出 PID 归属，杀错比留残留更糟）")

// 非 Linux:不设 subreaper、不收尸。
//
// 本仓的测试环境只跑 Linux(容器 PID 1 是 `sleep infinity`,不收孤儿 ⇒ 才有僵尸问题);
// macOS/Windows 上 init/launchd 会正常收尸,不需要这两个动作。
// 拆成两个带构建标签的文件,而不是在 main.go 里写死 syscall.Prctl,
// 是为了 `go build` 在任何平台都过 —— 工具编不出来,整套 PG 用例就全跳了。
func becomeSubreaper() error { return nil }

// reapOrphansOnce 在非 Linux 上恒报「已无后代」：init/launchd 会正常收尸，
// 没有可收的东西；报 errNoChildren 而不是 (0, nil)，是为了让调用方【立刻】收手，
// 而不是在时间上限里空转 5 秒（那会给 macOS 上的每次起停白加一笔时间）。
func reapOrphansOnce() (int, error) { return 0, errNoChildren }

// descendantCount 恒判不出（没有 /proc）⇒ 调用方退回 ECHILD 判，上一条已保证它立刻为真。
func descendantCount() (int, bool) { return 0, false }

// pidAlive 在非 Linux 上【判不出】⇒ 一律报 false，并如实写在返回值语义里：
// 于是 staleVerdict 会走 remove 分支（只删目录、不杀进程）。
// 🔴 这条路径的代价写明：非 Linux 上若真有一个活的孤儿 PG，本清理会删掉它的数据目录
// 而不终止它。可接受的理由是这套工具只服务 Linux CI（macOS/Windows 的 init 会收尸，
// 本就没有「留成永久僵尸」那条缺陷）；🔴 不得把它读成「跨平台等价」。
func pidAlive(pid int) bool { return false }

// terminatePID 在非 Linux 上不杀进程（判不出 PID 是否属于本工具起的那个 postmaster，
// 杀错进程比留残留更糟），只报一个明确的错误让调用方在 stderr 上看得见。
func terminatePID(pid int) error { return errNoKillOnPlatform }

// waitGonePID 在非 Linux 上无事可做。
func waitGonePID(pid int, d time.Duration) {}
