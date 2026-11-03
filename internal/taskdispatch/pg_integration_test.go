package taskdispatch_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

func usePG(t *testing.T, e *env) func(string) {
	t.Helper()
	ctx := context.Background()
	pool, err := repo.OpenPG(ctx, pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err = migrate.Migrate(ctx, pool, migrate.HubDomains()); err != nil {
		t.Fatal(err)
	}
	access, err := repo.NewAccessCtx("test", "r10-service")
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := pool.Exec(ctx, access, q); err != nil {
			t.Fatal(err)
		}
	}
	ids := identity.NewPGStore(pool)
	for _, name := range []string{"dispatcher", "holder", "next", "outsider"} {
		if err = ids.CreateUser(ctx, identity.User{UserID: name, ShortName: name, State: identity.UserActive, Type: identity.UserMember, CreatedAt: e.now}); err != nil {
			t.Fatal(err)
		}
		exec(fmt.Sprintf(`INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('agent-%s','%s','%s','%s','selfhost','active',now())`, name, name, name, name))
	}
	exec(`INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at) VALUES('group','r10','dispatcher','agent-dispatcher',now(),now());INSERT INTO np_group_member(code,agent_id,created_at) VALUES('group','agent-dispatcher',now()),('group','agent-holder',now()),('group','agent-next',now())`)
	e.s.Identity = ids
	return exec
}
func TestR10PGCurrentACLClassesAndOwnerReassignment(t *testing.T) {
	e := setup(t)
	exec := usePG(t, e)
	ctx := context.Background()
	for _, name := range []string{"dispatcher_not_qualified", "target_not_member", "target_user_inactive", "target_agent_inactive"} {
		exec(`UPDATE np_user SET state='active';UPDATE np_agent SET state='active'`)
		r := e.r
		r.OperationID = "pg_" + name + "_operation_1"
		actor := td.Actor{UserLC: "dispatcher"}
		switch name {
		case "dispatcher_not_qualified":
			actor.UserLC = "outsider"
		case "target_not_member":
			r.HolderAgentID = "agent-outsider"
		case "target_user_inactive":
			exec(`UPDATE np_user SET state='disabled' WHERE user_id_lc='holder'`)
		case "target_agent_inactive":
			exec(`UPDATE np_agent SET state='suspended' WHERE id='agent-holder'`)
		}
		if _, err := e.s.Execute(ctx, actor, r); err == nil {
			t.Fatalf("R10_PG_ACL_GUARD_BYPASSED case=%s", name)
		}
		if l, err := e.db.TaskDispatchLock(ctx, r.Key); err != nil || l != nil {
			t.Fatal("R10_PG_DENIED_GRANTED", name, l, err)
		}
	}
	exec(`UPDATE np_user SET state='active';UPDATE np_agent SET state='active'`)
	l := e.grant(t)
	reg := e.holder(l, "register_pr")
	reg.PullRequestID = 7
	r, err := e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, reg)
	if err != nil {
		t.Fatal(err)
	}
	l = r.Lock
	exec(`UPDATE np_agent SET owner_lc='next' WHERE id='agent-holder'`)
	if _, err = e.s.Inspect(ctx, td.Actor{AgentID: "agent-holder"}, l.Key, ""); err == nil {
		t.Fatal("R10_OWNER_CHANGE_INHERITED_LEASE")
	}
	e.now = e.now.Add(time.Minute)
	if err = e.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := e.db.TaskDispatchLock(ctx, l.Key)
	if err != nil || current.ComparisonState != "comparison_unknown" || current.PRDimension != "comparison_unknown" || current.HolderOwnerLC != "holder" {
		t.Fatal("R10_OWNER_CHANGE_NOT_PERSISTED_UNKNOWN", current, err)
	}
}
func TestR10PGSlowRepositoryCannotDelayIndependentRepository(t *testing.T) {
	e := setup(t)
	usePG(t, e)
	ctx := context.Background()
	e.r.MonitorRef = "slow"
	slow := e.grant(t)
	e.r.Key.Repository = "2"
	e.r.MonitorRef = "main"
	e.r.OperationID = "grant_independent_repo_1"
	fast := e.grant(t)
	gate := make(chan struct{})
	e.p.mu.Lock()
	e.p.waitRef = map[string]chan struct{}{"slow": gate}
	e.p.mu.Unlock()
	e.now = e.now.Add(time.Minute)
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- e.s.Tick(ctx) }()
	checked := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		l, err := e.db.TaskDispatchLock(ctx, fast.Key)
		if err != nil {
			t.Fatal(err)
		}
		if l.LastCheckedAt.Equal(e.now) {
			checked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	err := <-done
	close(gate)
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("R10_CROSS_REPOSITORY_HEAD_OF_LINE_BLOCKING")
	}
	if elapsed := time.Since(started); elapsed > 21*time.Second {
		t.Fatal("R10_MONITOR_15_PLUS_5_BUDGET_EXCEEDED", elapsed)
	}
	l, err := e.db.TaskDispatchLock(ctx, slow.Key)
	if err != nil || l.ComparisonState != "comparison_unknown" {
		t.Fatal("R10_TIMEOUT_FALSE_UNCHANGED", l, err)
	}
}

type closeOnApply struct {
	td.Repository
	db     *store.SQLite
	called bool
}

func (r *closeOnApply) ApplyTaskDispatch(ctx context.Context, m td.Mutation, clock func() time.Time, check func() error) (td.Result, error) {
	r.called = true
	r.db.Close()
	return r.Repository.ApplyTaskDispatch(ctx, m, clock, check)
}
func TestR10PGServiceSQLiteFailureAuditsOutsideAbortedCallback(t *testing.T) {
	e := setup(t)
	usePG(t, e)
	ctx := context.Background()
	// Separate SQLite file permits proving the durable state after intentionally
	// closing the first connection at the write boundary inside the PG callback.
	path := filepath.Join(t.TempDir(), "write-failure.db")
	db, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Copy only the existing R5 principals/bindings through their public APIs.
	for _, owner := range []string{"dispatcher", "holder"} {
		p, err := e.db.GetConsolePrincipal(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.UpsertConsolePrincipal(ctx, *p); err != nil {
			t.Fatal(err)
		}
		b, err := e.db.SpecReadBinding(ctx, owner, "github", "github.com")
		if err != nil {
			t.Fatal(err)
		}
		if err = db.SetSpecReadBinding(ctx, "admin", *b); err != nil {
			t.Fatal(err)
		}
	}
	failing := &closeOnApply{Repository: db, db: db}
	e.s.Repo = failing
	e.s.Reader.Bindings = db
	auditError := error(nil)
	e.s.AuditFailure = func(ctx context.Context, id, reason string) {
		reopened, err := store.OpenSQLite(path)
		if err != nil {
			auditError = err
			return
		}
		defer reopened.Close()
		auditError = reopened.WriteAudit(ctx, "system", "r10_operation_failed."+reason, id)
	}
	if _, err = e.s.Execute(ctx, td.Actor{UserLC: "dispatcher"}, e.r); err == nil || !failing.called {
		t.Fatal("R10_PG_WRITE_FAILURE_REPORTED_GRANT", err, failing.called)
	}
	reopened, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if l, err := reopened.TaskDispatchLock(ctx, e.r.Key); err != nil || l != nil {
		t.Fatal("R10_PG_WRITE_FAILURE_PERSISTED_GRANT", l, err)
	}
	audits, err := reopened.ListRecentAudit(ctx, "r10_operation_failed", 10)
	if err != nil || auditError != nil || len(audits) != 1 {
		t.Fatal("R10_PG_SERVICE_FAILURE_AUDIT_MISSING", err, auditError, len(audits))
	}
}

func TestR10PGExpiredInactiveHolderCanBeExplicitlyReassignedWithoutClaimingStopped(t *testing.T) {
	e := setup(t)
	exec := usePG(t, e)
	ctx := context.Background()
	old := e.grant(t)
	record := fmt.Sprintf("# Recovery\n%s\n%s\nunknown\n", old.TaskID, old.CommitSHA)
	e.p.files[c1][dir+"/handoff.md"] = record
	exec(`UPDATE np_agent SET state='suspended' WHERE id='agent-holder'`)
	e.now = old.ExpiresAt
	next := e.r
	next.OperationID = "expired_inactive_reassign_1"
	next.HolderAgentID = "agent-next"
	next.Generation = old.Generation
	next.Handoff = td.Handoff{Commit: c1, Path: dir + "/handoff.md", BlobSHA: blob(record), ExecutionCommit: c1, TaskID: old.TaskID, RecoveryState: "unknown"}
	result, err := e.s.Execute(ctx, td.Actor{UserLC: "dispatcher"}, next)
	if err != nil || result.Lock.HolderAgentID != "agent-next" || result.Lock.Generation <= old.Generation || result.Lock.Handoff.RecoveryState != "unknown" {
		t.Fatal("R10_EXPLICIT_RECOVERY_BLOCKED_OR_FALSE_STOPPED", result, err)
	}
}
