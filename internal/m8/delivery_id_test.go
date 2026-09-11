package m8

import (
	"strings"
	"testing"
)

// helperDeliveryIDRule 逐字复刻 sessionhelper/unified_queue.py:_delivery_id 的前缀规则:
//
//	return raw if raw.startswith("dw-") else f"dw-{raw}"
//
// 复刻而非调用,是因为两侧是不同语言的独立实现;此处要钉住的正是**两个实现是否同形**。
func helperDeliveryIDRule(raw string) string {
	if strings.HasPrefix(raw, "dw-") {
		return raw
	}
	return "dw-" + raw
}

// TestDeliveryIDSurvivesHelperPrefixing 是 T-074 的根因回归:
// Hub 生成的投递号,经 helper 的前缀规则后必须**逐字不变**。
// 一旦有人把 deliveryIDPrefix 改回 "dlv_",此测试立即变红。
func TestDeliveryIDSurvivesHelperPrefixing(t *testing.T) {
	hubSide := deliveryIDPrefix + randomHex(16)
	helperSide := helperDeliveryIDRule(hubSide)
	if helperSide != hubSide {
		t.Fatalf("投递号在 helper 侧被改写,渲染器将同时见到两种写法(T-074 根因)\n  Hub   =%s\n  helper=%s", hubSide, helperSide)
	}
}

// TestDeliveryIDPrefixIsIdempotent 阳性对照:
// 若规则本身失效(对任何输入都原样返回),上面那条测试就变成「不可能失败的检查」。
// 本条确保该规则**确实会改写**未带前缀的串 —— 即上面那条通过是有意义的。
func TestDeliveryIDPrefixIsIdempotent(t *testing.T) {
	legacy := "dlv_" + randomHex(16)
	if got := helperDeliveryIDRule(legacy); got == legacy {
		t.Fatalf("前缀规则对未带前缀的串未生效,说明该规则复刻有误,上条测试不可信: %s", got)
	}
	if got := helperDeliveryIDRule(legacy); got != "dw-"+legacy {
		t.Fatalf("前缀规则与 helper 实现不一致: got=%s want=%s", got, "dw-"+legacy)
	}
}
