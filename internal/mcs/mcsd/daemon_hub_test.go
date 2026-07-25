package mcsd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/hubdriver"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

// hubEchoDriver:worker 收 envelope 后把回执按 mcs_receipt_v1 线格式 JSON 序列化,
// 经 Daemon.HandleHubReceipt 回注——模拟真 CLI 工人经 hub 的完整线格式往返。
type hubEchoDriver struct {
	mu     sync.Mutex
	n      int
	daemon *Daemon
}

func (h *hubEchoDriver) Bind(d *Daemon) { h.mu.Lock(); h.daemon = d; h.mu.Unlock() }

func (h *hubEchoDriver) Spawn(context.Context) (pool.Worker, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := &hubEchoWorker{id: fmt.Sprintf("hw%d", h.n), drv: h}
	h.n++
	return w, nil
}

type hubEchoWorker struct {
	id  string
	drv *hubEchoDriver
}

func (w *hubEchoWorker) ID() string                        { return w.id }
func (w *hubEchoWorker) Health(context.Context) error      { return nil }
func (w *hubEchoWorker) Caps() pool.Caps                   { return pool.Caps{} }
func (w *hubEchoWorker) Stop(context.Context) error        { return nil }
func (w *hubEchoWorker) Deliver(ctx context.Context, env pool.Envelope) error {
	w.drv.mu.Lock()
	d := w.drv.daemon
	w.drv.mu.Unlock()
	go func() {
		mk := func(phase string, ok bool) []byte {
			b, _ := json.Marshal(hubdriver.WireReceipt{
				Kind: "mcs_receipt_v1", NodeID: env.NodeID, Generation: env.Generation,
				RID: env.RID, Phase: phase, OK: ok, ResultRef: "ref-" + env.NodeID, From: w.id,
			})
			return b
		}
		_ = d.HandleHubReceipt(context.Background(), mk("processing", false))
		_ = d.HandleHubReceipt(context.Background(), mk("done", true))
	}()
	return nil
}

// E2E:种子节点经线格式往返(派发→wire processing→wire done)收敛 done。
func TestHubReceiptE2E(t *testing.T) {
	drv := &hubEchoDriver{}
	d, err := New(Config{
		BoardPath: filepath.Join(t.TempDir(), "board.db"),
		Owner:     "mcsd-hub",
		Pool:      pool.Config{Min: 1, Max: 2},
		Tick:      20 * time.Millisecond,
	}, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	drv.Bind(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	bctx := context.Background()
	if err := d.B.CreateTask(bctx, "t1", d.Cfg.BoardID, "seed", testRoles(), d.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.B.AddNode(bctx, "n1", "t1", 1, "ready", nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "n1 done via hub wire", func() bool {
		s, err := d.B.GetNode(bctx, "n1")
		return err == nil && s.Status == "done"
	})
	cancel()
	_ = d.Shutdown()
}

// 负例:入口闭合——坏 JSON/未知 kind/未知 phase 全显式拒。
func TestHubReceiptEntryClosed(t *testing.T) {
	d, err := New(Config{
		BoardPath: filepath.Join(t.TempDir(), "board.db"),
		Owner:     "mcsd-hub2",
		Pool:      pool.Config{Min: 1, Max: 1},
	}, &hubEchoDriver{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown()
	ctx := context.Background()
	if err := d.HandleHubReceipt(ctx, []byte("{bad")); err == nil {
		t.Fatal("坏 JSON 应拒")
	}
	bad1, _ := json.Marshal(hubdriver.WireReceipt{Kind: "evil", NodeID: "n", RID: "r", Phase: "done"})
	if err := d.HandleHubReceipt(ctx, bad1); err == nil {
		t.Fatal("未知 kind 应拒")
	}
	bad2, _ := json.Marshal(hubdriver.WireReceipt{Kind: "mcs_receipt_v1", NodeID: "n", RID: "r", Phase: "later"})
	if err := d.HandleHubReceipt(ctx, bad2); err == nil {
		t.Fatal("未知 phase 应拒")
	}
}
