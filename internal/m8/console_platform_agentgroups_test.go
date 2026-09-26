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

func TestConsolePlatformAgentGroupsEntryIsManagementOnly(t *testing.T) {
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

	// 🔴 排他腿:owner 看不到它。只验"management 看得到"不排他 ——
	//    「对所有角色都发」这个变异必须在这里转红。
	ownerRole, ownerItems := consolePlatformItemsFor(t, "owner")
	if ownerRole != consolePlatformRoleOwner {
		t.Fatalf("夹具前提坏了:owner 应解析为 owner,实为 %q", ownerRole)
	}
	if _, leaked := findPlatformItem(ownerItems, "agent-groups"); leaked {
		t.Errorf("非 management 也拿到了 Agent 编组入口,items=%+v", ownerItems)
	}
	if len(ownerItems) != 2 || ownerItems[0].ID != "specs" || ownerItems[1].ID != "task-board" {
		t.Errorf("owner 的平台项应只有可操作的 builtin specs/task-board,实为 %+v", ownerItems)
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
