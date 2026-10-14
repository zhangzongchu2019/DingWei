package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func (s *SQLite) CreateConsoleSession(ctx context.Context, session model.ConsoleSession, now time.Time) error {
	if session.ID == "" || session.PrincipalID == "" || session.TokenHash == "" || !session.ExpiresAt.After(now) {
		return errors.New("invalid console session")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_session(id,principal_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)`,
		session.ID, session.PrincipalID, session.TokenHash, session.ExpiresAt.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) ResolveConsoleSession(ctx context.Context, tokenHash string, now time.Time) (*model.ConsoleSession, error) {
	var session model.ConsoleSession
	var expires, created string
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.principal_id,s.token_hash,s.expires_at,s.created_at FROM console_session s
		JOIN console_principal p ON p.id=s.principal_id WHERE s.token_hash=? AND s.revoked_at IS NULL AND p.active=1 AND s.expires_at>?`,
		tokenHash, now.UTC().Format(time.RFC3339Nano)).Scan(&session.ID, &session.PrincipalID, &session.TokenHash, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return nil, err
	}
	// created_at 供滑动续期算绝对上限;老行若格式异常不让整次鉴权失败 —— 上限退化为「不可续」,
	// 那是 fail-closed 的方向(宁可让用户重登,不可让一个算不出上限的会话无限续)。
	session.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		session.CreatedAt = time.Time{}
	}
	return &session, nil
}

// TouchConsoleSession 把会话的过期时刻**只向后**推。D1(业主第 7 条:页面一直开着就不必重登)。
//
// 🔴 三道守卫，缺一不可：
//   - `revoked_at IS NULL`  —— 已吊销的不得被续活
//   - `expires_at>?`(now)   —— **已经过期的不得被续活**(否则「过期」形同虚设)
//   - `expires_at<?`(新值)  —— 只延长不缩短(并发多标签下，晚到的旧值不得把已续的窗口拉回来)
func (s *SQLite) TouchConsoleSession(ctx context.Context, sessionID string, newExpiry, now time.Time) error {
	if sessionID == "" || !newExpiry.After(now) {
		return errors.New("invalid console session touch")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE console_session SET expires_at=?
		WHERE id=? AND revoked_at IS NULL AND expires_at>? AND expires_at<?`,
		newExpiry.UTC().Format(time.RFC3339Nano), sessionID,
		now.UTC().Format(time.RFC3339Nano), newExpiry.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) RevokeConsoleSession(ctx context.Context, sessionID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE console_session SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now.UTC().Format(time.RFC3339Nano), sessionID)
	return err
}

// RotateConsolePageCapability atomically verifies all four renewal gates:
// live console session, active interact grant, exact page-agent binding, and
// revocation of every older generation before the new capability is visible.
func (s *SQLite) RotateConsolePageCapability(ctx context.Context, capability model.ConsolePageCapability, keyID, sessionName string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_session s JOIN console_principal p ON p.id=s.principal_id
		JOIN console_agent_grant g ON g.principal_id=p.id JOIN console_agent a ON a.id=g.agent_id
		WHERE s.id=? AND s.principal_id=? AND s.revoked_at IS NULL AND s.expires_at>? AND p.active=1
		AND a.id=? AND a.key_id=? AND a.session_name=? AND a.active=1 AND g.active=1 AND g.permission='interact'`,
		capability.SessionID, capability.PrincipalID, now.UTC().Format(time.RFC3339Nano), capability.AgentID, keyID, sessionName).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("console capability authorization denied")
	}
	if capability.ID == "" || capability.PageID == "" || capability.TokenHash == "" || !capability.ExpiresAt.After(now) {
		return errors.New("invalid console capability")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_page_capability SET revoked_at=? WHERE session_id=? AND agent_id=? AND page_id=? AND revoked_at IS NULL`, now.UTC().Format(time.RFC3339Nano), capability.SessionID, capability.AgentID, capability.PageID); err != nil {
		return err
	}
	var generation int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1 FROM console_page_capability WHERE session_id=? AND agent_id=? AND page_id=?`, capability.SessionID, capability.AgentID, capability.PageID).Scan(&generation); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO console_page_capability(id,session_id,principal_id,agent_id,page_id,token_hash,generation,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, capability.ID, capability.SessionID, capability.PrincipalID, capability.AgentID, capability.PageID, capability.TokenHash, generation, capability.ExpiresAt.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeConsolePageCapability revokes exactly one previously issued
// capability. It deliberately does not rotate a page scope: a grant revoke
// must never invalidate a later approval for the same Agent and page.
func (s *SQLite) RevokeConsolePageCapability(ctx context.Context, capabilityID, sessionID, principalID, agentID, pageID string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE console_page_capability
		SET revoked_at=COALESCE(revoked_at, ?)
		WHERE id=? AND session_id=? AND principal_id=? AND agent_id=? AND page_id=?`,
		now.UTC().Format(time.RFC3339Nano), capabilityID, sessionID, principalID, agentID, pageID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *SQLite) ResolveConsolePageCapability(ctx context.Context, tokenHash, agentID, pageID string, now time.Time) (*model.ConsolePageCapability, error) {
	var c model.ConsolePageCapability
	var expires string
	err := s.db.QueryRowContext(ctx, `SELECT c.id,c.session_id,c.principal_id,c.agent_id,c.page_id,c.token_hash,c.generation,c.expires_at FROM console_page_capability c
		JOIN console_session s ON s.id=c.session_id JOIN console_principal p ON p.id=c.principal_id JOIN console_agent a ON a.id=c.agent_id
		JOIN console_agent_grant g ON g.principal_id=c.principal_id AND g.agent_id=c.agent_id AND g.permission='interact'
		WHERE c.token_hash=? AND c.agent_id=? AND c.page_id=? AND c.revoked_at IS NULL AND c.expires_at>? AND s.revoked_at IS NULL AND s.expires_at>? AND p.active=1 AND a.active=1 AND g.active=1`, tokenHash, agentID, pageID, now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)).Scan(&c.ID, &c.SessionID, &c.PrincipalID, &c.AgentID, &c.PageID, &c.TokenHash, &c.Generation, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	return &c, err
}

func (s *SQLite) UpsertConsolePrincipal(ctx context.Context, p model.ConsolePrincipal) error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Provider) == "" || strings.TrimSpace(p.ProviderIssuer) == "" || strings.TrimSpace(p.ProviderSubject) == "" || strings.TrimSpace(p.DisplayName) == "" {
		return errors.New("console principal identity fields required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_principal(id,provider,provider_issuer,provider_subject,display_name,active)
		VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET provider=excluded.provider,provider_issuer=excluded.provider_issuer,
		provider_subject=excluded.provider_subject,display_name=excluded.display_name,active=excluded.active`,
		p.ID, p.Provider, p.ProviderIssuer, p.ProviderSubject, p.DisplayName, boolInt(p.Active))
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: console_principal.display_name") {
		var existingID string
		if lookupErr := s.db.QueryRowContext(ctx, `SELECT id FROM console_principal WHERE display_name=? ORDER BY id LIMIT 1`, p.DisplayName).Scan(&existingID); lookupErr == nil {
			return fmt.Errorf("console principal display_name %q already exists (principal %s)", p.DisplayName, existingID)
		}
		return fmt.Errorf("console principal display_name %q already exists", p.DisplayName)
	}
	return err
}

func (s *SQLite) ResolveConsolePrincipal(ctx context.Context, provider, issuer, subject string) (*model.ConsolePrincipal, error) {
	var p model.ConsolePrincipal
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT id,provider,provider_issuer,provider_subject,display_name,active FROM console_principal
		WHERE provider=? AND provider_issuer=? AND provider_subject=?`, provider, issuer, subject).
		Scan(&p.ID, &p.Provider, &p.ProviderIssuer, &p.ProviderSubject, &p.DisplayName, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Active = active != 0
	return &p, nil
}

func (s *SQLite) GetConsolePrincipal(ctx context.Context, id string) (*model.ConsolePrincipal, error) {
	var p model.ConsolePrincipal
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT id,provider,provider_issuer,provider_subject,display_name,active FROM console_principal WHERE id=?`, id).Scan(&p.ID, &p.Provider, &p.ProviderIssuer, &p.ProviderSubject, &p.DisplayName, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Active = active != 0
	return &p, nil
}

func (s *SQLite) UpsertConsoleAgent(ctx context.Context, a model.ConsoleAgent) error {
	if a.ID == "" || a.KeyID == "" || a.SessionName == "" || a.OwnerPrincipal == "" {
		return errors.New("console agent identity fields required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_agent(id,key_id,session_name,owner_principal,active) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET key_id=excluded.key_id,session_name=excluded.session_name,owner_principal=excluded.owner_principal,active=excluded.active`,
		a.ID, a.KeyID, a.SessionName, a.OwnerPrincipal, boolInt(a.Active))
	return err
}

func (s *SQLite) UpsertConsoleAgentGrant(ctx context.Context, g model.ConsoleAgentGrant) error {
	if g.Permission != "view" && g.Permission != "interact" {
		return errors.New("invalid console permission")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_agent_grant(principal_id,agent_id,permission,active) VALUES(?,?,?,?)
		ON CONFLICT(principal_id,agent_id,permission) DO UPDATE SET active=excluded.active`, g.PrincipalID, g.AgentID, g.Permission, boolInt(g.Active))
	return err
}

func (s *SQLite) UpsertConsolePrincipalRole(ctx context.Context, role model.ConsolePrincipalRole) error {
	if strings.TrimSpace(role.PrincipalID) == "" || role.Role != "owner" || strings.TrimSpace(role.OwnerKey) == "" {
		return errors.New("invalid console principal role")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_principal_role(principal_id,role,owner_key,active) VALUES(?,?,?,?)
		ON CONFLICT(principal_id,role,owner_key) DO UPDATE SET active=excluded.active`,
		role.PrincipalID, role.Role, role.OwnerKey, boolInt(role.Active))
	return err
}

// ResolveConsoleOwnerScope returns one unambiguous owner data scope. Multiple
// active scopes fail closed: callers must select a scope explicitly before the
// model can support that shape.
func (s *SQLite) ResolveConsoleOwnerScope(ctx context.Context, principalID string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.owner_key FROM console_principal_role r
		JOIN console_principal p ON p.id=r.principal_id
		WHERE r.principal_id=? AND r.role='owner' AND r.active=1 AND p.active=1 ORDER BY r.owner_key`, principalID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var scopes []string
	for rows.Next() {
		var ownerKey string
		if err := rows.Scan(&ownerKey); err != nil {
			return "", err
		}
		scopes = append(scopes, ownerKey)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(scopes) != 1 {
		return "", nil
	}
	return scopes[0], nil
}

func (s *SQLite) UpsertConsolePlatformRole(ctx context.Context, role model.ConsolePlatformRole) error {
	if strings.TrimSpace(role.PrincipalID) == "" || (role.Role != "management" && role.Role != "owner") {
		return errors.New("invalid console platform role")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_platform_role(principal_id,role,active) VALUES(?,?,?)
		ON CONFLICT(principal_id,role) DO UPDATE SET active=excluded.active`,
		role.PrincipalID, role.Role, boolInt(role.Active))
	return err
}

// ResolveConsolePlatformRole returns the strongest active platform role.
// Every active console principal is an ordinary owner unless explicitly
// promoted to the management team. This default keeps existing owners able to
// read and submit while all management mutations remain opt-in.
func (s *SQLite) ResolveConsolePlatformRole(ctx context.Context, principalID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((
		SELECT r.role FROM console_platform_role r
		WHERE r.principal_id=p.id AND r.active=1
		ORDER BY CASE r.role WHEN 'management' THEN 0 ELSE 1 END LIMIT 1
	), 'owner') FROM console_principal p WHERE p.id=? AND p.active=1`, principalID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func (s *SQLite) ConsoleAgentAllowed(ctx context.Context, principalID, keyID, sessionName, permission string) (bool, error) {
	if permission != "view" && permission != "interact" {
		return false, errors.New("invalid console permission")
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_agent a JOIN console_agent_grant g ON g.agent_id=a.id
		JOIN console_principal p ON p.id=g.principal_id WHERE p.id=? AND p.active=1 AND a.key_id=? AND a.session_name=?
		AND a.active=1 AND g.active=1 AND (g.permission=? OR g.permission='interact')`, principalID, keyID, sessionName, permission).Scan(&n)
	return n > 0, err
}

func (s *SQLite) ListConsoleAgents(ctx context.Context, principalID string) ([]model.ConsoleAgent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT a.id,a.key_id,a.session_name,a.owner_principal,a.active FROM console_agent a
		JOIN console_agent_grant g ON g.agent_id=a.id JOIN console_principal p ON p.id=g.principal_id
		WHERE p.id=? AND p.active=1 AND a.active=1 AND g.active=1 ORDER BY a.session_name,a.key_id`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleAgent
	for rows.Next() {
		var a model.ConsoleAgent
		var active int
		if err := rows.Scan(&a.ID, &a.KeyID, &a.SessionName, &a.OwnerPrincipal, &active); err != nil {
			return nil, err
		}
		a.Active = active != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLite) ListOwnedConsoleAgents(ctx context.Context, principalID string) ([]model.ConsoleAgent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,key_id,session_name,owner_principal,active FROM console_agent WHERE owner_principal=? AND active=1 ORDER BY session_name,key_id`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleAgent
	for rows.Next() {
		var a model.ConsoleAgent
		var active int
		if err := rows.Scan(&a.ID, &a.KeyID, &a.SessionName, &a.OwnerPrincipal, &active); err != nil {
			return nil, err
		}
		a.Active = active != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLite) ResolveGrantedConsoleAgent(ctx context.Context, principalID, sessionName, permission string) (*model.ConsoleAgent, error) {
	if permission != "view" && permission != "interact" {
		return nil, errors.New("invalid console permission")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT a.id,a.key_id,a.session_name,a.owner_principal,a.active FROM console_agent a
		JOIN console_agent_grant g ON g.agent_id=a.id JOIN console_principal p ON p.id=g.principal_id
		WHERE p.id=? AND p.active=1 AND a.session_name=? AND a.active=1 AND g.active=1 AND (g.permission=? OR g.permission='interact')`, principalID, sessionName, permission)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleAgent
	for rows.Next() {
		var a model.ConsoleAgent
		var active int
		if err := rows.Scan(&a.ID, &a.KeyID, &a.SessionName, &a.OwnerPrincipal, &active); err != nil {
			return nil, err
		}
		a.Active = active != 0
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, nil
	}
	return &out[0], nil
}

// ListConsolePrincipals 列出全部 console principal(**含 active=0**),按 id 升序。
//
// Nova-260921-A(B1-b)「增删管理者」需要它:在此之前全仓**没有任何**列举 principal 的方法
// (只有 Upsert 与按 provider/issuer/subject 的 Resolve),`console_platform_role` 的唯一写入点
// 是 seed(cmd/workpulse/console_seed.go:81)⇒ 改管理者名单只能改配置文件再重启。
//
// 🔴 **含停用行是刻意的**:管理者名册页必须能看见"这个 principal 已停用"。
// 否则一个停用 principal 上挂着的 management 行会成为**看不见的悬空授权** ——
// 而 ResolveConsolePlatformRole 对停用 principal 返回 "",两边说法不一致却没人能看见。
func (s *SQLite) ListConsolePrincipals(ctx context.Context) ([]model.ConsolePrincipal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider,provider_issuer,provider_subject,display_name,active
		FROM console_principal ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ConsolePrincipal{}
	for rows.Next() {
		var p model.ConsolePrincipal
		var active int
		if err := rows.Scan(&p.ID, &p.Provider, &p.ProviderIssuer, &p.ProviderSubject, &p.DisplayName, &active); err != nil {
			return nil, err
		}
		p.Active = active != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListActiveConsoleManagement 返回当前 active 的 management principal id(升序)。
//
// 🔴 「剩最后一个不能删」这条守卫(板件 REQUIREMENT-REGISTRY-20260921.md:42)要靠它计数。
// 用**列举**而不是 COUNT(*):调用方需要知道"最后一个是谁",才能在名册页上
// 就地把那个按钮禁用并写明原因(判据写在它生效的地方),而不是让人点了才被拒。
// 🔴 查询出错时调用方必须**拒**,不得当成"0 个"或"很多个"继续(fail-closed)。
func (s *SQLite) ListActiveConsoleManagement(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT principal_id FROM console_platform_role
		WHERE role='management' AND active=1 ORDER BY principal_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
