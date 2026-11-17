# S-BROWSER-FLAKE-WINDOWS 交付报告

派单：组长 opus50tc2k.zzc，2026-11-17T20:31:56Z（delivery dw-dlv_7cf3166fd6819b2facf47ea4098acfc1）。
期限 2026-11-19T12:00:00Z。分支 `qwen38mvb4/browser-flake-windows`，基于 r60 集成锚
`9818b979e9e20f0bcfeffa6b78045dda71b1bd79`。只改两个测试文件＋证据，产品零改动。

## 〇 边界与同一性

- 改动件：`view-service/r15-session-history-browser-regression.js`、
  `view-service/console-browser-regression.js`（含既有刀 legacy-group-deadlock 的锚漂移修复，见 §2.4）。
- 产品同一性（sha256）：本分支 `view-service/renderer-canary-sse.html` ＝
  `14ffc39ea1ed61c1d230b7e3f3a810564daf05f61a7ec17cefde07ffc98eb6ef` ＝ 9818b979 原件
  （`git diff 9818b979 -- view-service/renderer-canary-sse.html` 为空）；
  `internal/m8/console_auth.go` ＝ `53a7388bd10f644adb31b569f4ce15ff780b87e34c522a840ee9937207e0b62c`
  ＝ N-2 冻结分支 1a7ac795 的同名件（N-2 从未改 console）。⇒ 两格在本分支上跑的就是
  9818b979 原产品；与 N-2 无关另以 N-2 renderer 对照读数佐证（§1.5）。
- 不改 VERSION、不推 main、不出发布件、不碰预发／现网。

## 一 r15 翻页格（r15-session-history-browser-regression.js:96）

### 1.1 现象

`await page.waitForFunction(() => timelineAtBottom === false, null, { timeout: 3000 })`
超时。N-2 期间读数：新 renderer 单跑五连 0/2/0/0/2，base renderer 单跑五连 0/0/2/0/2，
红点逐字同（既有抖性，组长 20:30:23Z 裁定另立本单）。

### 1.2 定因证据（不靠猜）

取证法：临时副本（不入提交）在页面内挂**只读**钩子——包 `scrollTimelineToBottom`／
`finishTimelineReplay` 记每次补拍时刻与调用栈、timeline scroll 监听记
scrollTop/gap/nearBottom/timelineAtBottom/replayInProgress/距手势毫秒、document wheel
监听记手势；Node 侧在 set300/wheel 前后打时刻标记；line-96 超时时转储全部日志＋截图。
无负载单跑 6 次 **6/6 红**（evidence/r15-red-solo1..6.json、solo-rc.txt；截图 solo1/2.png）。

solo1 时间线（Date.now() 毫秒，省略公共前缀 1794948160）：

| t | 事件 | 读数 |
|---|------|------|
| 738 | 钩子就位；fonts.ready 已 resolved | |
| 878→907 | 测试置 scrollTop=300 | scroll@901：gap=5251、**replay=true**、byUser=false ⇒ 标志不动（产品规则：非用户 scroll 只许置真） |
| 944 | beforeWheel | |
| **947** | **finishTimelineReplay(forceBottom=true)**（回放 quiet 定时器此刻才收尾） | 紧接 fonts.ready 微任务补拍：scrollTopBefore=**300** → 滚回底 5551 |
| 952 | scroll 事件 | scrollTop=5551、gap=0、nearBottom=true ⇒ timelineAtBottom=true |
| 967 / 1197 | 双 rAF 补拍 / 250ms 补拍 | 均在底，no-op |
| ~97x | wheel(+10) 落地 | **底部钳位，无位移**；手势已标记（dump 时 sinceGesture=2984）但 gap=0 ⇒ 释放条件 byUser&&gap>160 永不成立 |
| 396x | 3s 窗超时 | state：atBottom=true、scrollTop=5551=scrollHeight−clientHeight、replay=false |

solo2/3 同形（finishReplay 分别落在 276/550，均在测试已置 300 之后；wheel 事件
280/609 到达时位置已被补拍抢回底部）。六份转储机制一致。

**根因**：测试前置 `pendingRenders.size===0 && c1MergeBuf.size===0` **不蕴含回放收尾**。
`finishTimelineReplay(true)` 及其三层补拍（fonts 微任务／双 rAF／250ms，
renderer-canary-sse.html:1964-1968）仍可在手势前后把位置抢回底部；而程序化
scrollTop=300 不是用户手势、按产品规则（:2034-2055）不能释放跟随标志。随后 +10
微滚轮在底部钳位 ⇒ 不存在 gap>160 的用户 scroll ⇒ 标志恒真 ⇒ 3s 超时。
竞态双方都是产品的合法路径，测试不应依赖谁先谁后。

### 1.3 修法（测试侧，确定性）

1. `page.addInitScript` 设 `window.REPLAY_QUIET_MS=60000`、`window.REPLAY_HARD_LIMIT_MS=60000`。
   这是渲染器**明示的测试钩子**（renderer-canary-sse.html:1555-1556 `window.REPLAY_* || 默认`），
   不改产品文件。60000ms ≫ 本格运行时长（实测 7-15s）⇒ 自然收尾不可能与手势竞态；
   释放手势确定性地走「重放窗内用户上翻」逃生口（scroll handler :2035-2044：
   `finishTimelineReplay(false)` 清定时器、取消 followScrollFrame、**不武装补拍**）。
   自然收尾路径由 renderer-scroll-follow-regression（A/C 腿）与 renderer-sse-reconnect 覆盖，
   本格判据（翻页顺序／锚点／逐出守卫）不依赖回放如何收尾。
2. 前置改等可观察静止：`pendingRenders.size===0 && c1MergeBuf.size===0 &&
   followScrollFrame===null && document.fonts.status==='loaded'`，上界 7000ms（沿用本格
   既有内容等待上界；200 条历史渲染在负载下实测 <2s，7s 为既有裕量）。
3. 新增守卫断言 `replayInProgress===true`：钩子失效即响亮失败，不静默回到竞态路径。
4. wheel 由 +10 改 **−600（大幅上翻真手势）**：即便有补滚先落底，−600 仍产生
   gap=600>160 的离底位移；重放窗内唯一补滚来源 followScrollFrame 是 rAF，而帧序上
   「run the scroll steps」先于 rAF ⇒ 补滚抢不到释放判定之前；释放分支又显式取消
   followScrollFrame ⇒ 无竞态。保留 set300＋mouse.move（原「程序化置位＋真手势」形状）。
5. line-96 条件等待**原样保留**（3000ms 上界：释放是手势后首个 scroll 事件内的同步
   判定，3s ≫ 负载下帧调度余量）。下游全部断言一字未改。

### 1.4 判别力（删保护刀）

`r15-session-history-mutations.js` 三刀在修复后仍红于**原断言点**（4x 负载下实测，
evidence/verify/vr15mut.log）：

| 刀 | rc | 红点 |
|----|----|------|
| drop_history_node_move | 2 | `timeline order invalid:` |
| drop_scroll_anchor_correction | 2 | `scroll anchor shifted` |
| restore_live_eviction_during_paged_history | 2 | `post-live history prefix was evicted:` |

基线（无变异）绿。另：若删本报告 §1.3 的钩子/守卫/大手势任一，红点回到 line-96
超时（取证副本 6/6 红即删保护前的读数）。

### 1.5 负载读数（≥10 全绿）

负载法见 §三。4x 负载（384 busy-spin，nproc=96）＋两回归进程并行：

- 本分支 renderer（＝9818b979 原件）：12/12 绿（verify/vr15w1..12.log）。
- 显式 `RENDERER_OVERRIDE=/tmp/r15-base-renderer.html`（9818b979 原件副本）：10/10 绿
  （verify/vr15b1..10.log）⇒ 与 N-2 无关。
- N-2 renderer（1a7ac795，sha 49df8fe1…）对照：3/3 绿（verify/vr15n1..3.log）⇒ N-2
  钩子对本夹具纯 no-op，修复不依赖 renderer 版本。

## 二 console 零组格（console-browser-regression.js:530 起）

### 2.1 现象

组长全量 runner 一轮红（其 16 核机），单跑三连两边绿。本机复现：384 busy-spin＋
全量 runner 并行下，`--case='lets a human start from zero groups'` 48 次中 2 红
（hload24/38，evidence/console-red-hload24/38.json＋png、console-hload-rc.txt）。
**红不是超时，是断言本身**：

```
Expected values to be strictly equal:
+ actual   'Agent 列表加载中…'
- expected '没有自己的 Agent，无法建组或加入'
```

### 2.2 定因证据

hload24 时间线（省略前缀 1794948883）：

| t | 事件 |
|---|------|
| 390 | emptyPage 加载：refreshPlatform＋refreshAgents＋**refreshGroups()**（console_auth.go:1735，页面加载即发 groups GET；此时 groupMode=false ⇒ 不写状态文案） |
| 447 | 三个响应到达 |
| ~570 | entry.click 落地 → openGroupWorkspace：groupMode=true → refreshGroups('initial')（GET@573）＋mountEnrollPanels |
| 590→620 | initial 续体处理完 → 状态＝空态文案；测试 filter 等待通过 |
| 705 | retryClick → refreshGroups('retry') **同步**写「Agent 列表加载中…」，发 GET@737 |
| 748 | GET 响应到达网络层；`await refreshed`（waitForResponse）即返回 |
| ~750 | 断言读 `#group-status`.textContent → **'Agent 列表加载中…'** ⇒ 红 |
| dump 时 | 续体（r.json()→renderGroups→groupStatus）已执行完 → state.groupStatus＝期望文案、owned=0 |

hload38 同形（retry GET res@431，断言@434 读到加载文案）。

**根因**：`waitForResponse` 只保证**网络层收到响应**，不保证页面 JS 已执行
`await r.json()` 之后的 renderGroups/groupStatus 续体。负载下渲染器主线程繁忙
（同址页面共享 renderer 进程＋CPU 争用），续体滞后于断言读取几十毫秒 ⇒ 读到
refreshGroups('retry') 开头同步写入的加载文案。1500ms 默认超时不是本次红的直接
原因，但它是另一条负载敏感腿：本格各等待都依赖页面 fetch 完成，而产品自身
fetch 超时是 8000ms（console_auth.go:792 consoleFetchTimeoutMs/groupFetchTimeoutMs）；
测试上界比产品超时还短 ⇒ 产品仍在合法等待时测试可先红。

### 2.3 修法（测试侧，断言一字不改）

1. `await refreshed` 之后、原断言之前，改等**可观察条件**：
   `#group-status.textContent !== 'Agent 列表加载中…'`（加载文案消失＝该次
   refreshGroups 的 DOM 更新已落地）。成功／失败路径都会改写该文案；generation
   不合则不改写 ⇒ 超时红。条件**不预设结果值**，随后的 strictEqual 仍逐字断言
   期望文案 ⇒ 写错文案的产品缺陷照样红（不被等待掩盖）。上界
   CONDITION_WAIT_MS=30000ms（harness 既有条件等待上界，≫8000ms 产品超时＋负载裕量）。
2. `emptyPage.setDefaultTimeout(1500)` → `CONDITION_WAIT_MS`：理由同上（测试等待
   上界必须大于产品自身 fetch 超时，否则与产品 abort 竞态）。只影响本格新页；
   相邻 audit 格及其余格的 1500ms 不动（非本单对象）。
3. 不放宽断言、不删用例、不加重试循环。

### 2.4 既有刀锚漂移的发现与修复（判别力）

派单举例的 `--mutate=legacy-group-deadlock` 实测**假绿**（rc=0）：产品早已把重试
控件从 JS `detail.before(retryArea)` 改成静态 markup `<div id=groupRetry>`（列表区常显），
旧锚不存在，而该刀用静默 `html.replace` ⇒ no-op。这是**既有漂移**（非本次改动引入；
该刀无 runner 消费方，SUITES 的 console_browser 不带 --mutate）。

修复：在当前 markup 上复活**同形死锁**——把 groupRetry 从常显列表区挪进零组态隐藏的
groupDetail，并改用 `mutate()`（锚缺失即报错，杜绝再次静默漂移）。刀名、早退语义、
允许清单不变。修复后实测（evidence/console-knife-fixed.log）：

```
FAIL lets a human start from zero groups without a hidden-control deadlock: #retryGroups has no visible hit box
console browser harness: passed=8 failed=1   rc=1
```

红点正是零组格 humanHit 命中测试（隐藏入口控件 ⇒ 死锁）， preceding 八格全绿；
基线（无变异）绿。⇒ 判别力保持。

### 2.5 负载读数（≥10 全绿）

4x 负载（384 busy-spin）＋r15 回归并行：修复后 `--case` 单格 **12/12 绿**
（verify/vconsole1..12.log）。全量 runner 上下文读数见 §三。

## 三 负载法与全量门读数

负载法（人为加负载，写明方式）：`evidence/load.sh <count> <seconds>` 起 count 个
`node -e 'const end=Date.now()+seconds*1000; while(Date.now()<end){}'` busy-spin
（普通优先级、限时自灭）；nproc=96、cpu.max=max。
- 取证与格级验证：count=384（4x 超订）＋两个回归进程并行；复现窗口 load average 峰值 ~508。
- 全量 runner 验证：count=192（2x 超订）＋runner 单进程（避免极端负载把非目标格打红，见 §四）。

读数：
- 浏览器全量 runner（2x 负载，192 busy-spin）：**45 套件全跑，44 绿 1 红**
  （evidence/browser-all-final.log，receipt 摘录 browser-all-final-receipt.txt）。
  唯一红＝renderer_flood_stress（吞吐断言格，见 §四披露）；**目标格与保护格全绿**：
  console_browser=0、r15_session_history_pagination=0、r15_session_history_mutations=0、
  renderer_provenance_icon=0（8cabb351 来源图标格）、renderer_retry_role=0、
  renderer_c2=0、renderer_delivery_status=0（r60④ 重发链）。
- 浏览器全量 runner（静默，门基准条件，同 r60/N-2 先例）：**45 套件全绿，aggregate_rc=0**
  （evidence/browser-all-quiet.log＋browser-all-quiet-receipt.txt）；console_browser=0
  （harness passed=20 failed=0）、r15_pagination=0、r15_mutations=0、flood_stress=0
  （⇒ §四 2x 红确系负载伪影）、provenance_icon=0、retry_role=0。
- run-build-tests.sh：run1 rc=1（codex 1 红、claude 全绿）；run2 rc=1（claude 2 红、
  codex 1 红）——红格全部落在 AdmissionDeadline* 族（见 §四 helper 抖性披露）。
  **run3 rc=0（BUILD_TESTS_OK helper_profiles=2 python=27 shell=2，两 profile 各 1270 全绿）**
  （evidence/build-tests.log＋build-tests.rc；run1/2 存证 build-tests-run1.log、
  build-tests-run2.log）。

## 四 极端负载下非目标格抖性（不在本单范围，未修）

3x 全 suite 并行＋4x+ busy-spin（load ~400-500）的复现实验中，以下**非目标**格也红
（evidence/fullA1..4/fullB1..4/fullC1..4.out，旧未修副本）：
- 'distinguishes platform and Agent load failures from true empty states'（1500ms 超时／strictEqual）
- 'shows group load and network action failures beside their controls'（conditionDeadline ~29.6s 超时／文案竞态）
- 'runs the zero-group human workflow without hidden actions or long tasks'（deepEqual）

形状与零组格同源（网络响应≠DOM 落地、1500ms 上界），但属其它用例，派单明确只改两格 ⇒
不修、不掩盖，如实披露。2x 负载全量 runner 下这些格绿（§三）。建议组长另单统筹。

另：**renderer_flood_stress 在 2x 负载全量 runner 中红**（browser-all-final.log:1396）：
`[flood] X0 灌入真走完…20000 !== 100002`。读数 pushed=100000、push_rate=5994（≥5000 达标）、
但 receive_rate=4611、final_events=20000、page_unresponsive=true、agents_gap_max_ms=17048
⇒ 2x CPU 超订下渲染器排空速率跟不上灌入速率，观察窗内未收齐。该格是**吞吐压力断言**，
负载敏感是其本性；renderer sha 与锚逐字同（14ffc39e…），本单未改 renderer/该格/其夹具 ⇒
非本单回归。静默全量 runner 中该格绿（§三）。不在本单范围，未修。

另：**run-build-tests.sh run1 的 codex profile 一格红**（build-tests-run1.log:13843）：
`test_dispatch_lifecycle.AdmissionDeadlineTest.test_missing_accepted_uses_admission_deadline_and_recovers`
——「恢复后推进下一件」断言少一件。该格用 100ms admission 截止＋跨线程状态序
（tools/sessionhelper/tests/test_dispatch_lifecycle.py:322-359），调度敏感；本单未改
helper 任何件（git diff 仅两浏览器测试件），N-2 期同基两 profile 曾全绿。静默单模块
重跑 8 次采样 **6 绿 2 红**（≈25% 抖率；rerun6/7 红于同族另两格，
evidence/dispatch-lifecycle-rerun1..8.log）；run2 静默全量仍红（claude 2、codex 1，
均同族；gate 用 ThreadPoolExecutor 两 profile 并行，计时线程互扰）。⇒ **既有、本机
调度敏感的 helper 计时格抖性**，非负载独有、非本单回归（本单 diff 仅两 .js＋docs，
helper 可导入件零改动；N-2/r60 期同基全量门曾绿系单次采样）。不在本单范围，未修；
全量门 run3 rc=0（两 profile 各 1270 全绿）；run1/2 红与此 8 次采样一并披露，
建议组长另单统筹该族计时格（本机≈25% 抖率）。

## 五 复核入口

```
git fetch origin qwen38mvb4/browser-flake-windows
git checkout qwen38mvb4/browser-flake-windows   # 基于 9818b979
cd view-service
NODE_PATH=<playwright node_modules> node r15-session-history-browser-regression.js
NODE_PATH=<...> node r15-session-history-mutations.js        # 三刀红于原点
NODE_PATH=<...> node console-browser-regression.js --case='lets a human start from zero groups'
NODE_PATH=<...> node console-browser-regression.js --mutate=legacy-group-deadlock  # 须红
bash ../docs/reviews/browser-flake-windows/evidence/load.sh 384 120   # 负载下重跑上述
```

证据索引：r15-red-solo1..6.json（＋solo1/2.png）＝r15 红点转储；console-red-hload24/38.json
（＋png）＝console 红点转储；solo-rc.txt／console-hload-rc.txt／r15-fix-rc.txt＝取证读数账；
verify/*.log＋verify-rc.txt＝修复后负载读数；console-knife-fixed.log＝刀红读数；
browser-all-final.log＋receipt／build-tests.log＝全量门。

## 六 边界合规

产品零改动（renderer/console_auth sha 同锚）；VERSION 未动；未推 main；未跑 build.sh
出发布件；未碰预发／现网；未改他人分支。两测试件之外只新增 docs/evidence。
