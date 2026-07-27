package pool

import (
	"context"
	"time"
)

// Snapshot 原子读池状态(契约 §3.1 同快照决策)。
func (p *Pool) Snapshot() PoolSnapshot {
	p.mu.Lock()
	workers := make([]WorkerHealth, 0, len(p.entries))
	idle := 0
	for _, e := range p.entries {
		st := "busy"
		if e.state == StateIdle {
			st = "idle"
			idle++
		}
		workers = append(workers, WorkerHealth{WorkerID: e.w.ID(), Status: st, ObservedAt: p.clk.Now()})
	}
	max := p.cfg.Max
	p.mu.Unlock()
	seq, at := p.snap.next()
	return PoolSnapshot{Seq: seq, ObservedAt: at, Workers: workers, Capacity: max, Idle: idle}
}

// Acquire 类型化择人(契约 §3.1 SpawnOutcome;M3 新面,Pick 保留兼容)。
func (p *Pool) Acquire(ctx context.Context, preferID string) SpawnOutcome {
	snap := p.Snapshot()
	if p.stopped {
		return SpawnOutcome{Kind: SpawnNoHealthyWorker, PoolSnapshotSeq: snap.Seq, ObservedAt: snap.ObservedAt, Reason: ErrStopped.Error()}
	}
	// 同快照决策:idle 判定全部取自 snap,不再二次读池(防竞态)。
	if snap.Idle > 0 {
		// 有空闲:走既有 Pick 链(亲和+round-robin)
		w, ok, err := p.Pick(ctx, preferID)
		if err != nil {
			return SpawnOutcome{Kind: SpawnNoHealthyWorker, PoolSnapshotSeq: snap.Seq, ObservedAt: snap.ObservedAt, Reason: err.Error()}
		}
		if ok {
			return SpawnOutcome{Kind: SpawnOK, Worker: w, PoolSnapshotSeq: snap.Seq, ObservedAt: snap.ObservedAt}
		}
	}
	// 无空闲:未达上限→扩(I1)
	total, _ := p.Size()
	if total < p.cfg.Max {
		if err := p.spawnLocked2(ctx); err != nil {
			return SpawnOutcome{Kind: SpawnNoHealthyWorker, PoolSnapshotSeq: snap.Seq, ObservedAt: snap.ObservedAt, Reason: err.Error()}
		}
		w, ok, err := p.Pick(ctx, preferID)
		if err != nil || !ok {
			return SpawnOutcome{Kind: SpawnNoHealthyWorker, PoolSnapshotSeq: snap.Seq, ObservedAt: snap.ObservedAt, Reason: "post-spawn pick failed"}
		}
		return SpawnOutcome{Kind: SpawnOK, Worker: w, PoolSnapshotSeq: snap.Seq, ObservedAt: snap.ObservedAt}
	}
	// 达上限全忙→all_healthy_busy(有界排队+退避,契 §3.1)
	return SpawnOutcome{
		Kind:            SpawnAllHealthyBusy,
		PoolSnapshotSeq: snap.Seq,
		ObservedAt:      snap.ObservedAt,
		RetryAfter:      2 * time.Second,
		Reason:          "pool at max, all workers busy (bounded queue via Enqueue)",
	}
}

// spawnLocked2 持锁调 spawnLocked(spawnLocked 语义=调用方持 p.mu,与 Start 一致)。
func (p *Pool) spawnLocked2(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.spawnLocked(ctx)
}
