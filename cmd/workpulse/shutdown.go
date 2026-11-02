package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
)

const shutdownBudget = 15 * time.Second

type shutdownError struct {
	Code   int
	Report shutdownReport
}

func (e *shutdownError) Error() string {
	return fmt.Sprintf("shutdown %s (exit %d)", e.Report.ExitClass, e.Code)
}
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *shutdownError
	if errors.As(err, &e) {
		return e.Code
	}
	return 1
}

type shutdownStage struct {
	Name        string    `json:"name"`
	Started     time.Time `json:"started_utc"`
	Ended       time.Time `json:"ended_utc"`
	ElapsedMS   int64     `json:"elapsed_ms"`
	BudgetMS    int64     `json:"budget_ms"`
	Timeout     bool      `json:"timeout"`
	ForcedClose bool      `json:"forced_close"`
	Result      string    `json:"result"`
	Error       string    `json:"error"`
	Snapshot    any       `json:"snapshot"`
}
type shutdownReport struct {
	ID        string          `json:"shutdown_id"`
	Reason    string          `json:"reason"`
	Started   time.Time       `json:"started_utc"`
	Ended     time.Time       `json:"ended_utc"`
	ElapsedMS int64           `json:"total_elapsed_ms"`
	Stages    []shutdownStage `json:"stages"`
	ExitClass string          `json:"exit_class"`
	ExitCode  int             `json:"exit_code"`
	Snapshot  any             `json:"snapshot"`
}
type shutdownPlan struct {
	Stop     func(context.Context) error
	Drain    func(context.Context) error
	Flush    func(context.Context) error
	Storage  func(context.Context) error
	Finalize func(context.Context) error
	Force    func()
	Snapshot func() any
	// Production leaves this nil. The process-level deadline guard remains armed
	// until exit, so blocked logging/defer chains cannot extend the contract.
	Emit func(shutdownReport) error
}

func shutdownErrorClass(err error) string {
	switch {
	case err == nil:
		return "nil"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "operation_failed"
	}
}

func shutdownQueueCounts(q *bus.AsyncDBQueue) map[string]any {
	remaining, failed := q.ShutdownCounts()
	return map[string]any{"remaining": remaining, "failed": failed, "first_error": shutdownErrorClass(q.ShutdownError())}
}

func stopFeishuIngress(ctx context.Context, receiver feishu.Receiver, callbacks *lifecycle.Group) error {
	var err error
	if managed, ok := receiver.(feishu.ReceiverLifecycle); ok {
		err = managed.StopIngress(ctx)
	}
	callbacks.Seal()
	return errors.Join(err, callbacks.Wait(ctx))
}

// A callback already admitted before sealing keeps its SDK context values but
// finishes under the bounded work lifecycle, rather than t0's receiver cancel.
func callbackWorkContext(inbound, work context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(inbound))
	stop := context.AfterFunc(work, cancel)
	if work.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func coordinateShutdown(t0 time.Time, reason string, p shutdownPlan) *shutdownError {
	report := shutdownReport{ID: fmt.Sprintf("hlc-%d", t0.UnixNano()), Reason: reason, Started: t0.UTC(), ExitClass: "graceful"}
	timeout, failed := false, false
	safe := true
	phases := []struct {
		name             string
		budget, absolute time.Duration
		fn               func(context.Context) error
	}{
		{"access", time.Second, time.Second, p.Stop}, {"drain", 8 * time.Second, 9 * time.Second, p.Drain}, {"queue", 3 * time.Second, 12 * time.Second, p.Flush}, {"storage", time.Second, 13 * time.Second, p.Storage}, {"finalize", 2 * time.Second, 15 * time.Second, p.Finalize},
	}
	for _, phase := range phases {
		start := time.Now()
		deadline := start.Add(phase.budget)
		if absolute := t0.Add(phase.absolute); absolute.Before(deadline) {
			deadline = absolute
		}
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		stage := shutdownStage{Name: phase.name, Started: start.UTC(), BudgetMS: phase.budget.Milliseconds()}
		var err error
		if !safe && (phase.name == "queue" || phase.name == "storage") {
			err = errors.New("unsafe live storage users; close not attempted")
			stage.Result = "not_attempted_live_users"
		} else {
			done := make(chan error, 1)
			go func(fn func(context.Context) error) {
				if fn == nil {
					done <- nil
				} else {
					done <- fn(ctx)
				}
			}(phase.fn)
			select {
			case err = <-done:
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
		if errors.Is(err, context.DeadlineExceeded) || (!time.Now().Before(deadline)) {
			stage.Timeout = true
			timeout = true
		}
		if err != nil {
			failed = true
			if stage.Result == "" {
				stage.Result = "failed"
			}
		}
		if stage.Result == "" {
			stage.Result = "completed"
		}
		stage.Error = shutdownErrorClass(err)
		if stage.Timeout && (phase.name == "access" || phase.name == "drain") {
			safe = false
			stage.ForcedClose = true
			if p.Force != nil {
				p.Force()
			}
		}
		// Any incomplete queue writer must also prevent DB close.
		if phase.name == "queue" && stage.Timeout {
			safe = false
		}
		stage.Ended = time.Now().UTC()
		stage.ElapsedMS = time.Since(start).Milliseconds()
		if p.Snapshot != nil {
			stage.Snapshot = p.Snapshot()
		}
		report.Stages = append(report.Stages, stage)
		cancel()
	}
	if timeout {
		report.ExitClass = "timeout"
		report.ExitCode = 2
	} else if failed {
		report.ExitClass = "failure"
		report.ExitCode = 1
	}
	report.Ended = time.Now().UTC()
	report.ElapsedMS = time.Since(t0).Milliseconds()
	if p.Snapshot != nil {
		report.Snapshot = p.Snapshot()
	}
	if p.Emit != nil {
		if err := p.Emit(report); err != nil && report.ExitCode == 0 {
			report.ExitClass = "failure"
			report.ExitCode = 1
		}
	}
	return &shutdownError{Code: report.ExitCode, Report: report}
}

// Logging is best effort with a bounded wait. A blocked slog sink cannot hold
// the shutdown coordinator or postpone the independent process deadline.
func emitShutdown(logger *slog.Logger, r shutdownReport) error {
	done := make(chan struct{})
	go func() { logger.Info("hub shutdown result", "shutdown", r); close(done) }()
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("shutdown log sink blocked")
	}
}

type runTasksKey struct{}

func startRunTask(ctx context.Context, kind string, fn func()) bool {
	group, _ := ctx.Value(runTasksKey{}).(*lifecycle.Group)
	if group == nil {
		go fn()
		return true
	}
	done, ok := group.Enter(kind)
	if !ok {
		return false
	}
	go func() { defer done(); fn() }()
	return true
}

// Request admission is independent of HTTP BaseContext. Accepted finite work
// retains its transaction context. Draining rejects a late routed request 503.
type httpAdmission struct{ group lifecycle.Group }

func (a *httpAdmission) Wrap(kind string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, ok := a.group.Enter(kind)
		if !ok {
			http.Error(w, "hub draining", 503)
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}

type shutdownListener struct {
	net.Listener
	once   sync.Once
	mu     sync.Mutex
	closed time.Time
	err    error
}

func (l *shutdownListener) Close() error {
	l.once.Do(func() { l.err = l.Listener.Close(); l.mu.Lock(); l.closed = time.Now(); l.mu.Unlock() })
	return l.err
}
func (l *shutdownListener) ClosedAt() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed.UTC()
}

type serverShutdownResult struct {
	Error    string    `json:"error"`
	ClosedAt time.Time `json:"listener_closed_utc"`
	Finished time.Time `json:"drained_utc"`
}
type serverResults struct {
	mu     sync.Mutex
	values map[string]serverShutdownResult
}

func (r *serverResults) record(name string, ln *shutdownListener, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = map[string]serverShutdownResult{}
	}
	r.values[name] = serverShutdownResult{serverShutdownError(err), ln.ClosedAt(), time.Now().UTC()}
}
func (r *serverResults) snapshot() map[string]serverShutdownResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]serverShutdownResult{}
	for k, v := range r.values {
		out[k] = v
	}
	return out
}

// These loops seal admission separately from the context used by an accepted
// job. A stop never starts another ProcessOne, but an in-flight transaction can
// finish before the eight-second drain deadline.
func runInboundUntil(ctx context.Context, stop *lifecycle.Group, process func(context.Context) (bool, error), idle time.Duration) {
	for {
		done, ok := stop.Enter("processor")
		if !ok {
			return
		}
		processed, _ := process(ctx)
		done()
		if !processed {
			select {
			case <-ctx.Done():
				return
			case <-time.After(idle):
			}
		}
	}
}

func serverShutdownError(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}
