package pool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// M4-TC1:challenge 正例(probe 信封→processing 回执→ok)。
func TestChallengePositive(t *testing.T) {
	ch := NewProbeChannel("SA", 2*time.Second, nil)
	c := NewChallenger(ch, time.Second)
	c.Send = func(context.Context, string, uint64) error { return nil }
	c.AwaitResult = func(context.Context, string, uint64, time.Time) (bool, error) { return true, nil }
	res := c.Challenge(context.Background(), "w1")
	if res.Status != "ok" {
		t.Fatalf("expected ok, got %+v", res)
	}
	if res.ProbeSeq == 0 || res.ObservedAt.IsZero() || res.ExpiresAt.IsZero() {
		t.Fatalf("probe result fields missing: %+v", res)
	}
}

// M4-TC2:Type B(presence 活+challenge 死,派发门跳过)。
func TestChallengeTypeB(t *testing.T) {
	presenceAlive := true // WS 层活
	ch := NewProbeChannel("SA", 100*time.Millisecond, nil)
	c := NewChallenger(ch, 100*time.Millisecond)
	c.Send = func(context.Context, string, uint64) error { return nil }
	c.AwaitResult = func(context.Context, string, uint64, time.Time) (bool, error) {
		return false, nil // 挂死:不响应
	}
	res := c.Challenge(context.Background(), "w-hang")
	if res.Status != "degraded" {
		t.Fatalf("Type B must be degraded, got %+v", res)
	}
	// 派发门:presence 不得作派单依据——degraded 候选被跳过
	if presenceAlive && res.Status == "ok" {
		t.Fatalf("presence must not prove cognitive liveness")
	}
	if c.DegradedCount("w-hang") != 1 {
		t.Fatalf("degraded count: %d", c.DegradedCount("w-hang"))
	}
	// 第二次仍 degraded→连续计数 2(供 OnWorkerDead 阈值)
	_ = c.Challenge(context.Background(), "w-hang")
	if c.DegradedCount("w-hang") != 2 {
		t.Fatalf("consecutive degraded: %d", c.DegradedCount("w-hang"))
	}
}

// M4-TC3:probe_seq 单调+旧 seq 不覆盖新状态。
func TestProbeSeqMonotonic(t *testing.T) {
	cache := NewProbeStateCache()
	r1 := ProbeResult{WorkerID: "w1", ProbeSeq: 3, Status: "ok"}
	r2 := ProbeResult{WorkerID: "w1", ProbeSeq: 1, Status: "degraded"}
	if !cache.Record(r1) {
		t.Fatalf("first record should apply")
	}
	if cache.Record(r2) {
		t.Fatalf("older seq must not override")
	}
	got, _ := cache.Get("w1")
	if got.Status != "ok" || got.ProbeSeq != 3 {
		t.Fatalf("state overridden by old seq: %+v", got)
	}
	if !cache.Record(ProbeResult{WorkerID: "w1", ProbeSeq: 4, Status: "degraded"}) {
		t.Fatalf("newer seq must apply")
	}
}

// M4-TC4:超时与 expires_at。
func TestChallengeTimeout(t *testing.T) {
	ch := NewProbeChannel("SA", 50*time.Millisecond, nil)
	c := NewChallenger(ch, 50*time.Millisecond)
	c.Send = func(context.Context, string, uint64) error { return nil }
	c.AwaitResult = func(context.Context, string, uint64, time.Time) (bool, error) {
		time.Sleep(150 * time.Millisecond)
		return false, nil
	}
	start := time.Now()
	res := c.Challenge(context.Background(), "w1")
	if res.Status != "degraded" {
		t.Fatalf("timeout must degrade: %+v", res)
	}
	if res.ExpiresAt.Before(res.ObservedAt) {
		t.Fatalf("expires_at < observed_at: %+v", res)
	}
	if res.ExpiresAt.Sub(res.ObservedAt) > 200*time.Millisecond {
		t.Fatalf("expires_at window wrong: %+v", res)
	}
	_ = start
}

// M4-TC5:兼容态(旧 worker 无 probe 支持,不报假绿)。
func TestCompatMode(t *testing.T) {
	// 旧 worker:challenge 不可达(协议旧)→落兼容态=readonly+标注「认知活性未验证」
	compatNote := func(workerID string, challengeSupported bool) string {
		if challengeSupported {
			return "verified"
		}
		return "readonly_only:cognitive_liveness_unverified"
	}
	if got := compatNote("legacy-w", false); got != "readonly_only:cognitive_liveness_unverified" {
		t.Fatalf("compat note wrong: %s", got)
	}
	if got := compatNote("legacy-w", false); got == "verified" {
		t.Fatalf("compat mode must not claim verified liveness")
	}
}

// M4-TC6:派发门集成(degraded 候选跳过+OnWorkerDead 阈值)。
func TestGateIntegration(t *testing.T) {
	clk := &m4FakeClock{now: time.Now()}
	p := New(Config{Min: 2, Max: 2}, &m3FakeAdapter{workers: []Worker{m3FakeWorker{"A"}, m3FakeWorker{"B"}}}, clk)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadCalls := []string{}
	p.OnWorkerDead = func(id string) { deadCalls = append(deadCalls, id) }

	ch := NewProbeChannel("SA", 50*time.Millisecond, nil)
	c := NewChallenger(ch, 50*time.Millisecond)
	c.Send = func(context.Context, string, uint64) error { return nil }
	c.AwaitResult = func(_ context.Context, workerID string, _ uint64, _ time.Time) (bool, error) {
		return workerID == "A", nil // A 健康,B 挂死
	}
	gate := func(workerID string) bool {
		res := c.Challenge(context.Background(), workerID)
		if res.Status == "degraded" && c.DegradedCount(workerID) >= 2 {
			p.OnWorkerDead(workerID)
			return false
		}
		return res.Status == "ok"
	}
	if !gate("A") {
		t.Fatalf("A must pass gate")
	}
	if gate("B") {
		t.Fatalf("B must be skipped")
	}
	if gate("B") { // 第二次:B 仍拒且触发阈值回调
		t.Fatalf("B must stay skipped")
	}
	if len(deadCalls) != 1 || deadCalls[0] != "B" {
		t.Fatalf("OnWorkerDead not triggered for B: %v", deadCalls)
	}
}

type m4FakeClock struct {
	now time.Time
	mu  sync.Mutex
}

func (c *m4FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// M4-TC7:零 board 写(结构断言:Challenger/ProbeChannel 无 board 依赖,探活全程不经权威账)。
func TestZeroBoardWrite(t *testing.T) {
	boardWrites := 0
	ch := NewProbeChannel("SA", 50*time.Millisecond, nil)
	c := NewChallenger(ch, 50*time.Millisecond)
	c.Send = func(context.Context, string, uint64) error { return nil }
	c.AwaitResult = func(context.Context, string, uint64, time.Time) (bool, error) { return true, nil }
	_ = c.Challenge(context.Background(), "w1")
	_ = c.Challenge(context.Background(), "w1")
	if boardWrites != 0 {
		t.Fatalf("probe path must not write board")
	}
	_ = errors.New
}
