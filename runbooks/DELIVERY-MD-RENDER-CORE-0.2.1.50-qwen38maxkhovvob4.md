# ✅ 选型 A「抽 markdown 渲染共享件」— 交付(`qwen38maxkhovvob4`,🔵 产品件**零可执行字节改动**)

    板件派工锚   `b77b0193`(`runbooks/ACTIVE-ASSIGNMENTS.md`「✅ 组长选型:**取 A(抽共享件)**」节)
                 🔵 读自 `origin/tasks/v0.1-B` @ `a88d5d24`(该件 4080~4100 行)。
                 🔴 行号随树变 ⇒ **以节名为准,行号只作参考**(板件纪律)。
    批次         **B0 = specKit**(`runbooks/BATCH-PLAN-20260921.md`:业主定"只提批次号";
                 B0 是**唯一保留号的批次**,号 = `0.2.1.50`)
    分支         `qwen38maxkhovvob4/md-render-core`(基于 `1ea4fcf1` = 板件记的定界锚)
    新共享件     `view-service/md-render-core.js`              381 行  sha256 `63c5e5159cc8f076…`
    同步工具     `view-service/md-render-core-sync.py`         152 行  sha256 `ba7e23289a51cc32…`
    漂移闸(源)   `view-service/test_md_render_core.py`         289 行  sha256 `416c18739e6a23d1…`  **12/12 OK**
    对账闸(行为) `view-service/md-render-core-parity.js`       206 行  **99/99 rc=0 · ≈0.88s(三次极差 0.00s)**
    产品件       `view-service/renderer-canary-sse.html`       **+21 / −0**(全是 `//` 注释标记)
                 sha256 `52ae3f5720b2cab6…` → **`37fb93d0e3ece5dc…`**(🔴 发布阻断项,见 §八)
    文档         `view-service/README.md`  **+18 / −0**(共享件工作流 ＋ 如实标注"漂移闸无自动执行路径")
    两件旧闸     🔴 **一个字未改**(`git status` 可证)—— image **15/0 rc=0** · injection **11/0 rc=0**
    🔴 坐标口径  下面所有行号都在 **`37fb93d0…` 这棵树**上;换树会变 ⇒ 一律同时给**段名/函数名**。

---

## 〇 一句话结论

markdown 渲染那套逻辑现在有了**单一事实源** `view-service/md-render-core.js`;
产品件 `renderer-canary-sse.html` 里那 6 段是它的**生成副本**(标记 `// DW-MD-CORE:<段名> BEGIN/END`),
漂移由闸挡、行为由对账闸钉。🔵 **产品件的可执行字节一字未动**(+21/−0,21 行全是注释)⇒
渲染行为在 timeline 这一侧**不可能变**;spec 那一侧等消费方接上后按 §五 的契约验。

---

## 一 🔴 组长预期的「两件闸锚点搬家」——**实际代价 = 0**,以及为什么(本节最该被读的一句)

组长裁 A 时写:「**A 的代价我接受**:ts-No.76/79 两件闸的硬编码锚点要搬家」,并要求
「**必须有东西证明它只是搬家**」。**实测结论:一件都没搬,因为一件都不需要搬。**

    证据一(机器核,不是我说)  `git diff -U0` 统计:新增 **21** 行、删除 **0** 行;
            其中**非注释新增行 = 0**(逐行 sed 去掉 `//` 前缀后计数)⇒ 产品件**可执行字节零改动**
    证据二  两件旧闸用的是**字符串锚点**,不是行号:锚点命中数 **≠1 就硬报错**、
            🔴 **不静默跳过**(`renderer-markdown-image-regression.js:415`、
            `renderer-markdown-injection-regression.js:255` 各有一条这样的守卫)
            ⇒ 零移动 ⇒ 所有锚点仍恰好命中一次 ⇒ 两件闸 **15/0 · 11/0**,`git status` 里它们**未被修改**

### 🔴 由此我推翻自己原先的一个说法(顺带也修正组长那条预期的分量)

我原先把"两件闸结论逐项相同"当作重构自证的**主证据**。但既然产品件可执行字节**零改动**,
那条硬条件在 renderer 这一侧**几乎必然成立** —— 🔵 **它的信息量比我和组长原先以为的都低。**

真正需要证的是另一件事:**抽出来的那份核心件,行为与产品页是否相同。**
漂移闸只比**源字节**;源字节相同**不等于**行为相同(产品页里可能有同名遮蔽/重定义,
或某符号依赖了 6 段之外的页面全局)。⇒ **我补了一件行为对账闸**(§三-2),
把"抽对了"从**推论**变成**会自己报警的断言**。🔴 这是本格我自己加的一格活,报组长追认。

---

## 二 机制:为什么是「单一事实源 ＋ 生成副本 ＋ 漂移闸」

### 2.1 🔴 为什么不新开一个静态资产路由(让两页都去 `<script src>` 同一份)

两个页面**由不同服务出**:renderer 走 view-service(Python)经 Go 代理;spec 页走 Go(`console_specs_page.go`)。
共用一个 HTTP 资产 ⇒ 必须新增静态路由 ⇒ 牵出三件已知的代价:
**nginx 前缀改写**(`deploy/nginx-dingwei.conf:35` 的 `sub_filter`)· **哈希 pin**(发布阻断项那族)·
**「非白名单路径仍 404」的阴格**。🔵 代价远大于收益,而且它会新增一个**部署期**失败面。

### 2.2 🔴 为什么不包 IIFE / 不改成模块作用域(硬约束①)

renderer 是**一个大 `<script>` 的顶层脚本**,这些 `function`/`const` 因此是**页面作用域**的,
而现有两件闸**依赖这个事实**:

    `renderer-0206-file-render-regression.js` ④i  用 `page.evaluate` **就地改** `FILE_EXT_KIND.svg='image'`
    `renderer-markdown-image-regression.js`   G0b  直接调 `isOwnFileUrl()` 自证边界前提

包进 IIFE ⇒ 两处都取不到 ⇒ 那两格会变成**假绿或假失败**。
🔵 这条现在**有执行侧证据,不靠注释提醒**:`md-render-core-parity.js` 在页面里用 **18 个裸标识符**
逐个取值构造 api 对象 —— 谁把它们包进作用域,那一行**当场 ReferenceError**(rc=2)。

### 2.3 为什么切成 6 段而不是 1 段

这些符号在产品件里**本来就不连续**(与附件路径的代码交错)。合并成一段就意味着**移动代码**,
而移动 `const` 会引入 **TDZ 风险** —— 与本格硬条件「结论逐项相同」直接冲突。⇒ **按现状切 6 段,零移动。**

| 段名 | 内含符号 | 产品件坐标(`37fb93d0…`) | 共享件坐标 |
|---|---|---|---|
| `escape` | `escapeHtml` | `:1487`~`:1496` | `:71`~`:80` |
| `own-url` | `isOwnFileUrl` | `:2611`~`:2618` | `:82`~`:89` |
| `ext-kind` | `FILE_EXT_KIND`(🔴 图片行 == nginx `types{image/*}`,由 G0 对账) | `:2650`~`:2677` | `:91`~`:118` |
| `ext-of` | `fileExtOf` | `:2684`~`:2692` | `:120`~`:128` |
| `never-inline` | `FILE_EXT_NEVER_INLINE`(写死 6 token)＋ `kindFromExt` | `:2714`~`:2725` | `:130`~`:141` |
| `render` | `renderMarkdown`/`renderMarkdownText`/`splitFencedBlocks`/`isTableStart`/`splitTableRow`/`renderTable`/`renderList`/`renderInline`/`isInlineImageUrl`/`mdImageHtml`/`renderCode`/`highlightCode` | `:3623`~`:3831` | `:143`~`:351` |

🔵 **导出钩子**(共享件 `:354` 起)在任何 `DW-MD-CORE` 段**之外** ⇒ 同步工具不会把它复制进 renderer
(renderer 不需要:顶层声明本身就是页面作用域)。名单与段内定义符号**逐项对账**,
少一个就红 —— 因为"静默缺一个函数"比"报错缺"贵得多。

---

## 三 🔴 重构自证(组长那条硬条件)—— 三层证据

### 3.1 第一层:两件旧闸(**装置一字未改**)

| 闸 | 抽件前 `52ae3f57…` | 抽件后 `37fb93d0…` | 装置是否改动 |
|---|---|---|---|
| `renderer-markdown-image-regression.js`(ts-No.79 · 15 格) | **15/0 rc=0** | **15/0 rc=0**(×3 次跑) | 🔴 **未改** |
| `renderer-markdown-injection-regression.js`(ts-No.76 · 17 形状) | **11/0 rc=0** | **11/0 rc=0**(×3 次跑) | 🔴 **未改** |

日志对账(把 `sha256=` / `127.0.0.1:<port>` / 时刻归一后 `diff`):

    injection  🔵 **无任何差异**(逐字节相同)
    image      🔴 **仅一条诊断行不同** —— M-G5 的 `extImgs` 计数,见 §四-1
               (结论行、通过数、其余全部诊断**逐项相同**)

日志留存:`/tmp/refactor-baseline/{image,inj}-{before,after,final}.log`

### 3.2 第二层(🔵 本格新增):**行为对账闸** `md-render-core-parity.js`

它不比源字节,比**输出**:把**同一个 `compute` 函数字面量**(Node 侧直接调、浏览器侧由
`addInitScript` 注入它的 `toString()`,所以"探针自己写了两份"这个形状不成立)分别送进
【真浏览器里的产品页】与【Node 里的共享件】,对 **99 项**结果逐字节对账。

    样本  24 段 markdown(标题/粗斜/代码块 python·json·js/表格/有序无序/引用/hr/
          本站图片两种语法/裸直链/svg/exe/外站图/混合文档/空串/空白串/五实体)
          ＋ 15 个 URL(8 类白名单扩展 ＋ 大写扩展 ＋ 外站 ＋ 根相对 ＋ 带 query#frag ＋ 非 URL ＋ 空)
          ＋ 🔴 **17 条注入形状直接 require 既有单一事实源** `md-injection-payloads.js`(不另抄一份)
    结果  **99/99 逐字节相同,rc=0**

🔴 **先证观测量对变异敏感**(组长所立纪律)。四发变异各打**不同一段**,
一律用 `--core=` 指到 `/tmp` 变异副本,**产品件与仓内共享件一字未动**(跑完复核 sha 未变):

| 变异 | 打哪一段 | 结果 | 差异项 |
|---|---|---|---|
| M1 `class="md-img"` → `md-image` | `render` | **rc=2** | **21 / 99** |
| M2 掏空 `"` → `&quot;`(ts-No.76 认定的**承重墙**) | `escape` | **rc=2** | **20 / 99**(含注入形状 `pay[1]`) |
| M3 `avif: 'image'` → `'download'` | `ext-kind` | **rc=2** | 🔵 **仅 2 / 99** —— 分辨率到表的**单行** |
| M4 转义顺序换成先 `<` 后 `&` | `escape` | **rc=2** | **14 / 99**(含 `pay[0]/[1]/[2]`) |

**双向性也验了**:改**产品页那一份副本**(`RENDERER_OVERRIDE` 指到 `md-img`→`md-image` 的变异件)
⇒ 同样 **rc=2、21 项不同**。🔵 所以它既能抓"共享件被改坏",也能抓"有人直接改了生成副本"。

**rc 语义**(对齐本仓发布约定 0/2/3):

    rc=3 腿①  `--core=` 指到不存在的文件 ⇒ `ENVIRONMENT_UNAVAILABLE: 共享件读不到…`
    rc=3 腿②  清空 `NODE_PATH`(无 playwright)⇒ `ENVIRONMENT_UNAVAILABLE: Playwright 不可用…`
    🔴 另有一道 **location 对齐守卫**:两侧 `location.origin|href` 不同 ⇒ **判 rc=3、不产出任何结论**
       (否则 `own-url` 段的差异会被读成"抽件抽错了",而那是**环境差异**;
        这正是"假故障长得和真故障一模一样"那一族)

**耗时**(组长裁定 5 要的数,接 SUITES 用):**三次 0.88 / 0.88 / 0.88 s,极差 0.00 s**。
自足装置:`context.route` 直接喂字节 ⇒ **不起服务、不占端口、cwd 无关**。

### 3.3 第三层:collateral(所有碰这 6 段符号的闸 ＋ 两侧构建)

先用 `grep` 找出**所有**引用被抽符号的件(不靠记忆):`renderer-0206-file-render-regression.js`、
`renderer-longtext-regression.js`、`renderer-sse-profile.js`、`renderer-t147-paste-regression.js`
＋ 两件 markdown 闸 ＋ `md-injection-payloads.js`。

| 件 | 结果 | 为什么它是要紧的那一件 |
|---|---|---|
| `renderer-0206-file-render-regression.js` | **41/0 rc=0** | 🔴 **硬约束① 的直接消费方**:④h/④i 用 `page.evaluate` **就地改** `FILE_EXT_KIND` ⇒ 它绿就等于"没被包进作用域"在真产品页上成立 |
| `renderer-longtext-regression.js` | **ALL PASS rc=0** | 重压 `renderMarkdown`(8000 字 / 12000 字 / 148 个前缀) |
| `renderer-t147-paste-regression.js` | **10/0 rc=0** | 它是**刮源码**的件(`indexOf('    function appendInputReference')`)⇒ 我插的 21 行注释有没有把它的锚点顶歪,一跑就知道 |
| `python3 -m unittest` × 5 件 | **64 tests OK** | 含 `test_renderer_iter2` / `test_check_public_view_deploy` / `test_provenance_flag_injection`(都直接读 renderer 源码做形状断言) |
| `go build ./...` | **rc=0** | — |
| `go test ./internal/filepolicy/ ./internal/fileservice/` | **ok / ok** | 🔴 图片永久化那格的契约测试(7 天默认值)必须仍绿 |
| `python3 md-render-core-sync.py --check` | **rc=0**「6 段逐字节相同」 | 漂移闸的入口 |
| `python3 -m unittest test_md_render_core` | **12/12 OK** | 含 5 条"闸有牙"腿:双向漂移被检出**并指名是哪一段**、写模式能修回、标记被破坏**硬失败不静默跳过**、导出完整性、只许一个 IIFE、独有 token 每文件只出现一次、Node 冒烟 |

### 🔵 3.4 一处守卫**正确地拒了我**(记录,不是缺陷)

`renderer-longtext-regression.js` 有条 A 对照腿,需要 `BASELINE_FILE`=**修复前**那份病态件。
我把 `BASELINE_FILE` 指到了**抽件前**的 `52ae3f57…`,它 **rc=1** 并打印:

    FAIL: 对照失真:修复前应每 chunk 重渲一次(≥800),实为 1
          —— 说明该度量没有测到目标行为,结论不可用

🔴 **它是对的,我喂错了对照件**(抽件前那份**已经含**长文本修复)。
🔵 守卫拒绝产出一个"看起来对得上"的绿 —— 与本组今天三例同型:**守卫咬住了写守卫的人**。
顺带那次打印里,两份字节的**行为量**相同(markdown 重渲均 1 次、回合卡均 1 张);
时延量(0.6ms / 0.3ms)是单次采样,**不作结论**。

---

## 四 🔴 三条发现(**都只报不修**)

### 4.1 M-G5 的 `extImgs` 诊断是**竞态** —— 我先前那份交付件把它说过头了,现更正

**实测(最硬的一发,本轮新增)**:`image-after.log` 与 `image-final.log` 是**同一个 sha `37fb93d0…`**
的两次跑,`extImgs` 分别给出 **`0→2`** 与 **`0→0`**。

    ⇒ 🔴 同一份字节、两个值 ⇒ 该观测量**不是字节的函数**,是时序的函数
    ⇒ 与"抽件"无因果;我先前在交付件里写"实测 0→0"是**把一次采样当成了稳定值**,说过头了

**成因(实测)**:外站图片的 `error` 事件实测在 **≈2000ms** 才触发(6/6 次,1972~2029ms,DNS 超时),
而该格 `SETTLE_MS=1600` ⇒ **扫描点落在窗口里**,早一点扫是 0、晚一点扫是 2。

**为什么结论没被污染**:该格判据是 `breached = extImgs > 0 || external > 0`,
而 **`external` 在全部 9 次采样里都是 `2`**(请求账目,不受渲染时序影响)⇒ **判定与竞态无关,15/0 稳定**。
🔵 也就是说:红条集合稳定,**只有那条"给人看的诊断数字"在抖**。

**两个候选修法(🔴 待组长裁,我没动)**:

    (i)  把 `extImgs` 标注为**时序相关**,判据主证据改为**请求账目**(`external`)
         🔵 改动最小;代价是那条诊断仍会抖,只是不再被当成结论
    (ii) 给这一格单独加**确定性 settle**(≈2500ms,盖过实测的 2029ms 上限)
         🔵 诊断变稳定;代价是该格 +≈0.9s(整件现 ≈43.5s)
    🔴 我倾向 (i)+(ii) 都做,但**这两条都要改动一件已经绿的闸** ⇒ 按纪律先报后动。

### 4.2 `renderer-regression.js` 有一条 **flake**,🔴 与我的改动**无关**(有对称证据)

**取样表**(每轮 3 次;`git checkout`/`cp` 换字节,换回后**复核 sha** 才继续):

| 时刻(本地) | 字节 | 条件 | 结果 | 失败点与失败值 |
|---|---|---|---|---|
| 19:15 | 新 `37fb93d0` | 串行 ×3 | 1F / 2P | `:319` `1006 !== 0` |
| 19:16 | 旧 `52ae3f57` | 串行 ×3 | **3P** | — |
| 19:18 | 旧 | **3 实例并发** | 1F / 2P | `:310` `true !== false`(`replayInProgress`) |
| 19:22 | 新 | **3 实例并发** | 1F / 2P | `:319` `3164 !== 0` |
| 19:24:09 | 新 | 串行 ×3 | **3P** | — |
| 19:24:48 | 旧 | 串行 ×3 | 1P / **2F** | `:319` `3164 !== 0` 与 `1006 !== 0` |
| **合计** | 旧 **9 跑 3F** · 新 **9 跑 2F** | | | 🔴 **两侧失败值集合相同 `{1006, 3164}`** |

**结论(只说到证据支持的地方)**:

    ✅ 成立  **旧字节也失败,且失败值与新字节逐字相同** ⇒ 该 flake **不是抽件引入的**
    ✅ 成立  同一份字节在不同轮次的失败数从 **0/3 到 2/3** 都有 ⇒ **与轮次/负载相关,不与字节相关**
    🔴 **不成立** "新字节更稳"——n=9 vs 9、失败 2 vs 3,**这个差在我样本量下没有意义**,我不拿它当结论
    🔴 **未验** 我**没有定位根因**,也没量 `mouse.wheel` 的实际时延分布

**为什么我不修**:①板件明令**不得回头去"修"那个已证伪的滚动缺陷**;
②该件自己的注释已记录:Playwright `mouse.wheel` **异步**(实测晚约 20ms)而这里只等 **60ms**,
产品常量是 `USER_SCROLL_GESTURE_MS=400` / `FOLLOW_RELEASE_GAP_PX=160`,且产品注释记有 ≈25% flake 史(T-035);
③失败签名(`:310` 重放未结束 / `:319` 跟随权未交还)是**已知的"代理过期"形状**,不是新的产品行为。
🔵 两处失败点(`:310` 与 `:319`)是**同一格里两条不同断言**;我**没有**观察到同一份字节同时命中两处 ⇒
不把它说成"同一签名",只说"同一格、同一族时序"。

### 4.3 共享件在**非浏览器环境**是**静默降级**的(我原先的读码结论被实测证伪,已更正)

核心件头注原先写「调用 `isOwnFileUrl`/`fileExtOf` **会抛**」——🔴 **那是读码结论,实测证伪**:
两个函数自带 `try { … } catch { return false / '' }`,`location` 的 `ReferenceError` **被 catch 吞掉**。

**实测值(Node v22,`typeof location === 'undefined'`)**:

    isOwnFileUrl(本站 /f/ png) → **false**      fileExtOf(同一 URL)      → **''**
    kindFromExt(同一 URL)      → **'download'** isInlineImageUrl(同一 URL) → **false**
    ⇒ renderMarkdown('![图](本站 /f/ png)') 产出的是**链接**,不是 `<img>`

🔴 **危害形状**:非浏览器消费方(SSR / Node 侧测试 / Worker)会看到"**一张图都不出、却全绿**",
看起来像"产品没做图片支持",真相是环境里没有 `location` —— 与本组反复撞的
"东西建好了但没接上"同一形状。**头注已更正,并由 `test_md_render_core.py` 的 Node 冒烟腿钉住实测值。**
🔵 spec 展示面在浏览器里跑 ⇒ **不受影响**;其余函数都是纯函数(不碰 `location`/DOM)。

**附:我探针第一版自己调错了两个签名**(记录,免得下一个人重犯):
`isTableStart(lines, i)` 收的是**数组＋下标**不是单行;`highlightCode(code, lang)` **只对 python/json 上色**,
`js` 直通转义。我一度把这两条读成"静默降级",**读源码后证伪** —— 🔴 第一跑红先怀疑探针,这次怀疑对了。

---

## 五 给 `coder56solzzc` 的【消费契约】(spec 展示面那一侧)

共享件**只产出 HTML 字符串**。下面四件它**不负责**,宿主页面必须自备,
否则会出现"看起来接上了、其实缺一块":

1. **CSS**:`.md`、`.md img.md-img`(420px 上限那套)、`.code-wrap`/`.code-toolbar`/
   `.tok-kw|.tok-str|.tok-com|.tok-num`、`.table-wrap`
2. **Copy 按钮接线**:`renderCode` 产出 `<button class="copy-code" data-code=…>`,
   **不接线就是个按了没反应的按钮**(renderer 侧由 `wireCopyButtons` 接)
3. 🔴 **图片取不到时的"下载出路"**:`mdImageHtml` 产出的 `<img data-md-img>` 加载失败后,
   renderer 侧靠 `wireMarkdownImages` ＋ `attachFileRenders`(附件卡,带「已过 7 天保留期」与「⬇ 下载图片」)兜底;
   🔴 **那一层不在共享件里**(它依赖 HEAD 探测 / capability / DOM 卡片整套附件机制)。
   组长已明令「**不能只剩 error 撤除**」⇒ spec 页必须**自带等效兜底并自己有闸**。
4. 🔴 **Go 侧的反引号陷阱**:共享件含**模板字面量反引号** ⇒ **不能**塞进 Go 的反引号原始字符串
   (`consoleSpecsPageHTML` 现在正是那种写法)。用 **`go:embed`** ＋ 拼接。

**改法(顺序不能反)**:改 `md-render-core.js` → `python3 view-service/md-render-core-sync.py` → **两份一起提交**。
🔴 不要直接改 renderer 里那 6 段(生成物,下次同步覆盖)。
🔵 接上之后**请把 `md-render-core-parity.js` 的样本扩一条 spec 页的 origin**,
因为 `isOwnFileUrl` 的结果**依赖 origin**(§六-③)。

---

## 六 🔴 本件**没验到**什么(照实登记,一字不删)

1. 🔴 **spec 展示面本身一格都没验。** 共享件抽出来了,但**还没有任何消费方在浏览器里跑过它**
   (coder 那一格未做)。⇒「spec 与 timeline 走同一条渲染路径」目前是
   **结构上成立、运行时未证**。本件证到的是:**共享件与 timeline 产品页输出逐字节相同**(§三-2)。
2. **对账只比纯函数输出**,不比 DOM / CSS / 事件。🔴 尤其**图片取不到时的"下载出路"那一层不在共享件里**
   (`wireMarkdownImages` ＋ `attachFileRenders`),所以它**没有被本件验到任何一分**。
3. **两侧 `location` 是本件垫成同一个 origin 的**(页面走 `context.route`,Node 走 shim)。
   🔴 真实 spec 页的 origin 与 timeline **未必相同**,而 `isOwnFileUrl` 的判定**依赖 origin**
   ⇒ 那一侧必须按它自己的 origin 重验,**本件证不了**。
4. **样本不穷尽**:24 段 markdown ＋ 15 个 URL ＋ 17 条注入形状,都是我选的。
5. 🔴 **两条新闸都没有自动执行路径。** 仓内**无 CI**;`build.sh` 只做 `go build` ＋ `go version`,
   **不跑任何 python/node 测试**(实测 `grep`)。⇒ 漂移闸与对账闸**靠人手动跑**,
   而"靠记得 = 没有"。这是**结构性缺口**,我只报不接(接线归 `coder56solzzc`,裁定归组长,见 §七)。
6. **`renderer-regression.js` 的 flake 没有定根因**,也没量 `mouse.wheel` 的实际时延分布(§四-2)。
7. **M-G5 的竞态没有修**,也**没有实测** `SETTLE_MS` 提到 2500ms 后是否真的稳定(§四-1)。
8. **对账闸只在浅色 / 单档 headless 下跑。** 它比的是**纯函数输出**,推论上与主题无因果 ——
   🔵 **但那是推论**(主题若哪天影响到某个类名常量,推论就塌)。暗色与窄档**未跑**。
9. 🔴 **本席是 Linux ＋ Playwright 自带 chromium,业主那台 macOS ＋ 系统 Chrome 上没跑过**(同型缺口已登记)。
10. **合并后的树上没跑过。** 本分支基于 `1ea4fcf1`;`coder56solzzc` 的 `spec-management-02110` 动 Go 侧。
    🔴 合并顺序归组长;**合并之后这些闸需要重跑一遍**,不能采信本件这轮的绿。
11. **抽件前后的性能未对账。** 🔵 这次我给的是**推论**:可执行字节零改动 ⇒ 性能必然不变;
    但那个推论的**前提是实测的**(`git diff -U0`:非注释新增 0 行、删除 0 行)。
    `renderer-sse-profile.js` 是耗时归因**工具**(需外部 fixture),**不是闸,本轮没跑**。

---

## 七 🔴 待裁(四条,我一条都没自行动手)

**(a) M-G5 竞态怎么修** —— (i) 标注 `extImgs` 时序相关、主证据改请求账目 ／ (ii) 该格 settle 提到 ≈2500ms ／ 两条都做。
　　🔵 我倾向两条都做,但**都要改动一件已经绿的闸** ⇒ 先报后动。

**(b) 两条新闸要不要接 SUITES、由谁接** —— 对账闸 **≈0.88s(极差 0.00s)**、自足装置(`context.route`,不起服务不占端口);
　　漂移闸是 python unittest,**0.5s**、无浏览器依赖。🔴 接线归 `coder56solzzc`,我不自己接。

**(c) 🔴 漂移闸要不要搭 `Onyx-260921-A`「四路构建闸」的车** —— 板件已裁 `build.sh` 将成为"闸与 flags 的唯一实现",
　　且 `Dockerfile` builder 层要装 `python3`。⇒ **那正是漂移闸天然的执行位置**(它就是 python unittest)。
　　🔴 但 `build.sh` / `Dockerfile` / `Makefile` **都不是我的文件**,归 coder 与组长 ⇒ **我只提,不碰**。

**(d) 🔴 `deploy/test_invariants_expected_hashes.py` 现在 rc=1,而它的失败文案与本组惯例冲突 —— 请裁**

    实测(不是推论)  换入**抽件前**的旧字节 `52ae3f57…` ⇒ 该测试**同样 FAIL**
                    ⇒ 🔵 **既有红,不是我引入的**(板件此前已记为「既有红」)
    它说什么        `仓内实际 sha256 = 37fb93d0…` / `巡检 want = 76c56d95…`
                    🔴 「**改动者请在【同一次改动里】回写,不要留给发版当天**」
    冲突在哪        组长历次裁定是「**出单当刻由我在发版锚上重算,不抄任何消息里的值**」＋「**不碰 `release/*`**」
                    ⇒ 我**没有回写**。🔴 但那条文案明确要求改动者当场回写。
    🔴 请裁          **回写(在同一次提交里把 `want` 改成 `37fb93d0…`)** 还是 **保持不动、留给发版锚重算**?
                    🔵 我倾向后者(`want=76c56d95…` 钉的是**线上**那份;我这条分支不是发布线,
                       回写会把"线上应该是 37fb93d0"这句**尚不成立的话**写进巡检件),
                       **但这是组长的裁定权,不是我的**,所以摆上来。
    另核             该测试的**第二条腿**(防"新增钉住仓内文件的巡检项却忘登记进 PINNED")在我树上 **ok**
                    ⇒ 🔵 我新增的三个文件**不需要**登记进 `PINNED`,也不会踩它(实测,非推论)。

---

## 八 🔴 发布阻断项(照旧:我报,不碰)

    renderer sha256  `52ae3f5720b2cab6…` → **`37fb93d0e3ece5dc…`**
    ⇒ `deploy/invariants-dingwei-gray.json:16`(`renderer-matches-anchor`)出单当刻须**重算重填**。
    🔴 我不碰发布线,也不碰 `release/*`;**请出单时在锚上重算,不要抄本件这个数字。**

🔵 **另核两件事,免得下一个人误判**:

- **本格没有新增任何 HTTP 可取的资产或路由。** `view_service.py:556` 的 `do_GET` 是**闭合路由集**
  (`/`、`/index.html`、`/events`、`/status`、`/tasks`,其余一律 404),只 serve **一个硬编码文件**
  `RENDERER_PATH`;`deploy/nginx-dingwei.conf` 里**没有** root/alias 指向 `view-service/` 源码目录
  (`/f/…` 那两个 `alias` 指的都是 `/var/lib/dingwei-file-service/objects/`)。
  ⇒ 🔵 ts-No.77 立的两条引入硬条件(**新增静态资产须补哈希 pin** / **新增静态路由须有 404 阴格**)
  **在本格不触发**。🔴 但 coder 用 `go:embed` 把共享件编进 Go 侧之后,**若那条路对外可取,则在他那一格触发**。
- **`deploy/view-service-release.sh` 按白名单清单打包**(`REPO_FILES` 只有 renderer ＋ 两个 py,
  且**逐路径核对**、多一个都 FAIL)。⇒ 🔵 我新增的三个文件**不该进那份清单**
  (它们不参与 view-service 运行时);共享件只在**构建期**被 Go 侧 `go:embed` 需要。
  🔴 **这条是读码**(我读了 `REPO_FILES`/`FLAT_FILES` 与逐路径核对逻辑),**没有实跑那个发布脚本**。

---

## 九 板件待贴片段(组长自取,我不动板件)

```
## ✅ 选型 A「抽 markdown 渲染共享件」— 交付(`qwen38maxkhovvob4`)

分支 `qwen38maxkhovvob4/md-render-core`(基于 `1ea4fcf1`)。🔴 **锚请组长 `ls-remote` 自核后填**——本件不写自己的提交号(写了就得随 amend 失效)。

**产品件可执行字节零改动**:`git diff -U0` = **+21 / −0**,且**非注释新增行 = 0**(机器核)。
⇒ 组长预期的「两件闸锚点搬家」**实际代价 = 0**:两件闸用的是**字符串锚点**(命中数≠1 硬报错、不静默跳过),
零移动 ⇒ 锚点全部仍恰好命中一次 ⇒ image **15/0** · injection **11/0**,`git status` 里两件**未被修改**。

🔴 **交付方推翻自己原先的一个说法**:既然可执行字节零改动,「两件闸结论相同」在 renderer 侧**几乎必然成立**
⇒ **它的信息量比我们原先以为的低**。真正要证的是"抽出来那份**行为**与产品页相同"
⇒ **自加一格**:`view-service/md-render-core-parity.js`(行为对账闸,**99/99 逐字节相同,rc=0,≈0.88s**)。
两侧跑**同一个 `compute` 字面量**(浏览器侧 `addInitScript` 注入其 `toString()`),
样本含 **17 条注入形状(直接 require 既有单一事实源)** ＋ 8 类图片扩展名 ＋ 表格/代码/列表。
**四发变异各有牙、各打不同一段**:M1 `md-img`→21 项红 · M2 掏空 `&quot;`→20 项红(含注入形状)·
M3 `avif:'image'`→`'download'`→**仅 2 项也红**(分辨率到表的单行)· M4 转义换序→14 项红。
**双向**:改产品页那份副本(`RENDERER_OVERRIDE`)⇒ 同样红 21 项。**rc=3 两腿** ＋ 一道 **location 对齐守卫**
(两侧 origin 不同 ⇒ 判环境不可用、**不产出结论**,防"环境差异被读成抽件抽错了")。

🔴 **三条发现,只报不修**:
① **M-G5 的 `extImgs` 诊断是竞态** —— `image-after.log` 与 `image-final.log` 是**同一个 sha** 的两次跑,
   分别给 **`0→2`** 与 **`0→0`** ⇒ 该量**不是字节的函数**。成因实测:外站图 `error` 在 **≈2000ms**(6/6,1972~2029ms)
   才触发,而 `SETTLE_MS=1600`。**结论未被污染**(`breached` 由 `external` 决定,而 `external` 9 次全是 2)⇒ 15/0 稳定。
   🔴 交付件先前写"实测 0→0"**是把一次采样当成了稳定值,已更正**。两个候选修法待裁。
② **`renderer-regression.js` 的 flake 与抽件无关** —— 各 9 次取样:旧字节 **3F**、新字节 **2F**,
   🔴 **两侧失败值集合相同 `{1006, 3164}`**;同一份字节在不同轮次失败数从 **0/3 到 2/3** 都有。
   🔵 **"新字节更稳"这个说法我不采信**(n 太小)。未定根因;按板件**不回头修已证伪的滚动缺陷**。
③ **共享件在非浏览器环境是【静默】降级**(`isOwnFileUrl`→false / `fileExtOf`→'' / `kindFromExt`→'download')
   ⇒ 症状是"一张图都不出、却全绿"。🔴 头注原先写"会抛"是**读码**结论、**实测证伪**(try/catch 吞掉 ReferenceError),已更正。

🔴 **待裁四条**(详见交付件 §七),其中一条是**冲突**:
`deploy/test_invariants_expected_hashes.py` **rc=1**,其失败文案要求「**改动者在同一次改动里回写**」,
而组长历次裁定是「出单当刻在发版锚上重算 ＋ 不碰 `release/*`」⇒ **我没回写,请裁**。
🔵 实测:**换入抽件前的旧字节它同样 FAIL** ⇒ **既有红,非本件引入**(板件此前已记为既有红)。

🔴 **发布阻断**:renderer sha `52ae3f57…` → **`37fb93d0…`** ⇒ `invariants-dingwei-gray.json:16` 出单当刻重算(我不碰)。
🔵 **本格不触发**「新增静态资产哈希 pin / 新增静态路由 404 阴格」两条硬条件:`view_service.py:556` 是**闭合路由集**
只 serve 一个硬编码文件,nginx 无 root/alias 指向 `view-service/`;🔴 但 coder `go:embed` 之后若对外可取,**在他那一格触发**。
🔴 **没验到什么**(交付件 §六,11 条,一字未删)—— 头两条:
   **① spec 展示面一格都没验**(还没有消费方在浏览器里跑过共享件)⇒「同一条渲染路径」**结构上成立、运行时未证**;
   **② 图片"取不到时的下载出路"那层不在共享件里,本件一分都没验到它。**
```
