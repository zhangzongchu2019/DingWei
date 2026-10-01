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
		{"b.title=a.full_name", "全名须仍可得(悬停),短名不等于丢掉全名(名字口径 full_name,组长裁定 2026-10-01)"},
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

// TestConsolePageMultiTab 业主 2026-09-16 定:右侧多 tab,每点一个 Agent 增加一个属性页。
// 🔴 本测试是【结构级】的,它只证明这些行为的实现仍在页面里;
// 真行为由 view-service/renderer-t145-tabs-regression.js(抽真源码求值)与业主目视共同保证。
func TestConsolePageMultiTab(t *testing.T) {
	hub, _, _ := newTestHub(t)
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	body := rr.Body.String()
	for _, w := range []struct{ frag, why string }{
		{"MAX_TABS = 8", "上限必须存在且为业主定的 8"},
		{"tabs.has(session)", "已开过的 Agent 必须切过去,不得重建(重建=丢掉那一页的内容)"},
		{"v.pane.style.display", "🔴 切换必须用显示/隐藏"},
		{"closeTab", "必须能关闭,否则点多了收不回来"},
		{"tabs.delete(session)", "关闭必须真正移除,才会断开那条长连接"},
	} {
		if !strings.Contains(body, w.frag) {
			t.Errorf("控制台页缺少 %q —— %s", w.frag, w.why)
		}
	}
	// 🔴 阴性对照:旧的单 iframe 写法必须已消失。
	//    否则「多 tab 存在」与「旧写法也还在」可能同时为真,断言就不排他。
	if strings.Contains(body, "v.src='/view/'") || strings.Contains(body, "<iframe id=v") {
		t.Error("旧的单 iframe 替换写法仍在:新旧并存 ⇒ 断言不排他")
	}
	// 🔴 切换绝不能改 src —— 改 src 等于销毁并重建那一页,正是本次要消灭的行为
	if strings.Contains(body, "function activate") {
		i := strings.Index(body, "function activate")
		seg := body[i:]
		if j := strings.Index(seg, "}"); j > 0 {
			seg = seg[:j]
		}
		if strings.Contains(seg, ".src") {
			t.Error("activate() 里不得出现 .src —— 切换必须用显示/隐藏,不得重载")
		}
	}
}

func TestConsolePageRefreshesOnlyAgentList(t *testing.T) {
	hub, _, _ := newTestHub(t)
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	body := rr.Body.String()
	for _, w := range []struct{ frag, why string }{
		{"setInterval(refreshAgents,15000)", "Agent 列表须每 15 秒刷新"},
		{"nav.replaceChildren()", "刷新只重建左栏"},
		{"applyAgentSnapshot(agents)", "刷新须同步既有 tab 的在线状态"},
		{"e.pane.style.pointerEvents=online?'':'none'", "离线 tab 保留但不可交互"},
	} {
		if !strings.Contains(body, w.frag) {
			t.Errorf("控制台页缺少 %q —— %s", w.frag, w.why)
		}
	}
	// 列表刷新函数不得拥有右栏生命周期；右栏只由 openTab/closeTab 创建或销毁。
	start := strings.Index(body, "function renderAgentList")
	end := strings.Index(body, "async function refreshAgents")
	if start < 0 || end <= start {
		t.Fatal("无法定位左栏刷新函数")
	}
	segment := body[start:end]
	for _, forbidden := range []string{"getElementById('tabs')", "getElementById('panes')", ".src=", ".remove()"} {
		if strings.Contains(segment, forbidden) {
			t.Errorf("左栏刷新函数含右栏破坏动作 %q", forbidden)
		}
	}
}

func TestConsolePageFreshAgentStatusOverlaysGroupMembersWithoutNewPoller(t *testing.T) {
	hub, _, _ := newTestHub(t)
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	body := rr.Body.String()
	for _, required := range []string{
		"function agentTreeMemberOnline(member)",
		"agent.full_name===memberName(member)",
		"fresh?fresh.online===true:member.online===true",
		"agentListSnapshot=agents;renderAgentNav();if(groupMode)renderGroups();",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("编组成员 online 没有接到既有 Agent 新鲜快照: missing=%q", required)
		}
	}
	if got := strings.Count(body, "setInterval(refreshAgents,15000)"); got != 1 {
		t.Fatalf("必须复用唯一的 15 秒轮询，不得新增轮询: count=%d want=1", got)
	}
	start := strings.Index(body, "function agentTreeMemberOnline(member)")
	if start < 0 {
		t.Fatal("无法定位 online 覆盖函数")
	}
	end := strings.Index(body[start:], "function agentTreeMemberDisabled")
	if end < 0 {
		t.Fatal("无法定位 online 覆盖函数末尾")
	}
	overlay := body[start : start+end]
	for _, forbidden := range []string{"groupSnapshot.groups=", "group.members=", "members.push", "members.splice"} {
		if strings.Contains(overlay, forbidden) {
			t.Fatalf("online 覆盖不得改写编组成员集合: found=%q", forbidden)
		}
	}
}
