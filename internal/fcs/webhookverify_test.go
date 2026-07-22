package fcs

// webhookverify_test.go — validates the extracted webhook trust-root labelling
// (design §5.1): verified→webhook_verified, unsigned/mismatch→untrusted fail-closed.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// TestVerifyEncryptedRoundtrip encrypts a payload the way Feishu does (AES-CBC +
// pkcs7, key=sha256(encryptKey)) and asserts VerifyWebhook decrypts it back —
// turning "byte-equivalent to main.go" from a claim into an assertion (claudek3
// CR suggestion). A verified token also yields provenance=webhook_verified.
func TestVerifyEncryptedRoundtrip(t *testing.T) {
	const encryptKey = "test-encrypt-key-123"
	plaintext := []byte(`{"type":"event_callback","token":"tok","event":{"x":1}}`)
	body := []byte(`{"encrypt":"` + encryptFeishuTestPayload(t, plaintext, encryptKey) + `"}`)

	v := NewWebhookVerifier(keysFunc(encryptKey, "tok"))
	data, prov, err := v.VerifyWebhook(context.Background(), "bot-a", body)
	if err != nil {
		t.Fatalf("roundtrip decrypt+verify: %v", err)
	}
	if !bytes.Equal(data, plaintext) {
		t.Fatalf("decrypted %s != original %s", data, plaintext)
	}
	if prov != "webhook_verified" {
		t.Fatalf("prov=%q, want webhook_verified", prov)
	}
}

func encryptFeishuTestPayload(t *testing.T, plaintext []byte, encryptKey string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	n := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append(append([]byte{}, plaintext...), bytes.Repeat([]byte{byte(n)}, n)...)
	ciphertext := make([]byte, aes.BlockSize+len(padded)) // iv (zero) + ciphertext
	cipher.NewCBCEncrypter(block, ciphertext[:aes.BlockSize]).CryptBlocks(ciphertext[aes.BlockSize:], padded)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func keysFunc(encryptKey, verificationToken string) KeysFunc {
	return func(context.Context, string) (string, string, error) {
		return encryptKey, verificationToken, nil
	}
}

func TestVerifyUnsignedPlaintextIsUntrusted(t *testing.T) {
	// No encrypt key, no verification token: plaintext accepted (behavior preserved)
	// but provenance MUST be untrusted (§5.1 fail-closed — not "verified").
	v := NewWebhookVerifier(keysFunc("", ""))
	body := []byte(`{"type":"event","header":{}}`)
	data, prov, err := v.VerifyWebhook(context.Background(), "bot-a", body)
	if err != nil {
		t.Fatalf("unsigned should pass (behavior-equiv): %v", err)
	}
	if prov != "untrusted" {
		t.Fatalf("unsigned provenance = %q, want untrusted (not verified)", prov)
	}
	if string(data) != string(body) {
		t.Fatalf("unsigned data mangled: %s", data)
	}
}

func TestVerifyTokenMatchIsWebhookVerified(t *testing.T) {
	v := NewWebhookVerifier(keysFunc("", "secret-token"))
	body := []byte(`{"token":"secret-token","type":"event"}`)
	_, prov, err := v.VerifyWebhook(context.Background(), "bot-a", body)
	if err != nil {
		t.Fatalf("matching token should verify: %v", err)
	}
	if prov != "webhook_verified" {
		t.Fatalf("verified provenance = %q, want webhook_verified", prov)
	}
}

func TestVerifyTokenMismatchFailsClosed(t *testing.T) {
	v := NewWebhookVerifier(keysFunc("", "expected-token"))
	body := []byte(`{"token":"WRONG","type":"event"}`)
	_, prov, err := v.VerifyWebhook(context.Background(), "bot-a", body)
	if err == nil {
		t.Fatal("token mismatch must fail")
	}
	if prov != "untrusted" {
		t.Fatalf("mismatch provenance = %q, want untrusted (fail-closed)", prov)
	}
}

func TestVerifyTokenInHeader(t *testing.T) {
	// Feishu v2 carries the token under header.token.
	v := NewWebhookVerifier(keysFunc("", "hdr-token"))
	body := []byte(`{"header":{"token":"hdr-token"},"type":"event"}`)
	_, prov, err := v.VerifyWebhook(context.Background(), "bot-a", body)
	if err != nil || prov != "webhook_verified" {
		t.Fatalf("header token: prov=%q err=%v, want webhook_verified/nil", prov, err)
	}
}

func TestWebhookChallenge(t *testing.T) {
	if c := WebhookChallenge([]byte(`{"type":"url_verification","challenge":"abc123"}`)); c != "abc123" {
		t.Fatalf("challenge = %q, want abc123", c)
	}
	if c := WebhookChallenge([]byte(`{"type":"event","challenge":"nope"}`)); c != "" {
		t.Fatalf("non-verification challenge = %q, want empty", c)
	}
}
