package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchParentFiresOnReparent(t *testing.T) {
	calls := 0
	getppid := func() int {
		calls++
		if calls < 3 {
			return 4242 // 父进程还在
		}
		return 1 // 被过继给 PID 1 ⇒ 父没了
	}
	// 🔴 这一格的 sleep 必须是【空转】：它一阻塞，轮询协程就再也走不到第三次 getppid，
	// 于是「看门狗没报」既可能是产品坏了、也可能是测试自己把它卡住了 —— 两者分不开。
	// （首版就是这么写的，当场红给自己看。）第 3 次调用后协程即退出，不会跑飞。
	gone := watchParent(4242, time.Millisecond, getppid, func(time.Duration) {})
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("父进程被过继后看门狗没报（那正是它唯一要报的一件事）")
	}
	if calls < 3 {
		t.Errorf("getppid 只被问了 %d 次，期望至少 3 次（前两次父还在）", calls)
	}
}

func TestWatchParentStaysQuietWhileParentAlive(t *testing.T) {
	// 🔴 阴性对照：父进程一直活着时【绝不能】报「父没了」——
	// 误报的后果是把还在用的库停掉，那比留残留更糟（PR_SET_PDEATHSIG 的父线程陷阱就是这个形状）。
	var orphaned atomic.Bool
	getppid := func() int {
		if orphaned.Load() {
			return 1
		}
		return 4242
	}
	gone := watchParent(4242, time.Millisecond, getppid, time.Sleep)
	time.Sleep(60 * time.Millisecond) // 让它真轮询几十轮
	select {
	case <-gone:
		t.Fatal("父进程还活着却报了「父没了」—— 会把正在用的库停掉")
	default:
	}
	// 收尾：把轮询协程放出去，不在测试进程里留一个空转的协程。
	orphaned.Store(true)
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("看门狗在父进程消失后没退出（协程会一直空转）")
	}
}
