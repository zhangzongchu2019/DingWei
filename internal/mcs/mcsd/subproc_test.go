package mcsd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// TestMain helper:测试二进制被 SubprocDriver 以 worker 模式拉起时,走 worker 循环。
func TestMain(m *testing.M) {
	if os.Getenv("MCS_WORKER_SOCK") != "" {
		if err := RunWorker(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func e2Rig(t *testing.T, poolCfg pool.Config, behavior func(node string) WireBehavior) (*Daemon, *SubprocDriver, context.CancelFunc) {
	t.Helper()
	drv := &SubprocDriver{SockDir: t.TempDir(), BehaviorFor: behavior}
	if err := drv.Listen(); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{
		BoardPath:  filepath.Join(t.TempDir(), "board.db"),
		Owner:      "mcsd-e2",
		Pool:       poolCfg,
		Tick:       20 * time.Millisecond,
		SweepEvery: 60 * time.Millisecond,
	}, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv.Bind(d)
	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx)
	t.Cleanup(func() { cancel(); time.Sleep(50 * time.Millisecond); drv.Close() })
	return d, drv, cancel
}

// E2 主链:真子进程 worker,跨进程 socket 全链(派发→processing→terminal→done→fan-out)。
func TestE2_CrossProcessChain(t *testing.T) {
	d, drv, _ := e2Rig(t, pool.Config{Min: 2, Max: 2}, nil)
	bctx := context.Background()
	if err := d.B.CreateTask(bctx, "t1", d.Cfg.BoardID, "x", testRoles(), d.ID); err != nil {
		t.Fatal(err)
	}
	d.B.AddNode(bctx, "n1", "t1", 1, "ready", nil)
	d.B.AddNode(bctx, "n2", "t1", 2, "pending", []string{"n1"})
	waitUntil(t, 10*time.Second, "跨进程端到端收口", func() bool {
		e, err := d.B.TaskLedger(bctx, "t1")
		return err == nil && e.NodesDone == 2 && e.EndedTS != 0
	})
	// 真进程佐证:worker 是独立 pid 且存活过
	ws := drv.Workers()
	if len(ws) < 2 {
		t.Fatalf("workers=%d", len(ws))
	}
	for _, w := range ws {
		if w.proc.Pid == os.Getpid() {
			t.Fatal("worker 不是独立进程")
		}
	}
}

// E2 kill -9 failover 全链:在飞任务的 worker 被 SIGKILL(零回执)→Health 判死→reap→
// guarded reassign(gen+1)→重派存活/新 worker→新 rid 收口;旧 rid 无法销账。
func TestE2_Kill9FailoverFullChain(t *testing.T) {
	d, drv, _ := e2Rig(t, pool.Config{Min: 2, Max: 3}, func(node string) WireBehavior {
		if node == "slow" {
			return WireBehavior{DelayMS: 60_000} // 在飞:执行远超测试窗,回执永远来不及
		}
		return WireBehavior{}
	})
	bctx := context.Background()
	d.B.CreateTask(bctx, "t1", d.Cfg.BoardID, "x", testRoles(), d.ID)
	d.B.AddNode(bctx, "slow", "t1", 1, "ready", nil)
	waitUntil(t, 10*time.Second, "派发在飞", func() bool {
		s, err := d.B.GetNode(bctx, "slow")
		return err == nil && (s.Status == "dispatched" || s.Status == "running")
	})
	s, _ := d.B.GetNode(bctx, "slow")
	gen1 := s.Generation
	assigned := s.AssignedAgent.String
	// 真 SIGKILL 该 worker
	var killed bool
	for _, w := range drv.Workers() {
		if w.ID() == assigned {
			w.Kill9()
			killed = true
		}
	}
	if !killed {
		t.Fatalf("找不到在飞 worker %s", assigned)
	}
	// 换代重派并最终收口(重派后行为=默认快速完成——BehaviorFor 对新代仍返回 slow?
	// 注:BehaviorFor 按 node 名返回,slow 仍慢——故重派后节点仍在飞;本测断言【换代+新 rid+旧 rid 拒】)
	waitUntil(t, 10*time.Second, "failover 换代", func() bool {
		s, err := d.B.GetNode(bctx, "slow")
		return err == nil && s.Generation > gen1 && s.AssignedAgent.String != assigned && s.AssignedAgent.Valid
	})
	s2, _ := d.B.GetNode(bctx, "slow")
	if !s2.DeliveryID.Valid || s2.DeliveryID.String == "" {
		t.Fatalf("新代无 rid:%+v", s2)
	}
	// 旧代/旧 rid 迟到 terminal:拒(跨进程账面同样成立)
	if oc, _ := d.B.Terminal(bctx, "slow", s2.Generation, "rid-1", true, "", d.ID); oc == "applied" {
		t.Fatal("旧 rid 不得销账")
	}
	// 事件账:reassigned>=1,dispatched>=2
	if n, _ := d.B.Events(bctx, "slow", "node_reassigned"); n < 1 {
		t.Fatalf("reassigned=%d", n)
	}
	if n, _ := d.B.Events(bctx, "slow", "node_dispatched"); n < 2 {
		t.Fatalf("dispatched=%d", n)
	}
}

// E2 直达窗跨进程(V42a E2E):worker 丢 processing,只回 terminal→从 dispatched 直达 done,无伪 clear。
func TestE2_DirectTerminalAcrossProcess(t *testing.T) {
	d, _, _ := e2Rig(t, pool.Config{Min: 1, Max: 1}, func(node string) WireBehavior {
		return WireBehavior{DropProcessing: true}
	})
	bctx := context.Background()
	d.B.CreateTask(bctx, "t1", d.Cfg.BoardID, "x", testRoles(), d.ID)
	d.B.AddNode(bctx, "n1", "t1", 1, "ready", nil)
	waitUntil(t, 10*time.Second, "直达收口", func() bool {
		s, err := d.B.GetNode(bctx, "n1")
		return err == nil && s.Status == "done"
	})
	if n, _ := d.B.Events(bctx, "n1", "node_started"); n != 0 {
		t.Fatalf("processing 被丢不应有 node_started:%d", n)
	}
	if n, _ := d.B.Events(bctx, "n1", "node_annotation_cleared"); n != 0 {
		t.Fatalf("无注记不得伪 clear:%d", n)
	}
}
