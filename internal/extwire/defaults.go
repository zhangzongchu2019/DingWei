package extwire

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// ConsentTargets owns an immutable tenant allowlist; no online roster fallback.
type ConsentTargets struct {
	allowed  map[string][]string
	Identity *identity.Service
	Pool     *repo.Pool
	Access   repo.AccessCtx
}

// ParseConsentTargets parses DW_MCP_CONSENT_TARGETS_JSON, e.g.
// {"tenant-key":["helper.owner"]}. Missing/empty lists keep manual login.
func ParseConsentTargets(raw string) (*ConsentTargets, error) {
	d := &ConsentTargets{allowed: make(map[string][]string)}
	if strings.TrimSpace(raw) == "" {
		return d, nil
	}
	if err := json.Unmarshal([]byte(raw), &d.allowed); err != nil || d.allowed == nil {
		return nil, errors.New("extwire: invalid consent target configuration")
	}
	for tenant, names := range d.allowed {
		if tenant == "" || strings.TrimSpace(tenant) != tenant || len(names) > 64 {
			return nil, errors.New("extwire: invalid consent tenant or list size")
		}
		seen := map[string]bool{}
		for _, name := range names {
			i := strings.LastIndexByte(name, '.')
			if i < 1 || !identity.ValidAgentName(name[:i]) || !identity.ValidShortName(name[i+1:]) || seen[strings.ToLower(name)] {
				return nil, errors.New("extwire: invalid or duplicate consent target")
			}
			seen[strings.ToLower(name)] = true
		}
	}
	return d, nil
}
func (d *ConsentTargets) Allowed(tenant string) []string {
	return append([]string(nil), d.allowed[tenant]...)
}
func (d *ConsentTargets) Valid(ctx context.Context, target string) bool {
	if d.Identity == nil {
		return false
	}
	a, u, e := d.Identity.ResolveFullName(ctx, target)
	return e == nil && a.State == identity.AgentActive && u.State == identity.UserActive && a.Origin != identity.OriginExternal
}
func (d *ConsentTargets) Recent(ctx context.Context, p extshared.Principal) (string, error) {
	if d.Pool == nil {
		return "", errors.New("extwire: missing recent-target store")
	}
	row, e := d.Pool.QueryRow(ctx, d.Access, `SELECT a.name||'.'||u.short_name FROM np_external_principal p JOIN np_extseat s ON s.principal_id=p.id JOIN np_temp_group g ON g.external_agent_id=s.agent_id JOIN np_agent a ON a.id=g.target_agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE p.provider=$1 AND p.tenant_key=$2 AND p.issuer=$3 AND p.subject=$4 ORDER BY g.seq DESC LIMIT 1`, p.Provider, p.TenantKey, p.Issuer, p.Subject)
	if e != nil {
		return "", e
	}
	var target string
	e = row.Scan(&target)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	return target, e
}
