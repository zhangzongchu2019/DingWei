package m8

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Agent 编组入口(0.2.1.7)——「发给谁 / 带什么 target」这一半判据在 Go 侧,
// 浏览器语义(点击后 pane 不丢)那一半在 view-service/console-agent-groups-entry-regression.js。
//
// 🔴 两腿不是各测各的:JS 腿的平台载荷由 TestConsolePlatformPayloadDump 现场产出后交给它,
//
//	所以「后端把 target 去掉」这个变异会**同时**打到 JS 腿 —— 否则那边测的是夹具。
//
// 为什么必须 target="_blank":/admin/agent-groups 是**独立文档**,而 console 页的右栏
// 是一堆活着的 iframe(每个一条 SSE)。顶层导航过去 ⇒ 全部 pane 与长连接一起没了,
// 且后退回不来(实测:pane 数 2→0、SSE 全断)。
func consolePlatformItemsFor(t *testing.T, principalID string) (string, []model.ConsolePlatformItem) {
	t.Helper()
	hub := newConsolePlatformTestHub(t)
	rr := callConsolePlatform(t, hub, principalID, consolePlatformActionRead, http.MethodGet,
		"/api/console/platform", nil, hub.HandleConsolePlatform)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s list code=%d body=%q", principalID, rr.Code, rr.Body.String())
	}
	var listed struct {
		Role  string                      `json:"role"`
		Items []model.ConsolePlatformItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	return listed.Role, listed.Items
}

func findPlatformItem(items []model.ConsolePlatformItem, id string) (model.ConsolePlatformItem, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.ConsolePlatformItem{}, false
}

// 🔴 本测试原名 TestConsolePlatformAgentGroupsEntryIsManagementOnly,已随 S-GROUP-ENTRY 改名。
//
//	原名在改口径之后会**变成假的**(它说的是"只对 management 发"),而假名字会继续指挥下一个人
//	⇒ 改名,不删历史:0.2.1.7 那份交付件
//	   runbooks/DELIVERY-AGENT-GROUPS-ENTRY-0.2.1.7-qwen38maxkhovvob4.md 引的是旧名,
//	   已在该件加追记指向新名。
//	为什么口径翻了:「加入到其他业主编组」这个面板只在这个入口里面,而它的目标用户
//	(另一位业主,默认 role=owner)恰恰不是 management ⇒ 入口不发给 owner = 功能对他不存在。
func TestConsolePlatformAgentGroupsEntryIsInternalTabForBothRoles(t *testing.T) {
	role, items := consolePlatformItemsFor(t, "manager")
	if role != consolePlatformRoleManagement {
		t.Fatalf("夹具前提坏了:manager 应解析为 management,实为 %q", role)
	}
	entry, ok := findPlatformItem(items, "agent-groups")
	if !ok {
		t.Fatalf("management 拿不到 Agent 编组入口,items=%+v", items)
	}
	if entry.Href != "#agent-groups" {
		t.Errorf("href=%q,应为 console 内部 #agent-groups", entry.Href)
	}
	if entry.Target != "" {
		t.Errorf("target=%q,内部 tab 不得弹新窗口", entry.Target)
	}
	if entry.Title == "" || entry.Status != "approved" || !entry.Active {
		t.Errorf("入口必须是已批准的活平台项,实为 %+v", entry)
	}

	// 🔴 口径已翻(见函数头注):这一腿现在验的是「owner **也**拿得到,且形状与 management 那条逐项相同」。
	//    只验"management 看得到"不排他 —— 但排他的对象换了:
	//    「把 agent-groups 从 builtins 里整个删掉」这个变异必须在这里转红
	//    (它会让 management 那腿先红,而 owner 这腿抓住"只对 management 发"的回退)。
	//    🔵 「管理面不得一刀切放出去」那条排他腿现在钉的是 **admin**,
	//       在 console_platform_group_entry_owner_test.go 的子格 ②。
	ownerRole, ownerItems := consolePlatformItemsFor(t, "owner")
	if ownerRole != consolePlatformRoleOwner {
		t.Fatalf("夹具前提坏了:owner 应解析为 owner,实为 %q", ownerRole)
	}
	ownerEntry, ownerOK := findPlatformItem(ownerItems, "agent-groups")
	if !ownerOK {
		t.Fatalf("🔴 owner 拿不到 Agent 编组入口,items=%+v ⇒ 又退回\"只对 management 发\"了。\n"+
			"   查 internal/m8/console_platform.go:ID:\"agent-groups\" 那一项是否被挪回\n"+
			"   if role == consolePlatformRoleManagement 块内。", ownerItems)
	}
	if ownerEntry.Href != entry.Href || ownerEntry.Target != entry.Target ||
		ownerEntry.Kind != entry.Kind || ownerEntry.Status != entry.Status || ownerEntry.Active != entry.Active {
		t.Errorf("owner 与 management 拿到的入口形状必须逐项相同(同一个内部 tab),\n owner=%+v\n mgmt =%+v", ownerEntry, entry)
	}
	// S-R62-CONSOLE-SLIM：owner 菜单恰为一项 agent-groups；被摘项不得回潮。
	if len(ownerItems) != 1 || ownerItems[0].ID != "agent-groups" {
		t.Errorf("owner 的平台项应恰为 agent-groups 一项,实为 %+v", ownerItems)
	}
}

func TestConsolePlatformBuiltinAgentsIsReservedButNotEmitted(t *testing.T) {
	for _, principalID := range []string{"manager", "owner"} {
		_, items := consolePlatformItemsFor(t, principalID)
		if _, emitted := findPlatformItem(items, "agents"); emitted {
			t.Fatalf("%s 收到无动作的 builtin agents 项:%+v", principalID, items)
		}
	}
	if !consolePlatformReservedItemIDs["agents"] {
		t.Fatal("agents id 必须继续保留,防止用户自建同 id 假入口")
	}
}

func TestConsolePlatformAgentGroupsIDIsReserved(t *testing.T) {
	hub := newConsolePlatformTestHub(t)
	// builtin 入口占了 agent-groups 这个 id;若还允许提交同 id 项,
	// 页面会出现两个 data-item-id="agent-groups" ⇒ 闸与 UI 都无法唯一寻址。
	rr := callConsolePlatform(t, hub, "manager", consolePlatformActionSubmit, http.MethodPost,
		"/api/console/platform/submissions",
		[]byte(`{"id":"agent-groups","kind":"agent-groups","title":"冒名","href":"/admin/agent-groups"}`),
		hub.HandleConsolePlatformSubmit)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("提交 id=agent-groups 应被拒(与 builtin 入口同 id),code=%d body=%q", rr.Code, rr.Body.String())
	}
}

// TestConsolePlatformPayloadDump 不是断言,是**装置**:把真 handler 现产的平台载荷交出去,
// 供 view-service/console-agent-groups-entry-regression.js 当 /api/console/platform 的应答。
// 只在闸装置设了 DW_CONSOLE_PLATFORM_PAYLOAD_OUT 时运行,平时 skip。
func TestConsolePlatformPayloadDump(t *testing.T) {
	out := strings.TrimSpace(os.Getenv("DW_CONSOLE_PLATFORM_PAYLOAD_OUT"))
	if out == "" {
		t.Skip("仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT)")
	}
	hub := newConsolePlatformTestHub(t)
	var lines []string
	for _, principalID := range []string{"manager", "owner"} {
		rr := callConsolePlatform(t, hub, principalID, consolePlatformActionRead, http.MethodGet,
			"/api/console/platform", nil, hub.HandleConsolePlatform)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s code=%d body=%q", principalID, rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		line, err := json.Marshal(map[string]any{"principal": principalID, "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(line))
	}
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s(%d 行)", out, len(lines))
}
