// Package pool 实现多CLI调度的 worker 池（冻结 spec §1 池生命周期 / §2 轮换与 failover）。
//
// 语义来源（两签冻结件,不在此复述设计,只标条目号）:
//   - multi-cli-scheduler-spec-s6-v0.5.md §1/§2
//   - multi-cli-scheduler-test-spec-draft.md I1-I5(伸缩) / J1-J3(轮换/failover)
//   - multi-cli-scheduler-M1-view.md §3b: W1' 已终选 A —— 择人链①原 worker idle 派回→②否则 Board 工件重建;
//     派发路径【禁止】任何 resume/会话文件读取(V44 负测,fenced handoff=未来 B 窗)。
//
// 本包只管"池"与"择人";Board 侧 guarded 写(send/reassign/receipt)在 M2 接线。
package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

// Caps 是 worker 能力集(DEA 能力表条目名→是否可用)。池对上报的能力=成员保守交集(I5)。
type Caps map[string]bool

// Envelope 是一次投递的最小载体(M1 仅池层流转;rid 幂等在执行点,M2 接 Board)。
type Envelope struct {
	NodeID     string
	Generation int64
	RID        string // delivery_id:同 rid 重投=幂等(执行点拦),不同 rid=新投递
	Payload    []byte
}

// Worker 是单个子CLI 会话的句柄(由 DriverAdapter 产出)。
type Worker interface {
	ID() string
	Health(ctx context.Context) error // nil=存活
	Caps() Caps
	Deliver(ctx context.Context, env Envelope) error
	Stop(ctx context.Context) error
}

// DriverAdapter 抽象"起一个新 worker"。生产=各 CLI 驱动;测试=fake。
type DriverAdapter interface {
	Spawn(ctx context.Context) (Worker, error)
}

// State worker 在池内的状态。
type State int

const (
	StateIdle State = iota
	StateBusy
	StateDead
)

// Config 池参数。数值默认取冻结 spec §1 的 [2,5];细调属实现期 D15。
type Config struct {
	Min      int           // 常驻下限(保 failover,I3)
	Max      int           // 上限(I1 扩容不越)
	IdleTTL  time.Duration // 空闲超此时长且高于下限→缩(I2)
	QueueCap int           // 池满全忙时的缓冲上限(I4:有界不静默丢)
}

func (c *Config) fill() {
	if c.Min <= 0 {
		c.Min = 2
	}
	if c.Max < c.Min {
		c.Max = 5
		if c.Max < c.Min {
			c.Max = c.Min
		}
	}
	if c.IdleTTL <= 0 {
		c.IdleTTL = 10 * time.Minute
	}
	if c.QueueCap <= 0 {
		c.QueueCap = 64
	}
}

// ErrQueueFull 池满全忙且缓冲亦满:显式拒绝,上游按幂等重投(I4:绝不静默丢)。
var ErrQueueFull = errors.New("mcs/pool: queue full (bounded, not silently dropped)")

// ErrStopped 池已关闭。
var ErrStopped = errors.New("mcs/pool: pool stopped")

type entry struct {
	w         Worker
	state     State
	idleSince time.Time
}

// Pool 见包注释。所有导出方法并发安全。
type Pool struct {
	mu      sync.Mutex
	cfg     Config
	drv     DriverAdapter
	clk     clock.Clock
	entries []*entry
	rr      int // round-robin 游标(J1)
	queue   []Envelope
	stopped bool
	snap    snapshotter

	// OnWorkerDead 在 reap 判死并移除后回调(J2)。M2 在此接 Board 侧 reassign(guarded,换代)。
	// 池层不直接写 Board——判死→重派的账全部走 Board 原语,池只报告事实。
	OnWorkerDead func(workerID string)
}

// New 构造池(未启动)。clk 注入以便测试 IdleTTL。
func New(cfg Config, drv DriverAdapter, clk clock.Clock) *Pool {
	cfg.fill()
	if clk == nil {
		clk = clock.Real{}
	}
	return &Pool{cfg: cfg, drv: drv, clk: clk, snap: snapshotter{clk: clk}}
}

// Start 起满下限个 worker(§1 起)。
func (p *Pool) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return ErrStopped
	}
	for len(p.entries) < p.cfg.Min {
		if err := p.spawnLocked(ctx); err != nil {
			return fmt.Errorf("mcs/pool: start spawn: %w", err)
		}
	}
	return nil
}

// Stop 停全部 worker 并关池(§1 停)。
func (p *Pool) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	var firstErr error
	for _, e := range p.entries {
		if err := e.w.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	p.entries = nil
	return firstErr
}

func (p *Pool) spawnLocked(ctx context.Context) error {
	w, err := p.drv.Spawn(ctx)
	if err != nil {
		return err
	}
	p.entries = append(p.entries, &entry{w: w, state: StateIdle, idleSince: p.clk.Now()})
	return nil
}

// Size 返回(总数, idle 数)。
func (p *Pool) Size() (total, idle int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.state == StateIdle {
			idle++
		}
	}
	return len(p.entries), idle
}

// Pick 为一次派发择人:
//   - 亲和链①(W1' A 案):preferID 非空且该 worker 存活且 idle→派回它(仅 pick 权重,零等待);
//   - 否则 round-robin 取下一个 idle(J1);
//   - 无 idle 且未达上限→扩一个(I1);
//   - 达上限且全忙→返回 (nil,false):上游走 Enqueue 缓冲(I4)。
//
// 择中的 worker 立即置 busy(由调用方在收到 terminal receipt 后 Release)。
// 链①不可用即静默落链②(任何 worker 皆可从 Board 工件重建,正确性不依赖亲和)。
func (p *Pool) Pick(ctx context.Context, preferID string) (Worker, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil, false, ErrStopped
	}
	// 链①:原 worker idle 派回(不等待——忙/死即落下一步,零新状态)。
	if preferID != "" {
		for _, e := range p.entries {
			if e.w.ID() == preferID && e.state == StateIdle {
				e.state = StateBusy
				return e.w, true, nil
			}
		}
	}
	// 链②入口:round-robin 下一个 idle(J1)。
	n := len(p.entries)
	for i := 0; i < n; i++ {
		e := p.entries[(p.rr+i)%n]
		if e.state == StateIdle {
			p.rr = (p.rr + i + 1) % n
			e.state = StateBusy
			return e.w, true, nil
		}
	}
	// I1:有活、无闲、未达上限→扩。
	if n < p.cfg.Max {
		if err := p.spawnLocked(ctx); err != nil {
			return nil, false, fmt.Errorf("mcs/pool: scale-up spawn: %w", err)
		}
		e := p.entries[len(p.entries)-1]
		e.state = StateBusy
		return e.w, true, nil
	}
	// 达上限全忙:交上游缓冲(I4)。
	return nil, false, nil
}

// Release 归还 worker(terminal receipt 后)。未知 ID 幂等忽略(worker 可能已被 reap)。
func (p *Pool) Release(workerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.w.ID() == workerID && e.state == StateBusy {
			e.state = StateIdle
			e.idleSince = p.clk.Now()
			return
		}
	}
}

// Enqueue 入有界缓冲(I4)。满→ErrQueueFull(显式,上游幂等重投,绝不静默丢)。
func (p *Pool) Enqueue(env Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return ErrStopped
	}
	if len(p.queue) >= p.cfg.QueueCap {
		return ErrQueueFull
	}
	p.queue = append(p.queue, env)
	return nil
}

// DequeueReady 取出一个缓冲件(调度循环在有 idle 时消费)。
func (p *Pool) DequeueReady() (Envelope, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return Envelope{}, false
	}
	env := p.queue[0]
	p.queue = p.queue[1:]
	return env, true
}

// QueueLen 当前缓冲长度(观测用)。
func (p *Pool) QueueLen() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queue)
}

// IDs 当前池内 worker id 集(孤儿节点扫描用)。
func (p *Pool) IDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.entries))
	for _, e := range p.entries {
		out = append(out, e.w.ID())
	}
	return out
}

// Get 按 id 取 worker(redeliver 外呼用;不存在=false)。
func (p *Pool) Get(id string) (Worker, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.w.ID() == id {
			return e.w, true
		}
	}
	return nil, false
}

// Reap 健康检查:判死→移除+回调 OnWorkerDead(J2);低于下限→补拉新 worker(J3 恢复=补拉/重 Spawn 纳入)。
// 判死的账面后果(节点重派)由 M2 的 Board guarded reassign 处理,池层零 Board 写。
func (p *Pool) Reap(ctx context.Context) {
	p.mu.Lock()
	var dead []string
	kept := p.entries[:0]
	for _, e := range p.entries {
		if err := e.w.Health(ctx); err != nil {
			e.state = StateDead
			dead = append(dead, e.w.ID())
			_ = e.w.Stop(ctx) // 尽力而为;失败不阻塞 reap
			continue
		}
		kept = append(kept, e)
	}
	p.entries = kept
	for len(p.entries) < p.cfg.Min && !p.stopped {
		if err := p.spawnLocked(ctx); err != nil {
			break // 起不来留给下轮 reap;不 panic 不静默吞(错误在 spawn 处已包装)
		}
	}
	cb := p.OnWorkerDead
	p.mu.Unlock()
	if cb != nil {
		for _, id := range dead {
			cb(id)
		}
	}
}

// Shrink 缩容:idle 超 IdleTTL 且移除后总数仍≥下限→停并移除(I2);任何情况下不破下限(I3)。
func (p *Pool) Shrink(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clk.Now()
	remaining := len(p.entries) // 尚未定去留的+已保留的总量,移除即减
	kept := p.entries[:0]
	for _, e := range p.entries {
		if e.state == StateIdle && now.Sub(e.idleSince) > p.cfg.IdleTTL && remaining > p.cfg.Min {
			_ = e.w.Stop(ctx)
			remaining--
			continue
		}
		kept = append(kept, e)
	}
	p.entries = kept
}

// CapsIntersection 池能力=成员保守交集(I5)。空池→空集。
func (p *Pool) CapsIntersection() Caps {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := Caps{}
	for i, e := range p.entries {
		wc := e.w.Caps()
		if i == 0 {
			for k, v := range wc {
				if v {
					out[k] = true
				}
			}
			continue
		}
		for k := range out {
			if !wc[k] {
				delete(out, k)
			}
		}
	}
	return out
}
