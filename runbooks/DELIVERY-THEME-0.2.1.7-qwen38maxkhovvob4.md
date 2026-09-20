# `0.2.1.7` · 日夜主题【全局化 ＋ 持久化】交付件

**实施** `qwen38maxkhovvob4`
**派工** 组长 `opus50ts` ts-No.5(裁定:主题先做、**全栈归我**、console 侧也归我)· 板锚 `3f57754b`
**分支** `qwen38maxkhovvob4/u7-shot-crop-assert`(仓 `git@git.code.tencent.com:wego-ai/dingwei.git`)
**基线** `84eb1f10`(`preprod` == `release/0.2.1.6`)
**本格三提交** `ab113632`(闸,**当时是红的**)→ `6778bf40`(产品)→ `96f4e6a9`(闸补 T9/T10)
**改动范围**

    产品  internal/m8/console_auth.go            console 页:暗色 CSS 块 ＋ ◐ 落位 ＋ dwTheme* 一组函数
    产品  view-service/renderer-canary-sse.html  接收并应用 ＋ 本地 ◐ 由"只管这一 tab"提升为全局
    测试  view-service/console-theme-global-regression.js   新件,10 条判据

    sha256  console_auth.go            fefe6072164ed91023e43a6b577ab596e9a7bc9129ee736a05d766930abe3f40
            renderer-canary-sse.html   666427bf6551379c0f5890123d0c7e2f197a10df8cb2a80f2ba1c5c8e9340efe
            (改前 renderer 为 f459a284…,即遮罩那格修完的那一份)

🔵 `6778bf40` → `96f4e6a9` **只有测试件变了**,两个产品件的 git blob 逐字节相同
(`7f61e701…` / `53e953f9…`,已 `git rev-parse` 双向核过)⇒ 第四节那批回归结论**可以合法沿用**,不是"旧结论直接抄"。

---

## 〇 这格的主体在 console 侧,不在 renderer 侧(业主原话定的)

    业主:「◐ 应该要**提出来,放在整个 Agent 列表**,而不是某一个 tab」

⇒ 开关的**归属**在 console 页(`consolePageHTML`),renderer 侧只负责**接收并应用**。
改前实测(探针,不是读码):在 pane A 点 ◐ ⇒ **A=dark、B=light、外壳无 data-theme、localStorage 键数=0**
—— 正是业主说的那个"某一个 tab"。

🔵 组长 ts-No.5 指出的一件我原先没连起来的事:**我为了验遮罩造的那个装置,正好就是主题这格缺的场景**
(单一源 ⇒ 同源 localStorage 天然成立;console 页从 Go 源码实时抠 ⇒ 真 tab 机制;真 iframe ⇒ "已打开的 pane")。
本格的闸就是那套装置加一个 Agent 扩成两个 pane。⇒ 后备件第三节那三条推论**我全部自己实测了,没等 `opus50tc2k`**。

---

## 一 运行方式(🔴 cwd 必须是 `view-service/`)

```text
cd view-service          # 🔴 本仓这批套件都用相对路径读源码('../internal/m8/console_auth.go'、
                         #    'renderer-canary-sse.html')⇒ 从仓根跑会 ENOENT,而报错看着像产品坏了
NODE_PATH=<playwright-node_modules> PW_CHANNEL=chrome \
  node console-theme-global-regression.js
```

**结果**:`通过 10 / 失败 0`,`rc=0`。

🔵 本件**不需要**契约替身(自带 HTTP 装置),也**不需要**Playwright 自带 chromium(吃 `PW_CHANNEL`)。

---

## 二 十条判据 → 各自钉住什么(判据落在 `assert` 上,不落在注释上)

| # | 判据 | 钉住的是 |
|---|---|---|
| T1 | 开关在**顶层 console 的 Agent 列表区**、可见、有可访问名 | 业主那句"提出来";排他:pane 里那个 ◐ 允许保留,但**不能只有它** |
| T2 | 点它 ⇒ 外壳 ＋ 可见 pane ＋ **隐藏 pane** 都**真的变暗** | 全局生效;隐藏 pane 是 `display:none` 的**活文档**,靠"显示时刷新"永远救不到 |
| T3 | 点下去的**同一个 JS 任务里**外壳就已变暗、且已落盘 | 写入方**自己 apply**(推论③);异步事件不可能是原因 |
| T4 | `localStorage` 有、`sessionStorage` **没有**;重载仍暗;**新开一个 tab** 仍暗 | "含持久化";第三条腿是 localStorage / sessionStorage / 纯内存 的**判别器** |
| T5 | 在 pane 里点它自己的 ◐ ⇒ 外壳与**另一个** pane 跟随 | 两入口一致,不是各管各的 |
| T6 | **一个 pane 都不开**时,console 自己点也必须落盘、重载后保持 | 钉住"是 console 落的盘",不是 renderer 替它落的 |
| T7 | `localStorage` **被禁**时,广播仍必须送到隐藏 pane | 钉住 **BroadcastChannel** 那条路 |
| T8 | **没有 BroadcastChannel** 时,主题仍必须全局生效 | 钉住 **storage 事件**那条退路(pane 侧) |
| T9 | 两个 tab **同时开着**,A 点 ⇒ B **实时**跟随(**不重载 B**) | 跨 tab 广播;与 T4 的"读档"是**两条不同的腿** |
| T10 | 两个 tab ＋ **没有 BroadcastChannel** ⇒ 仍实时同步 | 钉住 **console 侧**的 storage 退路 |

**判据的两个写法选择(都是防假绿)**

    · "变暗"用**相对亮度**(WCAG 公式)判,不是"某个值变了" ⇒ 挡住"改了变量但颜色一样亮"
    · 外壳量**两个表面**(页面有效底色 ＋ tab 条底色)⇒ 挡住"body 暗了、tab 条还白"的花脸
    · 每条都带**前置守卫**:点前必须浅色且亮度 >0.5、隐藏 pane 必须真是 display:none、
      存储/频道必须真被禁掉、两个 tab 必须真共享存储 —— **守卫不成立则本条自动作废**,不静默变绿

---

## 三 🔴 改前的红(`0/8`,`rc=1`)—— 原文

```text
🔴 T1 -> console 顶层【没有】#themeBtn ⇒ 开关还只在各 pane 里,业主那句"提出来"没落地
🔴 T5 -> 两个入口不一致(共 3 项):
   - pane 内点 ◐,console 外壳没跟随:rgb(255, 255, 255) → rgb(255, 255, 255)
   - pane 内点 ◐,【另一个】pane B 没跟随(它自己是暗的、别人还是白的 ⇒ 正是业主说的"某一个 tab"):
     {"theme":"light","bg":"rgb(246, 247, 249)","hasOwnToggle":true}
   - pane 内点 ◐ 没有落盘到 localStorage['dw-theme'],实测 undefined
T2/T3/T4/T6/T7/T8 -> page.click 找不到 #themeBtn(30s 超时)/ getElementById 返回 null
通过 0 / 失败 8   (日夜主题全局化 + 持久化)
```

🔴 **如实标注(别把"红过"当成"验过")**:上面 8 条里**只有 T1 与 T5 是断言级的红**;
其余六条是"开关根本不存在"的**超时红**。⇒ **那六条的判别力由八个变异证明(第五节),不是由这次红证明。**

🔵 T9/T10 是**后来**因为变异跑出零转红才补的(见第五节),所以改前那次红档是 8 条,不是 10 条。

---

## 四 改后的绿 ＋ 回归(方法:**基线树与改后树各跑一遍同一套扫描**)

**改后(提交后 tip 上,rc 全部显式捕获)**

    console-theme-global-regression.js   通过 10 / 失败 0   rc=0
    renderer-in-console-iframe(遮罩)     通过 2  / 失败 0   rc=0
    renderer-0206-file-render(㉗㉘㉙)    通过 40 / 失败 0   rc=0
    console-browser-regression(C2)       通过 5  / 失败 0   rc=0
    renderer-t145-tabs(console 源码抽验) 通过 8  / 失败 0   rc=0
    renderer-t138-shortname              通过 5  / 失败 0   rc=0
    renderer-t146-agent-refresh          通过 4  / 失败 0   rc=0
    go test ./internal/m8/ ./internal/admin/   ok 40.4s / ok 2.5s   rc=0
        (含 console_page_t138_test.go 三条**结构级**测试:它们钉住 console 页里必须存在的片段,
         以及 renderAgentList→refreshAgents 段内不得出现 getElementById('panes')/.src=/.remove())

**回归对比法(🔵 值得记下来,因为它挡掉了一次假红)**

    基线树  git worktree add /tmp/dwbase HEAD(=5e262dec,改前)
    🔴 坑   renderer-0206 是从 ${MOCK_BASE}/renderer-canary-sse.html **取页面**的,而且它会核对
            "服务的那份 == 仓内那份"⇒ 两棵树共用一个替身会让基线树**假红**(报的是哈希不符,看着像产品坏了)
    处理    给基线树单起一个替身(:19444,cwd=/tmp/dwbase/view-service),两边各跑各的
    结果    21 件共有套件 **rc 完全一致**;基线替身提供 f459a284…、改后树 666427bf…,已 sha256 双向核过

**两棵树同样绿的**:`console-browser`(5/0)· `renderer-0206`(40/0)· `canary-g2` · `page-identity` ·
`t086`(6/0)· `t138`(5/0)· `t141`(9/0)· `t145`(8/0)· `t146`(4/0)· `t147`(10/0)

**🔴 两棵树同样红的 10 件 = 既有【环境不具备】,与本改动无关**(逐件取到首条错误原文):

    renderer-c2-regression            Error: ENOENT … open 'renderer-c2.html'(仓里没有这个文件)
    renderer-failcard / renderer-regression / t054-errorboard / t074-delivery-id
                                      ERR_CONNECTION_REFUSED http://127.0.0.1:19312/…(需要各自的服务没起)
    renderer-longtext                 见下(这条是**真断言失败**)
    renderer-provenance-icon          fetch failed
    renderer-sse-smoke / -profile / -reconnect
                                      ERR_CONNECTION_REFUSED :19400 / :19450 / :19460

🔴 **这 10 件正是组长 ts-No.5 ③ 说的"rc 必须分开"的实证**:它们**全部**以非零退出,
而原因是"环境不具备",从 rc 上**看不出**与"断言失败"的区别。已把这张表交给做聚合入口的 `coder56solzzc`。

---

## 五 八个变异(逐条实测;🔴 加 T9/T10 之后**全部重跑**,不采信旧结论)

| 变异 | 位置 | 红条 | 为什么该红 |
|---|---|---|---|
| **MT1** console 不广播 | `dwThemeApply` 的 publish | **仅 T7** | T7 是唯一禁掉 storage、只剩频道的档 |
| **MT2** renderer 收到广播不应用 | `themeChannel.onmessage` | **仅 T7** | 同上;🔴 补 T7 之前它是**零转红** |
| **MT3** console 不落盘 | `dwThemeApply` 的 write | T3 T6 T8 T10 | T3 同一任务内读盘(renderer 的写还没发生)· T6 无 pane 没人替它落 · T8/T10 没有频道 ⇒ 落盘触发的 storage 事件是**唯一**传输 |
| **MT4** 存储层换 sessionStorage(两侧) | 两个 store 函数 | T3 T4 T5 T6 T10 | 凡读 `localStorage` 的腿全红;T10 红是因为 sessionStorage **不跨 tab**。T2/T7/T8 仍绿:传输不看存储层 —— 可解释 |
| **MT5** console 不自己 apply(等事件) | `dwThemeShell(v)` | T2 T3 T5 T7 T8 T9 T10 | 凡断言"外壳真的变暗"的都红(写入方等不到自己的事件) |
| **MT6** renderer 加载时不读存档 | `if (stored) applyTheme(stored)` | **仅 T4** | 只有 T4 有"重载后**新开** pane"这条腿 |
| **MT7** renderer 的 storage 退路失效 | 其 storage 监听 | **仅 T8** | T8 是唯一禁掉频道的档 |
| **MT8** console 的 storage 退路失效 | 其 storage 监听 | **仅 T10** | T10 是唯一"两个 tab ＋ 没有频道"的档;🔴 补 T10 之前它是**零转红** |

每次变异后都核对:**还原后 sha256 与变异前逐字节一致**,且 `grep -c "MT[0-9]"` 为 0(无残留)。

🔴 **按判据③(每条红都要能解释):上表没有解释不了的红条,也没有零转红的变异。**

### 🔵 两次"零转红"的发现过程 —— 这比八个变异本身更值得记

    第一次  MT2(去掉 renderer 的频道接收)→ **零转红**。
            原因:console 落盘 ⇒ renderer 的 storage 监听替它把主题送到了 ⇒ 两条路互相掩盖。
            ⇒ 补 T7(禁 localStorage)/ T8(禁频道),两条路各钉一条。
    第二次  我**自己**按同一条纪律回头查:那 console 侧的 storage 监听呢?
            MT8 → **零转红**(跨 tab 那条被频道掩盖了)⇒ 补 T9/T10。

🔴 **教训(已写进提交信息)**:冗余是**故意**的(隐私模式下 localStorage 取用即抛 ⇒ 只有频道能用;
老浏览器没频道 ⇒ 只有 storage 事件能用),但**冗余必须各自被钉住**,
否则删掉任何一条都没人发现 —— 这正是本组一晚撞了十次的那个形状(东西建好了、没接上、等于不存在)。
🔵 而且第二次是**主动去找**的,不是变异碰巧暴露的:"我这闸里还有哪条路径删了没人知道?"

### 🔵 两处我自己查出并修掉的闸缺陷(判据未削弱)

    1. T2 首版断言循环写成 for (const k of [A, B]) —— A/B 是**会话名常量**,而量测对象的键是字面量 'A'/'B'
       ⇒ 取到 undefined 抛 TypeError。修的是**索引**,不是判据。
    2. 取 frame 首版用 page.frames().find(url…);MT1 那轮 T4 报"没找到 frame 对象" ——
       🔴 那是**装置抖动**(元素已 attached、frame 模型还没建好),不是主题信号。
       按判据③"解释不了的红条 = 真问题",改成 elementHandle().contentFrame() ＋ 轮询。

---

## 六 🔴 没验到什么(本节最值钱,一字不删)

1. **真后端未验。** console 页 HTML 与 renderer 字节都是**真件**,但
   `/api/console/agents`、`/api/console/platform`、`status`、`events` 四个依赖是本件自带的 HTTP 替身
   ⇒ **真 console ＋ 真后端(真 SSE 流、真鉴权跳转、真断线态)下的主题行为未验。**
   🔴 尤其没验:断线告警盖上来的时候点 ◐ 会怎样。
2. **真浏览器/真机未验。** 全部 headless(`chrome-headless-shell`)。T7/T8/T10 用 `addInitScript`
   **模拟**"存储被禁 / 没有频道",🔴 那**不是**真隐私模式、也**不是**真老浏览器
   (真隐私模式下 `sessionStorage` 也一起抛,而 renderer:1742 裸用它 ⇒ 真隐私模式下会撞上第 7 节那条既有缺陷)。
3. **暗色的观感没有做目视验收。** 我只断言了"亮度显著下降"与两个表面的颜色,
   **没有**逐元素核对暗色下的对比度/可读性,也没截图给业主看。
   ⇒ 暗色配色是我按 renderer 既有 `[data-theme=dark]` 变量集(:26-42)配的,**业主是否认可未验。**
4. **没有接入系统级偏好。** 不读 `prefers-color-scheme` ⇒ 首次访问仍固定浅色,只有点过之后才有偏好。
   业主若要"跟随系统",**这格没做,也没验。**
5. **renderer 独立页(不在 console 里)只验了一半。** T1 的对账腿证明了它自己那个 ◐ 还在;
   **没有**验"独立页刷新后是否保持暗"(它会读 localStorage,推论上保持,**未实测**)。
6. **与遮罩那格的交互未验。** ㉘ 与 iframe 那件都是在**浅色**下验的;
   🔴 **暗色下打开截屏遮罩的几何/观感没有专门跑过。**
7. **可达性只做了最低限度。** 给了 `title` 与 `aria-label`,但**没有** `aria-pressed` 之类的状态播报,
   也**没有**验 Tab 键焦点顺序、屏幕阅读器读出来是什么。
8. **只跑了 `view-service` 的浏览器套件 ＋ `internal/m8`/`internal/admin` 的 Go 测试。**
   🔴 **`go test ./...` 全仓没跑**,其他包未验。
9. **高 DPR、触摸/触控笔 pointer 仍未验**(与遮罩那格同一批遗留)。
10. **`MAX_TABS=8` 满页时的广播未验。** T2/T7/T8 只开了 2 个 pane;
    8 个 pane(其中 7 个隐藏)时广播是否全部到达,**未实测**(推论上 BroadcastChannel 与 pane 数无关)。

---

## 七 🔴 撞见的既有问题(登记,**本格一个都没修**)

1. **`renderer-longtext-regression.js` 有一条真断言失败**(不是环境问题):

       FAIL: E′: 重连重放后正文不得翻倍(应仍 1875 字,实为 3750)

   🔴 **基线树与改后树同样失败**(两份日志只差计时数字:11 PASS ＋ 同一条 FAIL)⇒ **既有缺陷,不是我造成的。**
   🔵 它一直被"这个套件是红的"掩盖着 —— 而它红的原因在别的档,所以这条真失败**没人看见**。
   与 `0.2.1.7` 无关,**登记不展开**。
2. **`renderer-canary-sse.html:1742` 裸用 `sessionStorage`(无 try/catch)**:
   `collapsed: sessionStorage.getItem('dw-taskboard-collapsed') !== '0'` 在顶层对象字面量里求值
   ⇒ **隐私模式下整页脚本加载即抛**。`:1776` 的 `setItem` 同样裸用。
   🔵 这就是我 T7 只禁 `localStorage`、**不禁** `sessionStorage` 的原因(禁了会红得与主题无关、解释不了)。
   **既有缺陷,本格不修**(边界:看见旁边的坑不下去)。
3. **这批套件的 cwd 依赖是个陷阱**:`t138/t145/t146/t147` 等用相对路径读源码,
   从仓根跑会 `ENOENT` 并以非零退出,**报错看着像产品坏了**。
   我自己就踩了一次(终验那轮 T145/T138 报 rc=1,查明是我的 cwd 错了,改到 `view-service/` 后 8/0、5/0)。
   ⇒ 已连同第 4 节那张"环境红"表一起交给做聚合入口的 `coder56solzzc`。

---

## 八 🔴 发布须知(归装配/发布,我没碰)

    renderer sha256   f459a284… → **666427bf6551379c0f5890123d0c7e2f197a10df8cb2a80f2ba1c5c8e9340efe**
                      ⇒ `deploy/invariants-dingwei-gray.json:16` 的 `want=` **仍须同步抬**
                        (与遮罩那格是同一格;若两格一起发,抬到 666427bf… 即可)
    console 页        在 Go 二进制里 ⇒ 巡检第 5 格 `workpulse-binary-matches-expected` 的 want 是
                      `${DW_EXPECT_WORKPULSE_SHA:?}`(**部署那一刻填**)⇒ **不需要改 invariants 文件**
    🔵 我**没有**碰 `deploy/`、没有碰生产、没有抬版本号。
