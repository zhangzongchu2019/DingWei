package m8

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClarificationPGRevocationAndBindingWriteSerialization(t *testing.T) {
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
	access, err := repo.NewAccessCtx("test", "pilot-question-order")
	if err != nil {
		t.Fatal(err)
	}
	pg := identity.NewPGStore(pool)
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
	for _, u := range []string{"zzc", "lbl"} {
		if _, err = pool.Exec(ctx, access, `INSERT INTO np_identity(provider,issuer,subject,user_id_lc) VALUES('feishu','question-bot',$1,$2)`, "ou_"+u, u); err != nil {
			t.Fatal(err)
		}
	}
	e.svc.Store = pg
	e.hub.SpecClarificationEnabled = true
	c, _ := e.db.QuestionAuthorizationContext(ctx, "", state.ID, "")
	c, _ = e.db.QuestionAuthorizationContext(ctx, c.SpecID, "", "")
	if _, err = e.db.SetQuestionPolicy(ctx, c, store.BlockerActor{UserID: "zzc"}, store.QuestionUpdate{RequestID: "policy", PMUser: "lbl", LeaderUser: "zzc"}, func() error { return nil }, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	q := questionHTTPCreate(t, e, state)
	c, _ = e.db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	p, _ := e.db.ClarificationPolicy(ctx, q.SpecID)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- e.hub.withClarificationIdentity(ctx, e.db, c, p, blockerCredential{actor: store.BlockerActor{UserID: "lbl"}}, nil, func(_ identity.ContextAuthorization, check func() error) error {
			close(held)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, x := e.db.AppendClarification(ctx, c, store.BlockerActor{UserID: "lbl"}, q.ID, store.ClarificationUpdate{RequestID: "pg-held", Kind: "explanation", EntryRefs: []string{"A1"}, Body: "commits before identity removal"}, true, check, e.hub.now())
			return x
		})
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	removed := make(chan error, 1)
	go func() {
		_, x := pool.Exec(ctx, access, `DELETE FROM np_identity WHERE provider='feishu' AND issuer='question-bot' AND subject='ou_lbl'`)
		removed <- x
	}()
	waitLocked := func() bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			rows, x := pool.Query(ctx, access, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%DELETE FROM np_identity%'`)
			if x != nil {
				return false
			}
			var n int
			if rows.Next() {
				x = rows.Scan(&n)
			}
			rows.Close()
			if x == nil && n > 0 {
				return true
			}
			time.Sleep(time.Millisecond)
		}
		return false
	}
	waiting := waitLocked()
	close(release)
	if x := <-done; x != nil {
		t.Fatal(x)
	}
	if x := <-removed; x != nil {
		t.Fatal(x)
	}
	if !waiting {
		t.Fatal("CLARIFICATION_PG_BINDING_UNLOCKED_BEFORE_SQLITE_COMMIT")
	}
	// Revoke first while a second request is queued on the PG user row. It
	// must return a denial after the row update commits, with no second floor.
	updated, commit := make(chan struct{}), make(chan struct{})
	revoked := make(chan error, 1)
	go func() {
		revoked <- pool.InTx(ctx, access, nil, func(tx repo.Tx) error {
			if _, x := tx.Exec(ctx, `UPDATE np_user SET state='disabled' WHERE user_id_lc='lbl'`); x != nil {
				return x
			}
			close(updated)
			select {
			case <-commit:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-updated:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- clarificationHTTP(e, "POST", q.ID, "", "lbl", "", clarificationHTTPBody("revoked-first", "explanation"), "", false)
	}()
	deadline := time.Now().Add(3 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		rows, x := pool.Query(ctx, access, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%np_user%'`)
		if x != nil {
			break
		}
		var n int
		if rows.Next() {
			x = rows.Scan(&n)
		}
		rows.Close()
		if x == nil && n > 0 {
			blocked = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(commit)
	if x := <-revoked; x != nil {
		t.Fatal(x)
	}
	out := <-result
	if !blocked || out.Code != 403 {
		t.Fatal("CLARIFICATION_PG_REVOKE_FIRST_WRITTEN", blocked, out.Code, out.Body)
	}
	page := clarificationHTTP(e, "GET", q.ID, "", "zzc", "", "", "", false)
	var snapshot struct {
		Posts []store.ClarificationPost `json:"posts"`
	}
	if x := json.Unmarshal(page.Body.Bytes(), &snapshot); x != nil || page.Code != 200 || len(snapshot.Posts) != 1 {
		t.Fatal("CLARIFICATION_PG_REVOKE_PARTIAL_SECOND_POST", page.Code, page.Body, x)
	}
}
