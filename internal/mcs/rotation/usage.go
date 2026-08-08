package rotation

import (
	"errors"
	"fmt"
)

type AdapterOutcome string

const (
	OutcomeAccepted             AdapterOutcome = "accepted"
	OutcomeStale                AdapterOutcome = "stale"
	OutcomeInvalid              AdapterOutcome = "invalid"
	OutcomeUnknown              AdapterOutcome = "unknown"
	OutcomeRebaselineModel      AdapterOutcome = "rebaseline_model"
	OutcomeRebaselineCompaction AdapterOutcome = "rebaseline_compaction"
	OutcomeRebaselineMaxChange  AdapterOutcome = "rebaseline_max_change"
)

type UsageSample struct {
	SessionID            string
	Provider             string
	Model                string
	NormalizedUsedTokens *int64
	MaxContext           *int64
	SampleSeq            int64
	Confidence           string
	Compaction           bool
}

// UsageState is JSON-serializable coordinator state. Persisting this value
// preserves the two-sample debounce count and pending rotation across restart.
type UsageState struct {
	SessionID       string `json:"session_id"`
	Model           string `json:"model"`
	MaxContext      int64  `json:"max_context"`
	LastSampleSeq   int64  `json:"last_sample_seq"`
	ConsecutiveHigh int    `json:"consecutive_high"`
	RotationID      string `json:"rotation_id,omitempty"`
}

type UsageDecision struct {
	Outcome    AdapterOutcome
	Trigger    bool
	Emergency  bool
	RotationID string
	Reason     string
}

type RotationIDSource func() (string, error)

type UsageCoordinator struct {
	State UsageState
	NewID RotationIDSource
}

func (c *UsageCoordinator) Apply(sample UsageSample) UsageDecision {
	if sample.SessionID == "" || sample.Provider == "" || sample.Model == "" || sample.SampleSeq <= 0 {
		return UsageDecision{Outcome: OutcomeInvalid, Reason: "required usage identity missing"}
	}
	if c.State.SessionID != "" && sample.SessionID != c.State.SessionID {
		return UsageDecision{Outcome: OutcomeInvalid, Reason: "session mismatch"}
	}
	if sample.SampleSeq <= c.State.LastSampleSeq {
		return UsageDecision{Outcome: OutcomeStale, Reason: "sample_seq is not newer"}
	}
	if sample.NormalizedUsedTokens == nil || sample.MaxContext == nil || sample.Confidence == "unknown" {
		return UsageDecision{Outcome: OutcomeUnknown, Reason: "usage numerator or denominator is not authoritative"}
	}
	used, maximum := *sample.NormalizedUsedTokens, *sample.MaxContext
	if used < 0 || maximum <= 0 || used > maximum {
		return UsageDecision{Outcome: OutcomeInvalid, Reason: "usage token values out of range"}
	}

	if c.State.SessionID == "" {
		c.rebaseline(sample, maximum)
		return UsageDecision{Outcome: OutcomeAccepted, Reason: "initial baseline"}
	}
	if sample.Model != c.State.Model {
		c.rebaseline(sample, maximum)
		return UsageDecision{Outcome: OutcomeRebaselineModel}
	}
	if sample.Compaction {
		c.rebaseline(sample, maximum)
		return UsageDecision{Outcome: OutcomeRebaselineCompaction}
	}
	if maximum != c.State.MaxContext {
		c.rebaseline(sample, maximum)
		return UsageDecision{Outcome: OutcomeRebaselineMaxChange}
	}

	c.State.LastSampleSeq = sample.SampleSeq
	if c.State.RotationID != "" {
		return UsageDecision{Outcome: OutcomeAccepted, RotationID: c.State.RotationID, Reason: "rotation already pending"}
	}
	if atLeast(used, maximum, 95, 100) {
		return c.trigger(true)
	}
	if atLeast(used, maximum, 85, 100) {
		return c.trigger(false)
	}
	if atLeast(used, maximum, 75, 100) {
		c.State.ConsecutiveHigh++
		if c.State.ConsecutiveHigh >= 2 {
			return c.trigger(false)
		}
	} else {
		c.State.ConsecutiveHigh = 0
	}
	return UsageDecision{Outcome: OutcomeAccepted}
}

func (c *UsageCoordinator) rebaseline(sample UsageSample, maximum int64) {
	c.State.SessionID = sample.SessionID
	c.State.Model = sample.Model
	c.State.MaxContext = maximum
	c.State.LastSampleSeq = sample.SampleSeq
	c.State.ConsecutiveHigh = 0
	c.State.RotationID = ""
}

func (c *UsageCoordinator) trigger(emergency bool) UsageDecision {
	if c.NewID == nil {
		return UsageDecision{Outcome: OutcomeInvalid, Reason: "rotation id source missing"}
	}
	id, err := c.NewID()
	if err != nil || id == "" {
		if err == nil {
			err = errors.New("empty rotation id")
		}
		return UsageDecision{Outcome: OutcomeInvalid, Reason: fmt.Sprintf("rotation id: %v", err)}
	}
	c.State.RotationID = id
	return UsageDecision{Outcome: OutcomeAccepted, Trigger: true, Emergency: emergency, RotationID: id}
}

// atLeast compares used/max >= numerator/denominator without floating point
// or integer overflow.
func atLeast(used, maximum, numerator, denominator int64) bool {
	q, r := maximum/denominator, maximum%denominator
	threshold := q*numerator + (r*numerator+denominator-1)/denominator
	return used >= threshold
}
