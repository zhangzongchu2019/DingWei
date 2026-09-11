// Command fcs 是 A3「飞书渠道服务」(Feishu Channel Service) 的独立进程入口 (走甲:
// 最小独立进程 + 简单 HTTP RPC, 张宗楚 2026-07-21).
//
// 它把飞书边界从 hub 单体中剥离为一个独立可部署、可灰度的进程:进程内装配 FCS
// (fcs.New: bot 凭证 + gateway + fanout),驱动入站 (WS 或 webhook → dedup → 广播),
// 对 N 个 hub 版本暴露简单的 HTTP RPC (send / resolve / inbound 流)。凭证只在 FCS
// 内部解析,绝不过 hub↔FCS 线 (design §8)。
//
// 本轮不做 (张宗楚 后补):provenance 两层信任根、durable fanout ledger + 重试、
// 密钥/epoch 管理。入站 fanout 为尽力而为 at-most-once,以最小可灰度为目标。
package main

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/version"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/fcs"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("fcs exited", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	bs := config.LoadBootstrap()
	if err := os.MkdirAll(bs.DataDir, 0o750); err != nil {
		return err
	}
	st, err := store.OpenSQLite(bs.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	// Assemble the FCS boundary (bot creds + gateway + fanout) and drive inbound.
	a, err := fcs.New(ctx, logger, st, bs)
	if err != nil {
		return err
	}
	fatalErr := make(chan error, 1)
	go func() {
		if err := a.Run(ctx); err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			fatalErr <- err
		}
	}()

	mux := http.NewServeMux()
	// hub-facing RPC subtree: send / resolve / inbound fanout stream / receipt
	// (all under /rpc/ — NewRPCServer's mux routes method+path internally).
	mux.Handle("/rpc/", fcs.NewRPCServer(a.Service, logger))
	// Feishu-facing webhook ingress (moved from hub); WS-transport tenants ignore it.
	mux.Handle("POST /webhook/{channel}", fcs.NewWebhookIngress(a.Provider, a.Service, logger))
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := strings.TrimSpace(os.Getenv("FCS_ADDR"))
	if addr == "" {
		addr = ":8081"
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("fcs started", "addr", addr, "db", bs.DBPath, "transport", bs.FeishuTransport,
			"version", version.Version, "commit", version.Commit)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatalErr <- err
		}
	}()

	var runErr error
	select {
	case runErr = <-fatalErr:
		stop()
	case <-ctx.Done():
	}
	logger.Info("fcs shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil && runErr == nil {
		runErr = err
	}
	return runErr
}
