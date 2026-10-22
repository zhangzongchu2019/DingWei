package m8

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"strings"
	"testing"
)

func TestSingleTenantAuthorityGates(t *testing.T) {
	for _, kind := range []string{"valid", "tenant-empty", "tenant-whitespace", "tenant-long", "tenant-control", "sender-empty", "sender-mismatch", "app-mismatch", "app-empty", "issuer-mismatch", "unverified", "provenance", "pin-match", "pin-mismatch", "pin-legacy"} {
		t.Run(kind, func(t *testing.T) {
			h := &Hub{ClarificationReplyAuthorities: map[string]ClarificationReplyAuthority{"fixture": {AppID: "app"}}}
			m := clarificationReplyMessage("om_probe", "ev_probe", "original")
			m.FeishuReply.SenderTenant = "tenant"
			expected := false
			switch kind {
			case "valid":
				expected = true
			case "tenant-empty":
				m.FeishuReply.Tenant = ""
				m.FeishuReply.SenderTenant = ""
			case "tenant-whitespace":
				m.FeishuReply.Tenant = " "
				m.FeishuReply.SenderTenant = " "
			case "tenant-long":
				m.FeishuReply.Tenant = strings.Repeat("x", 257)
				m.FeishuReply.SenderTenant = m.FeishuReply.Tenant
			case "tenant-control":
				m.FeishuReply.Tenant = "ten\x00ant"
				m.FeishuReply.SenderTenant = m.FeishuReply.Tenant
			case "sender-empty":
				m.FeishuReply.SenderTenant = ""
			case "sender-mismatch":
				m.FeishuReply.SenderTenant = "other"
			case "app-mismatch":
				m.FeishuReply.AppID = "other"
			case "app-empty":
				m.FeishuReply.AppID = ""
			case "issuer-mismatch":
				m.BotChannelID = "other"
			case "unverified":
				m.FeishuReply.Authenticated = false
			case "provenance":
				m.IngressProvenance = ""
			case "pin-match":
				h.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app", Tenant: "tenant"}
				expected = true
			case "pin-mismatch":
				h.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app", Tenant: "pinned-other"}
			case "pin-legacy":
				h.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app", Tenant: "tenant"}
				m.FeishuReply.SenderTenant = ""
				expected = true
			}
			got, ok := h.clarificationReplyAuthority(m)
			if ok != expected || ok && (got.Tenant != "tenant" || got.AppID != "app") {
				t.Fatalf("SINGLE_TENANT_AUTH_%s: expected=%v got=%+v ready=%v", kind, expected, got, ok)
			}
		})
	}
}

func TestSingleTenantUnmappedNoticeReplyAuditAndReplay(t *testing.T) {
	e, q, sends, _ := clarificationReplyFixture(t)
	e.hub.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app"}
	e.hub.processQuestions(e.ctx)
	if *sends != 1 {
		t.Fatal("SINGLE_TENANT_UNMAPPED_NOTICE_BLOCKED", *sends)
	}
	text := "无手工租户映射的原范围解释"
	m := clarificationReplyMessage("om_single", "ev_single", text)
	m.FeishuReply.SenderTenant = "tenant"
	for i := 0; i < 2; i++ {
		if _, err := e.hub.Dispatch(e.ctx, m, text); err != nil {
			t.Fatal(err)
		}
	}
	posts := clarificationReplyPosts(t, e, q)
	if len(posts) != 1 || posts[0].Body != text || posts[0].Role != "pm" {
		t.Fatal("SINGLE_TENANT_REPLY_NOT_RECORDED_ONCE", posts)
	}
	// Read the persisted queue entry; tenant and SDK sender tenant survive a
	// process-independent store read, not merely an in-memory authority cache.
	pending, err := e.db.PendingClarificationInbound(e.ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatal("SINGLE_TENANT_RECORDED_REPLY_LEFT_PENDING")
	}
	n, err := e.db.ClarificationNoticeByParent(e.ctx, "fixture", "tenant", "om_notice")
	if err != nil || n.Binding.Tenant != "" {
		t.Fatal("SINGLE_TENANT_NOTICE_REQUIRES_MANUAL_TENANT", n, err)
	}
	// A second event with a fresh message ID is a supplement, not a replay.
	second := clarificationReplyMessage("om_single2", "ev_single2", text)
	second.FeishuReply.SenderTenant = "tenant"
	if _, err = e.hub.Dispatch(e.ctx, second, text); err != nil {
		t.Fatal(err)
	}
	if len(clarificationReplyPosts(t, e, q)) != 2 {
		t.Fatal("SINGLE_TENANT_SUPPLEMENT_LOST")
	}
	// The trusted metadata is deliberately not accepted from ordinary JSON.
	raw, _ := json.Marshal(m)
	var forged model.Message
	if err = json.Unmarshal(raw, &forged); err != nil || forged.FeishuReply != nil {
		t.Fatal("SINGLE_TENANT_JSON_AUTH_FORGERY", err)
	}
}

func TestSingleTenantMappingAddedAfterUnboundNoticeIsStrict(t *testing.T) {
	e, q, sends, _ := clarificationReplyFixture(t)
	e.hub.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app"}
	e.hub.processQuestions(e.ctx)
	if *sends != 1 {
		t.Fatal("SINGLE_TENANT_UNMAPPED_NOTICE_BLOCKED")
	}
	e.hub.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app", Tenant: "pinned-other"}
	m := clarificationReplyMessage("om_wrongpin", "ev_wrongpin", "原范围解释")
	m.FeishuReply.SenderTenant = "tenant"
	if _, err := e.hub.Dispatch(e.ctx, m, "原范围解释"); err != nil {
		t.Fatal(err)
	}
	if len(clarificationReplyPosts(t, e, q)) != 0 {
		t.Fatal("SINGLE_TENANT_MAPPING_IGNORED_AFTER_PREPARE")
	}
}

func TestSingleTenantEarlyReplyRechecksCurrentAppAndPin(t *testing.T) {
	for _, kind := range []string{"valid", "app-changed", "pin-added"} {
		t.Run(kind, func(t *testing.T) {
			e, q, _, _ := clarificationReplyFixture(t)
			e.hub.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app"}
			e.hub.QuestionSender = func(context.Context, string, string, string, string) (string, error) {
				text := "回复早于通知落库"
				m := clarificationReplyMessage("om_early_single", "ev_early_single", text)
				m.FeishuReply.SenderTenant = "tenant"
				if _, err := e.hub.Dispatch(e.ctx, m, text); err != nil {
					t.Fatal(err)
				}
				return "om_notice", nil
			}
			e.hub.processQuestions(e.ctx)
			if len(clarificationReplyPosts(t, e, q)) != 0 {
				t.Fatal("SINGLE_TENANT_EARLY_REPLY_GUESSED_PARENT")
			}
			if kind == "app-changed" {
				e.hub.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "changed"}
			}
			if kind == "pin-added" {
				e.hub.ClarificationReplyAuthorities["fixture"] = ClarificationReplyAuthority{AppID: "app", Tenant: "other"}
			}
			e.hub.reconcileClarificationReplies(e.ctx)
			want := 0
			if kind == "valid" {
				want = 1
			}
			if got := clarificationReplyPosts(t, e, q); len(got) != want {
				t.Fatal("SINGLE_TENANT_EARLY_CURRENT_AUTHORITY_IGNORED_"+kind, got)
			}
		})
	}
}
