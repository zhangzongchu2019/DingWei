package main

import "testing"

func TestSessionHistorySwitchRequiresExactOne(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", false}, {"0", false}, {"true", false}, {" 1", false}, {"1 ", false}, {"01", false}, {"1", true}} {
		if got := sessionHistoryEnabledFromEnv(tc.value); got != tc.want {
			t.Errorf("value %q enabled=%t want %t", tc.value, got, tc.want)
		}
	}
}
