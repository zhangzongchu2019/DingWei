//go:build !instrmutex

package m8

import "sync"

type hubMutex struct{ sync.Mutex }

func (m *hubMutex) lockStats() (LockTimingStats, bool) { return LockTimingStats{}, false }
