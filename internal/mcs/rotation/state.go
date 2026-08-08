// Package rotation contains the coordinator-side M5 session rotation state
// machine. It deliberately does not own board authority: takeover and writer
// activation remain guarded board operations from interface-contracts-v1.
package rotation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// Phase is the local, non-authoritative progress of one rotation.
type Phase string

const (
	PhaseIdle       Phase = "IDLE"
	PhasePending    Phase = "ROTATION_PENDING"
	PhaseStaging    Phase = "STAGING"
	PhaseActivating Phase = "ACTIVATING"
	PhaseActive     Phase = "ACTIVE"
	PhaseRecovery   Phase = "RECOVERY"
	PhaseReaped     Phase = "REAPED"
)

// Event names the only transitions accepted by the first M5 state slice.
type Event string

const (
	EventTriggerUsage   Event = "trigger_usage"
	EventSpawnStaging   Event = "spawn_staging"
	EventRevokeStaging  Event = "revoke_staging"
	EventTakeover       Event = "takeover_applied"
	EventActivate       Event = "activate_applied"
	EventActivateFailed Event = "activate_failed"
	EventStagingTimeout Event = "staging_timeout"
)

var ErrIllegalTransition = errors.New("illegal rotation transition")

// Next applies the frozen M0/Phase-0 transition semantics without mutating
// board state. In particular takeover stops at ACTIVATING and activation
// failure enters RECOVERY; it never reopens the old writer epoch.
func Next(from Phase, event Event) (Phase, error) {
	if to, ok := transitions[transition{from, event}]; ok {
		return to, nil
	}
	return from, fmt.Errorf("%w: phase=%s event=%s", ErrIllegalTransition, from, event)
}

type transition struct {
	from  Phase
	event Event
}

var transitions = map[transition]Phase{
	{PhaseIdle, EventTriggerUsage}:         PhasePending,
	{PhasePending, EventSpawnStaging}:      PhaseStaging,
	{PhaseStaging, EventRevokeStaging}:     PhasePending,
	{PhaseStaging, EventTakeover}:          PhaseActivating,
	{PhaseActivating, EventActivate}:       PhaseActive,
	{PhaseActivating, EventActivateFailed}: PhaseRecovery,
	{PhaseStaging, EventStagingTimeout}:    PhaseReaped,
}

// Challenger is the M4 application-layer challenge seam reused by M5. It is
// intentionally narrower than worker presence/online-list APIs.
type Challenger interface {
	Challenge(context.Context, string) pool.ProbeResult
}

// ChallengeGate accepts only a fresh, positive application-layer challenge.
// UNKNOWN/degraded/stale results fail closed and therefore cannot activate SB.
type ChallengeGate struct {
	Probe Challenger
	Now   func() time.Time
	mu    sync.Mutex
	last  map[string]uint64
}

func (g *ChallengeGate) Check(ctx context.Context, workerID string) (pool.ProbeResult, error) {
	if g == nil {
		return pool.ProbeResult{}, errors.New("rotation challenge gate is required")
	}
	if g.Probe == nil {
		return pool.ProbeResult{}, errors.New("rotation challenge probe is required")
	}
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	result := g.Probe.Challenge(ctx, workerID)
	if result.WorkerID != workerID {
		return result, fmt.Errorf("challenge worker mismatch: got=%q want=%q", result.WorkerID, workerID)
	}
	if result.Status != "ok" {
		return result, fmt.Errorf("challenge not healthy: status=%s reason=%s", result.Status, result.Reason)
	}
	if result.ExpiresAt.IsZero() || !now().Before(result.ExpiresAt) {
		return result, errors.New("challenge result expired")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if result.ProbeSeq == 0 || result.ProbeSeq <= g.last[workerID] {
		return result, fmt.Errorf("challenge sequence replay: got=%d last=%d", result.ProbeSeq, g.last[workerID])
	}
	if g.last == nil {
		g.last = make(map[string]uint64)
	}
	g.last[workerID] = result.ProbeSeq
	return result, nil
}

// RetryPolicy controls coordinator retries after SB spawn or health failure.
// It returns delays but never sleeps, keeping the coordinator clock injectable.
type RetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

type RetryDecision struct {
	Retry      bool
	Delay      time.Duration
	FailureSeq int
	ManualGate bool
}

// AfterFailure returns an exponential, capped retry decision. failureSeq is
// one-based and must be persisted by the coordinator in the later durability
// slice; this helper is deterministic and side-effect free.
func (p RetryPolicy) AfterFailure(failureSeq int) RetryDecision {
	if failureSeq <= 0 {
		return RetryDecision{ManualGate: true}
	}
	if p.MaxRetries < 0 || p.BaseDelay <= 0 || p.MaxDelay < p.BaseDelay {
		return RetryDecision{FailureSeq: failureSeq, ManualGate: true}
	}
	if failureSeq > p.MaxRetries {
		return RetryDecision{FailureSeq: failureSeq, ManualGate: true}
	}
	delay := p.BaseDelay
	for i := 1; i < failureSeq && delay < p.MaxDelay; i++ {
		if delay > p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	return RetryDecision{Retry: true, Delay: delay, FailureSeq: failureSeq}
}
