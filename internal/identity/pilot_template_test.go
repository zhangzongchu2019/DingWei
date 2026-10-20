package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestPilotTemplatePGCurrentPrincipalAndRoleLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := repo.OpenPG(ctx, pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = migrate.Migrate(ctx, pool, migrate.HubDomains()); err != nil {
		t.Fatal(err)
	}
	access, err := repo.NewAccessCtx("test", "pc-context-lock")
	if err != nil {
		t.Fatal(err)
	}
	s := NewPGStore(pool)
	now := time.Now()
	for _, u := range []string{"pcowner", "pcother"} {
		if err = s.CreateUser(ctx, User{UserID: u, ShortName: u, State: UserActive, Type: UserMember, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []struct{ id, owner, name string }{{"pcactor", "pcowner", "Actor"}, {"pcpeer", "pcother", "Peer"}} {
		_, err = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,credential_hash,created_at) VALUES($1,$2,$3,$4,'selfhost','active',$5,now())`, a.id, a.owner, a.name, Lower(a.name), HashSecret(a.id))
		if err != nil {
			t.Fatal(err)
		}
	}

	var firstDigest string
	err = s.WithTemplateAuthorization(ctx, "pcactor", HashSecret("pcactor"), "", []string{"pcowner", "pcother"}, []string{"pcactor", "pcpeer"}, "draft", func(a ContextAuthorization, check func() error) error {
		if !a.Active("pcactor") || !a.Active("pcpeer") || !a.Users["pcother"] || a.HumanTypes["pcowner"] != "member" {
			t.Fatal("TEMPLATE_PG_CURRENT_PRINCIPALS_NOT_BOUND")
		}

		firstDigest = a.Digest()
		return check()
	})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err = s.WithTemplateAuthorization(ctx, "pcactor", HashSecret("bad-token"), "", []string{"pcowner"}, nil, "draft", func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("TEMPLATE_PG_CREDENTIAL_RECHECK_MISSING", err)
	}
	// Real read-first schedule: suspension must wait for the authorization callback.
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.WithTemplateAuthorization(ctx, "pcactor", HashSecret("pcactor"), "", []string{"pcowner", "pcother"}, []string{"pcactor", "pcpeer"}, "draft", func(a ContextAuthorization, check func() error) error {
			if a.Digest() != firstDigest {
				return ErrConflict
			}
			close(locked)
			<-release
			return check()
		})
	}()
	<-locked
	mutated := make(chan error, 1)
	go func() {
		_, e := pool.Exec(ctx, access, `UPDATE np_agent SET state='suspended' WHERE id='pcactor' /* PC_CONTEXT_REVOKE */`)
		mutated <- e
	}()
	waiting := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		rows, e := pool.Query(ctx, access, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE NOT l.granted AND a.query LIKE '%PC_CONTEXT_REVOKE%'`)
		if e != nil {
			close(release)
			t.Fatal(e)
		}
		var n int
		if rows.Next() {
			e = rows.Scan(&n)
		}
		rows.Close()
		if e != nil {
			close(release)
			t.Fatal(e)
		}
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = <-mutated; err != nil {
		t.Fatal(err)
	}
	if !waiting {
		t.Fatal("TEMPLATE_PG_AUTH_LOCK_NOT_HELD")
	}
	// Mutation-first schedule: the completed suspension prevents any private callback.
	called = false
	if err = s.WithTemplateAuthorization(ctx, "pcactor", HashSecret("pcactor"), "", []string{"pcowner", "pcother"}, []string{"pcactor", "pcpeer"}, "draft", func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("TEMPLATE_PG_POST_REVOKE_CALLBACK", err)
	}
	if _, err = pool.Exec(ctx, access, `UPDATE np_agent SET state='active' WHERE id='pcactor'`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, access, `UPDATE np_agent SET credential_hash=$1 WHERE id='pcactor'`, HashSecret("rotated")); err != nil {
		t.Fatal(err)
	}
	called = false
	if err = s.WithTemplateAuthorization(ctx, "pcactor", HashSecret("pcactor"), "", []string{"pcowner", "pcother"}, []string{"pcactor", "pcpeer"}, "draft", func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("TEMPLATE_PG_ROTATED_OLD_CREDENTIAL_ALLOWED", err)
	}
	if _, err = pool.Exec(ctx, access, `UPDATE np_user SET state='disabled' WHERE user_id_lc='pcowner'`); err != nil {
		t.Fatal(err)
	}
	called = false
	if err = s.WithTemplateAuthorization(ctx, "pcactor", HashSecret("rotated"), "", []string{"pcowner"}, nil, "draft", func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("TEMPLATE_PG_DISABLED_OWNER_ALLOWED", err)
	}

}
