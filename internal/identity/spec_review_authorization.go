package identity

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// SpecReviewAuthorizationStore authorizes a private snapshot against a current owned Agent.
// No group or task assignment grants access to this channel.
type SpecReviewAuthorizationStore interface {
	WithOwnedAgentAuthorization(context.Context, string, string, func(Agent, func() error) error) error
}

func (s *PGStore) WithOwnedAgentAuthorization(ctx context.Context, owner, id string, fn func(Agent, func() error) error) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		a, e := scanAgent(tx.QueryRow(ctx, `SELECT `+agentCols+` FROM np_agent WHERE id=$1 FOR SHARE`, id))
		if e != nil {
			return e
		}
		if a.State != AgentActive || a.OwnerLC != Lower(owner) {
			return ErrForbidden
		}
		var state string
		if e = tx.QueryRow(ctx, `SELECT state FROM np_user WHERE user_id_lc=$1 FOR SHARE`, Lower(owner)).Scan(&state); e != nil {
			return e
		}
		if state != string(UserActive) {
			return ErrForbidden
		}
		check := func() error { var n int; return tx.QueryRow(ctx, `SELECT 1`).Scan(&n) }
		return fn(*a, check)
	})
}
func (m *MemStore) WithOwnedAgentAuthorization(ctx context.Context, owner, id string, fn func(Agent, func() error) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.agents[id]
	u := m.users[Lower(owner)]
	if a == nil || a.State != AgentActive || a.OwnerLC != Lower(owner) || u == nil || u.State != UserActive {
		return ErrForbidden
	}
	return fn(*a, func() error { return ctx.Err() })
}
