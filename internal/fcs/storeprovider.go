package fcs

// storeprovider.go — phase-1 extraction (张宗楚 09:31 令). Moves bot credential
// resolution + gateway construction from cmd/workpulse/main.go (buildFeishuGateway
// / seedBootstrapBotChannel's secret resolution) into FCS. FCS now owns the bot
// credentials; the hub no longer resolves Feishu app secrets (design §4/§8).

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/secretbox"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// SeedBootstrap upserts the env-configured bootstrap bot channel into the store
// (byte-equivalent to cmd/workpulse/main.go seedBootstrapBotChannel). Extracted so
// FCS seeds on startup BEFORE listing channels — else a fresh deploy's configured
// channel never enters the store, BuildGateway's ListActive finds nothing, no
// LarkGateway is built, and Feishu WS silently dies (claudek3 CR N1). Old DBs are
// already seeded, which is exactly why tests miss it.
func SeedBootstrap(ctx context.Context, logger *slog.Logger, repo store.Repository, bs config.Bootstrap) error {
	if logger == nil {
		logger = slog.Default()
	}
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

// StoreProvider implements BotChannelProvider backed by the hub's store +
// bootstrap config (same source as today's buildFeishuGateway). Ownership of this
// data moves fully into FCS when the FCS process is stood up; here it reads the
// shared store so phase-1 in-proc extraction is behavior-equivalent.
type StoreProvider struct {
	repo store.Repository
	bs   config.Bootstrap
}

// NewStoreProvider builds the store-backed FCS bot channel provider.
func NewStoreProvider(repo store.Repository, bs config.Bootstrap) *StoreProvider {
	return &StoreProvider{repo: repo, bs: bs}
}

// ListActive returns active bot channels (design §4).
func (p *StoreProvider) ListActive(ctx context.Context) ([]BotChannelConfig, error) {
	channels, err := p.repo.ListBotChannels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]BotChannelConfig, 0, len(channels))
	for _, ch := range channels {
		if !ch.Active {
			continue
		}
		out = append(out, BotChannelConfig{
			ID:         ch.ID,
			Name:       ch.Name,
			AppID:      ch.AppID,
			Purpose:    ch.Purpose,
			CanSend:    ch.CanSend,
			CanReceive: ch.CanReceive,
		})
	}
	return out, nil
}

// AppSecret resolves the decrypted Feishu app secret (bootstrap secret for the
// bootstrap channel, else secretbox.Decrypt with WP_SECRET_KEY). FCS-internal —
// the secret never returns to the hub (design §8).
func (p *StoreProvider) AppSecret(ctx context.Context, botChannelID string) (string, error) {
	channels, err := p.repo.ListBotChannels(ctx)
	if err != nil {
		return "", err
	}
	for _, ch := range channels {
		if ch.ID != botChannelID {
			continue
		}
		if ch.ID == p.bs.FeishuBotChannelID && p.bs.FeishuAppSecret != "" {
			return p.bs.FeishuAppSecret, nil
		}
		if ch.AppSecretEnc != "" {
			return secretbox.Decrypt(p.bs.SecretKey, ch.AppSecretEnc)
		}
		return "", nil // no secret configured
	}
	return "", fmt.Errorf("fcs: bot channel %q not found", botChannelID)
}

// WebhookKeys resolves (encryptKey, verificationToken) for webhook decrypt+verify.
func (p *StoreProvider) WebhookKeys(ctx context.Context, botChannelID string) (string, string, error) {
	channels, err := p.repo.ListBotChannels(ctx)
	if err != nil {
		return "", "", err
	}
	for _, ch := range channels {
		if ch.ID == botChannelID {
			return ch.EncryptKey, ch.VerificationToken, nil
		}
	}
	return "", "", fmt.Errorf("fcs: bot channel %q not found", botChannelID)
}

var _ BotChannelProvider = (*StoreProvider)(nil)

// BuildGateway extracts buildFeishuGateway (cmd/workpulse/main.go) into FCS: it
// constructs the underlying Feishu gateway+receiver from the provider, resolving
// each active receive channel's secret FCS-internally. transport = "ws" | "fake"
// (config.Bootstrap.FeishuTransport). Behavior-equivalent to today's main.go.
func BuildGateway(ctx context.Context, logger *slog.Logger, p BotChannelProvider, transport string) (feishu.Gateway, feishu.Receiver, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if !strings.EqualFold(strings.TrimSpace(transport), "ws") {
		logger.Info("fcs feishu gateway configured", "transport", "fake")
		fake := &feishu.Fake{}
		return fake, nil, nil
	}
	channels, err := p.ListActive(ctx)
	if err != nil {
		return nil, nil, err
	}
	var gateways []*feishu.LarkGateway
	for _, ch := range channels {
		if !ch.CanReceive {
			continue
		}
		secret, err := p.AppSecret(ctx, ch.ID)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(secret) == "" {
			logger.Warn("fcs skip feishu bot without secret", "bot_channel", ch.ID, "app_id", ch.AppID)
			continue
		}
		gw, err := feishu.NewLarkGateway(ch.ID, ch.AppID, secret, logger)
		if err != nil {
			return nil, nil, err
		}
		gateways = append(gateways, gw)
		logger.Info("fcs feishu gateway configured", "transport", "ws", "bot_channel", ch.ID, "app_id", ch.AppID)
	}
	multi := feishu.NewMultiGateway(gateways...)
	return multi, multi, nil
}
