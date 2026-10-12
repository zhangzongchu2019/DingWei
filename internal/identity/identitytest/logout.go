package identitytest

import (
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"testing"
	"time"
)

func logoutCurrentSession(t *testing.T, s identity.Store) {
	mustUser(t, s, "Alice", "al", identity.UserActive)
	mustUser(t, s, "Bob", "bo", identity.UserActive)
	for _, v := range []struct{ hash, user string }{{"test-session-a", "alice"}, {"test-session-b", "alice"}, {"test-session-c", "bob"}} {
		if err := s.CreateSession(ctx, identity.Session{TokenHash: v.hash, UserIDLC: v.user, ExpiresAt: t0.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RevokeSession(ctx, "test-session-a", t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveSession(ctx, "test-session-a", t0); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("revoked session was reusable")
	}
	for _, h := range []string{"test-session-b", "test-session-c"} {
		if _, _, err := s.ResolveSession(ctx, h, t0); err != nil {
			t.Fatal("unrelated session revoked", err)
		}
	}
	if err := s.RevokeSession(ctx, "test-session-a", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(ctx, "missing", t0); err != nil {
		t.Fatal(err)
	}
}
