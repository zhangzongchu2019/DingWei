package m8

import "testing"

// T-071 验收：裸 @token 经模糊匹配投给意外 Agent。
// 四格逐条对应卡片 verify_level。
func TestT071_Acceptance(t *testing.T) {
	const src = "zzc-prodops-2642"

	// ① 阳性：与某席短名存在 ≥3 字符**子串**关系、但并非其短名的 token，不得被投给该席。
	//    历史成因：`@zzc` 曾以中间子串命中 coder56sol**zzc**。
	for _, tok := range []string{"zzc", "sol", "56sol", "us50"} {
		if ownerSessionFuzzyMatch("coder56solzzc", tok) {
			t.Errorf("① 中间子串 %q 不应命中 coder56solzzc（T-071 回归）", tok)
		}
		if ownerSessionFuzzyMatch("opus50ts", tok) && tok != "us50" {
			t.Errorf("① 中间子串 %q 不应命中 opus50ts", tok)
		}
	}

	// ② 反向对照：真实短名与合法前缀仍须能命中 —— 否则等于把功能关掉冒充修好。
	for _, ok := range []struct{ session, req string }{
		{"coder56solzzc", "coder56solzzc"}, {"coder56solzzc", "coder"},
		{"opus50ts", "opus50ts"}, {"opus50ts", "opus"},
		{"prodops", "prodops"}, {"prodops", "prod"},
	} {
		if !ownerSessionFuzzyMatch(ok.session, ok.req) {
			t.Errorf("② 合法前缀 %q 应命中 %q，却没有（功能被关掉）", ok.req, ok.session)
		}
	}

	// ③ 过短 token 不得进入模糊匹配（<3 字符直接不匹配，避免 @zz 命中一片）
	for _, tok := range []string{"z", "zz", "op"} {
		if ownerSessionFuzzyMatch("opus50ts", tok) {
			t.Errorf("③ 过短 token %q 不应进入模糊匹配", tok)
		}
	}

	// ④ 两条入口：#选择器 与 @提及 最终都落到同一解析，故解析层一致性须成立。
	for _, in := range []string{"#opus50ts hi", "@opus50ts hi"} {
		got, _ := parseAMRCandidatesDetailed(in, src, 20)
		if len(got) != 1 || got[0].selector != "#opus50ts" {
			t.Errorf("④ 入口 %q → 候选=%d %v，两条入口应等价", in, len(got), got)
		}
	}
}
