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
	for _, address := range []string{"ou_realuser", "oc_realgroup"} {
		t.Run(address[:2], func(t *testing.T) {
			p, routed := channelProcessor(hub, keyID)
			p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "按 ID 发"},
				amrCandidate{channel: "feishu", channelAddr: address, body: "按 ID 发", replyPolicy: "no_reply"})
			if len(*routed) != 1 {
				t.Fatalf("应投出一条,实测 %d", len(*routed))
			}
			got := (*routed)[0]
			if !strings.HasPrefix(got.To, address+"#") {
				t.Fatalf("🔴 ID 解析成功时必须直投,不得误兜底到业主。to=%q", got.To)
			}
			if metaBool(got.Meta, "channel_fell_back") || got.Body != "按 ID 发" {
				t.Fatalf("🔴 ID 寻址不得被名字兜底改写: meta=%v body=%q", got.Meta, got.Body)
			}
		})
	}
}

func TestChannelDeliveryUniqueNameSelectsNonFirstMember(t *testing.T) {
	hub, keyID, ctx := channelTestHub(t)
	for _, member := range []model.Member{
		{OwnerKey: "a-owner", DisplayName: "甲目标", FeishuOpenID: "ou_first", Active: true},
		{OwnerKey: "b-owner", DisplayName: "目标乙", FeishuOpenID: "ou_second", Active: true},
		{OwnerKey: "c-owner", DisplayName: "终目标", FeishuOpenID: "ou_third", Active: true},
		{OwnerKey: "inactive-owner", DisplayName: "目标乙", FeishuOpenID: "ou_inactive", Active: false},
	} {
		if err := hub.Repo.UpsertMember(ctx, member); err != nil {
			t.Fatal(err)
		}
	}
	p, routed := channelProcessor(hub, keyID)
	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "唯一命中正文"},
		amrCandidate{channel: "feishu", channelAddr: "目标乙", body: "唯一命中正文", replyPolicy: "no_reply"})
	if len(*routed) != 1 {
		t.Fatalf("唯一名字应投出一条,实测 %d", len(*routed))
	}
	got := (*routed)[0]
	if !strings.HasPrefix(got.To, "ou_second#") {
		t.Fatalf("必须投给三条成员中的第二条,不得固定取首项: to=%q", got.To)
	}
	if metaBool(got.Meta, "channel_fell_back") || got.Body != "唯一命中正文" {
		t.Fatalf("唯一名字寻址不应伪装成兜底: meta=%v body=%q", got.Meta, got.Body)
	}
}

func TestChannelDeliveryUniqueGroupNameRoutesToGroup(t *testing.T) {
	hub, keyID, ctx := channelTestHub(t)
	if err := hub.Repo.UpsertChatEntity(ctx, model.ChatEntity{ID: "bot:group:oc_named", BotChannelID: "bot", Type: model.ChatGroup, FeishuID: "oc_named", DisplayName: "发布群", Active: true}); err != nil {
		t.Fatal(err)
	}
	p, routed := channelProcessor(hub, keyID)
	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "群正文"},
		amrCandidate{channel: "feishu", channelAddr: "发布群", body: "群正文", replyPolicy: "no_reply"})
	if len(*routed) != 1 || !strings.HasPrefix((*routed)[0].To, "oc_named#") {
		t.Fatalf("唯一群名必须直投该群: routed=%+v", *routed)
	}
	if metaBool((*routed)[0].Meta, "channel_fell_back") || (*routed)[0].Body != "群正文" {
		t.Fatalf("唯一群名不应带兜底痕迹: meta=%v body=%q", (*routed)[0].Meta, (*routed)[0].Body)
	}
}

func TestChannelDeliveryDuplicateNameFallsBackWithOwnershipNote(t *testing.T) {
	hub, keyID, ctx := channelTestHub(t)
	if err := hub.Repo.UpsertMember(ctx, model.Member{OwnerKey: "person-owner", DisplayName: "同名目标", FeishuOpenID: "ou_duplicate", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.Repo.UpsertChatEntity(ctx, model.ChatEntity{ID: "bot:group:oc_duplicate", BotChannelID: "bot", Type: model.ChatGroup, FeishuID: "oc_duplicate", DisplayName: "同名目标", BoundOwner: "group-owner", Active: true}); err != nil {
		t.Fatal(err)
	}
	p, routed := channelProcessor(hub, keyID)
	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "重名正文"},
		amrCandidate{channel: "feishu", channelAddr: "同名目标", body: "重名正文", replyPolicy: "no_reply"})
	if len(*routed) != 1 {
		t.Fatalf("重名应兜底投出一条,实测 %d", len(*routed))
	}
	got := (*routed)[0]
	wantNote := "因为「group-owner」和「person-owner」同名，无法决定，因此发给业主本人"
	if !strings.HasPrefix(got.To, "ou_owner#") || !metaBool(got.Meta, "channel_fell_back") {
		t.Fatalf("重名必须发给业主本人: to=%q meta=%v", got.To, got.Meta)
	}
	if !strings.HasSuffix(got.Body, wantNote) {
		t.Fatalf("重名正文末尾未说明判定依据: body=%q want suffix=%q", got.Body, wantNote)
	}
	if got.Body == "重名正文" {
		t.Fatal("重名兜底与寻址成功在正文上不可区分")
	}
}

func TestChannelDeliveryMissingNameFallsBackWithNotFoundNote(t *testing.T) {
	hub, keyID, _ := channelTestHub(t)
	p, routed := channelProcessor(hub, keyID)

	p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "零命中正文"},
		amrCandidate{channel: "feishu", channelAddr: "不存在的名字", body: "零命中正文", replyPolicy: "no_reply"})

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
	wantNote := "未找到名为「不存在的名字」的飞书账号或群，因此发给业主本人"
	if !strings.HasSuffix(got.Body, wantNote) {
		t.Fatalf("零命中正文末尾未说明判定依据: body=%q want suffix=%q", got.Body, wantNote)
	}
	if got.Body == "零命中正文" {
		t.Fatal("零命中兜底与寻址成功在正文上不可区分")
	}
}

func TestFeishuNameLookupSkipsMemberDisplayNameThatIsAddressableID(t *testing.T) {
	hub, _, ctx := channelTestHub(t)
	if err := hub.Repo.UpsertMember(ctx, model.Member{OwnerKey: "prefill-owner", DisplayName: "ou_prefilled", FeishuOpenID: "ou_prefilled", Active: true}); err != nil {
		t.Fatal(err)
	}
	matches, _, err := hub.resolveFeishuName(ctx, "ou_prefilled")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("ID 形态的 display_name 不得再次进入名字匹配: %+v", matches)
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
