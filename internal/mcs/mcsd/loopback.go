// loopback.go —— E1 进程内 loopback 驱动:worker 收 envelope 后异步回 processing+terminal 回执。
// 不烧 LLM;可注入时延/失败/丢回执,供剧本用。E2 将替换为真子进程+unix socket 版。
package mcsd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// LoopbackBehavior 单 worker 行为注入。
type LoopbackBehavior struct {
	WorkDelay      time.Duration // 模拟执行耗时
	FailNodes      map[string]bool
	DropProcessing bool // 丢 processing 回执(测直达窗)
	DropTerminal   bool // 丢 terminal 回执(测 redeliver/deliver_fail 收敛)
}

// LoopbackDriver E1 进程内驱动。Receipts 指向 Daemon(组装后由 Bind 注入)。
type LoopbackDriver struct {
	mu       sync.Mutex
	n        int
	Behavior LoopbackBehavior
	daemon   *Daemon
	workers  []*loopWorker
}

// Bind 注入守护(worker 回执直呼 Daemon 入口)。
func (l *LoopbackDriver) Bind(d *Daemon) { l.mu.Lock(); l.daemon = d; l.mu.Unlock() }

// Workers 观测(测试用)。
func (l *LoopbackDriver) Workers() []*loopWorker {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*loopWorker, len(l.workers))
	copy(out, l.workers)
	return out
}

// Spawn 实现 pool.DriverAdapter。
func (l *LoopbackDriver) Spawn(context.Context) (pool.Worker, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := &loopWorker{id: fmt.Sprintf("lw%d", l.n), drv: l}
	l.n++
	l.workers = append(l.workers, w)
	return w, nil
}

type loopWorker struct {
	mu   sync.Mutex
	id   string
	dead bool
	drv  *LoopbackDriver
	got  int
}

func (w *loopWorker) ID() string { return w.id }
func (w *loopWorker) Caps() pool.Caps {
	return pool.Caps{"send": true}
}
func (w *loopWorker) Health(context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dead {
		return fmt.Errorf("loopback worker %s dead", w.id)
	}
	return nil
}
func (w *loopWorker) Stop(context.Context) error { return nil }

// Kill 模拟崩死(剧本用)。
func (w *loopWorker) Kill() { w.mu.Lock(); w.dead = true; w.mu.Unlock() }

// Delivered 观测收件数。
func (w *loopWorker) Delivered() int { w.mu.Lock(); defer w.mu.Unlock(); return w.got }

// Deliver 收 envelope→异步模拟执行→回执(执行点 rid 幂等由 Board 侧 request 账保证,worker 不重放)。
func (w *loopWorker) Deliver(_ context.Context, env pool.Envelope) error {
	w.mu.Lock()
	if w.dead {
		w.mu.Unlock()
		return fmt.Errorf("worker %s dead", w.id)
	}
	w.got++
	drv := w.drv
	w.mu.Unlock()
	go func() {
		drv.mu.Lock()
		d := drv.daemon
		beh := drv.Behavior
		drv.mu.Unlock()
		if d == nil {
			return
		}
		ctx := context.Background()
		if beh.WorkDelay > 0 {
			time.Sleep(beh.WorkDelay)
		}
		w.mu.Lock()
		dead := w.dead
		w.mu.Unlock()
		if dead {
			return // 崩死:无回执(由 failover/redeliver 收敛)
		}
		if !beh.DropProcessing {
			_ = d.ReceiptProcessing(ctx, env.NodeID, env.Generation, env.RID)
		}
		if beh.DropTerminal {
			return
		}
		ok := !beh.FailNodes[env.NodeID]
		_ = d.ReceiptTerminal(ctx, env.NodeID, env.Generation, env.RID, ok, "loopback://"+env.NodeID)
	}()
	return nil
}
