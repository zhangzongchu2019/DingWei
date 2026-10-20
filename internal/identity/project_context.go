package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// ContextClaim contains only candidate identity references, never document data.
type ContextClaim struct{ Owner, Group, Developer, Reviewer string }
type ContextAuthorization struct {
	Actor   Agent
	Groups  map[string]Group
	Members map[string]map[string]bool
	Agents  map[string]Agent
	Users   map[string]bool
	Names   map[string]string
}
type ProjectContextAuthorizationStore interface {
	WithProjectContextAuthorization(context.Context, string, string, []ContextClaim, func(ContextAuthorization, func() error) error) error
}

// BlockerAuthorizationStore also locks an explicitly addressed waiting person.
// A waiting person receives only that blocker's reply right, not task access.
// Human and Agent authentication are separate server-selected entry points.
type BlockerAuthorizationStore interface {
	WithBlockerAuthorization(context.Context, string, string, string, ContextClaim, func(ContextAuthorization, func() error) error) error
}

func (m *MemStore) WithBlockerAuthorization(ctx context.Context, actor, hash, human string, claim ContextClaim, fn func(ContextAuthorization, func() error) error) error {
	if claim.Group == "" || (actor == "") == (human == "") {
		return ErrForbidden
	}
	return m.withContextAuthorization(ctx, actor, hash, []ContextClaim{claim}, human, fn)
}
func (s *PGStore) WithBlockerAuthorization(ctx context.Context, actor, hash, human string, claim ContextClaim, fn func(ContextAuthorization, func() error) error) error {
	if claim.Group == "" || (actor == "") == (human == "") {
		return ErrForbidden
	}
	return s.withContextAuthorization(ctx, actor, hash, []ContextClaim{claim}, human, fn)
}

func (a ContextAuthorization) Member(group, id string) bool { return a.Members[group][id] }
func (a ContextAuthorization) Active(id string) bool {
	x, ok := a.Agents[id]
	return ok && x.State == AgentActive && a.Users[x.OwnerLC]
}
func (a ContextAuthorization) Allows(c ContextClaim) bool {
	g, ok := a.Groups[c.Group]
	if !ok || !a.Users[Lower(c.Owner)] || c.Developer == c.Reviewer || !a.Active(c.Developer) || !a.Active(c.Reviewer) || !a.Member(c.Group, c.Developer) || !a.Member(c.Group, c.Reviewer) {
		return false
	}
	if g.CreatorLC == Lower(c.Owner) {
		return true
	}
	for id := range a.Members[c.Group] {
		if a.Agents[id].OwnerLC == Lower(c.Owner) && a.Active(id) {
			return true
		}
	}
	return false
}
func (a ContextAuthorization) Digest() string {
	// Keys are sorted by encoding/json; no credentials or full Agent values are serialized.
	type principal struct {
		Owner, Name, State string
		ActiveOwner        bool
	}
	agents := map[string]principal{}
	for id, x := range a.Agents {
		agents[id] = principal{x.OwnerLC, x.Name, string(x.State), a.Users[x.OwnerLC]}
	}
	raw, _ := json.Marshal(struct {
		Groups  map[string]Group
		Members map[string]map[string]bool
		Agents  map[string]principal
		Users   map[string]bool
		Names   map[string]string
	}{a.Groups, a.Members, agents, a.Users, a.Names})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (a ContextAuthorization) validate(actor, hash string) error {
	if a.Actor.ID != actor || !a.Active(actor) || hash == "" || subtle.ConstantTimeCompare([]byte(a.Actor.CredentialHash), []byte(hash)) != 1 {
		return ErrForbidden
	}
	return nil
}
func contextAuth() ContextAuthorization {
	return ContextAuthorization{Groups: map[string]Group{}, Members: map[string]map[string]bool{}, Agents: map[string]Agent{}, Users: map[string]bool{}, Names: map[string]string{}}
}
func sortedContextKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func (m *MemStore) WithProjectContextAuthorization(ctx context.Context, actor, hash string, claims []ContextClaim, fn func(ContextAuthorization, func() error) error) error {
	return m.withContextAuthorization(ctx, actor, hash, claims, "", fn)
}
func (m *MemStore) withContextAuthorization(ctx context.Context, actor, hash string, claims []ContextClaim, human string, fn func(ContextAuthorization, func() error) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := contextAuth()
	groups := map[string]bool{}
	for code, ids := range m.members {
		if _, ok := ids[actor]; ok {
			groups[code] = true
		}
	}
	for _, claim := range claims {
		groups[claim.Group] = true
	}
	if human != "" {
		a.Users[Lower(human)] = false
	}

	for code := range groups {
		if g := m.groups[code]; g != nil {
			a.Groups[code] = *g
			a.Users[g.CreatorLC] = false
			a.Members[code] = map[string]bool{}
			for id := range m.members[code] {
				a.Members[code][id] = true
				if x := m.agents[id]; x != nil {
					a.Agents[id] = *x
				}
			}
		}
	}
	if x := m.agents[actor]; x != nil {
		a.Actor = *x
		a.Agents[actor] = *x
	}
	for _, x := range a.Agents {
		a.Users[x.OwnerLC] = false
	}
	for id := range a.Users {
		u := m.users[id]
		a.Users[id] = u != nil && u.State == UserActive
		if u != nil {
			a.Names[id] = u.ShortName
		}
	}
	if human != "" {
		if !a.Users[Lower(human)] || actor != "" || hash != "" {
			return ErrForbidden
		}
	} else {
		if err := a.validate(actor, hash); err != nil {
			return err
		}
	}
	return fn(a, func() error { return ctx.Err() })
}
func (s *PGStore) WithProjectContextAuthorization(ctx context.Context, actor, hash string, claims []ContextClaim, fn func(ContextAuthorization, func() error) error) error {
	return s.withContextAuthorization(ctx, actor, hash, claims, "", fn)
}
func (s *PGStore) withContextAuthorization(ctx context.Context, actor, hash string, claims []ContextClaim, human string, fn func(ContextAuthorization, func() error) error) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		a := contextAuth()
		groups := map[string]bool{}
		initial := map[string]bool{}
		// Every candidate query is a keyset page of at most 101, not a platform dump.
		after := ""
		for {
			rows, err := tx.Query(ctx, `SELECT code FROM np_group_member WHERE agent_id=$1 AND code>$2 ORDER BY code LIMIT 101`, actor, after)
			if err != nil {
				return err
			}
			n := 0
			for rows.Next() {
				var code string
				if err = rows.Scan(&code); err != nil {
					rows.Close()
					return err
				}
				groups[code] = true
				initial[code] = true
				after = code
				n++
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if n < 101 {
				break
			}
		}
		for _, claim := range claims {
			groups[claim.Group] = true
		}
		if human != "" {
			a.Users[Lower(human)] = false
		}
		for _, code := range sortedContextKeys(groups) {
			g, err := lockGroup(ctx, tx, code, true)
			if errors.Is(err, ErrGroupNotFound) || errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			a.Groups[code] = *g
			a.Users[g.CreatorLC] = false
			a.Members[code] = map[string]bool{}
			after = ""
			for {
				rows, e := tx.Query(ctx, `SELECT agent_id FROM np_group_member WHERE code=$1 AND agent_id>$2 ORDER BY agent_id LIMIT 101`, code, after)
				if e != nil {
					return e
				}
				n := 0
				for rows.Next() {
					var id string
					if e = rows.Scan(&id); e != nil {
						rows.Close()
						return e
					}
					a.Members[code][id] = true
					after = id
					n++
				}
				e = rows.Err()
				rows.Close()
				if e != nil {
					return e
				}
				if n < 101 {
					break
				}
			}
		}
		// Catch joins/leaves which committed between candidate discovery and locking.
		checkGroups := func() error {
			now := map[string]bool{}
			after := ""
			for {
				rows, e := tx.Query(ctx, `SELECT code FROM np_group_member WHERE agent_id=$1 AND code>$2 ORDER BY code LIMIT 101`, actor, after)
				if e != nil {
					return e
				}
				n := 0
				for rows.Next() {
					var code string
					if e = rows.Scan(&code); e != nil {
						rows.Close()
						return e
					}
					now[code] = true
					after = code
					n++
				}
				e = rows.Err()
				rows.Close()
				if e != nil {
					return e
				}
				if n < 101 {
					break
				}
			}
			if len(now) != len(initial) {
				return ErrConflict
			}
			for code := range now {
				if !initial[code] {
					return ErrConflict
				}
			}
			return nil
		}
		if err := checkGroups(); err != nil {
			return err
		}
		ids := map[string]bool{}
		if actor != "" {
			ids[actor] = true
		}
		for _, members := range a.Members {
			for id := range members {
				ids[id] = true
			}
		}
		for _, id := range sortedContextKeys(ids) {
			x, err := scanAgent(tx.QueryRow(ctx, `SELECT `+agentCols+` FROM np_agent WHERE id=$1 FOR SHARE`, id))
			if err != nil {
				return err
			}
			a.Agents[id] = *x
			a.Users[x.OwnerLC] = false
			if id == actor {
				a.Actor = *x
			}
		}
		for _, id := range sortedContextKeys(a.Users) {
			var state, name string
			err := tx.QueryRow(ctx, `SELECT state,short_name FROM np_user WHERE user_id_lc=$1 FOR SHARE`, id).Scan(&state, &name)
			if err != nil {
				return err
			}
			a.Users[id] = state == string(UserActive)
			a.Names[id] = name
		}
		if human != "" {
			if !a.Users[Lower(human)] || actor != "" || hash != "" {
				return ErrForbidden
			}
		} else {
			if err := a.validate(actor, hash); err != nil {
				return err
			}
		}
		check := func() error {
			if err := checkGroups(); err != nil {
				return err
			}
			var alive int
			return tx.QueryRow(ctx, `SELECT 1`).Scan(&alive)
		}
		return fn(a, check)
	})
}
