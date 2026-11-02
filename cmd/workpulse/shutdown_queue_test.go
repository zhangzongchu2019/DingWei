package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type shutdownQueueRepo struct {
	store.Repository
	calls   atomic.Int64
	commits atomic.Int64
	failure error
	block   bool
	release chan struct{}
}

// This isolated child exercises the real queue and coordinator, then returns
// their real process exit code. It is not a full Hub traffic/load fixture.
func TestShutdownS008QueueProcessChild(t *testing.T) {
	mode := os.Getenv("HLC_S008_QUEUE_CHILD")
	if mode == "" {
		return
	}
	r := &shutdownQueueRepo{block: strings.Contains(mode, "deadline"), release: make(chan struct{})}
	if strings.Contains(mode, "failure") {
		r.failure = errors.New("synthetic S008 commit failure")
	}
	batchSize := 200
	if r.block && r.failure != nil {
		batchSize = 1
	}
	q := bus.NewAsyncDBQueue(context.Background(), r, model.DirectionIn, bus.AsyncDBQueueConfig{BatchSize: batchSize, FlushInterval: time.Hour})
	for i := 0; i < 5; i++ {
		if err := q.Enqueue(context.Background(), model.Message{}); err != nil {
			t.Fatal(err)
		}
	}
	result := coordinateShutdown(time.Now(), "S008 isolated queue process", shutdownPlan{
		Flush: q.SealAndFlush, Snapshot: func() any { return shutdownQueueCounts(q) },
	})
	if err := json.NewEncoder(os.Stdout).Encode(result.Report); err != nil {
		t.Fatal(err)
	}
	os.Exit(exitCode(result))
}

func TestShutdownS008QueueProcessExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
	}{{"success", 0}, {"commit_failure", 1}, {"db_deadline", 2}, {"failure_then_deadline", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShutdownS008QueueProcessChild$", "-test.count=1")
			cmd.Dir = home
			cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "TZ=UTC", "HLC_S008_QUEUE_CHILD=" + tc.name}
			start := time.Now()
			out, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("S008 child execution failed: %v", err)
				}
				code = exit.ExitCode()
			}
			var report shutdownReport
			if err := json.Unmarshal(out, &report); err != nil {
				t.Fatalf("S008_PROCESS_REPORT: %v output=%s", err, out)
			}
			if code != tc.code || report.ExitCode != tc.code || len(report.Stages) != 5 {
				t.Fatalf("S008_PROCESS_EXIT: actual=%d want=%d report=%+v", code, tc.code, report)
			}
			counts := report.Snapshot.(map[string]any)
			if code == 0 && counts["remaining"].(float64) != 0 || code != 0 && counts["remaining"].(float64) == 0 {
				t.Fatalf("S008_PROCESS_REMAINING: %+v", counts)
			}
			if strings.Contains(tc.name, "failure") && counts["first_error"] != "operation_failed" {
				t.Fatalf("S008_PROCESS_FIRST_ERROR: %+v", counts)
			}
			t.Logf("S008 actual_process_exit=%d elapsed_ms=%d queue=%v", code, time.Since(start).Milliseconds(), counts)
		})
	}
}

func (r *shutdownQueueRepo) BatchEnqueueMessages(ctx context.Context, ms []model.Message) error {
	call := r.calls.Add(1)
	if r.block && (r.failure == nil || call > 1) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.release:
			return context.DeadlineExceeded
		}
	}
	if r.failure != nil {
		return r.failure
	}
	r.commits.Add(int64(len(ms)))
	return nil
}

func TestShutdownS008QueueStageResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure bool
		timeout bool
		code    int
	}{
		{"success", false, false, 0},
		{"commit_failure", true, false, 1},
		{"db_deadline", false, true, 2},
		{"failure_then_deadline", true, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &shutdownQueueRepo{block: tc.timeout, release: make(chan struct{})}
			if tc.failure {
				r.failure = errors.New("synthetic S008 commit failure")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			batchSize := 200
			if tc.timeout && tc.failure {
				batchSize = 1 // first commit fails; a later commit blocks
			}
			q := bus.NewAsyncDBQueue(ctx, r, model.DirectionIn, bus.AsyncDBQueueConfig{BatchSize: batchSize, FlushInterval: time.Hour})
			for i := 0; i < 5; i++ {
				if err := q.Enqueue(ctx, model.Message{}); err != nil {
					t.Fatal(err)
				}
			}
			closed := false
			start := time.Now()
			result := coordinateShutdown(start, "S008", shutdownPlan{
				Flush:    q.SealAndFlush,
				Storage:  func(context.Context) error { closed = true; return nil },
				Snapshot: func() any { return shutdownQueueCounts(q) },
			})
			close(r.release)
			// Join the writer even when the coordinator correctly stops waiting.
			join, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			_ = q.SealAndFlush(join)
			remaining, failed := q.ShutdownCounts()
			stage := result.Report.Stages[2]
			if tc.failure {
				counts := stage.Snapshot.(map[string]any)
				if counts["first_error"] != "operation_failed" || counts["failed"].(int64) < 1 {
					t.Fatalf("S008_FAILURE_REASON_SNAPSHOT: %+v", counts)
				}
			}
			if result.Code != tc.code || exitCode(result) != tc.code || stage.BudgetMS != 3000 || stage.Timeout != tc.timeout || closed == tc.timeout {
				t.Fatalf("S008_EXIT_AND_DB_ORDER: code=%d stage=%+v closed=%t", result.Code, stage, closed)
			}
			if tc.failure && (!errors.Is(q.ShutdownError(), r.failure) || failed == 0) {
				t.Fatalf("S008_COMMIT_CAUSE_LOST: remaining=%d failed=%d error=%v", remaining, failed, q.ShutdownError())
			}
			if tc.code == 0 && (remaining != 0 || r.commits.Load() != 5) || tc.code != 0 && remaining == 0 {
				t.Fatalf("S008_PERSISTENCE_COUNTS: committed=%d remaining=%d failed=%d", r.commits.Load(), remaining, failed)
			}
			if tc.timeout && (stage.ElapsedMS < 2900 || time.Since(start) > 4*time.Second) {
				t.Fatalf("S008_DB_STAGE_BOUND: elapsed=%v stage=%+v", time.Since(start), stage)
			}
			t.Logf("S008 exit=%d db_budget_ms=%d db_elapsed_ms=%d timeout=%t remaining=%d failed=%d db_closed=%t first_commit_error=%t", result.Code, stage.BudgetMS, stage.ElapsedMS, stage.Timeout, remaining, failed, closed, errors.Is(q.ShutdownError(), r.failure) && r.failure != nil)
		})
	}
}
