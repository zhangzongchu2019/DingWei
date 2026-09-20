# 交付件 · Agent 编组入口(0.2.1.7)· qwen38maxkhovvob4

基线 `4afd17f0`(= preprod tip / 已发的 0.2.1.6 之上,我这条分支的第 5 个提交起)。
组长照准处:ts-No.5 ③④(形式=`target="_blank"` + 断言"点击后 pane 数不变";只对 management 发 + 断言非 management 看不到)、
ts-No.9 ③(「你列的闸 ①②③④ 与三个变异全部照准,直接做,不必再报我」)。
板件锚 `a3003f42`(其中「两人独立收敛 — 平台项 `_blank` 必须逐项」一节已把 per-item target 钉死)。

---

## 一 一句话

console 左栏「Work platform」里给 management 加一个 **Agent 编组** 平台项,指向 `/admin/agent-groups`,
**逐项** `target="_blank"`;点击后在新标签页打开,**已打开的 Agent pane 与它们的 SSE 一条都不动**。

活缺陷(改前实测,不是推论):点一下 ⇒ 顶层文档被导航走 ⇒ **pane 2→0、tab 2→-1、两条 SSE 全断、后退回不来**。

---

## 二 判据(每条用什么验的 · 改前是否红过)

| # | 判据 | 用什么验 | 改前红过吗 |
|---|---|---|---|
| ① | management 拿到 `agent-groups` 平台项,`href=/admin/agent-groups`,`target=_blank`,且**唯一可寻址** | Go 腿 `TestConsolePlatformAgentGroupsEntryIsManagementOnly` + 浏览器腿 G1 | 🔴 **红过(断言级)** |
| ② | 点击后:新页打开 · console 顶层还在 · pane 数不变 · **iframe 对象不变**(标记仍在) · **两条 SSE 仍 OPEN** | 浏览器腿 G2(10 项现场一次性收齐再断言) | 🔴 **红过(断言级,10 条违约)** |
| ③ | 非 management(owner)**看不到**它,而 builtin `agents` 仍在、平台项仍只 1 个 | Go 腿排他腿 + 浏览器腿 G4(两腿都在才排他) | ⚪ 改前绿(见下"零红腿") |
| ④ | 它必须是**平台项形状**(沿用 builtin 先例),不是往 nav 里硬塞 HTML | 载荷由真 handler 现产;G1 断言 `#platform-items a[data-item-id=…]` | 🔴 改前红(项不存在) |
| ⑤ | 🔴 **逐项**,不是一律:builtin `Agents`(`href=#agents` 页内锚)**不得**带 target、点击**不得**开新页 | Go 腿 `TestConsolePlatformBuiltinAgentsKeepsInPageAnchor` + 浏览器腿 G3 | ⚪ 改前绿(见下"零红腿") |
| ⑥ | `agent-groups` 这个 id **保留**,不许被提交占用(否则页面出现两个同 `data-item-id`) | Go 腿 `TestConsolePlatformAgentGroupsIDIsReserved` | 🔴 **红过**(改前提交同 id 返回 201) |

### 🔴 改前"红"的**质量**分层(别把红过当验过)

改前正常跑 4 条里红 2 条,但**红的性质不同**:

    G1 红 = 断言级(#platform-items 里没有该项)
    G2 红 = 断言级(前置坏了:没有该项,本格无从点击)—— 🔴 **它没验到点击行为本身**
    G3/G4 绿 ⇒ **改前这两条是零红腿**,判别力只能由变异证明(M3 → G3 红、M4 → G4 红)

⇒ 所以我另跑了**阳性对照**:把载荷里那一项的 target 摘掉(产品还没发该项时按裁定形状合成),
   让 G2 在"入口存在但没有 target"的状态下真去点一次。**这才是 G2 判别力的证据**:

    改前·阳性对照跑(POSITIVE_CONTROL=1):**只红 G2**,10 条违约,rc=1
      - 点击后没有新标签页打开 ⇒ target 没生效,顶层文档被导航走了
      - console 顶层文档已不是控制台(#platform-items 没了)⇒ 后退回不来
      - console 顶层 URL 变了:http://127.0.0.1:41379/admin/agent-groups
      - pane 数 2→0(已打开的 Agent 页全丢)· tab 数 2→-1 · pane 标题集 []
      - A/B pane 的 iframe 对象都没了(frame-gone)· A/B 的 SSE readyState=undefined
      点击前={"url":"…/console","panes":2,"tabs":2,…} 点击后={"url":"…/admin/agent-groups","consoleStillThere":false,"panes":0,"tabs":-1,…}

   🔵 判据"只红该红的那条"在对照跑上成立:G1(对照前提腿)/G3/G4 全绿,**只有 G2 红**。

---

## 三 🔴 两腿**机器耦合**(这一格最贵的设计决定)

浏览器腿的 `/api/console/platform` 应答**不是夹具手写的**,是
`go test -run TestConsolePlatformPayloadDump`(env-gated 装置测试)从**真 handler** 现产、写文件、再读进来的:

    改后跑打印:manager.items=[["agents",""],["agent-groups","_blank"]]
                owner.items  =[["agents",""]]

为什么必须这样:组长照准的变异是「**去掉 target ⇒ ② 红**」。
若载荷手写死在 JS 里,改 `console_platform.go` 根本传不到浏览器腿 ⇒ **那条变异打不到 ②**,
② 就只会因为"元素不存在"而红,永远不会因为"点击毁了 pane"而红 —— 那就是在测夹具。

**M2 证明两腿不冗余**(这是耦合带来的额外收获):

    M2 = 后端照发 target,但 renderPlatform 不应用它(删掉 console_auth.go 那一行)
    ⇒ Go 腿 **全绿**(载荷里 target=_blank 确实在)
    ⇒ 浏览器腿 **G1+G2 红**
    🔴 这正是本组一整晚在治的形状:**东西发了,但没接上,所以等于不存在。** 只有跨腿耦合能抓到它。

---

## 四 变异矩阵(每条都能解释为什么它该红)

| 变异 | 改了什么 | Go 腿 | 浏览器腿 | 解释 |
|---|---|---|---|---|
| M1 | 去掉 `Target: "_blank"` | 🔴 `target="",应为 _blank` | 🔴 G1 + **G2(10 条)** | 没有 target ⇒ 顶层导航 ⇒ pane 全丢。**这就是照准的那条** |
| M2 | renderPlatform 不应用 `item.target` | ✅ **全绿** | 🔴 G1 + **G2(10 条)** | 后端发了前端没接 ⇒ 只有浏览器腿看得见 |
| M3 | 给 builtin 页内锚也加 `_blank` | 🔴 builtin 被加了 target(两个角色都报) | 🔴 **只红 G3** | 一律加 ⇒ `#agents` 开一个空标签页 |
| M4 | 去掉角色判定(`if true`) | 🔴 排他腿 + 🔴 **既有** `TestConsolePlatformHandlersMountAgentsAndEnforceWorkflow` | 🔴 **只红 G4** | owner 也看到了;既有闸也抓到了(owner 项数 1→2) |
| M5 | 不保留 `agent-groups` id | 🔴 同 id 提交应被拒(实为 201) | ✅ **4/0 全绿** | 提交侧的问题,渲染侧看不见 ⇒ 🔴 **浏览器腿对 M5 零判别力**,判别力只在 Go 腿 |

M5 == "不发这一项"的改前状态我另有两跑(正常 + 阳性对照),不重复计。

**还原核验**:每条变异跑完 `sha256sum -c` 两个产品件均 `OK`(5/5),
末次全树核验亦 `OK`;`git status` 只剩我这格的 3 改 2 新。
🔴 脚本用 `setsid nohup` 跑(上一格有过变异中途被 SIGHUP、产品件停在变异态的事故)。
🔵 一处日志瑕疵如实报:脚本里 `go rc=` / `js rc=` 那两行取错了 `PIPESTATUS`,**恒为 0,不作证据**;
   证据是 `--- FAIL:` / `🔴` / `合计` 行本身(均已逐字留存)。

原始日志:`/tmp/dwmock/mut-agentry.log`(变异矩阵全文)、
`ag-entry-prefix-normal.log`(改前正常跑)、`ag-entry-prefix-control.log`(改前阳性对照跑)、
`ag-entry-postfix-normal.log`(改后跑)。

---

## 五 改后 · 逐字

    Go 腿  go test ./internal/m8/ -run TestConsolePlatform -count=1 -v
      --- PASS: TestConsolePlatformAgentGroupsEntryIsManagementOnly (0.11s)
      --- PASS: TestConsolePlatformBuiltinAgentsKeepsInPageAnchor (0.11s)
      --- PASS: TestConsolePlatformAgentGroupsIDIsReserved (0.05s)
      --- SKIP: TestConsolePlatformPayloadDump (装置,仅闸取载荷时运行)
      --- PASS: TestConsolePlatformRoleMatrixIsEnforcedByServer (0.05s)   ← 既有
      --- PASS: TestConsolePlatformUnknownPrincipalIsForbidden (0.05s)    ← 既有
      --- PASS: TestConsolePlatformHandlersMountAgentsAndEnforceWorkflow (0.06s) ← 既有(owner 仍恰好 1 项)
      ok  github.com/zhangzongchu2019/dingwei/internal/m8  0.444s

    浏览器腿  node console-agent-groups-entry-regression.js
      · console 页取自生产源码 internal/m8/console_auth.go(11386 字节)
      · 平台载荷取自真 Go handler(TestConsolePlatformPayloadDump):manager/owner 两份
      ✅ G1 management 必须拿到「agent-groups」平台项,且 target=_blank
      ✅ G2 点击入口后 pane 数不变、iframe 对象不变、SSE 不断、console 页还在
      ✅ G3 builtin「Agents」仍须页内锚(#agents、无 target、点击不开新页)
      ✅ G4 非 management(owner)必须看不到入口,而 builtin agents 仍在
      合计 4 条:✅ 4 · 🔴 0     rc=0

---

## 六 回归(双树同跑)

基线树 `/tmp/dwbase` = `git worktree add … 4afd17f0`(本格改动之前);当前树 = 工作区(未提交态)。
🔴 两树**同一轮**跑,不采信上一格记下的旧数字(「旧结论在新代码下未必仍成立」)。

| 套件 | 基线 4afd17f0 | 改后 |
|---|---|---|
| console-theme-global-regression.js | rc=0 | rc=0 · **通过 10 / 失败 0** |
| renderer-t145-tabs-regression.js | rc=0 | rc=0 · 通过 8 / 失败 0 |
| renderer-t146-agent-refresh-regression.js | rc=0 | rc=0 · 通过 4 / 失败 0 |
| renderer-t138-shortname-regression.js | rc=0 | rc=0 · 通过 5 / 失败 0 |
| renderer-in-console-iframe-regression.js | rc=0 | rc=0 · 通过 2 / 失败 0(真 console iframe 路径) |
| console-browser-regression.js | rc=0 | rc=0 · passed=5 failed=0 |
| renderer-c2-regression.js | **rc=1** | **rc=1**(两树同) |
| renderer-0206-file-render-regression.js | —(renderer 字节未变,见下) | rc=0 · 通过 40 / 失败 0 |
| console-agent-groups-entry-regression.js | 本树无此件 | rc=0 · 4/0 |

    renderer-c2 的 rc=1 是**既有环境红**,两树同因:`ENOENT … path: 'renderer-c2.html'`(缺夹具文件),
    已在我上一格交的"十件非零退出逐件原因表"里(板件登记项 `0.2.1.35`-⑭)。

    Go:`go test ./internal/m8/ ./internal/admin/ -count=1` ⇒ **rc=0**
        ok internal/m8 40.204s · ok internal/admin 2.644s

    `gofmt -l` 对我碰的三个文件**无输出**。
    🔵 顺带如实报:`internal/m8/` 里有三个文件(`console_session_renew_test.go`、`delivery_id.go`、`reply_origin.go`)
       **本来就不是 gofmt 干净的**(HEAD 版本即如此),我没碰它们。

## 七 发布影响

    view-service/renderer-canary-sse.html  sha256 = 666427bf6551379c0f5890123d0c7e2f197a10df8cb2a80f2ba1c5c8e9340efe
    🔴 **本格未改 renderer 一个字节**(sha 与上一格交付时相同)⇒ **不新增** `deploy/invariants-dingwei-gray.json:16` 的待抬项;
       那条仍只欠 0.2.1.7 的遮罩+主题两格(同一个值,一次抬到位)。
    console 页在二进制内(`consolePageHTML`)⇒ 无需改 invariants;部署时 `DW_EXPECT_WORKPULSE_SHA` 由发布单填。

---

## 八 产品改动(3 文件,+23/-3)

    internal/model/model.go        +1  ConsolePlatformItem.Target `json:"target,omitempty"`
                                        🔵 空值 = 同标签页,与既有行为逐字相同 ⇒ **不需要迁移**,库里的项也不带这字段
                                        🔵 为保持 gofmt 的对齐组,`Status` 那行的尾随注释被重排了一处空白(仅此)
    internal/m8/console_platform.go +20/-2
                                        · HandleConsolePlatform:role==management 时追加 builtin 项(target=_blank)
                                        · consolePlatformReservedItemIDs{"agents","agent-groups"}(判据⑥)
    internal/m8/console_auth.go     +1  renderPlatform:if(item.target) a.target=item.target;
                                        🔴 该行在 t138 的禁段(renderAgentList→refreshAgents,451-469)**之外**,
                                           且不含 `getElementById('panes')`/`.src=`/`.remove()`;
                                           t145 会把整段 console `<script>` 放进极简替身求值 ⇒ 该行不碰
                                           window/localStorage/documentElement,沙箱安全。

新增测试件 2:
    internal/m8/console_platform_agentgroups_test.go          (Go 腿 + env-gated 载荷装置)
    view-service/console-agent-groups-entry-regression.js     (浏览器腿;rc: 0 全过 / 1 断言失败 / **3 环境缺失**)
    🔵 rc=3 是给"没有 go toolchain ⇒ 取不到真载荷"留的:**本件不验,不是产品坏了**
       (板件登记项 `0.2.1.35`-⑭ 说的正是"rc 上分不出环境还是断言",这里先在自己件上分开)。

---

## 九 🔴 一个我没擅自改的产品决定(需要你或业主一句)

**console 角色与 admin 鉴权是两个独立的域,而"只对 management 发"可能让业主自己看不到这个入口。**

    读码(非实测):
    · `internal/store/console_auth.go:218-232` `ResolveConsolePlatformRole` 的 COALESCE 兜底是 `'owner'`,
      函数注释自己写着「Every active console principal is an ordinary owner **unless explicitly promoted**
      to the management team」⇒ **management 需要显式提升**,默认拿不到。
    · `internal/admin/admin.go:2283-2299` `requireAuth` 要的是 **`wp_admin` cookie**(没有就 303 → `/admin/login`),
      与 console principal 的角色**互不相干**。

⇒ 两道门:①要有 `console_platform_role.role='management'` 才**看得见**入口;
        ②还要有 `wp_admin` 会话才**打得开** `/admin/agent-groups`。
   若业主的 console 身份是默认 `owner`,他**看不见**这个入口(而 admin 后台他可能一直进得去)。

我按你的裁定原样实现了(只对 management 发 + owner 看不到的断言),**没有**擅自放宽。
要翻转成"owner 也可见"只需改一行判定 + 改 G4/Go 排他腿的期望;
要不要翻转、以及业主的 principal 是否已被提升为 management,**我没有数据**(不碰生产库)。
🔵 建议:出单前由你确认业主那条 principal 的角色,否则可能出现"功能做了、业主看不见"——
   本组今晚第十次撞同一个形状。

---

## 十 没验到什么(逐条)

1. 🔴 **`/admin/agent-groups` 用的是夹具替身页**,不是真 admin 页。
   本件只验"新标签页里打开了那个文档、console 顶层没被带走",
   **完全没验 admin 侧鉴权**(`wp_admin` 缺失时 303→/admin/login 那条真路径),也没验 admin 页内容。
2. 🔴 **没在真 hub + 真 view_service 上跑**(与上一格同一边界)。console 页是从生产源码实时抠的、
   载荷是真 handler 现产的,但**HTTP 服务是夹具**;真部署下的反代/同源/cookie 作用域**未验**。
   特别是:真环境里 console 与 `/admin/*` 是否**同一个 origin** 我**没验** ——
   若不是,`href="/admin/agent-groups"` 会打到 console 那个 host 上去(**这会让入口 404**)。
   ⇒ 🔴 **这是本交付里最该被复核的一条。**
3. **没验中键/Ctrl+点击、键盘 Enter 打开**这些等价入口(只验了左键 click)。
4. **没验窄窗口**(上一格 420x900 那档):`#platform-items` 在窄面板下的排布与可点性**未测**。
5. 🔴 **M5 在浏览器腿零判别力**(4/0 全绿),那条判据只有 Go 腿在守。已在表里标明,不藏在"5 个变异全红"这种说法里。
6. ⚪ **G3/G4 改前是零红腿**,判别力由 M3/M4 证明,**不由改前那次红证明**。
7. **G2 的"iframe 对象不变"是用 `window.__marker` 判的**:它能证明文档没被重建/重载,
   **不能**证明"是同一个 DOM 元素对象"(元素被摘下来再插回去、文档没重载的话,标记会留着)。
   SSE 那条(`readyState===1`)是独立证据,不依赖标记。
8. **pane 上限(8)那一侧没测**:本件固定开 2 个 pane。"入口在满 tab 时点击是否仍不挤掉任何 pane"**未验**。
9. 🔵 **装置坑一条(已修,但同族风险还在别处)**:`locator.elementHandle()` **默认自动等待 30s**,
   我的 `frameOf` 原先 50 次重试 ⇒ 失败路径上要 **25 分钟**;改前第一版对照跑就这样**挂住 2 分钟无输出**才查出来。
   已改成 `{timeout:200}` × 25 次。🔴 **`console-theme-global-regression.js` 里的同名助手有同一潜在问题**
   (那边元素总在所以没暴露)——**我没顺手改它**(它已交付、已推,改了要重跑重证),登记在此。
10. **`TestConsolePlatformPayloadDump` 是个装置而不是断言**:它跳过时(未设 env)不提供任何保证;
    它写出的是"当时的载荷",若 handler 与 dump 之间出现分叉(例如有人只改 dump),**没有机制会发现** ——
    这一层我**没有**再加对账(要加就是"用闸验闸",无穷退后)。
