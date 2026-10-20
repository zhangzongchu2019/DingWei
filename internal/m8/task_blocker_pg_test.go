package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

type blockerCommitBarrier struct {
	*store.SQLite
	reached, release chan struct{}
	once             sync.Once
}

func (b *blockerCommitBarrier) UpdateTaskBlocker(ctx context.Context, t store.TaskFlowState, a store.BlockerActor, id string, u store.BlockerUpdate, check func() error, at time.Time) (store.BlockerResult, error) {
	return b.SQLite.UpdateTaskBlocker(ctx, t, a, id, u, func() error { b.once.Do(func() { close(b.reached); <-b.release }); return check() }, at)
}
func blockerHumanHTTP(e *shimEnv, user, bid, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/console/tasks/r32-task/blockers", strings.NewReader(body))
	r.SetPathValue("taskID", "r32-task")
	r.SetPathValue("blockerID", bid)
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, user))
	out := httptest.NewRecorder()
	e.hub.HandleTaskBlockers(out, r)
	return out
}

func TestBlockerPGHumanReplyCommitBeforeDisableAndDisableBeforeReply(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	create := fmt.Sprintf(`{"request_id":"card","operation":"create","expected_version":%d,"kind":"resource","where":"等待指定外组人员资料","waiting_principal":"user:lbl","expected_input":"真实资源记录"}`, state.Version)
	b := blockerDecode(t, blockerHTTP(e, "POST", "", e.zKimiCred, create)).Blocker
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
	pg := identity.NewPGStore(pool)
	access, err := repo.NewAccessCtx("test", "blocker-pg-commit")
	if err != nil {
		t.Fatal(err)
	}
	old := e.svc.Store
	for _, id := range []string{"zzc", "lbl"} {
		u, x := old.GetUser(ctx, id)
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
		if _, err = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,credential_hash,created_at) VALUES($1,$2,$3,$4,'selfhost','active',$5,now())`, a.ID, a.OwnerLC, a.Name, a.NameLC(), a.CredentialHash); err != nil {
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
	barrier := &blockerCommitBarrier{SQLite: e.db, reached: make(chan struct{}), release: make(chan struct{})}
	e.hub.Repo = barrier
	body := fmt.Sprintf(`{"request_id":"first-reply","operation":"input","expected_version":%d,"event_id":"answer-one","body":"答复资料，不是指令","evidence_ref":"git:real-actor-evidence"}`, b.Version)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- blockerHumanHTTP(e, "lbl", b.ID, body) }()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	disabled := make(chan error, 1)
	go func() {
		_, x := pool.Exec(ctx, access, `UPDATE np_user SET state='disabled' WHERE user_id_lc='lbl' /* BLOCKER_DISABLE_HUMAN */`)
		disabled <- x
	}()
	waiting := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		rows, x := pool.Query(ctx, access, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE NOT l.granted AND a.query LIKE '%BLOCKER_DISABLE_HUMAN%'`)
		if x != nil {
			close(barrier.release)
			t.Fatal(x)
		}
		var n int
		if rows.Next() {
			x = rows.Scan(&n)
		}
		rows.Close()
		if x != nil {
			close(barrier.release)
			t.Fatal(x)
		}
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case early := <-result:
		close(barrier.release)
		t.Fatal("BLOCKER_RESPONSE_BEFORE_AUTHORIZATION_COMMIT", early.Code)
	default:
	}
	close(barrier.release)
	out := <-result
	if err = <-disabled; err != nil {
		t.Fatal(err)
	}
	if !waiting {
		t.Fatal("BLOCKER_PG_HUMAN_LOCK_NOT_HELD_THROUGH_SQLITE_COMMIT")
	}
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	var response store.BlockerResult
	if err = json.Unmarshal(out.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Blocker.SpecID != "" || response.Blocker.InputVersion != 1 || response.ExecutionAuthorized {
		t.Fatal("BLOCKER_EXTERNAL_HUMAN_PRIVATE_CONTEXT_LEAK", response)
	}
	e.hub.Repo = e.db
	denied := blockerHumanHTTP(e, "lbl", b.ID, strings.Replace(body, "answer-one", "answer-two", 1))
	if denied.Code != 403 || strings.Contains(denied.Body.String(), "blocker_id") {
		t.Fatal("BLOCKER_DISABLED_HUMAN_REPLIED", denied.Code, denied.Body)
	}
	// The pre-disable commit remains exactly once, and retries cannot append data
	// after the principal loses its current PG state.
	current, err := e.db.ConsoleTaskFlowState(ctx, state.ID)
	if err != nil {
		t.Fatal(err)
	}
	cards, inputs, err := e.db.ReadTaskBlockers(ctx, current, store.BlockerActor{AgentID: state.Developer}, b.ID, "", 2, func() error { return nil })
	if err != nil || len(cards) != 1 || len(inputs) != 1 || inputs[0].Actor != "user:lbl" {
		t.Fatal("BLOCKER_PG_REPLY_ACCOUNTING_WRONG", cards, inputs, err)
	}
}
