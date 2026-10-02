//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

// ignoreSigPipe 让「往已破裂的 stdout/stderr 写」只返回 EPIPE，而不是把本进程打死。
//
// 🔴 这一句是【收尾能不能跑完】的开关，不是洁癖。本工具的 stdout 是测试进程那头的管道，
// 而「测试进程被杀」正是本工具要救的场景（stdin 关闭与父亡看门狗两条判据都为它而设）：
// 那时管道读端已关，而 Go 运行时对 fd 1/2 上的 SIGPIPE 走默认处置 —— 直接打死进程。
// 于是收尾死在【第一次往 stdout 写】那一步，两条救援路径都白设。
//
// 实测两场（同一个二进制、同一次起库，唯一差别是 fd 1 接什么；读数见交付件第六节表七）：
//
//	stdout 落文件    ：收尾跑完 —— STOPPED=1 CLEANED=1、两个目录删净、REAPED=1、不留僵尸；
//	stdout 落破裂管道 ：pg.Stop() 把库停了，工具随即死在写 stdout 上 ⇒ cleanDir 与收尸都没跑到，
//	                  两个目录留在 /tmp，postmaster 过继给 PID 1 变【永久僵尸】（PID 1 不收尸）。
//
// 忽略之后，收尾期间那些读数没人接（那头本来就死了），但【现场是干净的】——
// 现场才是要救的东西。读数丢掉不影响任何判据：残留由 internal/store/pgtest 独立核，
// 不采信工具自报（见 residue.go 的头注）。
func ignoreSigPipe() { signal.Ignore(syscall.SIGPIPE) }
