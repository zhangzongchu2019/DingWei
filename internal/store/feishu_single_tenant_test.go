package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSingleTenantStorePersistsObservedTenantAndSender(t *testing.T) {
	db, now, c, _, n, _ := clarificationNoticeFixture(t)
	ctx := context.Background()
	// Emulate the app-bound snapshot created before tenant was known. The Hub
	// integration test exercises actual tenant-less PrepareClarificationNotice.
	if _, err := db.db.Exec(`UPDATE spec_clarification_notice SET tenant='' WHERE notice_id=?`, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishQuestionNotice(ctx, n.ID, "om_parent", true, now); err != nil {
		t.Fatal(err)
	}
	notice, err := db.ClarificationNoticeByParent(ctx, "bot", "tenant", "om_parent")
	if err != nil {
		t.Fatal(err)
	}
	msg := clarificationGuardMessage("om_parent")
	msg.FeishuReply.SenderTenant = "tenant"
	in, err := db.StageClarificationInbound(ctx, msg, "原范围解释", now)
	if err != nil {
		t.Fatal(err)
	}
	// Both the dedicated audit column and reloadable metadata must retain it.
	var tenant, metadata string
	if err = db.db.QueryRow(`SELECT tenant,metadata_json FROM spec_clarification_inbound WHERE issuer='bot' AND message_id='om_reply'`).Scan(&tenant, &metadata); err != nil {
		t.Fatal(err)
	}
	var got clarificationInboundMetadata
	if err = json.Unmarshal([]byte(metadata), &got); err != nil || tenant != "tenant" || got.Reply.Tenant != "tenant" || got.Reply.SenderTenant != "tenant" {
		t.Fatal("SINGLE_TENANT_AUDIT_NOT_DURABLE", tenant, got.Reply, err)
	}
	binding := notice.Binding
	binding.Tenant = "tenant"
	if _, err = db.RecordFeishuClarification(ctx, c, notice, binding, BlockerActor{UserID: "pm"}, in, ClarificationUpdate{Kind: "explanation", Body: in.Body}, true, func() error { return nil }, now); err != nil {
		t.Fatal("SINGLE_TENANT_STORE_BINDING_REJECTED", err)
	}
	persisted, err := readClarificationInbound(ctx, db.db, "bot", "tenant", "om_reply")
	if err != nil || persisted.State != "recorded" || persisted.Message.FeishuReply.SenderTenant != "tenant" {
		t.Fatal("SINGLE_TENANT_AUDIT_RELOAD_LOST", persisted, err)
	}
}

func TestSingleTenantStoreKeepsOtherBindingFieldsAndTenantProofStrict(t *testing.T) {
	for _, kind := range []string{"issuer", "subject", "app", "binding-sha", "sender-empty", "sender-mismatch", "tenant-binding"} {
		t.Run(kind, func(t *testing.T) {
			db, now, c, _, n, _ := clarificationNoticeFixture(t)
			ctx := context.Background()
			if _, err := db.db.Exec(`UPDATE spec_clarification_notice SET tenant='' WHERE notice_id=?`, n.ID); err != nil {
				t.Fatal(err)
			}
			if err := db.FinishQuestionNotice(ctx, n.ID, "om_parent", true, now); err != nil {
				t.Fatal(err)
			}
			notice, err := db.ClarificationNoticeByParent(ctx, "bot", "tenant", "om_parent")
			if err != nil {
				t.Fatal(err)
			}
			m := clarificationGuardMessage("om_parent")
			m.FeishuReply.SenderTenant = "tenant"
			b := notice.Binding
			b.Tenant = "tenant"
			switch kind {
			case "issuer":
				b.Issuer = "other"
			case "subject":
				b.Subject = "ou_other"
			case "app":
				b.AppID = "other"
			case "binding-sha":
				b.BindingSHA = "wrong"
			case "sender-empty":
				m.FeishuReply.SenderTenant = ""
			case "sender-mismatch":
				m.FeishuReply.SenderTenant = "other"
			case "tenant-binding":
				b.Tenant = "other"
			}
			in, err := db.StageClarificationInbound(ctx, m, "解释", now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.RecordFeishuClarification(ctx, c, notice, b, BlockerActor{UserID: "pm"}, in, ClarificationUpdate{Kind: "explanation", Body: in.Body}, true, func() error { return nil }, now); !errors.Is(err, ErrTaskFlowPermission) {
				t.Fatal("SINGLE_TENANT_STORE_BAD_BINDING_ACCEPTED_"+kind, err)
			}
		})
	}
}

func TestSingleTenantAmbiguousParentFailsClosed(t *testing.T) {
	db, now, _, _, n, _ := clarificationNoticeFixture(t)
	ctx := context.Background()
	if err := db.FinishQuestionNotice(ctx, n.ID, "om_parent", true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO pilot_question_outbox(id,question_id,generation,recipient_user,kind,policy_version,event_version,state,created_at) SELECT 'duplicate-outbox',question_id,generation,recipient_user,kind,policy_version,event_version+1,state,created_at FROM pilot_question_outbox WHERE id=?`, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO spec_clarification_notice(`+clarificationNoticeCols+`) SELECT 'duplicate-outbox',question_id,generation,spec_id,revision,content_sha256,pm_user,policy_version,issuer,'',app_id,subject,binding_sha,entry_refs_json,provider_message_id,created_at,expires_at,state FROM spec_clarification_notice WHERE notice_id=?`, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClarificationNoticeByParent(ctx, "bot", "tenant", "om_parent"); !errors.Is(err, ErrTaskFlowConflict) {
		t.Fatal("SINGLE_TENANT_AMBIGUOUS_PARENT_ACCEPTED", err)
	}
}
