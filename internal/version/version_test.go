package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestVersionMatchesRepositoryVersionFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Version, strings.TrimSpace(string(data)); got != want {
		t.Fatalf("version constant=%q VERSION=%q", got, want)
	}
}

// 🔴 防的是「发了新版却忘了抬版本号」——2026-09-19 实测踩过:
// 0.2.0.7 上线后现网自报仍是 0.2.0.5(commit 是新的、version 是旧的),
// 而 TestVersionMatchesRepositoryVersionFile 当时是绿的:
// 它只焊「VERSION 文件 == Go 常量」,两处一致地都不改,它防不住。
// 本用例把常量与【上一个已发版本】焊住,构成另一维。
func TestVersionIsAheadOfLastReleasedVersion(t *testing.T) {
	const lastReleased = "0.2.1.7" // 0.2.1.7 已发布;下次发版把它抬到上一个已发号
	cur, prev := parseQuad(t, Version), parseQuad(t, lastReleased)
	if !(cur[0] > prev[0] ||
		(cur[0] == prev[0] && cur[1] > prev[1]) ||
		(cur[0] == prev[0] && cur[1] == prev[1] && cur[2] > prev[2]) ||
		(cur[0] == prev[0] && cur[1] == prev[1] && cur[2] == prev[2] && cur[3] > prev[3])) {
		t.Fatalf("Version=%q 未高于上一个已发版本 %q —— 发新版时忘了抬号?", Version, lastReleased)
	}
}

func parseQuad(t *testing.T, v string) [4]int {
	t.Helper()
	var q [4]int
	// 🔴 不能只用 Sscanf:它读满四段就返回成功,"0.2.0.7.1" 照样 n==4 无 err。
	//    2026-09-19 实测:本函数首版就是这么写的,五段号那发变异【没红】——
	//    报错文案写了"五段号会解析失败",而判据根本没盖住它。
	//    ⇒ 先按字面切分、卡死段数,再逐段转整数。
	parts := strings.Split(v, ".")
	if len(parts) != 4 {
		t.Fatalf("版本号必须四段 A.B.C.D,实得 %q(%d 段);五段号会让 Hub 强制校验解析失败", v, len(parts))
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || p == "" || (len(p) > 1 && p[0] == '0') {
			t.Fatalf("版本号第 %d 段非法:%q(整段 %q)", i+1, p, v)
		}
		q[i] = n
	}
	return q
}

// 🔴 三处版本号必须一致。既有的 TestVersionMatchesRepositoryVersionFile 只焊了
// VERSION 与 Go 常量【两处】;helper 的 __version__ 是第三处,不在任何仓内闸里 ——
// 2026-09-19 装配 0.2.1.0 时组长抬了前两处、漏了第三处,
// 由出单时的 preflight 第⑭闸拦下(T-162 记着那道闸当初也是被执行方拦出来的)。
// ⇒ 把它焊进仓内:出单前就能红,而不是等到出单那一刻。
func TestHelperVersionMatchesGoVersion(t *testing.T) {
	raw, err := os.ReadFile("../../tools/sessionhelper/sessionhelper/__init__.py")
	if err != nil {
		t.Fatalf("读不到 helper 版本文件:%v —— 查不了不等于通过", err)
	}
	m := regexp.MustCompile(`__version__\s*=\s*"([0-9.]+)"`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("helper __init__.py 里找不到 __version__")
	}
	if got := string(m[1]); got != Version {
		t.Fatalf("helper __version__=%q 与 Go const Version=%q 不一致 —— 装上去会自报错误版本", got, Version)
	}
}
