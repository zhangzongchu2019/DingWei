package identity_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

type adminTxEnv struct {
	ctx  context.Context
	pool *repo.Pool
	tc   repo.AccessCtx
	st   *identity.PGStore
	at   time.Time
}

func newAdminTxEnv(t *testing.T) adminTxEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	t.Cleanup(cancel)
	pool, tc := pgSchemaFactory(t)(t)
	exec := func(q string) {
		t.Helper()
		if _, err := pool.Exec(ctx, tc, q); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range identitytest.PGTestSchema() {
		exec(ddl)
	}
	exec(`CREATE TABLE g4_node(id text PRIMARY KEY,name text NOT NULL); CREATE TABLE g4_hosted_seat(agent_id text PRIMARY KEY,node_id text); INSERT INTO g4_node VALUES('node','node-in-tx'); INSERT INTO g4_hosted_seat VALUES('ag-node','node')`)
	st := identity.NewPGStore(pool)
	at := time.Now().UTC()
	if err := st.CreateUser(ctx, identity.User{UserID: "admin", ShortName: "ad", Type: identity.UserPlatformAdmin, State: identity.UserActive, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('ag-node','admin','Node','node','hosted','active',now())`)
	return adminTxEnv{ctx, pool, tc, st, at}
}
func (e adminTxEnv) number(t *testing.T, q string, args ...any) int {
	t.Helper()
	row, err := e.pool.QueryRow(e.ctx, e.tc, q, args...)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func (e adminTxEnv) audits(t *testing.T) int {
	return e.number(t, `SELECT count(*) FROM audit_log WHERE action='agent.admin_list'`)
}

type adminListResult struct {
	page identity.AdminAgentPage
	err  error
}

func (e adminTxEnv) list() <-chan adminListResult {
	out := make(chan adminListResult, 1)
	go func() { p, err := e.st.ListAdminAgents(e.ctx, "admin", "", 100, e.at); out <- adminListResult{p, err} }()
	return out
}
func waitAdminCondition(t *testing.T, marker string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal(marker)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestR31AdminAgentPGNodeProjectionFailureRollsBackAudit(t *testing.T) {
	e := newAdminTxEnv(t)
	if _, err := e.pool.Exec(e.ctx, e.tc, `ALTER TABLE g4_node RENAME COLUMN name TO missing_name`); err != nil {
		t.Fatal(err)
	}
	p, err := e.st.ListAdminAgents(e.ctx, "admin", "", 100, e.at)
	if err == nil || len(p.Agents) != 0 || e.audits(t) != 0 {
		t.Fatalf("ADMIN_NODE_PROJECTION_FAILURE_NOT_ATOMIC: %+v %v", p, err)
	}
}

func TestR31AdminAgentPGConcurrentRevokeListFirst(t *testing.T) {
	e := newAdminTxEnv(t)
	held := make(chan error, 1)
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	blocker := make(chan error, 1)
	go func() {
		blocker <- e.pool.InTx(e.ctx, e.tc, nil, func(tx repo.Tx) error {
			_, err := tx.Exec(e.ctx, `LOCK TABLE g4_node IN ACCESS EXCLUSIVE MODE`)
			held <- err
			if err != nil {
				return err
			}
			select {
			case <-release:
				return nil
			case <-e.ctx.Done():
				return e.ctx.Err()
			}
		})
	}()
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	out := e.list()
	nodePID := 0
	waitAdminCondition(t, "ADMIN_NODE_PROJECTION_DID_NOT_REACH_DATABASE", func() bool {
		nodePID = e.number(t, `SELECT COALESCE(max(pid),0) FROM pg_locks WHERE NOT granted AND relation=(SELECT oid FROM pg_class WHERE relname='g4_node' AND relnamespace=current_schema()::regnamespace)`)
		return nodePID != 0
	})
	if e.audits(t) != 0 {
		t.Fatal("ADMIN_NODE_AUDIT_COMMITTED_BEFORE_PROJECTION")
	}
	revoked := make(chan error, 1)
	go func() { revoked <- e.st.SetUserType(e.ctx, "admin", identity.UserMember) }()
	waitAdminCondition(t, "ADMIN_NODE_PROJECTION_TX_SPLIT", func() bool {
		return e.number(t, `SELECT count(*) FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE 'UPDATE np_user SET type%'`, nodePID) == 1
	})
	select {
	case err := <-revoked:
		t.Fatalf("ADMIN_NODE_ROLE_LOCK_RELEASED_EARLY: %v", err)
	default:
	}
	unlock()
	if err := <-blocker; err != nil {
		t.Fatal(err)
	}
	result := <-out
	if result.err != nil || len(result.page.Agents) != 1 || result.page.Agents[0].HostedNodeName != "node-in-tx" || e.audits(t) != 1 {
		t.Fatalf("ADMIN_NODE_LIST_FIRST_NOT_ATOMIC: %+v %v", result.page, result.err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	p, err := e.st.ListAdminAgents(e.ctx, "admin", "", 100, e.at)
	if !errors.Is(err, identity.ErrForbidden) || len(p.Agents) != 0 || e.audits(t) != 1 {
		t.Fatal("revoked admin read after committed demotion")
	}
}

func TestR31AdminAgentPGConcurrentRevokeFirst(t *testing.T) {
	e := newAdminTxEnv(t)
	held := make(chan int, 1)
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	changed := make(chan error, 1)
	go func() {
		changed <- e.pool.InTx(e.ctx, e.tc, nil, func(tx repo.Tx) error {
			if _, err := tx.Exec(e.ctx, `UPDATE np_user SET type='member' WHERE user_id_lc='admin'`); err != nil {
				held <- 0
				return err
			}
			var pid int
			if err := tx.QueryRow(e.ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				held <- 0
				return err
			}
			held <- pid
			select {
			case <-release:
				return nil
			case <-e.ctx.Done():
				return e.ctx.Err()
			}
		})
	}()
	pid := <-held
	if pid == 0 {
		t.Fatal("could not lock revocation")
	}
	out := e.list()
	waitAdminCondition(t, "ADMIN_NODE_REVOKE_FIRST_LOCK_MISSING", func() bool {
		return e.number(t, `SELECT count(*) FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE 'SELECT state,type FROM np_user%'`, pid) == 1
	})
	if e.audits(t) != 0 {
		t.Fatal("ADMIN_NODE_REVOKE_FIRST_AUDIT_LEAK")
	}
	unlock()
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	result := <-out
	if !errors.Is(result.err, identity.ErrForbidden) || len(result.page.Agents) != 0 || e.audits(t) != 0 {
		t.Fatalf("ADMIN_NODE_REVOKE_FIRST_RETURNED_PAGE: %+v %v", result.page, result.err)
	}
}
