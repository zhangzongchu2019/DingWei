package m8

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type r9ReplyRepoFixture struct {
	store.Repository
	target            *store.R9PMReplyTarget
	lookups, accepted int
	err               error
}

func (r *r9ReplyRepoFixture) R9PMReplyTarget(context.Context, string) (*store.R9PMReplyTarget, error) {
	r.lookups++
	return r.target, r.err
}

func TestR9PMDispatcherPassesNonR9ReplyThrough(t *testing.T) {
	h, _, ctx := newTestHub(t)
	repo := &r9ReplyRepoFixture{err: store.ErrR9PMNotFound}
	h.Repo = repo
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	msg := model.Message{ChatType: model.ChatPersonal, IngressProvenance: model.IngressFeishuWSAuthenticated, FeishuReply: &model.FeishuReplyMetadata{ParentID: "clarification-parent"}}
	_, handled, err := h.dispatchR9PMReply(ctx, msg, "同意")
	if err != nil || handled || repo.accepted != 0 {
		t.Fatalf("non-R9 parent was intercepted: handled=%v accepts=%d err=%v", handled, repo.accepted, err)
	}
}
func (r *r9ReplyRepoFixture) AcceptR9PMResponse(context.Context, store.R9PMResponseInput) (bool, error) {
	r.accepted++
	return true, nil
}

func TestR9PMFlagIsExactAndOffSkipsR9Lookup(t *testing.T) {
	h, _, ctx := newTestHub(t)
	repo := &r9ReplyRepoFixture{target: &store.R9PMReplyTarget{RequestID: "r", PMUserIDLC: "alice", Status: "open", Generation: 1}}
	h.Repo = repo
	msg := model.Message{ChatType: model.ChatPersonal, IngressProvenance: model.IngressFeishuWSAuthenticated, FeishuReply: &model.FeishuReplyMetadata{ParentID: "om_request"}}
	for _, flag := range []string{"", "true", " 1 ", "0"} {
		t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", flag)
		if R9PMDecisionRequestEnabled() {
			t.Fatalf("non-exact flag enabled: %q", flag)
		}
		if _, handled, err := h.dispatchR9PMReply(ctx, msg, "同意"); err != nil || handled {
			t.Fatalf("off path handled=%v err=%v", handled, err)
		}
	}
	if repo.lookups != 0 {
		t.Fatalf("disabled flag queried R9 tables %d times", repo.lookups)
	}
}

func TestR9PMFCSRejectsTrustedPrivateFeishuResponse(t *testing.T) {
	h, db, ctx := newTestHub(t)
	target := &store.R9PMReplyTarget{RequestID: "r", PMUserIDLC: "alice", Status: "open", Generation: 1, Provider: "github", Instance: "github.com", RepositoryID: "1", ChangeDirectory: "demo", CommitSHA: strings.Repeat("a", 40)}
	repo := &r9ReplyRepoFixture{target: target}
	h.Repo = repo
	mem := identity.NewMemStore()
	if err := mem.CreateUser(ctx, identity.User{UserID: "alice", ShortName: "alice", Type: identity.UserMember, State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	if err := mem.CreateRegistrationCode(ctx, "alice", "r9-bind", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.RedeemRegistrationCode(ctx, "r9-bind", identity.Identity{Provider: "feishu", Issuer: "bot", Subject: "ou_alice"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	h.Identity = identity.NewService(mem, nil, nil)
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "alice", Provider: "feishu", ProviderIssuer: "bot", ProviderSubject: "ou_alice", DisplayName: "alice", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSpecReadBinding(ctx, "alice", specread.Binding{Principal: "alice", Provider: "github", Instance: "github.com", AccountID: "1", Login: "alice"}); err != nil {
		t.Fatal(err)
	}
	h.SpecReader = &specread.Service{Bindings: db, Providers: map[string]specread.Provider{"github:github.com": &r9ProposalProvider{body: "## 待 PM 决策\n- R9-PM01: Q\n  - 问题：Q？\n  - A. yes\n  - B. no\n  - 建议：A\n"}}, Repositories: map[string]bool{"github:github.com:1": true}}
	h.ClarificationReplyAuthorities = map[string]ClarificationReplyAuthority{"bot": {AppID: "app", Tenant: "tenant"}}
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	t.Setenv("FCS_RPC_URL", "http://fcs.invalid")
	msg := model.Message{ChatType: model.ChatPersonal, BotChannelID: "bot", SenderOpenID: "ou_alice", IngressProvenance: model.IngressFeishuWSAuthenticated, FeishuReply: &model.FeishuReplyMetadata{Authenticated: true, MessageID: "om_reply", ParentID: "om_request", Tenant: "tenant", SenderTenant: "tenant", SenderType: "user", MessageType: "text", AppID: "app", ContentSHA: "payload"}}
	got, handled, err := h.dispatchR9PMReply(ctx, msg, "同意")
	if err != nil || !handled || got.Reply == "" {
		t.Fatalf("FCS Feishu reply not explicitly rejected: %+v handled=%v err=%v", got, handled, err)
	}
	if repo.accepted != 0 {
		t.Fatalf("FCS mode persisted %d Feishu responses", repo.accepted)
	}
}

func TestR9PMDecisionParserRequiresExplicitEnum(t *testing.T) {
	for _, tc := range []struct {
		text, decision string
		ok             bool
	}{{"同意", "agree", true}, {"不同意：理由", "disagree", true}, {"需修改\n请调整A", "revise", true}, {"收到", "", false}, {"我同意吗？", "", false}, {"同意，然后不同意", "", false}, {"同意吧", "", false}} {
		d, _, ok := parseR9PMDecision(tc.text)
		if d != tc.decision || ok != tc.ok {
			t.Errorf("parse %q = %q,%v", tc.text, d, ok)
		}
	}
}

func TestR9PMInitialOutboxUsesPrivateSenderAndStableProviderCorrelation(t *testing.T) {
	h, s, ctx := newTestHub(t)
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	k := store.R9PMKey{Provider: "github", Instance: "github.com", RepositoryID: "1", ChangeDirectory: "change", PointID: "PM01"}
	if _, err := s.SetR9PMResponsibility(ctx, k, "alice", "admin"); err != nil {
		t.Fatal(err)
	}
	_, created, err := s.CreateR9PMRequest(ctx, store.R9PMCreateInput{RequestID: "outbox-r9", R9PMKey: k, CommitSHA: "abc", ContentSHA256: "sha", ConsoleRecipient: "alice", FeishuRecipient: "ou_alice", FeishuIssuer: "bot", SummaryJSON: `{"question":"Q","options":["A"],"recommendation":"A"}`})
	if err != nil || !created {
		t.Fatalf("create outbox created=%v err=%v", created, err)
	}
	sends := 0
	h.R9PMQuestionSender = func(_ context.Context, issuer, subject, operationID, text string) (string, error) {
		sends++
		if issuer != "bot" || subject != "ou_alice" || operationID != "outbox-r9:feishu:initial" || !strings.Contains(text, "固定提交：abc") {
			t.Fatalf("wrong private outbox: issuer=%s subject=%s op=%s body=%s", issuer, subject, operationID, text)
		}
		return "om_r9_initial", nil
	}
	h.dispatchR9PMDeliveries(ctx, s)
	if sends != 1 {
		t.Fatalf("feishu sends=%d", sends)
	}
	target, err := s.R9PMReplyTarget(ctx, "om_r9_initial")
	if err != nil || target.RequestID != "outbox-r9" || target.Generation != 1 {
		t.Fatalf("reply target=%+v err=%v", target, err)
	}
}
