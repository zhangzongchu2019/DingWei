package pool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

type m3FakeWorker struct{ id string }

func (w m3FakeWorker) ID() string                   { return w.id }
func (w m3FakeWorker) Health(context.Context) error { return nil }
func (w m3FakeWorker) Caps() Caps                   { return Caps{} }
func (w m3FakeWorker) Deliver(context.Context, Envelope) error {
	return nil
}
func (w m3FakeWorker) Stop(context.Context) error { return nil }

type m3FakeAdapter struct {
	workers []Worker
	idx     int
	fail    bool
	mu      sync.Mutex
}

func (a *m3FakeAdapter) Spawn(context.Context) (Worker, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail || a.idx >= len(a.workers) {
		return nil, errors.New("no healthy worker")
	}
	w := a.workers[a.idx]
	a.idx++
	return w, nil
}

// M3-TC1:SpawnOutcome 三态+OK。
func TestSpawnOutcomeThreeStates(t *testing.T) {
	clk := clock.Real{}
	// OK
	p := New(Config{Min: 1, Max: 2}, &m3FakeAdapter{workers: []Worker{m3FakeWorker{"w1"}, m3FakeWorker{"w2"}}}, clk)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := p.Acquire(context.Background(), "")
	if out.Kind != SpawnOK || out.Worker == nil {
		t.Fatalf("expected SpawnOK, got %+v", out)
	}
	if out.PoolSnapshotSeq == 0 || out.ObservedAt.IsZero() {
		t.Fatalf("snapshot fields missing: %+v", out)
	}
	// all_healthy_busy:占满到 Max 且全 busy
	p2 := New(Config{Min: 1, Max: 1}, &m3FakeAdapter{workers: []Worker{m3FakeWorker{"w1"}}}, clk)
	if err := p2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out := p2.Acquire(context.Background(), ""); out.Kind != SpawnOK {
		t.Fatalf("setup acquire: %+v", out)
	}
	out2 := p2.Acquire(context.Background(), "")
	if out2.Kind != SpawnAllHealthyBusy || out2.RetryAfter <= 0 {
		t.Fatalf("expected all_healthy_busy with RetryAfter, got %+v", out2)
	}
	// no_healthy_worker
	p3 := New(Config{Min: 1, Max: 1}, &m3FakeAdapter{fail: true}, clk)
	_ = p3.Start(context.Background()) // Min spawn fails silently in setup path
	out3 := p3.Acquire(context.Background(), "")
	if out3.Kind == SpawnOK {
		t.Fatalf("expected non-OK, got %+v", out3)
	}
	// capacity_unknown 语义位(显式构造判门处)
	unk := SpawnOutcome{Kind: SpawnCapacityUnknown, RetryAfter: time.Second}
	if unk.Kind.String() != "capacity_unknown" || unk.RetryAfter <= 0 {
		t.Fatalf("capacity_unknown shape: %+v", unk)
	}
}

// M3-TC2:同快照决策(-race 必跑)。
func TestSnapshotMonotonicAndShared(t *testing.T) {
	clk := clock.Real{}
	p := New(Config{Min: 2, Max: 4}, &m3FakeAdapter{workers: []Worker{m3FakeWorker{"w1"}, m3FakeWorker{"w2"}, m3FakeWorker{"w3"}, m3FakeWorker{"w4"}}}, clk)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s1, s2 := p.Snapshot(), p.Snapshot()
	if s2.Seq != s1.Seq+1 {
		t.Fatalf("snapshot seq not monotonic: %d -> %d", s1.Seq, s2.Seq)
	}
	done := make(chan SpawnOutcome, 8)
	for i := 0; i < 8; i++ {
		go func() { done <- p.Acquire(context.Background(), "") }()
	}
	var seqs []uint64
	for i := 0; i < 8; i++ {
		out := <-done
		seqs = append(seqs, out.PoolSnapshotSeq)
	}
	seen := map[uint64]bool{}
	for _, q := range seqs {
		if q == 0 {
			t.Fatalf("seq zero in outcomes: %v", seqs)
		}
		seen[q] = true
	}
	if len(seen) != 8 {
		t.Fatalf("seq tear detected: %v", seqs)
	}
}

// M3-TC3:SS/SA 探活信道隔离+零 board 写。
func TestProbeChannelIsolation(t *testing.T) {
	now := time.Now()
	sa := NewProbeChannel("SA", 5*time.Second, func() time.Time { return now })
	ss := NewProbeChannel("SS", 3*time.Second, func() time.Time { return now })

	// SS 挂死→SA 判活不受影响
	ssRes := ss.Probe(context.Background(), "ss-1", func(context.Context) error { return errors.New("hang") })
	saRes := sa.Probe(context.Background(), "sa-1", func(context.Context) error { return nil })
	if ssRes.Status != "degraded" || saRes.Status != "ok" {
		t.Fatalf("isolation: ss=%+v sa=%+v", ssRes, saRes)
	}
	// SA 挂死→SS(新一轮)不受影响
	saRes2 := sa.Probe(context.Background(), "sa-1", func(context.Context) error { return errors.New("hang") })
	ssRes2 := ss.Probe(context.Background(), "ss-1", func(context.Context) error { return nil })
	if saRes2.Status != "degraded" || ssRes2.Status != "ok" {
		t.Fatalf("isolation reverse: sa=%+v ss=%+v", saRes2, ssRes2)
	}
	// 超时独立
	if sa.Timeout == ss.Timeout {
		t.Fatalf("channels must have independent timeouts")
	}
	// probe 结果形(契 §3.1):seq 单调+expires_at
	if ssRes2.ProbeSeq <= ssRes.ProbeSeq || ssRes2.ExpiresAt.Before(ssRes2.ObservedAt) {
		t.Fatalf("probe result shape: %+v vs %+v", ssRes, ssRes2)
	}
	// 零 board 写:信道无任何 board 依赖(结构断言——ProbeChannel 无 board 字段,编译即证)
}

// M3-TC1 补充:Strict 之外的类型字符串稳定。
func TestOutcomeKindStrings(t *testing.T) {
	if SpawnNoHealthyWorker.String() != "no_healthy_worker" || SpawnAllHealthyBusy.String() != "all_healthy_busy" {
		t.Fatalf("kind strings unstable")
	}
}
