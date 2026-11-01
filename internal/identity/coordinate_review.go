package identity

import "context"

// CoordinateReviewAuthorizationStore fences the owned Agent and its owner's
// trusted personal-channel identity together. A Git account is NOT inherited.
type CoordinateReviewAuthorizationStore interface {
	WithCoordinateReviewAuthorization(context.Context, string, string, func(Agent, Identity, func() error) error) error
}

func coordinateReviewAuthorization(owner, id string, fn func(Agent, Identity, func() error) error) func(ContextAuthorization, func() error) error {
	return func(a ContextAuthorization, check func() error) error {
		agent, ok := a.Agents[id]
		if !ok || !a.Active(id) || agent.OwnerLC != Lower(owner) {
			return ErrForbidden
		}
		return fn(agent, a.Bindings[Lower(owner)], check)
	}
}
func (m *MemStore) WithCoordinateReviewAuthorization(ctx context.Context, owner, id string, fn func(Agent, Identity, func() error) error) error {
	return m.withContextAuthorization(ctx, "", "", nil, owner, coordinateReviewAuthorization(owner, id, fn), contextAuthOptions{Users: []string{owner}, Agents: []string{id}})
}
func (s *PGStore) WithCoordinateReviewAuthorization(ctx context.Context, owner, id string, fn func(Agent, Identity, func() error) error) error {
	return s.withContextAuthorization(ctx, "", "", nil, owner, coordinateReviewAuthorization(owner, id, fn), contextAuthOptions{Users: []string{owner}, Agents: []string{id}})
}
