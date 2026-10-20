package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Synthetic binding/check failure fixture; all object/actor authorization is
// still performed by the real MemStore, with a real SQLite outbox and worker.
type questionCheckFixture struct {
	identity.Store
	auth   identity.QuestionAuthorizationStore
	failAt int
	fail   error
}

func (f questionCheckFixture) WithQuestionAuthorization(ctx context.Context, ag, hash, human string, c *identity.ContextClaim, users, agents []string, qid string, fn func(identity.ContextAuthorization, func() error) error) error {
	return f.auth.WithQuestionAuthorization(ctx, ag, hash, human, c, users, agents, qid, func(a identity.ContextAuthorization, check func() error) error {
		for id, active := range a.Users {
			if active {
				a.Bindings[id] = identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "ou_" + id}
			}
		}
		calls := 0
		return fn(a, func() error {
			calls++
			if calls == f.failAt {
				return f.fail
			}
			return check()
		})
	})
}
func questionDeliveryState(t *testing.T, e *shimEnv, id string) string {
	t.Helper()
	out := questionHTTP(e, "GET", id, "", "zzc", "", "")
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	var body struct {
		Question store.PilotQuestion `json:"question"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Question.Notifications) != 1 {
		t.Fatal("fixture expected one notice", out.Body)
	}
	return body.Question.Notifications[0].State
}
func TestPilotQuestionProviderErrorUnknownAndNeverResends(t *testing.T) {
	e, q := questionWorkerConfigured(t)
	original := e.svc.Store
	e.svc.Store = questionCheckFixture{Store: original, auth: original.(identity.QuestionAuthorizationStore)}
	e.hub.PublicOrigin = "https://console.fixture.invalid"
	sent := 0
	e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) {
		sent++
		return "provider-may-have-accepted", errors.New("lost provider receipt")
	}
	e.hub.processQuestions(e.ctx)
	if got := questionDeliveryState(t, e, q.ID); got != "unknown" {
		t.Fatal("QUESTION_SENDER_ERROR_NOT_UNKNOWN", got)
	}
	e.hub.processQuestions(e.ctx)
	if sent != 1 {
		t.Fatal("QUESTION_SENDER_ERROR_RETRIED", sent)
	}
}
func TestPilotQuestionPreProviderCheckFailureDisposition(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   int
		err  error
		want string
	}{{"prepare", 1, errors.New("PG unavailable"), "pending_config"}, {"late-transient", 2, errors.New("PG unavailable"), "queued"}, {"late-revoked", 2, identity.ErrForbidden, "cancelled"}} {
		t.Run(tc.name, func(t *testing.T) {
			e, q := questionWorkerConfigured(t)
			original := e.svc.Store
			e.svc.Store = questionCheckFixture{Store: original, auth: original.(identity.QuestionAuthorizationStore), failAt: tc.at, fail: tc.err}
			e.hub.PublicOrigin = "https://console.fixture.invalid"
			sent := 0
			e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) { sent++; return "ok", nil }
			e.hub.processQuestions(e.ctx)
			// Read with ordinary current authorization, not the failing-check fixture.
			e.svc.Store = original
			if sent != 0 || questionDeliveryState(t, e, q.ID) != tc.want {
				t.Fatal("QUESTION_UNSENT_DISPOSITION", sent, questionDeliveryState(t, e, q.ID), tc.want)
			}
		})
	}
}
func questionContactsHTTP(e *shimEnv, spec, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/api/console/specs/"+spec+"/question-context"+query, nil)
	r.SetPathValue("specID", spec)
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
	out := httptest.NewRecorder()
	e.hub.HandleQuestionCreateContext(out, r)
	return out
}
func TestPilotQuestionContactsScopedSortedAndPaginated(t *testing.T) {
	e, g, state := r32FlowEnv(t)
	c, err := e.db.QuestionAuthorizationContext(e.ctx, "", state.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	outside, err := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	query := "?task_id=" + state.ID
	out := questionContactsHTTP(e, c.SpecID, query)
	if out.Code != 200 || strings.Contains(out.Body.String(), `"id":"lbl"`) {
		t.Fatal("QUESTION_CONTACT_GLOBAL_DIRECTORY", out.Code, out.Body)
	}
	if err = e.svc.JoinGroup(e.ctx, "lbl", g.Code, outside.ID); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"zulu", "alpha"} {
		if err = e.svc.Store.CreateUser(e.ctx, identity.User{UserID: u, ShortName: u, State: identity.UserActive}); err != nil {
			t.Fatal(err)
		}
	}
	full := query + "&allow_user=ZULU&allow_user=alpha&limit=1"
	ids := []string{}
	cursor := ""
	for i := 0; i < 5; i++ {
		path := full
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		out = questionContactsHTTP(e, c.SpecID, path)
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body)
		}
		var b struct {
			Contacts []struct {
				ID string `json:"id"`
			} `json:"contacts"`
			Next string `json:"contacts_next_cursor"`
		}
		if err = json.Unmarshal(out.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		if len(b.Contacts) > 1 {
			t.Fatal("QUESTION_CONTACT_LIMIT_IGNORED", len(b.Contacts))
		}
		for _, x := range b.Contacts {
			ids = append(ids, x.ID)
		}
		cursor = b.Next
		if cursor == "" {
			break
		}
	}
	if strings.Join(ids, ",") != "alpha,lbl,zulu,zzc" {
		t.Fatal("QUESTION_CONTACT_ORDER_OR_PAGINATION", ids)
	}
	first := questionContactsHTTP(e, c.SpecID, full)
	var b struct {
		Next string `json:"contacts_next_cursor"`
	}
	json.Unmarshal(first.Body.Bytes(), &b)
	if err = e.svc.Store.LeaveGroup(e.ctx, g.Code, outside.ID, "lbl"); err != nil {
		t.Fatal(err)
	}
	out = questionContactsHTTP(e, c.SpecID, full+"&cursor="+url.QueryEscape(b.Next))
	if out.Code != 409 {
		t.Fatal("QUESTION_CONTACT_DRIFT_NOT_RECHECKED", out.Code, out.Body)
	}
	out = questionContactsHTTP(e, c.SpecID, query)
	if out.Code != 200 || strings.Contains(out.Body.String(), `"id":"lbl"`) || strings.Contains(out.Body.String(), `"id":"alpha"`) {
		t.Fatal("QUESTION_CONTACT_REVOKED_OR_IMPLICIT_ALLOW", out.Code, out.Body)
	}
}
func TestPilotQuestionHTTPRecipientCaseCanonical(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	q := questionHTTPCreate(t, e, state)
	body := fmt.Sprintf(`{"request_id":"case","operation":"assign","expected_version":%d,"pm_user":"LBL","leader_user":"ZZC"}`, q.Version)
	out := questionHTTP(e, "POST", q.ID, "", "zzc", body, "")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"pm_user":"lbl"`) || !strings.Contains(out.Body.String(), `"leader_user":"zzc"`) {
		t.Fatal("QUESTION_HTTP_RECIPIENT_CASE", out.Code, out.Body)
	}
}

func questionWorkerConfigured(t *testing.T) (*shimEnv, store.PilotQuestion) {
	e, _, state := r32FlowEnv(t)
	c, err := e.db.QuestionAuthorizationContext(e.ctx, "", state.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err = e.db.QuestionAuthorizationContext(e.ctx, c.SpecID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.db.SetQuestionPolicy(e.ctx, c, store.BlockerActor{UserID: "zzc"}, store.QuestionUpdate{RequestID: "worker-policy", PMUser: "zzc", LeaderUser: "zzc"}, func() error { return nil }, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	return e, questionHTTPCreate(t, e, state)
}

type questionContactsDriftFixture struct {
	identity.Store
	identity.QuestionAuthorizationStore
	base   identity.QuestionContactAuthorizationStore
	before func()
}

func (f questionContactsDriftFixture) WithQuestionContacts(ctx context.Context, human string, groups, users []string, fn func(identity.ContextAuthorization, func() error) error) error {
	f.before()
	return f.base.WithQuestionContacts(ctx, human, groups, users, fn)
}
func TestPilotQuestionContactsRecheckFormerGroup(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	outside, err := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	group, err := e.svc.CreateGroup(e.ctx, "lbl", outside.ID, "OutsideContacts")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.JoinGroup(e.ctx, "zzc", group.Code, state.Developer); err != nil {
		t.Fatal(err)
	}
	c, err := e.db.QuestionAuthorizationContext(e.ctx, "", state.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	original := e.svc.Store
	e.svc.Store = questionContactsDriftFixture{Store: original, QuestionAuthorizationStore: original.(identity.QuestionAuthorizationStore), base: original.(identity.QuestionContactAuthorizationStore), before: func() {
		if err := original.LeaveGroup(e.ctx, group.Code, state.Developer, "zzc"); err != nil {
			t.Fatal(err)
		}
	}}
	out := questionContactsHTTP(e, c.SpecID, "")
	if out.Code != 200 || strings.Contains(out.Body.String(), `"id":"lbl"`) {
		t.Fatal("QUESTION_CONTACT_FORMER_GROUP_LEAK", out.Code, out.Body)
	}
}

func TestPilotQuestionNoRecipientRemainsPendingConfig(t *testing.T) {
	e, _, state := r32FlowEnv(t)
	q := questionHTTPCreate(t, e, state)
	sent := 0
	e.hub.PublicOrigin = "https://console.fixture.invalid"
	e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) {
		sent++
		return "unexpected", nil
	}
	e.hub.processQuestions(e.ctx)
	if sent != 0 || questionDeliveryState(t, e, q.ID) != "pending_config" {
		t.Fatal("QUESTION_EMPTY_RECIPIENT_CANCELLED_OR_SENT", sent, questionDeliveryState(t, e, q.ID))
	}
}
