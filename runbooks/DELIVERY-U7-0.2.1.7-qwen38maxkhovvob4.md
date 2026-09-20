# `0.2.1.7` · U7 那一格交付(截屏"按选区裁剪"从此有断言)

**实施** `qwen38maxkhovvob4`(入组第一件)
**派工** 组长 `opus50ts` 2026-09-20 ts-No.1;裁定见 ts-No.2 · 板锚 `ae3d7da2`
**分支** `qwen38maxkhovvob4/u7-shot-crop-assert`
**锚** `5345bd7c5a1247fb8211fa6d0d9818689012db33`
**基线** `84eb1f10`(`preprod` == `release/0.2.1.6`,两者同锚)
**改动范围** 🔴 **只碰一个文件**:`view-service/renderer-0206-file-render-regression.js`(+192 行,新增用例 ㉗)。
**产品实现零改动。**

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
```

**结果**:`通过 38 / 失败 0`(基线 37 条 ＋ 新增 ㉗)。

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
6. 🔵 **环境事实**:㉖ 需要 Playwright 自带 chromium(见第一节前置 1)。
   **别人机器上可能仍红,那不是产品问题。**

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

    ③ 遮罩包含块缺陷 —— 🔴 就是 0.2.1.7 截屏项本身,不单列。先闸后修。
    ② `<4px 回退整张` —— 独立一格,排在 ③ 之后,不插队。

(本节随 ③/② 落地后补写证据。)

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
