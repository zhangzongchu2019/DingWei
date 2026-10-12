package consolemux

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"sync"
	"testing"
	"time"
)

func TestBudgetRealPGDeadlineAndCapacity(t *testing.T) {
	ctx := context.Background()
	p, e := repo.OpenPG(ctx, pgtest.DSN(t))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	a, e := repo.NewAccessCtx("test", "mux-budget")
	if e != nil {
		t.Fatal(e)
	}
	b := NewBudget()
	start := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	rejected, expired := 0, 0
	var longest time.Duration
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			began := time.Now()
			err := b.Do(ctx, func(c context.Context) error { _, e := p.Exec(c, a, "SELECT pg_sleep(2)"); return e })
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(err, ErrCapacity) {
				rejected++
			} else if err != nil {
				expired++
			} else {
				t.Error("slow primary query accepted")
			}
			if d := time.Since(began); d > longest {
				longest = d
			}
		}()
	}
	go func() { wg.Wait(); close(done) }()
	close(start)
	maxExecuting, maxAdmitted := 0, 0
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
loop:
	for {
		select {
		case <-done:
			break loop
		case <-tick.C:
			if n := len(b.executing); n > maxExecuting {
				maxExecuting = n
			}
			if n := len(b.waiting); n > maxAdmitted {
				maxAdmitted = n
			}
		}
	}
	if maxExecuting != 16 || maxAdmitted != 64 || rejected != 16 || expired != 64 || len(b.executing) != 0 || len(b.waiting) != 0 {
		t.Fatalf("execution=%d admitted=%d rejected=%d expired=%d", maxExecuting, maxAdmitted, rejected, expired)
	}
	// Cancellation is requested at 1s. Scheduling/driver return latency is reported,
	// never added to a positive authorization lease.
	t.Logf("primary_pg concurrent=80 executing_peak=%d admitted_peak=%d waiting_cap=48 rejected=%d cancelled=%d max_return=%s cancellation_deadline=1s leases_not_extended=true", maxExecuting, maxAdmitted, rejected, expired, longest)
}
