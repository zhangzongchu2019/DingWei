package rotation

import (
	"encoding/json"
	"testing"
)

func i64(v int64) *int64 { return &v }

func sample(seq, used, max int64) UsageSample {
	return UsageSample{SessionID: "s1", Provider: "claude", Model: "m1", SampleSeq: seq,
		NormalizedUsedTokens: i64(used), MaxContext: i64(max), Confidence: "known"}
}

func coordinator() *UsageCoordinator {
	n := 0
	return &UsageCoordinator{NewID: func() (string, error) { n++; return "rot-" + string(rune('0'+n)), nil }}
}

func TestUsageAdapterOutcomes(t *testing.T) {
	c := coordinator()
	if got := c.Apply(sample(1, 10, 100)); got.Outcome != OutcomeAccepted {
		t.Fatalf("initial=%+v", got)
	}
	if got := c.Apply(sample(1, 10, 100)); got.Outcome != OutcomeStale {
		t.Fatalf("stale=%+v", got)
	}
	bad := sample(2, 101, 100)
	if got := c.Apply(bad); got.Outcome != OutcomeInvalid {
		t.Fatalf("invalid=%+v", got)
	}
	unknown := sample(2, 10, 100)
	unknown.NormalizedUsedTokens = nil
	if got := c.Apply(unknown); got.Outcome != OutcomeUnknown {
		t.Fatalf("unknown=%+v", got)
	}
	model := sample(2, 10, 100)
	model.Model = "m2"
	if got := c.Apply(model); got.Outcome != OutcomeRebaselineModel {
		t.Fatalf("model=%+v", got)
	}
	compact := sample(3, 10, 100)
	compact.Model = "m2"
	compact.Compaction = true
	if got := c.Apply(compact); got.Outcome != OutcomeRebaselineCompaction {
		t.Fatalf("compaction=%+v", got)
	}
	changed := sample(4, 10, 200)
	changed.Model = "m2"
	if got := c.Apply(changed); got.Outcome != OutcomeRebaselineMaxChange {
		t.Fatalf("max-change=%+v", got)
	}
}

func TestUsageThresholdsAndSinglePendingRotation(t *testing.T) {
	c := coordinator()
	c.Apply(sample(1, 0, 100))
	if got := c.Apply(sample(2, 75, 100)); got.Trigger {
		t.Fatal("first 75% sample triggered")
	}
	got := c.Apply(sample(3, 75, 100))
	if !got.Trigger || got.Emergency || got.RotationID != "rot-1" {
		t.Fatalf("second 75%%=%+v", got)
	}
	got = c.Apply(sample(4, 95, 100))
	if got.Trigger || got.RotationID != "rot-1" {
		t.Fatalf("pending rotation duplicated=%+v", got)
	}

	c2 := coordinator()
	c2.Apply(sample(1, 0, 100))
	if got := c2.Apply(sample(2, 85, 100)); !got.Trigger || got.Emergency {
		t.Fatalf("85%%=%+v", got)
	}
	c3 := coordinator()
	c3.Apply(sample(1, 0, 100))
	if got := c3.Apply(sample(2, 95, 100)); !got.Trigger || !got.Emergency {
		t.Fatalf("95%%=%+v", got)
	}
}

func TestUsageStateSurvivesJSONRestart(t *testing.T) {
	c := coordinator()
	c.Apply(sample(1, 0, 100))
	c.Apply(sample(2, 75, 100))
	raw, err := json.Marshal(c.State)
	if err != nil {
		t.Fatal(err)
	}
	var restored UsageState
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	c2 := coordinator()
	c2.State = restored
	if got := c2.Apply(sample(3, 75, 100)); !got.Trigger {
		t.Fatalf("restored debounce lost=%+v state=%+v", got, c2.State)
	}
}

func TestUsageIntegerThresholdAtLargeValues(t *testing.T) {
	const max = int64(^uint64(0) >> 1)
	if !atLeast(max, max, 95, 100) {
		t.Fatal("max/max should satisfy threshold")
	}
	if atLeast(74, 100, 75, 100) {
		t.Fatal("74% accepted as 75%")
	}
}
