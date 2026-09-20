# 交付件 · `0.2.1.35`-⑬ 自动重连致正文无界增长(修复)· qwen38maxkhovvob4

板件锚 `5c984e57`(⑬ 改定并升为立即修)· 基线 `92f146de`(= 编组入口那格交付后的 tip)。
定界件在先前那封消息里(四条腿 + 三种驱动),本件只做板件裁定的 ①②③④。

## 〇 裁定逐条对账

| 裁定 | 办到了吗 | 在哪 |
|---|---|---|
| ① 走 `markStreamReconnect`,**不许**直接 `resetConversation()` | ✅ | `renderer-canary-sse.html` onopen 里 arm 的仍是 `markStreamReconnect()`;T-086 的延迟清空窗口原样保住(变异 **MR2** 专门验了"绕过它"会被抓) |
| ② 同时修 E′ 用例的过期驱动 | ✅ | `renderer-longtext-regression.js` T8 的重放改为**经产品真实消费点**;修后 E′ 绿、且被它遮住的 **T9/T10/T10b/T11/T12/T13 六格全部 PASS** |
| ③ 把 `renderer-sse-reconnect.js` 接进跑者 | 🔵 **半成品,如实报** | 我把它从"只打印、无断言、无 rc、要人工起夹具"改成了**自启自收夹具的真闸**(rc 0/2/3、cwd 无关、只用已登记的 env 面)⇒ **可一行接入**;但跑者件 `tools/run-browser-regressions.py` 在 `coder56solzzc/browser-release-gate` 分支上,**我没碰**(见第三节) |
| ④ 接受短暂闪空;逐条判重登记 ⑰ 本版不做 | ✅ | 未引入 per-event 序号;闸的判据写成"**不得增长,也不得归零**",允许重建期的瞬时下降(实测确实抓到过 `full=640` 的中间态样本) |

## 一 修复本体(4 行有效改动,+16/-4 含注释)

    1186  let streamEverOpened = false;        ← 🔴 从 connectEventStream() 的**闭包局部**提到模块级
    3841  if (streamEverOpened) markStreamReconnect();
    3842  streamEverOpened = true;

根因(读码,已由实测逐条对上):`everOpened` 是 `connectEventStream()` 的局部,
而 0.2.1.2 的自建重连每次都 **新建** EventSource(`scheduleReconnect → setTimeout → connectEventStream`)
⇒ 它每次重连都归零 ⇒ `if (everOpened)` 那支在**已发的自动重连路径上从不执行**
⇒ `consumeReconnectClear()` 永不为真 ⇒ `resetConversation()` 永不运行 ⇒ 每次重连追加一整份 ring。
EventSource **原生**重连是"同一对象二次 open",所以这个 bug 在 0.2.1.2 之前不成立(定界件腿B/腿C 实测)。

🔴 **为什么 arm 必须在 `open`,不能提前到 `scheduleReconnect`**(实现注记,不是空话):
`REPLAY_HARD_LIMIT_MS = 1000`(`renderer-canary-sse.html:1424`,实测值),窗口从 **arm 那一刻**起算,
而 backoff 首档就是 `1000ms` ⇒ 提前 arm 会让窗口在重放到达前就过期 ⇒ 等于没修。
**变异 MR4 就是这个错修法,闸抓到了**:`mark=14` 但 `reset=0`,正文照涨到 107520 字。

## 二 守闸:`view-service/renderer-sse-reconnect.js`(重写,37 行 → 276 行)

**改前它不是闸**:只 `console.log` 采样、无断言、无 rc、还要人工先起夹具
⇒ 全仓引用它的只有它自己注释里那一行(零跑者覆盖),而且**它今天是红的也没人知道**。

改后:真 EventSource + 真断链(`RECONNECT=1`:发完即 `shutdown+close`)+ hub 整批重放 ring,
**自启自收夹具、自挑空闲端口**,两条腿 12 条判据:

    L1 长正文 7680 字(>4000 折叠阈值)  L2 短正文 550 字(业主**肉眼**那一面)
    每腿 6 条:
      守卫   真断链重连确实发生了(EventSource 构造 ≥3 且 error ≥2)🔴 两态都成立,所以它能当守卫
      判据1  可取回全文长度**恒为单份**(观测最大值 == 末次 == 期望)—— 既不翻倍**也不归零**
      判据2  assistant 事件数恒为单份(48 / 4 条)
      判据3  时间线卡片数恒为 ring+1(21 / 3)—— 修法不许顺手把别的回合擦掉
      判据4  L1:折叠条**自报字数**恒为单份(业主点"打开全文/复制/下载"拿到的就是这份)
             L2:**DOM 可见字数**恒为单份
      判据5  🔴 机制钉:`markStreamReconnect ≥1` **且** `resetConversation ≥1`

🔴 **判据5 是本件最值钱的一条**(变异 MR2 证明):把 arm 换成直接 `resetConversation()` 时,
   **症状腿全绿**(正文长度完全正确!),只有机制钉红;而 `renderer-t086-deferred-clear-regression.js`
   **抓不到** —— 它抽的是 `markStreamReconnect` 的**函数体**,不含调用点(实测:MR2 下它 6/0 全绿)。
   ⇒ **"不许绕过延迟清空窗口"这条裁定,全仓只有判据5 在守。**
🔵 诚实标注:判据5 钉的是**当前修法**,不是永恒不变量。若将来改成 per-event 逐条判重(登记 ⑰),
   **这条要随之改写**(文件里已写明)。

### 接入跑者的三条契约(给 `coder56solzzc` / 组长)

    1. `node view-service/renderer-sse-reconnect.js`,**cwd 无关**(路径全走 __dirname)
    2. 调用方可控 env 面**只有两个,且都已在 OVERRIDE_ENV_NAMES 里**:`PW_CHANNEL` / `RENDERER_OVERRIDE`
       (跑者会机械扫描 `process.env.*` 并要求登记 ⇒ 我没开新面;其余参数一律显式传给子进程夹具)
    3. rc 与发布约定一致:**0 全过 / 2 断言失败 / 3 环境不可用**(缺 python3、夹具起不来、浏览器起不来 ⇒ 3)
    ⇒ 加进 SUITES 只需一行:`("renderer_sse_reconnect", ROOT / "view-service/renderer-sse-reconnect.js")`
    🔵 它自启的夹具用**自挑空闲端口**,不与跑者起的 0206 mock 抢端口。
    🔵 阳性对照(证明它有牙):`RENDERER_OVERRIDE=<改前字节> node …` ⇒ 必须 rc=2(取法写在文件头)。

## 三 改前红 / 改后绿(逐字摘要)

    改前(基线件 sha256=666427bf…):合计 12 条 → ✅4 · 🔴8   rc=2
      🔴 L1 全文长度出现过 **107520** 字(应 7680)· assistant 涨到 **672**(应 48)
      🔴 L1 折叠条自报字数出现过 15360,23040,…,99840,**107520**(应恒为 7680)
      🔴 L2 全文 **8800**(应 550)· assistant **64**(应 4)· **DOM 可见涨到 3850**(应 550)
      🔴 两腿机制钉:`markStreamReconnect` 调用数 = **0** ⇒ 幂等机制在这条路径上是死代码
      ✅ 两腿守卫(ctor=14/16、errors=14/16 ⇒ 重连真发生了)· ✅ 两腿卡片数(21 / 3,不增)
    改后(sha256=f0051852…):**合计 12 条 → ✅12 · 🔴0   rc=0**

    longtext 套件:改前 `rc=1`(E′ FAIL 1875→3750,**且硬 assert 把后面 6 格一起中止**)
                  改后 `ALL PASS rc=0`,E′ 绿 + **T9/T10/T10b/T11/T12/T13 六格解除遮蔽后全部 PASS**
    🔴 那 6 格自 T-086(`13e72586`,09-15)起**没人跑过** —— 掩盖是双向的:
       一条假失败不只误导人,它还**吃掉了同套件后面所有断言的执行**。

## 四 变异矩阵(5 条,每条都能解释;还原 sha256 -c 全部 OK)

| 变异 | 重连闸 | T-086 闸 | longtext | 解释 |
|---|---|---|---|---|
| MR1 回退修复(`everOpened` 退回闭包局部) | 🔴 **8 红**/4 绿(mark=0,full 107520) | — | — | 修复那一行被盯着 |
| MR2 onopen 里改成直接 `resetConversation()`(**违反裁定①**) | 🔴 **2 红**/10 绿 —— **只红机制钉**,症状腿全绿(full=7680/550 正确) | ✅ **绿**(抓不到) | — | 🔴 见第二节:全仓只有机制钉守这条裁定 |
| MR3 彻底不 arm(删掉 onopen 那一句) | 🔴 8 红(mark=0,full 107520) | — | — | 与 MR1 同形状,但隔离的是"那一句"而非"标志作用域" |
| MR4 **错的修法**:arm 提前到 `scheduleReconnect` | 🔴 8 红(**mark=14 而 reset=0**,full 107520) | — | — | 1000ms 窗口 vs 1000ms backoff ⇒ 窗口过期、清空取消 ⇒ 等于没修 |
| MR5(**测试侧**变异)E′ 驱动改回过期写法 | — | — | 🔴 `FAIL E′ 1875→3750` | 驱动修复是承重的;它一红就重新遮住 T9..T13 |

🔵 变异脚本用 `setsid nohup` 跑(上一格有过被 SIGHUP 打断、产品件停在变异态的事故);
   每条跑完 `sha256sum -c` 两个件均 OK(5/5),末次全树核验亦 OK。
🔵 一处日志瑕疵如实报:MR 脚本里的 rc 提取写在管道后,不可靠 ⇒ **不作证据**,
   证据是 `合计 …✅/🔴`、`FAIL:`、`sha256=` 这些行本身(逐字留在 `/tmp/dwmock/mut-reconnect.log`)。

## 五 双树回归(同一轮跑,不采信旧数字)

基线 `/tmp/dwbase` = `git worktree add … 92f146de`(修复前);当前树 = 工作区。
🔴 0206 那件会从 mock 取 renderer 字节做契约校验 ⇒ **两树各起一个 mock**
(基线 :19444 cwd=/tmp/dwbase/view-service,当前 :19443),否则基线会去校验改后的字节。

| 套件 | 基线 92f146de | 改后 | 说明 |
|---|---|---|---|
| renderer-sse-reconnect.js | rc=1(`name:'Error'`,旧版要人工起夹具) | **rc=0 · 12/0** | 🔵 本格重写;顺带**从"十件环境红"里减掉一件** |
| renderer-longtext-regression.js | rc=1(E′ FAIL) | **rc=0 · ALL PASS** | 🔵 E′ 驱动修复 + 6 格解除遮蔽 |
| console-agent-groups-entry | rc=0 · 4/0 | rc=0 · 4/0 | 上一格 |
| console-theme-global | rc=0 · 10/0 | rc=0 · 10/0 | |
| renderer-0206-file-render | rc=0 · 40/0 | rc=0 · 40/0 | 🔴 改后 renderer 字节仍过契约 |
| renderer-t086-deferred-clear | rc=0 · 6/0 | rc=0 · 6/0 | 延迟清空机制未被破坏 |
| renderer-in-console-iframe | rc=0 · 2/0 | rc=0 · 2/0 | |
| t138 5/0 · t141 9/0 · t145 8/0 · t146 4/0 · t147 10/0 · canary-g2 · page-identity | rc=0 | rc=0 | 全部一致 |
| c2 / failcard / provenance-icon / regression / sse-profile / sse-smoke / t054 / t074 | rc=1 | rc=1 | **两树同因**(缺 `renderer-c2.html`、缺 :19312/:19400/:19450/:19460 的服务)⇒ 既有环境红,板件登记项 ⑭ 那张表 |

⇒ **23 件里只有我改的那两件 rc 变化,其余 21 件两树完全一致。**
Go 侧本格**零改动**(`git status` 只有三个 view-service 文件),上一格的 `go test ./internal/m8/ ./internal/admin/` rc=0 仍适用。

## 六 🔴 发布影响:renderer sha256 **变了**,invariants 待抬值要换

    改前  666427bf6551379c0f5890123d0c7e2f197a10df8cb2a80f2ba1c5c8e9340efe   ← 遮罩+主题两格抬到的值
    改后  f0051852d8495f935141543b4c6c443798d43090a8d5e55ca2286f1af485c417   ← 🔴 **本格之后的新值**

`deploy/invariants-dingwei-gray.json:16` 的 `want=` 仍由组长出单时带(**我只报不碰**),
但**待抬的值从 `666427bf…` 变成 `f0051852…`** —— 三格(遮罩/主题/本格)一起发时抬到后者。
🔴 若出单时仍按 `666427bf…` 抬,巡检会立刻判不成立(它硬钉 renderer sha256)。

## 七 没验到什么(逐条)

1. 🔴 **没在真 hub + 真 view_service 上跑**。夹具按 `internal/m8/terminal.go:645`
   「先回放 eventRing,再挂实时流」的语义模拟;"真链路语义与夹具一致"是**读码**结论
   (另有既有 Go 测试 `hub_test.go:6361 TestInternalViewEventsReplaysOnSubscribe` 佐证),**未实测**。
2. 🔴 **没测"慢 hub / 大 ring"**:窗口只有 `REPLAY_HARD_LIMIT_MS=1000`,
   若首条重放在 open 后 **>1s** 才到 ⇒ 窗口过期、清空取消 ⇒ **本缺陷会原样回来**。
   夹具是"连上就立刻重放",**这条路径未实测**。⇒ 建议登记(见第八节 ⑱)。
3. **没测"断在流中途"**(夹具是发完即断)。中途断链时重放的仍是整份 ring,追加行为同理,
   但**具体字数形态未实测**。
4. **只测了网络断链这一种触发**。代码注释列的另两种(休眠唤醒 / 后台标签页被断流)**未测**。
5. **没在 console iframe 里测**(业主实际走 `console_auth.go:306` 那条)。
   iframe 内 EventSource 行为**推论相同,未实测** —— 上一格遮罩那件我栽过同类推论,所以明写。
6. **只跑约 20 秒 / 14~16 次重连**。增长是线性的,但**长时段的内存与渲染开销未实测**;
   "会越来越卡"仍是**推论**(依据:`group.events` 无界 + 每次 rebuildTurn 遍历全部 events)。
7. 🔴 **"ring 空时不白页"(T-086 的原始目的)没有端到端闸**。
   `renderer-t086-deferred-clear-regression.js` 是**抽函数体求值**的单元闸,
   **MR2 实测证明它抓不到"调用点被绕过"**。⇒ 建议登记(见第八节 ⑲)。
8. **`RECONNECT_BACKOFF_MS` 的长档未测**:夹具快断快连,而 `markStreamConnected()` 会把
   `reconnectAttempt` 归零 ⇒ backoff 一直停在首档 1s。**5s/10s/30s 那几档下的行为未实测**
   (MR4 已证明"提前 arm"在 1s 档就会露馅,更长档只会更露,但这是推论)。
9. **判据5(机制钉)对"换成 per-event 判重"是假红**:登记 ⑰ 若实施,这条会红而产品是好的。
   已在文件里写明"要随之改写",但**没有机制保证那时的人看得到这行注释**。
10. **③ 只做了一半**:闸已可接入,但**没有真的进跑者**(跑者件在别人分支上,我没碰)。
    🔴 在它被接进去之前,**这条缺陷仍然没有自动守闸** —— 与它当初"存在却零覆盖"的状态只差一步。

## 八 建议登记(我不自己开格)

    ⑱ `REPLAY_HARD_LIMIT_MS=1000` 与"首条重放必须 1s 内到达"是一对**没有闸的隐式契约**;
       慢 hub / 大 ring 下会静默退回"追加"形态。要么放宽窗口、要么给窗口过期一条可见告警。
    ⑲ T-086 的"ring 空 ⇒ 不白页"缺**端到端**守卫(单元闸抓不到调用点被绕过,MR2 已证)。
    ⑳ longtext 套件是**一个硬 assert 串到底**:任一格失败即中止后续所有格
       (E′ 那条假失败吃掉 6 格半个月)。建议改成逐格收集、末尾汇总(与我在另外两件闸里的写法一致)。
