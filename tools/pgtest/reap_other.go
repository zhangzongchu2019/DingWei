//go:build !linux

package main

// 非 Linux:不设 subreaper、不收尸。
//
// 本仓的测试环境只跑 Linux(容器 PID 1 是 `sleep infinity`,不收孤儿 ⇒ 才有僵尸问题);
// macOS/Windows 上 init/launchd 会正常收尸,不需要这两个动作。
// 拆成两个带构建标签的文件,而不是在 main.go 里写死 syscall.Prctl,
// 是为了 `go build` 在任何平台都过 —— 工具编不出来,整套 PG 用例就全跳了。
func becomeSubreaper() error { return nil }

func reapOrphans() int { return 0 }
