// Package lifecycle accounts for owned work without racing WaitGroup.Add and Wait.
package lifecycle

import (
	"context"
	"sync"
)

// Group is zero-value ready. Seal and Enter share one mutex; after Seal returns
// no new work can enter. Wait observes actual completion, not cancellation.
type Group struct {
	mu      sync.Mutex
	sealed  bool
	counts  map[string]int
	changed chan struct{}
}

func (g *Group) signal() {
	if g.changed != nil {
		close(g.changed)
	}
	g.changed = make(chan struct{})
}
func (g *Group) Enter(kind string) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sealed {
		return nil, false
	}
	if g.counts == nil {
		g.counts = map[string]int{}
	}
	g.counts[kind]++
	g.signal()
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); defer g.mu.Unlock(); g.counts[kind]--; g.signal() }) }, true
}
func (g *Group) Seal() { g.mu.Lock(); defer g.mu.Unlock(); g.sealed = true; g.signal() }
func (g *Group) Snapshot() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]int{}
	for k, v := range g.counts {
		out[k] = v
	}
	return out
}
func (g *Group) Wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		n := 0
		for _, v := range g.counts {
			n += v
		}
		if n == 0 {
			g.mu.Unlock()
			return nil
		}
		if g.changed == nil {
			g.changed = make(chan struct{})
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
