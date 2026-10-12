package m8

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// Invoked only by the real HTTP1 product fixture. No test bypass of primary
// authorization or notification hook: ResetPassword's current no-hook path is
// deliberately covered by the actual periodic revalidator and socket tap.
func exerciseMuxResetChain(ctx context.Context, ob *identity.Onboarding, store *identity.PGStore, activeToken string) error {
	second, _, err := ob.PasswordLogin(ctx, "owner", "Local-Test-Initial-0042", "reset-test-second")
	if err != nil {
		return fmt.Errorf("second login: %w", err)
	}
	temporary, err := ob.ResetPassword(ctx, "owner")
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	denied := func(token string) error {
		if _, err := ob.ResolveSession(ctx, token); err == nil {
			return errors.New("old session survived reset")
		}
		if _, err := store.AuthorizeConsoleMux(ctx, identity.HashSecret(token), []string{"agent04.owner"}); !errors.Is(err, consolemux.ErrDenied) {
			return fmt.Errorf("old mux authority: %w", err)
		}
		return nil
	}
	for _, token := range []string{activeToken, second} {
		if err := denied(token); err != nil {
			return err
		}
	}
	if _, _, err := ob.PasswordLogin(ctx, "owner", "Local-Test-Initial-0042", "reset-test-old"); !errors.Is(err, identity.ErrBadCredentials) {
		return errors.New("old password not rejected")
	}
	fresh, _, err := ob.PasswordLogin(ctx, "owner", temporary, "reset-test-temporary")
	if err != nil {
		return fmt.Errorf("temporary login: %w", err)
	}
	if _, err := ob.ResolveSession(ctx, fresh); !errors.Is(err, identity.ErrMustChange) {
		return errors.New("temporary login bypassed must-change")
	}
	if _, err := store.AuthorizeConsoleMux(ctx, identity.HashSecret(fresh), []string{"agent04.owner"}); !errors.Is(err, consolemux.ErrDenied) {
		return errors.New("must-change mux authority not denied")
	}
	if err := ob.ChangePassword(ctx, "owner", temporary, "Local-Test-Changed-0043"); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	if _, err := ob.ResolveSession(ctx, fresh); err != nil {
		return fmt.Errorf("changed session: %w", err)
	}
	a, err := store.AuthorizeConsoleMux(ctx, identity.HashSecret(fresh), []string{"agent04.owner"})
	if err != nil || a.Agents["agent04.owner"].ID != "agent04" {
		return errors.New("changed session failed fresh primary authority")
	}
	for _, token := range []string{activeToken, second} {
		if err := denied(token); err != nil {
			return err
		}
	}
	return nil
}

// Observe the actual PG transaction entry, after bcrypt, on the same monotonic
// clock as native socket writes. This is earlier than commit, never later.
type muxResetStore struct {
	identity.Store
	evidence *muxOutputEvidence
}

func (s *muxResetStore) ResetPassword(ctx context.Context, userID, hash string, now time.Time) error {
	s.evidence.mu.Lock()
	s.evidence.commit = time.Now()
	s.evidence.mu.Unlock()
	return s.Store.ResetPassword(ctx, userID, hash, now)
}
