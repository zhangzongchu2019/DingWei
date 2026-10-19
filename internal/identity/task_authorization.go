package identity

import (
	"context"
	"sort"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// TaskAuthorizationStore holds identity/group locks while a task callback commits
// to SQLite. This is not a distributed transaction: a callback must report an
// uncertain SQLite outcome as such, even if releasing this transaction fails.
type TaskAuthorizationStore interface {
	WithTaskAuthorization(context.Context, string, string, []string, func([]Agent, func() error) error) error
}

func (s *PGStore) WithTaskAuthorization(ctx context.Context, owner, group string, ids []string, fn func([]Agent, func() error) error) error {
	if group == "" || len(ids) == 0 {
		return ErrForbidden
	}
	return s.inTx(ctx, func(tx repo.Tx) error {
		g, err := lockGroup(ctx, tx, group, true)
		if err != nil {
			return err
		}
		// Group first matches JoinGroup/LeaveGroup. Stable ordering for peer agents
		// prevents opposing developer/reviewer assignments from taking inverse locks.
		keys := append([]string(nil), ids...)
		sort.Strings(keys)
		agents := make([]Agent, 0, len(keys))
		seen := map[string]bool{}
		for _, id := range keys {
			if seen[id] {
				continue
			}
			seen[id] = true
			a, e := scanAgent(tx.QueryRow(ctx, `SELECT `+agentCols+` FROM np_agent WHERE id=$1 FOR SHARE`, id))
			if e != nil {
				return e
			}
			if a.State != AgentActive {
				return ErrForbidden
			}
			agents = append(agents, *a)
		}
		users := map[string]bool{Lower(owner): true}
		for _, a := range agents {
			users[a.OwnerLC] = true
		}
		names := make([]string, 0, len(users))
		for u := range users {
			names = append(names, u)
		}
		sort.Strings(names)
		for _, u := range names {
			var state string
			if e := tx.QueryRow(ctx, `SELECT state FROM np_user WHERE user_id_lc=$1 FOR SHARE`, u).Scan(&state); e != nil {
				return e
			}
			if state != string(UserActive) {
				return ErrForbidden
			}
		}
		if g.CreatorLC != Lower(owner) {
			rows, e := tx.Query(ctx, `SELECT a.id FROM np_group_member m JOIN np_agent a ON a.id=m.agent_id WHERE m.code=$1 AND a.owner_lc=$2 AND a.state='active' ORDER BY a.id FOR SHARE OF a`, group, Lower(owner))
			if e != nil {
				return e
			}
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return e
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		check := func() error {
			for _, a := range agents {
				var n int
				if e := tx.QueryRow(ctx, `SELECT count(*) FROM np_group_member WHERE code=$1 AND agent_id=$2`, group, a.ID).Scan(&n); e != nil {
					return e
				}
				if n != 1 {
					return ErrForbidden
				}
			}
			if g.CreatorLC != Lower(owner) {
				var n int
				if e := tx.QueryRow(ctx, `SELECT count(*) FROM np_group_member m JOIN np_agent a ON a.id=m.agent_id WHERE m.code=$1 AND a.owner_lc=$2 AND a.state='active'`, group, Lower(owner)).Scan(&n); e != nil {
					return e
				}
				if n == 0 {
					return ErrForbidden
				}
			}
			// A round-trip immediately before the SQLite commit also detects a lost PG
			// connection. Group SHARE prevents concurrent membership deletion.
			var alive int
			return tx.QueryRow(ctx, `SELECT 1`).Scan(&alive)
		}
		if err = check(); err != nil {
			return err
		}
		return fn(agents, check)
	})
}

func (m *MemStore) WithTaskAuthorization(ctx context.Context, owner, group string, ids []string, fn func([]Agent, func() error) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.groups[group]
	if g == nil {
		return ErrForbidden
	}
	u := m.users[Lower(owner)]
	if u == nil || u.State != UserActive {
		return ErrForbidden
	}
	allowed := g.CreatorLC == Lower(owner)
	for id := range m.members[group] {
		if a := m.agents[id]; a != nil && a.OwnerLC == Lower(owner) && a.State == AgentActive {
			allowed = true
		}
	}
	if !allowed {
		return ErrForbidden
	}
	var agents []Agent
	for _, id := range ids {
		a := m.agents[id]
		if a == nil || a.State != AgentActive {
			return ErrForbidden
		}
		if _, ok := m.members[group][id]; !ok {
			return ErrForbidden
		}
		u := m.users[a.OwnerLC]
		if u == nil || u.State != UserActive {
			return ErrForbidden
		}
		agents = append(agents, *a)
	}
	return fn(agents, func() error { return ctx.Err() })
}

var _ TaskAuthorizationStore = (*PGStore)(nil)
var _ TaskAuthorizationStore = (*MemStore)(nil)
