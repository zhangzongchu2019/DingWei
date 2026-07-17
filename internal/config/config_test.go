package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBootstrap_Defaults(t *testing.T) {
	// 清空相关环境变量，确保取默认值
	for _, k := range []string{"WP_DB_PATH", "WP_DATA_DIR", "WP_ADDR", "WP_ADMIN_USER", "WP_ADMIN_INIT_PASSWORD", "WP_GLOBAL_SCHEDULE_PATH", "WP_SECRET_KEY", "WP_SCHEDULE_EVIDENCE_CRON", "WP_SCHEDULE_EVIDENCE_TZ", "FEISHU_TRANSPORT", "FEISHU_BOT_CHANNEL_ID", "FEISHU_APP_ID", "FEISHU_APP_SECRET"} {
		os.Unsetenv(k)
	}
	b := LoadBootstrap()
	if b.DBPath != "/data/workpulse.db" {
		t.Fatalf("DBPath=%q, want /data/workpulse.db", b.DBPath)
	}
	if b.Addr != ":8080" {
		t.Fatalf("Addr=%q, want :8080", b.Addr)
	}
	if b.AdminUser != "admin" {
		t.Fatalf("AdminUser=%q, want admin", b.AdminUser)
	}
	if b.AdminInitPass != "" {
		t.Fatalf("AdminInitPass=%q, want empty", b.AdminInitPass)
	}
	if b.GlobalSchedulePath != "" {
		t.Fatalf("GlobalSchedulePath=%q, want empty", b.GlobalSchedulePath)
	}
	if b.FeishuTransport != "fake" || b.FeishuBotChannelID != "default" {
		t.Fatalf("Feishu defaults = transport:%q channel:%q", b.FeishuTransport, b.FeishuBotChannelID)
	}
	if b.ScheduleEvidenceCron != "0 0,6 * * *" || b.ScheduleEvidenceTZ != "UTC" {
		t.Fatalf("evidence schedule defaults = cron:%q tz:%q", b.ScheduleEvidenceCron, b.ScheduleEvidenceTZ)
	}
}

func TestLoadBootstrap_OverrideFromEnv(t *testing.T) {
	os.Setenv("WP_DB_PATH", "/tmp/test.db")
	os.Setenv("WP_ADDR", ":9090")
	os.Setenv("WP_ADMIN_INIT_PASSWORD", "secret123")
	os.Setenv("WP_SECRET_KEY", "secret-key")
	os.Setenv("WP_SCHEDULE_EVIDENCE_CRON", "5 18 * * *")
	os.Setenv("WP_SCHEDULE_EVIDENCE_TZ", "UTC")
	defer func() {
		os.Unsetenv("WP_DB_PATH")
		os.Unsetenv("WP_ADDR")
		os.Unsetenv("WP_ADMIN_INIT_PASSWORD")
		os.Unsetenv("WP_SECRET_KEY")
		os.Unsetenv("WP_SCHEDULE_EVIDENCE_CRON")
		os.Unsetenv("WP_SCHEDULE_EVIDENCE_TZ")
	}()
	b := LoadBootstrap()
	if b.DBPath != "/tmp/test.db" {
		t.Fatalf("DBPath=%q, want /tmp/test.db", b.DBPath)
	}
	if b.Addr != ":9090" {
		t.Fatalf("Addr=%q, want :9090", b.Addr)
	}
	if b.AdminInitPass != "secret123" {
		t.Fatalf("AdminInitPass=%q, want secret123", b.AdminInitPass)
	}
	if b.SecretKey != "secret-key" {
		t.Fatalf("SecretKey=%q, want secret-key", b.SecretKey)
	}
	if b.ScheduleEvidenceCron != "5 18 * * *" || b.ScheduleEvidenceTZ != "UTC" {
		t.Fatalf("evidence schedule env = cron:%q tz:%q", b.ScheduleEvidenceCron, b.ScheduleEvidenceTZ)
	}
}

func TestLoadBootstrap_LoadsDotEnvWithoutOverridingEnv(t *testing.T) {
	for _, k := range []string{"FEISHU_TRANSPORT", "FEISHU_BOT_CHANNEL_ID", "FEISHU_APP_ID", "FEISHU_APP_SECRET"} {
		os.Unsetenv(k)
	}
	t.Setenv("FEISHU_APP_ID", "from_env")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FEISHU_TRANSPORT=ws # long connection\nFEISHU_BOT_CHANNEL_ID=dev\nFEISHU_APP_ID=from_file\nFEISHU_APP_SECRET='secret'\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	b := LoadBootstrap()
	if b.FeishuTransport != "ws" || b.FeishuBotChannelID != "dev" || b.FeishuAppSecret != "secret" {
		t.Fatalf("bootstrap feishu = %+v", b)
	}
	if b.FeishuAppID != "from_env" {
		t.Fatalf("env was overridden by .env: %q", b.FeishuAppID)
	}
}

func TestHolder_CurrentAndSwap(t *testing.T) {
	s1 := &Snapshot{Version: 1}
	s2 := &Snapshot{Version: 2}
	h := NewHolder(s1)
	if h.Current().Version != 1 {
		t.Fatalf("current version=%d want 1", h.Current().Version)
	}
	h.Swap(s2)
	if h.Current().Version != 2 {
		t.Fatalf("after swap version=%d want 2", h.Current().Version)
	}
}
