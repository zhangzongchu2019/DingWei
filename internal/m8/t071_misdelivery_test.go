package m8

import (
	"strings"
	"testing"
)

// T-071:裸 @token 经模糊匹配投给意外 Agent,且向发信方报「成功」。
// 两半修复必须分别有判据:①收紧匹配 ②让实际去向可见。
// 🔴 只做①不做②仍然危险 —— 前缀匹配同样是猜测,猜错时发信方依旧看不见。

// ① 收紧匹配:今晚真实事故的复现场景
func TestT071_MiddleSubstringNoLongerMatches(t *testing.T) {
	// 今晚实测:@zzc 命中 coder56solzzc,把发给业主的整段话投给了 coder。
	if ownerSessionFuzzyMatch("zzc-coder56solzzc-2642", "zzc") {
		t.Fatal("T-071 回归:`zzc` 仍命中 coder56solzzc")
	}
	// 业主 key 前缀是最自然会被写出的 token,必须覆盖三席实名
	for _, s := range []string{"zzc-opus50ts-2642", "zzc-prodops-2642", "zzc-coder56solzzc-2642"} {
		if ownerSessionFuzzyMatch(s, "zzc") {
			t.Fatalf("`zzc` 不应命中任何会话,却命中 %s", s)
		}
	}
}

// 反向对照:前缀简写必须仍然有效 —— 否则等于把功能关掉冒充修好
func TestT071_PrefixAbbreviationStillWorks(t *testing.T) {
	cases := []struct{ session, req string }{
		{"zzc-opus50ts-2642", "opus"},
		{"zzc-prodops-2642", "prod"},
		{"zzc-coder56solzzc-2642", "coder"},
		{"zzc-opus50ts-2642", "opus50ts"}, // 全短名
	}
	for _, c := range cases {
		if !ownerSessionFuzzyMatch(c.session, c.req) {
			t.Fatalf("前缀简写 %q → %q 失效,修过头了", c.req, c.session)
		}
	}
}

// 边界:不足 3 字符仍不参与模糊匹配(既有规则,不得被本次改动带坏)
func TestT071_ShortRequestStillExcluded(t *testing.T) {
	if ownerSessionFuzzyMatch("zzc-prodops-2642", "pr") {
		t.Fatal("两字符请求参与了模糊匹配")
	}
}

// ② 可见性:解析结果与写法不同时,投递结果必须摊开实际去向
func TestT071_RouteResultShowsResolvedTarget(t *testing.T) {
	got := amrResolvedShort("zzc-coder56solzzc-2642#FB-zzc-devteam-e0d12642")
	if got != "coder56solzzc" {
		t.Fatalf("未能从完整地址取出短名:%q", got)
	}
	if amrResolvedShort("") != "" {
		t.Fatal("空地址应返回空,不得编造")
	}
}

// 🔴 阳性对照:确认上面那个取短名的判据**能区分真伪** ——
// 若它对任何输入都返回同一个值,前一个测试就是空的。
func TestT071_ResolvedShortIsDiscriminating(t *testing.T) {
	a := amrResolvedShort("zzc-coder56solzzc-2642#k")
	b := amrResolvedShort("zzc-opus50ts-2642#k")
	if a == b {
		t.Fatalf("取短名对不同会话返回了相同结果 %q —— 判据无分辨力", a)
	}
	if strings.Contains(a, "#") || strings.Contains(b, "#") {
		t.Fatal("短名里仍带 key 段")
	}
}
