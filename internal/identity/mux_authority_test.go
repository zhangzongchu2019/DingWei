package identity_test

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"testing"
	"time"
)

func TestPGMuxFreshAuthorityAndOwnedBatch(t *testing.T) {
	ctx := context.Background()
	pool, access := pgSchemaFactory(t)(t)
	for _, q := range identitytest.PGTestSchema() {
		if _, err := pool.Exec(ctx, access, q); err != nil {
			t.Fatal(err)
		}
	}
	s := identity.NewPGStore(pool)
	for _, name := range []string{"owner", "other"} {
		if err := s.CreateUser(ctx, identity.User{UserID: name, ShortName: name, Type: identity.UserMember, State: identity.UserActive, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES($1,$1,'agent','agent','selfhost','active',clock_timestamp())`, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateSession(ctx, identity.Session{TokenHash: "test-hash", UserIDLC: "owner", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	a, err := s.AuthorizeConsoleMux(ctx, "test-hash", []string{"agent.owner", "agent.other"})
	if err != nil || a.Principal != "owner" || len(a.Agents) != 1 || a.Agents["agent.owner"].ID != "owner" {
		t.Fatal("owned batch boundary", err)
	}
	if _, err := pool.Exec(ctx, access, `UPDATE np_agent SET state='suspended' WHERE id='owner'`); err != nil {
		t.Fatal(err)
	}
	a, err = s.AuthorizeConsoleMux(ctx, "test-hash", []string{"agent.owner"})
	if err != nil || len(a.Agents) != 0 {
		t.Fatal("suspended Agent regranted", err)
	}
	if err := s.SetUserState(ctx, "owner", identity.UserDisabled, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeConsoleMux(ctx, "test-hash", nil); !errors.Is(err, consolemux.ErrDenied) {
		t.Fatal("disabled principal accepted", err)
	}
	if err := s.SetUserState(ctx, "owner", identity.UserActive, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, identity.Session{TokenHash: "current", UserIDLC: "owner", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, identity.Session{TokenHash: "second", UserIDLC: "owner", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(ctx, "current", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeConsoleMux(ctx, "current", nil); !errors.Is(err, consolemux.ErrDenied) {
		t.Fatal("old cookie authority reused", err)
	}
	if _, err := s.AuthorizeConsoleMux(ctx, "second", nil); err != nil {
		t.Fatal("another login accidentally revoked", err)
	}
	if _, err := pool.Exec(ctx, access, `UPDATE np_session SET expires_at=clock_timestamp()-interval '1 millisecond' WHERE token_hash='second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeConsoleMux(ctx, "second", nil); !errors.Is(err, consolemux.ErrDenied) {
		t.Fatal("expiry accepted", err)
	}
}
