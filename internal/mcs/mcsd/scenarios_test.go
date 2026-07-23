package mcsd

// E3 场景剧本(八连,phase-2 模拟测试台):全部跑在【真子进程+unix socket】栈上,
// 行为注入随件下发,确定性=条件谓词+有界超时。剧本编号对应 E3 计划①-⑧。

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// statefulBehavior 按 (node, 第几次投递) 出行为:剧本⑤⑥用("首投故障,重投正常")。
func statefulBehavior(firstOnly map[string]WireBehavior) func(string) WireBehavior {
	var mu sync.Mutex
	count := map[string]int{}
	return func(node string) WireBehavior {
		mu.Lock()
		defer mu.Unlock()
		count[node]++
		if b, ok := firstOnly[node]; ok && count[node] == 1 {
			return b
		}
		return WireBehavior{}
	}
}

func addTask(t *testing.T, d *Daemon, taskID string, nodes ...string) {
	t.Helper()
	ctx := context.Background()
	if err := d.B.CreateTask(ctx, taskID, d.Cfg.BoardID, taskID, testRoles(), d.ID); err != nil {
		t.Fatal(err)
	}
	for i, n := range nodes {
		if err := d.B.AddNode(ctx, n, taskID, i+1, "ready", nil); err != nil {
			t.Fatal(err)
		}
	}
}

// 剧本①:池伸缩——压满(Min=1,Max=3,3 慢件并发→扩到 3)→跑完→idle 超 TTL 缩回 Min。
func TestS1_PoolScaleUpThenShrink(t *testing.T) {
	drv := &SubprocDriver{SockDir: t.TempDir(), BehaviorFor: func(string) WireBehavior { return WireBehavior{DelayMS: 400} }}
	if err := drv.Listen(); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{
		BoardPath: filepath.Join(t.TempDir(), "b.db"), Owner: "s1",
		Pool: pool.Config{Min: 1, Max: 3, IdleTTL: 300 * time.Millisecond},
		Tick: 20 * time.Millisecond, SweepEvery: 60 * time.Millisecond,
	}, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv.Bind(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	addTask(t, d, "t1", "a", "b", "c")
	waitUntil(t, 10*time.Second, "扩容到3", func() bool {
		total, _ := d.P.Size()
		return total == 3
	})
	waitUntil(t, 10*time.Second, "三件收口", func() bool {
		e, err := d.B.TaskLedger(context.Background(), "t1")
		return err == nil && e.NodesDone == 3
	})
	waitUntil(t, 10*time.Second, "缩回下限", func() bool {
		total, _ := d.P.Size()
		return total == 1
	})
}

// 剧本②:轮换均衡——Min=Max=2,4 快件,两 worker 都被用到。
func TestS2_RotationSpreads(t *testing.T) {
	d, _, _ := e2Rig(t, pool.Config{Min: 2, Max: 2}, nil)
	addTask(t, d, "t1", "a", "b", "c", "e")
	ctx := context.Background()
	waitUntil(t, 10*time.Second, "四件收口", func() bool {
		e, err := d.B.TaskLedger(ctx, "t1")
		return err == nil && e.NodesDone == 4
	})
	used := map[string]bool{}
	for _, n := range []string{"a", "b", "c", "e"} {
		s, _ := d.B.GetNode(ctx, n)
		used[s.AssignedAgent.String] = true
	}
	if len(used) < 2 {
		t.Fatalf("轮换未铺开:%v", used)
	}
}

// 剧本③:亲和——修订件派回原 worker;原 worker 亡→静默降级新 worker。
func TestS3_AffinityThenDegrade(t *testing.T) {
	d, drv, _ := e2Rig(t, pool.Config{Min: 2, Max: 2}, nil)
	ctx := context.Background()
	addTask(t, d, "t1", "orig")
	waitUntil(t, 10*time.Second, "原件收口", func() bool {
		s, err := d.B.GetNode(ctx, "orig")
		return err == nil && s.Status == "done"
	})
	sOrig, _ := d.B.GetNode(ctx, "orig")
	origWorker := sOrig.AssignedAgent.String
	// 修订件继承 origin→派回原 worker
	d.B.AddNode(ctx, "rev1", "t1", 10, "ready", nil)
	d.D.SetOriginHint("rev1", origWorker)
	waitUntil(t, 10*time.Second, "修订件收口", func() bool {
		s, err := d.B.GetNode(ctx, "rev1")
		return err == nil && s.Status == "done"
	})
	s1, _ := d.B.GetNode(ctx, "rev1")
	if s1.AssignedAgent.String != origWorker {
		t.Fatalf("链① 未派回:%s vs %s", s1.AssignedAgent.String, origWorker)
	}
	// 原 worker 真死→第二修订件静默降级
	for _, w := range drv.Workers() {
		if w.ID() == origWorker {
			w.Kill9()
		}
	}
	d.B.AddNode(ctx, "rev2", "t1", 11, "ready", nil)
	d.D.SetOriginHint("rev2", origWorker)
	waitUntil(t, 10*time.Second, "降级收口", func() bool {
		s, err := d.B.GetNode(ctx, "rev2")
		return err == nil && s.Status == "done"
	})
	s2, _ := d.B.GetNode(ctx, "rev2")
	if s2.AssignedAgent.String == origWorker {
		t.Fatal("死 worker 不得再被派回")
	}
}

// 剧本④:failover 收口——在飞 worker SIGKILL,首投丢账,换代后新 worker 完成(全自动到 done)。
func TestS4_FailoverToCompletion(t *testing.T) {
	beh := statefulBehavior(map[string]WireBehavior{"n1": {DelayMS: 60_000}}) // 首投:挂死般慢
	d, drv, _ := e2Rig(t, pool.Config{Min: 2, Max: 2}, beh)
	ctx := context.Background()
	addTask(t, d, "t1", "n1")
	waitUntil(t, 10*time.Second, "在飞", func() bool {
		s, err := d.B.GetNode(ctx, "n1")
		return err == nil && s.Status != "ready" && s.AssignedAgent.Valid
	})
	s, _ := d.B.GetNode(ctx, "n1")
	for _, w := range drv.Workers() {
		if w.ID() == s.AssignedAgent.String {
			w.Kill9()
		}
	}
	waitUntil(t, 15*time.Second, "failover 后全自动收口", func() bool {
		s, err := d.B.GetNode(ctx, "n1")
		return err == nil && s.Status == "done"
	})
	s2, _ := d.B.GetNode(ctx, "n1")
	if s2.Generation < 2 {
		t.Fatalf("未换代:%+v", s2)
	}
}

// 剧本⑤:mcsd 自身崩溃恢复——首任在途账(首投丢双回执)→守护亡→新任 takeover(epoch+1)→
// 孤儿扫描自动 reassign→本池重派→全自动收口(零人工)。
func TestS5_DaemonCrashRecoveryAutomatic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "b.db")
	beh1 := statefulBehavior(map[string]WireBehavior{"n1": {DropProcessing: true, DropTerminal: true}})
	drv1 := &SubprocDriver{SockDir: t.TempDir(), BehaviorFor: beh1}
	if err := drv1.Listen(); err != nil {
		t.Fatal(err)
	}
	d1, err := New(Config{BoardPath: dbPath, Owner: "boss-a", Pool: pool.Config{Min: 1, Max: 1},
		Tick: 20 * time.Millisecond, SweepEvery: 60 * time.Millisecond, RedeliverAfter: time.Hour}, drv1, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv1.Bind(d1)
	ctx1, cancel1 := context.WithCancel(context.Background())
	go d1.Run(ctx1)
	ctx := context.Background()
	addTask(t, d1, "t1", "n1")
	waitUntil(t, 10*time.Second, "首任在途账(dispatched,双回执被丢)", func() bool {
		s, err := d1.B.GetNode(ctx, "n1")
		return err == nil && s.Status == "dispatched"
	})
	cancel1() // 守护亡(池 worker 一并 SIGTERM;库在盘,账=dispatched 无回执)
	time.Sleep(100 * time.Millisecond)
	drv1.Close()

	drv2 := &SubprocDriver{SockDir: t.TempDir()} // 新任:零注入,正常执行
	if err := drv2.Listen(); err != nil {
		t.Fatal(err)
	}
	d2, err := New(Config{BoardPath: dbPath, Owner: "boss-b", Pool: pool.Config{Min: 1, Max: 1},
		Tick: 20 * time.Millisecond, SweepEvery: 60 * time.Millisecond}, drv2, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv2.Bind(d2)
	if d2.ID.Epoch != d1.ID.Epoch+1 {
		t.Fatalf("epoch=%d want %d", d2.ID.Epoch, d1.ID.Epoch+1)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go d2.Run(ctx2)
	waitUntil(t, 15*time.Second, "新任孤儿扫描→自动收口(零人工)", func() bool {
		s, err := d2.B.GetNode(ctx, "n1")
		return err == nil && s.Status == "done"
	})
	s, _ := d2.B.GetNode(ctx, "n1")
	if s.Generation < 2 {
		t.Fatalf("孤儿未换代:%+v", s)
	}
	// 旧任身份被 fence
	if oc, _ := d2.B.SetBlocker(ctx, "n1", s.Generation, "timeout", d1.ID); oc != board.OutcomeCASMiss {
		t.Fatal("旧 epoch 未被 fence")
	}
}

// 剧本⑥:投递故障——首投丢双回执→redeliver(同 rid)→执行点幂等补回执→收口;delivery_attempt≥1。
func TestS6_LostReceiptsRedeliverConverges(t *testing.T) {
	beh := statefulBehavior(map[string]WireBehavior{"n1": {DropProcessing: true, DropTerminal: true}})
	drv := &SubprocDriver{SockDir: t.TempDir(), BehaviorFor: beh}
	if err := drv.Listen(); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{BoardPath: filepath.Join(t.TempDir(), "b.db"), Owner: "s6",
		Pool: pool.Config{Min: 1, Max: 1},
		Tick: 20 * time.Millisecond, SweepEvery: 60 * time.Millisecond,
		RedeliverAfter: 1 * time.Second}, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv.Bind(d)
	ctxr, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctxr)
	ctx := context.Background()
	addTask(t, d, "t1", "n1")
	waitUntil(t, 15*time.Second, "redeliver 收口", func() bool {
		s, err := d.B.GetNode(ctx, "n1")
		return err == nil && s.Status == "done"
	})
	s, _ := d.B.GetNode(ctx, "n1")
	if s.Generation != 1 {
		t.Fatalf("redeliver 不换代:%+v", s)
	}
	if n, _ := d.B.Events(ctx, "n1", "node_redelivered"); n < 1 {
		t.Fatalf("redelivered=%d want ≥1", n)
	}
}

// 剧本⑦:steer 栅栏 + interrupt B 模型(账面级,跨真栈)。
func TestS7_SteerBarrierAndInterruptCross(t *testing.T) {
	beh := func(node string) WireBehavior {
		if node == "long1" || node == "long2" {
			return WireBehavior{DelayMS: 1500}
		}
		return WireBehavior{}
	}
	d, _, _ := e2Rig(t, pool.Config{Min: 2, Max: 2}, beh)
	ctx := context.Background()
	addTask(t, d, "t1", "long1", "long2")
	waitUntil(t, 10*time.Second, "双件 running", func() bool {
		a, e1 := d.B.GetNode(ctx, "long1")
		b2, e2 := d.B.GetNode(ctx, "long2")
		return e1 == nil && e2 == nil && a.Status == "running" && b2.Status == "running"
	})
	// steer:预留→栅栏(reassign 拒)→确证注入 finalize applied→事件
	if oc, _ := d.B.SteerReserve(ctx, "long1", 1, "steer-1", 60, d.ID); oc != "pending" {
		t.Fatalf("reserve=%s", oc)
	}
	if oc, _ := d.B.Reassign(ctx, "long1", 1, "", d.ID); oc != board.OutcomeCASMiss {
		t.Fatal("steer 未决应拦 reassign")
	}
	if oc, _ := d.B.SteerFinalize(ctx, "steer-1", true, d.ID); oc != board.OutcomeApplied {
		t.Fatal("finalize")
	}
	// interrupt:intent→不拦 reassign(穿越)→旧代 finalize applied 事件记旧代
	if oc, _ := d.B.InterruptIntent(ctx, "long2", 1, "int-1", 60, d.ID); oc != "pending" {
		t.Fatal("intent")
	}
	if oc, _ := d.B.Reassign(ctx, "long2", 1, "", d.ID); oc != board.OutcomeApplied {
		t.Fatal("interrupt 未决不得拦 reassign")
	}
	if oc, _ := d.B.InterruptFinalize(ctx, "int-1", true, d.ID); oc != board.OutcomeApplied {
		t.Fatal("旧代 intent finalize")
	}
	// 两件最终收口:long1 由原投递完成;long2 换代后由守护重派完成
	waitUntil(t, 15*time.Second, "双件收口", func() bool {
		a, e1 := d.B.GetNode(ctx, "long1")
		b2, e2 := d.B.GetNode(ctx, "long2")
		return e1 == nil && e2 == nil && a.Status == "done" && b2.Status == "done"
	})
	if n, _ := d.B.Events(ctx, "long1", "node_steered"); n != 1 {
		t.Fatal("node_steered")
	}
	if n, _ := d.B.Events(ctx, "long2", "node_interrupted"); n != 1 {
		t.Fatal("node_interrupted")
	}
}

// 剧本⑧:治理——缺角色建单拒;全链后台账/RoleStats/零匿名齐。
func TestS8_GovernanceLedger(t *testing.T) {
	d, _, _ := e2Rig(t, pool.Config{Min: 1, Max: 1}, nil)
	ctx := context.Background()
	bad := testRoles()
	bad.QAOwner = board.RoleBinding{}
	if err := d.B.CreateTask(ctx, "t-bad", d.Cfg.BoardID, "x", bad, d.ID); err == nil {
		t.Fatal("缺角色建单应拒")
	}
	addTask(t, d, "t1", "a", "b")
	waitUntil(t, 10*time.Second, "收口", func() bool {
		e, err := d.B.TaskLedger(ctx, "t1")
		return err == nil && e.NodesDone == 2 && e.EndedTS != 0
	})
	e, _ := d.B.TaskLedger(ctx, "t1")
	if len(e.Roles) != 4 || e.Dispatcher == "" || e.StartedTS == 0 {
		t.Fatalf("台账:%+v", e)
	}
	stats, err := d.B.RoleStats(ctx)
	if err != nil || len(stats) == 0 {
		t.Fatalf("stats=%v err=%v", stats, err)
	}
	var anon int
	if err := queryInt(d, `SELECT COUNT(*) FROM board_event WHERE agent_id IS NULL OR agent_id=''`, &anon); err != nil {
		t.Fatal(err)
	}
	if anon != 0 {
		t.Fatalf("匿名事件=%d", anon)
	}
}

// queryInt 剧本断言辅助(只读)。
func queryInt(d *Daemon, q string, out *int) error {
	return d.B.DB().QueryRow(q).Scan(out)
}
