package m8

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

type contextBarrierRepo struct {
	store.Repository
	real    projectContextRepo
	read    chan struct{}
	release chan struct{}
}

func (x contextBarrierRepo) ProjectContextCandidates(ctx context.Context, a, b string, n int) ([]store.ContextCandidate, error) {
	return x.real.ProjectContextCandidates(ctx, a, b, n)
}
func (x contextBarrierRepo) WithProjectContextSnapshot(ctx context.Context, a string, c []store.ContextCandidate, fn func(*store.ContextSnapshot) error) error {
	return x.real.WithProjectContextSnapshot(ctx, a, c, func(snap *store.ContextSnapshot) error {
		if _, err := snap.Fingerprint(); err != nil {
			return err
		}
		close(x.read)
		<-x.release
		return fn(snap)
	})
}

func TestProjectContextPGSQLiteReadThenRevokeAndRevokeThenRead(t *testing.T) {
	e, g, state := r32FlowEnv(t)
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
	access, err := repo.NewAccessCtx("test", "project-context-combined")
	if err != nil {
		t.Fatal(err)
	}
	old := e.svc.Store
	pg := identity.NewPGStore(pool)
	for _, name := range []string{"zzc", "lbl"} {
		u, x := old.GetUser(ctx, name)
		if x != nil {
			t.Fatal(x)
		}
		if u.Type == "" {
			u.Type = identity.UserMember
		}
		if err = pg.CreateUser(ctx, *u); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{state.Developer, state.Reviewer} {
		a, x := old.GetAgent(ctx, id)
		if x != nil {
			t.Fatal(x)
		}
		_, err = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,credential_hash,created_at) VALUES($1,$2,$3,$4,'selfhost','active',$5,now())`, a.ID, a.OwnerLC, a.Name, a.NameLC(), a.CredentialHash)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = pg.CreateGroup(ctx, *g); err != nil {
		t.Fatal(err)
	}
	if err = pg.JoinGroup(ctx, g.Code, state.Developer, "zzc", e.hub.now()); err != nil {
		t.Fatal(err)
	}
	e.svc.Store = pg
	read, release := make(chan struct{}), make(chan struct{})
	e.hub.Repo = contextBarrierRepo{e.db, e.db, read, release}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- pcRequest(e.hub, e.zKimiCred, "scope=all", nil) }()
	select {
	case <-read:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	revoked := make(chan error, 1)
	go func() {
		_, x := pool.Exec(ctx, access, `UPDATE np_agent SET state='suspended' WHERE id=$1 /* PC_HTTP_REVOKE */`, state.Developer)
		revoked <- x
	}()
	waiting := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		rows, x := pool.Query(ctx, access, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE NOT l.granted AND a.query LIKE '%PC_HTTP_REVOKE%'`)
		if x != nil {
			close(release)
			t.Fatal(x)
		}
		var n int
		if rows.Next() {
			x = rows.Scan(&n)
		}
		rows.Close()
		if x != nil {
			close(release)
			t.Fatal(x)
		}
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	out := <-result
	if err = <-revoked; err != nil {
		t.Fatal(err)
	}
	if !waiting {
		t.Fatal("PC_HTTP_PG_LOCK_RELEASED_BEFORE_SQLITE_SNAPSHOT")
	}
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"task_id":"`+state.ID+`"`) {
		t.Fatal("PC_HTTP_READ_FIRST_SNAPSHOT_INCOMPLETE", out.Code, out.Body)
	}
	var response map[string]json.RawMessage
	if err = json.Unmarshal(out.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	// Revocation has committed; a new request must not enter projection at all.
	e.hub.Repo = e.db
	denied := pcRequest(e.hub, e.zKimiCred, "scope=all", nil)
	if denied.Code != 403 || strings.Contains(denied.Body.String(), "spec_id") {
		t.Fatal("PC_HTTP_REVOKE_FIRST_DATA_LEAK", denied.Code, denied.Body)
	}
}
