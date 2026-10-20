package identity

import "context"

// Candidate group IDs are rechecked under the existing PG group/member/user
// locks. Caller filtering must prove the owner still belongs to each group.
type QuestionContactAuthorizationStore interface {
	WithQuestionContacts(context.Context, string, []string, []string, func(ContextAuthorization, func() error) error) error
}

func questionContactClaims(groups []string, users []string) ([]ContextClaim, error) {
	if len(groups) > 200 || len(users) > 32 {
		return nil, ErrConflict
	}
	claims := make([]ContextClaim, 0, len(groups))
	for _, g := range groups {
		claims = append(claims, ContextClaim{Group: g})
	}
	return claims, nil
}
func (m *MemStore) WithQuestionContacts(ctx context.Context, human string, groups, users []string, fn func(ContextAuthorization, func() error) error) error {
	c, e := questionContactClaims(groups, users)
	if e != nil {
		return e
	}
	return m.withContextAuthorization(ctx, "", "", c, human, fn, contextAuthOptions{Users: users})
}
func (s *PGStore) WithQuestionContacts(ctx context.Context, human string, groups, users []string, fn func(ContextAuthorization, func() error) error) error {
	c, e := questionContactClaims(groups, users)
	if e != nil {
		return e
	}
	return s.withContextAuthorization(ctx, "", "", c, human, fn, contextAuthOptions{Users: users})
}
