package identity

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"testing"
	"time"
)

func TestR6PGCurrentAgentOwnerAndPersonalContactLock(t *testing.T) {
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
	access, e := repo.NewAccessCtx("test", "r6-owned-contact")
	if e != nil {
		t.Fatal(e)
	}
	s := NewPGStore(pool)
	for _, name := range []string{"r6owner", "r6peer"} {
		if e = s.CreateUser(ctx, User{UserID: name, ShortName: name, State: UserActive, Type: UserMember, CreatedAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('r6agent','r6owner','R6','r6','selfhost','active',now())`); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, access, `INSERT INTO np_identity(provider,issuer,subject,user_id_lc) VALUES('feishu','test-bot','test-reviewer','r6owner')`); e != nil {
		t.Fatal(e)
	}
	called := false
	if e = s.WithCoordinateReviewAuthorization(ctx, "r6peer", "r6agent", func(Agent, Identity, func() error) error { called = true; return nil }); e == nil || called {
		t.Fatal("R6_PG_FOREIGN_OWNER")
	}
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.WithCoordinateReviewAuthorization(ctx, "r6owner", "r6agent", func(a Agent, b Identity, check func() error) error {
			if a.ID != "r6agent" || b.Subject != "test-reviewer" {
				return ErrForbidden
			}
			close(locked)
			<-release
			return check()
		})
	}()
	<-locked
	deleted := make(chan error, 1)
	go func() {
		_, x := pool.Exec(ctx, access, `DELETE /*R6_CONTACT_DELETE*/ FROM np_identity WHERE user_id_lc='r6owner'`)
		deleted <- x
	}()
	observed := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, x := pool.Query(ctx, access, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'DELETE /*R6_CONTACT_DELETE*/%'`)
		if x != nil {
			t.Fatal(x)
		}
		var n int
		if rows.Next() {
			x = rows.Scan(&n)
		}
		rows.Close()
		if x != nil {
			t.Fatal(x)
		}
		if n > 0 {
			observed = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if e = <-deleted; e != nil {
		t.Fatal(e)
	}
	if !observed {
		t.Fatal("R6_PG_CONTACT_NOT_FENCED_THROUGH_SEND")
	}
	if e = s.WithCoordinateReviewAuthorization(ctx, "r6owner", "r6agent", func(a Agent, b Identity, check func() error) error {
		if b.Subject != "" {
			t.Fatal("R6_PG_CONTACT_CACHE")
		}
		return check()
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, access, `UPDATE np_agent SET state='suspended' WHERE id='r6agent'`); e != nil {
		t.Fatal(e)
	}
	called = false
	if e = s.WithCoordinateReviewAuthorization(ctx, "r6owner", "r6agent", func(Agent, Identity, func() error) error { called = true; return nil }); e == nil || called {
		t.Fatal("R6_PG_REVOKE_FIRST_ALLOWED")
	}
}
