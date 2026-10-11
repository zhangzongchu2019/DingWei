package m8

import (
	"errors"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新平台消息可达性改读 np_group（qwen 复验 🟡第七节第 3 条，组长 15:22:39Z ①）。
// 夹具：批准时每个 Agent 各自就地建了一个组（t<n>），所以初始时互不同组。
func TestNewPlatformGroupingUsesNpGroup(t *testing.T) {
	v := newViewEnv(t) // zzc：Opus50TS、Kimi；lbl：kimi
	ctx := v.ctx
	id := func(owner, nameLC string) string {
		ag, err := v.svc.Store.FindAgentByName(ctx, owner, nameLC)
		if err != nil {
			t.Fatal(err)
		}
		return ag.ID
	}
	opus, kimiZ, kimiL := id("zzc", "opus50ts"), id("zzc", "kimi"), id("lbl", "kimi")
	agentFrom := func(key, session string) address {
		return address{Kind: addressSession, KeyID: key, SessionName: session}
	}
	check := func(name string, from address, env model.Envelope, toKey, toSession string, wantReason string) {
		t.Helper()
		err := v.hub.enforceAgentGrouping(ctx, from, env, toKey, toSession)
		switch {
		case wantReason == "" && err != nil:
			t.Errorf("%s: denied (%v), want allowed", name, err)
		case wantReason != "" && (!errors.Is(err, errGroupAgentNotFound) || groupingRejectionReason(err) != wantReason):
			t.Errorf("%s: %v (reason %q), want rejection %q", name, err, groupingRejectionReason(err), wantReason)
		}
	}
	none := model.Envelope{}
	// 旧 agent_group 表在新平台下恒空 —— 旧逻辑会把它当迁移态一律放行；新平台必须照判
	check("different groups, same owner", agentFrom("u_zzc", "opus50ts"), none, "u_zzc", "kimi", "cross_group_denied")
	check("different groups, other owner", agentFrom("u_zzc", "opus50ts"), none, "u_lbl", "kimi", "cross_group_denied")
	check("target not an agent", agentFrom("u_zzc", "opus50ts"), none, "u_zzc", "ghost", "target_endpoint_not_found")
	// 同组 ⇒ 互通（跨属主也通）
	codes, _ := v.svc.Store.ListAgentGroups(ctx, opus)
	if err := v.svc.JoinGroup(ctx, "zzc", codes[0], kimiZ); err != nil {
		t.Fatal(err)
	}
	if err := v.svc.JoinGroup(ctx, "lbl", codes[0], kimiL); err != nil {
		t.Fatal(err)
	}
	check("same group, same owner", agentFrom("u_zzc", "opus50ts"), none, "u_zzc", "kimi", "")
	check("same group, other owner", agentFrom("u_lbl", "kimi"), none, "u_zzc", "opus50ts", "")
	// Hub 写的真实发送方优先于线上地址
	rewritten := model.Envelope{Meta: map[string]any{"source_key_id": "u_zzc", "source_session_name": "opus50ts"}}
	check("source meta wins", agentFrom("u_lbl", "nosuch"), rewritten, "u_lbl", "kimi", "")
	// 不在任何组 ⇒ 属主默认组：同属主互通、跨属主不通
	for _, c := range []struct{ owner, ag string }{{"lbl", kimiL}} {
		if err := v.svc.LeaveGroup(ctx, c.owner, codes[0], c.ag); err != nil {
			t.Fatal(err)
		}
	}
	lblOwn, _ := v.svc.Store.ListAgentGroups(ctx, kimiL)
	for _, c := range lblOwn { // 退掉 lbl 的 kimi 自己那个夹具组 —— 它是组长退不了，改为删组
		if err := v.svc.DeleteGroup(ctx, "lbl", c); err != nil {
			t.Fatal(err)
		}
	}
	_, gammaID := v.enrollFor("lbl", "dev-g", "Gamma")
	gammaOwn, _ := v.svc.Store.ListAgentGroups(ctx, gammaID)
	for _, c := range gammaOwn {
		if err := v.svc.DeleteGroup(ctx, "lbl", c); err != nil {
			t.Fatal(err)
		}
	}
	check("both groupless have no shared group", agentFrom("u_lbl", "kimi"), none, "u_lbl", "gamma", "cross_group_denied")
	check("groupless vs grouped other owner", agentFrom("u_lbl", "kimi"), none, "u_zzc", "opus50ts", "cross_group_denied")
	// 两个都不在任何组、但属主不同 ⇒ 各落自己的默认组，不互通（默认组按属主分，不把所有属主并成一组）
	kimiZOwn, _ := v.svc.Store.ListAgentGroups(ctx, kimiZ)
	for _, c := range kimiZOwn {
		if c == codes[0] {
			if err := v.svc.LeaveGroup(ctx, "zzc", c, kimiZ); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := v.svc.DeleteGroup(ctx, "zzc", c); err != nil {
			t.Fatal(err)
		}
	}
	if left, _ := v.svc.Store.ListAgentGroups(ctx, kimiZ); len(left) != 0 {
		t.Fatalf("zzc kimi still in groups %v", left)
	}
	check("both groupless, different owners", agentFrom("u_lbl", "kimi"), none, "u_zzc", "kimi", "cross_group_denied")

	// 飞书发信人：映射到激活用户 ⇒ 豁免；未绑定 ⇒ 拒
	feishu := address{Kind: addressFeishu, OpenID: "ou_x"}
	fenv := func(open string) model.Envelope {
		return model.Envelope{Meta: map[string]any{"bot_channel_id": "devplatform", "sender_open_id": open}}
	}
	check("unbound feishu sender", feishu, fenv("ou_nobody"), "u_zzc", "kimi", "sender_not_active_member")
	if err := v.svc.Store.CreateUser(ctx, identity.User{UserID: "fei", ShortName: "fe", State: identity.UserPendingBind}); err != nil {
		t.Fatal(err)
	}
	if err := v.svc.Store.CreateRegistrationCode(ctx, "fei", "digest-fei", v.svc.Now().Add(identity.RegistrationCodeTTL)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.svc.Store.RedeemRegistrationCode(ctx, "digest-fei", identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_fei"}, v.svc.Now()); err != nil {
		t.Fatal(err)
	}
	check("bound active feishu sender", feishu, fenv("ou_fei"), "u_zzc", "kimi", "")
	if err := v.svc.Store.SetUserState(ctx, "fei", identity.UserDisabled, v.svc.Now()); err != nil {
		t.Fatal(err)
	}
	check("disabled feishu sender", feishu, fenv("ou_fei"), "u_zzc", "kimi", "sender_not_active_member")

	// view 页（view-v2）：console 用户是激活用户 ⇒ 豁免
	view := func(principal string) model.Envelope {
		return model.Envelope{Meta: map[string]any{hubIngressMetaKey: hubIngressViewV2, "console_principal_id": principal}}
	}
	check("view-v2 active principal", agentFrom("u_zzc", "x"), view("zzc"), "u_zzc", "kimi", "")
	check("view-v2 disabled principal", agentFrom("u_zzc", "x"), view("fei"), "u_zzc", "kimi", "sender_not_active_member")
	check("view-v2 no principal", agentFrom("u_zzc", "x"), view(""), "u_zzc", "kimi", "sender_not_active_member")
}
