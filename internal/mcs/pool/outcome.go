package pool

import (
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

// SpawnOutcomeKind 三态(契约 §3.1:no_healthy_worker 快败/all_healthy_busy 有界排队/capacity_unknown 快照过期)。
type SpawnOutcomeKind int

const (
	SpawnOK SpawnOutcomeKind = iota
	SpawnNoHealthyWorker
	SpawnAllHealthyBusy
	SpawnCapacityUnknown
)

func (k SpawnOutcomeKind) String() string {
	switch k {
	case SpawnOK:
		return "ok"
	case SpawnNoHealthyWorker:
		return "no_healthy_worker"
	case SpawnAllHealthyBusy:
		return "all_healthy_busy"
	case SpawnCapacityUnknown:
		return "capacity_unknown"
	}
	return "unknown"
}

// SpawnOutcome 类型化择人结果(契约 §3.1)。Kind=SpawnOK 时 Worker 非 nil。
type SpawnOutcome struct {
	Kind            SpawnOutcomeKind
	Worker          Worker
	PoolSnapshotSeq uint64
	ObservedAt      time.Time
	RetryAfter      time.Duration
	Reason          string
}

// WorkerHealth 快照内单工人活性(M4 前=presence;observed_at 供 M4 challenge 升级不换代)。
type WorkerHealth struct {
	WorkerID   string
	Status     string // idle|busy|dead
	ObservedAt time.Time
}

// PoolSnapshot 决策快照(契约 §3.1:同一快照决策防 health/capacity 读取竞态;Seq 单调)。
type PoolSnapshot struct {
	Seq        uint64
	ObservedAt time.Time
	Workers    []WorkerHealth
	Capacity   int
	Idle       int
}

type snapshotter struct {
	mu  sync.Mutex
	seq uint64
	clk clock.Clock
}

func (s *snapshotter) next() (uint64, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return s.seq, s.clk.Now()
}
