package hosted

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustKey(t *testing.T, b byte) []byte {
	t.Helper()
	k := bytes.Repeat([]byte{b}, masterKeyLen)
	return k
}

func TestWrapUnwrapRoundtrip(t *testing.T) {
	parallelHostedTest(t)
	master := mustKey(t, 0x11)
	dk, err := GenerateDataKey()
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapDataKey(master, dk)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrapped, dk) {
		t.Fatal("包裹件里不得见数据密钥原文")
	}
	got, err := UnwrapDataKey(master, wrapped)
	if err != nil || !EqualConstantTime(got, dk) {
		t.Fatalf("roundtrip 失败: %v", err)
	}
}

func TestUnwrapWrongMasterFails(t *testing.T) {
	parallelHostedTest(t)
	dk, _ := GenerateDataKey()
	wrapped, err := WrapDataKey(mustKey(t, 0x11), dk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapDataKey(mustKey(t, 0x22), wrapped); err == nil {
		t.Fatal("错主密钥解包必须失败（反向）")
	}
}

func TestSealPerRecordNonceAndWrongKey(t *testing.T) {
	parallelHostedTest(t)
	dk := mustKey(t, 0x33)
	c1, _ := SealSecret(dk, []byte("sk-same"))
	c2, _ := SealSecret(dk, []byte("sk-same"))
	if bytes.Equal(c1, c2) {
		t.Fatal("同一明文两次加密必须不同密文（每条独立 nonce）")
	}
	if _, err := OpenSecret(dk, c1); err != nil {
		t.Fatalf("正确密钥应能解: %v", err)
	}
	if _, err := OpenSecret(mustKey(t, 0x44), c1); err == nil {
		t.Fatal("错数据密钥解密必须失败（反向）")
	}
	c1[len(c1)-1] ^= 0xFF
	if _, err := OpenSecret(dk, c1); err == nil {
		t.Fatal("篡改密文解密必须失败（反向）")
	}
}

func TestLoadMasterKeyPermAndForms(t *testing.T) {
	parallelHostedTest(t)
	dir := t.TempDir()
	k := mustKey(t, 0x55)
	// 权限宽于 600 ⇒ 拒
	p1 := filepath.Join(dir, "k1")
	os.WriteFile(p1, []byte(hex.EncodeToString(k)), 0o644)
	if _, err := LoadMasterKey(p1); err == nil {
		t.Fatal("0644 主密钥文件必须被拒")
	}
	// 600＋hex ⇒ 通
	p2 := filepath.Join(dir, "k2")
	os.WriteFile(p2, []byte(hex.EncodeToString(k)+"\n"), 0o600)
	got, err := LoadMasterKey(p2)
	if err != nil || !EqualConstantTime(got, k) {
		t.Fatalf("hex 形态读取失败: %v", err)
	}
	// 内容非法 ⇒ 拒
	p3 := filepath.Join(dir, "k3")
	os.WriteFile(p3, []byte("too-short"), 0o600)
	if _, err := LoadMasterKey(p3); err == nil {
		t.Fatal("非法内容必须被拒")
	}
}

func TestTeamKeyServiceFlow(t *testing.T) {
	parallelHostedTest(t)
	pool := testpool(t)
	ctx := context.Background()
	s := NewStore(pool)
	if err := s.DropSchema(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	master := mustKey(t, 0x66)

	v1, err := s.EnsureTeamKey(ctx, master, "tester")
	if err != nil || v1 != 1 {
		t.Fatalf("Ensure 首把: v=%d err=%v", v1, err)
	}
	v2, err := s.EnsureTeamKey(ctx, master, "tester")
	if err != nil || v2 != v1 {
		t.Fatalf("Ensure 幂等应同版本: v1=%d v2=%d err=%v", v1, v2, err)
	}

	secret := "sk-live-MARKER-不要出现在库里"
	enc, kv, err := s.EncryptAccountSecret(ctx, master, []byte(secret))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := s.CreateAccount(ctx, Account{ID: "acct_k1", Name: "k1", Kind: "api_key", CLI: "codex", LLM: "gpt", KeyVersion: kv}, enc, "tester"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 全库凭据明文扫描命中 0（V6-B7 的库层）
	var hits int
	if err := func() error {
		row, err := s.qrow(ctx, `
SELECT COUNT(*) FROM g4_account_pool
WHERE position($1::bytea in secret_enc) > 0`, []byte("sk-live-MARKER"))
		if err != nil {
			return err
		}
		return row.Scan(&hits)
	}(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hits != 0 {
		t.Fatalf("库中检出凭据明文 %d 处", hits)
	}
	// 错主密钥解密必败（反向）
	if _, err := s.DecryptAccountSecret(ctx, mustKey(t, 0x77), "acct_k1"); err == nil {
		t.Fatal("错主密钥解密必须失败（反向）")
	}
	// 正确链路还原
	plain, err := s.DecryptAccountSecret(ctx, master, "acct_k1")
	if err != nil || string(plain) != secret {
		t.Fatalf("roundtrip: %v", err)
	}
	if strings.Contains(string(enc), "sk-live") {
		t.Fatal("密文里不得见明文前缀")
	}
}
