package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

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
