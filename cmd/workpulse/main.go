// Command workpulse 是 WorkPulse 飞书工作管理机器人 / 消息路由平台 的入口。
//
// 详见 docs/产品规范.md。本文件负责装配：加载 bootstrap → 打开 SQLite → 迁移 →
// seed 管理员 → 启动 HTTP（webhook + M9 后台 + healthz）→ 优雅退出。
package main

import (
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
	"syscall"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/zhangzongchu2019/dingwei/internal/admin"
	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/coordination"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/llm"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/portal"
	"github.com/zhangzongchu2019/dingwei/internal/reminder"
	"github.com/zhangzongchu2019/dingwei/internal/schedule"
	"github.com/zhangzongchu2019/dingwei/internal/scheduler"
	"github.com/zhangzongchu2019/dingwei/internal/secretbox"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/worker"
)

var unsignedWebhookWarned sync.Map

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("workpulse exited", "error", err)
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

	ctx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	if err := st.Migrate(ctx); err != nil {
		return err
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
	inbound := bus.NewBestEffortDBQueue(ctx, st, model.DirectionIn, writeQueueConfig)
	outbound := bus.NewBestEffortDBQueue(ctx, st, model.DirectionOut, writeQueueConfig)
	prefixHub := m8.New(st)
	prefixHub.Outbound = outbound
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
	adm.Mount(mux)                          // /healthz /readyz /admin*
	portal.NewScheduleServer(st).Mount(mux) // /schedule/
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
	feishuGateway, feishuReceiver, err := buildFeishuGateway(ctx, logger, st, bs)
	if err != nil {
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
	mux.HandleFunc("GET /ws/view/{sessionName}", prefixHub.HandleTerminalViewWS)
	mux.HandleFunc("GET /view/{sessionName}", prefixHub.HandleTerminalViewPage)
	mux.Handle("GET /dl/", http.StripPrefix("/dl/", http.FileServer(http.Dir(provisionDownloadDir()))))

	srv := &http.Server{Addr: bs.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		logger.Info("workpulse started", "addr", bs.Addr, "db", bs.DBPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err)
		}
	}()
	go processor.Run(ctx, 200*time.Millisecond)
	prefixHub.StartL2Workers(ctx)
	go runOutboundLoop(ctx, logger, outbound, feishuGateway, 200*time.Millisecond)
	if feishuReceiver != nil {
		go func() {
			err := feishuReceiver.Start(ctx, func(ctx context.Context, m model.Message) error {
				if err := upsertInboundEntity(ctx, st, m); err != nil {
					return err
				}
				return inbound.Enqueue(ctx, m)
			})
			if err != nil && ctx.Err() == nil {
				logger.Error("feishu receiver stopped", "error", err)
			}
		}()
	}
	go runReminderLoop(ctx, logger, reminders, time.Minute)
	schedulerCron := startSchedulerCron(ctx, logger, schedulerSvc)
	defer schedulerCron.Stop()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cancelRun()
	logger.Info("shutting down")
	return srv.Shutdown(shutCtx)
}

func buildFeishuGateway(ctx context.Context, logger *slog.Logger, repo store.Repository, bs config.Bootstrap) (feishu.Gateway, feishu.Receiver, error) {
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
	go func() {
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
	}()
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
	c.Start()
	m.mu.Lock()
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

func provisionDownloadDir() string {
	if v := strings.TrimSpace(os.Getenv("WP_PROVISION_DL_DIR")); v != "" {
		return v
	}
	return "data/dl"
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
	if outbound == nil || gateway == nil {
		return
	}
	if idle <= 0 {
		idle = 200 * time.Millisecond
	}
	for {
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
				continue
			}
			_ = outbound.Ack(ctx, msg.ID)
			logger.Info("outbound sent", "message_id", msg.ID, "feishu_msg_id", msgID, "bot_channel", msg.BotChannelID)
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
