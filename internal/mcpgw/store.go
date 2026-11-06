package mcpgw

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"io"
	"time"
)

var errRecord = errors.New("mcpgw: missing, expired or consumed record")

// Store 保存短期 OAuth 事务与一次性码。key 是随机值的 SHA256；Take 必须原子删除。
// payload 含回调交来的本地令牌，持久实现 MUST 加密，不能记录日志。
type Store interface {
	Put(context.Context, string, string, []byte, time.Time, time.Time) error
	Get(context.Context, string, string, time.Time) ([]byte, error)
	Take(context.Context, string, string, time.Time) ([]byte, error)
}

// SQLStore 使用由装配提供的已迁移的 PG 数据库与 256 位持久密钥。
// 不依赖进程内 map；多个连接与重启共享原子消费事实。密钥不得进入仓库。
type SQLStore struct {
	resource string
	db       *repo.Pool
	access   repo.AccessCtx
	aead     cipher.AEAD
}

func NewSQLStore(ctx context.Context, db *repo.Pool, access repo.AccessCtx, resource string, key []byte) (*SQLStore, error) {
	if db == nil || resource == "" || len(key) != 32 {
		return nil, errors.New("mcpgw: store needs database and 32-byte key")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	rows, e := db.Query(ctx, access, `SELECT resource,kind,key_hash,sealed,expires_ns FROM mcpgw_oauth WHERE 1=0`)
	if e != nil {
		return nil, errors.New("mcpgw: OAuth migration missing")
	}
	_ = rows.Close()
	return &SQLStore{db: db, access: access, resource: resource, aead: a}, nil
}
func hash(v string) string { s := sha256.Sum256([]byte(v)); return hex.EncodeToString(s[:]) }
func (s *SQLStore) Put(ctx context.Context, kind, key string, payload []byte, expires, now time.Time) error {
	if !expires.After(now) {
		return errRecord
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e := io.ReadFull(rand.Reader, nonce); e != nil {
		return e
	}
	sealed := s.aead.Seal(nonce, nonce, payload, []byte(s.resource+"/"+kind+"/"+key))
	return s.db.InTx(ctx, s.access, nil, func(tx repo.Tx) error {
		if _, e := tx.Exec(ctx, `DELETE FROM mcpgw_oauth WHERE resource=$1 AND expires_ns <= $2`, s.resource, now.UnixNano()); e != nil {
			return e
		}
		var count int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM mcpgw_oauth WHERE resource=$1`, s.resource).Scan(&count); e != nil {
			return e
		}
		if count >= 10000 {
			return errors.New("mcpgw: pending OAuth limit")
		}
		_, e := tx.Exec(ctx, `INSERT INTO mcpgw_oauth(resource,kind,key_hash,sealed,expires_ns) VALUES($1,$2,$3,$4,$5)`, s.resource, kind, key, sealed, expires.UnixNano())
		return e
	})
}
func (s *SQLStore) read(ctx context.Context, kind, key string, now time.Time, take bool) ([]byte, error) {
	query := `SELECT sealed,expires_ns FROM mcpgw_oauth WHERE resource=$1 AND kind=$2 AND key_hash=$3`
	if take {
		query = `DELETE FROM mcpgw_oauth WHERE resource=$1 AND kind=$2 AND key_hash=$3 RETURNING sealed,expires_ns`
	}
	var sealed []byte
	var expiry int64
	row, e := s.db.QueryRow(ctx, s.access, query, s.resource, kind, key)
	if e != nil {
		return nil, e
	}
	if e := row.Scan(&sealed, &expiry); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, errRecord
		}
		return nil, e
	}
	if now.UnixNano() >= expiry || len(sealed) < s.aead.NonceSize() {
		return nil, errRecord
	}
	b, e := s.aead.Open(nil, sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():], []byte(s.resource+"/"+kind+"/"+key))
	if e != nil {
		return nil, errRecord
	}
	return b, nil
}
func (s *SQLStore) Get(ctx context.Context, kind, key string, now time.Time) ([]byte, error) {
	return s.read(ctx, kind, key, now, false)
}
func (s *SQLStore) Take(ctx context.Context, kind, key string, now time.Time) ([]byte, error) {
	return s.read(ctx, kind, key, now, true)
}
