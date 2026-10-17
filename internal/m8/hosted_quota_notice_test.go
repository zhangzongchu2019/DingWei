package m8

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestHostedQuotaNoticeDurableBoundOwnerAndExactOps(t *testing.T) {
	e := newAgentEnv(t)
	ctx := e.ctx
	h := e.hub
	if err := e.svc.Store.SetUserType(ctx, "zzc", identity.UserPlatformAdmin); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Store.CreateRegistrationCode(ctx, "zzc", "fixture-bind", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Store.RedeemRegistrationCode(ctx, "fixture-bind", identity.Identity{Provider: "feishu", Issuer: "fixture-bot", Subject: "ou_fixture_owner"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.Repo.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: AgentKeyID("zzc"), SessionName: "opus50ts", Active: true, LastSeenAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := h.Repo.SetSessionEndpointDeliveryACK(ctx, AgentKeyID("zzc"), "opus50ts", true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := h.QueueHostedQuotaNotice(ctx, "zzc", "Opus50TS.zzc", "owner", "quota-notice-owner", "fixed sanitized notice"); err != nil {
			t.Fatal(err)
		}
		if err := h.QueueHostedQuotaNotice(ctx, "zzc", "Opus50TS.zzc", "ops", "quota-notice-ops", "fixed sanitized notice"); err != nil {
			t.Fatal(err)
		}
	}
	m, err := h.Repo.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || m == nil || m.ChatEntityID != "fixture-bot:personal:ou_fixture_owner" || m.ID != "quota-notice-owner" {
		t.Fatalf("owner queue %+v %v", m, err)
	}
	if err = h.Repo.AckMessage(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	m, err = h.Repo.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || m != nil {
		t.Fatal("duplicate owner notice")
	}
	d, err := h.Repo.GetSessionDelivery(ctx, "dw-quota-notice-ops")
	if err != nil || d == nil || d.KeyID != AgentKeyID("zzc") || d.SessionName != "opus50ts" || d.SourceType != "hosted_quota_notice" {
		t.Fatalf("ops durable %+v %v", d, err)
	}
	var env model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &env) != nil || env.Meta["reply_policy"] != "no_reply" || env.Meta["system_notification"] != true || env.Meta["ack_required"] != true {
		t.Fatalf("notification envelope %+v", env)
	}
	for _, tc := range []struct{ owner, ops, recipient string }{{"", "Opus50TS.zzc", "owner"}, {"missing", "Opus50TS.zzc", "owner"}, {"zzc", "missing.zzc", "ops"}, {"zzc", "Opus50TS.zzc", "all"}} {
		if h.QueueHostedQuotaNotice(ctx, tc.owner, tc.ops, tc.recipient, "quota-notice-bad", "notice") == nil {
			t.Fatal("unbound destination accepted")
		}
	}
	if err = h.Repo.SetSessionEndpointDeliveryACK(ctx, AgentKeyID("zzc"), "opus50ts", false); err != nil {
		t.Fatal(err)
	}
	if h.QueueHostedQuotaNotice(ctx, "zzc", "Opus50TS.zzc", "ops", "quota-notice-no-ack", "notice") == nil {
		t.Fatal("non-durable ops path accepted")
	}
	if h.QueueHostedQuotaNotice(ctx, "zzc", "Opus50TS.zzc", "owner", "other-id", strings.Repeat("x", 5000)) == nil {
		t.Fatal("unbounded notice accepted")
	}
}
