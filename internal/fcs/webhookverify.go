package fcs

// webhookverify.go — phase-1 extraction (张宗楚 09:31 令: 真抽取). Moves the hub's
// Feishu webhook decrypt (AES-CBC) + token verify from cmd/workpulse/main.go
// (decodeWebhookPayload / decryptFeishuPayload / pkcs7Unpad / verifyWebhookToken)
// into FCS. This is the raw-transport verification underpinning the §5.1 层①
// raw-event provenance: a body that decrypts + a token that verifies →
// provenance=webhook_verified; anything unverified → untrusted, fail-closed.
//
// Behavior preserved byte-for-byte from main.go (same AES-CBC/pkcs7/token logic);
// only the trust-root PROVENANCE labelling is added (untrusted unless a token was
// actually required + matched — an unsigned plaintext webhook is accepted for
// behavior-equivalence but labelled untrusted per §5.1 fail-closed, not verified).

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// KeysFunc resolves (encryptKey, verificationToken) for a bot channel (from
// BotChannelProvider.WebhookKeys). Kept as a func so the verifier is decoupled
// from the concrete provider.
type KeysFunc func(ctx context.Context, botChannelID string) (encryptKey, verificationToken string, err error)

type webhookVerifier struct{ keys KeysFunc }

// NewWebhookVerifier builds an FCS WebhookVerifier over a per-channel key resolver.
func NewWebhookVerifier(keys KeysFunc) WebhookVerifier { return webhookVerifier{keys: keys} }

// VerifyWebhook decrypts + verifies a raw webhook body (design §4/§5.1).
func (v webhookVerifier) VerifyWebhook(ctx context.Context, botChannelID string, raw []byte) ([]byte, string, error) {
	encryptKey, verificationToken, err := v.keys(ctx, botChannelID)
	if err != nil {
		return nil, "untrusted", err
	}
	data, err := decodeWebhookPayload(raw, encryptKey)
	if err != nil {
		return nil, "untrusted", err
	}
	if err := verifyWebhookToken(data, verificationToken); err != nil {
		return nil, "untrusted", err
	}
	// 层① raw-event provenance (§5.1): webhook_verified ONLY when a token was
	// actually required AND matched. Unsigned plaintext (no token) is accepted for
	// behavior-equivalence but stays untrusted (fail-closed labelling, not verified).
	if strings.TrimSpace(verificationToken) != "" {
		return data, "webhook_verified", nil
	}
	return data, "untrusted", nil
}

// WebhookChallenge extracts the Feishu URL-verification challenge, if any (moved
// from main.go webhookChallenge). FCS answers URL-verification at its boundary.
func WebhookChallenge(data []byte) string {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return ""
	}
	if t := stringField(root, "type"); t == "url_verification" {
		return stringField(root, "challenge")
	}
	return ""
}

// ── extracted crypto/verify helpers (byte-equivalent to main.go) ─────────────

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

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

var _ WebhookVerifier = webhookVerifier{}
