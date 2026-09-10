package m8

import "strings"
import "testing"

// T-055: Markdown 标题不得被当成 legacy 选择器,且不得置 lookedLikeCandidate。
// ⚠️ 只改解析不改这个标志等于没修 —— 假的投递失败通知照发。
func TestMarkdownHeadingIsNotCandidate(t *testing.T) {
	const me = "zzc-opus50ts-2642"
	headings := []struct{ name, text string }{
		{"h1_ascii", "# Deployment complete\n\nbody"},
		{"h1_cjk_emoji", "# ✅ 飞书桥接迁移部署完成，端到端已跑通\n\nprodops 报回"},
		{"h2", "## 一、你要的①②\n\n正文"},
		{"h3", "### 三级标题\n\n正文"},
		{"h4", "#### 四级标题\n\n正文"},
		{"hash_tab", "#\tTab 开头\n\n正文"},
		{"lone_hash", "#"},
	}
	for _, c := range headings {
		t.Run(c.name, func(t *testing.T) {
			got, looked := parseAMRCandidatesDetailed(c.text, me, 8)
			if len(got) != 0 {
				t.Fatalf("标题被解析成了 %d 个目标: %+v", len(got), got)
			}
			if looked {
				t.Fatalf("lookedLikeCandidate=true ⇒ 仍会回一条假的投递失败通知")
			}
		})
	}
}

// 反向对照:真选择器必须仍然被识别。没有这一格,上面的测试可以靠「全判 false」通过。
func TestRealSelectorStillParsed(t *testing.T) {
	got, looked := parseAMRCandidatesDetailed("#prodops 请核对 sha256", "zzc-opus50ts-2642", 8)
	if !looked {
		t.Fatalf("真选择器被判成非候选")
	}
	if len(got) != 1 || got[0].selector != "#prodops" || got[0].body != "请核对 sha256" {
		t.Fatalf("真选择器解析错误: %+v", got)
	}
}

// 反向对照二:@提及主路径不受本次改动影响。
func TestAtMentionUnaffected(t *testing.T) {
	got, looked := parseAMRCandidatesDetailed("@prodops 请核对 sha256", "zzc-opus50ts-2642", 8)
	if !looked || len(got) != 1 || got[0].selector != "#prodops" {
		t.Fatalf("@提及解析被破坏: looked=%v got=%+v", looked, got)
	}
}

// T-060: 失败通知须回显真实冒犯 token,不得是占位字面量 "candidate"。
func TestOffendingTokenNotPlaceholder(t *testing.T) {
	cases := []struct{ in, want string }{
		{"@不存在的中文名 正文", "@不存在的中文名"},
		{"@a 正文", "@a"},
		{"@", "@"},
		{"   ", "(空)"},
		{"@" + strings.Repeat("x", 200), "@" + strings.Repeat("x", 63) + "…"},
	}
	for _, c := range cases {
		if got := amrOffendingToken(c.in); got != c.want {
			t.Fatalf("amrOffendingToken(%.20q) = %q, want %q", c.in, got, c.want)
		}
		if got := amrOffendingToken(c.in); got == "candidate" {
			t.Fatalf("仍在回显占位字面量")
		}
	}
}

// 通知正文不得夹带原始正文(凭据泄漏面)。
func TestOffendingTokenDoesNotLeakBody(t *testing.T) {
	got := amrOffendingToken("@bad SH_SECRET=abcdef123456 剩余正文")
	if strings.Contains(got, "SH_SECRET") || strings.Contains(got, "剩余正文") {
		t.Fatalf("通知回显泄漏了正文: %q", got)
	}
}
