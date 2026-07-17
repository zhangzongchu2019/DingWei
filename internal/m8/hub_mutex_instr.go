//go:build instrmutex

package m8

import (
	"sort"
	"sync"
	"time"
)

const lockTimingSampleLimit = 100_000

type hubMutex struct {
	sync.Mutex
	acquiredAt time.Time
	statsMu    sync.Mutex
	records    []time.Duration
	count      uint64
	max        time.Duration
}

func (m *hubMutex) Lock() {
	m.Mutex.Lock()
	m.acquiredAt = time.Now()
}

func (m *hubMutex) TryLock() bool {
	if !m.Mutex.TryLock() {
		return false
	}
	m.acquiredAt = time.Now()
	return true
}

func (m *hubMutex) Unlock() {
	held := time.Since(m.acquiredAt)
	m.Mutex.Unlock()
	m.statsMu.Lock()
	m.count++
	m.records = append(m.records, held)
	if held > m.max {
		m.max = held
	}
	if len(m.records) > lockTimingSampleLimit {
		copy(m.records, m.records[len(m.records)-lockTimingSampleLimit/2:])
		m.records = m.records[:lockTimingSampleLimit/2]
	}
	m.statsMu.Unlock()
}

func (m *hubMutex) lockStats() (LockTimingStats, bool) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	if len(m.records) == 0 {
		return LockTimingStats{Count: m.count}, true
	}
	values := append([]time.Duration(nil), m.records...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	percentile := func(p float64) time.Duration {
		idx := int(float64(len(values)-1) * p)
		return values[idx]
	}
	return LockTimingStats{
		Count: m.count,
		Min:   values[0],
		P50:   percentile(0.50),
		P95:   percentile(0.95),
		P99:   percentile(0.99),
		P999:  percentile(0.999),
		Max:   m.max,
	}, true
}
