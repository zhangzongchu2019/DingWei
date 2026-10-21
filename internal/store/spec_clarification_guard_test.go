package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"strings"
	"testing"
	"time"
)

func clarificationGuardMessage(parent string) model.Message {
	body := `{"text":"解释原条目"}`
	return model.Message{BotChannelID: "bot", SenderOpenID: "ou_pm", FeishuMsgID: "om_reply", ChatType: model.ChatPersonal, IngressProvenance: model.IngressFeishuWSAuthenticated, Content: body, FeishuReply: &model.FeishuReplyMetadata{Authenticated: true, ContentSHA: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), EventID: "event", MessageID: "om_reply", ParentID: parent, RootID: "om_parent", MessageType: "text", Tenant: "tenant", AppID: "app", ChatID: "oc_dm", SenderType: "user"}}
}
func TestClarificationDirectInboundGuardFields(t *testing.T) {
	for _, kind := range []string{"parent", "binding", "generation", "policy", "expiry", "disabled"} {
		t.Run(kind, func(t *testing.T) {
			db, now, c, q, n, _ := clarificationNoticeFixture(t)
			ctx := context.Background()
			if e := db.FinishQuestionNotice(ctx, n.ID, "om_parent", true, now); e != nil {
				t.Fatal(e)
			}
			notice, e := db.ClarificationNoticeByParent(ctx, "bot", "tenant", "om_parent")
			if e != nil {
				t.Fatal(e)
			}
			msg := clarificationGuardMessage("om_parent")
			binding := notice.Binding
			enabled := true
			at := now
			switch kind {
			case "parent":
				msg.FeishuReply.ParentID = "om_wrong_parent"
			case "binding":
				binding.BindingSHA = strings.Repeat("c", 64)
			case "generation":
				if _, e = db.db.Exec(`UPDATE pilot_question SET generation=generation+1 WHERE id=?`, q.ID); e != nil {
					t.Fatal(e)
				}
			case "policy":
				if _, e = db.db.Exec(`UPDATE spec_clarification_policy SET version=version+1 WHERE spec_id=?`, q.SpecID); e != nil {
					t.Fatal(e)
				}
			case "expiry":
				at = now.Add(8 * 24 * time.Hour)
			case "disabled":
				enabled = false
			}
			in, e := db.StageClarificationInbound(ctx, msg, "解释原条目", now)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.RecordFeishuClarification(ctx, c, notice, binding, BlockerActor{UserID: "pm"}, in, ClarificationUpdate{Kind: "explanation", Body: in.Body}, enabled, func() error { return nil }, at); e == nil {
				t.Fatal("CLARIFICATION_DIRECT_" + strings.ToUpper(kind) + "_ACCEPTED")
			}
			var count int
			if e = db.db.QueryRow(`SELECT count(*) FROM spec_clarification_post`).Scan(&count); e != nil || count != 0 {
				t.Fatal("CLARIFICATION_REJECTED_INBOUND_PARTIAL_POST", count, e)
			}
		})
	}
}
func TestClarificationDurableInboundDoubleIDConflict(t *testing.T) {
	for _, kind := range []string{"message", "event"} {
		t.Run(kind, func(t *testing.T) {
			db, now, _ := clarificationFixture(t)
			ctx := context.Background()
			m := clarificationGuardMessage("om_parent")
			if _, e := db.StageClarificationInbound(ctx, m, "original", now); e != nil {
				t.Fatal(e)
			}
			if kind == "message" {
				m.FeishuReply.EventID = "another_event"
				m.FeishuReply.ContentSHA = strings.Repeat("b", 64)
			} else {
				m.FeishuMsgID = "om_other"
				m.FeishuReply.MessageID = m.FeishuMsgID
			}
			if _, e := db.StageClarificationInbound(ctx, m, "changed", now); !errors.Is(e, ErrTaskFlowConflict) {
				t.Fatal("CLARIFICATION_STAGE_"+strings.ToUpper(kind)+"_CONFLICT_ACCEPTED", e)
			}
		})
	}
}
func TestClarificationPublicRevisionDoesNotInheritOldThread(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	if _, e := clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, clarificationUpdate("old-revision")); e != nil {
		t.Fatal(e)
	}
	if _, e := db.db.Exec(`INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_by_kind,created_via,supersedes_revision,created_at) SELECT spec_id,2,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_by_kind,created_via,revision,created_at FROM console_spec_revision WHERE spec_id='r32' AND revision=1`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.db.Exec(`UPDATE console_spec SET current_revision=2 WHERE id='r32'`); e != nil {
		t.Fatal(e)
	}
	c, e := db.QuestionAuthorizationContext(ctx, "r32", "", "")
	if e != nil {
		t.Fatal(e)
	}
	current, e := db.ReadSpecClarifications(ctx, c, 2, 0, 10, func() error { return nil })
	if e != nil || len(current.Posts) != 0 {
		t.Fatal("CLARIFICATION_PUBLIC_REVISION_CROSS_BIND", len(current.Posts), e)
	}
	prior, e := db.ReadSpecClarifications(ctx, c, 1, 0, 10, func() error { return nil })
	if e != nil || len(prior.Posts) != 1 {
		t.Fatal("CLARIFICATION_PRIOR_REVISION_LOST", len(prior.Posts), e)
	}
}
func TestClarificationCorrectionNotifiesCurrentReadersWithoutReopening(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	first, e := clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, clarificationUpdate("first"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.db.Exec(`UPDATE console_spec_task SET status='done' WHERE id='task'`); e != nil {
		t.Fatal(e)
	}
	u := clarificationUpdate("correction")
	u.Kind = "correction"
	u.Corrects = first.Post.Floor
	u.Reason = "explicit correction"
	if _, e = clarificationApply(t, db, now, BlockerActor{UserID: "pm"}, q, u); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = db.db.QueryRow(`SELECT count(*) FROM console_task_flow_outbox WHERE event LIKE 'clarification_correction/%'`).Scan(&n); e != nil || n != 3 {
		t.Fatal("CLARIFICATION_CORRECTION_NOTIFICATION_MISSING", n, e)
	}
	var state string
	if e = db.db.QueryRowContext(ctx, `SELECT status FROM console_spec_task WHERE id='task'`).Scan(&state); e != nil || state != "done" {
		t.Fatal("CLARIFICATION_CORRECTION_REOPENED_TASK", state, e)
	}
}
