package main

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/admin"
	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/scheduler"
	"github.com/zhangzongchu2019/dingwei/internal/secretbox"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/version"
)

func TestVersionCommandReportsBuildProvenance(t *testing.T) {
	var out bytes.Buffer
	called := false
	err := execute([]string{"workpulse", "version"}, &out, slog.Default(), func(*slog.Logger) error {
		called = true
		return nil
	})
	if err != nil || called || strings.TrimSpace(out.String()) != version.Version+" "+version.Commit {
		t.Fatalf("out=%q called=%t err=%v", out.String(), called, err)
	}
}

func TestRunWiresAdminFileBrowserConfiguration(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundRun, foundCall := false, false
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "run" {
			continue
		}
		foundRun = true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "configureAdminFileBrowser" {
				foundCall = true
			}
			return true
		})
	}
	if !foundRun || !foundCall {
		t.Fatalf("run wiring missing: found_run=%t configureAdminFileBrowser_call=%t", foundRun, foundCall)
	}
}

func TestConfigureAdminFileBrowserWarnsForEachMissingSetting(t *testing.T) {
	names := []string{"WP_FILE_ISSUER_URL", "WP_FILE_PUBLIC_URL", "WP_FILE_ISSUER_TOKEN", "WP_FILE_AUDIT_TOKEN"}
	values := []struct {
		name                               string
		issuerURL, publicURL, token, audit string
	}{
		{"WP_FILE_ISSUER_URL", "", "https://public", "issuer", "audit"},
		{"WP_FILE_PUBLIC_URL", "https://internal", "", "issuer", "audit"},
		{"WP_FILE_ISSUER_TOKEN", "https://internal", "https://public", "", "audit"},
		{"WP_FILE_AUDIT_TOKEN", "https://internal", "https://public", "issuer", ""},
	}
	for _, tt := range values {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			srv := &admin.Server{}
			configureAdminFileBrowser(srv, logger, tt.issuerURL, tt.publicURL, tt.token, tt.audit)
			logText := logs.String()
			if srv.Files != nil || !strings.Contains(logText, tt.name) {
				t.Fatalf("files=%v logs=%q", srv.Files, logs.String())
			}
			for _, other := range names {
				if other != tt.name && strings.Contains(logText, other) {
					t.Fatalf("missing=%s falsely reported=%s logs=%q", tt.name, other, logText)
				}
			}
		})
	}
}

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
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "dev", Name: "UnifiedRobot", AppID: "cli_x", VerificationToken: "vt", CanSend: true, CanReceive: true, Active: true}); err != nil {
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
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "dev", Name: "UnifiedRobot", AppID: "cli_x", VerificationToken: "good-token", CanSend: true, CanReceive: true, Active: true}); err != nil {
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
	// 「只警告一次」的记忆是包级 sync.Map，-count>1 时跨轮不复位 ⇒ 第 2 轮起抓不到警告、必红（复验方 qwen38maxkhovvob4 实测 19/20）。
	// 用例前后各清一次，每轮从干净状态起；同时把用例补成名副其实：同一 bot 连发两次，警告恰好一次。
	resetUnsignedWebhookWarnings()
	t.Cleanup(resetUnsignedWebhookWarnings)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	for i, id := range []string{"fm1", "fm2"} {
		body := `{"msg_id":"` + id + `","chat_type":"personal","open_id":"ou_1","sender_open_id":"ou_1","text":"帮助","ingress_provenance":"webhook_verified"}`
		req, err := http.NewRequest(http.MethodPost, "/webhook/plain", strings.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		if handled, err := handleFeishuWebhook(ctx, logger, db, bus.NewDBQueue(db, model.DirectionIn), "plain", req, httptest.NewRecorder()); err != nil || handled {
			t.Fatalf("handleFeishuWebhook plaintext #%d handled=%t err=%v", i+1, handled, err)
		}
	}
	if n := strings.Count(logs.String(), "unsigned plaintext"); n != 1 {
		t.Fatalf("unsigned plaintext warning appeared %d times for two requests, want exactly 1: %s", n, logs.String())
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
	if err := db.UpsertBotChannel(ctx, model.BotChannel{ID: "dev", Name: "UnifiedRobot", AppID: "cli_x", Purpose: "general", CanSend: true, CanReceive: true, Active: true}); err != nil {
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

// Nova-260921-A(B1-b):**门 B(/console/admin/*)的装配闸**。
//
// 🔴 这一格是变异 M10 咬出来的缺口,不是设计时想到的:把 run() 里
// `adm.MountConsole(mux)` 那一行注释掉 ⇒
// `go test ./internal/admin/... ./internal/m8/... ./cmd/workpulse/...` **rc=0、零转红**(实测),
// 而线上 `/console/admin/*` **全部 404** ⇒ 整格功能等于没接上,且没有任何告警。
// 原因:运行时闸都在 admin/m8 包里,它们**自己**调 MountConsole 建夹具
// ⇒ 它们能证明"门 B 挂了以后行为对",证明不了"主程序真的挂了"。
//
// 判据三条(形状照本文件既有的 TestRunWiresAdminFileBrowserConfiguration):
//
//	① run() 里必须有**恰好一处** MountConsole 调用 —— 🔴 注释掉不算(AST 里没有注释),
//	   这正是 M10 能悄悄通过的原因,也是本闸必须走 AST 而不是 grep 的原因。
//	② 它必须在 `<同一个接收者>.Prefix = …` 赋值**之后**:
//	   反了 ⇒ s.Prefix == nil ⇒ 门 B 全 503(fail-closed,不漏权限,但功能等于没接上)。
//	③ 它必须挂在**与 `<同一个接收者>.Mount(…)` 同一个 mux** 上:
//	   挂到别的 mux ⇒ 路由注册在一个没人监听的表上,请求打不到。
//
// 🔵 接收者名不硬写(不假设它叫 adm):三条判据都以 MountConsole 那个接收者为锚,
// 变量改名不影响本格。
func TestRunWiresConsoleAdminDoor(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var runBody *ast.BlockStmt
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "run" {
			continue
		}
		runBody = function.Body
	}
	if runBody == nil {
		t.Fatal("main.go 里没有 run() ⇒ 本件的解析对象不存在,结论不采信")
	}

	type recvCall struct {
		recv string
		mux  string
		at   token.Pos
	}
	type recvAssign struct {
		recv string
		at   token.Pos
	}
	var mountConsole, mount []recvCall
	var prefixAssigns []recvAssign
	identName := func(expr ast.Expr) string {
		ident, ok := expr.(*ast.Ident)
		if !ok {
			return ""
		}
		return ident.Name
	}
	ast.Inspect(runBody, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range statement.Lhs {
				selector, ok := lhs.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Prefix" {
					continue
				}
				if recv := identName(selector.X); recv != "" {
					prefixAssigns = append(prefixAssigns, recvAssign{recv: recv, at: statement.Pos()})
				}
			}
		case *ast.CallExpr:
			selector, ok := statement.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv := identName(selector.X)
			if recv == "" {
				return true // portal.NewScheduleServer(st).Mount(mux) 一类:接收者不是标识符,与本格无关
			}
			mux := ""
			if len(statement.Args) == 1 {
				mux = identName(statement.Args[0])
			}
			switch selector.Sel.Name {
			case "MountConsole":
				mountConsole = append(mountConsole, recvCall{recv: recv, mux: mux, at: statement.Pos()})
			case "Mount":
				mount = append(mount, recvCall{recv: recv, mux: mux, at: statement.Pos()})
			}
		}
		return true
	})

	if len(mountConsole) != 1 {
		t.Fatalf("🔴 run() 里有 %d 处 MountConsole 调用,期望恰好 1 处。\n"+
			"0 处 ⇒ /console/admin/* 全部 404:admin 能力搬进 console 这一格**等于没接上**,\n"+
			"      而 admin/m8 两包的所有闸照样全绿(M10 实测 rc=0)—— 本格是唯一的闸。\n"+
			">1 处 ⇒ 同一批路由注册两次,net/http 会 panic 或后注册的那份被忽略。\n"+
			"判据:门 B 必须由 run() 装配一次,且注释掉不算(AST 里没有注释)。", len(mountConsole))
	}
	wired := mountConsole[0]

	var prefixAt token.Pos
	for _, assign := range prefixAssigns {
		if assign.recv == wired.recv {
			prefixAt = assign.at
			break
		}
	}
	if prefixAt == 0 {
		t.Fatalf("run() 里找不到 %s.Prefix = … 的赋值 ⇒ 门 B 的鉴权 hub 没被装配,\n"+
			"requireConsoleCap 会一律 503(关死整道门)。判据:MountConsole 依赖那个 Prefix。", wired.recv)
	}
	if wired.at < prefixAt {
		t.Fatalf("🔴 %s.MountConsole(…) 出现在 %s.Prefix = … **之前**。\n"+
			"后果:装配门 B 时 s.Prefix 还是 nil ⇒ requireConsoleCap 一律 503(fail-closed,不漏权限,\n"+
			"但整道门打不开)。判据:先装配 console 鉴权 hub,再挂门 B。", wired.recv, wired.recv)
	}

	var mountMux string
	for _, call := range mount {
		if call.recv == wired.recv {
			mountMux = call.mux
			break
		}
	}
	if mountMux == "" {
		t.Fatalf("run() 里找不到 %s.Mount(<mux>) ⇒ 无法确定门 A 挂在哪个 mux 上,本格第③条判据无从核起。\n"+
			"(不是「没问题」,是**量不到** ⇒ 结论不采信。)", wired.recv)
	}
	if wired.mux != mountMux {
		t.Fatalf("🔴 门 B 挂在 mux %q 上,门 A 挂在 %q 上 ⇒ 两道门不在同一个路由表。\n"+
			"后果:门 B 的路由注册在一个没人监听的表上,请求打不到(与「没接上」同型)。\n"+
			"判据:MountConsole 必须与 Mount 用同一个 mux。", wired.mux, mountMux)
	}
}

// resetUnsignedWebhookWarnings 清空「每个 bot 只警告一次」的包级记忆（只供测试；用 Range＋Delete，不复制锁）。
func resetUnsignedWebhookWarnings() {
	unsignedWebhookWarned.Range(func(k, _ any) bool {
		unsignedWebhookWarned.Delete(k)
		return true
	})
}
