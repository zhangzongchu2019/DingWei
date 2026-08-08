package fcs

// storeprovider_test.go — migrated coverage for the extracted SeedBootstrap +
// BuildGateway (was cmd/workpulse TestBuildFeishuGateway* before the phase-1
// extraction moved that logic into FCS).

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/secretbox"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func findDevChannel(channels []model.BotChannel, id string) *model.BotChannel {
	for i := range channels {
		if channels[i].ID == id {
			return &channels[i]
		}
	}
	return nil
}

// TestSeedBootstrapBackfillsMissingSecretOnly — migrated from cmd/workpulse (was
// TestSeedBootstrapBotChannelBackfillsMissingSecretOnly before the extraction).
func TestSeedBootstrapBackfillsMissingSecretOnly(t *testing.T) {
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
	if err := SeedBootstrap(ctx, nil, db, bs); err != nil {
		t.Fatal(err)
	}
	channels, err := db.ListBotChannels(ctx)
	dev := findDevChannel(channels, "dev")
	if err != nil || dev == nil || !dev.AppSecretSet {
		t.Fatalf("backfill channels=%+v err=%v", channels, err)
	}
	if plain, err := secretbox.Decrypt("secret-key", dev.AppSecretEnc); err != nil || plain != "new-secret" {
		t.Fatalf("backfilled secret plain=%q err=%v", plain, err)
	}
	original := dev.AppSecretEnc
	bs.FeishuAppSecret = "replacement-secret"
	if err := SeedBootstrap(ctx, nil, db, bs); err != nil {
		t.Fatal(err)
	}
	channels, _ = db.ListBotChannels(ctx)
	if dev = findDevChannel(channels, "dev"); dev == nil || dev.AppSecretEnc != original {
		t.Fatalf("existing secret was overwritten: %+v", dev)
	}
}

// TestSeedBootstrapWithoutSecretKeyLeavesSecretUnset — migrated from cmd/workpulse.
func TestSeedBootstrapWithoutSecretKeyLeavesSecretUnset(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := SeedBootstrap(ctx, nil, db, config.Bootstrap{FeishuBotChannelID: "dev", FeishuAppID: "cli_x", FeishuAppSecret: "env-secret"}); err != nil {
		t.Fatal(err)
	}
	channels, err := db.ListBotChannels(ctx)
	dev := findDevChannel(channels, "dev")
	if err != nil || dev == nil || dev.AppSecretSet {
		t.Fatalf("secret should remain unset without WP_SECRET_KEY: %+v err=%v", dev, err)
	}
}

func TestNewSeedsEncryptedBootstrapBot(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	a, err := New(ctx, nil, db, config.Bootstrap{
		FeishuTransport:    "ws",
		FeishuBotChannelID: "dev",
		FeishuAppID:        "cli_x",
		FeishuAppSecret:    "secret-value",
		SecretKey:          "secret-key",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Service == nil {
		t.Fatal("assembly has no service")
	}
	// N1: fresh DB must have the env-configured bootstrap channel seeded with an
	// ENCRYPTED secret (not plaintext).
	channels, err := db.ListBotChannels(ctx)
	if err != nil {
		t.Fatalf("ListBotChannels: %v", err)
	}
	var dev *model.BotChannel
	for i := range channels {
		if channels[i].ID == "dev" {
			dev = &channels[i]
		}
	}
	if dev == nil {
		t.Fatalf("bootstrap channel 'dev' not seeded; channels=%+v", channels)
	}
	if !dev.AppSecretSet || strings.Contains(dev.AppSecretEnc, "secret-value") {
		t.Fatalf("bootstrap secret not encrypted: %+v", dev)
	}
}

func TestNewFakeTransportHasNoReceiver(t *testing.T) {
	db, err := store.OpenSQLite(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// fake transport: a Service is still assembled (Send works), and Run is a
	// no-op that blocks (nil receiver) — must not panic (claudek3 CR N3).
	a, err := New(ctx, nil, db, config.Bootstrap{FeishuTransport: "fake"})
	if err != nil {
		t.Fatalf("New(fake): %v", err)
	}
	if a.Service == nil {
		t.Fatal("fake assembly has no service")
	}
	ctx2, cancel := context.WithCancel(ctx)
	cancel() // Run should return promptly on a cancelled ctx (no receiver, no panic)
	if err := a.Run(ctx2); err == nil {
		t.Fatal("Run(fake) should return ctx error on cancel")
	}
}
