package m8

import (
	"context"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPilotQuestionPGActualSendReplyOrderingAndCurrentBinding(t *testing.T) {
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
	e.hub.PublicOrigin = "https://console.fixture.invalid"
	c, _ := e.db.QuestionAuthorizationContext(ctx, "", state.ID, "")
	c, _ = e.db.QuestionAuthorizationContext(ctx, c.SpecID, "", "")
	if _, err = e.db.SetQuestionPolicy(ctx, c, store.BlockerActor{UserID: "zzc"}, store.QuestionUpdate{RequestID: "policy", PMUser: "lbl", LeaderUser: "zzc"}, func() error { return nil }, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	q := questionHTTPCreate(t, e, state)
	sends := 0
	e.hub.QuestionFeishuEnabled = true
	e.hub.QuestionSender = func(_ context.Context, bot, person, key, body string) (string, error) {
		sends++
		if bot != "question-bot" || person != "ou_lbl" || strings.Contains(body, "实际现象") {
			return "", fmt.Errorf("QUESTION_PRIVATE_CONTACT_OR_BODY_LEAK")
		}
		return "om_initial", nil
	}
	e.hub.QuestionFeishuEnabled = false
	e.hub.processQuestions(ctx)
	closed := questionHTTP(e, "GET", q.ID, "", "zzc", "", "")
	if sends != 0 || closed.Code != 200 || !strings.Contains(closed.Body.String(), `"state":"pending_config"`) || !strings.Contains(closed.Body.String(), store.QuestionFeishuDisabledReason) {
		t.Fatal("D3_PG_BOUND_SWITCH_OFF_SENT_OR_LOST_REASON", sends, closed.Code, closed.Body)
	}
	e.hub.QuestionFeishuEnabled = true
	e.hub.processQuestions(ctx)
	if sends != 1 {
		t.Fatal("QUESTION_CONFIGURED_INITIAL_NOTICE", sends)
	}
	c, _ = e.db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	now := e.hub.now().Add(time.Hour)
	if err = e.db.QueueQuestionReminder(ctx, c, q.ID, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	e.hub.NowFunc = func() time.Time { return now }
	sending, release := make(chan struct{}), make(chan struct{})
	e.hub.QuestionFeishuEnabled = true
	e.hub.QuestionSender = func(_ context.Context, bot, person, key, body string) (string, error) {
		sends++
		if person != "ou_zzc" || !strings.Contains(body, "60分钟") {
			return "", fmt.Errorf("QUESTION_WRONG_REMINDER_TARGET")
		}
		close(sending)
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "om_reminder", nil
	}
	dispatched := make(chan struct{})
	go func() { e.hub.processQuestions(ctx); close(dispatched) }()
	select {
	case <-sending:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	answer := fmt.Sprintf(`{"request_id":"answer","operation":"answer","expected_version":%d,"event_id":"actual-reply","answer_kind":"substantive","body":"明确的范围结论","evidence_ref":"git:answer"}`, q.Version)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- questionHTTP(e, "POST", q.ID, "", "lbl", answer, "") }()
	// The waiter must be the same-question advisory lock, not an unrelated SQLite failure.
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rows, x := pool.Query(ctx, access, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE NOT l.granted AND l.locktype='advisory' AND a.query LIKE '%pg_advisory_xact_lock%'`)
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
		select {
		case out := <-result:
			close(release)
			t.Fatal("QUESTION_ANSWER_COMMITTED_DURING_SEND", out.Code, out.Body)
		default:
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	<-dispatched
	out := <-result
	if !waiting {
		t.Fatal("QUESTION_SEND_REPLY_ADVISORY_NOT_HELD")
	}
	if out.Code != 200 {
		t.Fatal("QUESTION_AFTER_SEND_ANSWER_REJECTED", out.Code, out.Body)
	}
	// A later worker sees the committed substantive answer and sends nothing.
	e.hub.QuestionFeishuEnabled = true
	e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) {
		t.Error("QUESTION_AFTER_ANSWER_SENT_REMINDER")
		return "", nil
	}
	e.hub.processQuestions(ctx)
	// Disable first: no cached authorized answer replay or private payload.
	if _, err = pool.Exec(ctx, access, `UPDATE np_user SET state='disabled' WHERE user_id_lc='lbl'`); err != nil {
		t.Fatal(err)
	}
	out = questionHTTP(e, "GET", q.ID, "", "lbl", "", "")
	if out.Code != 403 || strings.Contains(out.Body.String(), "实际现象") {
		t.Fatal("QUESTION_DISABLED_PM_READ", out.Code, out.Body)
	}
	out = questionHTTP(e, "POST", q.ID, "", "lbl", answer, "")
	if out.Code != 403 {
		t.Fatal("QUESTION_DISABLED_PM_REPLAY", out.Code, out.Body)
	}
	if sends != 2 {
		t.Fatal("QUESTION_EXTRA_PROVIDER_SEND", sends)
	}
}
