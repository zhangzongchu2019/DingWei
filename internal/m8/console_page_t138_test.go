package m8

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// T-138(业主目标②③):左栏只显短名、在线优先、离线折叠。
// 🔴 本测试是【结构级】的:它证明这些行为的实现仍在页面里,不证明浏览器里的渲染效果。
// 真行为由 view-service/renderer-t138-shortname-regression.js(抽取真源码求值)与业主目视验收共同保证。
func TestConsolePageShortNameAndOfflineGrouping(t *testing.T) {
	hub, _, _ := newTestHub(t)
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("console page code=%d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []struct{ frag, why string }{
		{"function dwShortName(", "短名函数必须在页面里"},
		{"p.length===3?p[1]:s", "短名取法:三段式取中间段,否则原样"},
		{"b.title=a.session", "全名须仍可得(悬停),短名不等于丢掉全名"},
		{"createElement('details')", "离线项必须折叠,不与在线项平铺"},
		{"return a.online", "必须按在线与否分流"},
	} {
		if !strings.Contains(body, want.frag) {
			t.Errorf("控制台页缺少 %q —— %s", want.frag, want.why)
		}
	}
	// 🔴 阴性对照:旧写法(全名平铺 ＋ " (offline)" 后缀)必须已消失,
	//    否则「新写法存在」可能与「旧写法也还在」同时为真,断言就不排他。
	if strings.Contains(body, "' (offline)'") {
		t.Error("旧的全名平铺写法仍在页面里:新旧并存 ⇒ 断言不排他")
	}
}
