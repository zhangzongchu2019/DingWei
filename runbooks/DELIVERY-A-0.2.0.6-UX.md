# A 线 UX 返工交付件 · 业主 2026-09-18 浏览器验收两条 ＋ 追加 R4

- **分支**：`opus50tc2k/a-0.2.0.6-ux-upload-feedback`（基于 A 线锚 `dd43e95c` 之后的 `665b5f7d`）
- **只动一个产品文件**：`view-service/renderer-canary-sse.html`（＋两份回归脚本、巡检期望值）
- **契约 §2 四字段未碰，未触 B 线**
- **验收人**：`coder56solzzc`（R-1 互为验收，不自审）
- **时刻**：`2026-09-18T23:21Z`

## 0. 业主原话与对应改动

| 业主 | 改动 |
|---|---|
| 「选择文件上传时，没有任何提示，这个很容易认为是出问题了」 | **R1** 上传队列：每个文件一行，文件名 ＋ 已传/总 ＋ 进度条 ＋ 状态；失败行标红并带可读原因 |
| 「输入框中的链接（文件名，字节数），不知道怎么来的。如果能显示为缩略图就更好了」 | **R2** 草稿附件块：图片出真缩略图，非图片出类型图标 ＋ 原名 ＋ 人类可读大小 |
| 下载下来是 `f5790871….bin`，**Word 打不开** | **R4** `download` 填已知原名；取不到则留空回退，不编名 |

## 1. 根因复核（组长判读，我核过，一致；并补一条他没提的）

`uploadOneFile` 三段全 `await`，只有终态改 UI ⇒ 票据/上传/等响应三段一律无声。
源码那句「上传成功前从不修改输入框」是为保护用户已输入文字而立，
**它把「不覆盖用户输入」与「不给任何反馈」绑在了一起** —— 只需解开后者。R3 保留前者。

🔴 **我补一条组长未提、但 R1 依赖的事实**：`fetch` **不上报上传进度**（无 `upload.onprogress`）。
要给「已传/总字节」，上传 POST 必须换 `XMLHttpRequest`。这是满足 R1 的**必需技术选择**，不是偏好；
票据、请求头、失败取 `{"error"}` 等语义与原 `fetch` 版逐条一致。

## 2. R2 的一条硬约束（组长明示，我在实现与测试里都钉住了）

**正文仍然是那条 https 直链，一个字都不动。** 缩略图是**附加的视觉层**，不替代正文 ——
接收侧靠扫正文里的 `/f/` 直链分派（附录 B2），为了好看把链接拿掉会让接收方什么都渲染不出来。
⇒ 草稿块从**输入框现有文本**里扫出直链再渲染：它只是正文的投影，用户删掉文字它就消失。

**并为此立了一条不变式断言**（用例⑩）：**附件块数 == 正文里的 `/f/` 直链数**。
📌 实测过一条失同步途径：有代码直接赋值 `humanInput.value` 而不派 `input` 事件 ⇒ 块留在上一份草稿上。
产品里已注明该约束；这条断言就是盯它的。

## 3. 证据

### 3.1 浏览器功能级 21/21（默认 FIXTURES，不带任何环境变量）

新增四条：

    ⑨  上传进行中界面可见：文件名 + 已传/总字节 + 进度在动，多文件逐个一行
    ⑩  草稿附件块：图片真缩略图（断言 naturalWidth>0）、非图片图标；正文仍保留原样直链 + 不变式
    ⑪  R4-V1/V2：本页上传 `marketing-price-center-v2 (1).docx`，download 属性 == 原名（空格与括号原样）
    ⑪b R4-V3：接收侧服务端不带 `Content-Disposition filename` ⇒ 回退空 download，不报错不编名
    ⑫  上传失败：行标红 + 服务端可读原因；且不清掉用户已输入的文字（R3）

单元级 `renderer-t147-paste-regression.js` 8/8（新增⑧：票据等待 → **非零**字节进度 → 完成，顺序也断言）。

### 3.2 变异实证（整包跑，不带 `-run`；每发只红它针对的那条）

| 变异 | 结果 |
|---|---|
| V1 去掉 `xhr.upload` 的 progress 监听 | 单元级 7/1 红⑧ |
| V1b progress 不报字节 | 单元级 7/1 红⑧ |
| V2 不建进度行 | 浏览器 19/1 红⑨ |
| V3 草稿块不渲染 | 浏览器 19/1 红⑩ |
| V4 `download` 回到空值 | 浏览器 19/1 红⑪（`download="" href=…bin`） |
| V5 失败不标在行上 | 浏览器 20/1 红⑫ |
| V6 失败时清空输入框 | 浏览器 19/2 红⑫ ＋ 红⑧（R3 两处都盯着） |

### 3.3 🔴 两处覆盖边界，写明不含糊

1. **字节进度的「数在涨」由单元级盯，浏览器级盯不到**：本机 mock 同机，54~62 KiB 一个 chunk 发完，
   `xhr.upload` 观测不到中间值（实测 `metaSeen` 只有初始值与终态）。
   ⇒ 浏览器级验「可见性」，单元级用可控 fakeXHR 验「progress 真的接上」。**两级分工，各写明覆盖哪一半。**
2. **接收侧的原名：B5 落地后仍拿不到原名，我先前那句话只对一半 —— 已更正。**
   组长 2026-09-18 撤销了「服务端设 `Content-Disposition: filename*`」这条判据，
   原因是 **`/f/` 是 nginx `alias` 直发、不经过 Go，那个头没有代码位置能设**。
   B5（实现锚 `68e84d6d`）走的是**另一条路**：落盘时保留真扩展名，URL 变成 `<fileid>.docx`。
   ⇒ 接收侧下载下来 **Word 能打开了**（扩展名对），但**文件名是 `<32位fileid>.docx`，不是原名**。
   🔴 所以我先前写的「B5 落地后接收侧自动就对」**只对一半**：
   「能打开」对了，「叫原来的名字」没有。⑪b 验的回退支仍然成立且仍是对的 ——
   接收侧 `download` 属性取不到原名时留空回退，正是当前的真实状态。
   **A 侧不必改；但别把它说成已经全对。**

### 3.4 哈希

    f842302d66b06a86cfea7f05ba3499a4044ddb2ddeb9e6defdfd443653a552e6  view-service/renderer-canary-sse.html
    c96eb1924abfe5d3a2f75dcaebc271c87472af02c3a2c1f643eb457ac22ed08c  view-service/renderer-0206-file-render-regression.js
    4f83aa700d35d45e2959f8a90a6963028f50f3b78a1b6d876478d6ca3ae59265  view-service/renderer-t147-paste-regression.js

巡检期望值已在**同一次改动里**回写（改 renderer 必然打破 `renderer-matches-anchor`，
`deploy/test_invariants_expected_hashes.py` 当场把我拦下）。全仓 `go test ./...` 分开 2 次 rc=0/0。

## 4. 🔴 我做不到的那一条：**人打开界面传 ≥2MB 文件并描述看到了什么**

组长判据明写这一条不可由制品级替代。**我能给的是机器视角的等价证据，不是那一条本身**：
截图在 `runbooks/evidence-A-UX/`（`04-upload-in-progress.png` 可见两行并行：
`sample.mp4 已附加 · 54.0 KiB` 与 `sample.webm 0% · 0 B / 62.5 KiB`）。
**≥2MB 且真实网络下的「30 秒里数在涨」只有人能确认** —— 那一格留给验收人与业主。

## 5. 过程中发现、已修的两处我自己的方法问题

1. **变异脚本「跑完再还原」**：某轮被超时杀掉，还原没执行，**产品文件带着变异留在工作区**，
   我随后几次测量都基于被变异的文件。已改为**每次先还原再变异**。
2. **用例间状态耦合**：⑨ 失败时 `release()` 未执行，把挂起的上传路由漏给后续用例 ⇒ 1 真红 + 5 级联。
   已把挂起放进 `try/finally`，并让 ⑩⑪ 自备输入。**爆炸半径读不出来时，变异结果就没有意义。**


---

## 8. 组长从我的证据截图里看出的两条（2026-09-18T23:45Z）

### ① 重复的「⬇ 下载视频」—— 既有缺陷，顺手修

    video.addEventListener('error', () => video.replaceWith(fileDownloadLink(...)))   // 替换出一条
    box.appendChild(fileDownloadLink(url, '下载视频', knownName));                     // 又无条件附一条

解不了码 ⇒ error 触发 ⇒ **两条并排**。业主那台真浏览器上，**凡解不了码的视频都会撞上**
（他那个 HEVC 的 `.mov` 正是）。

**改法**：保留无条件那一条（**能播时也该能下载**），error 分支只 `video.remove()`。
⇒ 能播时 1 条、不能播时也是 1 条。
**断言**：④b 的 `>= 1` 收紧为 **`=== 1`**。
**变异 W1**（改回 `replaceWith`）⇒ 红 ④b：「解不了码时应恰好一条下载链接，实测 2 条」。

### ② 完成态进度行不清理 —— 本次新引入，按裁定清

**裁定照办**：完成行在**对应草稿块出现后即移除**；失败行保留；进行中保留。
实现上顺序要紧：先 `renderDraftAttachments()` 再 `row.done(size, url)`，
`done` 查到该 url 的草稿块才立即移除（查不到留一个 2.5s 兜底，不把行永久挂住）。

**变异 W2**（完成行不清理）⇒ 红 ⑨；**变异 W3**（失败行也被清掉）⇒ 红 ⑫
⇒ **「完成态清、失败态留」是两条独立规则，各有一发变异盯着。**

### 🔴 由 ② 带出的一处断言更正（我原来的断言测的是被裁掉的行为）

⑨ 原本断言「多文件时**同时**看得见两行」。**清理完成行之后这条不再成立** ——
上传是**顺序**的，任一瞬间通常只有一行。
⇒ 改为**时间维度**断言：全程采集行名，断言 `sample.webm` 与 `sample.pdf` **各自有过自己的一行**。
R1 要的是「每个文件都被看见过」，那本来就是时间上的性质，我先前把它写成了空间上的。

📌 顺带：⑨ 里原有的「看得见『已附加』」断言也一并删掉 ——
**它会把「完成行留在屏幕上」也算通过，正是被裁掉的那个行为。**
换成「都传完后队列清空 ＋ 草稿块接住」。

### 8.1 单元替身同步

`done()` 现在要查 `draftAttachments` 与 `CSS.escape`。Node 侧替身补了 `querySelector`
与 `CSS` 注入 —— **替身不跟着产品走，测的就是上一版产品**（今天已栽过一次）。

### 8.2 本轮证据

    浏览器 21/21（默认 FIXTURES，不带环境变量） · 单元 8/8
    W1 / W2 / W3 三发变异，各只红它针对的那一条
    全仓 go test ./... 分开 2 次：rc=0 / rc=0
    巡检期望值同次回写（renderer 改了 ⇒ 我自己那条测试当场拦下）

    f6d03a8f026f533bd3b176ccd81f47d66f608e68a00fc5f15829ca637079854a  view-service/renderer-canary-sse.html
    4fdca73de5ac63bf345b77da8580781f18029231b7ba35a4fa1f1d329f9d87d0  view-service/renderer-0206-file-render-regression.js
    52c885c09e405e8f4feb87bcc0c5fe793a003fc46e41b9ea5b5b0ce3311a4076  view-service/renderer-t147-paste-regression.js


## 9. 契约 `cb8ac605` 第④条：我实测了，不是接受它（2026-09-18T23:50Z）

组长判「A 线反向映射表**不必**为外-A 新增条目，查不到即落 `download` 兜底，行为已正确」。
这条关系到业主那个 docx，我实跑而不是照收 —— **两条路径 × 七种扩展名**：

    HEAD 可用（Content-Type: octet-stream ＋ attachment）
      docx xlsx pptx doc xls ppt zip  →  dispatch=head  inline=0  links=1   ✅
    HEAD 不可用（降级按扩展名）
      docx xlsx pptx doc xls ppt zip  →  dispatch=ext   inline=0  links=1   ✅

⇒ **组长判断成立**，表不必动。新增用例 ⑬ 固化这 14 格。
**变异 X1**（往降级表里误加 `docx: 'text'`）⇒ 红 ⑬：`ext/docx 不得内联，实测 inline=1`。

### 9.1 🔴 打 ⑬ 的变异时，发现 attachment 头那道守卫**从未被用到**

**变异 X2**（把 `disposition.includes('attachment')` 整段去掉）⇒ ⑬ **仍全绿**。
原因：外-A 的 `Content-Type` 本就是 `octet-stream`，
`kindFromMediaType` 自己就返回 `download` ⇒ **⑬ 走的根本不是那道守卫**。
能分辨它的只有「**可内联的类型 ＋ attachment**」这一格 —— 而此前无人测。

**已补 ⑬b**：服务端声明 `image/png` 但带 `Content-Disposition: attachment` ⇒ 必须下载。

### 9.2 🔴 ⑬b 第一版**也是空的**，我自己又踩了一次

⑬b 首版的 GET 正文是假字节 `'x'`。结果：没有守卫时 `<img>` 因加载失败**自行降级**成下载链接
⇒ 断言在「守卫在」和「守卫没了」两种情况下**都绿**（实测 X2 零转红）。
**换成真 PNG 字节后**，X2c ⇒ 红 ⑬b：`{"d":"head","img":1,"links":0}`。

📌 记法：**当被测行为的失败路径与正确行为的结果相同，PoC 的素材必须让两者分开。**
假素材会让「产品做对了」和「产品做错但恰好也降级了」看起来一样 ——
这和「粗变异」「want/got 同源」是同一族：**证据看起来有，分辨力是零。**

### 9.3 本节证据

    浏览器 23/23（新增 ⑬ ⑬b） · 单元 8/8
    X1 ⇒ 红 ⑬ · X2c ⇒ 红 ⑬b，各只红它针对的那一条
    产品文件本节未改动（⑬/⑬b 只加测试）⇒ 巡检期望值无需回写

    e98666890787d237b178bc4d67502f5270e65911c36d6c9c70b681381c4417d0  view-service/renderer-0206-file-render-regression.js


---

## 10. 🔴 P0：中文文件名根本传不上去（A 侧，2026-09-18T23:55Z）

### 10.1 改了什么

    - xhr.setRequestHeader('X-File-Name', file.name || 'upload.bin');
    + xhr.setRequestHeader('X-File-Name', `UTF-8''${encodeURIComponent(file.name || 'upload.bin')}`);

按组长定死的协议，与服务端 `setDownloadHeaders` 已在用的 `filename*=UTF-8''…` **同一套**，不另发明。
`encodeURIComponent` 的输出必然全 ASCII ⇒ 永不再触发 ISO-8859-1(ByteString) 异常。

**契约替身也跟着实现了协议的 B 侧那一半**（`UTF-8''` 前缀则 percent-decode，无前缀按裸值）——
否则我会对着**编码后**的名字写断言，测出来的是替身的行为不是契约的行为。

### 10.2 三条判据

| 判据 | 结果 |
|---|---|
| **A-E1** 中文 / emoji / 空格括号 / 含 `%` 四种名都能成功上传 | ✅ 浏览器 ⑭：四个真文件全部上传成功，输入框回填**原名**，草稿块显示**原名**（不是 `%E5%AD%A3…`），且全程无未捕获异常 |
| **A-E2** 头值全 ASCII | ✅ 单元 ⑨：`/^[\x20-\x7e]*$/` 逐个断言，并断言 `UTF-8''` 前缀与解码可还原 |
| **A-E3** 去掉 `encodeURIComponent` ⇒ 中文用例必须转红 | ✅ 见下 |

**变异 P1b（去掉 `encodeURIComponent`）**：

    浏览器 23/1  →  ⑭ `page.waitForFunction: Timeout 60000ms exceeded`
                    （**上传根本没发生** —— 与业主撞上的症状同形）
    单元   8/2   →  ⑨「头值必须全 ASCII，实测 "季度报告（终版）.pdf"」
                    ⑩「% 必须被转义成 %25，实测 "50%off.pdf"」

📌 **浏览器侧的红是"超时"而不是"断言失败"，这恰好就是这个缺陷的真实形态**：
异常发生在 `setRequestHeader`，请求从未离开浏览器，所以服务端什么也看不到、
日志里什么也没有 —— 业主看到的就是「点了没反应」。

### 10.3 🔴 为什么 21/21 全绿还漏了它 —— 第五次同型

**夹具文件名全是 ASCII**（`sample.mp4` `sample.png` `sample-office.docx`…）。
**用 ASCII 名测文件名编码，测多少遍都测不出来。**

与今天前四次同型：用白名单内的样本测白名单（mp4/webm 都在旧表里 ⇒ iPhone 整类失效没测出）、
用假字节测 ⑬b（失败路径与正确行为结果相同）、`want` 与 `got` 同源、粗变异。
📌 **共同点都是：素材落在被测维度的同一侧。** 测编码要用非 ASCII，
测白名单要用白名单外，测「A 等于 B」要让 A、B 来源不同 —— **素材必须跨过那条线，否则断言无从分辨。**

⇒ 我在 ⑭ 里把四种名字**写死成用例**（中文 / emoji / 空格括号 / 含 `%`），
它们此后会一直跨在那条线上。

### 10.4 本轮证据

    浏览器 24/24 · 单元 10/10 · 变异 P1b 浏览器与单元同时转红且各中要害
    全仓 go test ./... 分开 2 次：rc=0 / rc=0
    巡检期望值同次回写

    8c1991a74d66fc23d74def35aab22791f2f83f6d3e8e3cb8aa52001dff2fd2a8  view-service/renderer-canary-sse.html
    b585dbada87ffd1cb5dc7c2e203871454c7290907c4610a099aa63265928a9f7  view-service/renderer-0206-file-render-regression.js
    b9fc29bd483b44c328707b0b47a323690c3bab0e87eecebb2c48f770a1ce4b9e  view-service/renderer-t147-paste-regression.js
    1aea368919df82428feda7a6f0b270027708569233cc6645cb324f7e827db26c  view-service/renderer-0206-contract-mock.py

### 10.5 我这侧**证明不了**的

1. **B 侧解码与 `safeName` 顺序**：组长定「先解码再 safeName」，
   并要求 `%2e%2e%2f` / `%0d%0a` / `%00` 三个编码形态被挡住。
   **那是 B 线的判据**，我的替身只做解码、不做 `safeName` ——
   ⇒ **我的 24/24 不构成对 B 侧安全性的任何证据**，别把它读成端到端已通。
2. 未在真浏览器 + 真后端上传过中文名文件（我用的是契约替身）。


---

## 11. R5：链接—内容交替排列（2026-09-19T00:12Z）

业主原话：「多个文件上传，文件名在上方，文件内容展示在下面，最好是一个文件链接下一个文件展示」。

### 11.1 改了什么

| 裁定 | 实现 |
|---|---|
| R5-1 文件名改为链接 | `fileAttachmentShell` 的 `attachment-name` 由 `<span>` 改为 `<a href=<直链> rel=noopener target=_blank>` |
| R5-2 正文中已渲染的 URL 视觉隐藏 | `hideRenderedLinks(cardEl)`：走 `.event-content` 的文本节点，把命中 URL 包进 `<span class="dw-link-rendered">`（CSS `display:none`） |
| R5-3 只隐藏**成功渲染**的 | 附件块就绪时打 `data-rendered="ok"`（`dispatch !== 'gone'` 才打；`catch` 分支不打）⇒ 过期/失败的正文原样可见 |
| R5-4 附件块仍在卡片上 | 未动 —— 正文每个流式 chunk 重写 `innerHTML`，搬进去会让播放中的 `<video>` 反复重建 |

**交替排列本身不需要新代码**：每个 `.attachment` 内部本就是 head(名) → 内容，
`.attachments` 是 `flex-direction: column` ⇒ 名1→内容1→名2→内容2… 已经成立。
**真正分离的是正文里那几条裸 URL**，R5-2 解决的正是它。

### 11.2 两个实现细节，都是实测逼出来的

1. 🔴 **重复包裹**：`hideRenderedLinks` 在每个附件就绪时都会重跑，首版把同一条 URL
   **反复嵌套包了 9 层**（三条链接 × 多次调用）。已在 TreeWalker 上加祖先判断
   （`closest('.dw-link-rendered')` 即 `FILTER_REJECT`）。
2. 🔴 **只隐藏不删除**：`span.textContent` 必须仍是那条 URL。
   删掉会让「接收侧扫正文 `/f/` 分派」失效（附录 B2），也会让不变式失去锚。
   变异「隐藏时清空文本」⇒ 红：`DOM 中 /f/ 直链总数应仍为 4，实测 1`。

### 11.3 判据与变异

    ⑮ 一条用例覆盖 R5-V1/V2/V3/V4：
       名在上内容在下 ✅ · 名是 <a> 且 href == 该块直链 ✅
       恰好隐藏 3 条（== 成功渲染块数）✅ · DOM 中直链总数仍为 4 ✅
       🔴 HEAD 404 的过期链接在正文中**保持可见** ✅（R5-V4 的阳性对照）

| 变异 | 转红 |
|---|---|
| **R5-V5** 把「只隐藏成功渲染的」改成「全隐藏」 | ⑮：`应恰好隐藏 3 条，实测 4` |
| 名不是链接（`<a>` → `<span>`） | ⑮：`文件名必须是 <a> 且 href == 该块直链: [false,false,false,false]` |
| 隐藏时清空文本（等价于从 DOM 抹掉） | ⑮：`DOM 中 /f/ 直链总数应仍为 4，实测 1` |

### 11.4 🔴 一处我差点写错的断言（套件与探针给出不同结果）

⑮ 首版用 `document.querySelector('.message-card')` 取卡 —— **探针里通过，套件里失败（隐藏数 1 ≠ 3）**。
因为探针只有一张卡，而套件里前面的用例留下了若干张，`querySelector` 取到的是**最早那张**。
已改为 `[...document.querySelectorAll('.message-card')].pop()`。
📌 **单独跑通过、进套件失败，往往不是 flaky，而是选择器在单卡环境下不具区分力** ——
与今天「素材落在被测维度同一侧」是同一族。

### 11.5 本轮证据

    浏览器 25/25 · 单元 10/10 · 三发变异各只红 ⑮ 且理由各不相同
    全仓 go test ./... 分开 2 次：rc=0 / rc=0 · 巡检期望值同次回写

    0f9a15c8c74b961f4ae5fd38b56d4fcbe3d915e06a13a5ee2e35fad1f97a937e  view-service/renderer-canary-sse.html
    abb43f1a2a8a7c6505a10658861c0e19bb0b20d41376bc5aeb7bb5f101d6f3ba  view-service/renderer-0206-file-render-regression.js
