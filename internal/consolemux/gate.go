package consolemux

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

const WriteLimit = 250 * time.Millisecond

var ErrDenied = errors.New("console_scope_expired")

// Gate serializes the final permission check and physical write with revocation.
// No database call or global registry/terminal lock is made under this mutex.
type Gate struct {
	mu            sync.Mutex
	version       uint64
	stopped       bool
	until         time.Time
	sessionExpiry time.Time
	slot          Owner
	scopes        map[string]string
}

func NewGate(o Owner) *Gate     { return &Gate{slot: o, scopes: map[string]string{}} }
func (g *Gate) Version() uint64 { g.mu.Lock(); defer g.mu.Unlock(); return g.version }

// CommitCheck discards results of checks that raced a revoke/control update.
// The caller supplies the check's START; this gate adds at most 1s, never result time + 1s.
func (g *Gate) CommitCheck(v uint64, started, expiry time.Time, o Owner, scopes map[string]string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	until := started.Add(time.Second)
	if started.After(time.Now()) || g.stopped || g.version != v || !time.Now().Before(until) || o.Nonce != g.slot.Nonce || o.Generation != g.slot.Generation {
		return false
	}
	g.until = until
	g.sessionExpiry = expiry
	g.slot = o
	g.scopes = make(map[string]string, len(scopes))
	for k, v := range scopes {
		g.scopes[k] = v
	}
	return true
}
func (g *Gate) Revoke(agentID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.version++
	if agentID == "" {
		g.stopped = true
		g.scopes = map[string]string{}
	} else {
		delete(g.scopes, agentID)
	}
}

// FailCheck invalidates all old positive leases without making recovery impossible.
func (g *Gate) FailCheck() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.version++
	g.until = time.Time{}
	g.scopes = map[string]string{}
}
func (g *Gate) Write(w http.ResponseWriter, agentID, generation string, frame []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if g.stopped || !now.Before(g.until) || !now.Before(g.sessionExpiry) || !now.Before(g.slot.Until) {
		return ErrDenied
	}
	scope, ok := g.scopes[agentID]
	if agentID != "" && (!ok || generation == "" || scope != generation) {
		return ErrDenied
	}
	deadline := now.Add(WriteLimit)
	for _, end := range []time.Time{g.until.Add(time.Second), g.sessionExpiry, g.slot.Until} {
		if end.Before(deadline) {
			deadline = end
		}
	}
	ctl := http.NewResponseController(w)
	if err := ctl.SetWriteDeadline(deadline); err != nil {
		return err
	}
	if _, err := w.Write(frame); err != nil {
		return err
	}
	return ctl.Flush()
}
