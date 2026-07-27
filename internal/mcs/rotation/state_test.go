package rotation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

func TestFrozenTransitions(t *testing.T) {
	tests := []struct {
		name  string
		from  Phase
		event Event
		want  Phase
	}{
		{"usage starts pending", PhaseIdle, EventTriggerUsage, PhasePending},
		{"spawn enters staging", PhasePending, EventSpawnStaging, PhaseStaging},
		{"revoke is pre-CAS", PhaseStaging, EventRevokeStaging, PhasePending},
		{"takeover does not skip activating", PhaseStaging, EventTakeover, PhaseActivating},
		{"activation completes", PhaseActivating, EventActivate, PhaseActive},
		{"activation failure never reopens old writer", PhaseActivating, EventActivateFailed, PhaseRecovery},
		{"staging timeout reaps", PhaseStaging, EventStagingTimeout, PhaseReaped},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Next(tc.from, tc.event)
			if err != nil || got != tc.want {
				t.Fatalf("Next(%s,%s)=(%s,%v), want (%s,nil)", tc.from, tc.event, got, err, tc.want)
			}
		})
	}
}

func TestIllegalTransitionsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		from  Phase
		event Event
	}{
		{PhasePending, EventTakeover},      // no healthy STAGING SB
		{PhaseStaging, EventActivate},      // cannot activate before board takeover
		{PhaseRecovery, EventActivate},     // recovery requires a new rotation
		{PhaseActive, EventActivateFailed}, // stale failure cannot demote active writer
	} {
		got, err := Next(tc.from, tc.event)
		if !errors.Is(err, ErrIllegalTransition) || got != tc.from {
			t.Fatalf("Next(%s,%s)=(%s,%v), want unchanged+ErrIllegalTransition", tc.from, tc.event, got, err)
		}
	}
}

type fakeChallenger struct{ result pool.ProbeResult }

func (f fakeChallenger) Challenge(context.Context, string) pool.ProbeResult { return f.result }

func TestChallengeGateRequiresFreshApplicationResult(t *testing.T) {
	now := time.Unix(100, 0)
	valid := pool.ProbeResult{WorkerID: "sb-1", ProbeSeq: 7, Status: "ok", ObservedAt: now, ExpiresAt: now.Add(time.Second)}
	gate := &ChallengeGate{Probe: fakeChallenger{valid}, Now: func() time.Time { return now }}
	if _, err := gate.Check(context.Background(), "sb-1"); err != nil {
		t.Fatalf("fresh challenge rejected: %v", err)
	}

	for _, tc := range []struct {
		name   string
		result pool.ProbeResult
		worker string
	}{
		{"degraded", pool.ProbeResult{WorkerID: "sb-1", Status: "degraded", ExpiresAt: now.Add(time.Second)}, "sb-1"},
		{"unknown", pool.ProbeResult{WorkerID: "sb-1", Status: "unknown", ExpiresAt: now.Add(time.Second)}, "sb-1"},
		{"expired", pool.ProbeResult{WorkerID: "sb-1", Status: "ok", ExpiresAt: now}, "sb-1"},
		{"wrong worker", pool.ProbeResult{WorkerID: "sa-1", Status: "ok", ExpiresAt: now.Add(time.Second)}, "sb-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := &ChallengeGate{Probe: fakeChallenger{tc.result}, Now: func() time.Time { return now }}
			if _, err := gate.Check(context.Background(), tc.worker); err == nil {
				t.Fatal("invalid challenge accepted")
			}
		})
	}
}

func TestChallengeGateRejectsReplayPerWorker(t *testing.T) {
	now := time.Unix(100, 0)
	probe := &mutableChallenger{result: pool.ProbeResult{WorkerID: "sb-1", ProbeSeq: 8, Status: "ok", ExpiresAt: now.Add(time.Second)}}
	gate := &ChallengeGate{Probe: probe, Now: func() time.Time { return now }}
	if _, err := gate.Check(context.Background(), "sb-1"); err != nil {
		t.Fatalf("first sequence rejected: %v", err)
	}
	if _, err := gate.Check(context.Background(), "sb-1"); err == nil {
		t.Fatal("duplicate sequence accepted")
	}
	probe.result.ProbeSeq = 7
	if _, err := gate.Check(context.Background(), "sb-1"); err == nil {
		t.Fatal("older sequence accepted")
	}
	probe.result.ProbeSeq = 9
	if _, err := gate.Check(context.Background(), "sb-1"); err != nil {
		t.Fatalf("newer sequence rejected: %v", err)
	}
}

type mutableChallenger struct{ result pool.ProbeResult }

func (f *mutableChallenger) Challenge(context.Context, string) pool.ProbeResult { return f.result }

func TestRetryPolicyBoundedExponentialBackoff(t *testing.T) {
	policy := RetryPolicy{MaxRetries: 3, BaseDelay: time.Second, MaxDelay: 2500 * time.Millisecond}
	wants := []RetryDecision{
		{Retry: true, Delay: time.Second, FailureSeq: 1},
		{Retry: true, Delay: 2 * time.Second, FailureSeq: 2},
		{Retry: true, Delay: 2500 * time.Millisecond, FailureSeq: 3},
		{FailureSeq: 4, ManualGate: true},
	}
	for i, want := range wants {
		if got := policy.AfterFailure(i + 1); got != want {
			t.Fatalf("AfterFailure(%d)=%+v, want %+v", i+1, got, want)
		}
	}
}

func TestRetryPolicyInvalidConfigurationFailsClosed(t *testing.T) {
	for _, policy := range []RetryPolicy{
		{MaxRetries: -1, BaseDelay: time.Second, MaxDelay: time.Second},
		{MaxRetries: 3, BaseDelay: 0, MaxDelay: time.Second},
		{MaxRetries: 3, BaseDelay: 2 * time.Second, MaxDelay: time.Second},
	} {
		if got := policy.AfterFailure(1); got.Retry || !got.ManualGate {
			t.Fatalf("invalid policy did not fail closed: %+v", got)
		}
	}
}
