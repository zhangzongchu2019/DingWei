# EasyMDE 断网实跑装置(ts-No.77 · `0.2.1.50` I 项选型前置)

**这是什么** 证明 **EasyMDE 2.21.0** 在"内网侧"能用的一套**只验证装置**:载入 + 真键盘编辑 + 保存 +
工具栏全点全程 **外部请求 = 0**,以及它有没有"悄悄往浏览器写正文"的默认。

**这不是什么** 🔴 **不是产品件、不是回归闸、不进 SUITES**(组长 ts-No.77 裁定 (a) 逐字:
「装置准予进仓 `runbooks/harness/easymde-offline/`;🔴 FA 第三方字节不入仓;**装置不进 SUITES**」)。
它也不判渲染保真度、不判编辑手感、不判暗色可读性(暗色是另一格 `dark-contrast-probe.js`,ts-No.78)。

**边界** 🔴 **只验不引入**:本目录里**没有一个第三方字节**(见 `.gitignore`);仓内 `package.json` 仍为 0;
不碰 `release/*`;不改产品件。

---

## 跑法

    cd runbooks/harness/easymde-offline
    node prepare-vendor.js                                   # 取字节到 vendor/ 并逐字校验 pin(幂等:命中就不联网)
    NODE_PATH=/home/is3/pwrun/node_modules node run.js       # S0–S7  主判据      rc 0/2/3
    NODE_PATH=/home/is3/pwrun/node_modules node fa-probe.js  # F1–F3  Font Awesome A/B(修法②的代价)
    NODE_PATH=/home/is3/pwrun/node_modules node cp-probe.js  # 工具栏图标 ::before 码位(打印 JSON)
    NODE_PATH=/home/is3/pwrun/node_modules node dark-contrast-probe.js   # ts-No.78 暗色对比度(独立页形状)
    NODE_PATH=/home/is3/pwrun/node_modules node console-iframe-dark-probe.js  # ts-No.78 追加腿:真 console 的 iframe 内

内网/离线镜像:`DW_NPM_MIRROR=https://你的镜像/ node prepare-vendor.js`。

## 🔴 rc 语义(**读日志先看这一行**)

    0   全过
    2   有格红(红的格会打印理由)
    3   **中止** —— 隔离自证 / 阴格不过 / 被测字节缺失或哈希不符
        ⇒ 🔴 **后面的"绿"一条都没产出**。这是**故意的**:见下面「硬条件⑥」。

## 硬条件⑥:阴格不过 ⇒ 后面的绿一条都不该产出

组长 ts-No.77 裁定(引入硬条件第 ⑥ 条):**装置进仓前,`S1` 须改成与 `S0` 同形的硬中止。**

原结构里 `t()` 会 catch 每格异常、计入 fail 后**继续跑** ⇒ 阴格 `S1` 红的时候,`S2`–`S7` 仍会各自打印
"外部请求 0" —— 那些 0 **技术上正确、语义上是假绿**(探针没有分辨力时,"0 个外部请求"什么都证明不了)。
🔵 治它**不能靠"读日志的人记得先看 rc"** —— **靠记得 = 没有**。所以做成硬中止:

    S0(隔离自证)不过  ⇒ exit(3),一条结论都不产出      (原本就有)
    S1(外联阴格)不过  ⇒ exit(3),S2–S7 不跑             (硬条件⑥,本次加)
    S6(存储阴格)不过  ⇒ exit(3),S7 不跑                (同一理由:S6 是 S7 的阴格,本次加)

**这道中止自己也要被证明会响** —— 见下面「装置自检」。

## 装置自检(**只能让它变红,不能让它变绿**)

    DW_SELFTEST_FAIL=S0 node run.js     # ⇒ 必须 rc=3,且日志里**看不到** S1..S7
    DW_SELFTEST_FAIL=S1 node run.js     # ⇒ 必须 rc=3,且日志里**看不到** S2..S7
    DW_SELFTEST_FAIL=S6 node run.js     # ⇒ 必须 rc=3,且日志里**看不到** S7

钩子只会**抛错**,不会**放过任何断言** ⇒ 它不可能被用来造绿。

## 文件

| 文件 | 干什么 |
|---|---|
| `prepare-vendor.js` | 从 npm/内网镜像取 `easymde@2.21.0` 与 `font-awesome@4.7.0`,**逐字校验 pin** 后落到 `vendor/`;FA css 是**派生件**(改写 6 处 `../fonts/` → `/vendor/fonts/`,改写前先断言"上游哈希命中"与"恰好 6 处") |
| `vendor-pins.js` | 🔴 **pin 的唯一源**(三个探针共用,防第二事实源) |
| `measure-core.js` | 🔴 **量测核的唯一源**:门槛与**维度归属**(裁定 (1))、`buildRows`(行集合)、`MUT_DARK110`(阴格配方)、页内量测函数、像素尺调用 —— 两个对比度探针共用,免得两条路径量成两件事 |
| `run.js` + `page.html` | S0–S7 主判据 |
| `fa-probe.js` + `fa-page.html` | F1–F3:关掉 Font Awesome 自动下载的**代价**(图标会不会消失),以及本地自托管在断网下成不成 |
| `cp-probe.js` | 工具栏图标 `::before` 的**码位**逐个实测(把"推断"换成"实测"用的那把尺) |
| `dark-contrast-probe.js` | ts-No.78:`data-theme` 两种取值下 正文/工具栏/边框 的**对比度实测**(独立页 + `data-theme` 挂 `<body>` 的形状) |
| `console-iframe-dark-probe.js` | ts-No.78 **追加腿**(裁定 (3)):同一把尺子搬进**真 console 的 iframe**(`console_auth.go` 的 `consolePageHTML` + 仓内真 renderer 字节),主题走**业主真实路径**(真点击顶层 ◐) |
| `px.py` | 像素尺:读 PNG,回 `{bg, darkest, lightest, contrast_max}`(半透明/合成之后**真渲染出来**的颜色) |

## `dark-contrast-probe.js`(ts-No.78)的 rc 与九条腿

    0 全过 · 2 有格红 · 3 装置自证不过(D0:产品 CSS 抽不到/形状守卫/隔离不成立 ⇒ 中止)

    D0 装置自证 · D1 浅色 · D2 暗色 · D3 阴格A(刻度块 1.077/1.100 必须判不合格、16.54 必须合格)
    D4 阴格B(真靶:注入 ~1.10 正文配色 ⇒ 必须判不合格) · D5 本仓元素级选择器有没有级联进编辑器 DOM(三腿定因)
    D6 定因(强制 `color-scheme:light` ⇒ 图标色翻不翻) · D7 候选修法实测(**未落产品**) · D8 风险显影(bare `pre` 提特异性)

🔴 **产品 CSS 不复制、不硬编码**:运行时从 `view-service/renderer-canary-sse.html` 抽 `<style>` 并做**形状守卫**;
   抽不到 / 变量集形状变了 ⇒ **rc=3**,不拿旧数字报。🔵 所以**产品件一改,这一格会自己说话**。

🔴 **D8 的装置自审(记在这里,免得下一个人重踩)**:第一版 D8 调了一个 **`MUTS` 里根本不存在的键**(`precascade`)
   ⇒ 注入为空,量的是**未变异**的页面;而它那道"提特异性之后仍合格就是反常"的守卫**把它抓住了**(红在守卫上)。
   第二版另加一条**归因守卫**:变异必须真的把 `pre.CodeMirror-line` 的颜色变成 `--code-text`,否则一律判**装置问题**
   —— 🔵 本组规矩:**"没变"既不许被读成"没风险",也不许被读成"有牙"**。

## `console-iframe-dark-probe.js`(ts-No.78 追加腿 · 裁定 (3))的 rc 与六条腿

    0 全过 · 2 有格红 · 3 装置自证不过(C0)⇒ 中止,不产出任何对比度结论

    C0 装置自证(console 页从 `console_auth.go` 抽 + renderer sha 对账 + 编辑器真在 iframe 内 + 隔离成立)
    C1 主题走**业主真实路径**:真点击 console 顶层 ◐ ⇒ 外层 `html[data-theme=dark]` 且 iframe 内 `body` 跟着暗
    C2 iframe 内(暗色)实测 · C3 独立页(暗色)实测 · C4 🔴 两条路径**逐项对照** · C5 阴格(iframe 形状下尺子有没有牙)

🔴 **两件"不许"已经写成断言,不靠人记得**:
   · **不许手设 `data-theme`** —— C1 会 grep 本文件,一旦发现对 `data-theme` 的**写**就判红(装置自证);
     主题落地只认产品自己写的 `localStorage["dw-theme"]`(renderer `THEME_KEY`)。
   · **不许把 C0 自证故意发的那几条外部请求算到被测件头上** —— 划账时**打印条数**,不静默清。
🔵 **两处装置自审(第一跑红,红的是我的件)**:① C2/C5 复用同一文档 ⇒ bold 已是 `.active`,再点一次是**取消**
   ⇒ 量不到激活态(守卫抓住了);现已先复位再量。② 上一腿真点击后鼠标还停在 bold 上 ⇒ "常态图标"读到的是 **hover 态**
   (C5 常态 1.026 vs C2/C3 的 18.608);现已在取样前把鼠标移到中性位置。

## 🔴 pin 与来源(逐字)

| 字节 | sha256 | 来源 |
|---|---|---|
| `vendor/easymde.min.js` | `2c06bddfd0c89176db08ccf9d42e2beaa9b4f1a4ff8fb2ec0bf1bed25ce08e05` | npm `easymde@2.21.0`(MIT)tarball 内 `package/dist/` |
| `vendor/easymde.min.css` | `6eee36340432776d682e7372ec4a7eb29be4fdb3ffada32c0bfa5e31a5ef34a2` | 同上 |
| `vendor/fonts/fontawesome-webfont.woff2` | `2adefcbc041e7d18fcf2d417879dc5a09997aa64d675b7a3c4b6ce33da13f3fe` | npm `font-awesome@4.7.0`(registry 许可证字段 `(OFL-1.1 AND MIT)`) |
| `vendor/fa.min.css` | `413eb1dc4d9e53a480fb226219f093854d21ab3214c7da778e62759719ef95ee` | **派生件**:上游 css(`799aeb25cc0373fd…`)改写 6 处字体路径 |

🔵 **硬条件⑤(不得抄本次从外网取回的那份)已满足**:交付当时我从 `maxcdn.bootstrapcdn.com` 取的
css/woff2,与 npm 上 `font-awesome@4.7.0` 的对应文件**逐字同哈希** ⇒ 现在**供应路径换成 npm**,字节没变。
🔴 **但真引入时这件事要重做一遍**:走内网镜像、按 §十-3 补 `invariants` 哈希 pin;
且 **FA 的许可证是 `(OFL-1.1 AND MIT)`(字体走 OFL)** ⇒ 🔵 **法务口径我不替他们下结论**。

## 🔴 隔离手段的诚实交代(结论必须带着这段一起读)

    手段     chromium `--proxy-server=http://127.0.0.1:9`(死端口;实测该端口 Connection refused)
    不是什么 🔴 **不是域名白名单** —— 规则里没有任何域名 ⇒ 未知域/拼错域/将来新加的域**一律同样出不去**
    为什么本地夹具还活着   Chromium 对 loopback 有**隐式 proxy bypass**
    是什么层 🔴 **浏览器层,不是 OS 层**。本席实测:`CapEff: 0000000000000000` · `unshare -U/-n/-Un` 全 EPERM ·
             `iptables -L -n` → Permission denied(uid=1006)⇒ 建不了 netns、也没有 iptables/nft 权限
    覆盖不到 🔴 **非浏览器通道**(本装置被测对象只有一个 UMD 前端库,没有这类通道;换组件时该条须重问)
    不靠手段自证   **每跑先做 S0**:本地必须 200、`cdn.jsdelivr.net` / `maxcdn.bootstrapcdn.com` / `example.com`
             **必须全 BLOCKED**;不过 ⇒ `rc=3` 中止,一条 "0" 都不产出
    两个独立观测面  CDP `Network.requestWillBeSent` + Playwright **context 级** `request`/`requestfailed`;
             不一致就**如实打印两个数**(S1 就是 CDP 3 条 / PW 6 条记录 —— 同一请求的 request+requestfailed 各一条)

## 没做什么 / 没验什么

🔴 **完整清单在交付件 §九(九条),不在这里复述**,免得两处漂移:
`runbooks/DELIVERY-EASYMDE-OFFLINE-0.2.1.50-qwen38maxkhovvob4.md`。
一句话版:它只量"**出不出网 / 落不落盘 / 图标有没有字形 / 对比度是多少**",
**不量**渲染保真度、编辑手感、大文档性能、图片上传、Vditor/Cherry。

## 与交付件的字节关系(🔴 别把两份当成同一份)

交付件 §四 那 **8/0 rc=0** 出自 `/tmp` 里**未加硬中止**的那份 `run.js`(sha256 `ea1384ee3d81448f…`)。
本仓这份**按硬条件⑥ 加了 S1/S6 硬中止 + 字节 pin 前置校验** ⇒ **哈希不同**。
🔵 **判据与断言一字未动**(加的只是"什么时候不许继续跑"与"字节不对就不许跑");
加完之后**重跑仍 8/0 rc=0**(记录见交付件 §十三「装置进仓后的复跑」)。
