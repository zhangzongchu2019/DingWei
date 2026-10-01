package hosted

// T-G4-11 · Hub 节点登记与凭据签发（节点代理设计件第二节）。
//
// 凭据纪律：完整凭据只在签发/轮换的应答里出现一次（一次性显示）；
// 库里只存 SHA-256 哈希；轮换即旧凭据失效；撤销后一切认证必拒。
// mTLS 双向认证中"Hub 验节点"的一层由 AuthenticateNode 承载（T-G4-10 握手时调）。

import (
	"context"
	"database/sql"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrNodeAuth   = errors.New("hosted: node authentication failed")
	ErrNodeRevoked = errors.New("hosted: node credential revoked")
)

// issueNodeSecret 生成节点凭据：32 字节随机，展示形态 base64（仅此一次给出）。
func issueNodeSecret() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", nil, err
	}
	return base64.RawURLEncoding.EncodeToString(raw), raw, nil
}

func hashNodeSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// RegisterNode 登记节点（标签随 Node.Labels），签发凭据一次性返回。
// 同名节点重复登记 ⇒ 轮换语义（UpsertNode 已覆盖）；新凭据顶替旧凭据。
func (s *Store) RegisterNode(ctx context.Context, n Node, by string) (string, error) {
	if n.ID == "" || n.Name == "" {
		return "", fmt.Errorf("%w: node id/name 必填", ErrInvalidState)
	}
	secret, _, err := issueNodeSecret()
	if err != nil {
		return "", err
	}
	err = s.pool.InTx(ctx, actx, nil, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx, `
INSERT INTO g4_node (id, name, labels, state, docker_version, node_protocol_version)
VALUES ($1,$2,$3,'offline',$4,$5)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, labels=EXCLUDED.labels,
  docker_version=EXCLUDED.docker_version, node_protocol_version=EXCLUDED.node_protocol_version,
  updated_at=now()`, n.ID, n.Name, n.Labels, n.DockerVersion, n.ProtocolVersion); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO g4_node_credential (node_id, secret_hash, issued_by) VALUES ($1,$2,$3)
ON CONFLICT (node_id) DO UPDATE SET secret_hash=EXCLUDED.secret_hash,
  issued_by=EXCLUDED.issued_by, issued_at=now(), rotated_at=now(), revoked_at=NULL`,
			n.ID, hashNodeSecret(secret), by); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
INSERT INTO g4_audit (actor, action, target, detail) VALUES ($1,'node.register',$2,NULL)`, by, n.ID)
		return err
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

// RotateNodeCredential 轮换：新凭据一次性返回，旧凭据立即失效。
func (s *Store) RotateNodeCredential(ctx context.Context, nodeID, by string) (string, error) {
	secret, _, err := issueNodeSecret()
	if err != nil {
		return "", err
	}
	res, err := s.pool.Exec(ctx, actx, `
UPDATE g4_node_credential SET secret_hash=$2, issued_by=$3, issued_at=now(), rotated_at=now(), revoked_at=NULL
WHERE node_id=$1`, nodeID, hashNodeSecret(secret), by)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrNotFound
	}
	if err := s.AddAudit(ctx, by, "node.credential_rotate", nodeID, nil); err != nil {
		return "", err
	}
	return secret, nil
}

// RevokeNodeCredential 撤销：此后一切认证必拒（ErrNodeRevoked 优先于口令比对）。
func (s *Store) RevokeNodeCredential(ctx context.Context, nodeID, by string) error {
	res, err := s.pool.Exec(ctx, actx, `
UPDATE g4_node_credential SET revoked_at=$2 WHERE node_id=$1`, nodeID, time.Now().UTC())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return s.AddAudit(ctx, by, "node.credential_revoke", nodeID, nil)
}

// AuthenticateNode 节点代理握手认证：撤销态优先拒；哈希常量时间比对。
func (s *Store) AuthenticateNode(ctx context.Context, nodeID, secret string) error {
	var hash string
	var revokedAt *time.Time
	err := s.qrow(ctx, `
SELECT secret_hash, revoked_at FROM g4_node_credential WHERE node_id=$1`, nodeID).Scan(&hash, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNodeAuth
	}
	if err != nil {
		return err
	}
	if revokedAt != nil {
		return ErrNodeRevoked
	}
	want := hashNodeSecret(secret)
	if subtle.ConstantTimeCompare([]byte(hash), []byte(want)) != 1 {
		return ErrNodeAuth
	}
	return nil
}
