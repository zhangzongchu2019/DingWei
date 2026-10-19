package identity

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"testing"
	"time"
)

func TestR32TaskAuthorizationPGMembershipLocksAndOwners(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := pgtest.Pool(t)
	if _, err := migrate.Migrate(ctx, pool, migrate.HubDomains()); err != nil {
		t.Fatal(err)
	}
	access, err := repo.NewAccessCtx("test", "r32-task-auth")
	if err != nil {
		t.Fatal(err)
	}
	s := NewPGStore(pool)
	for _, name := range []string{"owner", "peer"} {
		if err = s.CreateUser(ctx, User{UserID: name, ShortName: name, State: UserActive, Type: UserMember, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string) {
		t.Helper()
		if _, err := pool.Exec(ctx, access, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('dev','owner','Dev','dev','selfhost','active',now()),('review','owner','Review','review','selfhost','active',now()),('peer-review','peer','Peer','peer','selfhost','active',now());INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at) VALUES('r32-group','r32','owner','dev',now(),now());INSERT INTO np_group_member(code,agent_id,created_at) VALUES('r32-group','dev',now()),('r32-group','review',now()),('r32-group','peer-review',now())`)
	for _, pair := range [][]string{{"dev", "review"}, {"dev", "peer-review"}} {
		if err = s.WithTaskAuthorization(ctx, "owner", "r32-group", pair, func(a []Agent, check func() error) error {
			if len(a) != 2 {
				t.Fatal("bad pair")
			}
			return check()
		}); err != nil {
			t.Fatalf("VALID_SAME_OR_CROSS_OWNER_PAIR_REJECTED: %v", err)
		}
	}
	if err = s.WithTaskAuthorization(ctx, "owner", "missing", []string{"dev", "review"}, func([]Agent, func() error) error { t.Fatal("MISSING_GROUP_AUTHORIZED"); return nil }); err == nil {
		t.Fatal("MISSING_GROUP_AUTHORIZED")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- s.WithTaskAuthorization(ctx, "owner", "r32-group", []string{"dev", "review"}, func(_ []Agent, check func() error) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return check()
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	left := make(chan error, 1)
	go func() { left <- s.LeaveGroup(ctx, "r32-group", "review", "owner") }()
	observed := false
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		row, e := pool.QueryRow(ctx, access, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%np_group%')`)
		if e != nil {
			t.Fatal(e)
		}
		if e = row.Scan(&observed); e != nil {
			t.Fatal(e)
		}
		if observed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if err = <-left; err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("MEMBERSHIP_LOCK_NOT_HELD_THROUGH_TASK")
	}
	called := false
	err = s.WithTaskAuthorization(ctx, "owner", "r32-group", []string{"dev", "review"}, func([]Agent, func() error) error { called = true; return nil })
	if !errors.Is(err, ErrForbidden) || called {
		t.Fatalf("LEFT_GROUP_TASK_ACCESS: called=%v err=%v", called, err)
	}
	exec(`UPDATE np_user SET state='disabled' WHERE user_id_lc='peer'`)
	if err = s.WithTaskAuthorization(ctx, "owner", "r32-group", []string{"dev", "peer-review"}, func([]Agent, func() error) error { t.Fatal("DISABLED_OWNER_TASK_ACCESS"); return nil }); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled owner: %v", err)
	}
}
