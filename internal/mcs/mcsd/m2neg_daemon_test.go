package mcsd

// M2 负例组用例对号实测(守护/真 worker 链面):tester 冻结件
// docs/contract-cases/m2-negative-cases.md(blob 0e029ffd@43dfd5ef)。
// N1a/N1c 需真投递链(子进程 worker),复用 e2Rig;board 面各条见 board 包。

import (
	"context"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// TC-N1a:瞬态丢 ack(首投双回执丢)→Sweep 出 redeliver 候选→同 worker 重投→恢复正常收口;
// 判据:done ∧ delivery_attempt≥1 ∧ 同 worker ∧ worker 归还池(用后继节点可派证)。
func TestTCN1a_TransientLostAckRedeliverSameWorker(t *testing.T) {
	beh := statefulBehavior(map[string]WireBehavior{"n1": {DropProcessing: true, DropTerminal: true}})
	d, drv, _ := e2Rig(t, pool.Config{Min: 1, Max: 1}, beh)
	_ = drv
	ctx := context.Background()
	addTask(t, d, "t1", "n1")
	waitUntil(t, 15*time.Second, "redeliver 后同 worker 收口", func() bool {
		s, err := d.B.GetNode(ctx, "n1")
		return err == nil && s.Status == "done"
	})
	s, _ := d.B.GetNode(ctx, "n1")
	if s.DeliveryAttempt < 1 {
		t.Fatalf("delivery_attempt=%d want ≥1(经 redeliver)", s.DeliveryAttempt)
	}
	if s.Generation != 1 {
		t.Fatalf("gen=%d want 1(同 worker 重投,非换代)", s.Generation)
	}
	worker1 := s.AssignedAgent.String
	// worker 归还池证:单席池还能派后继节点,且派给同一 worker
	if err := d.B.AddNode(ctx, "n1b", "t1", 5, "ready", nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 15*time.Second, "worker 归还后可续派", func() bool {
		s2, err := d.B.GetNode(ctx, "n1b")
		return err == nil && s2.Status == "done"
	})
	s2, _ := d.B.GetNode(ctx, "n1b")
	if s2.AssignedAgent.String != worker1 {
		t.Fatalf("后继未派回同 worker:%s vs %s", s2.AssignedAgent.String, worker1)
	}
}

// TC-N1c:工人真死(SIGKILL)→Reap 判死→OnWorkerDead→Reassign(gen+1)→换 worker 收口;
// 判据:done ∧ gen>原 gen ∧ assigned_agent≠worker1。
func TestTCN1c_WorkerDeadReapReassignToNewWorker(t *testing.T) {
	beh := statefulBehavior(map[string]WireBehavior{"n1": {DelayMS: 60_000}}) // 首投挂死般慢
	d, drv, _ := e2Rig(t, pool.Config{Min: 2, Max: 2}, beh)
	ctx := context.Background()
	addTask(t, d, "t1", "n1")
	waitUntil(t, 10*time.Second, "在飞", func() bool {
		s, err := d.B.GetNode(ctx, "n1")
		return err == nil && s.AssignedAgent.Valid && s.Status != "ready"
	})
	s0, _ := d.B.GetNode(ctx, "n1")
	worker1 := s0.AssignedAgent.String
	gen0 := s0.Generation
	for _, w := range drv.Workers() {
		if w.ID() == worker1 {
			w.Kill9()
		}
	}
	waitUntil(t, 15*time.Second, "判死换代收口", func() bool {
		s, err := d.B.GetNode(ctx, "n1")
		return err == nil && s.Status == "done"
	})
	s1, _ := d.B.GetNode(ctx, "n1")
	if s1.Generation <= gen0 {
		t.Fatalf("gen=%d 未换代(原 %d)", s1.Generation, gen0)
	}
	if s1.AssignedAgent.String == worker1 {
		t.Fatalf("仍派死 worker %s", worker1)
	}
}
