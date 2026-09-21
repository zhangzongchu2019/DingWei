# 🔍 定界件 — specKit 展示面走哪条图片渲染路径(`0.2.1.50` 交付条件)

    席别   qwen38maxkhovvob4(renderer)· 派工:组长 ts-No.79 判合格信的「下一格 2️⃣」
    性质   🔴 **只读定界** —— 产品件、测试件、跑者、板件**一行未动**;探针**未入仓**(理由见 §十一)
    被测树 `coder56solzzc/spec-management-02110` tip **`ea6cc30b`**(我 `git ls-remote` 自核;
           🔴 板件记的 `b9bf8ba0` 已前进两格 —— 跨席给坐标必须带锚,这里带上)
    口径   下面每一条都标了【实测】/【读码】/【规范知识】。**没标的一律不要当实测读。**

---

## 〇 一句话结论

🔴 **spec 展示面不走任何渲染路径 —— 它走的是【第零条】:把 spec 正文当纯文本塞进 `<textarea>`。**

⇒ 组长提级的那条交付条件「**spec 展示面必须与 timeline 走同一条图片渲染路径,或显式证明它自带等效兜底**」:
   **两个分支目前都不成立。**
⇒ 业主在 spec 里看到的既不是图、也不是「点这里下载」的卡,而是 `![示意图](https://…/f/….png)` **这串源码字符**。
⇒ 🔴 **ts-No.79 的成果到不了 spec** —— 它落在 renderer 的 markdown 渲染路径上,而 spec 页根本不调它。
⇒ 【实测】更强的那条:spec 页对图片**连一次取图请求都没发**(`/f/` 请求数 = 0)。不是"取不到",是"没试"。

---

## 一 定界坐标(全部 read-only)

| 事项 | 坐标(`ea6cc30b` 树上) | 取法 |
|---|---|---|
| 展示面唯一入口 | `cmd/workpulse/main.go:278` `GET /console/specs` → `HandleConsoleSpecsPage` | 【读码】 |
| 页面字节 | `internal/m8/console_specs_page.go` 的 `consoleSpecsPageHTML`(63 行整页内联) | 【读码】 |
| console 左栏入口 | `internal/m8/console_platform.go:130` `Href:"/console/specs", Target:"_blank"` | 【读码】🔵 与既有裁定「本期沿用 `_blank` 独立页」一致 |
| 正文呈现方式 | `<textarea id=new-content>` / `<textarea id=edit-content>`;`openSpec()` 把 `data.revision.content_markdown` **直接赋给 `textarea.value`** | 【读码】+【实测】(§三 O3/O4) |
| 其余 spec 路由 | `main.go:279-286` 全是 **JSON API**(list/get/patch/submit/reviews/claim/archive) | 【读码】🔴 **没有任何一条把 markdown 渲染成 HTML** |

🔵 所以问题不是"它走了另一条渲染路径、兜底不一样",而是**它没有渲染这一步**。

---

## 二 三处独立检索为空(不是"我没看见",是检索结果为零)

【读码/检索】,【实测】的等价证据在 §三。

    1  git grep -n "content_markdown" ea6cc30b --
       ⇒ 全部消费点 = JSON 字段(console_specs.go:43 / model.go:385)· textarea 读写(console_specs_page.go:35,48)
         · store INSERT/SELECT(internal/store/console_specs.go:30,122)· migration 列(0039_console_specs.sql:18)· 测试夹具
       🔴 **没有一个消费点是"渲染"**
    2  git grep -n -i "renderMarkdown|attachFileRenders|md-img|isInlineImageUrl" ea6cc30b -- internal/ view-service/console-specs-browser-regression.js
       ⇒ **零命中**
    3  git grep -n -i "image|upload|/f/|permanent|永久" ea6cc30b -- internal/m8/console_specs.go internal/store/console_specs.go internal/m8/console_specs_page.go internal/store/migrations/0039_console_specs.sql
       ⇒ **零命中** ⇒ 业主那句「进入 spec 的图拷贝到永久存储区」在 spec 侧**也尚未实现**

---

## 三 【实测】一次性探针:观测量表 ＋ 阳性对照

装置:临时 `git worktree`(`/tmp/specwt`,已 `worktree remove`)检出 `ea6cc30b`;
页面字节用**与他那件闸同一取法**从 Go 源码实时抠(起止标记 + 形状守卫,不复制);
mock API 自启自收、`listen(0)` 自挑端口;真 Chromium、真 `page.fill`/`page.click`,不走测试缝。
探针全文见 §十一(它**没入仓**,理由也在那)。

灌入的 spec 正文(同时含两种写法):

    # Spec

    ![示意图](https://dw.example.invalid/f/aa/bb/26/0921/0123456789abcdef0123456789abcdef.png)

    裸链接: https://dw.example.invalid/f/aa/bb/26/0921/0123456789abcdef0123456789abcdef.png

| 观测量 | 基线(`ea6cc30b` 原字节) | 阳性对照(内存里加一条真渲染路径) |
|---|---|---|
| O1 全页 `<img>` 数 | **0** | **1** |
| O2 对 `/f/` 的请求数 | **0** | **12** |
| O3 `#edit-content` 的 tagName | **TEXTAREA** | TEXTAREA |
| O4 textarea 值 == 原 markdown(逐字) | **true** | true |
| O5 页面上有无渲染面 | **false** | true |
| O6 列表行内 `<img>` 数 | **0** | 0 |
| O7 `svg/object/embed/iframe` 数 | **0** | 0 |

🔴 **阳性对照成立** ⇒ O1/O2 那两个 **0 是「真的没有」,不是探针瞎**(组长纪律:先证观测量对变异敏感)。
🔵 对照的做法是**在内存里给页面追加一段脚本**,把 `![alt](url)` 渲成 `<img>` —— **产品字节一个字没动**;
   等待条件用 `#edit-title` 的值(**两版都有**),🔴 不以对照自己造的 `#probe-render` 为等待条件
   (否则对照失效那一刻等待条件就不成立,会把"探针有牙"伪装成"装置坏了")。
🔵 O2 的 **12** 是对照自身形状造成的(它 50ms 轮询重渲 ⇒ 重复请求),**这个数字本身无意义**;有意义的只是 **0 → 非 0**。

🔴 **一处不要误读**:O8(`body.innerText` 含 `![`)在基线是 **false** —— 因为 **textarea 的 value 不进 innerText**。
   ⇒ "业主看见源码字符"这个结论**由 O3+O4 证**(它是可编辑文本框、内容逐字就是那串 markdown),**不由 innerText 证**。
   我把 O8 打出来正是为了**防止下一个人拿它当反证**。

---

## 四 存在性:spec 代码只在冻结分支上

【实测】`git ls-tree -r --name-only`:

    origin/tasks/v0.1-B(tip `8869c808`)  ⇒ 无任何 console_specs* 文件
    f3c2af80(线上 `0.2.1.31`)            ⇒ 无任何 console_specs* 文件
    coder56solzzc/spec-management-02110(`ea6cc30b`)⇒ 有(22 文件 +1601 行)

⇒ 🔴 **本件结论的效力只及于 `ea6cc30b` 这棵树。** 发布线与板件分支上现在**没有 spec 展示面可验**;
   板件既有裁定是「`0.2.1.10` 实现已存在但**冻结**,业主未核完前不得前进」⇒ 我**没部署、没并入、没检出到工作区**。

---

## 五 🔴 对提级那条交付条件的判定

    条件(板件 8869c808 树,3922 行):spec 展示面必须与 timeline 走同一条图片渲染路径,
                                      或显式证明它自带等效兜底。
    判定  🔴 **两条都不成立**(现状):
          · 不同路径 —— 它**没有**路径(§一/§二/§三)
          · 无等效兜底 —— 连 `<img>` 都不产生,谈不上"图取不到时给下载出路";
            而且 ts-No.79 的兜底是靠 `attachFileRenders` 建卡实现的,spec 页**没有这个函数**
    ⇒ 🔵 **这条不是"验收时补一下"的事,是 `0.2.1.50` 里有一格活还没开**:spec 需要一个**渲染面**。
    ⇒ 🔴 而"渲染面"一旦有,`0.2.1.50` 的核心指标(spec 要显示图)才谈得上验收。

---

## 六 实现时的硬约束(由他那件闸决定)

【读码,全文已读】`view-service/console-specs-browser-regression.js`(102 行)钉的是:
两份 spec 同屏 · 打开编辑表单 · `inputValue('#edit-title')` · `fill('#edit-content')` · 保存出 `saved revision 2` · reload 后仍在。

    🔴 硬约束:`#new-content` / `#edit-content` 两个 **textarea 必须保留且可被 Playwright `fill`**
       ⇒ **编辑面不能被换成只读渲染面**(与既有裁定「组件不接管展示,本期只做编辑」同向)
    🔵 它**不**钉"textarea 是正文的唯一呈现" ⇒ **另加一个渲染面不会撞他的断言**
【实测】我在临时 worktree 里跑了他那件:`console specs browser: passed=1 failed=0`(rc=0)
       ⇒ 至少**今天**它是绿的;🔴 但"加了渲染面之后仍绿"**我没验**(那要实现,超出只读定界)。

---

## 七 附带坐标:业主的「永久存储区」可能**不需要新机制**(超出定界范围,但直接决定实现代价,所以报)

【读码】`internal/fileservice`:

    · `RemovalCandidates` 的 SQL 带 **`AND archived=0`**(repository.go:178)
      ⇒ 归档件**既不因保留期到期被删,也不因配额 FIFO 被淘汰**
    · 三层 fail-closed:`MarkDeleting` 拒绝 archived 行 · `FinishRemoval` 复查 archived ·
      归档时 `expires_at` **保持不变**(repository.go:294 的 `CASE WHEN ?=0 THEN ? ELSE expires_at END`)
    · `Retention` 是**全局**配置(`FS_RETENTION`,默认 `filepolicy.DefaultRetention = 7*24h`,policy.go:7),
      且 `Put()` 在 `Retention<=0` 时**直接拒绝上传**(manager.go:153)
      ⇒ 🔴 **"永久"表达不成"把保留期设成无穷"**,只能走 `archived` 那条

【实测】`go test ./internal/fileservice/ -run 'Archived|Archive|Sweep|RemovalCandidates|Retention' -v`
       ⇒ **13/13 PASS**,其中直接相关的四条:
       `TestArchivedFileSurvivesWhileDefaultExpiresAndTombstonePurgesLater` · `TestRemovalCandidatesExcludeArchived` ·
       `TestMarkDeletingRejectsArchivedRow` · `TestFinishRemovalRechecksArchivedFailClosed`

🔴 **三条待裁(我不自决)**:

    1 把 admin 语义的 `archived` 复用为"spec 图永久" = **语义扩张**(它现在的用途是 admin 归档),归组长/业主裁
    2 **授权面**:`SetArchivedByOwner` 要求 `f.OwnerKey == ownerKey`(manager.go:106)
      ⇒ spec 里那张图的 owner 是**上传它的那个 agent**,不是 spec 作者 ⇒ **谁能归档需要新的服务端判据**
    3 「拷贝到永久存储区」业主说的是**拷贝**;而 `archived` 是**原地打标志**(不复制字节)
      ⇒ 两者是否等价,归业主;🔵 工程上原地标志更省,但语义上"拷贝"可能另有所指(例如脱离 session 归属)

---

## 八 三条可选实现路径(只列代价 —— 🔴 我不选、不实现,选型归组长派)

    A 把 renderer 的 markdown 渲染抽成**共享件**,spec 页与 timeline 各引一份
      ✅ 唯一真正满足「同一条渲染路径」的做法;消毒口径天然一致
      🔴 代价:渲染函数现在**内联在 `renderer-canary-sse.html` 里**;抽出会动到 ts-No.76/ts-No.79 两份闸的
         **硬编码锚点字符串**(它们的变异副本靠锚点定位)⇒ 那两件会红,**必须一起改**(不是回归,是锚点搬家)
    B spec 页内嵌 renderer(iframe / 同页复用 `attachFileRenders`)
      ✅ 兜底(图取不到 ⇒ 卡给下载出路)**天然存在**
      🔴 代价:引入第二份 DOM 契约;且 console iframe 那条已有实测结论 ——
         **`color-scheme` 不穿 iframe 边界**(ts-No.78 C4)⇒ 暗色下要重验一遍
    C **服务端**把 spec markdown 渲染成 HTML
      🔴 代价最大:必须与现有「**先转义再 markdown**」的消毒口径**逐字对齐**,否则等于新开一条注入面
         ⇒ ts-No.76 那 17 形状要在新路径上**全部重跑**,`md-injection-payloads.js` 可直接复用(它就是为共用建的)

🔴 **三条都必须满足同一条**(与选哪条无关):**图取不到时要有"下载出路"的兜底,不能只剩 error 撤除。**
🔵 若走 A/B,ts-No.79 的 `isInlineImageUrl` 判据可**原样复用**(`isOwnFileUrl` 只要求同源 + `/f/` 前缀;
   【读码】nginx 把 app 挂在 `/dingwei/`、`/f/` 挂在根 ⇒ **同源**,判据不需要改)。
🔴 **一处实现陷阱**【读码,未实测】:`deploy/nginx-dingwei.conf:35` 有 `sub_filter 'src="/' → 'src="/dingwei/'`;
   若 spec 页自己拼**根相对**的 `src="/f/…"`,会被改写成 `/dingwei/f/…` ⇒ **404**。
   文件服务返回的是**绝对 URL**(`FS_PUBLIC_BASE_URL` + `/f/` + key,http.go:85)⇒ 用绝对 URL 就不踩。
   🔵 这条是读 nginx 配置推出来的,**我没起 nginx 实测**。

---

## 九 🔴 自更正:我上一件里那句"spec 展示面还不存在"**不准确**

`runbooks/DELIVERY-MD-IMAGE-INLINE-0.2.1.50-qwen38maxkhovvob4.md` §七-3 我写的是
「Specs 独立页/未来的 spec 展示面(**还不存在**)」。

    准确的说法  它在**我这棵树/发布线/板件分支上不存在**(§四 三个 ls-tree 为证),
               但在 `coder56solzzc/spec-management-02110` @ `ea6cc30b` 上**已经存在**,只是**零渲染**。
    🔴 差别要紧  "还不存在" 读起来像"以后建的时候再说";
               "**已存在且零渲染**" 意味着 `0.2.1.50` 现在**就缺一格活**,而且缺的正是它的核心指标。
    已在同一提交里给那份交付件补了更正节(§十二),不改原文(留痕)。

---

## 十 🔴 没验到什么(照实登记)

1. 🔴 **没部署、没实跑真 spec 页**(它只在冻结分支上,业主未核完 ⇒ 板件明令不得前进)。
   §三 的实测跑在**临时 worktree + 本地 mock API** 上,页面字节是**从 Go 源码实时抠的真字节**,
   但 **不是** 经 nginx、经 console 鉴权、经真 store 的那条路。
2. 结论只及于 **`ea6cc30b`**。他若已改动,结论要重取(锚会前进,对谁都一样)。
3. 🔴 **"spec 里图片该怎么显示"是产品可见决定**(内嵌尺寸/位置/是否带下载按钮/是否要缩略图)⇒ 归业主,我没替他设计。
4. 永久存储区那条我只定界到「**机制已存在且有 13 条测试**」;🔴 **没验**归档后 `/f/` 直链在 **nginx 侧**是否仍可取
   (【读码】上是同一个 `object_key`,但没实测)。
5. §八 的 nginx `sub_filter` 陷阱是**读配置推的**,没起 nginx 验。
6. §六 那句"加渲染面不会撞他的闸"是**读断言清单推的**;🔴 我没实现,所以**没验**。
7. 本席 **Linux + Playwright 自带 chromium**;业主那台 **macOS + 系统 Chrome** 上没跑过(同型缺口已登记)。

---

## 十一 复现配方(探针**未入仓**的理由 + 全文)

🔴 **为什么不入仓**:探针的被测对象是 `internal/m8/console_specs_page.go`,**它在我这棵树上不存在**(§四)。
   入仓即得到一件**在自己分支上永远 rc=3 的闸** —— 那正是本组在治的"长期红着的套件 = 没人看的告警"。
    precedent:板件既有记录「`qwen38maxkhovvob4` 只读诊断(产品件一行未改,**探针未入仓**)」。
🔵 所以把全文内联在这里,让**手上有那棵树的人**(coder / 组长)能逐字复核。

复现步骤:

    cd /home/is3/dingwei
    git fetch origin coder56solzzc/spec-management-02110
    git worktree add --detach /tmp/specwt ea6cc30b          # 只读,不动工作区
    # 把下面全文存成 /tmp/spec-norender-probe.js
    NODE_PATH=<playwright 的 node_modules> node /tmp/spec-norender-probe.js   # rc=0 = 结论成立
    git worktree remove /tmp/specwt --force

```js
// 🔴 一次性只读探针(不进仓、不改产品):量 specKit 展示面对"图片链接"到底做了什么。
//    被测字节取自 coder56solzzc/spec-management-02110 @ ea6cc30b 的 internal/m8/console_specs_page.go
//    (与他那件 console-specs-browser-regression.js 同一取法:起止标记 + 形状守卫)。
//    观测量:O1 <img> 数 · O2 /f/ 请求数 · O3 #edit-content 的 tagName · O4 textarea 值是否逐字等于原 markdown
//           O5 页面上有没有渲染面(#probe-render) · O6 spec 列表行里有没有图
//    🔴 阳性对照:同页**在内存里**追加一段"把 ![alt](url) 渲染成 <img>"的脚本(不动产品字节),
//       O1/O2 必须移动 —— 否则"0 个 img"可能只是探针瞎(组长纪律:先证观测量对变异敏感)。
//       等待条件一律与变异无关(等 #edit-title 的值,两版都有),不以 #probe-render 为等待条件。
'use strict';
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const { chromium } = require('playwright');

const GO_SOURCE = '/tmp/specwt/internal/m8/console_specs_page.go';
const START = 'const consoleSpecsPageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleSpecsPage';
const IMG_URL = 'https://dw.example.invalid/f/aa/bb/26/0921/0123456789abcdef0123456789abcdef.png';
const MD = '# Spec\n\n![示意图](' + IMG_URL + ')\n\n裸链接: ' + IMG_URL + '\n';
// 阳性对照用的渲染面:轮询 #edit-content,把 markdown 图片语法渲成 <img>(故意用真 URL,好让 O2 也动)
const MUTANT = '<script>(function(){var t=setInterval(function(){var e=document.getElementById("edit-content");'
  + 'if(!e||!e.value||e.value.indexOf("![")<0)return;var d=document.getElementById("probe-render");'
  + 'if(!d){d=document.createElement("div");d.id="probe-render";document.body.appendChild(d);}'
  + 'd.innerHTML=e.value.replace(/!\\[([^\\]]*)\\]\\(([^)\\s]+)\\)/g,\'<img src="$2" alt="$1">\');},50);})();</script>';

function productionHTML() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'end marker missing');
  return source.slice(contentStart, end);
}

async function body(req) { const c = []; for await (const x of req) c.push(x); return c.length ? JSON.parse(Buffer.concat(c).toString('utf8')) : {}; }
const listen = (s) => new Promise((r, j) => { s.once('error', j); s.listen(0, '127.0.0.1', () => r(s.address().port)); });

async function run(html, label) {
  const specs = [], revisions = new Map();
  const server = http.createServer(async (req, res) => {
    if (req.url === '/console/specs') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return; }
    if (req.url === '/api/console/specs' && req.method === 'GET') { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ specs })); return; }
    if (req.url === '/api/console/specs' && req.method === 'POST') {
      const b = await body(req);
      const item = { id: 'spec-1', title: b.title, state: 'draft', current_revision: 1 };
      specs.push(item);
      revisions.set(item.id, { ...b, spec_id: item.id, revision: 1, acceptance_json: JSON.stringify(b.acceptance), input_anchors_json: JSON.stringify(b.input_anchors) });
      res.writeHead(201, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(item)); return;
    }
    const m = String(req.url || '').match(/^\/api\/console\/specs\/([^/]+)$/);
    if (m && req.method === 'GET') {
      const item = specs.find((c) => c.id === m[1]);
      if (!item) { res.writeHead(404); res.end(); return; }
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ spec: item, revision: revisions.get(item.id) })); return;
    }
    res.writeHead(404); res.end('not found');
  });
  const port = await listen(server);
  const fRequests = [];
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    page.setDefaultTimeout(5000);
    page.on('request', (r) => { try { if (new URL(r.url()).pathname.startsWith('/f/')) fRequests.push(r.url()); } catch {} });
    await page.goto(`http://127.0.0.1:${port}/console/specs`, { waitUntil: 'domcontentloaded' });
    await page.fill('#new-title', 'Spec A');
    await page.fill('#new-content', MD);
    await page.fill('#new-acceptance', 'A1 ok');
    await page.fill('#new-anchor', 'msg:1');
    await page.click('#create-spec');
    await page.locator('#spec-list [data-title="Spec A"]').waitFor({ state: 'visible' });
    await page.locator('#spec-list [data-title="Spec A"] .edit-spec').click();
    // 🔴 等待条件与变异无关:两版都有 #edit-title
    await page.waitForFunction(() => document.getElementById('edit-title').value === 'Spec A');
    await page.waitForTimeout(600);   // 给变异版的 50ms 轮询留时间
    const obs = await page.evaluate(() => ({
      O1_imgs: document.querySelectorAll('img').length,
      O3_editTag: (document.getElementById('edit-content') || {}).tagName || null,
      O4_textareaValue: (document.getElementById('edit-content') || {}).value || null,
      O5_renderPane: !!document.getElementById('probe-render'),
      O6_rowImgs: document.querySelectorAll('#spec-list img').length,
      O7_svgOrObject: document.querySelectorAll('svg,object,embed,iframe').length,
      O8_bodyTextHasBang: document.body.innerText.includes('!['),
    }));
    return { label, obs: { ...obs, O2_fRequests: fRequests.length, O2_urls: fRequests.slice(0, 3) } };
  } finally {
    await browser.close();
    await new Promise((r) => server.close(r));
  }
}

(async () => {
  const html = productionHTML();
  const base = await run(html, '基线(ea6cc30b 原字节)');
  const mut = await run(html + MUTANT, '阳性对照(内存里加一条真渲染路径,产品字节未动)');
  console.log('=== 基线观测量 ===');
  console.log(JSON.stringify(base.obs, null, 1));
  console.log('=== 阳性对照观测量 ===');
  console.log(JSON.stringify(mut.obs, null, 1));
  // 🔴 先证观测量对变异敏感
  assert.ok(mut.obs.O1_imgs >= 1, '阳性对照失效:加了渲染路径 O1 仍是 ' + mut.obs.O1_imgs + ' ⇒ 探针看不见 <img>');
  assert.ok(mut.obs.O2_fRequests >= 1, '阳性对照失效:加了渲染路径 O2 仍是 ' + mut.obs.O2_fRequests + ' ⇒ 探针看不见 /f/ 请求');
  assert.strictEqual(mut.obs.O5_renderPane, true, '阳性对照失效:渲染面没出现');
  console.log('✅ 阳性对照成立:O1 ' + base.obs.O1_imgs + '→' + mut.obs.O1_imgs + ' · O2 ' + base.obs.O2_fRequests + '→' + mut.obs.O2_fRequests + ' ⇒ 基线那两个 0 是【真的没有】,不是探针瞎');
  // 基线结论
  assert.strictEqual(base.obs.O1_imgs, 0, '基线出现了 <img>:' + base.obs.O1_imgs);
  assert.strictEqual(base.obs.O2_fRequests, 0, '基线对 /f/ 发了请求:' + base.obs.O2_urls.join(','));
  assert.strictEqual(base.obs.O3_editTag, 'TEXTAREA', '基线 #edit-content 不是 textarea:' + base.obs.O3_editTag);
  assert.strictEqual(base.obs.O4_textareaValue, MD, '基线 textarea 值不等于原 markdown(逐字)');
  assert.strictEqual(base.obs.O6_rowImgs, 0, '基线列表行里有图');
  assert.strictEqual(base.obs.O7_svgOrObject, 0, '基线有 svg/object/embed/iframe');
  assert.strictEqual(base.obs.O5_renderPane, false, '基线不该有渲染面');
  console.log('✅ 基线结论:spec 正文只以 textarea 的 value 存在(逐字等于原 markdown),'
    + '全页 <img>=0、对 /f/ 的请求=0、无 svg/object/iframe ⇒ 展示面对图片【零处理】,连尝试取图都没有');
  console.log('🔵 O8 body.innerText 含 "![" = ' + base.obs.O8_bodyTextHasBang
    + ' —— 🔴 别把它读成"业主看见源码":textarea 的 value 不进 innerText;'
    + '业主看见源码这件事由 O3+O4 证(它是可编辑文本框、内容就是那串 markdown),不是由 innerText 证');
  console.log('spec-norender-probe: PASS');
})().catch((e) => { console.error('🔴 ' + (e && e.message ? e.message : e)); process.exit(2); });
```

---

## 十二 板件待贴片段(逐字;按既有裁定 **由组长自己贴,我不动板件**)

```
## 🔍 定界:specKit 展示面走【第零条】渲染路径 —— 它不渲染(qwen38maxkhovvob4,只读,锚 <组长填>)

    被测树  coder56solzzc/spec-management-02110 @ ea6cc30b(🔴 板件记的 b9bf8ba0 已前进两格)
    件      runbooks/BOUNDARY-SPEC-RENDER-PATH-0.2.1.50-qwen38maxkhovvob4.md(十二节)

🔴 提级那条交付条件的【两个分支都不成立】:
   spec 页把 content_markdown **直接塞进 <textarea>**,全页 **<img>=0**、对 **/f/ 的请求=0**、
   无 svg/object/iframe ⇒ 既不与 timeline 同路径,也没有等效兜底。
   业主看到的是 `![示意图](https://…/f/….png)` 这串**源码字符**;ts-No.79 的成果**到不了 spec**。
🔵 【实测】不是推论:阳性对照(内存里加一条真渲染路径,产品字节未动)令 **O1 0→1 · O2 0→12**
   ⇒ 那两个 0 是"真的没有",不是探针瞎。等待条件用两版都有的 #edit-title(不以对照产物为等待条件)。
🔴 检索三处为空:content_markdown 的**全部**消费点无一是渲染 · renderMarkdown/attachFileRenders/md-img/
   isInlineImageUrl 在 internal/ 与他的闸里**零命中** · spec 侧 image|upload|/f/|permanent **零命中**。
🔴 存在性:spec 代码**只在冻结分支**上;板件分支(8869c808)与线上(f3c2af80)都没有 ⇒ 结论只及于 ea6cc30b。
🔵 硬约束(他的闸决定,已实跑 passed=1 failed=0):两个 textarea 必须保留且可 fill ⇒ **编辑面不能换成只读渲染面**;
   但它不钉"textarea 是唯一呈现" ⇒ 另加渲染面不撞他的断言(🔴 这句是读断言推的,没实现所以没验)。
🔵 附带:业主的「永久存储区」**可能不需要新机制** —— fileservice 的 `archived` 已被
   `RemovalCandidates … AND archived=0` 排除在删除候选外,三层 fail-closed,
   【实测】13/13 PASS(含 TestArchivedFileSurvivesWhileDefaultExpiresAndTombstonePurgesLater)。
   🔴 三条待裁:①把 admin 语义的 archived 复用为"永久"是语义扩张 ②归档要求 OwnerKey 相符,
   而 spec 图的 owner 是上传它的 agent ⇒ 需要新的服务端判据 ③业主说的是"拷贝",archived 是"原地打标志"。
🔴 三条实现路径(A 抽共享件 / B 内嵌 renderer / C 服务端渲染)各有代价,**我不选、不实现**,选型请派。
   走 A 会动到 ts-No.76/79 两份闸的硬编码锚点(锚点搬家,不是回归),必须一起改。
```
