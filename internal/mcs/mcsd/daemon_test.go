package mcsd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

func testRoles() board.RoleAssignments {
	rb := func(w string) board.RoleBinding { return board.RoleBinding{Who: w, CLI: "claude", LLM: "claude-opus-4-8"} }
	return board.RoleAssignments{Designer: rb("devo48"), Implementer: rb("devo48"), Tester: rb("tester"), QAOwner: rb("shence")}
}

// waitUntil 轮询断言(端到端异步收敛;确定性由超时上限+条件谓词保证)。
func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting: %s", what)
}

// E1 冒烟:守护起→种子任务(DAG 两节点)→端到端走完(派发→回执→done→fan-out→done)→台账收口→优雅退出。
func TestE1_SeedTaskEndToEnd(t *testing.T) {
	drv := &LoopbackDriver{}
	d, err := New(Config{
		BoardPath: filepath.Join(t.TempDir(), "board.db"),
		Owner:     "mcsd-e1",
		Pool:      pool.Config{Min: 2, Max: 3},
		Tick:      20 * time.Millisecond,
		SweepEvery: 100 * time.Millisecond,
	}, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv.Bind(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	bctx := context.Background()
	if err := d.B.CreateTask(bctx, "t1", d.Cfg.BoardID, "seed", testRoles(), d.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.B.AddNode(bctx, "n1", "t1", 1, "ready", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.B.AddNode(bctx, "n2", "t1", 2, "pending", []string{"n1"}); err != nil {
		t.Fatal(err)
	}

	waitUntil(t, 5*time.Second, "任务端到端收口", func() bool {
		e, err := d.B.TaskLedger(bctx, "t1")
		return err == nil && e.NodesDone == 2 && e.EndedTS != 0
	})
	e, _ := d.B.TaskLedger(bctx, "t1")
	if e.StartedTS == 0 || e.Dispatcher != "mcsd-e1" || len(e.Roles) != 4 {
		t.Fatalf("台账:%+v", e)
	}
	// 全事件零匿名(治理约束在 E2E 栈同样成立)
	// 优雅退出
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run 退出:%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("退出超时")
	}
}

// E1:执行失败节点→failed;不影响其他节点;台账收口(done+failed=全终态)。
func TestE1_FailedNodeConverges(t *testing.T) {
	drv := &LoopbackDriver{Behavior: LoopbackBehavior{FailNodes: map[string]bool{"bad": true}}}
	d, err := New(Config{
		BoardPath: filepath.Join(t.TempDir(), "board.db"),
		Owner:     "mcsd-e1",
		Pool:      pool.Config{Min: 2, Max: 3},
		Tick:      20 * time.Millisecond,
	}, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv.Bind(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	bctx := context.Background()
	d.B.CreateTask(bctx, "t1", d.Cfg.BoardID, "x", testRoles(), d.ID)
	d.B.AddNode(bctx, "good", "t1", 1, "ready", nil)
	d.B.AddNode(bctx, "bad", "t1", 2, "ready", nil)
	waitUntil(t, 5*time.Second, "双节点终态", func() bool {
		g, err1 := d.B.GetNode(bctx, "good")
		b2, err2 := d.B.GetNode(bctx, "bad")
		return err1 == nil && err2 == nil && g.Status == "done" && b2.Status == "failed"
	})
	e, _ := d.B.TaskLedger(bctx, "t1")
	if e.EndedTS == 0 {
		t.Fatalf("全终态应收口:%+v", e)
	}
}

// E1:守护重启接管(takeover epoch+1)后继续调度同一库(文件持久)。
func TestE1_RestartTakeoverContinues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "board.db")
	drv := &LoopbackDriver{Behavior: LoopbackBehavior{DropTerminal: true}} // 首任丢 terminal:留在途账
	d1, err := New(Config{BoardPath: dbPath, Owner: "mcsd-a", Pool: pool.Config{Min: 1, Max: 1}, Tick: 20 * time.Millisecond}, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv.Bind(d1)
	ctx1, cancel1 := context.WithCancel(context.Background())
	go d1.Run(ctx1)
	bctx := context.Background()
	d1.B.CreateTask(bctx, "t1", d1.Cfg.BoardID, "x", testRoles(), d1.ID)
	d1.B.AddNode(bctx, "n1", "t1", 1, "ready", nil)
	waitUntil(t, 5*time.Second, "首任派发+running(terminal 被丢)", func() bool {
		s, err := d1.B.GetNode(bctx, "n1")
		return err == nil && s.Status == "running"
	})
	cancel1() // 守护"崩"(优雅路径即可,库在盘)
	time.Sleep(50 * time.Millisecond)

	drv2 := &LoopbackDriver{}
	d2, err := New(Config{BoardPath: dbPath, Owner: "mcsd-b", Pool: pool.Config{Min: 1, Max: 1}, Tick: 20 * time.Millisecond}, drv2, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv2.Bind(d2)
	defer d2.Shutdown()
	if d2.ID.Epoch != d1.ID.Epoch+1 {
		t.Fatalf("接管 epoch=%d want %d", d2.ID.Epoch, d1.ID.Epoch+1)
	}
	// 旧身份被 fence:guarded 写全拒
	if oc, _ := d2.B.SetBlocker(bctx, "n1", 1, "timeout", d1.ID); oc != board.OutcomeCASMiss {
		t.Fatalf("旧 epoch 应被拒:%s", oc)
	}
	// 新任以人工 reassign 收敛在途账(running 无回执节点=运维决策;E3 剧本走全自动路径)
	s, _ := d2.B.GetNode(bctx, "n1")
	if oc, _ := d2.B.Reassign(bctx, "n1", s.Generation, "", d2.ID); oc != board.OutcomeApplied {
		t.Fatalf("接管后 reassign=%s", oc)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go d2.Run(ctx2)
	waitUntil(t, 5*time.Second, "接管后收口", func() bool {
		s, err := d2.B.GetNode(bctx, "n1")
		return err == nil && s.Status == "done"
	})
}
