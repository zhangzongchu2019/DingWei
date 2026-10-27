package m8

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func clarificationReplyFixture(t *testing.T, beforeCreate ...func(*shimEnv, store.PilotQuestion)) (*shimEnv, store.PilotQuestion, *int, *[]string) {
	t.Helper()
	e, _, state, old := clarificationHTTPFixture(t)
	if err := e.svc.Store.CreateRegistrationCode(e.ctx, "lbl", "clarification-bind", e.hub.now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Store.RedeemRegistrationCode(e.ctx, "clarification-bind", identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "ou_pm"}, e.hub.now()); err != nil {
		t.Fatal(err)
	}
	for _, configure := range beforeCreate {
		configure(e, old)
	}
	c, err := e.db.QuestionAuthorizationContext(e.ctx, old.SpecID, state.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"request_id":"anchored-question","expected_version":%d,"entry_refs":["A1"],"kind":"requirement_unclear","title":"有条目锚点的问题","observed_fact":"实际现象","question":"请解释条目A1","expected_answer":"明确原范围的边界"}`, c.Task.Version)
	w := questionHTTP(e, "POST", "", e.zKimiCred, "", body, "")
	if w.Code != 200 {
		t.Fatal("CLARIFICATION_TYPED_QUESTION_CREATE", w.Code, w.Body)
	}
	var result store.QuestionResult
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	e.hub.QuestionFeishuEnabled = true
	e.hub.SpecClarificationFeishuReplyEnabled = true
	e.hub.PublicOrigin = "https://console.fixture.invalid"
	e.hub.ClarificationReplyAuthorities = map[string]ClarificationReplyAuthority{"fixture": {AppID: "app", Tenant: "tenant"}}
	sends := 0
	receipts := []string{}
	e.hub.QuestionSender = func(_ context.Context, issuer, subject, notice, text string) (string, error) {
		sends++
		if !strings.Contains(text, "直接回复本消息＝对本问题的解释，不改范围") || !strings.Contains(text, "A1") || strings.Contains(text, "实际现象") {
			t.Error("CLARIFICATION_NOTICE_CONTRACT", text)
		}
		return "om_notice", nil
	}
	e.hub.ClarificationReceiptSender = func(_ context.Context, issuer, subject, id, text string) (string, error) {
		receipts = append(receipts, text)
		return "om_receipt", nil
	}
	return e, result.Question, &sends, &receipts
}

func clarificationReplyMessage(id, event, body string) model.Message {
	content, _ := json.Marshal(map[string]string{"text": body})
	return model.Message{ID: id, FeishuMsgID: id, BotChannelID: "fixture", SenderOpenID: "ou_pm", ChatType: model.ChatPersonal, IngressProvenance: model.IngressFeishuWSAuthenticated, Content: string(content), FeishuReply: &model.FeishuReplyMetadata{Authenticated: true, ContentSHA: fmt.Sprintf("%x", sha256.Sum256(content)), EventID: event, MessageID: id, ParentID: "om_notice", RootID: "om_notice", MessageType: "text", Tenant: "tenant", AppID: "app", ChatID: "oc_dm", SenderType: "user"}}
}

func clarificationReplyPosts(t *testing.T, e *shimEnv, q store.PilotQuestion) []store.ClarificationPost {
	t.Helper()
	w := clarificationHTTP(e, "GET", q.ID, "", "zzc", "", "", "", false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var b struct {
		Posts []store.ClarificationPost `json:"posts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b.Posts
}

func TestClarificationFeishuPlainReplySupplementsAndReplay(t *testing.T) {
	e, q, sends, receipts := clarificationReplyFixture(t)
	e.hub.processQuestions(e.ctx)
	if *sends != 1 {
		t.Fatal("CLARIFICATION_NOTICE_NOT_SENT", *sends)
	}
	// Outbound disabled after delivery must not disable this independent ingress.
	e.hub.QuestionFeishuEnabled = false
	e.hub.QuestionSender = nil
	text := "这是原范围的解释，无需条目前缀"
	first := clarificationReplyMessage("om_reply1", "ev1", text)
	for i := 0; i < 2; i++ {
		r, err := e.hub.Dispatch(e.ctx, first, text)
		if err != nil || !r.Matched || r.Reply != "" {
			t.Fatal("CLARIFICATION_NATIVE_REPLY_ROUTED_ELSEWHERE", r, err)
		}
	}
	second := clarificationReplyMessage("om_reply2", "ev2", text)
	if _, err := e.hub.Dispatch(e.ctx, second, text); err != nil {
		t.Fatal(err)
	}
	posts := clarificationReplyPosts(t, e, q)
	if len(posts) != 2 || posts[0].Floor != 2 || posts[0].PreviousFloor != 1 || posts[0].Source != "feishu" || posts[0].Role != "pm" || posts[0].Body != text || posts[1].Body != text || len(*receipts) != 2 {
		t.Fatal("CLARIFICATION_FEISHU_SUPPLEMENT_OR_REPLAY", posts, *receipts)
	}
	if !strings.Contains((*receipts)[0], "已入第1条") || !strings.Contains((*receipts)[1], "已入第2条") {
		t.Fatal("CLARIFICATION_RECEIPT_NOT_BOUND_TO_FLOOR", *receipts)
	}
	altered := first
	altered.FeishuReply = &model.FeishuReplyMetadata{}
	*altered.FeishuReply = *first.FeishuReply
	if _, err := e.hub.Dispatch(e.ctx, altered, text+" changed same message ID"); err != nil {
		t.Fatal(err)
	}
	if got := clarificationReplyPosts(t, e, q); len(got) != 2 {
		t.Fatal("CLARIFICATION_CHANGED_REPLAY_INSERTED", got)
	}
	alias := first
	alias.FeishuReply = &model.FeishuReplyMetadata{}
	*alias.FeishuReply = *first.FeishuReply
	alias.FeishuReply.EventID = "ev-alias"
	if _, err := e.hub.Dispatch(e.ctx, alias, text); err != nil {
		t.Fatal(err)
	}
	conflict := clarificationReplyMessage("om_reply3", "ev-alias", text)
	if _, err := e.hub.Dispatch(e.ctx, conflict, text); err != nil {
		t.Fatal(err)
	}
	if len(clarificationReplyPosts(t, e, q)) != 2 {
		t.Fatal("CLARIFICATION_EVENT_ALIAS_REUSED")
	}
}

func TestClarificationFeishuRejectsForgedMetadataAndWrongParent(t *testing.T) {
	for _, name := range []string{"unverified", "tenant", "app", "sender", "bot", "group", "image", "root-only", "wrong-parent", "expired", "disabled", "policy-changed", "inactive"} {
		t.Run(name, func(t *testing.T) {
			e, q, _, _ := clarificationReplyFixture(t)
			e.hub.processQuestions(e.ctx)
			text := "不能伪造为PM解释"
			m := clarificationReplyMessage("om_bad", "ev_bad", text)
			switch name {
			case "unverified":
				m.FeishuReply.Authenticated = false
			case "tenant":
				m.FeishuReply.Tenant = "other"
			case "app":
				m.FeishuReply.AppID = "other"
			case "sender":
				m.SenderOpenID = "ou_other"
			case "bot":
				m.FeishuReply.SenderType = "app"
			case "group":
				m.ChatType = model.ChatGroup
			case "image":
				m.FeishuReply.MessageType = "image"
			case "root-only":
				m.FeishuReply.ParentID = ""
			case "wrong-parent":
				m.FeishuReply.ParentID = "om_not_notification"
			case "expired":
				now := e.hub.now().Add(8 * 24 * time.Hour)
				e.hub.NowFunc = func() time.Time { return now }
			case "disabled":
				e.hub.SpecClarificationFeishuReplyEnabled = false
			case "policy-changed":
				w := clarificationHTTP(e, "POST", "", q.SpecID, "zzc", "", `{"expected_version":1,"pm_user":"zzc"}`, "", true)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body)
				}
			case "inactive":
				if err := e.svc.Store.SetUserState(e.ctx, "lbl", identity.UserDisabled, e.hub.now()); err != nil {
					t.Fatal(err)
				}
			}
			r, err := e.hub.Dispatch(e.ctx, m, text)
			if err != nil || !r.Matched || r.Reply != "" {
				t.Fatal("CLARIFICATION_INVALID_REPLY_FELL_THROUGH", name, r, err)
			}
			if posts := clarificationReplyPosts(t, e, q); len(posts) != 0 {
				t.Fatal("CLARIFICATION_INVALID_REPLY_POSTED", name, posts)
			}
			if name == "disabled" {
				e.hub.SpecClarificationFeishuReplyEnabled = true
				if _, err = e.hub.Dispatch(e.ctx, m, text); err != nil {
					t.Fatal(err)
				}
				if len(clarificationReplyPosts(t, e, q)) != 0 {
					t.Fatal("CLARIFICATION_DISABLED_REPLY_REPLAYED_AFTER_REENABLE")
				}
			}
		})
	}
}

func TestClarificationFeishuEarlyReplyReconcilesExactParent(t *testing.T) {
	e, q, sends, _ := clarificationReplyFixture(t)
	e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) {
		*sends++
		text := "回复先到，等待通知ID持久化"
		m := clarificationReplyMessage("om_early", "ev_early", text)
		r, err := e.hub.Dispatch(e.ctx, m, text)
		if err != nil || !r.Matched {
			t.Fatal("CLARIFICATION_EARLY_REPLY_NOT_DURABLE", r, err)
		}
		return "om_notice", nil
	}
	e.hub.processQuestions(e.ctx)
	if got := clarificationReplyPosts(t, e, q); len(got) != 0 {
		t.Fatal("CLARIFICATION_EARLY_REPLY_GUESSED_BEFORE_FINISH", got)
	}
	e.hub.reconcileClarificationReplies(e.ctx)
	if got := clarificationReplyPosts(t, e, q); len(got) != 1 || got[0].Body != "回复先到，等待通知ID持久化" {
		t.Fatal("CLARIFICATION_EARLY_REPLY_NOT_RECONCILED", got)
	}
	e.hub.reconcileClarificationReplies(e.ctx)
	if len(clarificationReplyPosts(t, e, q)) != 1 {
		t.Fatal("CLARIFICATION_RECONCILIATION_DUPLICATED_POST")
	}
}

func TestClarificationFeishuOptionalAnchorsAndCorrection(t *testing.T) {
	e, q, _, _ := clarificationReplyFixture(t)
	e.hub.processQuestions(e.ctx)
	for i, text := range []string{"原范围解释", "条目：A1/case-1\n对子例的补充", "更正1：原表述仅作解释，不改变范围"} {
		m := clarificationReplyMessage(fmt.Sprintf("om_%d", i), fmt.Sprintf("ev_%d", i), text)
		if _, err := e.hub.Dispatch(e.ctx, m, text); err != nil {
			t.Fatal(err)
		}
	}
	posts := clarificationReplyPosts(t, e, q)
	if len(posts) != 3 || posts[0].Corrects != 1 || posts[1].EntryRefs[0] != "A1/case-1" || posts[2].Body != "原范围解释" {
		t.Fatal("CLARIFICATION_OPTIONAL_PREFIX_OR_CORRECTION", posts)
	}
}

// Both rejected actors have a valid current binding and a same-group Agent.
// ResolveIdentity must succeed: an unbound fake subject cannot cover this gate.
func TestClarificationFeishuRejectsBoundNonPMMembers(t *testing.T) {
	for _, role := range []string{"deputy", "ordinary_member"} {
		t.Run(role, func(t *testing.T) {
			e, q, sends, receipts := clarificationReplyFixture(t, func(e *shimEnv, old store.PilotQuestion) {
				if err := e.svc.Store.SetUserType(e.ctx, "lbl", identity.UserMember); err != nil {
					t.Fatal(err)
				}
				agent, err := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
				if err != nil {
					t.Fatal(err)
				}
				c, err := e.db.QuestionAuthorizationContext(e.ctx, "", "", old.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err = e.svc.JoinGroup(e.ctx, "lbl", c.Task.GroupCode, agent.ID); err != nil {
					t.Fatal(err)
				}
				w := clarificationHTTP(e, "POST", "", old.SpecID, "zzc", "", `{"expected_version":1,"pm_user":"zzc"}`, "", true)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body)
				}
				if role == "deputy" {
					w = clarificationHTTP(e, "POST", "", old.SpecID, "zzc", "", `{"expected_version":2,"pm_user":"zzc","deputy_user":"lbl"}`, "", true)
					if w.Code != 200 {
						t.Fatal(w.Code, w.Body)
					}
				}
				if err = e.svc.Store.CreateRegistrationCode(e.ctx, "zzc", "current-pm-code", e.hub.now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				if _, err = e.svc.Store.RedeemRegistrationCode(e.ctx, "current-pm-code", identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "ou_current_pm"}, e.hub.now()); err != nil {
					t.Fatal(err)
				}
			})
			user, err := e.svc.Store.ResolveIdentity(e.ctx, identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "ou_pm"})
			if err != nil || user == nil || user.UserIDLC() != "lbl" || user.State != identity.UserActive || user.Type != identity.UserMember {
				t.Fatal("CLARIFICATION_NON_PM_FIXTURE_NOT_BOUND_ACTIVE_MEMBER", user, err)
			}
			policy, err := e.db.ClarificationPolicy(e.ctx, q.SpecID)
			if err != nil || policy.PMUser != "zzc" || (role == "deputy") != (policy.DeputyUser == "lbl") {
				t.Fatal("CLARIFICATION_NON_PM_FIXTURE_ROLE", policy, err)
			}
			e.hub.processQuestions(e.ctx)
			if *sends != 1 {
				t.Fatal("CLARIFICATION_NON_PM_FIXTURE_NOTICE_MISSING", *sends)
			}
			text := "已绑定非PM也不能通过飞书解释"
			message := clarificationReplyMessage("om_non_pm", "ev_non_pm", text)
			result, err := e.hub.Dispatch(e.ctx, message, text)
			if err != nil || !result.Matched || result.Reply != "" {
				t.Fatal("CLARIFICATION_BOUND_NON_PM_REPLY_FELL_THROUGH", result, err)
			}
			e.hub.reconcileClarificationReplies(e.ctx)
			if posts := clarificationReplyPosts(t, e, q); len(posts) != 0 {
				t.Fatal("CLARIFICATION_BOUND_NON_PM_REPLY_POSTED", role, posts)
			}
			in, err := e.db.StageClarificationInbound(e.ctx, message, text, e.hub.now())
			if err != nil || in.State != "rejected" || in.Reason != "current_pm_identity_required" {
				t.Fatal("CLARIFICATION_BOUND_NON_PM_NOT_EXPLICITLY_REJECTED", role, in.State, in.Reason, err)
			}
			if len(*receipts) != 1 || !strings.Contains((*receipts)[0], "未入帖") {
				t.Fatal("CLARIFICATION_BOUND_NON_PM_RECEIPT", *receipts)
			}
			// The exact same issued parent still works for its current PM: this is not
			// a disabled/unmatched/expired-notice false red.
			positive := clarificationReplyMessage("om_current_pm", "ev_current_pm", "当前PM本人解释")
			positive.SenderOpenID = "ou_current_pm"
			if _, err = e.hub.Dispatch(e.ctx, positive, "当前PM本人解释"); err != nil {
				t.Fatal(err)
			}
			if posts := clarificationReplyPosts(t, e, q); len(posts) != 1 || posts[0].Role != "pm" || posts[0].Source != "feishu" {
				t.Fatal("CLARIFICATION_CURRENT_PM_POSITIVE_FAILED", posts)
			}
			console := clarificationHTTP(e, "POST", q.ID, "", "lbl", "", clarificationHTTPBody("console-non-pm", "explanation"), "", false)
			want := 403
			if role == "deputy" {
				want = 200
			}
			if console.Code != want {
				t.Fatal("CLARIFICATION_NON_PM_CONSOLE_BOUNDARY", role, console.Code, console.Body)
			}
		})
	}
}

func TestClarificationFeishuExplicitSupplementKindSurvivesNativeReplyPipeline(t *testing.T) {
	e, q, _, receipts := clarificationReplyFixture(t)
	e.hub.processQuestions(e.ctx)
	for i, text := range []string{"原范围解释", "补充：新增原范围内的边界例子"} {
		msg := clarificationReplyMessage(fmt.Sprintf("om_supplement_%d", i), fmt.Sprintf("ev_supplement_%d", i), text)
		result, err := e.hub.Dispatch(e.ctx, msg, text)
		if err != nil || !result.Matched || result.Reply != "" {
			t.Fatalf("飞书补充离开原通知回写链: %+v %v", result, err)
		}
	}
	posts := clarificationReplyPosts(t, e, q)
	if len(posts) != 2 || posts[0].Kind != "supplement" || posts[0].Body != "新增原范围内的边界例子" || posts[0].PreviousFloor != 1 || posts[1].Kind != "explanation" || len(*receipts) != 2 {
		t.Fatalf("补充类型未贯通parser/store/楼层回执: %+v %+v", posts, *receipts)
	}
}

func TestParseClarificationReplySupplementAndExistingKinds(t *testing.T) {
	for _, test := range []struct {
		body, kind, text string
		invalid          bool
	}{
		{"解释原范围", "explanation", "解释原范围", false},
		{"补充：边界例子", "supplement", "边界例子", false},
		{"补充: boundary", "supplement", "boundary", false},
		{"条目：A1/case-1\n补充：子例", "supplement", "子例", false},
		{"更正1：原范围边界", "correction", "原范围边界", false},
		{"补充：", "", "", true},
		{"需改 Spec：新增范围", "", "", true},
	} {
		t.Run(test.body, func(t *testing.T) {
			u, err := parseClarificationReply(test.body)
			if test.invalid {
				if err == nil {
					t.Fatal("非法回复应拒绝")
				}
				return
			}
			if err != nil || u.Kind != test.kind || u.Body != test.text {
				t.Fatalf("回复类型回归: %+v %v", u, err)
			}
		})
	}
}
