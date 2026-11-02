// Command workpulse 是 WorkPulse 飞书工作管理机器人 / 消息路由平台 的入口。
//
// 详见 docs/v0.1/产品规范.md。本文件负责装配：加载 bootstrap → 打开 SQLite → 迁移 →
// seed 管理员 → 启动 HTTP（webhook + M9 后台 + healthz）→ 优雅退出。
package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/zhangzongchu2019/dingwei/internal/admin"
	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/coordination"
	"github.com/zhangzongchu2019/dingwei/internal/datad"
	"github.com/zhangzongchu2019/dingwei/internal/fcs"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
	"github.com/zhangzongchu2019/dingwei/internal/llm"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
	"github.com/zhangzongchu2019/dingwei/internal/portal"
	"github.com/zhangzongchu2019/dingwei/internal/reminder"
	"github.com/zhangzongchu2019/dingwei/internal/schedule"
	"github.com/zhangzongchu2019/dingwei/internal/scheduler"
	"github.com/zhangzongchu2019/dingwei/internal/secretbox"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/version"
	"github.com/zhangzongchu2019/dingwei/internal/worker"
)

var unsignedWebhookWarned sync.Map

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := execute(os.Args, os.Stdout, logger, run); err != nil {
		var stopErr *shutdownError
		if !errors.As(err, &stopErr) {
			logger.Error("workpulse exited", "error", err)
		}
		os.Exit(exitCode(err))
	}
}

func execute(args []string, out io.Writer, logger *slog.Logger, runServer func(*slog.Logger) error) error {
	if len(args) > 1 {
		if args[1] == "version" && len(args) == 2 {
			_, err := fmt.Fprintln(out, version.Version, version.Commit)
			return err
		}
		if args[1] == "bootstrap-admin" {
			return runBootstrapAdmin(context.Background(), args[2:], out, strings.TrimSpace(os.Getenv("DW_NEWPLAT_PG_DSN")))
		}
		if args[1] == "reset-password" {
			return runResetPassword(context.Background(), args[2:], out, strings.TrimSpace(os.Getenv("DW_NEWPLAT_PG_DSN")))
		}
		if args[1] == "promote-admin" {
			return runPromoteAdmin(context.Background(), args[2:], out, strings.TrimSpace(os.Getenv("DW_NEWPLAT_PG_DSN")))
		}
		if args[1] == "hosted-admin" {
			return runHostedAdmin(args[2:])
		}
		if args[1] == "helper-update-register" {
			return runHelperUpdateRegister(context.Background(), args[2:], out)
		}
		if args[1] != "validate-console-seed" || len(args) != 3 {
			return errors.New("usage: workpulse version | workpulse validate-console-seed <seed.json> | workpulse helper-update-register ... | workpulse hosted-admin register-node|seed-account ... | " + bootstrapAdminUsage + " | " + resetPasswordUsage + " | " + promoteAdminUsage)
		}
		if err := validateConsoleSeedFile(args[2]); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, "CONSOLE_SEED_VALID")
		return err
	}
	return runServer(logger)
}

func configureAdminFileBrowser(adm *admin.Server, logger *slog.Logger, issuerURL, publicURL, issuerToken, auditToken string) {
	values := []struct{ name, value string }{
		{"WP_FILE_ISSUER_URL", issuerURL},
		{"WP_FILE_PUBLIC_URL", publicURL},
		{"WP_FILE_ISSUER_TOKEN", issuerToken},
		{"WP_FILE_AUDIT_TOKEN", auditToken},
	}
	missing := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.value) == "" {
			missing = append(missing, value.name)
		}
	}
	if len(missing) != 0 {
		logger.Warn("admin file browser disabled: missing configuration", "missing_env", strings.Join(missing, ","))
		return
	}
	adm.Files = admin.NewHTTPFileBrowser(issuerURL, publicURL, issuerToken, auditToken)
	logger.Info("admin file browser configured")
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
	storageOwned := true
	defer func() {
		if storageOwned {
			_ = st.Close()
		}
	}()

	var runJobs lifecycle.Group
	var claims lifecycle.Group
	ctx, cancelRun := context.WithCancel(context.WithValue(context.Background(), runTasksKey{}, &runJobs))
	queueCtx, cancelQueues := context.WithCancel(context.Background())
	defer cancelQueues()
	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()
	var consumerJobs lifecycle.Group
	access := &httpAdmission{}
	inboundGate := &lifecycle.Group{}
	defer cancelRun()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if err := configureLegacySpecFreeze(ctx, st, os.Getenv("WP_LEGACY_SPEC_FROZEN")); err != nil {
		return err
	}
	consoleAuthMode, err := m8.ParseConsoleAuthEnabled(os.Getenv("WP_CONSOLE_AUTH_ENABLED"))
	if err != nil {
		return err
	}
	logger.Info("unified console authentication", "enabled", consoleAuthMode.Enabled())
	if consoleAuthMode.Enabled() {
		if err := seedConsoleACL(ctx, st, os.Getenv("WP_CONSOLE_SEED_FILE")); err != nil {
			return err
		}
	}
	if n, err := st.RecoverProcessing(ctx); err != nil {
		return err
	} else if n > 0 {
		logger.Warn("recovered processing messages", "count", n)
	}
	if seeded, err := admin.SeedAdmin(ctx, st, bs.AdminUser, bs.AdminInitPass); err != nil {
		return err
	} else if seeded {
		logger.Info("seeded initial admin", "user", bs.AdminUser)
	}

	mux := http.NewServeMux()
	writeQueueConfig := bus.AsyncDBQueueConfigFromEnv(logger)
	inbound := bus.NewBestEffortDBQueue(queueCtx, st, model.DirectionIn, writeQueueConfig)
	outbound := bus.NewBestEffortDBQueue(queueCtx, st, model.DirectionOut, writeQueueConfig)
	prefixHub := m8.New(st, consoleAuthMode)
	prefixHub.Outbound = outbound
	fileIssuerURL := strings.TrimSpace(os.Getenv("WP_FILE_ISSUER_URL"))
	filePublicURL := strings.TrimSpace(os.Getenv("WP_FILE_PUBLIC_URL"))
	fileIssuerCredential := strings.TrimSpace(os.Getenv("WP_FILE_ISSUER_TOKEN"))
	if fileIssuerURL != "" || filePublicURL != "" || fileIssuerCredential != "" {
		issuer, err := m8.NewHTTPFileTicketIssuer(fileIssuerURL, filePublicURL, fileIssuerCredential)
		if err != nil {
			return err
		}
		prefixHub.SetFileTicketIssuer(issuer)
		prefixHub.SetFileArchiveClient(issuer)
		prefixHub.SetFilePermanentClient(issuer)
		logger.Info("file capability issuer configured", "internal_url", fileIssuerURL, "public_url", filePublicURL)
	}
	if bs.L2DeepSeekAPIKey != "" {
		prefixHub.L2 = llm.NewDeepSeek(bs.L2DeepSeekAPIKey, bs.L2DeepSeekBaseURL, bs.L2DeepSeekModel)
		if n, err := strconv.Atoi(bs.L2Workers); err == nil && n > 0 {
			prefixHub.L2Config.Workers = n
		}
	} else {
		logger.Warn("L2 triage provider not configured", "env", "WP_L2_DEEPSEEK_API_KEY")
	}
	adm := admin.New(st)
	adm.Outbound = outbound
	adm.Prefix = prefixHub
	adm.SecretKey = bs.SecretKey
	configureAdminFileBrowser(adm, logger, fileIssuerURL, filePublicURL, fileIssuerCredential, strings.TrimSpace(os.Getenv("WP_FILE_AUDIT_TOKEN")))
	if datadURL := strings.TrimSpace(os.Getenv("DATAD_URL")); datadURL != "" {
		datadToken := strings.TrimSpace(os.Getenv("DATAD_TOKEN"))
		if datadToken == "" {
			// claudek3 M3:URL 配而 token 空=对 datad 恒 401,页面只见横幅难定位,启动即警示。
			logger.Warn("DATAD_URL set but DATAD_TOKEN empty — datad board will always 401")
		}
		adm.Datad = datad.NewClient(datadURL, datadToken)
		logger.Info("portal board direct-connect datad", "url", datadURL)
	}
	// 新平台不注册旧 admin 编组页（须在 Mount 之前置位；配置读错在下面 newPlatformConfigFromEnv 那里报）。
	_, adm.NewPlatform, _ = newPlatformConfigFromEnv()
	adm.Mount(mux)                          // /healthz /readyz /admin*
	portal.NewScheduleServer(st).Mount(mux) // /schedule/
	// Nova-260921-A(B1-b):admin 能力的 console 门(/console/admin/*),由 console 角色授权。
	// 🔴 必须在 adm.Prefix = prefixHub 之后 —— 门 B 的鉴权走的是那个 hub;
	//    s.Prefix 为 nil 时 MountConsole 注册的路由会**关死**(503),不是放行。
	adm.MountConsole(mux)
	coord := coordination.New(st, outbound, clock.Real{}, bs.GlobalSchedulePath)
	schedulerSvc := scheduler.New(scheduler.Config{
		TeamFile:       bs.ScheduleTeamFile,
		PersonalDir:    bs.SchedulePersonalDir,
		BackupDir:      bs.ScheduleBackupDir,
		ReportDir:      bs.SchedulePersonalDir,
		TranscriptDirs: splitCSV(bs.TranscriptDirs),
		NotifyChatID:   bs.ScheduleNotifyChat,
		NotifyBotID:    bs.ScheduleNotifyBot,
		EvidenceCron:   bs.ScheduleEvidenceCron,
		EvidenceTZ:     bs.ScheduleEvidenceTZ,
		Command:        bs.SchedulerCLI,
		ConfigDir:      bs.SchedulerConfigDir,
	}, nil, clock.Real{}, outbound)
	schedulerSvc.Repo = st
	schedulerSvc.Legacy = coord
	adm.Scheduler = schedulerSvc
	prefixHub.System = schedulerSvc
	scheduleSvc := schedule.New(st, clock.Real{}, nil)
	scheduleSvc.Coordinator = schedulerSvc
	var stopNotifyEndpoint func()
	var feishuGateway feishu.Gateway
	var feishuReceiver feishu.Receiver
	if rpcURL := strings.TrimSpace(os.Getenv("FCS_RPC_URL")); rpcURL != "" {
		// 🔴 hubID 必须**显式配置且全局唯一**,不再从主机名探测。
		//
		// 原实现:hubID, _ := os.Hostname(); 取不到则回退字面量 "hub-local"。
		// 而 fcs.Subscribe 是 s.sinks[hubID] = ... —— **赋值,不是追加**。
		// ⇒ 两个 Hub 实例若主机名相同(或都取不到而同时回退 "hub-local"),
		//   后订阅的会**静默顶掉**前一个,s.sinks 任何时刻只剩最后连上的那个。
		//
		// 外部症状是「飞书消息随机投到两边之一」,实际是「只有一个在位,
		// 而谁在位取决于谁最后连上」—— 广播逻辑(fanout)本身是对的,坏的是订阅表的 key。
		// 业主原始设计意图是「每条入站复制广播给多个 Hub」,该缺陷把它退化成了单播。
		//
		// 故:不设默认值、不回退主机名。**回退正是本缺陷的来源** ——
		// 一个"总能拿到某个值"的探测,会把配置错误变成运行期的静默单播,
		// 而不是启动期的明确失败。
		hubID := strings.TrimSpace(os.Getenv("WP_HUB_ID"))
		if hubID == "" {
			return fmt.Errorf("FCS_RPC_URL 已配置但 WP_HUB_ID 为空:" +
				"每个 Hub 实例必须有显式且全局唯一的 hub id。" +
				"若多实例共用同一 id,飞书入站订阅会互相顶替,表现为消息随机丢失。" +
				"请为本实例设置 WP_HUB_ID(例如 hub-dw2-a),不要依赖主机名")
		}
		logger.Info("fcs mode: remote rpc", "url", rpcURL, "hub_id", hubID)
		service := fcs.NewRPCClient(rpcURL, nil)
		// 版本取编译期常量(业主 2026-09-10 定):它回答「跑的是哪个构建」,
		// 与 WP_HUB_ID 回答的「是哪个实例」互补。灰度时两个都要知道。
		feishuGateway = fcs.NewHubGateway(service, hubID)
		feishuReceiver = fcs.NewHubReceiver(service, hubID, version.Version)
	} else {
		feishuGateway, feishuReceiver, err = buildFeishuGateway(ctx, logger, st, bs)
		if err != nil {
			return err
		}
	}
	configurePilotQuestionFeishu(prefixHub, feishuGateway)
	configureSpecClarification(prefixHub)
	if err := configureClarificationReplyAuthority(prefixHub, feishuGateway); err != nil {
		return err
	}
	adm.Collector = collectorOrNil(feishuGateway)
	if channels, err := st.ListBotChannels(ctx); err == nil {
		for _, ch := range channels {
			if ch.Active {
				prefixHub.RegisterBot(ch.ID, ch.Name)
			}
		}
	}
	processor := &worker.Processor{
		Inbound:  inbound,
		Outbound: outbound,
		Feishu:   feishuGateway,
		Schedule: scheduleSvc,
		Repo:     st,
		Prefix:   prefixHub,
		Coord:    coord,
	}
	reminders := reminder.New(st, outbound, clock.Real{}, nil)

	// 飞书 webhook 接收：URL 验证 / 明文或加密事件 / 归一化入接收队列。
	mux.HandleFunc("POST /webhook/{channel}", func(w http.ResponseWriter, r *http.Request) {
		if handled, err := handleFeishuWebhook(r.Context(), logger, st, inbound, r.PathValue("channel"), r, w); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		} else if handled {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"queued"}`))
	})
	mux.HandleFunc("GET /ws/service/{serviceID}", prefixHub.HandleWS)
	mux.HandleFunc("GET /ws/session/{sessionName}", prefixHub.HandleSessionWS)
	// 新平台（G3）：身份存储与新平台路由；其余 console／view 路由见 registerConsoleRoutes。
	var closePlatformStore func() error
	newplatCfg, newplatOn, newplatErr := newPlatformConfigFromEnv()
	if newplatErr != nil {
		return fmt.Errorf("newplat config: %w", newplatErr)
	}
	if newplatOn {
		npStore, npPool, closeNPStore, err := openNewPlatformStore(ctx, newplatCfg.pgDSN)
		if err != nil {
			return fmt.Errorf("newplat identity store: %w", err)
		}
		defer func() {
			if storageOwned {
				closeNPStore()
			}
		}()
		closePlatformStore = func() error { return npPool.Close() }
		if err := wireNewPlatform(ctx, mux, prefixHub, newplatCfg, npStore, npPool); err != nil {
			return fmt.Errorf("newplat wiring refused to start: %w", err)
		}
	}
	configureCoordinateReviewFeishu(prefixHub, feishuGateway)
	registerConsoleRoutes(mux, prefixHub, newplatOn)
	mux.HandleFunc("GET /links/{ownerKey}", prefixHub.HandleOwnerLinksPage)
	registerRetiredProvisionDownloads(mux)

	// View Service 对接内网口(仅 127.0.0.1 / 内网,不进公开 mux):
	// C2 事件订阅 / C3 鉴权 / C3 输入回投。View Service 作为内部消费者调用。
	internalMux := http.NewServeMux()
	internalMux.Handle("GET "+m8.HealthAuthPath, m8.NewHealthAuthHandler())
	internalMux.HandleFunc("GET /internal/view/{session}/events", prefixHub.HandleInternalViewEvents)
	internalMux.HandleFunc("POST /internal/console/cost-seed-authority", prefixHub.HandleConsoleMuxCostSeedAuthority)
	internalMux.HandleFunc("POST /internal/console/event-feeds/{feed_id}/bridges/{bridge_id}/events", prefixHub.HandleInternalConsoleMuxEvents)
	internalMux.HandleFunc("POST /internal/view/authorize", prefixHub.HandleInternalViewAuthorize)
	internalMux.HandleFunc("POST /internal/view/{session}/input", prefixHub.HandleInternalViewInput)
	internalMux.HandleFunc("POST /internal/view/{session}/interrupt", prefixHub.HandleInternalViewInterrupt)
	internalMux.HandleFunc("POST /internal/view/{session}/cancel", prefixHub.HandleInternalViewCancel)
	internalMux.HandleFunc("POST /internal/view-v2/{session}/resources", prefixHub.HandleInternalAgentDiskResources)
	internalMux.HandleFunc("POST /internal/view-v2/{session}/input", prefixHub.HandleInternalViewV2Input)
	internalMux.HandleFunc("POST /internal/view-v2/{session}/cancel", prefixHub.HandleInternalViewV2Cancel)
	internalMux.HandleFunc("POST /internal/view-v2/{session}/interrupt", prefixHub.HandleInternalViewV2Interrupt)
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WP_DEBUG_HOLD_LOCK")), "true") {
		internalMux.HandleFunc("POST /debug/hold-lock", prefixHub.HandleDebugHoldLock)
		internalMux.HandleFunc("GET /debug/lock-stats", prefixHub.HandleDebugLockStats)
	}
	internalAddr := strings.TrimSpace(os.Getenv("WP_INTERNAL_ADDR"))
	if internalAddr == "" {
		internalAddr = "127.0.0.1:8792"
	}
	internalSrv := &http.Server{Addr: internalAddr, Handler: access.Wrap("internal_http", obs.Default.Middleware("internal", internalMux)), ReadHeaderTimeout: 5 * time.Second}

	// 🔴 D(0.2.0.4)告警出口:**只挂 Unix socket,不挂 TCP**。
	//    首版挂 127.0.0.1:8792 并声称「宿主不可达」—— prodops 实测从宿主得到 400,该假设被证伪。
	//    本仓既有内网端点均验 HMAC 签名、不依赖端口不可达;改用 socket 后
	//    **既不依赖网络拓扑,也不新增凭据**。未配置 WP_NOTIFY_SOCKET ⇒ 整个出口不存在。
	if sock := strings.TrimSpace(os.Getenv("WP_NOTIFY_SOCKET")); sock != "" {
		notifyMux := http.NewServeMux()
		notifyMux.HandleFunc("POST /internal/notify", access.Wrap("notify_http", makeInternalNotifyHandler(outbound, logger)).ServeHTTP)
		if stopNotify, err := serveNotifySocket(sock, notifyMux, logger); err != nil {
			return fmt.Errorf("notify socket %s: %w", sock, err)
		} else {
			stopNotifyEndpoint = stopNotify
			defer stopNotify()
		}
	}

	// DW123-PER-RELEASE-OBSERVABILITY：运行身份进快照；有效配置只哈希 workpulseEffectiveConfig 的观测键集合（不含 secret）。
	obs.Default.SetIdentity(obs.Identity{
		Service:      "workpulse",
		Version:      version.Version,
		Commit:       version.Commit,
		ConfigSHA256: obs.ConfigSHA256(config.Line(workpulseEffectiveConfig())),
		ConfigScope:  "sha256 of effective_config line (workpulseEffectiveConfig observed keys only), not full config",
		DeployID:     obs.SanitizeDeployID(strings.TrimSpace(os.Getenv("WP_DEPLOY_ID"))),
	})
	srv := &http.Server{Addr: bs.Addr, Handler: access.Wrap("public_http", obs.Default.Middleware("public", mux)), ReadHeaderTimeout: 5 * time.Second}
	publicListener, publicAccept, err := listenWithHeartbeat(bs.Addr)
	if err != nil {
		return fmt.Errorf("listen public %s: %w", bs.Addr, err)
	}
	publicShutdownListener := &shutdownListener{Listener: publicListener}
	publicListener = publicShutdownListener
	defer publicListener.Close()
	internalListener, internalAccept, err := listenWithHeartbeat(internalAddr)
	if err != nil {
		return fmt.Errorf("listen internal %s: %w", internalAddr, err)
	}
	internalShutdownListener := &shutdownListener{Listener: internalListener}
	internalListener = internalShutdownListener
	defer internalListener.Close()
	mux.HandleFunc("GET /livez", livezHandler(prefixHub, st, publicAccept, internalAccept))

	go func() {
		// commit 随启动日志打出:运维无需 strings/反编译即可回答「线上是哪份源码」。
		logger.Info("workpulse started", "addr", bs.Addr, "db", bs.DBPath,
			"version", version.Version, "commit", version.Commit)
		// T-066:让「取的是谁给的值」可见。**不改任何默认值** ——
		// warn→enforce 这类渐进放量是正当做法,改默认值会破坏它;
		// 四例(WP_HUB_ID/WP_DB_PATH/FCS_ADDR/WP_NAME_ENFORCE)的共同毛病不是选错了默认值,
		// 而是默认值不出现在任何可见处 ⇒ 无人复核、无人知道现在处在哪一档。
		effective := workpulseEffectiveConfig()
		// 全量快照:两台机器可直接 diff,用以发现「同一份配置在两处生效值不同」
		logger.Info(config.Line(effective))
		// 需要人看见的子集
		for _, w := range config.Warnings(effective) {
			logger.Warn("配置落在有风险的默认值上", "key", w.Key, "value", w.Value, "risk", w.Warn)
		}
		if err := srv.Serve(publicListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err)
		}
	}()
	go func() {
		logger.Info("workpulse internal (view-service dock) started", "addr", internalAddr)
		if err := internalSrv.Serve(internalListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("internal http server", "error", err)
		}
	}()
	startAcceptPulse(ctx, publicListener.Addr().String(), 5*time.Second)
	startAcceptPulse(ctx, internalListener.Addr().String(), 5*time.Second)
	watchdogCtx, cancelWatchdog := context.WithCancel(context.Background())
	defer cancelWatchdog()
	watchdog := startSystemdWatchdog(watchdogCtx, logger, prefixHub)
	consumerDone, _ := consumerJobs.Enter("inbound")
	go func() {
		defer consumerDone()
		runInboundUntil(workCtx, &claims, processor.ProcessOne, 200*time.Millisecond)
	}()
	prefixHub.StartL2Workers(ctx)
	prefixHub.StartTaskFlow(ctx)
	outDone, _ := consumerJobs.Enter("outbound")
	go func() {
		defer outDone()
		runOutboundLoopUntil(workCtx, logger, outbound, feishuGateway, 200*time.Millisecond, &claims)
	}()
	if feishuReceiver != nil {
		startRunTask(ctx, "receiver", func() {
			err := feishuReceiver.Start(ctx, func(ctx context.Context, m model.Message) error {
				done, ok := inboundGate.Enter("receiver_callback")
				if !ok {
					return errors.New("hub draining")
				}
				defer done()
				callbackCtx, cancelCallback := callbackWorkContext(ctx, workCtx)
				defer cancelCallback()
				if err := upsertInboundEntity(callbackCtx, st, m); err != nil {
					return err
				}
				return inbound.Enqueue(callbackCtx, m)
			})
			if err != nil && ctx.Err() == nil {
				logger.Error("feishu receiver stopped", "error", err)
			}
		})
	}
	startRunTask(ctx, "reminders", func() { runReminderLoop(ctx, logger, reminders, time.Minute) })
	schedulerCron := startSchedulerCron(ctx, logger, schedulerSvc)
	defer schedulerCron.Stop()

	stop := make(chan os.Signal, 4)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	if err := sdNotify("READY=1"); err != nil {
		return fmt.Errorf("sd_notify READY: %w", err)
	}

	sig := <-stop
	t0 := time.Now()
	storageOwned = false // coordinator owns storage from this point, including failures
	// Keep this armed through return/defer/logging until the actual process exits.
	// Repeated signals remain captured and never reset the single deadline.
	time.AfterFunc(shutdownBudget, func() { os.Exit(2) })
	watchdog.begin(t0)
	results := &serverResults{}
	var queueMu sync.Mutex
	queueCounts := map[string]any{}
	persistenceState := "unknown"
	var durableCounts map[string]int64
	safeUsers := false
	var barrierReady atomic.Bool
	var stoppedCron context.Context
	drainDone := make(chan struct{})
	snapshot := func() any {
		queueMu.Lock()
		q := map[string]any{}
		for k, v := range queueCounts {
			q[k] = v
		}
		state := persistenceState
		durable := durableCounts
		queueMu.Unlock()
		// Read live counters even if the phase deadline beat SealAndFlush's
		// return. Preserve a prior commit failure separately from the timeout.
		for name, queue := range map[string]bus.Queue{"inbound": inbound, "outbound": outbound} {
			if async, ok := queue.(*bus.AsyncDBQueue); ok {
				counts := shutdownQueueCounts(async)
				if previous, ok := q[name].(map[string]any); ok {
					counts["error"] = previous["error"]
				}
				q[name] = counts
			}
		}
		var ingress any = "remote_or_no_sdk"
		if receiver, ok := feishuReceiver.(feishu.ReceiverLifecycle); ok {
			ingress = receiver.IngressSnapshot()
		}
		return map[string]any{"servers": results.snapshot(), "hub": prefixHub.LifecycleCounts(), "hub_ws_close": prefixHub.WSCloseSnapshot(), "http_overlaps_streams": access.group.Snapshot(), "producers": runJobs.Snapshot(), "consumers": consumerJobs.Snapshot(), "feishu_ingress": ingress, "queue": q, "persistence": state, "durable_counts": durable, "watchdog_enabled": watchdog.enabled, "watchdog_window_ms": watchdog.window.Milliseconds()}
	}
	return coordinateShutdown(t0, sig.String(), shutdownPlan{
		Stop: func(stopCtx context.Context) error {
			access.group.Seal()
			if _, managed := feishuReceiver.(feishu.ReceiverLifecycle); !managed {
				inboundGate.Seal()
			}
			claims.Seal()
			runJobs.Seal()
			prefixHub.SealRegistrations()
			cancelRun()
			if stopNotifyEndpoint != nil {
				stopNotifyEndpoint()
			}
			stoppedCron = schedulerCron.Stop()
			// Close both actual listeners concurrently before waiting on registration.
			var listeners sync.WaitGroup
			listeners.Add(2)
			go func() { defer listeners.Done(); _ = publicListener.Close() }()
			go func() { defer listeners.Done(); _ = internalListener.Close() }()
			listeners.Wait()

			err := prefixHub.WaitRegistrations(stopCtx)
			if err == nil {
				barrierReady.Store(true)
			}
			return err
		},
		Drain: func(drainCtx context.Context) error {
			defer close(drainDone)
			var waits sync.WaitGroup
			errs := make(chan error, 7)
			launch := func(fn func() error) { waits.Add(1); go func() { defer waits.Done(); errs <- fn() }() }
			launch(func() error {
				err := srv.Shutdown(drainCtx)
				results.record("public", publicShutdownListener, err)
				return err
			})
			launch(func() error {
				err := internalSrv.Shutdown(drainCtx)
				results.record("internal", internalShutdownListener, err)
				return err
			})
			launch(func() error {
				if !barrierReady.Load() {
					return errors.New("registration barrier incomplete")
				}
				return prefixHub.CloseContext(drainCtx)
			})
			launch(func() error { return runJobs.Wait(drainCtx) })
			launch(func() error { return consumerJobs.Wait(drainCtx) })
			launch(func() error {
				return stopFeishuIngress(drainCtx, feishuReceiver, inboundGate)
			})
			launch(func() error {
				select {
				case <-stoppedCron.Done():
					return nil
				case <-drainCtx.Done():
					return drainCtx.Err()
				}
			})
			waits.Wait()
			close(errs)
			var all []error
			for err := range errs {
				all = append(all, err)
			}
			if err := access.group.Wait(drainCtx); err != nil {
				all = append(all, err)
			}
			joined := errors.Join(all...)
			safeUsers = drainCtx.Err() == nil && barrierReady.Load()
			for _, counts := range []map[string]int{access.group.Snapshot(), prefixHub.LifecycleCounts(), runJobs.Snapshot(), consumerJobs.Snapshot(), inboundGate.Snapshot()} {
				for _, n := range counts {
					if n != 0 {
						safeUsers = false
					}
				}
			}
			return joined
		},
		Flush: func(queueStage context.Context) error {
			if !safeUsers {
				return errors.New("storage users not joined")
			}
			var waits sync.WaitGroup
			var mu sync.Mutex
			var all []error
			for _, entry := range []struct {
				name string
				q    bus.Queue
			}{{"inbound", inbound}, {"outbound", outbound}} {
				entry := entry
				waits.Add(1)
				go func() {
					defer waits.Done()
					if q, ok := entry.q.(*bus.AsyncDBQueue); ok {
						err := q.SealAndFlush(queueStage)
						remaining, failed := q.ShutdownCounts()
						queueMu.Lock()
						queueCounts[entry.name] = map[string]any{"remaining": remaining, "failed": failed, "error": shutdownErrorClass(err)}
						queueMu.Unlock()
						mu.Lock()
						all = append(all, err)
						mu.Unlock()
					}
				}()
			}
			waits.Wait()
			counts, err := st.ShutdownSnapshot(queueStage)
			if err != nil {
				all = append(all, errors.New("durable queue snapshot failed"))
			}
			queueMu.Lock()
			durableCounts = counts
			if err == nil {
				persistenceState = "snapshot_captured"
			}
			queueMu.Unlock()
			return errors.Join(all...)
		},
		Storage: func(context.Context) error {
			if !safeUsers {
				return errors.New("storage users not joined")
			}
			var errs []error
			if closePlatformStore != nil {
				errs = append(errs, closePlatformStore())
			}
			errs = append(errs, st.Close())
			return errors.Join(errs...)
		},
		Finalize: func(finalCtx context.Context) error {
			select {
			case <-drainDone:
				return watchdog.Err()
			case <-finalCtx.Done():
				return errors.Join(watchdog.Err(), finalCtx.Err())
			}
		},
		Force:    func() { cancelWork(); prefixHub.ForceCloseSockets(); _ = srv.Close(); _ = internalSrv.Close() },
		Snapshot: snapshot,
		Emit:     func(r shutdownReport) error { return emitShutdown(logger, r) },
	})

}

func workpulseEffectiveConfig() []config.Observed {
	return []config.Observed{
		config.Observe("WP_DB_PATH", "/data/workpulse.db",
			"同主机起第二个实例时会静默共用同一个库(T-065)"),
		config.Observe("WP_ADDR", ":8080",
			"绑全部网络接口;同主机多实例还会端口冲突"),
		config.Observe("WP_NAME_ENFORCE", "warn",
			"名字门只告警不拒绝 ⇒ 系统会接受一个它自己无法寻址的会话名(T-068)"),
		config.Observe("WP_VERSION_ENFORCE", "warn",
			"版本门只告警不拒绝 ⇒ 不兼容 helper 仍可连接(T-093)"),
		config.ObserveRequired("WP_HUB_ID"),
	}
}

func buildFeishuGateway(ctx context.Context, logger *slog.Logger, repo store.Repository, bs config.Bootstrap) (feishu.Gateway, feishu.Receiver, error) {
	// T17:测试 Hub 出站经 fan-out 代理(代理加[测试环境]后缀+代持凭据);fake/ws 路径零触碰
	if strings.EqualFold(strings.TrimSpace(bs.FeishuTransport), "proxy") {
		url := strings.TrimSpace(bs.FeishuProxyURL)
		if url == "" {
			return nil, nil, errors.New("FEISHU_PROXY_URL is required when FEISHU_TRANSPORT=proxy")
		}
		logger.Info("feishu gateway configured", "transport", "proxy", "url", url)
		return feishu.NewProxyGateway(url, bs.FeishuProxyToken), nil, nil
	}
	if strings.EqualFold(strings.TrimSpace(bs.FeishuTransport), "ws") {
		if err := seedBootstrapBotChannel(ctx, logger, repo, bs); err != nil {
			return nil, nil, err
		}
		channels, err := repo.ListBotChannels(ctx)
		if err != nil {
			return nil, nil, err
		}
		var gateways []*feishu.LarkGateway
		for _, ch := range channels {
			if !ch.Active || !ch.CanReceive {
				continue
			}
			secret := ""
			if ch.ID == bs.FeishuBotChannelID && bs.FeishuAppSecret != "" {
				secret = bs.FeishuAppSecret
			} else if ch.AppSecretEnc != "" {
				secret, err = secretbox.Decrypt(bs.SecretKey, ch.AppSecretEnc)
				if err != nil {
					return nil, nil, err
				}
			}
			if strings.TrimSpace(secret) == "" {
				logger.Warn("skip feishu bot without secret", "bot_channel", ch.ID, "app_id", ch.AppID)
				continue
			}
			gw, err := feishu.NewLarkGateway(ch.ID, ch.AppID, secret, logger)
			if err != nil {
				return nil, nil, err
			}
			gateways = append(gateways, gw)
			logger.Info("feishu gateway configured", "transport", "ws", "bot_channel", ch.ID, "app_id", ch.AppID)
		}
		multi := feishu.NewMultiGateway(gateways...)
		return multi, multi, nil
	}
	logger.Info("feishu gateway configured", "transport", "fake")
	fake := &feishu.Fake{}
	return fake, nil, nil
}

func seedBootstrapBotChannel(ctx context.Context, logger *slog.Logger, repo store.Repository, bs config.Bootstrap) error {
	if bs.FeishuBotChannelID == "" || bs.FeishuAppID == "" {
		return nil
	}
	ch := model.BotChannel{
		ID:         bs.FeishuBotChannelID,
		Name:       "UnifiedRobot",
		AppID:      bs.FeishuAppID,
		Purpose:    "general",
		CanSend:    true,
		CanReceive: true,
		Active:     true,
	}
	existingSecret := false
	if channels, err := repo.ListBotChannels(ctx); err == nil {
		for _, existing := range channels {
			if existing.ID == bs.FeishuBotChannelID && existing.AppSecretEnc != "" {
				existingSecret = true
				break
			}
		}
	}
	if !existingSecret && bs.FeishuAppSecret != "" {
		if bs.SecretKey == "" {
			logger.Warn("bootstrap feishu app secret cannot be stored without WP_SECRET_KEY", "bot_channel", bs.FeishuBotChannelID, "app_id", bs.FeishuAppID)
		} else {
			enc, err := secretbox.Encrypt(bs.SecretKey, bs.FeishuAppSecret)
			if err != nil {
				return err
			}
			ch.AppSecretEnc = enc
		}
	}
	return repo.UpsertBotChannel(ctx, ch)
}

type schedulerCronManager struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	current *cron.Cron
	key     string
}

type schedulerCronEntry struct {
	Kind      string
	ProjectID string
	Spec      string
	TZ        string
}

func startSchedulerCron(ctx context.Context, logger *slog.Logger, svc *scheduler.Service) *schedulerCronManager {
	runCtx, cancel := context.WithCancel(ctx)
	m := &schedulerCronManager{cancel: cancel}
	m.reload(runCtx, logger, svc)
	startRunTask(ctx, "cron_reload", func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				m.reload(runCtx, logger, svc)
			}
		}
	})
	return m
}

func (m *schedulerCronManager) reload(ctx context.Context, logger *slog.Logger, svc *scheduler.Service) {
	entries, err := schedulerCronPlan(ctx, svc)
	if err != nil {
		logger.Warn("scheduler cron plan failed", "error", err)
		return
	}
	key := schedulerCronKey(entries)
	m.mu.Lock()
	if key == m.key {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	c := cron.New(cron.WithLocation(time.UTC))
	for _, entry := range entries {
		entry := entry
		spec := cronSpecWithTZ(entry.Spec, entry.TZ)
		if _, err := c.AddFunc(spec, func() {
			group, _ := ctx.Value(runTasksKey{}).(*lifecycle.Group)
			if group != nil {
				done, ok := group.Enter("cron_job")
				if !ok {
					return
				}
				defer done()
			}
			switch entry.Kind {
			case "group":
				if err := svc.RunGroupNotifications(ctx, "定时群通知"); err != nil {
					logger.Warn("scheduler group notify failed", "error", err)
				}
			case "personal":
				if err := svc.RunPersonalReminders(ctx, "定时个人提醒"); err != nil {
					logger.Warn("scheduler personal reminder failed", "error", err)
				}
			case "project":
				if _, err := svc.RunEvidenceProject(ctx, entry.ProjectID, "定时佐证"); err != nil {
					logger.Warn("scheduler project evidence failed", "project_id", entry.ProjectID, "error", err)
				}
			case "weekly_project":
				if _, err := svc.RunWeeklyProjectReports(ctx, "定时非聚合项目周报"); err != nil {
					logger.Warn("scheduler weekly project reports failed", "error", err)
				}
			case "aggregate_weekly_draft":
				if _, err := svc.RunAggregateWeeklyDrafts(ctx, "定时聚合周报草稿"); err != nil {
					logger.Warn("scheduler aggregate weekly draft failed", "error", err)
				}
			case "aggregate_weekly_publish":
				if _, err := svc.PublishDueAggregateWeeklyReports(ctx, "定时聚合周报发布"); err != nil {
					logger.Warn("scheduler aggregate weekly publish failed", "error", err)
				}
			}
		}); err != nil {
			logger.Warn("scheduler cron entry disabled", "kind", entry.Kind, "project_id", entry.ProjectID, "spec", entry.Spec, "tz", entry.TZ, "error", err)
			return
		}
	}
	m.mu.Lock()
	if ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	c.Start()
	old := m.current
	m.current = c
	m.key = key
	m.mu.Unlock()
	if old != nil {
		old.Stop()
	}
	logger.Info("scheduler cron configured", "entries", len(entries), "key", key)
}

func schedulerCronPlan(ctx context.Context, svc *scheduler.Service) ([]schedulerCronEntry, error) {
	groupSpec, groupTZ := svc.GroupNotifyCronConfig(ctx)
	personalSpec, personalTZ := svc.PersonalNotifyCronConfig(ctx)
	entries := []schedulerCronEntry{
		{Kind: "group", Spec: normalizeCronSpec(groupSpec), TZ: firstNonEmpty(groupTZ, "UTC")},
		{Kind: "personal", Spec: normalizeCronSpec(personalSpec), TZ: firstNonEmpty(personalTZ, "UTC")},
		{Kind: "weekly_project", Spec: "0 2 * * 6", TZ: "UTC"},
		{Kind: "aggregate_weekly_draft", Spec: "0 22 * * 0", TZ: "UTC"},
		{Kind: "aggregate_weekly_publish", Spec: "0 2 * * 1", TZ: "UTC"},
	}
	if svc.Repo == nil {
		return entries, nil
	}
	projects, err := svc.Repo.ListProjects(ctx, true)
	if err != nil {
		return nil, err
	}
	_, defaultTZ := svc.EvidenceCronConfig(ctx)
	for _, project := range projects {
		if strings.TrimSpace(project.EvidenceCron) == "" {
			continue
		}
		entries = append(entries, schedulerCronEntry{
			Kind:      "project",
			ProjectID: project.ID,
			Spec:      normalizeCronSpec(project.EvidenceCron),
			TZ:        firstNonEmpty(project.EvidenceTZ, defaultTZ, "UTC"),
		})
	}
	return entries, nil
}

func schedulerCronKey(entries []schedulerCronEntry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, entry.Kind+":"+entry.ProjectID+":"+entry.TZ+":"+entry.Spec)
	}
	return strings.Join(parts, "|")
}

func normalizeCronSpec(spec string) string {
	fields := strings.Fields(strings.TrimSpace(spec))
	if len(fields) == 2 {
		return fields[0] + " " + fields[1] + " * * *"
	}
	return strings.Join(fields, " ")
}

func cronSpecWithTZ(spec, tz string) string {
	spec = normalizeCronSpec(spec)
	tz = strings.TrimSpace(tz)
	if tz == "" || tz == "UTC" {
		return spec
	}
	return "CRON_TZ=" + tz + " " + spec
}

func (m *schedulerCronManager) Stop() context.Context {
	if m == nil {
		done, cancel := context.WithCancel(context.Background())
		cancel()
		return done
	}
	m.cancel()
	m.mu.Lock()
	current := m.current
	m.current = nil
	m.mu.Unlock()
	if current != nil {
		return current.Stop()
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	return done
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func collectorOrNil(g feishu.Gateway) admin.SeenPersonCollector {
	if c, ok := g.(admin.SeenPersonCollector); ok {
		return c
	}
	return nil
}

func upsertInboundEntity(ctx context.Context, repo store.Repository, m model.Message) error {
	feishuID := feishuIDFromEntity(m)
	if feishuID == "" {
		return errors.New("missing inbound feishu id")
	}
	if err := repo.UpsertChatEntity(ctx, model.ChatEntity{
		ID:           m.ChatEntityID,
		BotChannelID: m.BotChannelID,
		Type:         m.ChatType,
		FeishuID:     feishuID,
		Active:       true,
	}); err != nil {
		return err
	}
	if openID := firstNonEmpty(m.SenderOpenID, personalOpenID(m)); openID != "" {
		_ = repo.UpsertSeenPerson(ctx, model.SeenPerson{
			OpenID:       openID,
			BotChannelID: m.BotChannelID,
			Source:       "inbound",
			LastSeenAt:   time.Now().UTC(),
		})
		if m.ChatType == model.ChatGroup {
			_ = repo.UpsertSeenPersonGroup(ctx, model.SeenPersonGroup{
				OpenID:       openID,
				BotChannelID: m.BotChannelID,
				GroupChatID:  feishuID,
				LastSeenAt:   time.Now().UTC(),
			})
		}
	}
	return nil
}

func runOutboundLoop(ctx context.Context, logger *slog.Logger, outbound bus.Queue, gateway feishu.Gateway, idle time.Duration) {
	runOutboundLoopUntil(ctx, logger, outbound, gateway, idle, nil)
}
func runOutboundLoopUntil(ctx context.Context, logger *slog.Logger, outbound bus.Queue, gateway feishu.Gateway, idle time.Duration, claims *lifecycle.Group) {
	if outbound == nil || gateway == nil {
		return
	}
	if idle <= 0 {
		idle = 200 * time.Millisecond
	}
	for {
		finish := func() {}
		if claims != nil {
			var ok bool
			finish, ok = claims.Enter("outbound_job")
			if !ok {
				return
			}
		}
		process := func() bool {
			msg, err := outbound.Dequeue(ctx)
			if err != nil {
				logger.Warn("outbound dequeue failed", "error", err)
			}
			if msg != nil {
				out := feishu.OutMessage{
					BotChannelID: msg.BotChannelID,
					ToID:         feishuIDFromEntity(*msg),
					ToType:       string(chatTypeFromMessage(*msg)),
					Text:         messageText(msg.Content),
				}
				msgID, err := gateway.Send(ctx, out)
				if err != nil {
					_ = outbound.Fail(ctx, msg.ID, err.Error())
					logger.Warn("outbound send failed", "message_id", msg.ID, "bot_channel", msg.BotChannelID, "error", err)
					return true
				}
				_ = outbound.Ack(ctx, msg.ID)
				logger.Info("outbound sent", "message_id", msg.ID, "feishu_msg_id", msgID, "bot_channel", msg.BotChannelID)
				return true
			}
			return false
		}
		processed := process()
		finish()
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(idle):
		}
	}
}

func feishuIDFromEntity(m model.Message) string {
	prefix := m.BotChannelID + ":" + string(chatTypeFromMessage(m)) + ":"
	if strings.HasPrefix(m.ChatEntityID, prefix) {
		return strings.TrimPrefix(m.ChatEntityID, prefix)
	}
	if m.ChatType == model.ChatGroup {
		prefix = m.BotChannelID + ":group:"
		if strings.HasPrefix(m.ChatEntityID, prefix) {
			return strings.TrimPrefix(m.ChatEntityID, prefix)
		}
	}
	if m.ChatType == model.ChatPersonal || m.ChatType == "" {
		prefix = m.BotChannelID + ":personal:"
		if strings.HasPrefix(m.ChatEntityID, prefix) {
			return strings.TrimPrefix(m.ChatEntityID, prefix)
		}
	}
	return m.ChatEntityID
}

func chatTypeFromMessage(m model.Message) model.ChatType {
	if m.ChatType != "" {
		return m.ChatType
	}
	if strings.Contains(m.ChatEntityID, ":group:") {
		return model.ChatGroup
	}
	return model.ChatPersonal
}

func personalOpenID(m model.Message) string {
	if m.ChatType != model.ChatPersonal {
		return ""
	}
	prefix := m.BotChannelID + ":personal:"
	if strings.HasPrefix(m.ChatEntityID, prefix) {
		return strings.TrimPrefix(m.ChatEntityID, prefix)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func messageText(content string) string {
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &p); err == nil && p.Text != "" {
		return p.Text
	}
	return content
}

type reminderRunner interface {
	RunOnce(context.Context) (int, error)
}

func runReminderLoop(ctx context.Context, logger *slog.Logger, reminders reminderRunner, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	run := func() {
		n, err := reminders.RunOnce(ctx)
		if err != nil {
			logger.Warn("reminder scan failed", "error", err)
			return
		}
		if n > 0 {
			logger.Info("reminders enqueued", "count", n)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func handleFeishuWebhook(ctx context.Context, logger *slog.Logger, repo store.Repository, inbound bus.Queue, channel string, r *http.Request, w http.ResponseWriter) (bool, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return false, err
	}
	bot, err := resolveWebhookBotChannel(ctx, repo, channel)
	if err != nil {
		return false, err
	}
	data, err := decodeWebhookPayload(raw, bot.EncryptKey)
	if err != nil {
		return false, err
	}
	if err := verifyWebhookToken(data, bot.VerificationToken); err != nil {
		return false, err
	}
	// T17 v2.3 §K1/C3(Hub 加性改动③):条件严格分支——payload 含 "v"=代理投递契约,
	// 须 v==1+严格 schema(未知字段拒),漂移=显式 400 非静默;无 "v"=legacy 飞书路原样。
	if err := verifyVersionedDelivery(data); err != nil {
		return false, err
	}
	if bot.VerificationToken == "" && bot.EncryptKey == "" {
		if logger == nil {
			logger = slog.Default()
		}
		key := bot.ID
		if _, loaded := unsignedWebhookWarned.LoadOrStore(key, true); !loaded {
			logger.Warn("feishu webhook accepts unsigned plaintext", "bot_channel", bot.ID)
		}
	}
	if challenge := webhookChallenge(data); challenge != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": challenge})
		return true, nil
	}
	msg, err := feishu.WebhookMessageFromPayload(bot.ID, data)
	if err != nil {
		return false, err
	}
	// Authentication provenance is derived only from checks performed by this
	// ingress adapter. Payload fields can never promote their own trust level.
	msg.IngressProvenance = model.IngressUntrusted
	if strings.TrimSpace(bot.VerificationToken) != "" || strings.TrimSpace(bot.EncryptKey) != "" {
		msg.IngressProvenance = model.IngressWebhookVerified
	}
	if err := upsertInboundEntity(ctx, repo, msg); err != nil {
		return false, err
	}
	return false, inbound.Enqueue(ctx, msg)
}

func resolveWebhookBotChannel(ctx context.Context, repo store.Repository, channel string) (model.BotChannel, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "default"
	}
	channels, err := repo.ListBotChannels(ctx)
	if err != nil {
		return model.BotChannel{}, err
	}
	for _, ch := range channels {
		if strings.EqualFold(ch.ID, channel) || strings.EqualFold(ch.Name, channel) {
			if !ch.Active || !ch.CanReceive {
				return model.BotChannel{}, fmt.Errorf("bot channel %s cannot receive webhook", ch.ID)
			}
			return ch, nil
		}
	}
	return model.BotChannel{ID: channel, Name: channel, CanReceive: true, Active: true}, nil
}

func decodeWebhookPayload(raw []byte, encryptKey string) ([]byte, error) {
	if strings.TrimSpace(encryptKey) == "" {
		return raw, nil
	}
	var envelope struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Encrypt == "" {
		return nil, errors.New("encrypted webhook missing encrypt field")
	}
	return decryptFeishuPayload(envelope.Encrypt, encryptKey)
}

func decryptFeishuPayload(encrypted, encryptKey string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aes.BlockSize*2 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("invalid encrypted webhook length")
	}
	sum := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ciphertext)-aes.BlockSize)
	cipher.NewCBCDecrypter(block, ciphertext[:aes.BlockSize]).CryptBlocks(plain, ciphertext[aes.BlockSize:])
	return pkcs7Unpad(plain, aes.BlockSize)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid webhook padding length")
	}
	n := int(data[len(data)-1])
	if n == 0 || n > blockSize || n > len(data) {
		return nil, errors.New("invalid webhook padding")
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, errors.New("invalid webhook padding bytes")
		}
	}
	return data[:len(data)-n], nil
}

// verifyVersionedDelivery T17 §K1 条件严格:含 "v" 才启用;v!=1 或未知字段=错(→400)。
func verifyVersionedDelivery(data []byte) error {
	var probe struct {
		V *int `json:"v"`
	}
	if json.Unmarshal(data, &probe) != nil || probe.V == nil {
		return nil // 无 v=legacy 飞书路,原样
	}
	if *probe.V != 1 {
		return fmt.Errorf("unsupported delivery version %d", *probe.V)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var strict struct {
		V            int             `json:"v"`
		Token        string          `json:"token"`
		MsgID        string          `json:"msg_id"`
		ChatType     string          `json:"chat_type"`
		ChatID       string          `json:"chat_id"`
		OpenID       string          `json:"open_id"`
		SenderOpenID string          `json:"sender_open_id"`
		Text         string          `json:"text"`
		Raw          json.RawMessage `json:"raw"`
		TS           int64           `json:"ts"`
		Challenge    string          `json:"challenge"` // pre-arm 探测(runbook 阶段1门②)
	}
	if err := dec.Decode(&strict); err != nil {
		return fmt.Errorf("v1 delivery schema violation: %w", err)
	}
	return nil
}

func verifyWebhookToken(data []byte, want string) error {
	want = strings.TrimSpace(want)
	if want == "" {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	got := stringField(root, "token")
	if got == "" {
		if header, ok := root["header"].(map[string]any); ok {
			got = stringField(header, "token")
		}
	}
	if got != want {
		return fmt.Errorf("feishu webhook verification token mismatch")
	}
	return nil
}

func webhookChallenge(data []byte) string {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return ""
	}
	return stringField(root, "challenge")
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
