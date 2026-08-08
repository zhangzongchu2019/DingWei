// Package dispatch 是调度循环粘合层:pool(择人)× board(guarded 记账)× worker(投递外呼)。
//
// 语义来源(冻结件):M0 3.1 send crash cuts(记账先行→外呼,至少一次投递+执行点恰一次)/
// §2 failover(判死→3.2 reassign 换代→重派)/M1-View §3b A案择人链(①idle 派回→②Board 重建)。
// 派发路径零 resume/会话文件读取(V44 负测;fenced handoff=未来 B 窗)。
package dispatch

import (
	"context"
	"fmt"
	"sync"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// RIDGen 生成投递 id(生产=uuid;测试=可控序列)。
type RIDGen func() string

// Dispatcher 见包注释。
type Dispatcher struct {
	P   *pool.Pool
	B   *board.Board
	ID  board.Identity
	RID RIDGen

	mu     sync.Mutex
	origin map[string]string // node_id→原 worker(A案链①亲和的 pick 权重;纯内存提示,丢了=退化链②,正确性无损)
}

// New 构造;OnWorkerDead 由调用方接到 pool.OnWorkerDead。
func New(p *pool.Pool, b *board.Board, id board.Identity, rid RIDGen) *Dispatcher {
	return &Dispatcher{P: p, B: b, ID: id, RID: rid, origin: map[string]string{}}
}

// DispatchOne 派发单个 ready 节点:择人(A案链)→Send 记账→Deliver 外呼(记账先行)。
// 返回 (投递到的 workerID, rid, error)。池满全忙→入池缓冲,返回 ("","",nil)。
func (d *Dispatcher) DispatchOne(ctx context.Context, nodeID string, gen int64) (string, string, error) {
	d.mu.Lock()
	prefer := d.origin[nodeID] // 链①:原 worker(仅 pick 权重;不存在/忙=静默落链②)
	d.mu.Unlock()
	w, ok, err := d.P.Pick(ctx, prefer)
	if err != nil {
		return "", "", err
	}
	if !ok { // 池满全忙:有界缓冲(I4),调度循环有闲再消费
		if err := d.P.Enqueue(pool.Envelope{NodeID: nodeID, Generation: gen}); err != nil {
			return "", "", err
		}
		return "", "", nil
	}
	rid := d.RID()
	if err := d.B.RegisterAgent(ctx, w.ID(), "claude"); err != nil { // agent 行=FK 目标;幂等
		d.P.Release(w.ID())
		return "", "", err
	}
	oc, err := d.B.Send(ctx, nodeID, gen, rid, w.ID(), d.ID)
	if err != nil {
		d.P.Release(w.ID())
		return "", "", err
	}
	if oc != board.OutcomeApplied { // 旧代/态不符:归还 worker,不投递(零副作用)
		d.P.Release(w.ID())
		return "", "", fmt.Errorf("mcs/dispatch: send %s gen%d: %s", nodeID, gen, oc)
	}
	d.mu.Lock()
	d.origin[nodeID] = w.ID() // 记亲和提示(A案:内存级,非正确性依赖)
	d.mu.Unlock()
	// 记账已提交→外呼(崩于此间=dispatched 无回执→redeliver 收敛,crash cut ①)
	if err := w.Deliver(ctx, pool.Envelope{NodeID: nodeID, Generation: gen, RID: rid}); err != nil {
		// 投递失败:不回滚账(已 dispatched);收敛走 redeliver/deliver_fail(M4);此处仅报告
		return w.ID(), rid, fmt.Errorf("mcs/dispatch: deliver: %w", err)
	}
	return w.ID(), rid, nil
}

// SetOriginHint 记录节点的亲和提示(A案链①):修订节点建图时由上层调用,
// 把【原产出 node 的最后执行 worker】继承给修订 node(M1-View §3b:仅 pick 权重,非正确性依赖)。
func (d *Dispatcher) SetOriginHint(nodeID, workerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.origin[nodeID] = workerID
}

// OriginOf 读取节点当前生效的亲和提示(观测/继承用;不存在=""=链②)。
func (d *Dispatcher) OriginOf(nodeID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.origin[nodeID]
}

// OnWorkerDead J2 全链:判死 worker 的在途节点逐个 reassign(gen+1,guarded)→重派新 worker。
// reassign 被拒的节点(delivery_unresolved 注记/steer 预留/并发换代)按 spec 留给对应收敛路径,不硬闯。
func (d *Dispatcher) OnWorkerDead(ctx context.Context, workerID string) error {
	nodes, err := d.B.ActiveNodesOf(ctx, workerID)
	if err != nil {
		return err
	}
	var firstErr error
	for nodeID, gen := range nodes {
		oc, err := d.B.Reassign(ctx, nodeID, gen, "", d.ID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if oc != board.OutcomeApplied {
			continue // 注记期/预留期/已被他人换代:拒,零副作用(收敛后自然回 ready)
		}
		d.mu.Lock()
		delete(d.origin, nodeID) // 原 worker 已死:亲和提示作废
		d.mu.Unlock()
		if _, _, err := d.DispatchOne(ctx, nodeID, gen+1); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
