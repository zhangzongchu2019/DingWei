package store

import (
	"context"
	"database/sql"
	"errors"
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
	var expires string
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.principal_id,s.token_hash,s.expires_at FROM console_session s
		JOIN console_principal p ON p.id=s.principal_id WHERE s.token_hash=? AND s.revoked_at IS NULL AND p.active=1 AND s.expires_at>?`,
		tokenHash, now.UTC().Format(time.RFC3339Nano)).Scan(&session.ID, &session.PrincipalID, &session.TokenHash, &expires)
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
	return &session, nil
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
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Provider) == "" || strings.TrimSpace(p.ProviderIssuer) == "" || strings.TrimSpace(p.ProviderSubject) == "" {
		return errors.New("console principal identity fields required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_principal(id,provider,provider_issuer,provider_subject,display_name,active)
		VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET provider=excluded.provider,provider_issuer=excluded.provider_issuer,
		provider_subject=excluded.provider_subject,display_name=excluded.display_name,active=excluded.active`,
		p.ID, p.Provider, p.ProviderIssuer, p.ProviderSubject, p.DisplayName, boolInt(p.Active))
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
