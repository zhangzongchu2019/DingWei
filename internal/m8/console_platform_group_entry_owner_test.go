package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// S-GROUP-ENTRY —— 「Agent 编组」入口对**普通业主**可见。
//
//	派工  runbooks/DISPATCH-20260929-S-GROUP-ENTRY.md(判据四格 ①②③④)
//	背景  runbooks/DEFECT-group-entry-is-management-only-20260929.md
//	      (取证面 preprod = 608d4b24 = 现网 0.2.1.93,build == run 已核)
//
// 为什么这是缺陷而不是策略 —— 业主 2026-09-29 逐字:「界面上没有 Agent 编组(只有 spec 和任务)」。
// 而「加入到其他业主编组」那个面板**只在这个入口里面**:
// internal/m8/console_auth.go 里 openGroupWorkspace 全文件仅一个调用点
// (renderPlatform 中 item.id==='agent-groups' 的 onclick)⇒ **没有第二条打开路径**。
// 业主自己 09-24 的设计件(runbooks/DESIGN-OWNER-agent-grouping-20260924.md:22)把这一项定成
// 「它本来就是跨业主入组的入口」,而"另一个业主"按默认角色恰恰**不是** management
// (store 的 ResolveConsolePlatformRole 对每个 active principal COALESCE(…, 'owner') ⇒ 默认 owner)。
// ⇒ 功能建好了、按钮在页面里、后端放行,**唯独入口不发给它的目标用户**。
//
// 🔴 本件只钉**可见面**。授权面一个字不动,由下面 ④ 与既有闸各自守:
//
//	cmd/workpulse/console_routes.go（registerConsoleRoutes）里六条 /api/console/agent-groups* 挂的是 **RequireConsolePrincipal**
//	  (不是 RequireConsoleManagement);
//	internal/m8/console_agent_groups.go 的列表 visible = canManage || 该组成员里有我自己的 Agent;
//	canManageConsoleAgentGroup 另行守删除/剔除/开关添加。
//
// 🔴 判据③(**反向变异**)跑法 —— 必须显式断言红,超时红/panic 红不算:
//
//	把 internal/m8/console_platform.go 里 ID:"agent-groups" 那一项**挪回**
//	if role == consolePlatformRoleManagement 块内,然后:
//	  go test ./internal/m8/ -run TestSGroupEntryOwnerSeesGroupEntryButNotAdminEntry -count=1 -v
//	期望:子格 ① 红,且红文案**点名 agent-groups**。
//	🔵 施加后若 ① 仍绿 ⇒ **先怀疑变异点打错了**(组长同日自己踩过两次),不要写成「对照没有牙」。
//	跑完 `git checkout -- internal/m8/console_platform.go` 还原,并核 `git diff --quiet` 逐字节相同。

// sGroupEntryIDs 把 items 压成 id 列表,只为让红文案可读(不参与判定)。
func sGroupEntryIDs(items []model.ConsolePlatformItem) string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return "[" + strings.Join(ids, " ") + "]"
}

// 判据①② —— 🔴 **同一次请求**上判两条(派工②逐字要求「同一请求」,不是各发一趟):
//
//	① role=owner 的 items **含** agent-groups
//	② 同一请求的 items **仍不含** admin(平台管理)⇒ 防「一刀切」把管理面一起放出去
func TestSGroupEntryOwnerSeesGroupEntryButNotAdminEntry(t *testing.T) {
	ownerRole, ownerItems := consolePlatformItemsFor(t, "owner")
	if ownerRole != consolePlatformRoleOwner {
		t.Fatalf("夹具前提坏了:owner 应解析为 %q,实为 %q(这一格判的是默认角色,前提塌了结论就不成立)",
			consolePlatformRoleOwner, ownerRole)
	}
	// 🔵 装置自证:这趟请求本身必须拿到了东西。空集合下「不含 admin」是真的但**证不了任何事**
	//    (与本仓既有那道 admin 入口闸同一个理由:空集合不等于排他)。
	if len(ownerItems) == 0 {
		t.Fatal("夹具前提坏了:owner 一个平台项都没拿到 ⇒ 本件两条判据都不采信(空集合证不了排他)")
	}

	t.Run("① owner 的 items 含 agent-groups,且是 console 内部 tab 形状", func(t *testing.T) {
		entry, ok := findPlatformItem(ownerItems, "agent-groups")
		if !ok {
			t.Fatalf("🔴 role=owner 的 items 里没有 agent-groups,实为 %s。\n"+
				"   「加入到其他业主编组」那个面板**只在这个入口里面**(console_auth.go 的 openGroupWorkspace 仅一个调用点),\n"+
				"   而该功能的目标用户恰恰是默认 role=owner 的另一位业主 ⇒ 入口不发给他 = 功能对他不存在。\n"+
				"   查 internal/m8/console_platform.go:ID:\"agent-groups\" 那一项是否又被挪回\n"+
				"   if role == consolePlatformRoleManagement 块内(那就是判据③ 的反向变异形状)。",
				sGroupEntryIDs(ownerItems))
		}
		if entry.Href != "#agent-groups" {
			t.Errorf("href=%q,应为 console 内部锚 #agent-groups —— 指向 /admin/agent-groups 是**方向相反**"+
				"(那是旧 admin 独立鉴权域,业主点进去 303 → /admin/login)", entry.Href)
		}
		if entry.Target != "" {
			t.Errorf("target=%q,内部 tab 不得弹新窗口(顶层导航会带走右栏那堆活 iframe 与它们的 SSE)", entry.Target)
		}
		if entry.Title == "" || entry.Status != "approved" || !entry.Active {
			t.Errorf("入口必须是已批准的活平台项,实为 %+v", entry)
		}
	})

	t.Run("② 同一请求的 items 仍不含 admin(平台管理)", func(t *testing.T) {
		if leaked, ok := findPlatformItem(ownerItems, "admin"); ok {
			t.Fatalf("🔴 一刀切把管理面也放出去了:role=owner 收到 %+v。\n"+
				"   本单只把 agent-groups 移出 management 块;**平台管理必须留在块内** ——\n"+
				"   它指向 /console/admin,是真·管理面(RequireConsoleManagement 那道门在后面,\n"+
				"   但让普通业主看到一个点了就 403 的入口本身就是缺陷)。", leaked)
		}
	})
}

// 排他的另一半:management 侧**两项都还在**。
//
//	🔴 没有这一格,「把 agent-groups 从 builtins 里整个删掉」也能让 ① 之外的所有闸绿 ——
//	   那不是修复,是把功能对**所有人**关掉。判据要能区分"移给所有人"与"谁都没有"。
func TestSGroupEntryManagementStillSeesBothEntries(t *testing.T) {
	role, items := consolePlatformItemsFor(t, "manager")
	if role != consolePlatformRoleManagement {
		t.Fatalf("夹具前提坏了:manager 应解析为 %q,实为 %q", consolePlatformRoleManagement, role)
	}
	if _, ok := findPlatformItem(items, "agent-groups"); !ok {
		t.Fatalf("🔴 management 反而拿不到 agent-groups 了,实为 %s ⇒ 这不是\"移出 management 块\","+
			"是把入口对所有人删掉了", sGroupEntryIDs(items))
	}
	if _, ok := findPlatformItem(items, "admin"); !ok {
		t.Fatalf("🔴 management 拿不到「平台管理」入口,实为 %s ⇒ 本单不得动 admin 那一项", sGroupEntryIDs(items))
	}
}

// 判据④ —— **授权面未变**:role=owner 对**别人的**组 can_manage=false,且删除仍被拒。
//
//	🔵 派工说这一格用「既有闸复跑」即可。实查:Go 侧**没有**任何一条既有断言字面判 can_manage=false
//	   (只有 can_manage:true 的三条,以及 owner-b 删除得 403 的授权腿);字面的 false 只出现在
//	   view-service/console-agent-groups-vega-regression.js 的夹具里。
//	   ⇒ 本格补上**字面**断言,让判据④ 有自己的观测量,而不是借用别的观测量推。
//	🔴 阳性对照内置:同一份列表里 creator(owner-a)那一趟必须 can_manage=**true**。
//	   否则「false」可能只是这个字段恒为 false(观测量对变异不敏感 ⇒ 闸再有牙也咬空)。
func TestSGroupEntryOwnerCannotManageForeignGroup(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	a1 := addOwnedGroupAgent(t, db, "owner-a", "sge-a1", "sge-ka1", "owner-a-sge-one-2642", "oa")
	b1 := addOwnedGroupAgent(t, db, "owner-b", "sge-b1", "sge-kb1", "owner-b-sge-full-2642", "ob")
	hub.sessionClients[a1.KeyID] = map[string]*sessionClient{a1.SessionName: {keyID: a1.KeyID, sessionName: a1.SessionName}}
	hub.sessionClients[b1.KeyID] = map[string]*sessionClient{b1.SessionName: {keyID: b1.KeyID, sessionName: b1.SessionName}}

	// call 显式注入 **role=owner**(不是留空):判据④ 逐字说的是 role=owner,
	// 而 canManageConsoleAgentGroup 的第三个入参正是 platformRole(r)==management。
	call := func(principal, method, path, code, agentID string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.SetPathValue("code", code)
		req.SetPathValue("agentID", agentID)
		ctx := context.WithValue(req.Context(), consolePrincipalContextKey{}, principal)
		ctx = context.WithValue(ctx, consolePlatformRoleContextKey{}, consolePlatformRoleOwner)
		rr := httptest.NewRecorder()
		if path == "/api/console/agent-groups" {
			hub.HandleConsoleAgentGroups(rr, req.WithContext(ctx))
		} else {
			hub.HandleConsoleAgentGroup(rr, req.WithContext(ctx))
		}
		return rr
	}
	listGroups := func(principal string) []consoleAgentGroupView {
		rr := call(principal, http.MethodGet, "/api/console/agent-groups", "", "", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s 列表 code=%d body=%s", principal, rr.Code, rr.Body.String())
		}
		var listed struct {
			Groups []consoleAgentGroupView `json:"groups"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
			t.Fatalf("%s 列表解码失败 err=%v body=%s", principal, err, rr.Body.String())
		}
		return listed.Groups
	}

	rr := call("owner-a", http.MethodPost, "/api/console/agent-groups", "", "",
		[]byte(`{"agent_id":"`+a1.ID+`","short_name":"sgegroup"}`))
	if rr.Code != http.StatusCreated {
		t.Fatalf("creator 建组 code=%d body=%s", rr.Code, rr.Body.String())
	}
	var group model.AgentGroup
	if err := json.Unmarshal(rr.Body.Bytes(), &group); err != nil || len(group.Code) != 32 {
		t.Fatalf("建组应答 err=%v group=%+v", err, group)
	}
	// owner-b(role=owner)**用组码**把自己的 Agent 加进去 —— 这正是判据②④ 里业主在产品上
	// **做不到**的那一步(入口不发给 owner ⇒ 他连这个面板都打不开)。
	if rr := call("owner-b", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "",
		[]byte(`{"agent_id":"`+b1.ID+`"}`)); rr.Code != http.StatusNoContent {
		t.Fatalf("开放编组必须接受另一位业主的在线 Agent(凭组码)code=%d body=%s", rr.Code, rr.Body.String())
	}
	// creator 再放一个**自己的非组长** Agent 进去,给下面「剔除别人的成员」那一腿当靶子。
	a2 := addOwnedGroupAgent(t, db, "owner-a", "sge-a2", "sge-ka2", "owner-a-sge-two-2642", "oa")
	hub.sessionClients[a2.KeyID] = map[string]*sessionClient{a2.SessionName: {keyID: a2.KeyID, sessionName: a2.SessionName}}
	if rr := call("owner-a", http.MethodPost, "/api/console/agent-groups/"+group.Code+"/members", group.Code, "",
		[]byte(`{"agent_id":"`+a2.ID+`"}`)); rr.Code != http.StatusNoContent {
		t.Fatalf("creator 应能加自己的第二个 Agent,code=%d body=%s", rr.Code, rr.Body.String())
	}

	foreign := listGroups("owner-b")
	if len(foreign) != 1 {
		t.Fatalf("owner-b 加入后应看到且只看到这一个组,实为 %d 个:%+v", len(foreign), foreign)
	}
	// 🔴 判据④ 的**字面**观测量。
	if foreign[0].CanManage {
		t.Errorf("🔴 授权面被放宽了:role=owner 对**别人的**组 can_manage=true,组=%+v。\n"+
			"   本单只改可见面(把入口发给 owner),**不得**让 can_manage 跟着变 —— \n"+
			"   canManageConsoleAgentGroup 只认 creator/legacy-leader/management 三种归属。", foreign[0])
	}
	// 🔴 阳性对照:同一个字段在 creator 那一趟必须是 **true**。
	//   否则"false"可能只是字段恒为 false ⇒ 观测量对变异不敏感,上面那条不采信。
	own := listGroups("owner-a")
	if len(own) != 1 || !own[0].CanManage {
		t.Fatalf("🔴 阳性对照失效:creator 自己那一趟应 can_manage=true,实为 %+v。\n"+
			"   这一趟不成立 ⇒ 上面那条 false 可能只是字段恒为 false,**不采信**(观测量对变异不敏感)", own)
	}

	// 授权面逐腿:删组 / 剔除**别人的**成员 ⇒ 403「无权管理该编组」。
	if rr := call("owner-b", http.MethodDelete, "/api/console/agent-groups/"+group.Code, group.Code, "", nil); rr.Code != http.StatusForbidden ||
		!strings.Contains(rr.Body.String(), "无权管理该编组") {
		t.Errorf("owner-b 删除别人的组必须被显式拒绝,code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := call("owner-b", http.MethodDelete, "/api/console/agent-groups/"+group.Code+"/members/"+a2.ID, group.Code, a2.ID, nil); rr.Code != http.StatusForbidden ||
		!strings.Contains(rr.Body.String(), "无权管理该编组") {
		t.Errorf("owner-b 剔除**别人的**成员必须被显式拒绝,code=%d body=%s", rr.Code, rr.Body.String())
	}
	// 🔵 目标是**组长**时实测 409「组长不可剔除」而不是 403 ——
	//   读码核实成因:组长保护写在 UnassignConsoleAgentGroup 的 `DELETE … AND NOT (leader)` 里
	//   (internal/store/sqlite.go:3750-3756)⇒ 那一趟 RowsAffected=0,落进 n!=1 的错误分支;
	//   而该分支**先判 leader、后判越权**(:3788-3791)⇒ 无权者打组长拿到的是"组长不可剔除"。
	//   🔵 本格**只断言"被拒"**(非 2xx),不断言具体码:
	//      · 安全性质是"没删掉",两条码都满足;断到码上会把一个**错误文案精度**问题钉成契约;
	//      · 也不构成信息泄露:owner-b 本就是该组成员,列表里 leader_key_id/leader_session_name 已可见。
	//   ⇒ 精度问题**超出本单范围,不修**,已如实报组长。
	if rr := call("owner-b", http.MethodDelete, "/api/console/agent-groups/"+group.Code+"/members/"+a1.ID, group.Code, a1.ID, nil); rr.Code < 400 {
		t.Errorf("🔴 owner-b 竟然能剔除别人组的组长,code=%d body=%s", rr.Code, rr.Body.String())
	}
	// 🔴 撤回**自己的** Agent 是 **204** —— 这是**正当能力**,不是越权,别当缺陷改掉:
	//   UnassignConsoleAgentGroup 的 SQL 里有 `a.owner_principal=?` 分支(internal/store/sqlite.go:3750)
	//   ⇒ 业主永远能把自己的 Agent 从任何组里撤出来。
	//   🔵 本席第一版把这一腿写成「必须 403」,实测 **204** ⇒ **是断言写错了,不是产品坏了**;
	//      读码核实后才改成钉住真实行为。留在这一格,免得下一个人再把它读成漏洞。
	if rr := call("owner-b", http.MethodDelete, "/api/console/agent-groups/"+group.Code+"/members/"+b1.ID, group.Code, b1.ID, nil); rr.Code != http.StatusNoContent {
		t.Errorf("owner-b 撤回自己的 Agent 应成功(正当能力),code=%d body=%s", rr.Code, rr.Body.String())
	}
	// 撤回后该组对 owner-b **不再可见** —— 这正是"入口发给 owner 也不泄露别人组"的那条性质:
	// visible = canManage || 该组成员里有我自己的 Agent。
	if still := listGroups("owner-b"); len(still) != 0 {
		t.Errorf("撤回自己的 Agent 后,该组对 owner-b 应不再可见,实为 %+v", still)
	}
}

// 判据⑤⑥ —— 业主 2026-09-29 在确认 0.2.1.95 指标时**追加**的那条:
//
//	「另外:**非管理者也可以自建 Agent 编组**」
//
// 🔵 读码核实(组长先读、我复核同意):创建路径 internal/m8/console_agent_groups.go 的 POST 分支
// 只要求「指定的组长是调用者**自己拥有**的 Agent」(在 ListOwnedConsoleAgents 里找得到)
// ＋ 该 Agent 的 session endpoint 存在(用它取 ownerKey),**没有任何 management 判据**
// ⇒ 代码本来就支持;此前唯一阻碍是**入口看不见**,而那正是判据① 修的东西。
// 🔴 但「指标里写了、闸里没有」等于没有 ⇒ 本格补上**字面**观测量(真 handler + 真库,不打桩)。
//
// 🔴 判据⑥(**反向变异**)跑法 —— 必须显式断言红;超时红 / panic 红 / **编译红都不算**:
// 在 internal/m8/console_agent_groups.go 的 POST 创建分支里
// (`if r.Method != http.MethodPost { … }` 那段守卫**之后**、解码 body 之前;该守卫在本提交是 :182,
// 🔵 行号随树变 —— 认形状不认行号:全文件只此一处 MethodPost 守卫)插入:
//
//	if platformRole(r) != consolePlatformRoleManagement {
//	    http.Error(w, "无权创建编组", http.StatusForbidden)
//	    return
//	}
//
// 然后:go test ./internal/m8/ -run TestSGroupEntryOwnerCanCreateGroup -count=1 -v
// 期望:本格红,且红文案**点名「非管理者不能建组」**。
// 🔵 施加后若本格仍绿 ⇒ **先怀疑变异点打错了**(组长同日自己踩过两次:同文件里有相似形状,
// 匹配到前一处就落进别的函数里;而且**编译红不算红**,重做到能跑才算),不要写成「对照没有牙」。
// 跑完 `git checkout -- internal/m8/console_agent_groups.go` 还原,并核 `git diff --quiet` 逐字节相同。
//
// 🔵 本格已实跑过**三发**变异(2026-09-29,基线 e7aa57f5,产品件还原后 sha256 与变异前逐字节相同);
// 三发都先过 `go build` ＋ `go vet`(编译红不算红),结果记在这里,免得下一个人重猜:
// M1 创建路径加 management 判据(上面那段)⇒ 本格红,红文案点名「非管理者不能建组」;
// ①② 与 management 格**仍绿**;判据④ 那格也红,但红在**装置步**(它那句 "creator 建组 code=403")、
// 不点名真因 ⇒ 🔴 只有本格的红文案说的是对的缺陷。
// M2 `canManage := canManageConsoleAgentGroup(…) && false`(列表视图 canManage 那一行,
// console_agent_groups.go:127)⇒ 本格红在 **can_manage 那两条断言**(第一腿 owner-a、第二腿 owner-b),
// **不是**红在列表长度 ⇒ 观测量确实会动、没断在盲量上;
// 判据④ 那格靠它自带的阳性对照("阳性对照失效:creator 自己那一趟应 can_manage=true")红。
// M3 创建路径只特批 owner-a(`if principal == "owner-b" { 403 }`)⇒ 本格红在**第二腿**
// (owner-b 建组失败 code=403),而 ①② / management 格 / 🔴 **判据④ 全部绿**
// ⇒ 本格第二腿是**别处没有的覆盖**:
// "能自建"若不是对每个 owner 都成立(而是某人被特批),只有这一腿看得见。
// ⇒ M1 红第一腿、M3 红第二腿 ⇒ 两条腿**各自单独能红**(逐处变异),不是一腿替两腿背书。
func TestSGroupEntryOwnerCanCreateGroup(t *testing.T) {
	hub := newConsoleSpecTestHub(t)
	db := hub.Repo.(*store.SQLite)
	a1 := addOwnedGroupAgent(t, db, "owner-a", "sge-c1", "sge-kc1", "owner-a-sge-create-2642", "oa")
	b1 := addOwnedGroupAgent(t, db, "owner-b", "sge-c2", "sge-kc2", "owner-b-sge-create-2642", "ob")
	hub.sessionClients[a1.KeyID] = map[string]*sessionClient{a1.SessionName: {keyID: a1.KeyID, sessionName: a1.SessionName}}
	hub.sessionClients[b1.KeyID] = map[string]*sessionClient{b1.SessionName: {keyID: b1.KeyID, sessionName: b1.SessionName}}

	// 🔵 装置自证(一):两位创建者在**真库**里解析出来的平台角色都必须**不是** management。
	//   夹具只给 "manager" 写了 console_platform_role=management;owner-a / owner-b **没有角色行**
	//   ⇒ ResolveConsolePlatformRole 走 COALESCE 默认 'owner'(与现网「第二位业主默认 owner」同形)。
	//   🔴 这一步不是形式:若哪天夹具给他们补了 management 行,下面那趟就变成"管理者建组成功",
	//      而判据⑥ 的变异**照样绿** ⇒ 观测量对变异不敏感,结论一律不采信。
	for _, principal := range []string{"owner-a", "owner-b"} {
		role, err := db.ResolveConsolePlatformRole(context.Background(), principal)
		if err != nil {
			t.Fatalf("夹具前提坏了:解析 %s 的平台角色失败 err=%v", principal, err)
		}
		if role != consolePlatformRoleOwner {
			t.Fatalf("夹具前提坏了:%s 在真库里应解析为 %q,实为 %q ⇒ 本格判的是「非管理者能建组」,前提塌了结论不成立",
				principal, consolePlatformRoleOwner, role)
		}
	}

	// call 显式注入 **role=owner**(与判据④ 同一套做法):业主追加的那条逐字说的是"非管理者"。
	call := func(principal, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		ctx := context.WithValue(req.Context(), consolePrincipalContextKey{}, principal)
		ctx = context.WithValue(ctx, consolePlatformRoleContextKey{}, consolePlatformRoleOwner)
		rr := httptest.NewRecorder()
		hub.HandleConsoleAgentGroups(rr, req.WithContext(ctx))
		return rr
	}
	listGroups := func(principal string) []consoleAgentGroupView {
		rr := call(principal, http.MethodGet, "/api/console/agent-groups", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s 列表 code=%d body=%s", principal, rr.Code, rr.Body.String())
		}
		var listed struct {
			Groups []consoleAgentGroupView `json:"groups"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
			t.Fatalf("%s 列表解码失败 err=%v body=%s", principal, err, rr.Body.String())
		}
		return listed.Groups
	}
	createGroup := func(principal, agentID, shortName string) model.AgentGroup {
		rr := call(principal, http.MethodPost, "/api/console/agent-groups",
			`{"agent_id":"`+agentID+`","short_name":"`+shortName+`"}`)
		if rr.Code != http.StatusCreated {
			t.Fatalf("🔴 判据⑤:%s(role=owner)**自建编组失败**,code=%d body=%s\n"+
				"   若这一红的成因是创建路径被加上了 management 判据 ⇒ 那就是**「非管理者不能建组」**,\n"+
				"   与业主 2026-09-29 逐字「非管理者也可以自建 Agent 编组」相悖(= 判据⑥ 的反向变异形状)。\n"+
				"   创建路径本来只要求「组长是调用者自己拥有的 Agent」＋该 endpoint 存在,没有任何 management 判据;\n"+
				"   查 internal/m8/console_agent_groups.go 的 POST 分支开头是不是多了 platformRole 判定。",
				principal, rr.Code, rr.Body.String())
		}
		var group model.AgentGroup
		if err := json.Unmarshal(rr.Body.Bytes(), &group); err != nil || len(group.Code) != 32 {
			t.Fatalf("建组应答解码 err=%v group=%+v body=%s", err, group, rr.Body.String())
		}
		return group
	}

	// ⑤ 第一腿:role=owner 的 owner-a **真 handler + 真库**建组成功。
	groupA := createGroup("owner-a", a1.ID, "sgeowna")
	if groupA.CreatorPrincipal != "owner-a" {
		t.Errorf("creator_principal=%q,应为 owner-a(他是创建者)—— can_manage 与删除权都挂在这个字段上", groupA.CreatorPrincipal)
	}
	if groupA.LeaderKeyID != a1.KeyID || groupA.LeaderSessionName != a1.SessionName {
		t.Errorf("组长端点=%q/%q,应为他自己那个 Agent %q/%q", groupA.LeaderKeyID, groupA.LeaderSessionName, a1.KeyID, a1.SessionName)
	}
	// ⑤ 建完该组**对他可见**,且 can_manage=true(他是 creator)。
	ownA := listGroups("owner-a")
	if len(ownA) != 1 {
		t.Fatalf("🔴 装置自证失败:owner-a 建完组后列表里应恰好 1 个,实为 %d 个:%+v\n"+
			"   列表为空 ⇒ 下面 can_manage 那条断言证不了任何事(空集合不等于排他)", len(ownA), ownA)
	}
	if ownA[0].Code != groupA.Code {
		t.Errorf("列表里的组 code=%q,与刚建出来的 %q 不是同一个", ownA[0].Code, groupA.Code)
	}
	if !ownA[0].CanManage {
		t.Errorf("🔴 can_manage=false:他是 creator(canManageConsoleAgentGroup 认的第一种归属就是 creator)。\n" +
			"   本格同时是判据④ 的**阳性对照** —— 那里断言 owner 对**别人的**组 can_manage=false;\n" +
			"   若这里也是 false,那个 false 可能只是字段恒为 false(观测量对变异不敏感)⇒ 不采信。")
	}

	// ⑤ 第二腿:**另一位** role=owner(owner-b)同样能建 ⇒ 不是"某个 principal 被特批"。
	groupB := createGroup("owner-b", b1.ID, "sgeownb")
	if groupB.Code == groupA.Code {
		t.Errorf("两位业主建出来的组 code 相同(%q)⇒ 组码不唯一,凭组码入组那条路会串", groupB.Code)
	}
	ownB := listGroups("owner-b")
	if len(ownB) != 1 || ownB[0].Code != groupB.Code {
		t.Fatalf("owner-b 应只看到**自己**建的那一个组,实为 %d 个:%+v", len(ownB), ownB)
	}
	if !ownB[0].CanManage {
		t.Errorf("owner-b 对自己建的组 can_manage 应为 true,实为 %+v", ownB[0])
	}
	// 🔵 顺带钉住「能自建」**不等于**「互相看得见」:owner-b 建完之后,owner-a 那一趟仍只有他自己的组。
	if again := listGroups("owner-a"); len(again) != 1 || again[0].Code != groupA.Code {
		t.Errorf("owner-b 建组后,owner-a 的列表应仍只有他自己那一个,实为 %+v", again)
	}
}
