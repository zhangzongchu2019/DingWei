package m8

import (
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-FEISHU-OWNER-ID-FIX：新平台「＠飞书＃<名字>」名字匹配不到时，兜底发业主本人——业主飞书取自身份库绑定
// （与 console「飞书已绑定」、与只写「＠飞书」时同一个取法），不取旧平台成员表。
// 旧平台的兜底与唯一命中直发由 channel_wire_test.go 的
// TestChannelDeliveryMissingNameFallsBackWithNotFoundNote／唯一命中格继续钉住（不退化）。

func newPlatformChannelEnv(t *testing.T, bindFeishu bool) (*agentEnv, string) {
	t.Helper()
	e := newAgentEnv(t)
	e.hub.RegisterBot("ch-alpha", "alpha-bot")
	if bindFeishu {
		if err := e.svc.Store.CreateRegistrationCode(e.ctx, "zzc", "digest-owner-bind", time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Store.RedeemRegistrationCode(e.ctx, "digest-owner-bind",
			identity.Identity{Provider: "feishu", Subject: "ou_np_owner"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return e, AgentKeyID("zzc")
}

func TestNewPlatformChannelNameFallbackUsesIdentityFeishu(t *testing.T) {
	e, keyID := newPlatformChannelEnv(t, true)
	p, routed := channelProcessor(e.hub, keyID)

	err := p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "测试消息"},
		amrCandidate{channel: "feishu", channelAddr: "张宗楚", body: "测试消息", replyPolicy: "no_reply"})

	if err != nil || len(*routed) != 1 {
		t.Fatalf("新平台名字兜底应投出一条: err=%v routed=%d", err, len(*routed))
	}
	got := (*routed)[0]
	if !strings.HasPrefix(got.To, "ou_np_owner#") {
		t.Fatalf("兜底须发到身份库里业主的飞书绑定, to=%q", got.To)
	}
	if metaBool(got.Meta, "channel_fell_back") != true {
		t.Fatal("名字未命中走兜底，必须标记 channel_fell_back")
	}
	if want := "未找到名为「张宗楚」的飞书账号或群，因此发给业主本人"; !strings.HasSuffix(got.Body, want) {
		t.Fatalf("兜底正文末尾说明丢失: body=%q", got.Body)
	}
}

func TestNewPlatformChannelNameFallbackWithoutIdentityFailsVisibly(t *testing.T) {
	e, keyID := newPlatformChannelEnv(t, false)
	p, routed := channelProcessor(e.hub, keyID)

	err := p.deliverChannelCandidate(amrRouteJob{key: channelJob(keyID), body: "x"},
		amrCandidate{channel: "feishu", channelAddr: "张宗楚", body: "x", replyPolicy: "no_reply"})

	if len(*routed) != 0 {
		t.Fatalf("业主未绑定飞书时不得凭空投递, routed=%d", len(*routed))
	}
	if err != errChannelNoTarget {
		t.Fatalf("业主未绑定飞书须仍是可见失败 errChannelNoTarget, err=%v", err)
	}
}

func TestNewPlatformChannelFallbackPrefersIdentityOverLegacyMemberRow(t *testing.T) {
	e, keyID := newPlatformChannelEnv(t, true)
	// 旧平台成员表里恰有同一属主的一行（历史残留）：新平台必须以身份库为准，不取它。
	if err := e.hub.Repo.UpsertMember(e.ctx, model.Member{OwnerKey: "zzc", DisplayName: "别名", FeishuOpenID: "ou_legacy_row", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	r, err := e.hub.channelDeliveryTarget(e.ctx, keyID, "张宗楚")
	if err != nil || r.openID != "ou_np_owner" || !r.fellBack {
		t.Fatalf("新平台兜底须取身份库: %+v err=%v", r, err)
	}
	// 只写「＠飞书」与名字兜底必须是同一个答案。
	empty, err := e.hub.channelDeliveryTarget(e.ctx, keyID, "")
	if err != nil || empty.openID != r.openID {
		t.Fatalf("空地址与名字兜底不一致: empty=%+v name=%+v", empty, r)
	}
}
