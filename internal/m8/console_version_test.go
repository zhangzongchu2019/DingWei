package m8

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	dwversion "github.com/zhangzongchu2019/dingwei/internal/version"
)

// 控制台版本角标(业主 2026-09-24 批「版本号显示 ---- 可以」)。
//
// 🔴 本组判据特别容易做成恒绿,故三条各自守一件事,缺一条都能被硬编码实现骗过:
//
//	① 页面显示值必须【跟着产品版本常量变】—— 用一个不可能与真版本相等的版本渲染,
//	   显示值必须等于它。硬编码实现下这条必红(它会继续显示真版本号)。
//	② 模板源码里【不得出现】当前版本号字面量 —— 直接堵死"写死在前端字符串里"这条路。
//	③ Handler 必须把【真常量】喂给渲染 —— 否则 ① 只证明了渲染函数好用,
//	   而页面上那行可能来自别处(比如一个写死的形参)。
//
// 🔴 刻意【不写】"页面上有版本号字样"这种断言:那种断言在硬编码实现下照样绿,
//
//	判据会弱于要求 —— 而"判据弱于要求"正是本次要避免的那一类。
var versionBadgeText = regexp.MustCompile(`<div id=app-version[^>]*>([^<]*)</div>`)

func badgeText(t *testing.T, html string) string {
	t.Helper()
	m := versionBadgeText.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("页面里找不到 id=app-version 这个角标元素")
	}
	return m[1]
}

// ① 显示值跟着产品版本常量走。
func TestConsolePageVersionFollowsProductVersionConstant(t *testing.T) {
	// 取一个【不可能与真版本相等】的值:真版本是四段数字,这里带字母后缀。
	const fakeVersion = "9.9.9.9-not-the-real-version"
	const fakeCommit = "abcdef1234567890"

	page := renderConsolePage(fakeVersion, fakeCommit)
	got := badgeText(t, page)
	if want := fakeVersion + " · abcdef1"; got != want {
		t.Fatalf("角标显示 %q,应为 %q —— 显示值没有跟着传入的版本变(硬编码?)", got, want)
	}
	if strings.Contains(page, dwversion.Version) {
		t.Fatalf("用假版本渲染,页面里却仍出现真版本号 %q ⇒ 某处把版本号写死了", dwversion.Version)
	}

	// commit 未注入时按 version.go 既有口径只显版本号:宁可不显示,也不显示可能是错的来源。
	if got := badgeText(t, renderConsolePage(fakeVersion, "unknown")); got != fakeVersion {
		t.Fatalf("commit 未注入时角标为 %q,应只显版本号 %q", got, fakeVersion)
	}
}

// ② 模板源码里不得有版本号字面量 —— 这条直接堵死"写死在前端"。
func TestConsolePageSourceCarriesNoVersionLiteral(t *testing.T) {
	if strings.Contains(consolePageHTML, dwversion.Version) {
		t.Fatalf("consolePageHTML 里出现了版本号字面量 %q ——"+
			"写死在前端意味着页面旧的时候版本号也是旧的,这一行就会跟着旧页面一起骗人", dwversion.Version)
	}
	for _, ph := range []string{"{{CONSOLE_VERSION}}", "{{CONSOLE_VERSION_TEXT}}", "{{CONSOLE_COMMIT}}"} {
		if !strings.Contains(consolePageHTML, ph) {
			t.Fatalf("模板缺少占位符 %s —— 服务端没有可填的位置", ph)
		}
	}
}

// ③ Handler 必须喂真常量(否则 ① 只证明渲染函数好用,不证明页面上那行是真的)。
func TestConsolePageHandlerServesProductVersion(t *testing.T) {
	hub, _, _ := newTestHub(t)
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("console page code=%d", rr.Code)
	}
	body := rr.Body.String()
	if got := badgeText(t, body); !strings.HasPrefix(got, dwversion.Version) {
		t.Fatalf("handler 渲染出的角标是 %q,应以产品版本常量 %q 开头", got, dwversion.Version)
	}
	// 占位符必须已被替换:漏替换时页面上会出现 {{...}},业主看到的是一行乱码而不是版本号。
	if strings.Contains(body, "{{CONSOLE_") {
		t.Fatalf("页面里仍有未替换的占位符:%s", body[strings.Index(body, "{{CONSOLE_"):])
	}
}

// ③b 🔴 前三条挡不住的最后一条路:Handler 把【字面量】喂给渲染。
//
//	实测(2026-09-24 阴性对照 B):把 renderConsolePage(dwversion.Version, …) 改成
//	renderConsolePage("0.2.1.68", …),①②③ 全绿 —— 因为字面量今天恰好等于常量。
//	等到抬版本号那天页面才会落后,而那正是最没人会去看这条格的时刻。
//	⇒ 只能在源码层焊住"喂进去的是常量本身"。本仓已有同形做法
//	  (view-service 的夹具也按源码相邻关系切 consolePageHTML)。
func TestConsolePageHandlerFeedsTheProductConstant(t *testing.T) {
	src, err := os.ReadFile("console_auth.go")
	if err != nil {
		t.Fatal(err)
	}
	const want = "renderConsolePage(dwversion.Version, dwversion.Commit)"
	if !strings.Contains(string(src), want) {
		t.Fatalf("HandleConsolePage 没有把产品常量本身喂给渲染(应出现 %q)——"+
			"喂字面量时今天一切正常,抬版本号那天页面才会落后", want)
	}
}
