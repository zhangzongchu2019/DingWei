package m8

// Nova-260921-A(B1-b) —— console 左栏那个「平台管理」入口。
//
// 🔴 本格是**变异 M11 咬出来的缺口**,不是设计时想到的:
// 把 HandleConsolePlatform 里 `ID:"admin"` 那条 builtin 整个删掉 ⇒
// `go test ./internal/admin/... ./internal/m8/... ./cmd/workpulse/...` **rc=0、零转红**(实测),
// 而业主在 console 里**再也看不见这个入口**。功能其实还在(直接访问 /console/admin 可用),
// 但「看不见的功能等于不存在」—— 本组今日同型第十次。
//
// 三腿,每腿都带阳性对照(判据写在失败消息里,不写在文档里让人记得):
//
//	① management 看得到,且 href=/console/admin、target=_blank
//	   🔴 href 必须是 **/console/admin**(console 角色授权的新门),不是 /admin(旧后台 wp_admin 那道)。
//	      指错了业主点进去就是 303 → /admin/login:形态对上了、方向相反
//	      (板件 runbooks/SPEC-OWNER-TAB-ADMIN-20260920.md §二 的原话)。
//	② owner 看不到 —— 否则普通业主看见一个点了就 403 的入口
//	   (板件 B1-a 已因「界面显示可管理、点下去 404 ⇒ 业主当故障反复点」卡过一次)。
//	   🔴 阳性对照:owner 那一趟必须**确实拿到了** agents/specs,
//	      否则"没有 admin"可能只是整条请求失败 ⇒ 那是假绿。
//	③ id=admin 是保留字:提交同 id 的自定义项必须被拒。
//
// 🔵 UI 可见性**不是**授权判据:真正的门是 RequireConsoleManagement
// (internal/m8/console_management.go),那一侧由 G1/G2 与本包的
// TestRequireConsoleManagement* 钉住。本格只管"入口存在、发给谁、指向哪"。
import (
	"net/http"
	"testing"
)

func TestConsolePlatformAdminEntryIsManagementOnlyAndPointsAtTheConsoleDoor(t *testing.T) {
	role, items := consolePlatformItemsFor(t, "manager")
	if role != consolePlatformRoleManagement {
		t.Fatalf("夹具前提坏了:manager 应解析为 management,实为 %q ⇒ 下面两腿的结论都不采信", role)
	}

	t.Run("management 必须看得到入口,且指向 console 那道门", func(t *testing.T) {
		entry, ok := findPlatformItem(items, "admin")
		if !ok {
			t.Fatalf("🔴 management 拿不到「平台管理」入口,items=%+v。\n"+
				"后果:/console/admin 仍然可用,但业主在 console 里**看不见它** ⇒ 等于没搬。\n"+
				"判据:HandleConsolePlatform 的 builtin 里必须有 ID:\"admin\" 这一条(M11 实测:删掉它零转红)。", items)
		}
		if entry.Href != "/console/admin" {
			t.Fatalf("🔴 入口 href=%q,必须是 /console/admin。\n"+
				"判据:指到 /admin(旧后台)⇒ 业主点进去 303 → /admin/login —— 形态对上了、方向相反\n"+
				"(板件 SPEC-OWNER-TAB-ADMIN-20260920.md §二)。业主原话是「admin 接纳 console 的鉴权方式」。", entry.Href)
		}
		if entry.Target != "_blank" {
			t.Errorf("target=%q,应为 _blank:/console/admin 是独立文档,顶层导航会把右栏活 iframe 与它们的 SSE 一起带走"+
				"(与 Agent 编组入口同一条实测理由)", entry.Target)
		}
		if entry.Title == "" || entry.Status != "approved" || !entry.Active {
			t.Errorf("入口必须是已批准的活平台项,实为 %+v", entry)
		}
	})

	t.Run("owner 看不到入口,且这一趟请求本身是好的(阳性对照)", func(t *testing.T) {
		ownerRole, ownerItems := consolePlatformItemsFor(t, "owner")
		if ownerRole != consolePlatformRoleOwner {
			t.Fatalf("夹具前提坏了:owner 应解析为 owner,实为 %q", ownerRole)
		}
		// 🔴 阳性对照:先证明这一趟**确实拿到了东西**,再断言"里面没有 admin"。
		// 少了这一步,"没漏"与"整条请求失败"长得一模一样。
		if _, ok := findPlatformItem(ownerItems, "agents"); !ok {
			t.Fatalf("阳性对照失效:owner 连 builtin agents 都没拿到,items=%+v ⇒ 本腿的「没有 admin」不采信", ownerItems)
		}
		if _, ok := findPlatformItem(ownerItems, "specs"); !ok {
			t.Fatalf("阳性对照失效:owner 连 builtin specs 都没拿到,items=%+v ⇒ 本腿的「没有 admin」不采信", ownerItems)
		}
		if leaked, ok := findPlatformItem(ownerItems, "admin"); ok {
			t.Fatalf("🔴 非 management 也拿到了「平台管理」入口:%+v。\n"+
				"后果:普通业主(默认角色就是 owner)看见一个点下去 403 的入口,会当成故障反复点。\n"+
				"判据:这一项与 agent-groups 同口径,只对 management 发。", leaked)
		}
	})
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
