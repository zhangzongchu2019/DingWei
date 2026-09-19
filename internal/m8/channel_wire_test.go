package m8

// G ② 第二/三格(G2-a 接入解析 · G2-b 飞书投递与兜底)的判据。
//
// 🔴 每条都配反向/阳性对照:
//    · 渠道识别的反向 —— `@zzc#opus50ts` 不得被当成渠道
//    · 兜底的阳性对照 —— 解析成功时不得误兜底到业主
//    只验正向的话,「一律判为渠道」和「一律发业主」两种实现都能全绿。

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// ---- G2-a:解析层 ----

func TestChannelMentionParsesChannelsAndRejectsMembers(t *testing.T) {
	t.Run("渠道被识别", func(t *testing.T) {
		for _, text := range []string{"@飞书#oc_group 正文", "@feishu#ou_user 正文", "@fs 正文", "@FS 正文", "@lark#oc_g 正文"} {
			channel, _, body, ok := parseChannelMention(text)
			if !ok {
				t.Fatalf("%q 应被识别为渠道提及", text)
			}
			if channel != "feishu" {
				t.Fatalf("%q 规范化渠道名应为 feishu,实测 %q", text, channel)
			}
			if body != "正文" {
				t.Fatalf("%q 正文切分错,实测 %q", text, body)
			}
		}
	})
	t.Run("反向:业主名会话名不得被当成渠道", func(t *testing.T) {
		for _, text := range []string{"@zzc#opus50ts 正文", "@opus50ts 正文", "@feishuman#s1 正文", "@fsx 正文"} {
			if channel, _, _, ok := parseChannelMention(text); ok {
				t.Fatalf("%q 不是渠道,却被判为 %q —— 「一律判为渠道」的实现会在这里露出来", text, channel)
			}
		}
	})
	t.Run("地址切分", func(t *testing.T) {
		_, addr, _, ok := parseChannelMention("@飞书#oc_abc 正文")
		if !ok || addr != "oc_abc" {
			t.Fatalf("addr=%q ok=%t", addr, ok)
		}
		_, addr, _, ok = parseChannelMention("@飞书 正文")
		if !ok || addr != "" {
			t.Fatalf("无 # 时 addr 必须为空,实测 %q ok=%t", addr, ok)
		}
	})
}

func TestChannelMentionDoesNotBecomeAgentCandidate(t *testing.T) {
	// 渠道提及 ⇒ 渠道候选(selector 为空,不进 Agent 寻址)
	candidates, looked := parseAMRCandidatesDetailed("@飞书#oc_group 把这条发出去", "bee", 20)
	if len(candidates) != 1 || !looked {
		t.Fatalf("应产出一条候选,实测 %d looked=%t", len(candidates), looked)
	}
	if candidates[0].channel != "feishu" {
		t.Fatalf("必须是渠道候选,实测 channel=%q", candidates[0].channel)
	}
	if candidates[0].selector != "" {
		t.Fatalf("🔴 渠道候选不得带 Agent 选择器(否则会去 prepareSessionSelector 寻址),实测 %q", candidates[0].selector)
	}
	// 反向:成员提及仍然是 Agent 候选
	agent, _ := parseAMRCandidatesDetailed("@zzc#opus50ts 正文", "bee", 20)
	if len(agent) != 1 || agent[0].channel != "" || agent[0].selector != "@zzc#opus50ts" {
		t.Fatalf("成员提及必须仍走 Agent 寻址,实测 %+v", agent)
	}
}

func TestReservedChannelNameRejectedAtSessionRegistration(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if _, err := hub.registerSessionEndpoint(ctx, model.SessionEndpoint{OwnerKey: "owner", KeyID: "k1", SessionName: "飞书"}); err == nil {
		t.Fatal("🔴 渠道保留字不得用作会话短名 —— 不在写入口拦,歧义就从解析期搬进数据里")
	}
	// 反向:普通名字必须仍能注册
	if _, err := hub.registerSessionEndpoint(ctx, model.SessionEndpoint{OwnerKey: "owner", KeyID: "k1", SessionName: "bee"}); err != nil {
		t.Fatalf("普通短名必须仍可注册,实测 err=%v", err)
	}
}

// ---- G2-b:投递与兜底 ----

func channelTestHub(t *testing.T) (*Hub, string, context.Context) {
	t.Helper()
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc-ch", Name: "svc-ch", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc-ch", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "owner", DisplayName: "Owner", FeishuOpenID: "ou_owner", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	// 机器人注册顺序故意不是字母序:默认机器人必须按 a-z 取首,而不是按注册先后
	hub.RegisterBot("ch-zeta", "zeta-bot")
	hub.RegisterBot("ch-alpha", "alpha-bot")
	hub.RegisterBot("ch-mike", "mike-bot")
	return hub, key.ID, ctx
}

func channelProcessor(hub *Hub, keyID string) (*amrProcessor, *[]model.Envelope) {
	p := testAMRProcessor()
	p.hub = hub
	p.modes = map[string]amrMode{"bee": amrRoute}
	routed := &[]model.Envelope{}
	p.routeEnvelope = func(_ context.Context, env model.Envelope) error {
		*routed = append(*routed, env)
		return nil
	}
	return p, routed
}

func channelJob(keyID string) amrTurnKey {
	return amrTurnKey{KeyID: keyID, Session: "bee", DeliveryID: "dw-dlv_ch", TurnID: "t1"}
}

func TestChannelDeliveryNoAddressGoesToOwner(t *testing.T) {
	hub, keyID, _ := channelTestHub(t)
	p, routed := channelProcessor(hub, keyID)

	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "给业主"},
		amrCandidate{channel: "feishu", channelAddr: "", body: "给业主", replyPolicy: "no_reply"})

	if len(*routed) != 1 {
		t.Fatalf("应投出一条,实测 %d", len(*routed))
	}
	got := (*routed)[0]
	if !strings.HasPrefix(got.To, "ou_owner#") {
		t.Fatalf("只有 @飞书 时必须发给业主本人,to=%q", got.To)
	}
	if metaBool(got.Meta, "channel_fell_back") != true {
		t.Fatal("无地址时应标记为兜底")
	}
}

func TestChannelDeliveryAddressableIDIsNotFallenBack(t *testing.T) {
	hub, keyID, _ := channelTestHub(t)
	p, routed := channelProcessor(hub, keyID)

	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "发群"},
		amrCandidate{channel: "feishu", channelAddr: "oc_realgroup", body: "发群", replyPolicy: "no_reply"})

	if len(*routed) != 1 {
		t.Fatalf("应投出一条,实测 %d", len(*routed))
	}
	got := (*routed)[0]
	if !strings.HasPrefix(got.To, "oc_realgroup#") {
		t.Fatalf("🔴 阳性对照:解析成功时必须发给该群,不得误兜底到业主。to=%q", got.To)
	}
	if metaBool(got.Meta, "channel_fell_back") != false {
		t.Fatal("🔴 阳性对照:解析成功不得标记为兜底 ——「一律发业主」的实现会在这里露出来")
	}
}

func TestChannelDeliveryUnparsableAddressFallsBackToOwner(t *testing.T) {
	hub, keyID, _ := channelTestHub(t)
	p, routed := channelProcessor(hub, keyID)

	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "写错了"},
		amrCandidate{channel: "feishu", channelAddr: "张三的群", body: "写错了", replyPolicy: "no_reply"})

	if len(*routed) != 1 {
		t.Fatalf("应投出一条,实测 %d", len(*routed))
	}
	got := (*routed)[0]
	if !strings.HasPrefix(got.To, "ou_owner#") {
		t.Fatalf("解析失败必须兜底发业主本人,to=%q", got.To)
	}
	if metaBool(got.Meta, "channel_fell_back") != true {
		t.Fatal("兜底必须被标记出来,否则事后分不清这条是发对了还是兜底的")
	}
}

func TestChannelDeliveryDefaultBotIsAlphabeticallyFirst(t *testing.T) {
	hub, keyID, _ := channelTestHub(t)
	if got := hub.defaultBotName(); got != "alpha-bot" {
		t.Fatalf("🔴 默认机器人必须按名称 a-z 取首(注册顺序是 zeta/alpha/mike),实测 %q", got)
	}
	p, routed := channelProcessor(hub, keyID)
	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "x"},
		amrCandidate{channel: "feishu", channelAddr: "oc_g", body: "x", replyPolicy: "no_reply"})
	if len(*routed) != 1 || !strings.HasSuffix((*routed)[0].To, "#alpha-bot") {
		t.Fatalf("投递地址必须带默认机器人,to=%q", (*routed)[0].To)
	}
}

func TestChannelDeliveryWithoutOwnerOpenIDDoesNotRouteSilently(t *testing.T) {
	hub, _, _ := newTestHub(t) // 没有 member、没有绑定 ⇒ 取不到业主 open_id
	hub.RegisterBot("ch-alpha", "alpha-bot")
	p, routed := channelProcessor(hub, "k-unbound")

	err := p.deliverChannelCandidate(amrRouteJob{key: channelJob("k-unbound"), body: "x"},
		amrCandidate{channel: "feishu", channelAddr: "", body: "x", replyPolicy: "no_reply"})

	if len(*routed) != 0 {
		t.Fatalf("没有可投目标时不得凭空投递,实测 %d 条", len(*routed))
	}
	if err == nil {
		t.Fatal("没有可投目标必须返回错误(调用方据此记日志/通知),不得静默成功")
	}
}

// 装配级(判据 3):删掉 amr.go process() 里的渠道分流,本条必须转红。
func TestChannelDeliveryIsWiredIntoProcess(t *testing.T) {
	hub, keyID, _ := channelTestHub(t)
	p, routed := channelProcessor(hub, keyID)
	p.mu.Lock()
	p.stats.opened, p.stats.queued = 1, 1
	p.mu.Unlock()

	p.process(amrRouteJob{key: channelJob(keyID), body: "@飞书#oc_group 请发出去"})

	if len(*routed) != 1 {
		t.Fatalf("装配:process() 必须把渠道候选交给渠道投递,实测 %d 条 —— 删掉 amr.go 里那段分流时本条应转红", len(*routed))
	}
	if !strings.HasPrefix((*routed)[0].To, "oc_group#") {
		t.Fatalf("装配:投递目标错 to=%q", (*routed)[0].To)
	}
}

// 飞书入站侧:`@飞书#x` 不得走成员解析。
// 🔴 这条是我做完变异清点时补的 —— 我在 dispatchMemberMention 加了拦截,却没有任何用例盯着它:
//
//	删掉那段拦截时全绿。「改了但没人测」与判据 3 是同一形状。
func TestChannelMentionNotDispatchedAsMember(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	msg := model.Message{ID: "m1", BotChannelID: "ch-alpha", ChatType: model.ChatPersonal, ChatEntityID: "dev:personal:ou_sender", SenderOpenID: "ou_sender"}

	_, matched, err := hub.dispatchMemberMention(ctx, msg, "@飞书#oc_group 正文")
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Fatal("🔴 `@飞书#x` 不得被成员解析吃掉 —— resolveMemberByName 匹配 OwnerKey/DisplayName,一旦有人叫「飞书」就会被静默投给 Agent")
	}

	// 反向:普通成员提及仍应被这条路径处理(matched=true,哪怕找不到人也会给出 Reply)
	_, matched, err = hub.dispatchMemberMention(ctx, msg, "@zzc#opus50ts 正文")
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("成员提及必须仍由本路径处理,否则「拦截」是靠把整条路堵死实现的")
	}
}
