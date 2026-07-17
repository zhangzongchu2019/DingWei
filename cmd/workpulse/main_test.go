package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/scheduler"
	"github.com/zhangzongchu2019/dingwei/internal/secretbox"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestHandleFeishuWebhookURLVerification(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/webhook.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "dev", Name: "ExampleBot", AppID: "cli_x", VerificationToken: "vt", CanSend: true, CanReceive: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "/webhook/dev", strings.NewReader(`{"type":"url_verification","token":"vt","challenge":"challenge-code"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	rec := httptest.NewRecorder()
	handled, err := handleFeishuWebhook(ctx, nilLogger(), db, bus.NewDBQueue(db, model.DirectionIn), "dev", req, rec)
	if err != nil || !handled {
		t.Fatalf("handleFeishuWebhook handled=%t err=%v", handled, err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "challenge-code") {
		t.Fatalf("url verification response code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProvisionDownloadServesFilesButNotDirectoryListing(t *testing.T) {
	dl := t.TempDir()
	t.Setenv("WP_PROVISION_DL_DIR", dl)
	if err := os.WriteFile(dl+"/artifact.tar.gz", []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dl/artifact.tar.gz", nil)
	req.SetPathValue("file", "artifact.tar.gz")
	provisionDownload(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "artifact" {
		t.Fatalf("file response code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/dl/", nil)
	req.SetPathValue("file", "")
	provisionDownload(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("directory listing should be denied, code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/dl/../artifact.tar.gz", nil)
	req.SetPathValue("file", "../artifact.tar.gz")
	provisionDownload(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("path traversal should be denied, code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleFeishuWebhookEnqueuesInboundMessage(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/webhook.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	inbound := bus.NewDBQueue(db, model.DirectionIn)
	body := `{"msg_id":"fm1","chat_type":"personal","open_id":"ou_1","sender_open_id":"ou_1","name":"Alice","text":"帮助"}`
	req, err := http.NewRequest(http.MethodPost, "/webhook/dev", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if handled, err := handleFeishuWebhook(ctx, nilLogger(), db, inbound, "dev", req, httptest.NewRecorder()); err != nil || handled {
		t.Fatalf("handleFeishuWebhook handled=%t err=%v", handled, err)
	}
	msg, err := inbound.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if msg == nil || msg.FeishuMsgID != "fm1" || msg.ChatEntityID != "dev:personal:ou_1" || !strings.Contains(msg.Content, "帮助") {
		t.Fatalf("message = %+v", msg)
	}
	seen, err := db.ListSeenPersons(ctx)
	if err != nil || len(seen) != 1 || seen[0].OpenID != "ou_1" || seen[0].Source != "inbound" {
		t.Fatalf("seen persons=%+v err=%v", seen, err)
	}
}

func TestHandleFeishuWebhookEnqueuesCCConnectorInboundMessage(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/webhook.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	inbound := bus.NewDBQueue(db, model.DirectionIn)
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "bot-test", Name: "bot-test", AppID: "cli_testbot000000000", CanSend: true, CanReceive: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	body := `{"schema":"2.0","header":{"event_id":"evt1","event_type":"im.message.receive_v1","create_time":"1783200000000"},"event":{"sender":{"sender_id":{"open_id":"ou_1"}},"message":{"message_id":"cc1","chat_type":"group","chat_id":"oc_ai","create_time":"1783200000000","content":"{\"text\":\"#修改日程 今天推进聚合通知\"}"}}}`
	req, err := http.NewRequest(http.MethodPost, "/webhook/bot-test", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if handled, err := handleFeishuWebhook(ctx, nilLogger(), db, inbound, "bot-test", req, httptest.NewRecorder()); err != nil || handled {
		t.Fatalf("handleFeishuWebhook handled=%t err=%v", handled, err)
	}
	msg, err := inbound.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if msg == nil || msg.BotChannelID != "bot-test" || msg.ChatEntityID != "bot-test:group:oc_ai" || !strings.Contains(msg.Content, "#修改日程") {
		t.Fatalf("cc connector inbound = %+v", msg)
	}
}

func TestHandleFeishuWebhookVerifiesTokenWhenConfigured(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/webhook.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "dev", Name: "ExampleBot", AppID: "cli_x", VerificationToken: "good-token", CanSend: true, CanReceive: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	body := `{"schema":"2.0","header":{"event_id":"evt1","event_type":"im.message.receive_v1","token":"bad-token"},"event":{"sender":{"sender_id":{"open_id":"ou_1"}},"message":{"message_id":"m1","chat_type":"p2p","content":"{\"text\":\"hi\"}"}}}`
	req, err := http.NewRequest(http.MethodPost, "/webhook/dev", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := handleFeishuWebhook(ctx, nilLogger(), db, bus.NewDBQueue(db, model.DirectionIn), "dev", req, httptest.NewRecorder()); err == nil {
		t.Fatalf("handleFeishuWebhook accepted bad token")
	}
	if msg, err := db.ClaimNextMessage(ctx, model.DirectionIn); err != nil || msg != nil {
		t.Fatalf("bad token reached queue: msg=%+v err=%v", msg, err)
	}
	bodyMissing := strings.ReplaceAll(body, `,"token":"bad-token"`, "")
	req, _ = http.NewRequest(http.MethodPost, "/webhook/dev", strings.NewReader(bodyMissing))
	if _, err := handleFeishuWebhook(ctx, nilLogger(), db, bus.NewDBQueue(db, model.DirectionIn), "dev", req, httptest.NewRecorder()); err == nil {
		t.Fatalf("handleFeishuWebhook accepted missing token")
	}
	if msg, err := db.ClaimNextMessage(ctx, model.DirectionIn); err != nil || msg != nil {
		t.Fatalf("missing token reached queue: msg=%+v err=%v", msg, err)
	}
	body = strings.ReplaceAll(body, "bad-token", "good-token")
	req, _ = http.NewRequest(http.MethodPost, "/webhook/dev", strings.NewReader(body))
	if handled, err := handleFeishuWebhook(ctx, nilLogger(), db, bus.NewDBQueue(db, model.DirectionIn), "dev", req, httptest.NewRecorder()); err != nil || handled {
		t.Fatalf("handleFeishuWebhook good token handled=%t err=%v", handled, err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil || msg == nil {
		t.Fatalf("ClaimNextMessage: msg=%+v err=%v", msg, err)
	}
	if msg.IngressProvenance != model.IngressWebhookVerified {
		t.Fatalf("provenance = %q, want webhook verified", msg.IngressProvenance)
	}
}

func TestHandleFeishuWebhookPlaintextWithoutTokenWarnsOnce(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/webhook.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	body := `{"msg_id":"fm1","chat_type":"personal","open_id":"ou_1","sender_open_id":"ou_1","text":"帮助","ingress_provenance":"webhook_verified"}`
	req, err := http.NewRequest(http.MethodPost, "/webhook/plain", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if handled, err := handleFeishuWebhook(ctx, logger, db, bus.NewDBQueue(db, model.DirectionIn), "plain", req, httptest.NewRecorder()); err != nil || handled {
		t.Fatalf("handleFeishuWebhook plaintext handled=%t err=%v", handled, err)
	}
	if !strings.Contains(logs.String(), "unsigned plaintext") {
		t.Fatalf("missing unsigned plaintext warning: %s", logs.String())
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil || msg == nil {
		t.Fatalf("ClaimNextMessage: msg=%+v err=%v", msg, err)
	}
	if msg.IngressProvenance != model.IngressUntrusted {
		t.Fatalf("payload promoted provenance to %q", msg.IngressProvenance)
	}
}

func TestHandleFeishuWebhookRejectsMissingEntity(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/webhook.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "/webhook/dev", strings.NewReader(`{"msg_id":"fm1","chat_type":"group","text":"帮助"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := handleFeishuWebhook(ctx, nilLogger(), db, bus.NewDBQueue(db, model.DirectionIn), "dev", req, httptest.NewRecorder()); err == nil {
		t.Fatalf("handleFeishuWebhook missing entity succeeded")
	}
}

func TestRunOutboundLoopSendsAndAcks(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/outbound.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	if err := outbound.Enqueue(ctx, model.Message{
		ChatEntityID: "dev:personal:ou_1",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"hello"}`,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	fake := &feishu.Fake{}
	go runOutboundLoop(ctx, nilLogger(), outbound, fake, time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sent := fake.SentMessages(); len(sent) == 1 {
			if sent[0].ToID != "ou_1" || sent[0].Text != "hello" || sent[0].ToType != "personal" {
				t.Fatalf("sent = %+v", sent[0])
			}
			next, err := outbound.Dequeue(ctx)
			if err != nil {
				t.Fatalf("Dequeue after ack: %v", err)
			}
			if next != nil {
				t.Fatalf("message not acked: %+v", next)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("outbound message was not sent")
}

func TestBuildFeishuGatewaySeedsEncryptedBootstrapBot(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	gw, receiver, err := buildFeishuGateway(ctx, nilLogger(), db, config.Bootstrap{
		FeishuTransport:    "ws",
		FeishuBotChannelID: "dev",
		FeishuAppID:        "cli_x",
		FeishuAppSecret:    "secret-value",
		SecretKey:          "secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gw == nil || receiver == nil {
		t.Fatalf("gateway=%T receiver=%T", gw, receiver)
	}
	channels, err := db.ListBotChannels(ctx)
	devChannel := findBotChannel(channels, "dev")
	if err != nil || devChannel == nil || !devChannel.AppSecretSet || strings.Contains(devChannel.AppSecretEnc, "secret-value") {
		t.Fatalf("channels=%+v err=%v", channels, err)
	}
}

func TestBuildFeishuGatewayConfiguresMultipleBotChannels(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ccSecret, err := secretbox.Encrypt("secret-key", "cc-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "bot-test", Name: "bot-test", AppID: "cli_testbot000000000", AppSecretEnc: ccSecret, Purpose: "general", CanSend: true, CanReceive: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	gw, receiver, err := buildFeishuGateway(ctx, nilLogger(), db, config.Bootstrap{
		FeishuTransport:    "ws",
		FeishuBotChannelID: "dev",
		FeishuAppID:        "cli_x",
		FeishuAppSecret:    "dev-secret",
		SecretKey:          "secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	multi, ok := gw.(*feishu.MultiGateway)
	if !ok || receiver != multi {
		t.Fatalf("gateway=%T receiver=%T", gw, receiver)
	}
	ids := strings.Join(multi.BotChannelIDs(), ",")
	if !strings.Contains(ids, "dev") || !strings.Contains(ids, "bot-test") {
		t.Fatalf("multi gateway ids=%s", ids)
	}
}

func TestSeedBootstrapBotChannelBackfillsMissingSecretOnly(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "dev", Name: "ExampleBot", AppID: "cli_x", Purpose: "general", CanSend: true, CanReceive: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	bs := config.Bootstrap{FeishuBotChannelID: "dev", FeishuAppID: "cli_x", FeishuAppSecret: "new-secret", SecretKey: "secret-key"}
	if err := seedBootstrapBotChannel(ctx, nilLogger(), db, bs); err != nil {
		t.Fatal(err)
	}
	channels, err := db.ListBotChannels(ctx)
	devChannel := findBotChannel(channels, "dev")
	if err != nil || devChannel == nil || !devChannel.AppSecretSet {
		t.Fatalf("backfill channels=%+v err=%v", channels, err)
	}
	plain, err := secretbox.Decrypt("secret-key", devChannel.AppSecretEnc)
	if err != nil || plain != "new-secret" {
		t.Fatalf("backfilled secret plain=%q err=%v", plain, err)
	}

	original := devChannel.AppSecretEnc
	bs.FeishuAppSecret = "replacement-secret"
	if err := seedBootstrapBotChannel(ctx, nilLogger(), db, bs); err != nil {
		t.Fatal(err)
	}
	channels, err = db.ListBotChannels(ctx)
	devChannel = findBotChannel(channels, "dev")
	if err != nil || devChannel == nil || devChannel.AppSecretEnc != original {
		t.Fatalf("existing secret was overwritten channels=%+v err=%v", channels, err)
	}
}

func TestSeedBootstrapBotChannelWithoutSecretKeyLeavesSecretUnset(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	err = seedBootstrapBotChannel(ctx, nilLogger(), db, config.Bootstrap{
		FeishuBotChannelID: "dev",
		FeishuAppID:        "cli_x",
		FeishuAppSecret:    "env-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	channels, err := db.ListBotChannels(ctx)
	devChannel := findBotChannel(channels, "dev")
	if err != nil || devChannel == nil || devChannel.AppSecretSet {
		t.Fatalf("secret should remain unset without WP_SECRET_KEY channels=%+v err=%v", channels, err)
	}
}

func TestSchedulerCronPlanR16NotificationTiers(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/cron.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:aggregate", Name: "聚合项目", NotifyChatID: "oc_ai", NotifyBotID: "cc", EvidenceCron: "0 2 * * 1,3", EvidenceTZ: "UTC", Active: true}); err != nil {
		t.Fatal(err)
	}
	svc := scheduler.New(scheduler.Config{}, nil, &clock.Fake{T: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)}, nil)
	svc.Repo = db
	entries, err := schedulerCronPlan(ctx, svc)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]schedulerCronEntry{}
	for _, entry := range entries {
		key := entry.Kind
		if entry.ProjectID != "" {
			key += ":" + entry.ProjectID
		}
		got[key] = entry
	}
	if got["group"].Spec != "0 0 * * *" {
		t.Fatalf("group cron spec=%q", got["group"].Spec)
	}
	if got["personal"].Spec != "0 0,6 * * *" {
		t.Fatalf("personal cron spec=%q", got["personal"].Spec)
	}
	if got["weekly_project"].Spec != "0 2 * * 6" || got["weekly_project"].TZ != "UTC" {
		t.Fatalf("weekly project cron=%+v entries=%+v", got["weekly_project"], entries)
	}
	if got["aggregate_weekly_draft"].Spec != "0 22 * * 0" || got["aggregate_weekly_draft"].TZ != "UTC" {
		t.Fatalf("aggregate weekly draft cron=%+v entries=%+v", got["aggregate_weekly_draft"], entries)
	}
	if got["aggregate_weekly_publish"].Spec != "0 2 * * 1" || got["aggregate_weekly_publish"].TZ != "UTC" {
		t.Fatalf("aggregate weekly publish cron=%+v entries=%+v", got["aggregate_weekly_publish"], entries)
	}
	if got["project:proj:aggregate"].Spec != "0 2 * * 1,3" {
		t.Fatalf("project override spec=%q entries=%+v", got["project:proj:aggregate"].Spec, entries)
	}
	if cronSpecWithTZ("0 0", "Asia/Shanghai") != "CRON_TZ=Asia/Shanghai 0 0 * * *" {
		t.Fatalf("cron TZ expansion failed")
	}
}

func nilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func findBotChannel(channels []model.BotChannel, id string) *model.BotChannel {
	for i := range channels {
		if channels[i].ID == id {
			return &channels[i]
		}
	}
	return nil
}
