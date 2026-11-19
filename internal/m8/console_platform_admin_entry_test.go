package m8

// 历史: Nova-260921-A(B1-b) 时本格钉「旧版后台入口 management 可见、指向 /console/admin」
// (变异 M11 咬出来的缺口:删掉 builtin 零转红)。S-R62-CONSOLE-SLIM 后管理角色入口由
// platform-admin 内部页签承载。
//
// S-R62-OLDPLAT-SLIM(业主令 rulings ㊿-补133):旧版后台(id=admin)从菜单摘除,
// 本格翻转为「任何角色的菜单都不得下发 admin」——把 builtin 加回必红。
// 后端路由 /console/admin、RequireConsoleManagement 授权与数据不动(直连行为保持现状);
// 授权面仍由 TestRequireConsoleManagement* 与 internal/admin 侧钉住,本格只管菜单下发。
//
// 阳性对照沿用 2026-09-29 组长裁定的「与集合内容无关」三条:
//   (a) 请求 200 且可解析(consolePlatformItemsFor 内部已钉);
//   (b) 该角色拿到的项**非空** —— 空集合证不了「漏与不漏」;
//   (c) 同一条通道看得见保留项 agent-groups —— 观测量对菜单内容敏感。
// id=admin 保留字一格(TestConsolePlatformAdminIDIsReserved)原样保留。
import (
	"net/http"
	"testing"
)

func TestConsolePlatformAdminEntrySlimMenuNeverExposesEntry(t *testing.T) {
	for _, principalID := range []string{"manager", "owner"} {
		role, items := consolePlatformItemsFor(t, principalID)
		if len(items) == 0 {
			t.Fatalf("阳性对照失效:%s 这一趟 200 且可解析,但一项都没拿到 ⇒ 空集合证不了「没有 admin」", principalID)
		}
		if _, ok := findPlatformItem(items, "agent-groups"); !ok {
			t.Fatalf("阳性对照失效:%s 菜单里连 agent-groups 都观测不到 ⇒ 通道对菜单内容不敏感,本腿不采信", principalID)
		}
		if leaked, ok := findPlatformItem(items, "admin"); ok {
			t.Fatalf("🔴 S-R62-OLDPLAT-SLIM:菜单不得再下发旧版后台(id=admin),%s(role=%s)却收到 %+v。\n"+
				"判据:HandleConsolePlatform 的 builtin 里不得有 ID:\"admin\" 这一条(加回必红);\n"+
				"后端路由 /console/admin 与授权不动,摘的只是菜单入口。", principalID, role, leaked)
		}
	}
}

func TestConsolePlatformAdminIDIsReserved(t *testing.T) {
	hub := newConsolePlatformTestHub(t)
	// builtin 入口占了 admin 这个 id;若还允许提交同 id 项,页面会出现两个
	// data-item-id="admin" ⇒ 闸与 UI 都无法唯一寻址(与既有 agents/agent-groups 同一理由)。
	rr := callConsolePlatform(t, hub, "manager", consolePlatformActionSubmit, http.MethodPost,
		"/api/console/platform/submissions",
		[]byte(`{"id":"admin","kind":"admin","title":"冒名","href":"/console/admin"}`),
		hub.HandleConsolePlatformSubmit)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("提交 id=admin 应被拒(与 builtin 入口同 id),code=%d body=%q", rr.Code, rr.Body.String())
	}
}
