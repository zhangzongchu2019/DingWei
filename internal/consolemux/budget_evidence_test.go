package consolemux

import (
	"context"
	"errors"
	"fmt"
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
	seed, _ := NewOwner("test", "https://example.invalid", "budget", "console")
	tag := "mux_budget_" + seed.Nonce
	b := NewBudget()
	var traceMu sync.Mutex
	traces := map[int]map[string]time.Time{}
	b.observe = func(c context.Context, stage string, at time.Time) {
		id := c.Value(budgetTraceKey{}).(int)
		traceMu.Lock()
		defer traceMu.Unlock()
		if traces[id] == nil {
			traces[id] = map[string]time.Time{}
		}
		traces[id][stage] = at
	}
	start := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	rejected, expired := 0, 0
	var longest time.Duration
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			began := time.Now()
			var workDeadline, queryEnded time.Time
			err := b.Do(context.WithValue(ctx, budgetTraceKey{}, id), func(c context.Context) error {
				workDeadline, _ = c.Deadline()
				if c.Err() != nil {
					return c.Err()
				}
				_, e := p.Exec(c, a, "SELECT pg_sleep(2) /*"+tag+"*/")
				queryEnded = time.Now()
				return e
			})
			returned := time.Now()
			traceMu.Lock()
			stage := traces[id]
			executionReleased, admissionReleased := stage["execution_released"], stage["admission_released"]
			traceMu.Unlock()
			if !workDeadline.IsZero() {
				if executionReleased.Before(queryEnded) || admissionReleased.Before(executionReleased) || returned.Before(admissionReleased) {
					t.Error("release trace order violated")
				}
				t.Logf("id=%d query_end=%s execution_release=%s admission_release=%s call_return=%s relative_to_work_deadline", id, queryEnded.Sub(workDeadline), executionReleased.Sub(workDeadline), admissionReleased.Sub(workDeadline), returned.Sub(workDeadline))
			}
			if !workDeadline.IsZero() {
				if queryEnded.Sub(workDeadline) > 50*time.Millisecond || returned.Sub(workDeadline) > 50*time.Millisecond {
					t.Errorf("cleanup exceeded50ms: query=%s return=%s", queryEnded.Sub(workDeadline), returned.Sub(workDeadline))
				}
				t.Logf("work_deadline=%s query_end_relative=%s return_release_relative=%s", workDeadline.Sub(began), queryEnded.Sub(workDeadline), returned.Sub(workDeadline))
			}
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
		}(i)
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
	if longest > 1050*time.Millisecond {
		t.Fatalf("wall-return >1.050s: %s", longest)
	}
	health, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	row, e := p.QueryRow(health, a, "SELECT count(*) FROM pg_stat_activity WHERE pid<>pg_backend_pid() AND state='active' AND query LIKE $1", "%"+tag+"%")
	var active int
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&active); e != nil || active != 0 {
		t.Fatalf("background SQL remains: active=%d err=%v", active, e)
	}
	row, e = p.QueryRow(health, a, "SELECT 1")
	var one int
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&one); e != nil || one != 1 {
		t.Fatal("pool not restored", e)
	}
	t.Log(fmt.Sprintf("cancelled_primary_sql_remaining=%d pool_reusable=true cleanup_deadline=50ms", active))
	// Cancellation is requested at 1s. Scheduling/driver return latency is reported,
	// never added to a positive authorization lease.
	t.Logf("primary_pg concurrent=80 executing_peak=%d admitted_peak=%d waiting_cap=48 rejected=%d cancelled=%d max_return=%s cancellation_deadline=1s leases_not_extended=true", maxExecuting, maxAdmitted, rejected, expired, longest)
}

type budgetTraceKey struct{}
