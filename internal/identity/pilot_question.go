package identity

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
)

// QuestionAuthorizationStore binds a current human/Agent and the selected
// task pair, plus explicit question recipients. Bindings are private snapshots
// for a sender, never a platform directory or client-selected external identity.
type QuestionAuthorizationStore interface {
	WithQuestionAuthorization(context.Context, string, string, string, *ContextClaim, []string, []string, string, func(ContextAuthorization, func() error) error) error
}

func questionClaims(actor, hash, human string, claim *ContextClaim) ([]ContextClaim, error) {
	if (actor == "") == (human == "") {
		return nil, ErrForbidden
	}
	if human != "" && hash != "" {
		return nil, ErrForbidden
	}
	if claim != nil && claim.Group != "" {
		return []ContextClaim{*claim}, nil
	}
	if actor != "" {
		return nil, ErrForbidden
	}
	return nil, nil
}
func (m *MemStore) WithQuestionAuthorization(ctx context.Context, actor, hash, human string, claim *ContextClaim, users, agents []string, questionID string, fn func(ContextAuthorization, func() error) error) error {
	claims, err := questionClaims(actor, hash, human, claim)
	if err != nil {
		return err
	}
	// Include the actor even for an unbound question, enabling the binding path.
	users = append(append([]string{}, users...), human)
	return m.withContextAuthorization(ctx, actor, hash, claims, human, fn, contextAuthOptions{Users: users, Agents: agents, QuestionID: questionID})
}
func (s *PGStore) WithQuestionAuthorization(ctx context.Context, actor, hash, human string, claim *ContextClaim, users, agents []string, questionID string, fn func(ContextAuthorization, func() error) error) error {
	claims, err := questionClaims(actor, hash, human, claim)
	if err != nil {
		return err
	}
	users = append(append([]string{}, users...), human)
	return s.withContextAuthorization(ctx, actor, hash, claims, human, fn, contextAuthOptions{Users: users, Agents: agents, QuestionID: questionID})
}

// Advisory namespace is independent of group/user keys. It serializes only
// same-question operations through provider send, without holding a SQLite
// writer or blocking unrelated tasks over the external network call.
type contextAuthOptions struct {
	Users      []string
	Agents     []string
	QuestionID string
}

func questionLockID(id string) int64 {
	sum := sha256.Sum256([]byte("dingwei:pilot-question:" + id))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}
