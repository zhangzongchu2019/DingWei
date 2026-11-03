//go:build !race

package m8

import "time"

const (
	completedTotalCapHitLimit     = 50 * time.Millisecond
	completedTotalCapAverageLimit = time.Millisecond
)
