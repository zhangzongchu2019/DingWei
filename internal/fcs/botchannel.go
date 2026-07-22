package fcs

// botchannel.go — FCS bot_channel management seam (design §4/§8). FCS owns bot
// credentials (AppSecretEnc / VerificationToken / EncryptKey + WP_SECRET_KEY) and
// the webhook decrypt/verify; the HUB no longer holds Feishu bot secrets (design
// §8, reduces hub attack surface).
//
// This file defines the FCS-side seam only. The actual extraction of
// buildFeishuGateway / seedBootstrapBotChannel / decodeWebhookPayload /
// verifyWebhookToken from cmd/workpulse/main.go into FCS lands when the FCS
// process is stood up (extraction phase 1). Secrets are resolved via the provider
// methods and NEVER cross the hub↔FCS wire (design §4).

import "context"

// BotChannelConfig is the NON-secret bot channel descriptor FCS needs to run a
// channel (WS receive + outbound send). Mirrors the non-secret fields of
// internal/model.BotChannel. Secrets (app secret / encrypt key / verification
// token) are resolved via BotChannelProvider methods below — never carried in
// this struct nor across the hub↔FCS wire (design §4/§8).
type BotChannelConfig struct {
	ID         string
	Name       string
	AppID      string
	Purpose    string // dm | group | general
	CanSend    bool
	CanReceive bool
}

// BotChannelProvider is FCS's source of bot channel config + credentials. Backed
// today by the hub's model.BotChannel store (ListBotChannels + secretbox decrypt
// with WP_SECRET_KEY); ownership moves into FCS at extraction (design §4).
//
// Credential methods are FCS-INTERNAL: their results (app secret, webhook keys)
// are used inside FCS to talk to Feishu / verify webhooks and MUST NOT be returned
// to the hub or placed on the hub↔FCS wire (design §8 hub 零暂存 / 不持飞书 secret).
type BotChannelProvider interface {
	// ListActive returns the active channels FCS should serve (active + can send/receive).
	ListActive(ctx context.Context) ([]BotChannelConfig, error)

	// AppSecret resolves the decrypted Feishu app secret for a channel (for WS/API).
	AppSecret(ctx context.Context, botChannelID string) (string, error)

	// WebhookKeys resolves (encryptKey, verificationToken) for webhook decrypt+verify.
	// Empty strings mean the channel accepts unsigned plaintext (a warn-worthy config,
	// preserved from current behavior main.go:716) — the caller decides fail-closed policy.
	WebhookKeys(ctx context.Context, botChannelID string) (encryptKey, verificationToken string, err error)
}

// WebhookVerifier is the FCS inbound webhook trust step (moves from hub main.go:
// decodeWebhookPayload + verifyWebhookToken into FCS). Given a raw webhook body and
// the channel keys, it returns the decrypted+verified plaintext or fails closed.
//
// This is the raw-transport verification underpinning the §5.1 层① raw-event
// provenance (webhook_verified): a body that decrypts + verifies here is eligible
// for provenance=webhook_verified; a failure → untrusted, fail-closed. The final
// canonical source stamp (§5) still awaits B0's source object.
type WebhookVerifier interface {
	// VerifyWebhook decrypts (AES-CBC via encryptKey) + verifies (token) a raw body.
	// On success returns the plaintext and provenance="webhook_verified"; on failure
	// returns ("", "untrusted", err) — fail-closed (design §5.1).
	VerifyWebhook(ctx context.Context, botChannelID string, raw []byte) (plaintext []byte, provenance string, err error)
}
