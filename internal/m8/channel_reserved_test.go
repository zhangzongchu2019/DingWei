package m8

import "testing"

// 🔴 覆盖业主 2026-09-19 对 Q1 的三条裁定,每条一格,且两侧都钉:
//
//	① 渠道名是保留字(命中)         ② 非渠道名不得被误判为渠道(不命中)
//	③ 保留字不得用作业主名/短名     ④ 业主名在其名下保留(业主域内)
//
// 只钉①③是半条:一个「一律返回 true」的实现能让①③全过,而②④全坏。
func TestChannelReservedNamesAndNamingRules(t *testing.T) {
	for _, name := range []string{"飞书", "feishu", "FeiShu", " fs ", "lark", "企微", "wecom", "slack", "google"} {
		if _, ok := channelOfMention(name); !ok {
			t.Fatalf("渠道名 %q 必须是保留字 —— 否则 @%s#账号 会被当成 @业主名#会话名", name, name)
		}
		if !isReservedForNaming(name) {
			t.Fatalf("保留字 %q 必须禁止用作业主名/Agent 短名", name)
		}
	}
	// 🔴 反向:普通名字不得被误判成渠道,否则 @某业主#某会话 会被路由去外部渠道
	for _, name := range []string{"zzc", "opus50ts", "coder56solzzc", "prodops", "", "feishu2", "飞书群"} {
		if _, ok := channelOfMention(name); ok {
			t.Fatalf("%q 不是渠道名,却被判为保留字 —— 正常 Agent 寻址会被劫持到渠道", name)
		}
		if isReservedForNaming(name) {
			t.Fatalf("%q 不该被禁止用作命名", name)
		}
	}
}

func TestOwnerScopedReservationIsPerOwner(t *testing.T) {
	// 业主名在自己名下保留:zzc 名下的 Agent 不得叫 zzc
	if !isOwnerScopedReserved("zzc", "zzc") || !isOwnerScopedReserved("zzc", " ZZC ") {
		t.Fatal("业主名在其名下必须保留 —— 否则 @zzc#zzc 无从分辨")
	}
	// 🔴 但它是【业主域内】的:别的业主名下叫 zzc 不受影响
	if isOwnerScopedReserved("other", "zzc") {
		t.Fatal("业主名保留必须限于该业主域内,不得跨业主生效")
	}
	if isOwnerScopedReserved("zzc", "opus50ts") || isOwnerScopedReserved("zzc", "") {
		t.Fatal("非同名不得判为保留")
	}
}
