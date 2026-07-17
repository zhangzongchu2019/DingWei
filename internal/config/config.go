// Package config 处理 bootstrap 配置（环境变量）与运行时配置快照（热更新）。
//
// 真值存 SQLite（经 M9 管理）；这里只负责：
//   - Bootstrap：启动最小集（DB 路径、监听地址、初始管理员口令）走 env。
//   - Snapshot：运行时只读配置快照 + 原子替换（配置热更新见规范 §13.4）。
package config

import (
	"bufio"
	"os"
	"strings"
	"sync/atomic"
)

// Bootstrap 启动最小配置（仅来自环境变量，不入库、不入 git）。
type Bootstrap struct {
	DBPath               string // SQLite 主库路径（落持久卷）
	DataDir              string // 归档库 / 数据目录
	Addr                 string // HTTP 监听（webhook + M9 后台 + healthz）
	AdminUser            string // 首个管理员用户名
	AdminInitPass        string // 首个管理员初始密码（仅首次 seed 用）
	GlobalSchedulePath   string // 全局日程导出文件路径（可选）
	FeishuTransport      string // fake|ws
	FeishuBotChannelID   string // ExampleBot 默认管道 ID
	FeishuAppID          string // 飞书自建应用 app_id
	FeishuAppSecret      string // 飞书自建应用 app_secret（敏感）
	SecretKey            string // 加密后台管道 secret 的 bootstrap 密钥
	SchedulerCLI         string // 系统级调度器 CLI 命令
	SchedulerConfigDir   string // deepseek/claude 配置目录
	ScheduleTeamFile     string // 团队总排期基线文件
	SchedulePersonalDir  string // 个人排期目录
	ScheduleBackupDir    string // 排期版本备份目录
	ScheduleNotifyChat   string // 佐证/调度通知群 chat_id
	ScheduleNotifyBot    string // 发送通知的 bot_channel_id
	ScheduleEvidenceCron string // 定时佐证 cron 表达式
	ScheduleEvidenceTZ   string // 定时佐证 cron 时区
	TranscriptDirs       string // 逗号分隔 transcript 数据源目录
	L2DeepSeekAPIKey     string // P2 L2 triage DeepSeek API key
	L2DeepSeekBaseURL    string // P2 L2 triage DeepSeek OpenAI-compatible base URL
	L2DeepSeekModel      string // P2 L2 triage model
	L2Workers            string // P2 L2 worker concurrency
}

// LoadBootstrap 从环境变量读取 bootstrap。
func LoadBootstrap() Bootstrap {
	loadDotEnv(".env")
	return Bootstrap{
		DBPath:               env("WP_DB_PATH", "/data/workpulse.db"),
		DataDir:              env("WP_DATA_DIR", "/data"),
		Addr:                 env("WP_ADDR", ":8080"),
		AdminUser:            env("WP_ADMIN_USER", "admin"),
		AdminInitPass:        os.Getenv("WP_ADMIN_INIT_PASSWORD"),
		GlobalSchedulePath:   os.Getenv("WP_GLOBAL_SCHEDULE_PATH"),
		FeishuTransport:      env("FEISHU_TRANSPORT", "fake"),
		FeishuBotChannelID:   env("FEISHU_BOT_CHANNEL_ID", "default"),
		FeishuAppID:          os.Getenv("FEISHU_APP_ID"),
		FeishuAppSecret:      os.Getenv("FEISHU_APP_SECRET"),
		SecretKey:            os.Getenv("WP_SECRET_KEY"),
		SchedulerCLI:         os.Getenv("WP_SCHEDULER_CLI"),
		SchedulerConfigDir:   os.Getenv("WP_SCHEDULER_CONFIG_DIR"),
		ScheduleTeamFile:     env("WP_SCHEDULE_TEAM_FILE", "docs/schedule/team.md"),
		SchedulePersonalDir:  env("WP_SCHEDULE_PERSONAL_DIR", "docs/schedule"),
		ScheduleBackupDir:    os.Getenv("WP_SCHEDULE_BACKUP_DIR"),
		ScheduleNotifyChat:   os.Getenv("WP_SCHEDULE_NOTIFY_CHAT"),
		ScheduleNotifyBot:    env("WP_SCHEDULE_NOTIFY_BOT", env("FEISHU_BOT_CHANNEL_ID", "default")),
		ScheduleEvidenceCron: env("WP_SCHEDULE_EVIDENCE_CRON", "0 0,6 * * *"),
		ScheduleEvidenceTZ:   env("WP_SCHEDULE_EVIDENCE_TZ", "UTC"),
		TranscriptDirs:       os.Getenv("WP_TRANSCRIPT_DIRS"),
		L2DeepSeekAPIKey:     os.Getenv("WP_L2_DEEPSEEK_API_KEY"),
		L2DeepSeekBaseURL:    env("WP_L2_DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
		L2DeepSeekModel:      env("WP_L2_DEEPSEEK_MODEL", "deepseek-chat"),
		L2Workers:            env("WP_L2_WORKERS", "4"),
	}
}

// Snapshot 运行时只读配置快照（带版本号；热更新时原子替换，见规范 §13.4）。
type Snapshot struct {
	Version int64
	// 此处后续承载：机器人管道、路由字头、成员、LLM provider、留存策略等
	// （真值来自 SQLite，校验通过后构建快照）。
}

// Holder 原子持有当前快照；in-flight 用旧快照，新请求用新快照。
type Holder struct{ v atomic.Value }

func NewHolder(s *Snapshot) *Holder {
	h := &Holder{}
	h.v.Store(s)
	return h
}

// Current 返回当前快照。
func (h *Holder) Current() *Snapshot { return h.v.Load().(*Snapshot) }

// Swap 原子替换为新快照（调用方应先校验新快照合法，再 Swap；非法不替换）。
func (h *Holder) Swap(s *Snapshot) { h.v.Store(s) }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		val = strings.TrimSpace(val)
		if i := strings.Index(val, "#"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		val = strings.Trim(val, `"'`)
		_ = os.Setenv(key, val)
	}
}
