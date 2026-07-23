package dispatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// ---- fakes(与 pool 包 fake 同构;M2 全链用) ----

type fakeWorker struct {
	mu   sync.Mutex
	id   string
	dead bool
	got  []pool.Envelope
}

func (f *fakeWorker) ID() string { return f.id }
func (f *fakeWorker) Health(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead {
		return errors.New("dead")
	}
	return nil
}
func (f *fakeWorker) Caps() pool.Caps { return pool.Caps{"send": true} }
func (f *fakeWorker) Deliver(_ context.Context, env pool.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, env)
	return nil
}
func (f *fakeWorker) Stop(context.Context) error { return nil }

type fakeDriver struct {
	mu      sync.Mutex
	n       int
	spawned []*fakeWorker
}

func (d *fakeDriver) Spawn(context.Context) (pool.Worker, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	w := &fakeWorker{id: fmt.Sprintf("w%d", d.n)}
	d.n++
	d.spawned = append(d.spawned, w)
	return w, nil
}

func ridSeq() RIDGen {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("rid-%d", n)
	}
}

func rig(t *testing.T) (*Dispatcher, *fakeDriver, *board.Board, context.Context) {
	t.Helper()
	ctx := context.Background()
	b, err := board.Open(":memory:", &clock.Fake{T: time.Unix(10_000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	epoch, err := b.Takeover(ctx, "board1", "orch1", 3600)
	if err != nil {
		t.Fatal(err)
	}
	id := board.Identity{Owner: "orch1", Epoch: epoch}
	if err := b.CreateTask(ctx, "t1", "board1", "任务"); err != nil {
		t.Fatal(err)
	}
	if err := b.AddNode(ctx, "n1", "t1", 1, "ready", nil); err != nil {
		t.Fatal(err)
	}
	drv := &fakeDriver{}
	p := pool.New(pool.Config{Min: 2, Max: 3}, drv, nil)
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(ctx) })
	d := New(p, b, id, ridSeq())
	p.OnWorkerDead = func(workerID string) { _ = d.OnWorkerDead(ctx, workerID) }
	return d, drv, b, ctx
}

// 派发主链:择人→记账(dispatched)→外呼恰一次。
func TestDispatchOne_AccountThenDeliver(t *testing.T) {
	d, drv, b, ctx := rig(t)
	wid, rid, err := d.DispatchOne(ctx, "n1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wid != "w0" || rid != "rid-1" {
		t.Fatalf("wid=%s rid=%s", wid, rid)
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "dispatched" || s.AssignedAgent.String != "w0" || s.DeliveryID.String != "rid-1" {
		t.Fatalf("账错:%+v", s)
	}
	if len(drv.spawned[0].got) != 1 || drv.spawned[0].got[0].RID != "rid-1" {
		t.Fatalf("外呼错:%+v", drv.spawned[0].got)
	}
}

// ★J2 全链(M2 门):health 挂→reap→OnWorkerDead→guarded reassign(gen+1)→重派新 worker(新 rid)→
// 旧 rid 迟到 terminal 拒(幂等不双投),新 rid 正常销账。
func TestJ2_FullChain_DeadWorkerReassignRedeliver(t *testing.T) {
	d, drv, b, ctx := rig(t)
	if _, _, err := d.DispatchOne(ctx, "n1", 1); err != nil { // →w0, rid-1
		t.Fatal(err)
	}
	drv.spawned[0].mu.Lock()
	drv.spawned[0].dead = true
	drv.spawned[0].mu.Unlock()
	d.P.Reap(ctx) // 判死→回调 OnWorkerDead→换代重派
	s, _ := b.GetNode(ctx, "n1")
	if s.Generation != 2 || s.Status != "dispatched" {
		t.Fatalf("未换代重派:%+v", s)
	}
	newWorker, newRID := s.AssignedAgent.String, s.DeliveryID.String
	if newWorker == "w0" {
		t.Fatal("不得重派回死 worker")
	}
	if newRID == "rid-1" {
		t.Fatal("换代必须新 rid")
	}
	// 旧 rid 迟到终结:拒,零副作用(不双投的账面保证=旧投递收不了账)
	oc, _ := b.Terminal(ctx, "n1", 2, "rid-1", true, "", d.ID)
	if oc != board.OutcomeCASMiss {
		t.Fatalf("旧 rid terminal=%s want cas_miss", oc)
	}
	// 新链正常收敛
	if oc, _ := b.Processing(ctx, "n1", 2, newRID, d.ID); oc != board.OutcomeApplied {
		t.Fatalf("processing=%s", oc)
	}
	if oc, _ := b.Terminal(ctx, "n1", 2, newRID, true, "ref", d.ID); oc != board.OutcomeApplied {
		t.Fatalf("terminal=%s", oc)
	}
	// 事件账:恰一次 reassign,两次 dispatch(gen1+gen2)
	if n, _ := b.Events(ctx, "n1", "node_reassigned"); n != 1 {
		t.Fatalf("reassigned=%d want 1", n)
	}
	if n, _ := b.Events(ctx, "n1", "node_dispatched"); n != 2 {
		t.Fatalf("dispatched=%d want 2", n)
	}
}

// A案链①(M3 前置):同 task 修订节点 prefer 原 worker;原 worker 忙→静默落链②。
func TestAffinity_OriginPreferredWhenIdle(t *testing.T) {
	d, _, b, ctx := rig(t)
	if _, _, err := d.DispatchOne(ctx, "n1", 1); err != nil { // n1→w0
		t.Fatal(err)
	}
	// n1 完成,w0 归还
	s, _ := b.GetNode(ctx, "n1")
	b.Processing(ctx, "n1", 1, s.DeliveryID.String, d.ID)
	b.Terminal(ctx, "n1", 1, s.DeliveryID.String, true, "", d.ID)
	d.P.Release("w0")
	// 修订节点 rev1(deps 空,直接 ready;origin 提示继承自 n1 由上层置——此处直接演练 prefer 路径)
	if err := b.AddNode(ctx, "rev1", "t1", 3, "ready", nil); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.origin["rev1"] = "w0"
	d.mu.Unlock()
	wid, _, err := d.DispatchOne(ctx, "rev1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wid != "w0" {
		t.Fatalf("链① 应派回 w0,got %s", wid)
	}
}

// 池满全忙→入缓冲不丢(I4 在 dispatch 层的表现)。
func TestDispatch_FullPoolBuffers(t *testing.T) {
	d, _, b, ctx := rig(t)
	for i := 0; i < 3; i++ { // 占满 Max=3
		nid := fmt.Sprintf("busy%d", i)
		if err := b.AddNode(ctx, nid, "t1", 10+i, "ready", nil); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.DispatchOne(ctx, nid, 1); err != nil {
			t.Fatal(err)
		}
	}
	wid, rid, err := d.DispatchOne(ctx, "n1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if wid != "" || rid != "" {
		t.Fatalf("满池应入缓冲,got %s/%s", wid, rid)
	}
	if d.P.QueueLen() != 1 {
		t.Fatalf("queue=%d want 1", d.P.QueueLen())
	}
	s, _ := b.GetNode(ctx, "n1")
	if s.Status != "ready" {
		t.Fatalf("缓冲不得动账:%+v", s)
	}
}
