# 交付件 — ts-No.77 · **EasyMDE 断网实跑**(选型定案的前置)

**派工** 组长 ts-No.77(板件锚见 `runbooks/ACTIVE-ASSIGNMENTS.md`)
**交付方** `qwen38maxkhovvob4` · 2026-09-21
**边界(照单执行)** 🔴 **只验不引入**:未写进任何 `package.json`、未进仓、未碰发布线、未改产品。
仓内 `package.json` 数量**仍为 0**(交付时实测 `find … -name package.json | wc -l` = **0**);
被测字节与装置**全在 `/tmp/md-easymde-offline/`**,本件把装置源码**逐字内联**(§十一),因为 `/tmp` 会被清掉。

**同批** 更正了调研件 `runbooks/RESEARCH-MARKDOWN-EDITOR-0.2.1.50-qwen38maxkhovvob4.md` 里**被本件推翻的那一条**
(EasyMDE「关掉拼写检查后无其它外网依赖」)—— 原文划掉保留,受影响 8 处就地更正并标「ts-No.77 更正」。

---

## 〇 一句话

**EasyMDE 2.21.0 能离线用,但"离线"是 3 处配置换来的,不是 1 处;而且第 2 处关掉之后必须自己补一份 Font Awesome,否则工具栏图标直接消失。**

    主判据   offline 配置下 载入 + 真键盘编辑 + 保存 + 工具栏全点 ⇒ **外部请求 0**(CDP 与 Playwright 两个观测面一致)
    阴格     默认配置 ⇒ 探针抓到 **3 条**外部 URL;不隔离对照下这 3 条**真的能出去**(还多一条 `.woff2`)
    存储     `autosave` 是 **opt-in** ⇒ 不开时 localStorage/sessionStorage/IndexedDB/SW **全空**(无 Vditor 那种默认写草稿)
    实跑     S0–S7 **8/0 rc=0** · 附带 F1–F3 **3/0 rc=0**
    🔴 口径  「**这套配置下外部请求为 0**」,**不是**「EasyMDE 永不出网」——默认配置**会**出网

---

## 一 判据逐条对照(组长原话 → 我做到了什么 → **在哪一层**)

| 组长原话(逐字) | 我做到 | 层 |
|---|---|---|
| 🔴 **真断外网**(不是改 host、不是拦域名白名单 —— 要能证明"任何外部域都出不去) | 死端口代理 `--proxy-server=http://127.0.0.1:9`:**任何**非 loopback 请求都被送进一个必然 connect 失败的本地套接字(不是域名表)。S0 每跑先自证 3 个外部域全 BLOCKED,自证不过 **rc=3 中止全跑** | 🔴 **浏览器层**,**不是 OS 层**(§二 有实测证据) |
| 载入 ＋ 编辑 ＋ 保存 全程:**外部请求数 = 0** | S2:外部 **0**(CDP 面 0 · Playwright 面 0),本地 4 条;正文从键盘进去、从"保存"按钮出来**一字不差**(61 字) | 实测 |
| 🔴 阴格 **必须先证探针有牙**:默认拼写检查(会去 jsdelivr)**必须被探针抓到** | S1:CDP 抓到 **3 条**、PW 抓到 **6 条记录**(= 3 个请求 × `request`+`requestfailed` 各一条),去重后 **3 个 URL**(词典 `.aff`/`.dic` + Font Awesome) | 实测 |
| 抓不到 ⇒ 先修探针再报结论 | S1 红 ⇒ 后续 S2/S3 的 "0" 一个都不产出的结构(`assert` 在 S1 内,S1 红则该格计入 fail、rc=2) | 装置 |
| 另验 `cache.enable` 类"悄悄写 localStorage":**EasyMDE 也要查有没有同类默认** | S6(存储阴格:开 `autosave` **必须**看到写盘)+ S7(不开 ⇒ **四类存储全空**) | 实测 |
| 🔴 仍是**只验不引入** | 仓内 `package.json` **0**;装置与被测字节只在 `/tmp`;`release/*` 未碰;产品件未动 | 实测 |

---

## 二 🔴 隔离手段的诚实交代(**先读这一节再读结论**)

**判据要的是"真断外网"。我做不到 OS 级,做到的是浏览器级。这条必须跟着结论一起走。**

### 2.1 我做不到的(OS 级隔离)—— 实测证据,不是推测

    $ grep CapEff /proc/self/status   →  CapEff: 0000000000000000
    $ unshare -U  true                →  unshare failed: Operation not permitted
    $ unshare -n  true                →  unshare failed: Operation not permitted
    $ unshare -Un true                →  unshare failed: Operation not permitted
    $ iptables -L -n                  →  Could not fetch rule set generation id: Permission denied (you must be root)
    $ id                              →  uid=1006(is3) gid=1006(is3)      (iptables/nft 二进制**在**,但无权限)

⇒ **建不了 netns,也没有 iptables/nft 权限** ⇒ 本席**无法**在这台机器上做"整机断网"。

### 2.2 我做到的(浏览器层)

    手段   chromium --proxy-server=http://127.0.0.1:9      (:9 = discard 端口)
    实测   $ python3 → connect("127.0.0.1", 9)  →  ConnectionRefusedError [Errno 111] Connection refused
           ⇒ 那个端口**确实无人监听**,任何被代理进去的请求必然失败
    为什么本地夹具还活着   Chromium 对 loopback 有**隐式 proxy bypass** ⇒ `127.0.0.1:<随机端口>` 的夹具照常可用
    为什么它**不是**域名白名单   代理规则里**没有任何域名**;分类是事后按 URL 前缀做的(见 `classify()`),
                              而"能不能出去"由代理决定 ⇒ 未知域、拼错域、新加的域**一律同样出不去**

### 2.3 🔴 结论口径(不许升级)

    成立   「**浏览器发出的任何非 loopback 请求,在这次实跑里都出不去**」
    不成立 「这台机器断网了」/「EasyMDE 在任何环境下都不出网」
    覆盖不到的通道   非浏览器通道(本件被测对象只有一个 UMD 前端库,**没有**这类通道;
                    但若将来引入带 node 侧/构建期的组件,**这套隔离一概不管**)

### 2.4 不靠手段自证 —— 每跑都做 S0,不过就中止

S0 是**独立一格**:同一个隔离浏览器里,先 `fetch` 本地 `/ping`(**必须 200**),再 `fetch` 三个外部域
(`cdn.jsdelivr.net` / `maxcdn.bootstrapcdn.com` / `example.com`,**必须全 BLOCKED**)。

    实测   {"local":"OK 200",
            "jsdelivr":"BLOCKED TypeError: Failed to fetch",
            "bootstrapcdn":"BLOCKED TypeError: Failed to fetch",
            "example":"BLOCKED TypeError: Failed to fetch"}

🔴 **S0 不过 ⇒ `process.exit(3)`,一条 "外部请求 0" 都不产出。**
(理由:`--proxy-server` 若被某个开关吃掉,后面所有 0 都会变成**假绿**;
 而"隔离把本地也断了"同样是装置坏 —— 两种都中止,不猜。)

### 2.5 两个**互相独立**的观测面

| 面 | 来源 | 抓什么 |
|---|---|---|
| CDP | `Network.requestWillBeSent`(浏览器网络栈层) | 请求**发出即计**,无论成败 |
| Playwright | **context 级** `request` / `requestfailed` | 请求 + 失败原因;context 级 ⇒ **popup 的请求也收得到** |

🔴 两面**不一致就如实打印两个数**(S1 就是 3 vs 6 条记录 —— 不是矛盾,是 PW 对同一请求记了 `request` 与 `requestfailed` 两条)。
判据用 CDP 面(它不会漏"发出但立刻失败"的请求),PW 面作交叉核对。

---

## 三 被测件与来源链(provenance)

| 件 | 版本 | sha256(全量) | 来源 |
|---|---|---|---|
| `easymde.min.js` | 2.21.0 | `2c06bddfd0c89176db08ccf9d42e2beaa9b4f1a4ff8fb2ec0bf1bed25ce08e05` | npm tarball `easymde-2.21.0.tgz` 内 `package/dist/easymde.min.js` |
| `easymde.min.css` | 2.21.0 | `6eee36340432776d682e7372ec4a7eb29be4fdb3ffada32c0bfa5e31a5ef34a2` | 同上 `package/dist/easymde.min.css` |
| `fa.min.css`(**附带实测用**) | Font Awesome **4.7.0** | `413eb1dc4d9e53a480fb226219f093854d21ab3214c7da778e62759719ef95ee` | 🔴 **我从 `maxcdn.bootstrapcdn.com/…/latest/css/` 取回**,并把 6 处 `../fonts/` 改写为 `/vendor/fonts/`(改写后 `url(...)` 里**无外部域**) |
| `fontawesome-webfont.woff2` | 4.7.0 | `2adefcbc041e7d18fcf2d417879dc5a09997aa64d675b7a3c4b6ce33da13f3fe` | 同上域 |

🔵 **来源链核对**:夹具 `vendor/` 里那两份 EasyMDE 字节的 sha256,与 ts-No.75 下载的 npm tarball 内 `dist/` 字节
**逐字一致**(同一对哈希)⇒ 本件量的**就是** npm 上发布的那份,不是我改过的副本。
`package.json` 实测:`easymde 2.21.0 · MIT · dependencies 5`(`@types/codemirror`、`@types/marked`、`codemirror`、`codemirror-spell-checker`、`marked`,均已打包进 dist)。

🔴 **FA 那两份的来源不可抄**:它们是**外网取回来再本地服务**的字节,只为做 A/B。
真引入时 Font Awesome 必须走 npm 包/内网镜像,**并补一条哈希 pin**(调研件 §二-4:`invariants-dingwei-gray.json:16` 同族)。

### 装置字节的时序自证(证明"S0–S7 的数字出自现在这份源码")

    run.js        mtime 2026-09-20T14:20:56   sha256 ea1384ee3d81448f…   22243 B
    page.html     mtime 2026-09-20T14:16:14   sha256 de91a56d79131ed8…    2759 B
    easymde-run2.log(产出 S0–S7 的那次实跑)   mtime 2026-09-20T14:21:25
    fa-probe.js   mtime 2026-09-20T14:37:02   sha256 f78a3462aa96d65c…   12448 B
    fa-page.html  mtime 2026-09-20T14:32:53   sha256 04ec679b1e12f235…    2834 B
    fa-probe2.log(产出 F1–F3 的那次实跑)      mtime 2026-09-20T14:37:14

⇒ 两个装置的 mtime 都**早于**各自日志的完成时间 ⇒ 本件引用的数字与内联源码**是同一份字节**。
🔵 `fa-probe.js` 与 `run.js`/`page.html` **互不影响**:附带实测是独立两个文件,主判据那 8 格的字节没被动过。

---

## 四 S0–S7 逐格实测

| 格 | 判据(要它红才叫有牙) | 🔬 实测 | 这格**看不见**什么 |
|---|---|---|---|
| **S0** 隔离自证 | 本地必须 200,3 个外部域**必须全 BLOCKED**;不过 ⇒ **rc=3 中止全跑** | `local=OK 200`;jsdelivr / bootstrapcdn / example **全 `BLOCKED TypeError: Failed to fetch`** | 只证"这一次跑隔离成立";它不证 OS 层(§二) |
| **S1** 🔴 阴格(探针有牙) | 默认配置**必须**看到外联;且**必须**含 jsdelivr 与 bootstrapcdn 各至少一条 | CDP **3** 条 / PW **6** 条记录;去重 3 个 URL:`maxcdn…/font-awesome.min.css` · `cdn.jsdelivr.net/…/en_US.aff` · `…/en_US.dic` | 只证"默认配置会被抓到";不证"所有可能的外联都会被抓到"(见 §九-①) |
| **S2** 🔴 主判据 | offline 配置:载入+真键盘编辑+保存 ⇒ 外部 **0**;且正文一字不差;且本地请求 ≥3(否则页面根本没起来) | **CDP 外部 0 · PW 外部 0 · 本地 4 条**;`typed == saved == 61 字`;无 dialog、无 pageerror | 只走这一条流程;图片上传、预览渲染、大文档未走 |
| **S3** 🔴 工具栏全点 | 除 `guide` 外**全点一遍**(≥8 颗才算验到工具栏)⇒ 外部仍 **0**;且正文**必须以打进去的开头且更长**(证明按钮真插了东西,不是空转) | 点到 **12** 颗:`Bold/Italic/Heading/Quote/Generic List/Numbered List/Check List/Create Link/Insert Image/Toggle Preview/Toggle Side by Side/Toggle Fullscreen`;外部 **0/0**;正文 **61 → 102 字** | 🔴 这里**不能**断言 `saved == typed`(按钮会合法改写正文)——第一跑我就是这么写错的(§八-1) |
| **S4** 🔵 显影 guide 按钮 | 主证据 = `window.open` 桩记录的目标**必须**是 `markdownguide.org`;若将来被改指内网 ⇒ **本格会红**(那是好事,报组长转"已内化"再撤) | `window.__dwOpened = ["https://www.markdownguide.org/basic-syntax/"]`;网络面 CDP 0 / PW(ctx 级)0 / popup 0 | 🔴 **只在被点击时**出网 ⇒ 不影响 S2/S3 的 0;但内网侧点它 = **打不开一个外网站点** |
| **S5** 🔵 不隔离对照 | 去掉代理、默认配置 ⇒ 那些外联**必须真的成功过**(否则我证明不了"是隔离挡住的") | 外部 **4** 条,**成功 4** 条(3 条同 S1 + `maxcdn…/fontawesome-webfont.woff2`) | 对照只证"请求是真请求";不证这些域的内容对不对 |
| **S6** 🔴 存储阴格 | 显式开 `autosave` ⇒ **必须**看到 localStorage 被写(否则 S7 的"空"不可信) | `localStorage={"smde_dw-offline-probe":61}` · `sessionStorage={}` · `idb=[]` | key 前缀 `smde_` 是它自己的命名;只量了这一种配置 |
| **S7** 🔴 存储主判据 | offline 配置、**不开** autosave ⇒ localStorage/sessionStorage/IndexedDB/serviceWorker **四类全空**,且外部 **0** | `localStorage={} sessionStorage={} indexedDB=[] serviceWorker=0`,外部 **0** | 🔴 **不等于"永远不会写"**:一旦有人打开 `autosave`,浏览器里就有**第二份 spec 正文**(§六) |

**实跑汇总**:`通过 8 / 失败 0`,`rc=0`(rc 由外层 shell 记进 `/tmp/easymde-run2.log`,不是推断)。

---

## 五 🔴 三个默认外网面(推翻调研件一条包级结论)

调研件原话:「**必须 `spellChecker:false`** 或自托管词典;**关掉后无其它外网依赖**」⇒ 🔴 **后半句错**。

| # | 外网面 | 触发时机 | bundle 原文(🔬 逐字) | 实测 URL | 关掉它要写什么 |
|---|---|---|---|---|---|
| ① | 拼写词典 | 启用拼写检查后 | `n.open("GET","https://cdn.jsdelivr.net/codemirror.spell-checker/latest/en_US.aff",!0)` 与同形 `.dic` 一处 | `…/en_US.aff` · `…/en_US.dic` | `spellChecker:false`(或自托管词典) |
| ② | **Font Awesome** | 🔴 **载入即发**(与用户是否编辑无关) | `var t=!0; … if(!1===e.autoDownloadFontAwesome&&(t=!1))` ⇒ **只有显式 `false` 才不下载**;随后 `r.href="https://maxcdn.bootstrapcdn.com/font-awesome/latest/css/font-awesome.min.css"` 插进 `<head>` | `maxcdn…/font-awesome.min.css`(+ 不隔离时还有 `fontawesome-webfont.woff2`) | `autoDownloadFontAwesome:false` **＋ 自己补一份 FA**(§七) |
| ③ | 工具栏 `guide` 按钮 | 🔴 **点击才发** | `guide:{name:"guide",action:"https://www.markdownguide.org/basic-syntax/",className:te.guide,noDisable:!0,title:"Markdown Guide",default:!0}` | `window.open("https://www.markdownguide.org/basic-syntax/")` | 从 `toolbar` 数组里去掉 `guide`,或改指内网文档 |

**为什么 ts-No.75 的包级 grep 漏了 ② ③**(这条比结论本身有用):

    ② 我当时的 CDN 串正则是 `unpkg|jsdelivr|cdnjs|cdn\.` 一族 —— **`maxcdn` 不在里面**
    ③ 那个 URL 是**普通字符串常量**,不含 `cdn` 字样,也不在任何 `XMLHttpRequest`/`fetch` 附近
       ⇒ 它是 `window.open` 的目标,而我只 grep 了网络 API

🔵 **教训(与"包级证据 ≠ 运行时证据"同源)**:**grep 用的关键字,决定了你能看见什么。**
⇒ 调研件里 **Vditor / Cherry 那两格的 ✅/⚠️ 仍是包级证据,同类漏报风险原样存在**,不可当作已验。

🔵 **② 还有一个坑**(bundle 原文里读出来的,📖 未单独实跑):它在注入前先扫
`document.styleSheets[i].href.indexOf("//maxcdn.bootstrapcdn.com/font-awesome/")>-1`,**有**才跳过
⇒ **自托管 FA 若不用这个域,它照样会去下载**(除非同时显式给 `autoDownloadFontAwesome:false`)。

---

## 六 存储副作用:EasyMDE vs Vditor(组长点名要查的那条)

| | Vditor 4.0.0 | EasyMDE 2.21.0 |
|---|---|---|
| 默认是否往浏览器写正文 | 🔴 **是**(`cache.enable` 默认 `true`:types 注释"默认值: true" + bundle `cache:{enable:!0`,ts-No.75 包级实测) | ✅ **否** —— `autosave` 是 **opt-in**;🧪 不开时 **localStorage/sessionStorage/IndexedDB/serviceWorker 四类全空**(S7) |
| 打开后写什么 | 草稿(与"正文唯一源"冲突) | `smde_<uniqueId>`(🧪 S6 实测 `smde_dw-offline-probe`,61 字 = 我打进去的正文) |
| 对 I 项「正文唯一源」 | 🔴 **默认就相撞**,必须显式关 | 🔵 **默认不撞**;🔴 但**一旦有人打开 `autosave`,浏览器里就有第二份 spec 正文** ⇒ 引入时须**显式禁止**(§十-5) |

🔴 **口径**:这是"**默认配置下**没有 Vditor 那种悄悄写盘",**不是**"EasyMDE 不会写盘"。

---

## 七 附带实测 F1–F3:修法② 的**代价**(我自加的一格,不属组长判据)

**为什么要加**:若只报"`autoDownloadFontAwesome:false` 就干净了",那是**只报好处不报代价** ——
那颗开关关掉的是**图标字体的来源**,而 EasyMDE 的工具栏图标正是 `<i class="fa fa-bold">` 这种 FA 字形。

**装置**:`fa-probe.js` + `fa-page.html`(独立两文件,不影响 S0–S7)。两腿**唯一差别**是有没有从**本地**挂一份 FA 4.7.0。
**两腿都在同一个断网隔离下跑** ⇒ 顺带证明了"自托管 FA 在断网下可行"。

| 腿 | `document.fonts` 枚举 | `button.bold > i` 实测 | 工具栏截图墨迹 | 外部请求 |
|---|---|---|---|---|
| **F1** 不自托管 | `faces=[]` | `class="fa fa-bold"` · computed `font-family: Arial`(回退) · `::before content = none` · **rect 0×0** | 1264×52,**非背景 1446 px**,不同颜色 **18**(PNG 只有 **557 B**) | **0** |
| **F2** 本地自托管 | `faces=["FontAwesome/loaded"]` | computed `font-family: FontAwesome` · `::before content = "\uf032"` · **rect 10×13** | 1264×49,**非背景 3012 px**,不同颜色 **534**(PNG **3799 B**) | **0**(CDP 与 PW 两面) |
| **F3** A/B 分辨力 | — | 两腿工具栏**钮数相同**(守卫) | **27.8 → 61.5 每行墨迹 = 2.2×**(🔵 两腿高度不同 ⇒ 按高度归一化后再比,不拿裸像素数糊弄) | — |

**结论(实测)**:

    🔴 关掉 `autoDownloadFontAwesome` 而**不**自托管 FA ⇒ 工具栏图标**没有字形**
       (图标元素 0×0、`::before` 无内容、墨迹只剩边框/分隔线)——**业主会直接看见一排空白按钮**
    ✅ 从**本地**挂一份 FA 4.7.0 ⇒ 字形回来(10×13),且**断网下外部请求仍为 0**
    ⇒ 修法② 的完整形态是「**关开关 ＋ 自托管 FA**」,或「**关开关 ＋ 改写 EasyMDE 的图标 CSS 用本仓图标**」
       🔵 后者与本仓主题全局化(`data-dw-theme`)更合拍,但那是**工作量**,不是**配置**

### 🔴 自我更正:`document.fonts.check()` 不能当判据(第一跑 F1 假红)

    我原先的判据   assert document.fonts.check('14px FontAwesome') === false
    实测           faces=[] (确实没有 FA) 而 **check 返回 true** ⇒ F1 红
    真相           Chrome 对**没有任何 @font-face 匹配**的 family,`check()` 也返回 `true`(它答的是"能不能用这个
                   font-family 渲染这段文本",而未知 family 会走系统回退 ⇒ 视为可用)
    ⇒ 红的是**我的判据**,不是产品,也不是"字体真的在"

🔵 **判据已换成三个互不依赖的量**:`document.fonts` **枚举** · `::before` 的 `content` · 元素**几何**(0×0)。
三者一致才下结论;像素墨迹作第四道交叉核对。
🔴 **可复用的一句话**:**`document.fonts.check()` 证明不了"字体在"** —— 它证明的是"这段文本能渲染"。

**实跑汇总**:`通过 3 / 失败 0`,`rc=0`(`/tmp/fa-probe2.log`)。

---

## 八 三次"探针第一次红" —— **全是我的件,没有一条是产品缺陷**

| # | 红的那条 | 我原来的写法 | 真相 | 改法 |
|---|---|---|---|---|
| 1 | **S3** | `assert saved === TYPED` | 工具栏按钮(Bold/Link/Image…)**本来就会往正文里插语法** ⇒ 保存值**应该**变长 | 改为 `saved.startsWith(TYPED) && saved.length > TYPED.length`,并**加守卫**:若正文没变长 ⇒ 判"按钮空转,这一格是空的" |
| 2 | **S4** | 用 **per-page** 监听统计 popup 的外部请求 | popup 的**首个导航请求竞态跑在监听挂上之前** ⇒ 报"0 条外部请求",而 popup 明明开了 ⇒ **假红**(而且假红长得像"guide 不出网",会被人当成好消息) | 改 **context 级**监听;并把**主证据**换成 `window.open` 桩(它量"这颗按钮要把用户送去哪",**不依赖网络时序**,断网/有网同样成立) |
| 3 | **F1** | `document.fonts.check()` 当判据 | 见 §七:该 API 对未知 family 也返回 `true` | 换成 fonts 枚举 + `::before content` + 元素几何 |

🔵 三例同一形状:**探针第一次红,先怀疑探针。**
其中第 2 例值得单独记:它属于「**假故障长得和真故障一模一样**」的**反面** ——
**假绿也会长得和真绿一模一样**(若我当时只信"0 条外部请求",就会把"guide 按钮不出网"当成结论写进交付件)。

---

## 九 🔴 没验到什么(照实登记,不许被"8/0"盖过去)

1. 🔴 **隔离是浏览器层,不是 OS 层**(§二 有 EPERM 实测)。它挡的是"浏览器发出的非 loopback 请求";
   **非浏览器通道一概不管**(本件被测对象没有这类通道,但换一家组件就不一定)。
2. 🔴 **"外部请求 0"的分辨力上限 = 我的分类函数**:`classify()` 把 `127.0.0.1:<夹具端口>`/`localhost:<同端口>` 记 local、
   `data|blob|about|filesystem:` 记 inline、**其余记 external**。
   ⇒ 若某请求走**另一个本地端口**(例如组件自带 dev server)它会被记成 external(保守方向,不会漏报成 0);
   但若某请求**根本不经浏览器网络栈**(如 WebRTC/`navigator.sendBeacon` 之外的奇异通道),CDP 面**可能**看不到。
   🔵 我做的对冲:S7 里 `serviceWorker` 注册数**必须为 0**(实测 0)⇒ 排除了"SW 在背后代发"这一类。
3. 🔴 **只验了 EasyMDE 2.21.0 这一份字节**(哈希见 §三)。**Vditor / Cherry 一律未跑**(按裁定不并行)。
   ⇒ 调研件里它们那两格的离线 ✅/⚠️ **仍是包级证据**,而且 §五 已证明包级 grep 会漏。
4. 🔴 **只走了这一条流程**:载入 + 键盘输入 + 保存 + 工具栏全点 + guide。
   **未验**:`imageUploadFunction`(图片拖拽/粘贴/选择器)、自定义 `previewRender`、`side-by-side`/`fullscreen` 的**渲染结果**、
   大文档性能、与本仓 CSS/主题(**暗色**)的冲突、`toolbar` 自定义后的形状。
5. 🔴 **没验"渲染得对不对"**:S2/S3 只断言**正文字节**从键盘到保存一字不差,
   **没有**断言预览面板的 HTML(渲染保真度是调研件 §八-2 那条,仍开着;三渲染器逐字比对**按裁定挂起**)。
6. 🔴 **Font Awesome 那两份字节是我从外网取的**(§三),只用于 A/B;
   **没验** npm 上的 `font-awesome` 包与 bootstrapcdn `latest` 是否逐字一致 ⇒ 引入时**必须重新取源并补哈希 pin**。
7. 🔵 **`spellChecker:false` 之后拼写检查功能就没了**(不是"离线也能拼写检查")。
   若业主要拼写检查,得**自托管 `en_US.aff/.dic`** 并把它指给 `codemirror-spell-checker` —— **那条路我没验**。
8. 🔵 **没做 PoC 集成**:所有"要改几处"仍是调研件 §六 的**读码清单**(本次只把 E 行从 1 处更正为 3 处,依据是 §五 实测)。
9. 🔵 **单进程单次跑**:S0–S7 一遍过、F1–F3 一遍过;**没做多次重跑**看抖动
   (计时相关的量只有 `waitForTimeout`,不涉及被测行为的时间判定 ⇒ 我判它不需要抖动对照,**但这是我的判断,不是实测**)。

---

## 十 引入时的验收标准(供裁 —— **不是我现在要做的**,组长明令"只验不引入")

1. 🔴 **三处配置缺一不可**:`spellChecker:false` · `autoDownloadFontAwesome:false` · `toolbar` 去掉/改写 `guide`。
2. 🔴 **补一份 Font Awesome**(或改写图标 CSS)—— §七 实测:不补 ⇒ 工具栏**一排空白按钮**,业主直接看见。
3. 🔴 **新增静态资产路由必须补哈希 pin**(`invariants-dingwei-gray.json:16` 同族):`easymde.min.js`/`easymde.min.css`/FA 的 css+字体。
4. 🔴 **新增静态路由必须有阴格**:非白名单路径仍 404(调研件 §二-2:`view_service.py:556-566` 只认 5 条路径)。
5. 🔴 **显式禁止 `autosave`**(或把它接到服务端草稿,不能落浏览器)—— 否则与 I 项「正文唯一源」相撞(§六)。
6. 🔵 **本件的 S1/S2/S3/S7 可直接改造成引入后的回归闸**:阴格(默认配置必须抓到外联)+ 主判据(产品配置下外部 0)+ 存储空。
   🔴 但那时**隔离手段要换成 OS 级**(在 prodops/CI 机器上做,本席无权限),否则它挡不住非浏览器通道。
7. 🔵 **`guide` 那颗按钮若改指内网文档**,§七 的 F/S4 会**自动转红** —— 那是好事,报组长把它转成"已内化"再撤钉。

---

## 十一 装置源码(逐字内联 —— 因为它们只存在于 `/tmp`,按边界**不进仓**)

**跑法**(需要 Playwright:`NODE_PATH=/home/is3/pwrun/node_modules`):

    mkdir -p /tmp/md-easymde-offline/vendor/fonts && cd /tmp/md-easymde-offline
    # 把下面四个文件按名写回;被测字节取 npm easymde@2.21.0 的 dist/ 两份(哈希见 §三)
    NODE_PATH=/home/is3/pwrun/node_modules node run.js        # S0–S7,rc 0/2/3
    NODE_PATH=/home/is3/pwrun/node_modules node fa-probe.js   # F1–F3,rc 0/2/3

**rc 约定**:`0` 全过 · `2` 有格红 · `3` 环境/隔离自证不过(**中止,不产出任何"0 外部请求"的结论**)。

### 11.1 `run.js`(S0–S7 主判据装置)

````javascript
// 🔴 ts-No.77 · EasyMDE 断网实跑(组长出单)—— 只验不引入:本目录在 /tmp,**不在仓内**
//
// 判据(组长逐字):真断外网(不是改 host、不是拦域名白名单 —— 要能证明"任何外部域都出不去");
//                  载入 ＋ 编辑 ＋ 保存 全程:**外部请求数 = 0**。
// 阴格(组长逐字):**必须先证探针有牙** —— 故意留着默认拼写检查(会去 jsdelivr)⇒ 必须被探针抓到;
//                  抓不到 ⇒ "0 个外部请求"没有分辨力,**先修探针再报结论**。
//
// 🔴 隔离手段的诚实交代(这条要跟着结论一起报):
//   本席**没有** OS 级隔离能力 —— 实测 `CapEff=0`、`unshare -U` / `-n` / `-Un` 全部 EPERM、
//   `unshare(CLONE_NEWUSER|CLONE_NEWNET)` 亦 EPERM ⇒ 建不了 netns,也没有 iptables/nft 权限。
//   ⇒ 退而用**死端口代理**:`--proxy-server=http://127.0.0.1:9`(discard 端口,无人监听)。
//      它不是域名白名单:**任何**非 loopback 的请求都被送进一个必然 connect 失败的本地套接字;
//      Chromium 对 loopback 有隐式 bypass,所以本地夹具照常可用。
//   ⇒ 而且**不靠它自证**:S0 每一跑都先做隔离自证(本地必须通、三个外部域必须不通),
//      自证不过 ⇒ **rc=3 中止全跑**,绝不产出"看起来是 0"的绿。
//   🔵 计数用**两个互相独立的观测面**:CDP `Network.requestWillBeSent`(浏览器网络栈层)
//      与 Playwright 的 request/requestfailed 事件;两者不一致就报,不取其一。
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { chromium } = require('playwright');

const HERE = __dirname;
const PROXY = 'http://127.0.0.1:9';
const TYPED = '断网实跑 #标题\n\n**粗体** 与 `code` 与 [链接](https://intranet.example/a)\n';
const WAIT = 20000;

const saved = [];
function startServer() {
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    if (u === '/ping') { res.writeHead(200, { 'Content-Type': 'text/plain' }); return res.end('pong'); }
    if (u === '/save' && req.method === 'POST') {
      let b = ''; req.on('data', (c) => { b += c; });
      return req.on('end', () => { saved.push(b); res.writeHead(200, { 'Content-Type': 'text/plain' }); res.end(String(b.length)); });
    }
    if (u === '/' || u === '/index.html') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(fs.readFileSync(path.join(HERE, 'page.html')));
    }
    if (u.startsWith('/vendor/')) {
      const f = path.join(HERE, 'vendor', path.basename(u));
      if (!fs.existsSync(f)) { res.writeHead(404); return res.end('nf'); }
      res.writeHead(200, { 'Content-Type': f.endsWith('.css') ? 'text/css' : 'text/javascript', 'Cache-Control': 'no-store' });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); res.end('nf');
  });
  return new Promise((r) => server.listen(0, '127.0.0.1', () => r({ server, port: server.address().port })));
}

const classify = (url, port) => {
  if (url.startsWith(`http://127.0.0.1:${port}`) || url.startsWith(`http://localhost:${port}`)) return 'local';
  if (/^(data|blob|about|filesystem):/.test(url)) return 'inline';
  return 'external';
};

async function runScenario(browser, sc, port) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' });
  const page = await ctx.newPage();
  const rec = { id: sc.id, cdp: [], pw: [], dialogs: [], pageErrors: [], popups: 0 };
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('Network.enable');
  cdp.on('Network.requestWillBeSent', (e) => rec.cdp.push({ url: e.request.url, cls: classify(e.request.url, port), type: e.type }));
  // 🔵 context 级监听:popup 的请求也一定收得到(per-page 监听会**竞态漏掉** popup 的首个导航请求 ——
  //    第一跑 S4 就是这么假的红:popup 明明开了 2 个,却报"0 条外部请求")
  ctx.on('request', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'ctx', rt: r.resourceType() }));
  ctx.on('requestfailed', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'ctx', rt: r.resourceType(), failed: (r.failure() || {}).errorText }));
  page.on('dialog', async (d) => { rec.dialogs.push(d.type() + ':' + d.message().slice(0, 50)); try { await d.dismiss(); } catch {} });
  page.on('pageerror', (e) => rec.pageErrors.push(String(e.message).slice(0, 120)));
  ctx.on('page', () => { rec.popups += 1; });   // 只计一次(第一跑我在这儿和 guide 分支里各加了一次 ⇒ 重复计数)

  await page.goto(`http://127.0.0.1:${port}/?cfg=${sc.cfg}`, { waitUntil: 'load', timeout: WAIT });
  await page.waitForFunction('window.__dwReady === true', null, { timeout: WAIT });

  if (sc.kind === 'selftest') {
    rec.probe = await page.evaluate('window.__dwProbe()');
  } else {
    // 载入 → 编辑(真键盘)→ 保存
    if (sc.kind !== 'guide') {
      await page.click('.CodeMirror', { timeout: WAIT });
      await page.keyboard.type(TYPED, { delay: 4 });
      rec.typed = await page.evaluate('window.__dwEditor.value()');
      if (sc.clickToolbar) {
        const btns = await page.$$('.editor-toolbar button');
        rec.clicked = [];
        for (const b of btns) {
          const title = (await b.getAttribute('title')) || '';
          if (/Markdown Guide/i.test(title)) continue;   // guide 单独一格量,不混进"载入+编辑+保存"
          try { await b.click({ timeout: 2000 }); rec.clicked.push(title.split('(')[0].trim() || '(untitled)'); } catch {}
          await page.waitForTimeout(60);
        }
      }
      if (sc.save) {
        saved.length = 0;
        await page.click('#dwSave');
        await page.waitForFunction('document.getElementById("dwOut").textContent.startsWith("saved:")', null, { timeout: WAIT });
        rec.saved = saved.slice();
      }
    }
    if (sc.kind === 'guide') {
      const btns = await page.$$('.editor-toolbar button');
      let hit = null;
      for (const b of btns) { if (/Markdown Guide/i.test((await b.getAttribute('title')) || '')) { hit = b; break; } }
      assert.ok(hit, '装置:默认工具栏里找不到 "Markdown Guide" 按钮 ⇒ 这个版本形状变了,别当"没有外联"');
      // 🔵 主证据走 **window.open 桩**:它量的是"这颗按钮要把用户送去哪",不依赖 popup 的网络时序,
      //    所以在断网/有网两种情况下都同样成立(第一跑只靠网络面,popup 请求竞态漏掉 ⇒ 假红)。
      await page.evaluate(`window.__dwOpened = [];
        const __o = window.open;
        window.open = function (u) { window.__dwOpened.push(String(u)); return null; };`);
      await hit.click();
      await page.waitForTimeout(800);
      rec.opened = await page.evaluate('window.__dwOpened');
      await page.waitForTimeout(1200);
    }
    await page.waitForTimeout(2500);   // 给懒加载(词典等)留时间
    rec.storage = await page.evaluate('window.__dwStorage()');
  }
  await ctx.close().catch(() => {});
  return rec;
}

const ext = (rec, key) => rec[key].filter((r) => r.cls === 'external');
const uniq = (arr) => Array.from(new Set(arr.map((r) => r.url.split('?')[0])));

(async () => {
  console.log('EasyMDE 断网实跑(ts-No.77 · 组长出单)');
  console.log(`  隔离手段  ${PROXY}(死端口代理;🔴 非 OS 级 —— 本席无 netns/iptables 权限,实测 EPERM)`);
  const vend = ['easymde.min.js', 'easymde.min.css'].map((f) => {
    const h = require('crypto').createHash('sha256').update(fs.readFileSync(path.join(HERE, 'vendor', f))).digest('hex');
    return `${f} sha256=${h.slice(0, 16)}…`;
  });
  console.log(`  被测件    EasyMDE 2.21.0 · ${vend.join(' · ')}`);
  console.log('  🔴 边界:只验不引入 —— 本目录在 /tmp,仓内 package.json 仍为 0 个,产品件未动');

  const { server, port } = await startServer();
  let browser;
  try {
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  } catch (e) { console.error('环境不可用:Playwright 起不来 ' + e.message.split('\n')[0]); process.exit(3); }

  const results = [];
  let pass = 0, fail = 0; const failures = [];
  const t = async (title, fn) => {
    process.stdout.write(`· ${title} … `);
    try { await fn(); pass += 1; console.log('✅'); }
    catch (e) { fail += 1; failures.push(title.split(':')[0]); console.log(`\n  🔴 ${String(e.message).split('\n').slice(0, 10).join('\n  ')}`); }
  };

  const isoArgs = ['--no-sandbox', `--proxy-server=${PROXY}`];
  const launchIso = async () => chromium.launch({ headless: true, args: isoArgs });

  try {
    // ── S0 隔离自证:不过就中止全跑 ─────────────────────────────────────────────
    let isoBrowser;
    await t('S0 🔴 隔离自证:本地必须通、三个外部域必须【全都不通】(不通才算真断)', async () => {
      isoBrowser = await launchIso();
      const r = await runScenario(isoBrowser, { id: 'S0', cfg: 'default', kind: 'selftest' }, port);
      results.push(r);
      console.log(`\n     探针结果:${JSON.stringify(r.probe)}`);
      if (!/^OK 200$/.test(r.probe.local)) throw new Error(`隔离把本地也断了(local=${r.probe.local})⇒ 夹具不可用,这不是"断外网",是"全断"`);
      const leaked = ['jsdelivr', 'bootstrapcdn', 'example'].filter((k) => !/^BLOCKED/.test(r.probe[k]));
      assert.deepStrictEqual(leaked, [],
        `🔴 隔离**不成立**:这些外部域居然通了 ⇒ ${leaked.map((k) => `${k}=${r.probe[k]}`).join(' | ')}`
        + '\n     ⇒ 中止全跑:在隔离不成立的前提下报"外部请求 0"是假绿。');
      console.log('     🔵 隔离成立:外部一律 BLOCKED,错误名见上(它说明是**哪一层**挡的 —— 报结论时要一起报)');
    });
    if (fail) { console.log('\n🔴 隔离自证没过 ⇒ 按判据中止,不产出任何"0 外部请求"的结论'); await server.close(); process.exit(3); }

    // ── S1 阴格:默认配置(留着拼写检查)必须被探针抓到 ─────────────────────────
    await t('S1 🔴 阴格(探针有牙):默认配置 ⇒ **必须**观测到外联(jsdelivr 词典 + bootstrapcdn 字体)', async () => {
      const r = await runScenario(isoBrowser, { id: 'S1', cfg: 'default', kind: 'teeth' }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp'), pwE = ext(r, 'pw');
      console.log(`\n     CDP 观测外部 ${cdpE.length} 条:${JSON.stringify(uniq(cdpE))}`);
      console.log(`     PW  观测外部 ${pwE.length} 条:${JSON.stringify(uniq(pwE))}`);
      assert.ok(cdpE.length > 0 && pwE.length > 0,
        `🔴 探针没有分辨力:默认配置下两个观测面都是 0(CDP=${cdpE.length} PW=${pwE.length})`
        + '\n     ⇒ 按判据**先修探针再报结论**,后面那些"0"一个都不能信。');
      const urls = uniq(cdpE).join(' ');
      assert.ok(/jsdelivr/.test(urls), `🔴 没抓到已知的 jsdelivr 词典请求 ⇒ 探针漏报。实测:${urls}`);
      assert.ok(/bootstrapcdn|font-awesome/.test(urls), `🔴 没抓到 bootstrapcdn 的 Font Awesome ⇒ 探针漏报。实测:${urls}`);
      console.log('     🔵 探针有牙:默认配置下**两类**外联都被抓到(词典 + 字体),后面 S2/S3 的 0 才有意义');
    });

    // ── S2 主判据:载入 + 编辑 + 保存,外部请求 = 0 ─────────────────────────────
    await t('S2 🔴 主判据:offline 配置(spellChecker:false + autoDownloadFontAwesome:false)载入+编辑+保存 ⇒ 外部请求 **0**', async () => {
      const r = await runScenario(isoBrowser, { id: 'S2', cfg: 'offline', kind: 'flow', save: true }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp'), pwE = ext(r, 'pw');
      console.log(`\n     CDP 外部 ${cdpE.length} · PW 外部 ${pwE.length} · 本地 CDP ${r.cdp.filter((x) => x.cls === 'local').length} 条`);
      assert.deepStrictEqual(uniq(cdpE), [], `🔴 载入+编辑+保存期间仍有外部请求(CDP):${JSON.stringify(uniq(cdpE))}`);
      assert.deepStrictEqual(uniq(pwE), [], `🔴 同上(Playwright 面):${JSON.stringify(uniq(pwE))}`);
      assert.ok(r.cdp.filter((x) => x.cls === 'local').length >= 3, `守卫:本地请求只有 ${r.cdp.filter((x) => x.cls === 'local').length} 条 ⇒ 页面根本没正常加载,这个 0 是空的`);
      assert.strictEqual(r.typed, TYPED, `守卫:编辑没落到编辑器里(实测 ${JSON.stringify(String(r.typed).slice(0, 60))})`);
      assert.deepStrictEqual(r.saved, [TYPED], `🔴 保存环节没拿到我打进去的正文:${JSON.stringify(r.saved)}`);
      // 🔵 注意:本断言只在**不点工具栏**的例上成立(S2)。点过工具栏的例(S3)正文会被按钮改写过,
      //    那里改成"以打进去的正文开头 + 非空"—— 第一跑我把等值断言也用在 S3 上,红得对但不是产品问题。
      assert.deepStrictEqual(r.dialogs, [], `弹了对话框:${JSON.stringify(r.dialogs)}`);
      assert.deepStrictEqual(r.pageErrors, [], `页面报错:${JSON.stringify(r.pageErrors)}`);
      console.log('     🔵 正文从键盘进去、从"保存"按钮出来,一字不差;全程外部请求 0(两个观测面一致)');
    });

    // ── S3 把默认工具栏(除 guide)全点一遍,仍须 0 ─────────────────────────────
    await t('S3 🔴 工具栏全点(除 guide)+ 载入编辑保存 ⇒ 外部请求仍 **0**', async () => {
      const r = await runScenario(isoBrowser, { id: 'S3', cfg: 'offline', kind: 'flow', save: true, clickToolbar: true }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp');
      console.log(`\n     点过的按钮(${(r.clicked || []).length}):${JSON.stringify(r.clicked)}`);
      console.log(`     CDP 外部 ${cdpE.length} · PW 外部 ${ext(r, 'pw').length}`);
      assert.deepStrictEqual(uniq(cdpE), [], `🔴 点工具栏点出了外部请求:${JSON.stringify(uniq(cdpE))}`);
      assert.ok((r.clicked || []).length >= 8, `守卫:只点到 ${(r.clicked || []).length} 个按钮 ⇒ 这一格没验到工具栏`);
      // 工具栏按钮(Bold/Italic/Heading/Quote/列表/链接/图片…)**会往正文里插语法** ⇒
      // 🔴 这里不能断言"保存值 == 打进去的值"(第一跑我就这么写,红了,红的是我的期望不是产品)。
      //    改为:保存值必须**以打进去的正文开头**且**比它长**(证明按钮真的插了东西,而不是空转)。
      assert.ok(Array.isArray(r.saved) && r.saved.length === 1, `保存环节没落一次:${JSON.stringify(r.saved)}`);
      assert.ok(r.saved[0].startsWith(TYPED),
        `🔴 保存出来的正文不是以我打进去的开头 ⇒ 编辑内容被改写/丢失:${JSON.stringify(r.saved[0].slice(0, 90))}`);
      assert.ok(r.saved[0].length > TYPED.length,
        `守卫:点了 ${(r.clicked || []).length} 个按钮,正文却没变长(${r.saved[0].length} <= ${TYPED.length})⇒ 按钮可能是空转,这一格是空的`);
      console.log(`     🔵 工具栏确实改了正文(${TYPED.length} → ${r.saved[0].length} 字),而外部请求仍为 0`);
    });

    // ── S4 guide 按钮:默认工具栏里那颗**外网**按钮 ─────────────────────────────
    await t('S4 🔵 显影:默认工具栏的 "Markdown Guide" 按钮 = **外网站点**(点了就出网)', async () => {
      const r = await runScenario(isoBrowser, { id: 'S4', cfg: 'offline', kind: 'guide' }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp'), pwE = ext(r, 'pw');
      const urls = uniq(cdpE).concat(uniq(pwE)).join(' ');
      console.log(`\n     window.open 目标(主证据)=${JSON.stringify(r.opened)}`);
      console.log(`     网络面:CDP 外部 ${cdpE.length} · PW(context 级)外部 ${pwE.length} · popup ${r.popups} · URL:${JSON.stringify(uniq(pwE))}`);
      assert.ok(Array.isArray(r.opened) && r.opened.some((u) => /markdownguide\.org/.test(u)),
        `🔴 形状变了:点 "Markdown Guide" 没有把用户送去 markdownguide.org(实测 window.open=${JSON.stringify(r.opened)})`
        + '\n     ⇒ 若将来它被改指内网,这一格会红 —— 那是好事,报组长把它转成"已内化"再撤');
      // 🔵 网络面在**断网**下可能因代理直接失败而不留 popup 请求记录;这一条只作旁证,不作判据
      if (pwE.length) console.log(`     🔵 旁证:网络面也确实尝试了 ${pwE.length} 条外部请求`);
      else console.log('     🔵 旁证:网络面 0 条(死端口代理下 popup 导航当场失败)—— 主证据是上面的 window.open 目标');
      console.log('     🔵 结论:这颗按钮**只在被点击时**出网(载入/编辑/保存全程不碰它 ⇒ 不影响 S2/S3 的 0);'
        + '但在内网侧点它 = 打不开一个外网站点 ⇒ 必须移除或改指内网文档');
    });

    // ── S5 不隔离的对照:证明那些外联是**真的会出去**,不是被我拦没了 ────────────
    await t('S5 🔵 对照(**不隔离**):默认配置 ⇒ 那些外联请求真的能到外网(证明 S1 抓到的是真请求)', async () => {
      const b2 = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
      try {
        const r = await runScenario(b2, { id: 'S5', cfg: 'default', kind: 'flow' }, port);
        results.push(r);
        const pwE = ext(r, 'pw');
        const ok = pwE.filter((x) => !x.failed);
        console.log(`\n     外部请求 ${pwE.length} 条,其中**成功** ${ok.length} 条:${JSON.stringify(uniq(ok))}`);
        assert.ok(ok.length > 0,
          `🔴 不隔离时外部请求也没成功过(${pwE.length} 条全失败)⇒ 我没法证明"隔离才是挡住它的那一层"。`
          + `实测:${JSON.stringify(pwE.slice(0, 4))}`);
        console.log('     🔵 对照成立:同样两个开关不关,请求**真能出去** ⇒ S2/S3 的 0 是隔离+配置共同作用的结果,不是探针瞎');
      } finally { await b2.close().catch(() => {}); }
    });

    // ── S6/S7 存储:先证探针有牙,再量默认配置 ─────────────────────────────────
    await t('S6 🔴 存储阴格(探针有牙):打开 autosave ⇒ **必须**看到 localStorage 被写', async () => {
      const r = await runScenario(isoBrowser, { id: 'S6', cfg: 'autosave', kind: 'flow', save: true }, port);
      results.push(r);
      const ls = (r.storage || {}).localStorage || {};
      console.log(`\n     localStorage=${JSON.stringify(ls)} sessionStorage=${JSON.stringify((r.storage || {}).sessionStorage)} idb=${JSON.stringify((r.storage || {}).indexedDB)}`);
      assert.ok(Object.keys(ls).length > 0,
        '🔴 存储探针没有分辨力:显式开了 autosave 却一个 key 都没看到 ⇒ 后面 S7 的"空"不可信,先修探针');
      console.log('     🔵 探针有牙:autosave 一开,localStorage 立刻有 key ⇒ S7 的"空"是真结论');
    });

    await t('S7 🔴 EasyMDE 有没有 Vditor 那种"悄悄写草稿"的默认?(offline 配置,autosave 不开)', async () => {
      const r = await runScenario(isoBrowser, { id: 'S7', cfg: 'offline', kind: 'flow', save: true }, port);
      results.push(r);
      const st = r.storage || {};
      console.log(`\n     localStorage=${JSON.stringify(st.localStorage)} sessionStorage=${JSON.stringify(st.sessionStorage)}`
        + ` indexedDB=${JSON.stringify(st.indexedDB)} serviceWorker=${JSON.stringify(st.serviceWorkerRegistrations)}`);
      assert.deepStrictEqual(Object.keys(st.localStorage || {}), [],
        `🔴 有同类默认:未开 autosave 却写了 localStorage:${JSON.stringify(st.localStorage)}`);
      assert.deepStrictEqual(Object.keys(st.sessionStorage || {}), [],
        `🔴 未开 autosave 却写了 sessionStorage:${JSON.stringify(st.sessionStorage)}`);
      assert.deepStrictEqual(st.indexedDB || [], [], `🔴 建了 IndexedDB 库:${JSON.stringify(st.indexedDB)}`);
      assert.deepStrictEqual(st.serviceWorkerRegistrations, 0, `🔴 注册了 service worker:${JSON.stringify(st.serviceWorkerRegistrations)}`);
      assert.deepStrictEqual(ext(r, 'cdp').map((x) => x.url), [], `🔴 同时还有外部请求:${JSON.stringify(uniq(ext(r, 'cdp')))}`);
      console.log('     🔵 实测结论:autosave 是**opt-in**(不开就一个字节都不落盘),没有 Vditor `cache.enable=true` 那种默认;'
        + '\n        ⇒ 对 I 项「正文唯一源」这条**不构成**同类风险(但 autosave 一旦被人打开,就会在浏览器里存第二份正文)');
    });

    await isoBrowser.close().catch(() => {});
  } finally {
    await browser.close().catch(() => {});
    await new Promise((r) => server.close(r));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  console.log('  🔵 这条绿【看不见什么】:');
  console.log('     ① 隔离是**浏览器层**(死端口代理),不是 OS 级 netns —— 本席无权限(实测 EPERM)。');
  console.log('        它挡住的是"浏览器发出的任何非 loopback 请求",**不包括**任何非浏览器通道(本件也没有)。');
  console.log('     ② 只验了 EasyMDE 2.21.0 这一份字节;Vditor / Cherry **未验**(按裁定不并行)。');
  console.log('     ③ 只走"载入+编辑+保存+工具栏+guide"这条流程;图片上传、自定义 previewRender、插件生态未验。');
  console.log('     ④ 结论口径:「**这套配置下外部请求为 0**」,不是「EasyMDE 永不出网」——');
  console.log('        默认配置**会**出网(S1/S5 实测),必须显式关掉那两个开关 + 处理 guide 按钮。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
````

### 11.2 `page.html`(被测页;`?cfg=default|offline|autosave`)

````html
<!doctype html><meta charset="utf-8"><title>DW · EasyMDE 断网实跑夹具</title>
<link rel="stylesheet" href="/vendor/easymde.min.css">
<textarea id="md"></textarea>
<button id="dwSave" type="button">保存</button>
<pre id="dwOut"></pre>
<script src="/vendor/easymde.min.js"></script>
<script>
(function () {
  var cfg = new URLSearchParams(location.search).get('cfg') || 'default';
  var opts = { element: document.getElementById('md') };
  // 🔴 "offline" 只动两个开关 —— 这就是 ts-No.75 里查到的两个默认外联点
  if (cfg === 'offline' || cfg === 'autosave') { opts.spellChecker = false; opts.autoDownloadFontAwesome = false; }
  if (cfg === 'autosave') { opts.autosave = { enabled: true, uniqueId: 'dw-offline-probe', delay: 100 }; }
  window.__dwCfg = cfg;
  window.__dwEditor = new EasyMDE(opts);
  document.getElementById('dwSave').addEventListener('click', function () {
    var v = window.__dwEditor.value();
    fetch('/save', { method: 'POST', headers: { 'Content-Type': 'text/plain;charset=utf-8' }, body: v })
      .then(function (r) { return r.text(); })
      .then(function (t) { document.getElementById('dwOut').textContent = 'saved:' + t; });
  });
  // 隔离自证:本地必须通,外部必须不通(不通时把**错误名**带回来,好知道是哪一层拦的)
  window.__dwProbe = async function () {
    var out = {};
    try { var r = await fetch('/ping'); out.local = 'OK ' + r.status; }
    catch (e) { out.local = 'FAILED ' + e.name + ': ' + e.message; }
    var ext = {
      jsdelivr: 'https://cdn.jsdelivr.net/codemirror.spell-checker/latest/en_US.aff',
      bootstrapcdn: 'https://maxcdn.bootstrapcdn.com/font-awesome/latest/css/font-awesome.min.css',
      example: 'https://example.com/'
    };
    for (var k in ext) {
      try { var rr = await fetch(ext[k], { mode: 'no-cors' }); out[k] = 'REACHED status=' + rr.status + ' type=' + rr.type; }
      catch (e2) { out[k] = 'BLOCKED ' + e2.name + ': ' + e2.message; }
    }
    return out;
  };
  window.__dwStorage = async function () {
    var ls = {}, ss = {}, i;
    for (i = 0; i < localStorage.length; i++) { var a = localStorage.key(i); ls[a] = (localStorage.getItem(a) || '').length; }
    for (i = 0; i < sessionStorage.length; i++) { var b = sessionStorage.key(i); ss[b] = (sessionStorage.getItem(b) || '').length; }
    var idb = null;
    try { idb = (await indexedDB.databases()).map(function (d) { return d.name; }); } catch (e3) { idb = 'ERR ' + e3.message; }
    var sw = 0;
    try { sw = (await navigator.serviceWorker.getRegistrations()).length; } catch (e4) { sw = 'ERR'; }
    return { localStorage: ls, sessionStorage: ss, indexedDB: idb, serviceWorkerRegistrations: sw };
  };
  window.__dwReady = true;
})();
</script>
````

### 11.3 `fa-probe.js`(附带实测 F1–F3)

````javascript
// 🔵 ts-No.77 **附带实测**(不属组长判据,是我自己加的一格):
//    修法②(`autoDownloadFontAwesome:false`)的**代价**是什么?—— 工具栏图标会不会变成空白?
//    以及:自托管 FA 在**断网**下做不做得成?
//    🔴 不量这一格就给"关掉它"的建议 = 只报好处不报代价。
//    本文件**独立**于 run.js/page.html(那两个的字节未动,S0–S7 的数字仍由它们产出)。
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { execFileSync } = require('child_process');
const { chromium } = require('playwright');

const HERE = __dirname;
const OUT = path.join(HERE, 'out');
const PROXY = 'http://127.0.0.1:9';
const WAIT = 20000;

const INK_PY = `
import sys, json
from PIL import Image
from collections import Counter
im = Image.open(sys.argv[1]).convert('RGB')
px = list(im.getdata())
c = Counter(px)
bg, bgn = c.most_common(1)[0]
ink = sum(n for col, n in c.items() if col != bg)
print(json.dumps({"w": im.size[0], "h": im.size[1], "bg": list(bg), "bgpx": bgn, "ink": ink, "distinct": len(c)}))
`;

function ink(png) {
  const out = execFileSync('python3', ['-c', INK_PY, png], { encoding: 'utf8' });
  return JSON.parse(out.trim().split('\n').pop());
}

function startServer() {
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    if (u === '/ping') { res.writeHead(200, { 'Content-Type': 'text/plain' }); return res.end('pong'); }
    if (u === '/' || u === '/fa') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(fs.readFileSync(path.join(HERE, 'fa-page.html')));
    }
    if (u.startsWith('/vendor/')) {
      const rel = u.slice('/vendor/'.length);
      const f = path.join(HERE, 'vendor', rel);
      if (!f.startsWith(path.join(HERE, 'vendor')) || !fs.existsSync(f)) { res.writeHead(404); return res.end('nf'); }
      const ct = f.endsWith('.css') ? 'text/css' : f.endsWith('.js') ? 'text/javascript'
        : f.endsWith('.woff2') ? 'font/woff2' : 'application/octet-stream';
      res.writeHead(200, { 'Content-Type': ct, 'Cache-Control': 'no-store' });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); res.end('nf');
  });
  return new Promise((r) => server.listen(0, '127.0.0.1', () => r({ server, port: server.address().port })));
}

const classify = (url, port) => {
  if (url.startsWith(`http://127.0.0.1:${port}`) || url.startsWith(`http://localhost:${port}`)) return 'local';
  if (/^(data|blob|about|filesystem):/.test(url)) return 'inline';
  return 'external';
};

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  console.log('EasyMDE Font Awesome A/B(ts-No.77 附带实测 · 断网)');
  console.log(`  隔离手段  ${PROXY}(与 S0–S7 同一手段:死端口代理,浏览器层)`);
  console.log('  问题      修法② `autoDownloadFontAwesome:false` 的**代价**是什么?自托管 FA 在断网下成不成?');

  const { server, port } = await startServer();
  let browser;
  try {
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox', `--proxy-server=${PROXY}`] });
  } catch (e) { console.error('环境不可用:' + e.message.split('\n')[0]); process.exit(3); }

  let pass = 0, fail = 0; const failures = [];
  const t = async (title, fn) => {
    process.stdout.write(`· ${title} … `);
    try { await fn(); pass += 1; console.log('✅'); }
    catch (e) { fail += 1; failures.push(title.split(':')[0]); console.log(`\n  🔴 ${String(e.message).split('\n').slice(0, 8).join('\n  ')}`); }
  };

  const legs = {};
  async function leg(cfg) {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' });
    const page = await ctx.newPage();
    const rec = { cfg, cdp: [], pw: [], pageErrors: [] };
    const cdp = await ctx.newCDPSession(page);
    await cdp.send('Network.enable');
    cdp.on('Network.requestWillBeSent', (e) => rec.cdp.push({ url: e.request.url, cls: classify(e.request.url, port) }));
    ctx.on('request', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'req' }));
    ctx.on('requestfailed', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'failed' }));
    page.on('pageerror', (e) => rec.pageErrors.push(String(e.message).slice(0, 120)));
    await page.goto(`http://127.0.0.1:${port}/fa?cfg=${cfg}`, { waitUntil: 'load', timeout: WAIT });
    await page.waitForFunction('window.__dwReady === true', null, { timeout: WAIT });
    await page.waitForTimeout(2000);            // 给字体加载留时间
    await page.evaluate('document.fonts.ready');
    rec.fa = await page.evaluate('window.__dwFa()');
    const bar = await page.$('.editor-toolbar');
    assert.ok(bar, `装置:cfg=${cfg} 找不到 .editor-toolbar ⇒ 编辑器没起来,别把这条读成"图标正常"`);
    const png = path.join(OUT, `${cfg}-toolbar.png`);
    await bar.screenshot({ path: png });
    rec.png = png;
    rec.ink = ink(png);
    rec.externalCdp = rec.cdp.filter((x) => x.cls === 'external').map((x) => x.url);
    rec.externalPw = Array.from(new Set(rec.pw.filter((x) => x.cls === 'external').map((x) => x.url)));
    await ctx.close().catch(() => {});
    legs[cfg] = rec;
    return rec;
  }

  try {
    await t('F1 🔴 腿 A(不自托管 FA):图标**必须不存在**(无 FontAwesome face · ::before 无内容 · 元素 0×0)', async () => {
      const r = await leg('offline');
      console.log(`\n     faces=${JSON.stringify(r.fa.faces)} · boldIcon=${JSON.stringify(r.fa.boldIcon)}`);
      console.log(`     🔴 faCheck=${r.fa.faCheck} —— **不作判据**(理由见本格末尾那条自我更正)`);
      console.log(`     sheets=${JSON.stringify(r.fa.sheets)}`);
      console.log(`     工具栏像素 ${r.ink.w}x${r.ink.h}:背景色 ${JSON.stringify(r.ink.bg)} · 非背景像素 ${r.ink.ink} · 不同颜色 ${r.ink.distinct}`);
      assert.ok(!r.fa.faces.some((f) => /FontAwesome/.test(f)),
        `🔴 没自托管却枚举到 FontAwesome face:${JSON.stringify(r.fa.faces)} ⇒ A/B 的对照塌了`);
      assert.strictEqual(r.fa.boldIcon.beforeContent, 'none',
        `🔴 没有 FA 却仍有 ::before 内容(${JSON.stringify(r.fa.boldIcon.beforeContent)})⇒ 图标不靠 FA,我这条推论错了`);
      assert.ok(r.fa.boldIcon.rectW === 0 && r.fa.boldIcon.rectH === 0,
        `守卫:图标元素仍有尺寸 ${r.fa.boldIcon.rectW}x${r.fa.boldIcon.rectH} ⇒ "没有字形"这个说法要重新表述,别照抄`);
      assert.ok(!/FontAwesome/.test(r.fa.boldIcon.fontFamily),
        `守卫:computed font-family 仍是 FontAwesome(${r.fa.boldIcon.fontFamily})⇒ 与 faces=[] 矛盾,先查装置`);
      assert.ok(r.fa.toolbarButtons >= 8, `守卫:工具栏只有 ${r.fa.toolbarButtons} 颗钮 ⇒ 编辑器没正常装配`);
      assert.deepStrictEqual(r.externalCdp, [], `🔴 断网腿居然有外部请求:${JSON.stringify(r.externalCdp)}`);
      assert.deepStrictEqual(r.pageErrors, [], `页面报错:${JSON.stringify(r.pageErrors)}`);
      console.log('     🔵 自我更正(本件第二次"探针先怀疑自己"):我原先拿 document.fonts.check("14px FontAwesome") 当判据,'
        + '\n        实测它在**没有任何 @font-face 匹配**时**也返回 true**(faces=[] 而 check=true)⇒ **它证明不了"字体在"**。'
        + '\n        🔴 判据已换成三个互不依赖的量:`document.fonts` 枚举 · ::before content · 元素几何(0×0)。');
    });

    await t('F2 🔴 腿 B(**本地**自托管 FA):图标**必须在**,且**仍然零外部请求**', async () => {
      const r = await leg('localfa');
      console.log(`\n     faces=${JSON.stringify(r.fa.faces)} · boldIcon=${JSON.stringify(r.fa.boldIcon)}`);
      console.log(`     sheets=${JSON.stringify(r.fa.sheets)}`);
      console.log(`     工具栏像素 ${r.ink.w}x${r.ink.h}:背景色 ${JSON.stringify(r.ink.bg)} · 非背景像素 ${r.ink.ink} · 不同颜色 ${r.ink.distinct}`);
      assert.strictEqual(r.fa.faError, null, `装置:本地 FA 那份没挂上(${r.fa.faError})⇒ 这条腿不能用来证明"自托管可行"`);
      assert.ok(r.fa.faces.some((f) => /FontAwesome\/loaded/.test(f)),
        `🔴 本地挂了 FA 却没有 loaded 的 FontAwesome face(${JSON.stringify(r.fa.faces)})⇒ 自托管这条路**没验成**`
        + '(是我的装置或 FA 版本对不上,**不是产品缺陷**)');
      assert.ok(/FontAwesome/.test(r.fa.boldIcon.beforeFontFamily),
        `守卫:::before 的 font-family 不是 FontAwesome(${r.fa.boldIcon.beforeFontFamily})⇒ 图标不是本地这份 FA 画的`);
      assert.notStrictEqual(r.fa.boldIcon.beforeContent, 'none', '守卫:::before 仍无内容 ⇒ 图标没出来');
      assert.ok(r.fa.boldIcon.rectW > 0 && r.fa.boldIcon.rectH > 0,
        `守卫:图标元素仍是 0×0(${r.fa.boldIcon.rectW}x${r.fa.boldIcon.rectH})⇒ 字体 loaded 了却没画出来,别报"自托管可行"`);
      assert.deepStrictEqual(r.externalCdp, [], `🔴 自托管 FA 这腿仍有外部请求:${JSON.stringify(r.externalCdp)}`);
      assert.deepStrictEqual(r.externalPw, [], `🔴 同上(Playwright 面):${JSON.stringify(r.externalPw)}`);
      assert.ok(r.fa.sheets.some((s) => /\/vendor\/fa\.min\.css/.test(s)), `守卫:样式表里没有本地 FA ⇒ 这腿量的不是自托管:${JSON.stringify(r.fa.sheets)}`);
    });

    await t('F3 🔴 A/B 有分辨力:自托管后工具栏**真的多出字形**(单位高度墨迹必须显著增加)', async () => {
      const a = legs.offline, b = legs.localfa;
      assert.ok(a && b, '装置:两条腿没都跑成,无法 A/B');
      // 🔵 两腿工具栏高度不同(实测 52 vs 49 px)⇒ 用"每行墨迹"归一化后再比,不拿裸像素数糊弄
      const apx = a.ink.ink / a.ink.h, bpx = b.ink.ink / b.ink.h;
      console.log(`\n     不自托管 非背景像素 ${a.ink.ink} / 高 ${a.ink.h} = ${apx.toFixed(1)} 每行(不同颜色 ${a.ink.distinct})`);
      console.log(`     自托管   非背景像素 ${b.ink.ink} / 高 ${b.ink.h} = ${bpx.toFixed(1)} 每行(不同颜色 ${b.ink.distinct})`);
      assert.strictEqual(a.fa.toolbarButtons, b.fa.toolbarButtons,
        `守卫:两腿工具栏钮数不同(${a.fa.toolbarButtons} vs ${b.fa.toolbarButtons})⇒ A/B 比的不是同一个工具栏`);
      assert.ok(bpx > apx * 1.5,
        `🔴 自托管 FA 后每行墨迹没有显著增加(${apx.toFixed(1)} → ${bpx.toFixed(1)})⇒ **不能说"关掉自动下载只是图标变空、自托管就补回来"**;`
        + '\n     那意味着 FA 与这套图标其实无关,我这条推论没有证据 —— 如实报,不要替它找补。');
      console.log(`     🔵 结论:不自托管 FA ⇒ 工具栏图标**没有字形**(每行墨迹 ${apx.toFixed(1)}px,基本只剩边框/分隔线;`
        + `图标元素实测 0×0、::before content=none)`
        + `\n        从**本地**挂一份 FA 4.7.0 ⇒ 每行墨迹 ${bpx.toFixed(1)}px(${(bpx / Math.max(0.0001, apx)).toFixed(1)}×),图标元素 ${b.fa.boldIcon.rectW}x${b.fa.boldIcon.rectH},且**断网下零外部请求**`
        + '\n        ⇒ 修法② 的代价是**必须同时自托管 Font Awesome**(或改写 EasyMDE 的图标 CSS 用本仓图标),不是"关个开关就完事"。');
    });
  } finally {
    await browser.close().catch(() => {});
    await new Promise((r) => server.close(r));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  console.log('  🔵 这条绿【看不见什么】:');
  console.log('     ① 只量了**工具栏**那一条截图的墨迹,没量编辑区/状态栏/预览面板的图标;');
  console.log('     ② 自托管用的是 **Font Awesome 4.7.0**(bootstrapcdn `latest` 当前发的那份),');
  console.log('        🔴 它是**外网取回来再本地服务**的字节 ⇒ 引入时该走 npm 包/内网镜像,并补哈希 pin,别抄这个来源;');
  console.log('     ③ 墨迹差是"有字形 vs 无字形"的判别,**不是**观感评价(好不好看归业主);');
  console.log('     ④ 隔离仍是**浏览器层**(死端口代理),与 S0–S7 同一手段、同一条边界。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
````

### 11.4 `fa-page.html`(附带实测的被测页;`?cfg=offline|localfa`)

````html
<!doctype html><meta charset="utf-8"><title>DW · EasyMDE Font Awesome A/B 夹具(ts-No.77 附带实测)</title>
<textarea id="md"></textarea>
<script>
// 🔵 两腿**唯一差别**:有没有从**本地**挂一份 Font Awesome。
//    offline : autoDownloadFontAwesome:false,且**不**自托管 FA  ← 修法②的"裸"形态
//    localfa : autoDownloadFontAwesome:false + 本地 /vendor/fa.min.css ← 修法②的完整形态
(function () {
  var cfg = new URLSearchParams(location.search).get('cfg') || 'offline';
  window.__dwCfg = cfg;
  window.__dwFaError = null;

  function link(href, cb) {
    var l = document.createElement('link');
    l.rel = 'stylesheet'; l.href = href;
    if (cb) { l.onload = function () { cb(null); }; l.onerror = function () { cb('link failed: ' + href); }; }
    document.head.appendChild(l);
    return l;
  }

  function boot() {
    var s = document.createElement('script');
    s.src = '/vendor/easymde.min.js';
    s.onload = function () {
      window.__dwEditor = new EasyMDE({
        element: document.getElementById('md'),
        spellChecker: false,                 // 外网面①
        autoDownloadFontAwesome: false       // 外网面②
      });
      window.__dwReady = true;
    };
    s.onerror = function () { window.__dwFaError = 'easymde.min.js 载入失败'; };
    document.head.appendChild(s);
  }

  link('/vendor/easymde.min.css');
  if (cfg === 'localfa') link('/vendor/fa.min.css', function (err) { if (err) window.__dwFaError = err; boot(); });
  else boot();

  // 量:图标字体在不在 · 工具栏几颗钮 · bold 那颗的字形从哪来
  window.__dwFa = function () {
    var out = { cfg: window.__dwCfg, faError: window.__dwFaError, faCheck: null, faces: [], toolbarButtons: 0, boldIcon: null, sheets: [] };
    try { out.faCheck = document.fonts.check('14px FontAwesome'); } catch (e) { out.faCheck = 'ERR ' + e.message; }
    try { document.fonts.forEach(function (f) { out.faces.push(f.family + '/' + f.status); }); } catch (e2) {}
    var btns = document.querySelectorAll('.editor-toolbar button');
    out.toolbarButtons = btns.length;
    var b = document.querySelector('.editor-toolbar button.bold');
    if (b) {
      var i = b.querySelector('i') || b;
      var cs = getComputedStyle(i, '::before');
      var r = i.getBoundingClientRect();
      out.boldIcon = {
        innerTag: i.tagName, innerClass: i.className,
        fontFamily: getComputedStyle(i).fontFamily,
        beforeContent: cs.content, beforeFontFamily: cs.fontFamily,
        rectW: Math.round(r.width), rectH: Math.round(r.height)
      };
    }
    for (var k = 0; k < document.styleSheets.length; k++) {
      try { out.sheets.push(String(document.styleSheets[k].href)); } catch (e3) { out.sheets.push('(inaccessible)'); }
    }
    return out;
  };
})();
</script>
````

---

## 十二 板件待贴片段(逐字;按既有裁定 **由组长自己贴,我不动板件**)

```text
## ✅ ts-No.77 EasyMDE 断网实跑 —— 交付(qwen38maxkhovvob4,只验不引入)

交付件 `runbooks/DELIVERY-EASYMDE-OFFLINE-0.2.1.50-qwen38maxkhovvob4.md`;
同批更正调研件 `runbooks/RESEARCH-MARKDOWN-EDITOR-0.2.1.50-qwen38maxkhovvob4.md`(被本件推翻的那条,原文划掉保留)。
(🔵 锚不给自引:一改就变 ⇒ **远端 tip 即本件所在提交**。行号随树变,请以 tip 为准。)
🔴 **仓内 package.json 仍为 0 个**;装置与被测字节只在 `/tmp/md-easymde-offline/`(源码已逐字内联进交付件 §十一);
   未碰 `release/*`,未改产品件。

**实跑**:S0–S7 **8/0 rc=0** · 附带 F1–F3 **3/0 rc=0**。

**判据结论(口径不许升级)**:「**这套配置下外部请求为 0**」,**不是**「EasyMDE 永不出网」。
    offline 配置(`spellChecker:false` + `autoDownloadFontAwesome:false`)载入+真键盘编辑+保存+工具栏全点(12 颗)
    ⇒ **外部请求 0**(CDP 与 Playwright 两个独立观测面一致),正文 61 字一字不差。

🔴 **隔离手段的诚实交代(必须与结论一起读)**:
    **浏览器层**,不是 OS 层 —— 本席 `CapEff=0`、`unshare -U/-n/-Un` 全 EPERM、`iptables` Permission denied(uid 1006)
    ⇒ 用**死端口代理** `--proxy-server=http://127.0.0.1:9`(实测该端口 Connection refused;非域名白名单)
    ⇒ **每跑先做 S0 隔离自证**(本地必须 200、3 个外部域必须全 BLOCKED),**不过就 rc=3 中止,一条 0 都不产出**。
    🔴 它**不覆盖**非浏览器通道(本件被测对象没有这类通道)。

🔴 **推翻调研件一条包级结论**:EasyMDE 默认外网面是 **3 处**,不是 1 处 ——
    ① 拼写词典 `cdn.jsdelivr.net/…/en_US.aff|.dic`
    ② **Font Awesome `maxcdn.bootstrapcdn.com`,默认开、载入即出网**(`autoDownloadFontAwesome` 只有显式 false 才不下载)
    ③ 默认工具栏 `guide` 按钮 → `window.open("https://www.markdownguide.org/basic-syntax/")`(点了才出网)
    **漏报原因**:CDN 正则里没有 `maxcdn`;guide 的 URL 是不含 `cdn` 字样的字符串常量 ⇒ **grep 的关键字决定你能看见什么**。
    ⇒ 调研件 §六 E 行已由「1 处」更正为「**3 处**」。
    🔴 **Vditor / Cherry 未做同类更正 —— 它们仍是包级证据,同类漏报风险原样存在。**

🔵 **修法②的代价(附带实测,断网下 A/B)**:关掉 FA 自动下载而**不**自托管 ⇒ 工具栏图标**没有字形**
    (`button.bold > i` 实测 `::before content=none`、**rect 0×0**、computed 回退到 Arial;墨迹 27.8/行);
    从**本地**挂 FA 4.7.0 ⇒ 字形回来(rect 10×13、`faces=["FontAwesome/loaded"]`、墨迹 61.5/行 = **2.2×**),
    **且断网下外部请求仍 0** ⇒ 自托管可行。
    ⇒ **完整形态 = 关开关 ＋ 自托管 FA(或改写图标 CSS 用本仓图标)**;后者与 `data-dw-theme` 主题全局化更合拍,但那是工作量。

🔵 **存储副作用(组长点名那条)**:EasyMDE `autosave` 是 **opt-in** —— 不开时
    localStorage/sessionStorage/IndexedDB/serviceWorker **四类全空**(S7);开了立刻写 `smde_<uniqueId>`(S6 阴格先证探针有牙)
    ⇒ **没有 Vditor `cache.enable=true` 那种默认写草稿的坑**;🔴 但一旦有人打开 `autosave`,浏览器里就有**第二份 spec 正文**
    ⇒ 引入时须**显式禁止**(与 I 项「正文唯一源」相撞)。

🔵 **不隔离对照(S5)**:同样两个开关不关,**4 条外部请求真的成功**(词典 ×2 + FA css + FA woff2)
    ⇒ S2/S3 的 0 是「隔离 + 配置」共同作用的结果,**不是探针瞎**。

🔴 **三次"探针第一次红",全是我的件、无一条产品缺陷**(已按"先怀疑探针"处置):
    S3 我断言 `saved == typed`,而工具栏按钮**本来就会改正文** ⇒ 改成"以 typed 开头且更长" + 空转守卫;
    S4 我用 per-page 监听统计 popup,**竞态漏掉首个导航请求** ⇒ 假红(且假红长得像"guide 不出网"这个好消息)
       ⇒ 改 context 级监听 + 把主证据换成 `window.open` 桩(**不依赖网络时序**);
    F1 我拿 `document.fonts.check("14px FontAwesome")` 当判据,实测它**对无 @font-face 匹配的 family 也返回 true**
       ⇒ 🔴 **它证明不了"字体在"**;判据换成 fonts 枚举 + `::before content` + 元素几何。

🔴 **仍没验到**(交付件 §九 九条,逐字保留):渲染保真度/预览 HTML · 图片上传 · 暗色与本仓 CSS 冲突 · 大文档性能 ·
    Vditor/Cherry 全部 · 自托管拼写词典那条路 · FA 字节的正式取源与 pin · 多次重跑抖动(我判不需要,**是我的判断不是实测**)。
🔵 **三渲染器逐字比对**:按裁定**挂起未做**。**跑者**:仍归 `coder56solzzc`,我未接、未动。
```
