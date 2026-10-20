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

func TestProjectContextPGAuthorizationLocksAndCurrentState(t *testing.T) {
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
	g := Group{Code: "pcgroup", ShortName: "Project", CreatorLC: "pcowner", LeaderAgentID: "pcactor", CreatedAt: now, UpdatedAt: now}
	if err = s.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err = s.JoinGroup(ctx, g.Code, "pcpeer", "pcother", now); err != nil {
		t.Fatal(err)
	}
	var firstDigest string
	err = s.WithProjectContextAuthorization(ctx, "pcactor", HashSecret("pcactor"), nil, func(a ContextAuthorization, check func() error) error {
		if !a.Allows(ContextClaim{"pcowner", g.Code, "pcactor", "pcpeer"}) {
			t.Fatal("PC_PG_SHARED_AUTH_MISSING")
		}
		if a.Allows(ContextClaim{"pcother", g.Code, "pcactor", "missing"}) {
			t.Fatal("PC_PG_INVALID_PAIR_ALLOWED")
		}
		if a.Names["pcother"] != "pcother" {
			t.Fatal("PC_PG_NAME_MISSING")
		}
		firstDigest = a.Digest()
		return check()
	})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err = s.WithProjectContextAuthorization(ctx, "pcactor", HashSecret("bad-token"), nil, func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("PC_CREDENTIAL_RECHECK_MISSING", err)
	}
	// Real read-first schedule: suspension must wait for the authorization callback.
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.WithProjectContextAuthorization(ctx, "pcactor", HashSecret("pcactor"), nil, func(a ContextAuthorization, check func() error) error {
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
		t.Fatal("PC_PG_AUTH_LOCK_NOT_HELD")
	}
	// Mutation-first schedule: the completed suspension prevents any private callback.
	called = false
	if err = s.WithProjectContextAuthorization(ctx, "pcactor", HashSecret("pcactor"), nil, func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("PC_PG_POST_REVOKE_CALLBACK", err)
	}
	// Reset only the disposable test DB, then remove the peer before reading.
	if _, err = pool.Exec(ctx, access, `UPDATE np_agent SET state='active' WHERE id='pcactor'`); err != nil {
		t.Fatal(err)
	}
	if err = s.LeaveGroup(ctx, g.Code, "pcpeer", "pcother"); err != nil {
		t.Fatal(err)
	}
	if err = s.WithProjectContextAuthorization(ctx, "pcactor", HashSecret("pcactor"), nil, func(a ContextAuthorization, check func() error) error {
		if a.Allows(ContextClaim{"pcowner", g.Code, "pcactor", "pcpeer"}) || a.Member(g.Code, "pcpeer") {
			t.Fatal("PC_PG_REMOVED_PEER_ALLOWED")
		}
		if a.Digest() == firstDigest {
			t.Fatal("PC_PG_AUTH_DIGEST_UNCHANGED")
		}
		return check()
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, access, `UPDATE np_agent SET credential_hash=$1 WHERE id='pcactor'`, HashSecret("rotated")); err != nil {
		t.Fatal(err)
	}
	called = false
	if err = s.WithProjectContextAuthorization(ctx, "pcactor", HashSecret("pcactor"), nil, func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("PC_PG_ROTATED_OLD_CREDENTIAL_ALLOWED", err)
	}
	if _, err = pool.Exec(ctx, access, `UPDATE np_user SET state='disabled' WHERE user_id_lc='pcowner'`); err != nil {
		t.Fatal(err)
	}
	called = false
	if err = s.WithProjectContextAuthorization(ctx, "pcactor", HashSecret("rotated"), nil, func(ContextAuthorization, func() error) error { called = true; return nil }); !errors.Is(err, ErrForbidden) || called {
		t.Fatal("PC_PG_DISABLED_OWNER_ALLOWED", err)
	}

}
