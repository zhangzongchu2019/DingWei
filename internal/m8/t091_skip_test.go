package m8

import "testing"

// T-091(业主裁定):不合规 token 跳过而非整轮拒绝。
func TestT091_SkipUnsupportedToken(t *testing.T) {
	const src = "zzc-prodops-2642"
	chk := func(in string, wantN int, wantFirst string) {
		got, looked := parseAMRCandidatesDetailed(in, src, 20)
		if len(got) != wantN {
			t.Errorf("%q → 候选=%d want=%d looked=%v", in, len(got), wantN, looked)
			return
		}
		if wantN > 0 && got[0].selector != wantFirst {
			t.Errorf("%q → first=%q want=%q", in, got[0].selector, wantFirst)
		}
	}
	// ① 要害:英文+中文混排,英文那个必须照常投出(此前整轮为 0)
	chk("@opus50ts @张宗楚 hi", 1, "#opus50ts")
	chk("@张宗楚 @opus50ts hi", 1, "#opus50ts")
	// ② 纯中文名:仍无候选(暂不支持),但不得 panic
	chk("@张宗楚 请复核", 0, "")
	// ③ 防回归:纯英文多目标仍为 2
	chk("@opus50ts @coder56solzzc hi", 2, "#opus50ts")
	// ④ 防误伤:合法名+拼错名,合法的仍投出
	chk("@prodops @notarealsession-xx hi", 2, "#prodops")
	// ⑤ 与 T-088 叠加:全角空格 + 中文名混排
	chk("@opus50ts　@张宗楚 hi", 1, "#opus50ts")
}
