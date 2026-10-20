package identity

import "context"

// Explicit template invitations carry no task/group permission. Current actor,
// owners and proposed principals remain locked in PG through SQLite commit.
type TemplateAuthorizationStore interface {
	WithTemplateAuthorization(context.Context, string, string, string, []string, []string, string, func(ContextAuthorization, func() error) error) error
}

func templateClaims(actor, hash, human string) error {
	if (actor == "") == (human == "") || human != "" && hash != "" {
		return ErrForbidden
	}
	return nil
}
func (m *MemStore) WithTemplateAuthorization(ctx context.Context, actor, hash, human string, users, agents []string, id string, fn func(ContextAuthorization, func() error) error) error {
	if e := templateClaims(actor, hash, human); e != nil {
		return e
	}
	users = append(append([]string{}, users...), human)
	return m.withContextAuthorization(ctx, actor, hash, nil, human, fn, contextAuthOptions{Users: users, Agents: agents, QuestionID: "pilot-template:" + id})
}
func (s *PGStore) WithTemplateAuthorization(ctx context.Context, actor, hash, human string, users, agents []string, id string, fn func(ContextAuthorization, func() error) error) error {
	if e := templateClaims(actor, hash, human); e != nil {
		return e
	}
	users = append(append([]string{}, users...), human)
	return s.withContextAuthorization(ctx, actor, hash, nil, human, fn, contextAuthOptions{Users: users, Agents: agents, QuestionID: "pilot-template:" + id})
}
