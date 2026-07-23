// cmd/mcsd 多CLI调度守护入口(E1:仅 loopback 驱动;真实 CLI 驱动=后续段)。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/mcsd"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

func main() {
	// worker 模式:被 daemon 以子进程拉起(E2 真进程边界)。
	if os.Getenv("MCS_WORKER_SOCK") != "" {
		if err := mcsd.RunWorker(); err != nil {
			log.Fatalf("mcs worker: %v", err)
		}
		return
	}
	var (
		boardPath = flag.String("board", "mcs-board.db", "Board SQLite 路径")
		boardID   = flag.String("board-id", "board1", "board id")
		owner     = flag.String("owner", "mcsd", "调度者身份(问责主体)")
		min       = flag.Int("pool-min", 2, "池下限")
		max       = flag.Int("pool-max", 5, "池上限")
	)
	flag.Parse()

	drv := &mcsd.LoopbackDriver{}
	d, err := mcsd.New(mcsd.Config{
		BoardPath: *boardPath,
		BoardID:   *boardID,
		Owner:     *owner,
		Pool:      pool.Config{Min: *min, Max: *max},
		Tick:      100 * time.Millisecond,
		SweepEvery: time.Second,
	}, drv, nil)
	if err != nil {
		log.Fatalf("mcsd: %v", err)
	}
	drv.Bind(d)

	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("mcsd: shutting down")
		cancel()
	}()
	log.Printf("mcsd: up (board=%s owner=%s epoch=%d)", *boardPath, *owner, d.ID.Epoch)
	if err := d.Run(ctx); err != nil {
		log.Fatalf("mcsd: run: %v", err)
	}
}
