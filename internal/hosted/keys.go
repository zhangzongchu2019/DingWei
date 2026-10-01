package hosted

// T-G4-02 · 团队密钥加密层（账号池设计件第二节：信封加密）。
//
// 层级：主密钥（Hub 主机 600 权限文件，不进库）
//
//	└ 包裹 → 团队数据密钥（g4_team_key 里只存被包裹后的密文＋版本号）
//	           └ 加密 → 每条账号凭据（g4_account_pool.secret_enc；每条独立随机 nonce）
//
// 判据锚：全库凭据明文扫描命中 0（V6-B7）；错密钥/错版本解密必败（反向）；
// 用户之间隔离靠"一席一账号＋按用户授权"，团队内隔离靠主密钥与库分放。

import (
	"context"
	"database/sql"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	ErrMasterKeyFile   = errors.New("hosted: master key file problem")
	ErrKeyDecrypt      = errors.New("hosted: key unwrap/decrypt failed")
	ErrNoActiveTeamKey = errors.New("hosted: no active team key")
)

const masterKeyLen = 32 // AES-256

// LoadMasterKey 读主密钥文件。硬性要求（G1 部署 checklist 同源）：
// 权限必须 ≤0600；内容接受 32 字节裸值 / 64 位 hex / base64 三种形态（去空白）。
func LoadMasterKey(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMasterKeyFile, err)
	}
	if fi.Mode().Perm() > 0o600 {
		return nil, fmt.Errorf("%w: 权限 %o 宽于 600（主密钥文件必须仅属主可读写）", ErrMasterKeyFile, fi.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMasterKeyFile, err)
	}
	s := strings.TrimSpace(string(raw))
	if b, err := hex.DecodeString(s); err == nil && len(b) == masterKeyLen {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(b) == masterKeyLen {
		return b, nil
	}
	if len([]byte(s)) == masterKeyLen {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("%w: 内容不是 32 字节密钥（裸/hex/base64 三形态之一）", ErrMasterKeyFile)
}

func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != masterKeyLen {
		return nil, fmt.Errorf("key length %d != %d", len(key), masterKeyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// GenerateDataKey 生成一把新的团队数据密钥。
func GenerateDataKey() ([]byte, error) {
	dk := make([]byte, masterKeyLen)
	_, err := io.ReadFull(rand.Reader, dk)
	return dk, err
}

// WrapDataKey 用主密钥包裹团队数据密钥（入库形态）。
func WrapDataKey(master, dataKey []byte) ([]byte, error) {
	return seal(master, dataKey)
}

// UnwrapDataKey 解包团队数据密钥。错主密钥必败（反向判据锚）。
func UnwrapDataKey(master, wrapped []byte) ([]byte, error) {
	return open(master, wrapped)
}

// SealSecret 用团队数据密钥加密一条凭据（每条独立随机 nonce）。
func SealSecret(dataKey, plaintext []byte) ([]byte, error) {
	return seal(dataKey, plaintext)
}

// OpenSecret 解密一条凭据。错数据密钥/篡改必败（反向判据锚）。
func OpenSecret(dataKey, ciphertext []byte) ([]byte, error) {
	return open(dataKey, ciphertext)
}

func seal(key, plaintext []byte) ([]byte, error) {
	a, err := gcm(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, plaintext, nil), nil
}

func open(key, ciphertext []byte) ([]byte, error) {
	a, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < a.NonceSize() {
		return nil, ErrKeyDecrypt
	}
	nonce, body := ciphertext[:a.NonceSize()], ciphertext[a.NonceSize():]
	plain, err := a.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, ErrKeyDecrypt
	}
	return plain, nil
}

// EqualConstantTime 供测试断言两密钥是否相同（不引入时序侧信道到业务路径）。
func EqualConstantTime(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }

// ---------- 与 Store 配合的密钥服务 ----------

// EnsureTeamKey 无 active 团队数据密钥时生成并包裹入库（幂等）；返回 active 版本号。
func (s *Store) EnsureTeamKey(ctx context.Context, master []byte, by string) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM g4_team_key WHERE state='active' ORDER BY version DESC LIMIT 1`).Scan(&v)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	dk, err := GenerateDataKey()
	if err != nil {
		return 0, err
	}
	wrapped, err := WrapDataKey(master, dk)
	if err != nil {
		return 0, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO g4_team_key (version, wrapped, created_by) VALUES (1,$1,$2) ON CONFLICT (version) DO NOTHING`, wrapped, by)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

// ActiveDataKey 取 active 团队数据密钥（解包后仅在内存）。
func (s *Store) ActiveDataKey(ctx context.Context, master []byte) (int, []byte, error) {
	var v int
	var wrapped []byte
	err := s.db.QueryRowContext(ctx, `SELECT version, wrapped FROM g4_team_key WHERE state='active' ORDER BY version DESC LIMIT 1`).Scan(&v, &wrapped)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, ErrNoActiveTeamKey
	}
	if err != nil {
		return 0, nil, err
	}
	dk, err := UnwrapDataKey(master, wrapped)
	if err != nil {
		return 0, nil, err
	}
	return v, dk, nil
}

// EncryptAccountSecret 以 active 团队数据密钥加密一条凭据，返回密文与密钥版本。
func (s *Store) EncryptAccountSecret(ctx context.Context, master, plaintext []byte) ([]byte, int, error) {
	v, dk, err := s.ActiveDataKey(ctx, master)
	if err != nil {
		return nil, 0, err
	}
	enc, err := SealSecret(dk, plaintext)
	if err != nil {
		return nil, 0, err
	}
	return enc, v, nil
}

// DecryptAccountSecret 解密指定账号的凭据（按记录上的 key_version 取对应团队密钥解包）。
// 只在"给绑定席位下发 profile"时调用（最小暴露）；每次调用方须另写审计（T-G4-03）。
func (s *Store) DecryptAccountSecret(ctx context.Context, master []byte, accountID string) ([]byte, error) {
	var enc []byte
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT secret_enc, key_version FROM g4_account_pool WHERE id=$1`, accountID).Scan(&enc, &v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var wrapped []byte
	err = s.db.QueryRowContext(ctx, `SELECT wrapped FROM g4_team_key WHERE version=$1`, v).Scan(&wrapped)
	if err != nil {
		return nil, err
	}
	dk, err := UnwrapDataKey(master, wrapped)
	if err != nil {
		return nil, err
	}
	return OpenSecret(dk, enc)
}
