//go:build race

package m8

import "time"

// Race instrumentation adds substantial map and synchronization overhead.
// Keep both performance assertions active, with a bounded 20x allowance.
const (
	completedTotalCapHitLimit     = 20 * 50 * time.Millisecond
	completedTotalCapAverageLimit = 20 * time.Millisecond
)
