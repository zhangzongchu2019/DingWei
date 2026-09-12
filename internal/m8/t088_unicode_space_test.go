package m8

import "testing"

// T-088:token 切分须按 Unicode 空白，不能只认 ASCII 四件套。
func TestT088_UnicodeSpaceTokenSplit(t *testing.T) {
	const src = "zzc-prodops-2642"
	sp := func(r rune) string { return string(r) }

	// ① 防回归:半角空白仍正常
	for _, r := range []rune{0x20, 0x09, 0x0D, 0x0A} {
		got, _ := parseAMRCandidatesDetailed("@opus50ts"+sp(r)+"@coder56solzzc hi", src, 20)
		if len(got) != 2 {
			t.Errorf("① U+%04X 半角空白:候选=%d want=2", r, len(got))
		}
	}

	// ② 新能力:Unicode 空白也能切开（多目标）
	for _, r := range []rune{0x00A0, 0x2009, 0x3000} {
		got, _ := parseAMRCandidatesDetailed("@opus50ts"+sp(r)+"@coder56solzzc hi", src, 20)
		if len(got) != 2 {
			t.Errorf("② U+%04X Unicode 空白:候选=%d want=2", r, len(got))
		}
	}

	// ③ 🔴 要害:单目标 + 全角空格 —— 中文写法最常见的形态
	for _, r := range []rune{0x00A0, 0x2009, 0x3000} {
		got, _ := parseAMRCandidatesDetailed("@prodops"+sp(r)+"请复核", "zzc-opus50ts-2642", 20)
		if len(got) != 1 {
			t.Errorf("③ U+%04X 单目标+Unicode 空白:候选=%d want=1", r, len(got))
		} else if got[0].selector != "#prodops" {
			t.Errorf("③ U+%04X selector=%q want=#prodops", r, got[0].selector)
		}
	}

	// ④ 防误伤:完全不含 @ 的正文仍判非候选，且不报"看起来像候选"
	if got, looked := parseAMRCandidatesDetailed("这是一段普通正文，没有提及任何人。", src, 20); len(got) != 0 || looked {
		t.Errorf("④ 纯正文:候选=%d looked=%v want=0/false", len(got), looked)
	}

	// ⑤ 失败通知回显的首个 token 也按 Unicode 空白切（否则会回显整句）
	if tok := amrOffendingToken("@不合规名字　后面还有一整句话不应被回显"); tok != "@不合规名字" {
		t.Errorf("⑤ amrOffendingToken=%q want=@不合规名字", tok)
	}
}
