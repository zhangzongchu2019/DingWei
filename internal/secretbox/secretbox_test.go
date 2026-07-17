package secretbox

import (
	"strings"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	enc, err := Encrypt("test-key", "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "" || strings.Contains(enc, "secret-value") {
		t.Fatalf("ciphertext leaked plaintext: %q", enc)
	}
	plain, err := Decrypt("test-key", enc)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "secret-value" {
		t.Fatalf("plain=%q", plain)
	}
}

func TestEncryptRequiresKey(t *testing.T) {
	if _, err := Encrypt("", "secret"); err == nil {
		t.Fatal("Encrypt accepted empty key")
	}
}
