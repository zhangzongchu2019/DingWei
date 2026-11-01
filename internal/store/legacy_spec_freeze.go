package store

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

var ErrLegacySpecFrozen = errors.New("legacy_spec_frozen")

type legacySpecLeaseKey struct{}
type legacySpecLease struct {
	owner  *SQLite
	active atomic.Bool
}

func (s *SQLite) LegacySpecsFrozen(ctx context.Context) (bool, error) {
	var frozen bool
	err := s.db.QueryRowContext(ctx, `SELECT frozen FROM legacy_spec_freeze WHERE id=1`).Scan(&frozen)
	return frozen, err
}

// BeginLegacySpecWrite serializes complete operations (including outbound sends)
// with the cutover. SQL triggers independently guard every business transaction.
// Nested callers may reuse a live lease, never a context whose lease has ended.
func (s *SQLite) BeginLegacySpecWrite(ctx context.Context) (context.Context, func(), error) {
	if lease, ok := ctx.Value(legacySpecLeaseKey{}).(*legacySpecLease); ok && lease.owner == s && lease.active.Load() {
		return ctx, func() {}, nil
	}
	release, err := s.lockLegacySpecs(ctx, false)
	if err != nil {
		return ctx, nil, err
	}
	frozen, err := s.LegacySpecsFrozen(ctx)
	if err == nil && frozen {
		err = ErrLegacySpecFrozen
	}
	if err != nil {
		release()
		return ctx, nil, err
	}
	lease := &legacySpecLease{owner: s}
	lease.active.Store(true)
	return context.WithValue(ctx, legacySpecLeaseKey{}, lease), func() {
		if lease.active.Swap(false) {
			release()
		}
	}, nil
}

// EnableLegacySpecFreeze is a one-way latch. A missing/false startup setting must
// never reopen an already-frozen database. There is deliberately no HTTP setter.
func (s *SQLite) EnableLegacySpecFreeze(ctx context.Context) error {
	release, err := s.lockLegacySpecs(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	_, err = s.db.ExecContext(ctx, `UPDATE legacy_spec_freeze SET frozen=1,frozen_at=? WHERE id=1 AND frozen=0`, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) lockLegacySpecs(ctx context.Context, exclusive bool) (func(), error) {
	if s.legacySpecLockPath != "" {
		return lockLegacySpecFile(ctx, s.legacySpecLockPath, exclusive)
	}
	// In-memory/test stores have no cross-process database identity.
	for {
		locked := false
		if exclusive {
			locked = s.legacySpecMu.TryLock()
		} else {
			locked = s.legacySpecMu.TryRLock()
		}
		if locked {
			if exclusive {
				return s.legacySpecMu.Unlock, nil
			}
			return s.legacySpecMu.RUnlock, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}
