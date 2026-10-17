package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type stateWrite struct {
	path   string
	raw    []byte
	writer func(string, []byte) error
	reply  chan error
}

// Exactly one persistence worker per controller. No filesystem operation holds
// the quota-state mutex. An uninterruptible call retains at most this worker;
// subsequent requests fail closed rather than spawning replacement goroutines.
func (q *softQuota) startPersisterLocked() {
	q.persistOnce.Do(func() {
		q.persistRequests = make(chan stateWrite)
		q.persistQuit = make(chan struct{})
		q.persistDone = make(chan struct{})
		go func() {
			defer func() {
				q.mu.Lock()
				lock := q.retiredLock
				q.retiredLock = nil
				q.mu.Unlock()
				if lock != nil {
					lock.Close()
				}
				close(q.persistDone)
			}()
			for {
				select {
				case <-q.persistQuit:
					return
				case req := <-q.persistRequests:
					req.reply <- req.writer(req.path, req.raw)
				}
			}
		}()
	})
}
func (q *softQuota) stopPersister() {
	if q.persistDone == nil {
		return
	}
	q.persistQuitOnce.Do(func() { close(q.persistQuit) })
	select {
	case <-q.persistDone:
	case <-time.After(q.policy.StateWriteBudget):
	}
}
func (q *softQuota) persistLocked() error {
	if q.broken {
		return fmt.Errorf("soft state unavailable")
	}
	q.startPersisterLocked()
	q.persisting++
	q.mu.Unlock()
	q.persistMu.Lock()
	q.mu.Lock()
	var err error
	if q.broken {
		err = fmt.Errorf("soft state unavailable")
	}
	raw, marshalErr := json.Marshal(q.state)
	if marshalErr != nil {
		err = marshalErr
	}
	req := stateWrite{q.path, raw, q.writeState, make(chan error, 1)}
	q.mu.Unlock()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), q.policy.StateWriteBudget)
		select {
		case q.persistRequests <- req:
			select {
			case err = <-req.reply:
			case <-ctx.Done():
				err = ctx.Err()
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
		cancel()
	}
	q.mu.Lock()
	q.persisting--
	if err != nil {
		q.broken = true
	}
	q.persistMu.Unlock()
	return err // caller still holds q.mu, but no IO/wait held it
}

func validNodeSpace(space fsSpace) bool {
	return space.Total > 0 && space.Total <= 1<<60 && space.Free >= 0 && space.Free <= space.Total && space.Files > 0 && space.FreeFiles <= space.Files
}

// Only the fixed sampler calls this in production. There is never a request-
// spawned statfs goroutine. Each phase publishes under a short state lock.
func (q *softQuota) sampleNode() {
	q.mu.Lock()
	q.sampling = true
	q.samplingAt = q.now()
	q.mu.Unlock()
	space, spaceErr := q.disk(q.root)
	if spaceErr == nil && !validNodeSpace(space) {
		spaceErr = fmt.Errorf("invalid space snapshot")
	}
	q.mu.Lock()
	q.spaceErr = spaceErr
	if spaceErr == nil {
		q.space = space
		q.spaceKnown = true
		q.spaceAt = q.now()
		q.state.LastSpaceAt = q.spaceAt
	}
	q.mu.Unlock()
	cpu, total, available, resourceErr := q.resources()
	entries, inventoryErr := q.readInventory(q.root)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	q.mu.Lock()
	q.sampledCPU, q.sampledTotal, q.sampledAvailable = cpu, total, available
	q.resourcesErr, q.resourcesAt = resourceErr, q.now()
	q.inventory, q.inventoryErr, q.inventoryAt = names, inventoryErr, q.now()
	q.sampling = false
	q.mu.Unlock()
}
func (q *softQuota) samplingExpiredLocked() bool {
	return q.sampling && (q.now().Sub(q.samplingAt) > q.policy.ScanBudget || q.now().Before(q.samplingAt))
}
func (q *softQuota) rootUnknownLocked() bool {
	return !q.spaceKnown || q.spaceErr != nil || q.spaceAt.IsZero() || q.now().Sub(q.spaceAt) > q.policy.MaxAge || q.now().Before(q.spaceAt) || q.samplingExpiredLocked()
}
