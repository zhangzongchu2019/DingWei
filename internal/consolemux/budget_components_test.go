package consolemux

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Separate maximum-capacity component: 16 executing +64 waiting, not the
// production conservative 64-total (16+48) admission configuration.
func TestBudgetIndependent16Execute64WaitBoundary(t *testing.T) {
	b := &Budget{executing: make(chan struct{}, 16), waiting: make(chan struct{}, 80)}
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := b.Do(context.Background(), func(c context.Context) error {
				select {
				case <-release:
					return nil
				case <-c.Done():
					return c.Err()
				}
			}); e != nil {
				t.Error(e)
			}
		}()
	}
	end := time.Now().Add(500 * time.Millisecond)
	for len(b.waiting) != 80 || len(b.executing) != 16 {
		if time.Now().After(end) {
			t.Fatal("boundary did not fill")
		}
		time.Sleep(time.Millisecond)
	}
	if e := b.Do(context.Background(), func(context.Context) error { t.Error("overlimit executed"); return nil }); !errors.Is(e, ErrCapacity) {
		t.Fatal("81st admitted", e)
	}
	close(release)
	wg.Wait()
	if len(b.executing) != 0 || len(b.waiting) != 0 {
		t.Fatal("retained tokens")
	}
	t.Log("component=16exec+64wait, total=80, 81st rejected, production=16exec+48wait total64 remains distinct")
}

type delayedCancelContext struct{ context.Context }

func (delayedCancelContext) Deadline() (time.Time, bool) { return time.Now().Add(-time.Second), true }
func TestBudgetExpiredAbsoluteDeadlineNeverStartsWork(t *testing.T) {
	called := false
	err := NewBudget().Do(delayedCancelContext{context.Background()}, func(context.Context) error { called = true; return nil })
	if called || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("late cancellation started work", called, err)
	}
}

func TestBudgetCleanupOverrunFailsNewAdmissionWithoutReleasingWorker(t *testing.T) {
	b := NewBudget()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- b.Do(ctx, func(c context.Context) error { <-c.Done(); <-release; return c.Err() }) }()
	deadline := time.Now().Add(400 * time.Millisecond)
	for !b.unhealthy.Load() {
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("cleanup watchdog did not fence new work")
		}
		time.Sleep(time.Millisecond)
	}
	if len(b.executing) != 1 || len(b.waiting) != 1 {
		close(release)
		t.Fatal("live worker not still charged")
	}
	if e := b.Do(context.Background(), func(context.Context) error { t.Error("work admitted after cleanup fault"); return nil }); !errors.Is(e, ErrCleanupTimeout) {
		close(release)
		t.Fatal(e)
	}
	close(release)
	if e := <-done; !errors.Is(e, ErrCleanupTimeout) {
		t.Fatal(e)
	}
	if len(b.executing) != 0 || len(b.waiting) != 0 {
		t.Fatal("actual worker completion did not release")
	}
	t.Log("cleanup>50ms: budget fenced; old SQL/worker still charged; no replacement worker; no new admission")
}
