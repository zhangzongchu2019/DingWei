package pool

import (
	"context"
	"fmt"
	"time"
)

// Challenger 应用层 challenge 探活(M4;契约 §3.1 活性=交付进度回执/应用层 ping-pong,非在线状态)。
// 复用既有 dispatch/receipt 链:challenge 信封 probe_seq 的 processing 回执=活性证;零新线格式。
type Challenger struct {
	ProbeCh     *ProbeChannel // 复用 M3 信道(独立超时,零 board 写)
	Send        func(ctx context.Context, workerID string, probeSeq uint64) error
	AwaitResult func(ctx context.Context, workerID string, probeSeq uint64, deadline time.Time) (bool, error)
	Timeout     time.Duration
	degraded    map[string]int
}

// NewChallenger 构造(契约 §3.1 活性判据;send=发 probe=true 信封,await=等该 seq processing 回执)。
func NewChallenger(ch *ProbeChannel, timeout time.Duration) *Challenger {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Challenger{ProbeCh: ch, Timeout: timeout, degraded: map[string]int{}}
}

// Challenge 对 worker 跑一次 challenge,返回 probe 结果(契 §3.1 结果形;degraded 累计在内存,不写 board)。
func (c *Challenger) Challenge(ctx context.Context, workerID string) ProbeResult {
	seq := c.ProbeCh.NextSeq()
	deadline := time.Now().Add(c.Timeout)
	if c.Send != nil {
		if err := c.Send(ctx, workerID, seq); err != nil {
			c.degraded[workerID]++
			r := ProbeResult{WorkerID: workerID, ProbeSeq: seq, Status: "degraded", Reason: "challenge send: " + err.Error(), ObservedAt: time.Now()}
			r.ExpiresAt = r.ObservedAt.Add(c.Timeout)
			return r
		}
	}
	ok := false
	var err error
	if c.AwaitResult != nil {
		ok, err = c.AwaitResult(ctx, workerID, seq, deadline)
	}
	res := ProbeResult{WorkerID: workerID, ProbeSeq: seq, ObservedAt: time.Now()}
	res.ExpiresAt = res.ObservedAt.Add(c.Timeout) // 结果有效期=观测点+一个超时窗(契约 §3.1 expires_at 语义)
	switch {
	case err != nil:
		res.Status = "unknown"
		res.Reason = err.Error()
	case ok:
		res.Status = "ok"
		c.degraded[workerID] = 0
	default:
		res.Status = "degraded"
		res.Reason = "challenge timeout: no processing receipt within " + c.Timeout.String()
		c.degraded[workerID]++
	}
	return res
}

// DegradedCount 连续 degraded 次数(供 OnWorkerDead 阈值判定)。
func (c *Challenger) DegradedCount(workerID string) int { return c.degraded[workerID] }

// NextSeq 供 Challenger 取 probe_seq(单调)。
func (c *ProbeChannel) NextSeq() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

// probeEnvelope 构造 challenge 信封线格式(复用 mcs_dispatch_v1,payload.probe=true)。
func probeEnvelope(nodeID string, gen int64, rid string, probeSeq uint64) Envelope {
	return Envelope{
		NodeID:     nodeID,
		Generation: gen,
		RID:        fmt.Sprintf("probe-%d-%s", probeSeq, rid),
		Payload:    []byte(fmt.Sprintf(`{"probe":true,"probe_seq":%d}`, probeSeq)),
	}
}

// ProbeStateCache 旧 probe_seq 不覆盖新状态(契约 §3.1 明文)。
type ProbeStateCache struct {
	latest map[string]ProbeResult
}

func NewProbeStateCache() *ProbeStateCache { return &ProbeStateCache{latest: map[string]ProbeResult{}} }

// Record 只在 probe_seq 更新时覆盖。
func (c *ProbeStateCache) Record(r ProbeResult) bool {
	prev, ok := c.latest[r.WorkerID]
	if ok && r.ProbeSeq <= prev.ProbeSeq {
		return false // 旧 seq 忽略
	}
	c.latest[r.WorkerID] = r
	return true
}

func (c *ProbeStateCache) Get(workerID string) (ProbeResult, bool) {
	r, ok := c.latest[workerID]
	return r, ok
}
