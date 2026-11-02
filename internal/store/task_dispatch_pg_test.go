package store

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"testing"
	"time"
)

func TestR10PGFenceSQLiteFailureDoesNotGrant(t *testing.T) {
	db, m, now := r10DB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, e := repo.OpenPG(ctx, pgtest.DSN(t))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if _, e = migrate.Migrate(ctx, pool, migrate.HubDomains()); e != nil {
		t.Fatal(e)
	}
	access, e := repo.NewAccessCtx("test", "r10-fault")
	if e != nil {
		t.Fatal(e)
	}
	identities := identity.NewPGStore(pool)
	for _, name := range []string{"dispatcher", "holder"} {
		if e = identities.CreateUser(ctx, identity.User{UserID: name, ShortName: name, Type: identity.UserMember, State: identity.UserActive, CreatedAt: now}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('agent-1','holder','holder','holder','selfhost','active',now()),('dispatch-agent','dispatcher','dispatch','dispatch','selfhost','active',now());INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at) VALUES('group','r10','dispatcher','dispatch-agent',now(),now());INSERT INTO np_group_member(code,agent_id,created_at) VALUES('group','agent-1',now()),('group','dispatch-agent',now())`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.db.Exec(`CREATE TRIGGER r10_fail_event BEFORE INSERT ON r10_task_lock_event BEGIN SELECT RAISE(ABORT,'r10 write failure'); END`); e != nil {
		t.Fatal(e)
	}
	callback := false
	e = identities.WithTaskAuthorization(ctx, "dispatcher", "group", []string{"agent-1"}, func(_ []identity.Agent, check func() error) error {
		callback = true
		_, err := db.ApplyTaskDispatch(ctx, m, func() time.Time { return now }, check)
		return err
	})
	if !callback || e == nil {
		t.Fatal("R10_PG_SQLITE_FAILURE_REPORTED_SUCCESS", callback, e)
	}
	if l, err := db.TaskDispatchLock(ctx, m.Key); err != nil || l != nil {
		t.Fatal("R10_PG_SQLITE_FAILURE_GRANTED", l, err)
	}
	// The service's separate failure audit uses the existing audit sink after the
	// failed callback released both locks. It cannot be part of the aborted tx.
	if err := db.WriteAudit(ctx, "system", "r10_operation_failed.r10_unavailable", m.OperationID); err != nil {
		t.Fatal(err)
	}
	audits, err := db.ListRecentAudit(ctx, "r10_operation_failed", 10)
	if err != nil || len(audits) != 1 {
		t.Fatal("R10_PG_SQLITE_FAILURE_NOT_AUDITED", len(audits), err)
	}
}
