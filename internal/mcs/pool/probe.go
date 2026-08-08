package pool

import (
	"context"
	"sync"
	"time"
)

// ProbeChannel 单角色探活信道(契约 §3.1:SS 探活独立于 SA 信道/超时,防相关性致盲;SS 探活不经 board 写——本通道只写内存缓存+日志,无任何权威账写路径)。
type ProbeChannel struct {
	Role    string        // "SA" | "SS"
	Timeout time.Duration // 独立超时配置
	now     func() time.Time
	lastOK  time.Time
	lastErr error
	seq     uint64
	mu      sync.Mutex
}

// NewProbeChannel 构造独立信道(角色必填,超时独立)。
func NewProbeChannel(role string, timeout time.Duration, now func() time.Time) *ProbeChannel {
	if now == nil {
		now = time.Now
	}
	return &ProbeChannel{Role: role, Timeout: timeout, now: now}
}

// ProbeResult 探活结果(契约 §3.1 probe 结果形)。
type ProbeResult struct {
	WorkerID   string
	ProbeSeq   uint64
	Status     string // ok|degraded|unknown
	Reason     string
	ObservedAt time.Time
	ExpiresAt  time.Time
}

// Probe 执行一次探活(纯内存,不写 board)。probeFn=角色侧活性判据(readonly presence 或 M4 challenge)。
func (c *ProbeChannel) Probe(ctx context.Context, workerID string, probeFn func(ctx context.Context) error) ProbeResult {
	c.seq++
	at := c.now()
	res := ProbeResult{WorkerID: workerID, ProbeSeq: c.seq, ObservedAt: at, ExpiresAt: at.Add(c.Timeout)}
	if err := probeFn(ctx); err != nil {
		c.lastErr = err
		res.Status = "degraded"
		res.Reason = err.Error()
		return res
	}
	c.lastOK = at
	c.lastErr = nil
	res.Status = "ok"
	return res
}
