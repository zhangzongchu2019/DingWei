// Package mcsd 是多CLI调度守护:把 pool(择人)/board(权威账)/dispatch(粘合)组装为常驻进程。
//
// E1 骨架职责(phase-2 模拟测试台第一段):
//   - 文件库 Board + Takeover(epoch+1)+ Renew 循环(3.4:活调度者必须能续期);
//   - 调度循环:ReadyNodes→择人→Send 记账→Deliver 外呼;池满入缓冲,有闲回灌;
//   - reconciler 定时(Sweep:过期降级/timeout 注记/#15 兜底/redeliver 候选→Redeliver);
//   - 回执入口(processing/terminal→guarded 写+归还 worker);
//   - 优雅退出(停池、关库)。
package mcsd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/dispatch"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// Config 守护参数(数值域=D15,默认取保守值)。
type Config struct {
	BoardPath string // SQLite 文件路径(":memory:" 仅测试)
	BoardID   string
	Owner     string // 调度者身份(问责主体;M5v2 全事件 actor 源)
	Pool      pool.Config

	LeaseTTL      time.Duration // orchestrator 租约
	Tick          time.Duration // 调度循环节拍
	SweepEvery    time.Duration // reconciler 节拍
	RedeliverAfter time.Duration
}

func (c *Config) fill() {
	if c.BoardID == "" {
		c.BoardID = "board1"
	}
	if c.Owner == "" {
		c.Owner = "mcsd"
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 60 * time.Second
	}
	if c.Tick <= 0 {
		c.Tick = 50 * time.Millisecond
	}
	if c.SweepEvery <= 0 {
		c.SweepEvery = 500 * time.Millisecond
	}
	if c.RedeliverAfter <= 0 {
		c.RedeliverAfter = 5 * time.Second
	}
}

// Daemon 见包注释。
type Daemon struct {
	Cfg Config
	B   *board.Board
	P   *pool.Pool
	D   *dispatch.Dispatcher
	ID  board.Identity

	mu      sync.Mutex
	ridToW  map[string]string // rid→workerID(terminal 时归还;E1 进程内账,丢=Release 幂等无害)
	stopped bool
}

// New 组装守护(未跑循环):开库→接管→起池→接线 failover 回调。
func New(cfg Config, drv pool.DriverAdapter, clk clock.Clock) (*Daemon, error) {
	cfg.fill()
	b, err := board.Open(cfg.BoardPath, clk)
	if err != nil {
		return nil, err
	}
	epoch, err := b.Takeover(context.Background(), cfg.BoardID, cfg.Owner, int64(cfg.LeaseTTL/time.Second))
	if err != nil {
		b.Close()
		return nil, err
	}
	id := board.Identity{Owner: cfg.Owner, Epoch: epoch}
	p := pool.New(cfg.Pool, drv, clk)
	if err := p.Start(context.Background()); err != nil {
		b.Close()
		return nil, err
	}
	d := &Daemon{Cfg: cfg, B: b, P: p, ID: id, ridToW: map[string]string{}}
	rid := newRIDGen()
	d.D = dispatch.New(p, b, id, rid)
	p.OnWorkerDead = func(workerID string) {
		_ = d.D.OnWorkerDead(context.Background(), workerID)
	}
	return d, nil
}

var ridCounter struct {
	mu sync.Mutex
	n  int64
}

func newRIDGen() dispatch.RIDGen {
	return func() string {
		ridCounter.mu.Lock()
		defer ridCounter.mu.Unlock()
		ridCounter.n++
		return fmt.Sprintf("rid-%d", ridCounter.n)
	}
}

// Run 主循环:节拍派发+续租+定时 reconciler;ctx 取消=优雅退出。
func (d *Daemon) Run(ctx context.Context) error {
	tick := time.NewTicker(d.Cfg.Tick)
	defer tick.Stop()
	sweep := time.NewTicker(d.Cfg.SweepEvery)
	defer sweep.Stop()
	renew := time.NewTicker(d.Cfg.LeaseTTL / 3)
	defer renew.Stop()
	for {
		select {
		case <-ctx.Done():
			return d.Shutdown()
		case <-renew.C:
			if ok, err := d.B.Renew(ctx, d.Cfg.BoardID, d.ID, int64(d.Cfg.LeaseTTL/time.Second)); err == nil && !ok {
				// 失租=被接管/过期:守护自降(guarded 写会全拒),退出交由上层重启接管
				return fmt.Errorf("mcsd: lease lost (owner=%s epoch=%d)", d.ID.Owner, d.ID.Epoch)
			}
		case <-tick.C:
			d.dispatchRound(ctx)
		case <-sweep.C:
			d.sweepRound(ctx)
		}
	}
}

// dispatchRound 一轮派发:ready 全试派;缓冲有闲回灌。
func (d *Daemon) dispatchRound(ctx context.Context) {
	ready, err := d.B.ReadyNodes(ctx)
	if err != nil {
		return
	}
	for _, nodeID := range ready {
		s, err := d.B.GetNode(ctx, nodeID)
		if err != nil || s.Status != "ready" {
			continue
		}
		wid, rid, err := d.D.DispatchOne(ctx, nodeID, s.Generation)
		if err == nil && wid != "" {
			d.mu.Lock()
			d.ridToW[rid] = wid
			d.mu.Unlock()
		}
	}
	// 缓冲回灌:有闲才出队
	for {
		if _, idle := d.P.Size(); idle == 0 {
			return
		}
		env, ok := d.P.DequeueReady()
		if !ok {
			return
		}
		s, err := d.B.GetNode(ctx, env.NodeID)
		if err != nil || s.Status != "ready" {
			continue
		}
		wid, rid, err := d.D.DispatchOne(ctx, env.NodeID, s.Generation)
		if err == nil && wid != "" {
			d.mu.Lock()
			d.ridToW[rid] = wid
			d.mu.Unlock()
		}
	}
}

func (d *Daemon) sweepRound(ctx context.Context) {
	rep, err := d.B.Sweep(ctx, d.ID, int64(d.Cfg.RedeliverAfter/time.Second))
	if err != nil {
		return
	}
	for _, nodeID := range rep.RedeliverCandidates {
		s, err := d.B.GetNode(ctx, nodeID)
		if err != nil || !s.DeliveryID.Valid {
			continue
		}
		if oc, _ := d.B.Redeliver(ctx, nodeID, s.Generation, s.DeliveryID.String, d.ID); oc == board.OutcomeApplied {
			// 同 rid 重投外呼(执行点幂等);worker 不可得时留下一轮
			d.mu.Lock()
			wid := d.ridToW[s.DeliveryID.String]
			d.mu.Unlock()
			_ = wid // E1:进程内 loopback 由回执自愈;真实重投外呼在 E2 接 socket
		}
	}
}

// ReceiptProcessing worker 回执入口:dispatched→running。
func (d *Daemon) ReceiptProcessing(ctx context.Context, nodeID string, gen int64, rid string) error {
	_, err := d.B.Processing(ctx, nodeID, gen, rid, d.ID)
	return err
}

// ReceiptTerminal worker 回执入口:终结销账+归还 worker(Release 幂等)。
func (d *Daemon) ReceiptTerminal(ctx context.Context, nodeID string, gen int64, rid string, ok bool, ref string) error {
	oc, err := d.B.Terminal(ctx, nodeID, gen, rid, ok, ref, d.ID)
	if err != nil {
		return err
	}
	if oc == board.OutcomeApplied {
		d.mu.Lock()
		wid := d.ridToW[rid]
		delete(d.ridToW, rid)
		d.mu.Unlock()
		if wid != "" {
			d.P.Release(wid)
		}
	}
	return nil
}

// Shutdown 优雅退出:停池、关库(幂等)。
func (d *Daemon) Shutdown() error {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return nil
	}
	d.stopped = true
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = d.P.Stop(ctx)
	return d.B.Close()
}
