package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

// ---- fakes ----

type fakeWorker struct {
	mu      sync.Mutex
	id      string
	dead    bool
	stopped bool
	caps    Caps
	got     []Envelope
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
func (f *fakeWorker) Caps() Caps { return f.caps }
func (f *fakeWorker) Deliver(_ context.Context, env Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, env)
	return nil
}
func (f *fakeWorker) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return nil
}

type fakeDriver struct {
	mu      sync.Mutex
	n       int
	caps    []Caps // 依次分配给第 n 个 spawn;超出用最后一个
	spawned []*fakeWorker
}

func (d *fakeDriver) Spawn(context.Context) (Worker, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := Caps{"send": true, "steer": true}
	if len(d.caps) > 0 {
		if d.n < len(d.caps) {
			c = d.caps[d.n]
		} else {
			c = d.caps[len(d.caps)-1]
		}
	}
	w := &fakeWorker{id: fmt.Sprintf("w%d", d.n), caps: c}
	d.n++
	d.spawned = append(d.spawned, w)
	return w, nil
}

func newTestPool(t *testing.T, cfg Config, drv *fakeDriver, clk clock.Clock) *Pool {
	t.Helper()
	p := New(cfg, drv, clk)
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	return p
}

// ---- I 组:池伸缩 ----

// I1:有 ready 活、无空闲、未达上限 → 扩容新 worker。
func TestI1_ScaleUpWhenBusyBelowMax(t *testing.T) {
	drv := &fakeDriver{}
	p := newTestPool(t, Config{Min: 2, Max: 5}, drv, nil)
	ctx := context.Background()
	// 占满两个常驻
	for i := 0; i < 2; i++ {
		if _, ok, err := p.Pick(ctx, ""); err != nil || !ok {
			t.Fatalf("pick %d: ok=%v err=%v", i, ok, err)
		}
	}
	// 第三单:应扩容(总数 3)
	w, ok, err := p.Pick(ctx, "")
	if err != nil || !ok {
		t.Fatalf("scale-up pick: ok=%v err=%v", ok, err)
	}
	if total, _ := p.Size(); total != 3 {
		t.Fatalf("total=%d want 3 (I1 扩容)", total)
	}
	if w.ID() != "w2" {
		t.Fatalf("new worker id=%s want w2", w.ID())
	}
}

// I2:idle 超 TTL 且高于下限 → 缩;I3:不缩破下限。
func TestI2I3_ShrinkIdleTTLButKeepMin(t *testing.T) {
	drv := &fakeDriver{}
	fc := &clock.Fake{T: time.Unix(1000, 0)}
	p := newTestPool(t, Config{Min: 2, Max: 5, IdleTTL: time.Minute}, drv, fc)
	ctx := context.Background()
	// 扩到 4:占 2 常驻+2 扩容,然后全部归还
	var ids []string
	for i := 0; i < 4; i++ {
		w, ok, err := p.Pick(ctx, "")
		if err != nil || !ok {
			t.Fatalf("pick: %v %v", ok, err)
		}
		ids = append(ids, w.ID())
	}
	for _, id := range ids {
		p.Release(id)
	}
	fc.Advance(2 * time.Minute) // 全体 idle 超 TTL
	p.Shrink(ctx)
	total, idle := p.Size()
	if total != 2 || idle != 2 {
		t.Fatalf("after shrink total=%d idle=%d want 2/2 (I2 缩容+I3 保下限)", total, idle)
	}
	p.Shrink(ctx) // 再缩:不得破下限
	if total, _ := p.Size(); total != 2 {
		t.Fatalf("I3 violated: total=%d want 2", total)
	}
}

// I4:池满全忙 → 有界缓冲,不静默丢;缓冲满 → 显式 ErrQueueFull。
func TestI4_BoundedQueueNoSilentDrop(t *testing.T) {
	drv := &fakeDriver{}
	p := newTestPool(t, Config{Min: 1, Max: 1, QueueCap: 2}, drv, nil)
	ctx := context.Background()
	if _, ok, _ := p.Pick(ctx, ""); !ok {
		t.Fatal("pick")
	}
	if _, ok, err := p.Pick(ctx, ""); ok || err != nil {
		t.Fatalf("满池 pick 应 (nil,false):ok=%v err=%v", ok, err)
	}
	if err := p.Enqueue(Envelope{NodeID: "n1", RID: "r1"}); err != nil {
		t.Fatalf("enqueue1: %v", err)
	}
	if err := p.Enqueue(Envelope{NodeID: "n2", RID: "r2"}); err != nil {
		t.Fatalf("enqueue2: %v", err)
	}
	if err := p.Enqueue(Envelope{NodeID: "n3", RID: "r3"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("第三件应 ErrQueueFull,got %v", err)
	}
	if env, ok := p.DequeueReady(); !ok || env.NodeID != "n1" {
		t.Fatalf("dequeue 应 FIFO 取 n1,got %+v ok=%v", env, ok)
	}
	if p.QueueLen() != 1 {
		t.Fatalf("queue len=%d want 1", p.QueueLen())
	}
}

// I5:池 caps=成员保守交集。
func TestI5_CapsConservativeIntersection(t *testing.T) {
	drv := &fakeDriver{caps: []Caps{
		{"send": true, "steer": true, "interrupt": true},
		{"send": true, "steer": false},
	}}
	p := newTestPool(t, Config{Min: 2, Max: 2}, drv, nil)
	got := p.CapsIntersection()
	if !got["send"] {
		t.Fatal("send 应在交集")
	}
	if got["steer"] || got["interrupt"] {
		t.Fatalf("steer/interrupt 不应在交集:%v", got)
	}
}

// ---- J 组:轮换 / failover ----

// J1:N=2 round_robin 负载交替。
func TestJ1_RoundRobinAlternates(t *testing.T) {
	drv := &fakeDriver{}
	p := newTestPool(t, Config{Min: 2, Max: 2}, drv, nil)
	ctx := context.Background()
	var seq []string
	for i := 0; i < 4; i++ {
		w, ok, err := p.Pick(ctx, "")
		if err != nil || !ok {
			t.Fatalf("pick: %v %v", ok, err)
		}
		seq = append(seq, w.ID())
		p.Release(w.ID())
	}
	want := []string{"w0", "w1", "w0", "w1"}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("J1 轮换序=%v want %v", seq, want)
		}
	}
}

// J2(池层):health 挂 → reap 移除+OnWorkerDead 回调(Board 侧 reassign 在 M2 接)+补拉保下限。
func TestJ2_ReapDeadCallbackAndRefill(t *testing.T) {
	drv := &fakeDriver{}
	p := newTestPool(t, Config{Min: 2, Max: 5}, drv, nil)
	ctx := context.Background()
	var deadIDs []string
	var mu sync.Mutex
	p.OnWorkerDead = func(id string) { mu.Lock(); deadIDs = append(deadIDs, id); mu.Unlock() }
	drv.spawned[0].mu.Lock()
	drv.spawned[0].dead = true
	drv.spawned[0].mu.Unlock()
	p.Reap(ctx)
	mu.Lock()
	if len(deadIDs) != 1 || deadIDs[0] != "w0" {
		t.Fatalf("OnWorkerDead=%v want [w0]", deadIDs)
	}
	mu.Unlock()
	total, idle := p.Size()
	if total != 2 || idle != 2 {
		t.Fatalf("reap 后 total=%d idle=%d want 2/2(补拉保下限)", total, idle)
	}
	// 判死的 w0 不得再被择中
	for i := 0; i < 4; i++ {
		w, ok, _ := p.Pick(ctx, "")
		if !ok {
			break
		}
		if w.ID() == "w0" {
			t.Fatal("死 worker 不得再被择中")
		}
	}
}

// J3:worker 恢复(此实现=补拉新 worker 纳入池;原 worker 恢复由驱动层重 Spawn 表达)。
func TestJ3_RefillRejoinsRotation(t *testing.T) {
	drv := &fakeDriver{}
	p := newTestPool(t, Config{Min: 2, Max: 2}, drv, nil)
	ctx := context.Background()
	drv.spawned[1].mu.Lock()
	drv.spawned[1].dead = true
	drv.spawned[1].mu.Unlock()
	p.Reap(ctx) // w1 出,w2 进
	var seen []string
	for i := 0; i < 2; i++ {
		w, ok, _ := p.Pick(ctx, "")
		if !ok {
			t.Fatal("pick")
		}
		seen = append(seen, w.ID())
		p.Release(w.ID())
	}
	found := false
	for _, id := range seen {
		if id == "w2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("新 worker 未纳入轮换:%v", seen)
	}
}

// ---- W1' A 案链①(亲和 pick 权重;M3 门的先行单测,零等待零新状态) ----

func TestAffinity_PreferIdleElseFallthrough(t *testing.T) {
	drv := &fakeDriver{}
	p := newTestPool(t, Config{Min: 2, Max: 2}, drv, nil)
	ctx := context.Background()
	// 链①命中:w1 idle 时 prefer w1 → 得 w1
	w, ok, _ := p.Pick(ctx, "w1")
	if !ok || w.ID() != "w1" {
		t.Fatalf("链① 应派回 w1,got %v", w)
	}
	// 链①不可用(w1 busy):prefer w1 → 静默落链②(round-robin 的 w0),零等待
	w2, ok, _ := p.Pick(ctx, "w1")
	if !ok || w2.ID() != "w0" {
		t.Fatalf("链① busy 应立即落链②取 w0,got %v ok=%v", w2, ok)
	}
	// prefer 已死/不存在 → 同样静默落链②
	p.Release("w0")
	w3, ok, _ := p.Pick(ctx, "ghost")
	if !ok || w3.ID() != "w0" {
		t.Fatalf("prefer 不存在应落链②,got %v", w3)
	}
}

// 停池后一切入口显式拒绝。
func TestStoppedPoolRejects(t *testing.T) {
	drv := &fakeDriver{}
	p := newTestPool(t, Config{Min: 1, Max: 1}, drv, nil)
	ctx := context.Background()
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, _, err := p.Pick(ctx, ""); !errors.Is(err, ErrStopped) {
		t.Fatalf("pick after stop: %v", err)
	}
	if err := p.Enqueue(Envelope{}); !errors.Is(err, ErrStopped) {
		t.Fatalf("enqueue after stop: %v", err)
	}
	if !drv.spawned[0].stopped {
		t.Fatal("worker 未被 Stop")
	}
}
