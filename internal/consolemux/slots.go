// Package consolemux contains the shared authority and bounded transport primitives.
package consolemux

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"sync/atomic"
	"time"
)

const SlotTTL = 3 * time.Second

var (
	ErrInUse          = errors.New("console_in_use")
	ErrLost           = errors.New("console_slot_lost")
	ErrCapacity       = errors.New("console_capacity")
	ErrCleanupTimeout = errors.New("console_db_cleanup_timeout")
)

// Budget is shared by all mux authorization and slot work on one Hub.
type Budget struct {
	executing, waiting chan struct{}
	unhealthy          atomic.Bool
	observe            func(context.Context, string, time.Time)
}

func NewBudget() *Budget {
	return &Budget{executing: make(chan struct{}, 16), waiting: make(chan struct{}, 64)}
}
func (b *Budget) Do(ctx context.Context, fn func(context.Context) error) error {
	if b.unhealthy.Load() {
		return ErrCleanupTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	deadlineForTrace, _ := ctx.Deadline()
	b.note(ctx, "work_deadline", deadlineForTrace)
	select {
	case b.waiting <- struct{}{}:
	default:
		return ErrCapacity
	}
	defer func() { <-b.waiting; b.note(ctx, "admission_released", time.Now()) }()
	select {
	case b.executing <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-b.executing; b.note(ctx, "execution_released", time.Now()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	// A delayed cancellation callback is not permission to start new work.
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	if b.unhealthy.Load() {
		return ErrCleanupTimeout
	}
	deadline, _ := ctx.Deadline()
	finished := make(chan struct{})
	// At most one watchdog per charged executing operation (<=16). It never
	// replaces the worker or releases its token while SQL is still running.
	watchdog := time.AfterFunc(time.Until(deadline.Add(50*time.Millisecond)), func() {
		select {
		case <-finished:
			return
		default:
			b.unhealthy.Store(true)
		}
	})
	defer watchdog.Stop()
	defer close(finished)
	err := fn(ctx)
	if b.unhealthy.Load() {
		return ErrCleanupTimeout
	}
	if time.Now().After(deadline.Add(50 * time.Millisecond)) {
		b.unhealthy.Store(true)
		return ErrCleanupTimeout
	}
	if err == nil {
		if !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		return ctx.Err()
	}
	return err
}

// Owner must remain server-side. Its nonce is not a login credential.
type Owner struct {
	Principal, Origin, Session, Instance, Nonce string
	Generation                                  int64
	Until                                       time.Time
}

func NewOwner(principal, origin, session, instance string) (Owner, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Owner{}, err
	}
	return Owner{Principal: principal, Origin: origin, Session: session, Instance: instance, Nonce: hex.EncodeToString(nonce[:])}, nil
}

type Slots interface {
	Acquire(context.Context, Owner) (Owner, error)
	Renew(context.Context, Owner) (Owner, error)
	Check(context.Context, Owner) (Owner, error)
	Release(context.Context, Owner) error
}
type PGSlots struct {
	pool   *repo.Pool
	access repo.AccessCtx
	budget *Budget
}

func NewPGSlots(pool *repo.Pool, access repo.AccessCtx, budget *Budget) *PGSlots {
	return &PGSlots{pool, access, budget}
}
func (s *PGSlots) query(ctx context.Context, o Owner, q string, args ...any) (Owner, error) {
	start := time.Now()
	err := s.budget.Do(ctx, func(ctx context.Context) error {
		row, err := s.pool.QueryRow(ctx, s.access, q, args...)
		if err != nil {
			return err
		}
		var expiry, now time.Time
		if err := row.Scan(&o.Generation, &expiry, &now); err != nil {
			return err
		}
		// Round-trip latency is deducted, never added to the authority's lease.
		o.Until = start.Add(expiry.Sub(now))
		if !time.Now().Before(o.Until) {
			return ErrLost
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Owner{}, ErrLost
	}
	return o, err
}
func (s *PGSlots) Acquire(ctx context.Context, o Owner) (Owner, error) {
	if o.Principal == "" || o.Origin == "" || o.Session == "" || o.Instance == "" || len(o.Nonce) != 32 {
		return Owner{}, ErrLost
	}
	out, err := s.query(ctx, o, `INSERT INTO console_transport_slot
 (principal_id,origin,owner_session,owner_instance,nonce,generation,expires_at)
 VALUES($1,$2,$3,$4,$5,1,clock_timestamp()+interval '3 seconds')
 ON CONFLICT(principal_id,origin) DO UPDATE SET
 owner_session=EXCLUDED.owner_session,owner_instance=EXCLUDED.owner_instance,nonce=EXCLUDED.nonce,
 generation=CASE WHEN console_transport_slot.expires_at>clock_timestamp() THEN console_transport_slot.generation ELSE console_transport_slot.generation+1 END,
 expires_at=clock_timestamp()+interval '3 seconds'
 WHERE console_transport_slot.expires_at<=clock_timestamp() OR
 (console_transport_slot.owner_session=$3 AND console_transport_slot.owner_instance=$4 AND console_transport_slot.nonce=$5)
 RETURNING generation,expires_at,clock_timestamp()`, o.Principal, o.Origin, o.Session, o.Instance, o.Nonce)
	if errors.Is(err, ErrLost) {
		return Owner{}, ErrInUse
	}
	return out, err
}
func (s *PGSlots) Renew(ctx context.Context, o Owner) (Owner, error) {
	return s.query(ctx, o, `UPDATE console_transport_slot SET expires_at=clock_timestamp()+interval '3 seconds'
 WHERE principal_id=$1 AND origin=$2 AND owner_session=$3 AND owner_instance=$4 AND nonce=$5 AND generation=$6
 AND expires_at>clock_timestamp() RETURNING generation,expires_at,clock_timestamp()`,
		o.Principal, o.Origin, o.Session, o.Instance, o.Nonce, o.Generation)
}
func (s *PGSlots) Check(ctx context.Context, o Owner) (Owner, error) {
	return s.query(ctx, o, `SELECT generation,expires_at,clock_timestamp() FROM console_transport_slot
 WHERE principal_id=$1 AND origin=$2 AND owner_session=$3 AND owner_instance=$4 AND nonce=$5 AND generation=$6
 AND expires_at>clock_timestamp()`, o.Principal, o.Origin, o.Session, o.Instance, o.Nonce, o.Generation)
}

// Release is called only after the owner has stopped its writer. A stale owner
// cannot clear a successor, even if its delayed cleanup runs after takeover.
func (s *PGSlots) Release(ctx context.Context, o Owner) error {
	return s.budget.Do(ctx, func(ctx context.Context) error {
		_, err := s.pool.Exec(ctx, s.access, `UPDATE console_transport_slot
 SET expires_at=LEAST(expires_at,clock_timestamp()),owner_session='',owner_instance='',nonce=''
 WHERE principal_id=$1 AND origin=$2 AND owner_session=$3 AND owner_instance=$4 AND nonce=$5 AND generation=$6`,
			o.Principal, o.Origin, o.Session, o.Instance, o.Nonce, o.Generation)
		return err
	})
}

// Optional per-instance test observer; production assembly leaves this nil.
func (b *Budget) note(ctx context.Context, stage string, at time.Time) {
	if b.observe != nil {
		b.observe(ctx, stage, at)
	}
}
