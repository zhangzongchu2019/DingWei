package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新平台身份垫片（R-V2-SPEC-TASK-SHIM-IMPL-20261002164524，定案甲）：把新平台（PG np_user／np_agent）的一个用户
// 投影进旧 SQLite 的 console_principal／console_principal_role／console_platform_role／console_agent／console_agent_grant，
// 让 spec 与任务管理的旧处理器（外键绑在这些表上）不改一行就能用新平台身份。
//
// 🔴 一个用户的投影在【一个事务】里写完：任何一步失败整体回滚（复验方 H15：同步失败不得半投影）。
// 🔴 只动 provider=newplat 的 principal 及其名下行；旧主体一行不碰。

// NewPlatformConsoleProvider 投影出的 console_principal.provider。
const NewPlatformConsoleProvider = "newplat"

// SyncNewPlatformConsoleIdentity 写一个新平台用户的投影。返回实际写入的 display_name（撞名时与 p.DisplayName 不同）。
func (s *SQLite) SyncNewPlatformConsoleIdentity(ctx context.Context, p model.NewPlatformConsoleProjection) (string, error) {
	if p.PrincipalID == "" || p.OwnerKey == "" || p.DisplayName == "" {
		return "", errors.New("newplat projection: principal, owner key and display name required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 已有同 id 的非 newplat 主体 ⇒ 拒（不覆盖旧世界的人）。
	var provider string
	switch err := tx.QueryRowContext(ctx, `SELECT provider FROM console_principal WHERE id=?`, p.PrincipalID).Scan(&provider); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return "", err
	case provider != NewPlatformConsoleProvider:
		return "", errors.New("newplat projection: principal id taken by a non-newplat principal")
	}
	// display_name 唯一（console_principal.display_name UNIQUE，见 preflightConsolePrincipalDisplayNames）：
	// 撞上别的主体 ⇒ 取「短名·用户ID」，不让整次同步失败（复验方 U4 → 本件 U5）。
	display := p.DisplayName
	var holder string
	switch err := tx.QueryRowContext(ctx, `SELECT id FROM console_principal WHERE display_name=? AND id<>?`, display, p.PrincipalID).Scan(&holder); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return "", err
	default:
		display = p.DisplayName + "·" + p.PrincipalID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO console_principal(id,provider,provider_issuer,provider_subject,display_name,active) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name,active=excluded.active`,
		p.PrincipalID, NewPlatformConsoleProvider, p.Issuer, p.PrincipalID, display, boolInt(p.Active)); err != nil {
		return "", err
	}
	// 单一 owner 作用域：恰一行 u_<user>（ResolveConsoleOwnerScope 要求唯一）；本主体其余 owner 行一律停用。
	if _, err := tx.ExecContext(ctx, `UPDATE console_principal_role SET active=0 WHERE principal_id=? AND role='owner' AND owner_key<>?`, p.PrincipalID, p.OwnerKey); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO console_principal_role(principal_id,role,owner_key,active) VALUES(?,?,?,?)
		ON CONFLICT(principal_id,role,owner_key) DO UPDATE SET active=excluded.active`, p.PrincipalID, "owner", p.OwnerKey, boolInt(p.Active)); err != nil {
		return "", err
	}
	// 门 B（组长 16:55:17Z）：可管理用户投影 console_platform_role＝management；失去管理能力即停用该行（回落 owner）。
	if _, err := tx.ExecContext(ctx, `INSERT INTO console_platform_role(principal_id,role,active) VALUES(?,?,?)
		ON CONFLICT(principal_id,role) DO UPDATE SET active=excluded.active`, p.PrincipalID, "management", boolInt(p.Active && p.Management)); err != nil {
		return "", err
	}
	keep := map[string]bool{}
	for _, a := range p.Agents {
		if a.ID == "" || a.SessionName == "" {
			return "", errors.New("newplat projection: agent id and session name required")
		}
		keep[a.ID] = true
		// (key_id, session_name) 唯一：同名 Agent 吊销后重新入网是另一个 id ⇒ 旧行改名腾位（id 与外键不动，保持停用）。
		if _, err := tx.ExecContext(ctx, `UPDATE console_agent SET session_name=session_name||'~'||id,active=0
			WHERE key_id=? AND session_name=? AND id<>?`, p.OwnerKey, a.SessionName, a.ID); err != nil {
			return "", err
		}
		active := p.Active && a.Active
		if _, err := tx.ExecContext(ctx, `INSERT INTO console_agent(id,key_id,session_name,owner_principal,active) VALUES(?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET key_id=excluded.key_id,session_name=excluded.session_name,owner_principal=excluded.owner_principal,active=excluded.active`,
			a.ID, p.OwnerKey, a.SessionName, p.PrincipalID, boolInt(active)); err != nil {
			return "", err
		}
		// D4 只属主：属主对自己的 Agent 一条 interact。
		if _, err := tx.ExecContext(ctx, `INSERT INTO console_agent_grant(principal_id,agent_id,permission,active) VALUES(?,?,?,?)
			ON CONFLICT(principal_id,agent_id,permission) DO UPDATE SET active=excluded.active`, p.PrincipalID, a.ID, "interact", boolInt(active)); err != nil {
			return "", err
		}
	}
	// 新平台已不在（吊销）的 Agent：停用，不删（spec／任务外键行保留）。
	rows, err := tx.QueryContext(ctx, `SELECT id FROM console_agent WHERE owner_principal=? AND active=1`, p.PrincipalID)
	if err != nil {
		return "", err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		if !keep[id] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, id := range stale {
		if _, err := tx.ExecContext(ctx, `UPDATE console_agent SET active=0 WHERE id=?`, id); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE console_agent_grant SET active=0 WHERE agent_id=?`, id); err != nil {
			return "", err
		}
	}
	if p.Session != nil {
		s := p.Session
		if s.ID == "" || s.TokenHash == "" || !strings.HasPrefix(s.ID, NewPlatformSessionPrefix) || !strings.HasPrefix(s.TokenHash, NewPlatformSessionPrefix) {
			return "", errors.New("newplat projection: session id and token hash must carry the newplat prefix")
		}
		// 投影会话只做外键与 capability 的会话锚；它的有效性另由 Hub 每次按新平台会话现查（不靠这里的 expires_at）。
		if _, err := tx.ExecContext(ctx, `INSERT INTO console_session(id,principal_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET expires_at=excluded.expires_at`,
			s.ID, p.PrincipalID, s.TokenHash, s.ExpiresAt.UTC().Format(time.RFC3339Nano), p.Now.UTC().Format(time.RFC3339Nano)); err != nil {
			return "", err
		}
	}
	return display, tx.Commit()
}

// NewPlatformSessionPrefix 投影会话的 id 与 token_hash 前缀：与旧 console 会话的令牌空间隔开（旧令牌哈希是裸 hex）。
const NewPlatformSessionPrefix = "np:"

// DeactivateNewPlatformConsoleAgent 即时停用一个投影 Agent（吊销／暂停钩子用，不等下一次同步）。
// 只动 provider=newplat 主体名下的行。
func (s *SQLite) DeactivateNewPlatformConsoleAgent(ctx context.Context, agentID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE console_agent SET active=0 WHERE id=? AND owner_principal IN
		(SELECT id FROM console_principal WHERE provider=?)`, agentID, NewPlatformConsoleProvider); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE console_agent_grant SET active=0 WHERE agent_id=? AND principal_id IN
		(SELECT id FROM console_principal WHERE provider=?)`, agentID, NewPlatformConsoleProvider); err != nil {
		return err
	}
	return tx.Commit()
}

// ListNewPlatformConsolePrincipalIDs 已投影的新平台主体 id（启动对账用：新平台已不存在的用户要停用）。
func (s *SQLite) ListNewPlatformConsolePrincipalIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM console_principal WHERE provider=? ORDER BY id`, NewPlatformConsoleProvider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeactivateNewPlatformConsolePrincipal 停用一个新平台主体及其名下全部投影（新平台里已查不到该用户时用）。
func (s *SQLite) DeactivateNewPlatformConsolePrincipal(ctx context.Context, principalID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`UPDATE console_principal SET active=0 WHERE id=? AND provider='` + NewPlatformConsoleProvider + `'`,
		`UPDATE console_principal_role SET active=0 WHERE principal_id=? AND principal_id IN (SELECT id FROM console_principal WHERE provider='` + NewPlatformConsoleProvider + `')`,
		`UPDATE console_platform_role SET active=0 WHERE principal_id=? AND principal_id IN (SELECT id FROM console_principal WHERE provider='` + NewPlatformConsoleProvider + `')`,
		`UPDATE console_agent SET active=0 WHERE owner_principal=? AND owner_principal IN (SELECT id FROM console_principal WHERE provider='` + NewPlatformConsoleProvider + `')`,
		`UPDATE console_agent_grant SET active=0 WHERE principal_id=? AND principal_id IN (SELECT id FROM console_principal WHERE provider='` + NewPlatformConsoleProvider + `')`,
	} {
		if _, err := tx.ExecContext(ctx, q, principalID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
