// dingwei-datad — T18 VS 统一 API 服务(设计冻结版 t18-design-frozen)。
// 唯一 PG 入口;env 配置;token 注册表=配置文件(600,PG 不存明文)。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/datad"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := datad.Config{Logger: logger}

	// token 注册表:DATAD_TOKENS_FILE(600 JSON {"<token>":{"session":"zzc-x-2642","role":"member"}})
	tokensFile := os.Getenv("DATAD_TOKENS_FILE")
	if tokensFile == "" {
		return fmt.Errorf("DATAD_TOKENS_FILE is required")
	}
	raw, err := os.ReadFile(tokensFile)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &cfg.Tokens); err != nil {
		return fmt.Errorf("tokens file bad json: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	switch dsn := os.Getenv("DATAD_PG_DSN"); {
	case dsn != "":
		pg, err := datad.OpenPG(ctx, dsn)
		if err != nil {
			return fmt.Errorf("pg: %w", err) // fail-closed:PG 不可达=不起(P9)
		}
		defer pg.Close()
		cfg.Store = pg
		logger.Info("store=pg")
	case os.Getenv("DATAD_STORE_MEM") == "1": // 联调 dev 模式(易失,勿生产)
		cfg.Store = datad.NewMemStore()
		logger.Warn("store=mem (dev only, volatile)")
	default:
		return fmt.Errorf("DATAD_PG_DSN required (or DATAD_STORE_MEM=1 for dev)")
	}

	if dir := os.Getenv("DATAD_KB_DIR"); dir != "" {
		cfg.KB = &datad.FSGrepProvider{Dir: dir}
		logger.Info("kb provider=fsgrep", "dir", dir)
	}

	addr := os.Getenv("DATAD_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8796"
	}
	h := datad.New(cfg)
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("dingwei-datad started", "addr", addr, "tokens", len(cfg.Tokens))

	// P8 token 轮换:SIGHUP 重读 tokens 文件原子换表(双活窗=文件同含新旧)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			raw, err := os.ReadFile(tokensFile)
			if err != nil {
				logger.Error("tokens 重读失败,保旧表(告警)", "error", err)
				continue
			}
			var next map[string]datad.Identity
			if err := json.Unmarshal(raw, &next); err != nil {
				logger.Error("tokens 解析失败,保旧表(告警)", "error", err)
				continue
			}
			h.ReloadTokens(next)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-stop:
	}
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	return srv.Shutdown(shutCtx)
}
