# `0.2.1.7` · U7 那一格交付(截屏"按选区裁剪"从此有断言)

**实施** `qwen38maxkhovvob4`(入组第一件)
**派工** 组长 `opus50ts` 2026-09-20 ts-No.1;裁定见 ts-No.2 · 板锚 `ae3d7da2`
**分支** `qwen38maxkhovvob4/u7-shot-crop-assert`
**锚** `5345bd7c5a1247fb8211fa6d0d9818689012db33`
**测试件 tip** `63fd4cdb`(㉘ 三档 ＋ iframe 路径闸;本件文档提交紧随其后)
**基线** `84eb1f10`(`preprod` == `release/0.2.1.6`,两者同锚)
**改动范围(ts-No.1 那一格)** 🔴 **只碰一个文件**:`view-service/renderer-0206-file-render-regression.js`(+192 行,新增用例 ㉗)。
**该格产品实现零改动。**
**后续范围(ts-No.2 / ts-No.4 裁定后追加,详见第六节)**

    产品件  view-service/renderer-canary-sse.html   仅 #shotOverlay 的 DOM 位置 ＋ 注释(ts-No.2 授权并指定修法)
    测试件  renderer-0206-file-render-regression.js ㉘(三档视口)· ㉙(<4px 回退整张)
    新件    renderer-in-console-iframe-regression.js 真 console iframe 路径的遮罩闸(ts-No.4 ③,组长判为阻断项)

    sha256  测试件    453f3f5149250334140cb98475cb6183ff6d83d3c34efcf0b0c59d450e86e821
            renderer  76c56d954db51039c2f63352136f8981e8f3edb992067a445cbe2bedd072930c
            (与三次变异施加前逐字节相同,已 diff -q 核过)

---

## 〇 为什么有这一格

业主用 `0.2.1.2` 的 📷 发来**整屏未裁**的图。核 ㉕ 的**断言**(不是注释)发现:

    ㉕ 实际断言:上传发生 1 次 · 文件名匹配 · 遮罩关闭 · 取消不上传
    🔴 没有一条断言"裁出来的图是选区尺寸而非整张"

变异 U7(忽略选区、一律传整张)⇒ `rc=0`,**37 通过 / 0 失败,零转红**。
⇒ **不是"真路径上的裁剪没验",是任何路径上的裁剪都没人验。** ㉗ 就是补这一格。

---

## 一 运行方式

```text
# 1) 契约替身(HTTPS,自带证书路径按环境给)
MOCK_CERT=<cert.pem> MOCK_KEY=<key.pem> python3 view-service/renderer-0206-contract-mock.py 19443

# 2) 套件
NODE_PATH=<playwright-node_modules> PW_CHANNEL=chrome \
MOCK_BASE=https://127.0.0.1:19443 SHOTS=/tmp/xxx \
  node view-service/renderer-0206-file-render-regression.js

# 3) 真 console iframe 路径(ts-No.4 ③ 追加;自带 HTTP 装置,【不需要】上面那个契约替身)
NODE_PATH=<playwright-node_modules> PW_CHANNEL=chrome \
  node view-service/renderer-in-console-iframe-regression.js

# 3b) 它的【阳性对照】:喂生产钉住的那份 renderer 字节,必须红
RENDERER_OVERRIDE=<旧 renderer.html,sha256=76c56d954db51039…> \
NODE_PATH=<playwright-node_modules> PW_CHANNEL=chrome \
  node view-service/renderer-in-console-iframe-regression.js
```

**结果**:

    2)  通过 40 / 失败 0    rc=0   (基线 37 ＋ ㉗ ㉘ ㉙)
    3)  通过 2  / 失败 0    rc=0
    3b) 通过 1  / 失败 1    rc=1   唯一红的是 iframe-②,共 11 项违规(原文见第六节)

🔵 **环境前置(踩过的,写下来省别人半天)**:

1. 🔴 **㉖ 自起 `chromium.launch(...)` 且【不吃】`PW_CHANNEL`** ⇒ 它需要 **Playwright 自带的
   chromium**,系统 Chrome 顶不上。本机原缺,首次基线跑成 `36 通过 / 1 失败`,
   红的就是 ㉖,报错 `browserType.launch: Executable doesn't exist at
   ~/.cache/ms-playwright/chromium_headless_shell-1243/...`。
   手工补 `chrome-headless-shell 153.0.8010.12` 后才 38/0。
   **别人机器上 ㉖ 可能仍红 —— 那不是产品问题,也不是 ㉗ 的问题。**
2. `cdn.playwright.dev` 在本网络下 30s 超时(它 307 重定向到 `storage.googleapis.com`);
   `npx playwright install chromium` 会失败。可直连
   `https://storage.googleapis.com/chrome-for-testing-public/<ver>/linux64/chrome-headless-shell-linux64.zip`
   下载后解压到 `~/.cache/ms-playwright/chromium_headless_shell-<rev>/`。
3. 契约替身若用 `nohup ... &` 起在交互式 shell 里,**会随该 shell 退出被杀**,
   表现为 `curl: (7) Failed to connect` / `renderer=000` —— 看着像服务坏了,其实是进程没了。
   用 `setsid` 脱离进程组。

---

## 二 三条判据 → 各自的断言(判据落在 `assert` 上,不落在注释上)

### ① 上传字节的 PNG IHDR 宽高 == 选区宽高

取证:用例内**临时**挂 `page.on('request')` 收 `postDataBuffer()`,`finally` 里 `page.off`
(沿用本套件既有写法,不改共享装置 —— 共享的 `uploadCalls` 只存 URL,存不了字节)。

解析:**逐字节**,不经任何图像库 ——
PNG 签名 8 字节 → 首 chunk 类型必须是 `IHDR` → `buf.readUInt32BE(16)` / `(20)` 取宽高。

**三层对账,缺一不可**:

    独立期望值   由【浏览器实收 pointer 坐标】经 aspect 映射回源坐标算出
                (不假设 Chrome 保留小数:期望值来自实收坐标,不来自我下发的坐标)
    产品选区     读 `shotRect`(脚本作用域 let,与本套件既有读 `stats` 同一手法)
    上传字节     IHDR 宽高

### ② 真实 pointer 事件拖选,不用测试缝 `__dwSetShotRect`

`mouse.move → down → move(steps:12) → up`。**全程未调用 `__dwSetShotRect`。**

    B1  拖之前 `#shotSel` 必须隐藏
    B2  拖之后 `#shotSel` 必须出现
        ⇒ 产品里只有 pointermove 会把它显示出来,这两条合起来证明
          选区确由产品的 pointer 处理器产生,不是被塞进去的
    守卫 C  canvas 显示宽必须 ≠ 源宽。实测 2400 → 1280,**缩放比 1.875**。
        🔴 若哪天缩放比 = 1,`toSource()`(renderer:4556)那段换算等于没跑,
           本条对"换算写错"零判别力 ⇒ **守卫 C 让本条自己失败,不假装验过。**
    端点约束  拖拽两端必须落在用户真能看见的区域(canvas ∩ wrap)内;
        拖到被裁剪掉的地方不算真实拖选。

🔵 帧源仍用 `__dwSetScreenshotFrameSource`,沿用 ㉕/㉖ 既定分工:
**缝 = 画面可控(验裁得对不对)**,**浏览器开关 = 真路径(㉖ 验通不通)**。
裁剪对不对只有可控画面验得了。

### ③ U7 ⇒ 必须转红,且只红这一条

排他句:`IHDR == 2400x1200`(整张)即红,报错直接写出业主撞到的形状。

### 附带 D 尺寸对 ≠ 裁对了地方

帧源画成**四象限异色**(左上红/右上绿/左下蓝/右下黄),选区整块落在右上绿象限内;
解码**上传字节本身**,四角 + 中心采样必须全是 `[0,255,0]`。
🔵 纯色帧验不出"尺寸对但偏移错",四色才验得出 —— 这一维是组长原派工里没有的。

---

## 三 🔴 可证伪证据:三个变异,各打中不同断言层

| 变异 | 位置 | 施加内容 | 命中的断言 | 失败输出(原文) |
|---|---|---|---|---|
| **U7** | `renderer:4594` | `const r = full;`(忽略选区) | A0 排他句 | `🔴 U7:上传的是整张 2400x1200 ⇒ 选区被忽略了(业主"框选了却收到整屏"就是这个形状)` |
| **M2** | `renderer:4557-4558` | `toSource()` 缩放比恒为 1 | A1 IHDR vs 独立期望 | `IHDR 宽应 == 选区宽 600(源 2400 → 显示 1280,缩放比 1.8750),实测 320` |
| **M3** | `renderer:4597` | `drawImage` 起点恒为 `(0,0)` | D 像素采样 | `采样点 tl 应为纯绿 [0,255,0](选区整块在右上绿象限内),实测 [255,0,0] ⇒ 尺寸对但裁错了地方` |

**三次跑的结果完全一致**:`通过 37 / 失败 1`,**唯一红的就是 ㉗**(判据③)。

🔴 **三个变异下 ㉕ 全部仍绿** ⇒ 印证 ㉕ 对裁剪正确性**零判别力**,这一格此前确实无人承担。
🔴 **M2 那条是判据②的价值证明**:坐标换算写错这个 bug,**用测试缝(`__dwSetShotRect`)永远看不见** ——
㉕ 就是这么漏的。这也是"为什么必须真 pointer 事件"的实证,不只是道理。

三个变异均已回滚;回滚后 `diff -q` 与变异前副本逐字节相同,并复跑 `38 / 0`。
**终验跑的是已推送那份(工作树 `git status --porcelain` 为空)。**

---

## 四 🔴 没验到什么(本件最值钱的一节,一字不删)

1. **「真路径 + 框选 + 裁得对」这个组合仍然没有用例覆盖。**
   ㉗ 用帧源缝(画面可控),㉖ 用浏览器开关(只验真路径通不通 ＋ 尺寸非替身)。
   `--use-fake-device-for-media-stream` 给的是**合成画面、内容不可控、尺寸也不由用例指定**
   ⇒ 这是**结构性缺口**,不是漏写。要补需要另想取证手段。
2. **高 DPR 未验。** 全部在 headless + `deviceScaleFactor=1` 下。
   2x 屏上 `getBoundingClientRect` 与 canvas 内在尺寸的关系没验 ——
   **业主真机若是高分屏,本件不背书。**
3. **「选区 <4px 回退整张」那个兜底分支没有独立用例。**
   U7 变异把整条三元式换成 `full`,所以它被一起吞掉了;
   但"小选区应回退整张"**本身**当时仍然零判别力。
   ⇒ 组长 ts-No.2 已裁定**单开一格**,见本件第六节。
4. **未验真实选源框、真实屏幕内容下的裁剪** —— 选源那一步需要真实用户手势,headless 给不了
   (㉕ 的注释里也写明了这条边界)。
5. **只跑了这一个套件。** `console-browser-regression.js` 等其余浏览器套件、Go 侧测试**一个都没跑**。
   🔵 **更新(ts-No.4 之后,原文不删)**:现在**多跑了一个**自立的
   `renderer-in-console-iframe-regression.js`(真 console iframe 路径,见第六节)。
   但 `console-browser-regression.js`(C2)等**其余既有**浏览器套件、Go 侧测试
   **仍然一个都没跑** —— 这一条的实质(合入前没跑全仓回归)**没有变**。
6. 🔵 **环境事实**:㉖ 需要 Playwright 自带 chromium(见第一节前置 1)。
   **别人机器上可能仍红,那不是产品问题。**
7. 🔴 **iframe 那件是【替身支撑】的,不是真后端。** console 页 HTML **确实取自生产源码**
   (`console_auth.go` 的 `consolePageHTML`,5994 字节),`/view/*` 返回的也**确实是仓内
   真实的 renderer 字节**;但 `/api/console/agents`、`/api/console/platform`、
   `status`、`events` 四个依赖是本件自带的 HTTP 替身
   ⇒ **真 console ＋ 真后端(真 SSE 流、真上传、真鉴权跳转)下的遮罩几何未验。**
   替身与真件在"几何"这一层是否等价,**我推论等价,没有实测。**
8. 🔴 **iframe 路径下,遮罩盖不到 console 自己的 nav / tabs。** 实测:遮罩开着时,
   console 顶层 nav 位置的最上层命中元素仍是 `platform`(不是遮罩)。
   iframe 画不到自己外面,这是**架构固有边界**,不是本次修复能解决的。
   本件**只打印、不断言**它(断言等于把缺陷钉成契约)。
   ⇒ **本件不背书"框选时整个浏览器窗口都变暗"这个观感。**
9. 🔵 **窗口 420 宽时,业主能框选的面积只有 180px 宽。** 实测 iframe 视口 `180x859`
   —— console 的 nav 是**固定 240px 列、没有响应式规则**,窄窗口下面板被挤到 180px。
   **这是 console 侧的问题,不是 renderer 侧的,本格没改它、也没替它找补。**
   遮罩在 180px 里仍然铺满(`ovTop=0 / ovH=859 / chain=body`),所以**修法不需要重议**;
   但"窄窗口下截屏基本不可用"这件事**本身可能需要另开一格**,已报组长。
10. 🔴 **iframe 路径下没验真实 `getDisplayMedia`。** 帧源仍走测试缝
   `__dwSetScreenshotFrameSource`(与第 1 条同源:真路径的画面不可控)。
   ⇒ iframe 路径上验的是**遮罩几何**,不是**真截屏内容**。

---

## 五 只读诊断中撞见的产品缺陷(本件未改,已由组长改写为 `0.2.1.7` 截屏项)

🔴 **截屏遮罩根本没有铺满视口,用户只看到 110px 高的一条。**

**成因(实测,非推断)**:`<footer class="composer">` 上有 `backdrop-filter: blur(10px)`;
按 CSS 规范,`backdrop-filter ≠ none` 的元素**成为 fixed 后代的包含块**
⇒ `.shot-overlay { position:fixed; inset:0 }` 只对**页脚那个盒子**解析。

    renderer-canary-sse.html:779   .composer { … }        ← 规则起点
    renderer-canary-sse.html:789     backdrop-filter: blur(10px);
    renderer-canary-sse.html:838   .shot-overlay { position: fixed; inset: 0; … }
    renderer-canary-sse.html:1054  <footer class="composer">
    renderer-canary-sse.html:1096    └─ #shotOverlay       ← 是 footer 的后代

**实测数据(headless Chrome,`PW_CHANNEL=chrome`)**:

    视口 1280x900    #shotOverlay top=740 h=160     ← 不是 (0,0,1280,900)
                     #shotWrap    top=790 h=110,overflow:auto,scrollHeight=640 / clientHeight=110
                     #shotCanvas  显示 1280x640,top=790 ⇒ 底边 1430,**超出视口 530px**
    视口 1280x1600   #shotOverlay top=1440 h=160    ⇒ **与视口高度无关,始终只盖页脚**

**后果**:① 全屏变暗的遮罩效果没有(只盖住页脚);
② 2400x1200 的帧只显示 110px 高的一条,必须滚动才能框选;
③ 业主说「截屏,不是我希望的方式」—— **很可能是直接原因之一,且比"没按选区裁"更早撞上。**

🔵 **对 ㉗ 的影响**:㉗ 已把拖拽端点约束在**真能看见的那一带**内,
所以本用例**不依赖该缺陷被修好**;将来遮罩修成全视口,可见区只会变大,端点仍在内 ⇒ 不会因此变红。
(该推论在修复落地后已实测复核,见第六节。)

**修法裁定(组长 ts-No.2)**:把 overlay 移出 footer、挂到 `<body>` 直属。
🔴 不采纳"去掉 composer 的 `backdrop-filter`"(会改业主看惯的观感);
🔴 不采纳 portal(为一个元素引入机制,不划算)。
🔴 **顺序硬性要求:先写闸,后修 —— 闸必须在动手之前就是红的。**

---

## 六 后续两项(组长 ts-No.2 裁定)

### ③ 遮罩包含块缺陷 —— 已修(严格按"先写闸、跑到红、再动手")

**闸 = 用例 ㉘**,三个维度 × **两档视口高度(900 / 1600)**:

    A  `#shotOverlay` 的 rect == 视口 rect(容差 1px,只吸收亚像素取整)
    B  画布被遮罩**完整包住** ⇒ 用户不必滚动就能看到整帧
    C  视口中心命中测试(`elementsFromPoint`)落在遮罩内 ⇒ 屏幕中央真的被盖住

🔵 只验 A 会漏掉"铺满了但仍裁切画布";只验 A+B 会漏掉"铺满了但被别的层压在下面"。

**修复前的红(真 test-first:阳性对照是活缺陷,不是变异模拟)** —— 输出原文:

```text
rc=1   通过 38 / 失败 1        ← 唯一红的就是 ㉘
· ㉘ 实测: [{"vp":"1280x900","ovTop":667,"ovH":233,"cvBottom":1317,"hit":false,"hitTop":"timeline"},
            {"vp":"1280x1600","ovTop":1367,"ovH":233,"cvBottom":2017,"hit":false,"hitTop":"timeline"}]
🔴 ㉘ … -> 🔴 截屏遮罩没有铺满视口(共 6 项):
   - 1280x900: 遮罩 rect={"left":0,"top":667,"right":1280,"bottom":900,"width":1280,"height":233} ≠ 视口 1280x900(应铺满)
   - 1280x900: 画布 {"left":240,"top":717,"right":1040,"bottom":1317,"width":800,"height":600} 未被遮罩完整包住 ⇒ 用户必须滚动才能框选
   - 1280x900: 视口中心命中的是 "timeline" 而不是遮罩 ⇒ 屏幕中央没变暗、也点不到
   - 1280x1600: 遮罩 rect={"left":0,"top":1367,…,"height":233} ≠ 视口 1280x1600(应铺满)
   - 1280x1600: 画布 {…,"bottom":2017,…} 未被遮罩完整包住 ⇒ 用户必须滚动才能框选
   - 1280x1600: 视口中心命中的是 "timeline" 而不是遮罩 ⇒ 屏幕中央没变暗、也点不到
```

🔵 **㉗ 在修复前是绿的** —— 它把拖拽端点约束在"真能看见的那一带"内,所以不依赖该缺陷被修好。
**这条此前是推论,现已由实测坐实**(修复前那次跑:㉗ 绿、㉘ 红)。

**修法(按裁定)**:把 `#shotOverlay` 整块**移出 `<footer class="composer">`,挂到 `<body>` 直属**,
并在原位置留下注释写明"为什么不能改回去 / 不要去 backdrop-filter / 不要上 portal"。
🔴 **产品改动只有这一处 DOM 位置 ＋ 注释:CSS 一行未改、JS 一行未改。**

**依赖排查(组长要求实测,不只读码)**

静态排查:

    · `.shot-overlay` / `.shot-bar` / `.shot-canvas-wrap` / `.shot-sel` / `.text-btn`
      **全是全局类选择器**(renderer:838-843、:269),没有任何 `.composer` 后代选择器
    · 全仓唯一一处 `closest('.composer')` 在 renderer:4624,是从 `composerInner` **往上**找
      (拖放态 `.composer.dragging` 样式),与遮罩无关
    · `body` 是普通块容器(`margin:0;height:100vh;overflow:hidden`,renderer:44),
      fixed 元素出流 ⇒ 不影响 `.app` 的 grid;也没有 `body > *` / 兄弟选择器
    · 无 `:first-child` / `:last-child` / `:nth-child` 牵涉遮罩

实测(同一探针,修复前/后各跑一次,计算样式全量对比):

    footer.composer  rect  [0,740,1280,160] → [0,740,1280,160]   🔵 **一字未变**(移动遮罩没扰动页脚)
    #shotOverlay     rect  [0,740,1280,160] → [0,0,1280,900]     ✅ 铺满视口
    #shotWrap        高    110px            → 850px               ✅ 整帧可见
    #shotCanvas      rect  [240,790,800,600] → [240,50,800,600]   (尺寸未变,只是不再被裁切)
    祖先链           footer.composer[pos=sticky,bf=blur(10px),zi=6] → **body[pos=static]**

🔴 **唯一一处非尺寸差异已实测归因,不是样式回归**:
`#shotCancel.borderTopColor` 由 `rgb(11,107,203)` 变成 `rgb(216,222,232)`。
探针记录 `:hover` 链证明:修复前 hover 链末端是 `button#shotCancel.text-btn`
(**遮罩的按钮正好压在 📷 上方**,探针点完 📷 后鼠标停在原位),修复后同一点落在 `#shotWrap`
⇒ 差异来自 **hover 态**,`.text-btn` 本身的样式一字未变。
🔵 这顺带暴露一个用户可见细节:修复前**点完 📷,遮罩的「取消」按钮就压在 📷 的位置上**。

实测(全量):`rc=0`,**`通过 40 / 失败 0`**;㉕ / ㉖ / ㉗ 全部仍绿。

```text
· ㉘ 实测: [{"vp":"1280x900","ovTop":0,"ovH":900,"cvBottom":650,"hit":true,"hitTop":"shotCanvas"},
            {"vp":"1280x1600","ovTop":0,"ovH":1600,"cvBottom":650,"hit":true,"hitTop":"shotWrap"}]
```

**附带发现(未修,随本修复自动消失,记录备查)**:修复前 `.composer` 是
`position:sticky; z-index:6` 的**层叠上下文** ⇒ 遮罩的 `z-index:9999` 被关在里面,
**全局只相当于 6**;而 `.toast-region` 是 `z-index:100`(renderer:544)、
`.workspace-dock` / `.taskboard-handle` 是 `z-index:40`(:484 / :202)
⇒ **框选时 toast 与右侧栏浮在遮罩之上**。移出 footer 后 9999 成为全局最高,一并解决。

🔴 **发布须知(归装配/发布,我没碰)**:renderer 的 sha256 由
`76c56d954db51039…` 变为 `f459a2849ccec897…`,而 **`deploy/invariants-dingwei-gray.json:16`
把生产 renderer 的 `want=` 钉在旧哈希上** ⇒ 部署 0.2.1.7 时**必须同步抬这一格**,
否则灰度巡检会报不成立。**这是装配动作,按边界我没改它、也没碰生产。**

### ② `<4px 回退整张` —— 独立一格(用例 ㉙),三条腿各有前置守卫

    L1 不拖          守卫 shotRect === null       ⇒ IHDR == 2400x1200,且四象限红/绿/蓝/黄各就各位
    L2 拖 2 CSS px   守卫 0 < w,h < 4(实测 3.75) ⇒ IHDR == 2400x1200(回退整张,不是碎片),红/黄仍在
    L3 拖到源 10x10  守卫 w,h >= 4                ⇒ IHDR == 10x10 且【不是整张】,中心像素必须是绿

🔴 **L3 是阳性对照**:没有它,"一律传整张"(U7)也能让 L1/L2 全过 ⇒ 本条又成了半条。
🔴 **每条腿的前置守卫**先读产品自己的 `shotRect`,确认这一腿真的走在它声称的分支上 ——
否则"拖大了/拖没了"都不会有人发现,那一腿就是空断言(与本仓"守卫 C"同一思路)。

**变异 M4**(`const r = shotRect ? shotRect : full;`,即删掉 `>=4` 阈值,renderer:4594):

```text
rc=1   通过 39 / 失败 1        ← 唯一红的就是 ㉙
🔴 ㉙ … -> 🔴 L2:选区 3.75x3.75(<4px,按误触处理)必须回退整张,不许裁出碎片
```

### 🔴 红条集合**变了** —— 必须说清,别当成"只红一条"被破坏

新增 ㉙ 之后,把四个变异**全部重跑**了一遍(不采信加 ㉙ 之前的结论):

| 变异 | 位置 | 红条 | 各自命中的断言(输出原文摘句) |
|---|---|---|---|
| **U7** 忽略选区 | `renderer:4594` | **㉗ + ㉙** | ㉗`上传的是整张 2400x1200` · ㉙`L3 阳性对照:选区已过阈值(10.00px)却传了整张` |
| **M2** 缩放比恒为 1 | `renderer:4557-4558` | **㉗ + ㉙** | ㉗`IHDR 宽…实测 320` · ㉙`L3:…中心实测 [255,0,0] ⇒ 尺寸对但位置错` |
| **M3** drawImage 起点恒为 (0,0) | `renderer:4597` | **㉗ + ㉙** | ㉗`采样点 tl…实测 [255,0,0]` · ㉙`L3:…中心实测 [255,0,0]` |
| **M4** 删掉 `>=4` 阈值 | `renderer:4594` | **仅 ㉙** | ㉙`L2:选区 3.75x3.75…必须回退整张` |
| **遮罩缺陷**(真缺陷,非变异) | 修复前的树 | **仅 ㉘** | ㉘ 三维度 × 两档视口,共 6 项 |

每次都是 `通过 38 / 失败 2`(M4 为 `39/1`),**没有任何无关用例被牵连**。

🔴 **对 ts-No.1 判据③「只红这一条」的影响,如实报**:
那条判据成立的前提是**当时只有 ㉗ 一格承载"按选区裁"**。现在 ㉙ 的 L3 阳性对照
**也**依赖"按选区裁",所以 U7/M2/M3 各红两条 —— **这是预期且正确的**(同一行为有两格覆盖),
不是 invariant 被破坏。M4 与遮罩缺陷仍然**各自只红一条**。

🔵 **M2 为什么也红 ㉙(我原先判断错了,更正)**:我原以为 M2 下 ㉙ 会绿,
因为 L3 的 IHDR 是拿产品自己的 `shotRect` 对账的(缩放比错了两边一起错 ⇒ 尺寸仍自洽)。
**实测红了**,原因是 M2 让**落点**也跑偏:选区从源 x=1800(绿象限)漂到 x=960(**红象限**),
于是 L3 的**内容采样**转红。⇒ **内容断言比尺寸断言覆盖更宽**,这是意外收获,也说明
"读码推出来的红条集合"必须重跑核实,不能直接写进交付件。

🔵 **留给组长裁定的一个取舍**:若希望 ㉙ **只**管阈值(红条归属更干净),
可以把 L3 的内容采样去掉、只留尺寸;代价是 M3 那类"裁错位置"就只剩 ㉗ 一格覆盖。
**我的建议是保留**(牙更多),红条归属用上面这张表说清即可。

🟢 **已裁定(ts-No.4 ①)**:**保留** L3 的内容采样。判据③ 由组长**重述**为
"变异后转红的**每一条都必须能解释为什么它该红**;解释不了的红条 = 真问题",
🔴 **这是判据被反例修正,不是判据向本件让步** —— 归属就用上面那张表说清。

### 🔵 ts-No.4 ③④ 追加:在【业主真走的那条路】上补验 ＋ 窄窗口档

**③ 组长判为阻断项的理由(我原先把它当备查,判错了)**:
`position:fixed` 在 iframe 内相对的是 **iframe 自己的视口**,不是浏览器窗口
⇒ "遮罩铺满视口"在**独立 renderer 页**与**console iframe** 两条路上根本不是同一件事。
🔴 而业主实际走的就是 console 那条(`internal/m8/console_auth.go:306` 建 iframe,
`src='/view/'+session`),**独立页他从不走** ⇒ 只在独立页上验收 =
**在用户不走的那条路上验收**。推论等价 ≠ 实测等价,这一格就是把那条推论换成实测。

**新件** `view-service/renderer-in-console-iframe-regression.js`(装置 ＋ 两条用例)。
装置沿用 C2 的手法,但有两处关键区别:

    · console 页【从生产源码里取】(console_auth.go 的 consolePageHTML,5994 字节),不复制一份
    · /view/* 返回【仓内真实的 renderer-canary-sse.html】,不是占位页 —— 否则验的不是业主那份

🔵 **踩坑写进文件头注释了**:iframe 内 renderer 的 `status` / `events` 是**相对路径**,
会被解析成 `/view/<session>/status`;只挂 `/status` 会 404 ⇒ `stats.readonly` 恒为 true
⇒ 📷 点不动,而失败信息看起来像"页面没加载好"(**又一例"错误消息指错方向"**)。

**两条用例**:

    iframe-① 对账   被服务的 renderer 字节 == 仓内那一份 · iframe 铺满 #panes ·
                    stats.readonly === false · sessionName 对 · 有 📷 入口
    iframe-② 闸     判据与 ㉘ 同形,但【参照物换成 iframe 视口】,三档窗口 × 四维度:
                      A 遮罩 rect == iframe 视口 rect(容差 1px)
                      B 遮罩父链 == body(包含块没被谁改回去)
                      C 画布被遮罩完整包住
                      D iframe 视口中心 elementsFromPoint 命中遮罩

**实测(修复后的树)** `通过 2 / 失败 0`,`rc=0`。三档窗口量到的 iframe 视口与遮罩:

| 浏览器窗口 | iframe 视口 | 遮罩 ovTop / ovH | 父链 | 中心命中 |
|---|---|---|---|---|
| 1280x900 | **1040x859** | 0 / 859 | body | ✅ 遮罩内 |
| 1280x1600 | **1040x1559** | 0 / 1559 | body | ✅ 遮罩内 |
| 420x900 | **180x859** | 0 / 859 | body | ✅ 遮罩内 |

🔵 **第二列正是③ 要证的那件事**:窗口 1280 宽,面板只有 1040 宽 ⇒
"铺满视口"在这条路上**必须**以 iframe 视口为参照物,拿浏览器窗口当参照物会误判。

**🔴 阳性对照:喂的不是变异,是【生产钉住的那份字节】**

    RENDERER_OVERRIDE=<renderer-prefix.html>
    sha256=76c56d954db51039…  ==  deploy/invariants-dingwei-gray.json:16 的 want=
    ⇒ 通过 1 / 失败 1,rc=1,唯一红的是 iframe-②,共 11 项违规(420 档少一项"画布越界")

```text
🔴 iframe-② … -> 🔴 真 console 的 iframe 内,遮罩没有铺满 iframe 视口(共 11 项):
   - 1280x900(iframe 视口 1040x859): 遮罩 rect={"left":0,"top":699,…,"height":160} ≠ iframe 视口 1040x859
   - 1280x900(iframe 视口 1040x859): 遮罩父链是 footer<div<body 而不是 body ⇒ 包含块又被谁改了
   - 1280x900(iframe 视口 1040x859): 画布 {…,"top":749,…,"bottom":1349,…} 未被遮罩完整包住 ⇒ 面板内仍需滚动才能框选
   - 1280x900(iframe 视口 1040x859): iframe 视口中心命中的是 "timeline" 而不是遮罩
   - 1280x1600(iframe 视口 1040x1559): 遮罩 rect={"left":0,"top":1399,…,"height":160} ≠ iframe 视口 1040x1559
   - 1280x1600(iframe 视口 1040x1559): 遮罩父链是 footer<div<body 而不是 body ⇒ 包含块又被谁改了
   - 1280x1600(iframe 视口 1040x1559): 画布 {…,"top":1449,…,"bottom":2049,…} 未被遮罩完整包住 ⇒ 面板内仍需滚动才能框选
   - 1280x1600(iframe 视口 1040x1559): iframe 视口中心命中的是 "timeline" 而不是遮罩
   - 420x900(iframe 视口 180x859): 遮罩 rect={"left":0,"top":611,"right":220.11,…,"width":220.11,"height":248} ≠ iframe 视口 180x859
   - 420x900(iframe 视口 180x859): 遮罩父链是 footer<div<body 而不是 body ⇒ 包含块又被谁改了
   - 420x900(iframe 视口 180x859): iframe 视口中心命中的是 "timeline" 而不是遮罩
```

🔵 **420 那一档多出来的信息(修复前)**:遮罩 `width=220.11` 而 iframe 视口只有 `180` 宽
⇒ **窄窗口下遮罩还横向溢出面板**。这条只在窄档看得见,④ 补这一档是对的。

**④ 独立页那一侧同步补窄窗口档** —— ㉘ 由两档扩为三档(新增 `420x900`),判据三维度不变。
实测 `通过 40 / 失败 0`,`rc=0`:

```text
· ㉘ 实测: [{"vp":"1280x900","ovTop":0,"ovH":900,"cvBottom":650,"hit":true,"hitTop":"shotCanvas"},
            {"vp":"1280x1600","ovTop":0,"ovH":1600,"cvBottom":650,"hit":true,"hitTop":"shotWrap"},
            {"vp":"420x900","ovTop":0,"ovH":900,"cvBottom":371.28,"hit":true,"hitTop":"shotWrap"}]
```

**🔴 两件如实报、且【故意不断言】的(详见第四节 8 / 9 条)**

1. **遮罩开着时,console 自己的 nav / tabs 仍是最上层命中元素**(实测 `platform`)。
   iframe 盖不到自己外面 —— 架构固有边界。**只打印不断言**:断言等于把缺陷钉成契约,
   将来真要修就会红。⇒ 本件**不背书**"框选时整个浏览器窗口都变暗"这个观感。
2. **窗口 420 宽时面板只有 180px**(console nav 固定 240px 列、无响应式规则)。
   **这是 console 侧的问题,不在本格范围,我没改它、也不替它找补。**
   🔵 但它对业主是真场景 ⇒ "窄窗口下截屏基本不可用"**本身可能需另开一格**,已报组长。

🟢 **对组长那句"若 iframe 视口本身就很小,修法可能要再议"的回答**:**修法不需要重议。**
判据是"遮罩铺满它拿到的那个视口",三档(859 / 1559 / 180x859)实测**都铺满且父链都是 body**
⇒ 缺陷与修法都在 renderer 侧、且已被这两件钉住;180px 那件事的成因在 **console 的栅格**,
**换一个修法也不会让它变宽。**

---

## 七 🔵 附:U7 附近另一例「错误消息指错方向」(**读码得出,未运行时验证**)

    renderer-canary-sse.html:4537-4539
      注释写:「用户自己在选源框点『取消』也走这里,**文案要能区分得开**」
      实现是:handleIncomingEvent({ kind:'error', text: `截屏未完成：${err.message}` })

⇒ 用户取消时 Chrome 给的是 `NotAllowedError` ⇒ 用户看到「截屏未完成：Permission denied」,
会去查**权限**,而真相是**他自己点了取消**。与 `Transport closed` 同族:
**把「用户主动取消」报成「权限坏了」。**

🔴 **这条是【读码】得出的,没有在运行时验证 Chrome 的确切文案**
(headless 给不了真实的"取消"手势)。同族于 U7 那次「注释承诺 ≠ 断言」。
**登记,不展开。**
