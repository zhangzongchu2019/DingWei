// feishu-fanout — T17 飞书消息 fan-out 代理(独立服务,设计 docs/v0.1/t17-feishu-fanout-proxy-design-v1.md)。
// 配置全 env(设计 §5);SIGHUP=热重载白名单;encrypt_key 旋转=更新 FANOUT_CHANNELS 后重启(或后续接热重载)。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/fanout"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

type fakeSender struct{}

func (fakeSender) Send(ctx context.Context, out feishu.OutMessage) (string, error) {
	return "fake-msg-id", nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := fanout.Config{
		ProdURL:       os.Getenv("FANOUT_PROD_URL"),
		TestURL:       os.Getenv("FANOUT_TEST_URL"),
		WhitelistFile: os.Getenv("FANOUT_WHITELIST_FILE"),
		OutboundToken: os.Getenv("FANOUT_OUTBOUND_TOKEN"),
		Logger:        logger,
	}
	if cfg.ProdURL == "" {
		return fmt.Errorf("FANOUT_PROD_URL is required")
	}
	if raw := os.Getenv("FANOUT_CHANNELS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.Channels); err != nil {
			return fmt.Errorf("FANOUT_CHANNELS bad json: %w", err)
		}
	}
	if v := os.Getenv("FANOUT_QUEUE_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("FANOUT_QUEUE_SIZE: %w", err)
		}
		cfg.QueueSize = n
	}
	// 出站测试路凭据(代理代持,测试 Hub 不持真凭据=安全边界,设计 §4)
	if appID, secret := os.Getenv("FANOUT_LARK_APP_ID"), os.Getenv("FANOUT_LARK_APP_SECRET"); appID != "" && secret != "" {
		bot := os.Getenv("FANOUT_LARK_BOT_CHANNEL")
		gw, err := feishu.NewLarkGateway(bot, appID, secret, logger)
		if err != nil {
			return fmt.Errorf("lark gateway: %w", err)
		}
		cfg.Sender = gw
		logger.Info("outbound enabled", "bot_channel", bot, "app_id", appID)
	} else if os.Getenv("FANOUT_OUTBOUND_FAKE") == "1" {
		// e2e/联调用假发送器(同 hub FEISHU_TRANSPORT=fake 先例),不触真 Lark
		cfg.Sender = fakeSender{}
		logger.Info("outbound enabled (fake)")
	} else {
		logger.Warn("outbound disabled (FANOUT_LARK_APP_ID/SECRET unset)")
	}

	// v2.3(§E/§F):声明式配置文件+PG spool+管理面。
	cfg.ConfigPath = os.Getenv("FANOUT_CONFIG_FILE") // fanout.yaml,文件=唯一真源
	if dsn := os.Getenv("FANOUT_PG_DSN"); dsn != "" {
		pool, err := openSpoolPool(context.Background(), dsn)
		if err != nil {
			return fmt.Errorf("FANOUT_PG_DSN: %w", err)
		}
		defer pool.Close()
		cfg.SpoolPool = pool
	}
	if raw := os.Getenv("FANOUT_ADMIN_TOKENS"); raw != "" { // {"<token>":{"id":"ops-a","role":"admin"}}
		if err := json.Unmarshal([]byte(raw), &cfg.AdminTokens); err != nil {
			return fmt.Errorf("FANOUT_ADMIN_TOKENS bad json: %w", err)
		}
	}
	cfg.TestInject = os.Getenv("FANOUT_TEST_INJECT") == "1" // 仅 e2e;生产勿设

	srv := fanout.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.Start(ctx)

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			// §E:SIGHUP 与 admin PUT 同一校验管道;失败=保旧配置跑
			if err := srv.ReloadFromFile(ctx); err != nil {
				logger.Error("SIGHUP reload rejected (keeping old config)", "error", err)
			} else {
				logger.Info("SIGHUP: config+whitelist reloaded")
			}
		}
	}()

	addr := os.Getenv("FANOUT_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8795"
	}
	httpSrv := &http.Server{Addr: addr, Handler: srv, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("feishu-fanout started", "addr", addr,
		"prod", cfg.ProdURL, "test", cfg.TestURL, "whitelist", cfg.WhitelistFile)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-stop:
	}
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	return httpSrv.Shutdown(shutCtx)
}

// openSpoolPool 建 fanout spool 的产品池（唯一建池处；DSN 仍只在装配层读取，见 FPG-1）。
//
// 🔴 抽成具名函数不是为了好看，是为了让耐久判据绑在【产品真的会建出来的那个会话】上：
//
//	FPG-3 半一要求「真实测试连接的 synchronous_commit 必须为 on」。若测试自己另调一次
//	OpenPG，它证明的就是夹具的会话参数，与装配层无关 —— 交接件坑#3「只检查 DSN 或工具
//	默认值不足以证明调用连接未漂移」说的正是这类冒充。共用本函数后，被测会话与产品会话
//	由同一处代码建，参数漂移无处可藏（本目录 durability_test.go 直接调它）。
//
// MaxOpenConns(1)：沿用 spool 收编前的既有并发语义（单连接串行，杜绝游标互等），
// 由 repo.OpenPG 的 Option 表达，不在此处另设开关。
func openSpoolPool(ctx context.Context, dsn string) (*repo.Pool, error) {
	return repo.OpenPG(ctx, dsn, repo.MaxOpenConns(1))
}
