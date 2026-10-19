package identity

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"testing"
	"time"
)

func TestR8OwnedAgentPGAuthorizationAndRevocationLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, e := repo.OpenPG(ctx, pgtest.DSN(t))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if _, e = migrate.Migrate(ctx, pool, migrate.HubDomains()); e != nil {
		t.Fatal(e)
	}
	access, e := repo.NewAccessCtx("test", "r8-owned-auth")
	if e != nil {
		t.Fatal(e)
	}
	s := NewPGStore(pool)
	for _, name := range []string{"r8owner", "r8peer"} {
		if e = s.CreateUser(ctx, User{UserID: name, ShortName: name, State: UserActive, Type: UserMember, CreatedAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('r8agent','r8owner','R8','r8','selfhost','active',now())`); e != nil {
		t.Fatal(e)
	}
	called := false
	if e = s.WithOwnedAgentAuthorization(ctx, "r8peer", "r8agent", func(Agent, func() error) error { called = true; return nil }); e == nil || called {
		t.Fatal("R8_CROSS_OWNER_PG_AUTHORIZED")
	}
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.WithOwnedAgentAuthorization(ctx, "r8owner", "r8agent", func(a Agent, check func() error) error {
			if a.ID != "r8agent" {
				return ErrForbidden
			}
			close(locked)
			<-release
			return check()
		})
	}()
	<-locked
	revoked := make(chan error, 1)
	go func() {
		_, e := pool.Exec(ctx, access, `UPDATE np_agent SET state='suspended' WHERE id='r8agent'`)
		revoked <- e
	}()
	// Observe a real waiting lock, rather than inferring a block from a sleep.
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		rows, e := pool.Query(ctx, access, `SELECT count(*) FROM pg_locks WHERE NOT granted`)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if rows.Next() {
			e = rows.Scan(&n)
		}
		rows.Close()
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		close(release)
		t.Fatal("R8_PG_SHARED_LOCK_MISSING")
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if e = <-revoked; e != nil {
		t.Fatal(e)
	}
	called = false
	if e = s.WithOwnedAgentAuthorization(ctx, "r8owner", "r8agent", func(Agent, func() error) error { called = true; return nil }); e == nil || called {
		t.Fatal("R8_SUSPENDED_AGENT_PG_AUTHORIZED")
	}
}
