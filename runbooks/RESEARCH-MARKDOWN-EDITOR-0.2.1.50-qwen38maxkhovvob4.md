# 调研件 — `0.2.1.50`(I · specKit)markdown 编辑组件**选型对照表**

**派工** 组长 ts-No.75(板件锚见 `runbooks/ACTIVE-ASSIGNMENTS.md` 末节「派工调整 — ts-No.75」)
**交付方** `qwen38maxkhovvob4` · 2026-09-21
**边界(照裁定执行)** 🔴 **只调研**:未引入任何依赖、未装任何包、未改产品、未碰发布线。
仓内 `package.json` 数量**仍为 0**(实测),所有下载物只在 `/tmp/md-research/`,**不在仓内**。

---

## 〇 一句话

**三个候选都能用,但没有一个"拿来就行"** —— 每一个都至少撞上本仓一道硬约束(见 §二),
且 🔴 **三者都过不了同一条**:**图片直链只活 7 天**(§二-6),那是**产品/基建决定,不是选型决定**。

**供裁的排序(🔴 不是结论,且未经任何浏览器实跑)**:

| 名次 | 候选 | 为什么它排这里 | 它的硬伤 |
|---|---|---|---|
| 1 | **EasyMDE 2.21.0**(MIT) | 约束最少:**104KB gz** · UMD 直用 · 上传是**纯客户端回调**(服务端 0 改动) | 无所见即所得 · 默认会打外网取拼写词典 · 无自带暗色 · 附件只支持图片 |
| 2 | **Vditor 4.0.0**(MIT) | 功能最全(WYSIWYG/IR/分屏 + 数学图表 + `setTheme` 暗色 API) | 真实离线载荷 **≈656KB gz 起**(编辑器 69KB + lute 引擎 578KB),全功能 **21.9MB**;默认 `cdn` 指向 **unpkg**;默认往 **localStorage 写草稿**(与"正文唯一源"冲突);上传响应体要适配 |
| 3 | **Cherry Markdown 0.11.10**(Apache-2.0) | **上传/附件口径最贴合我们**(`fileUpload(file,cb)` + `urlProcessor` + 多类型附件表) | 🔴 **npm 包不发 `.min.js`**:core 2.26MB/591KB gz、full 12.7MB/2.3MB gz(**未压缩**);体积最大 |

---

## 一 取数方法(三类证据**分开标**)

| 标 | 含义 | 本件里哪些是 |
|---|---|---|
| 🔬 **实测(包级)** | 今天(2026-09-21)从 `registry.npmjs.org` 取元数据 + 下载 tarball 到 `/tmp/md-research/tb/`,量 `gzip -9` 字节数、`grep` 包内字符串 | 体积 · 发布日 · 许可证字段 · 反引号数 · UMD 与否 · CDN 串 · 默认值 · 依赖数 · 包内文件清单 |
| 📖 **读码(本仓)** | 只读本仓源码得出的结论,**没有运行时验证** | §二 六道约束 · §六 "要改几处" 清单 |
| 🌐 **网络检索** | 搜索引擎/上游页面,**未逐个复核** | 仅用于**佐证**淘汰(tui.editor 自述 No Longer Maintained、cherry 是腾讯开源);**不作为任何一格的唯一依据** |

🔴 **本件里没有任何一条"浏览器实跑"证据。** 见 §八-1。

---

## 二 🔴 本仓地形:六道硬约束(任何候选都要过)

> 这一节是本次调研**最值钱的部分**:它不是从候选文档抄来的,是**量本仓量出来的**。
> 换任何一个候选,这六道都还在。

**1️⃣ 仓内没有 npm 构建链 —— `package.json` 实测 0 个**(`find . -name package.json -not -path './.git/*' | wc -l` → `0`)
⇒ 任何 **ESM-only / 需编译**的候选(`md-editor-v3`、`@uiw/react-md-editor`、`@milkdown/*`)
**等于新增一条构建链**,不是"加个组件"。

**2️⃣ view-service 没有静态资产路由**(`view-service/view_service.py:556-566`)
`do_GET` 只认 `/`、`/index.html`、`/events`、`/status`、`/tasks`,**其余一律 `send_error(404)`**。
⇒ 要外挂 `.js`/`.css`,必须**新增路由**(Python 侧)**＋ nginx 侧路由**(生产侧,由 prodops 出单),
否则只剩"内联进 HTML"这一条路。

**3️⃣ console 页面是 Go 原始字符串,语法上不能含反引号**(`internal/m8/console_auth.go:254`
`const consolePageHTML = ` + 反引号起始的 raw string;该文件共 606 行)
⇒ 🔴 **含反引号的 bundle 无法内联进 console**。今天实测各候选入口 bundle 的反引号数:

| 候选 | 反引号数 | 能否内联进 Go raw string |
|---|---|---|
| DOMPurify 3.4.15 | **0** | ✅ 唯一可直接内联的 |
| Vditor `index.min.js` | 39 | ❌ |
| EasyMDE `easymde.min.js` | 73 | ❌ |
| Toast UI `toastui-editor.js` | 178 | ❌ |
| Editor.js `editorjs.umd.js` | 184 | ❌ |
| marked `marked.umd.js` | 229 | ❌ |
| Cherry `cherry-markdown.core.js` | **1780** | ❌ |
| markdown-it `umd.min.js` | 1686 | ❌ |
| react-md-editor `dist/mdeditor.js` | **8627** | ❌ |

⇒ 若编辑器挂在 console 侧(I 项的挂载点是 `console_platform.go` 的 builtin 项 ＋ `renderPlatform`,
板件 2044 段),**要么走独立资产路由(见 2️⃣),要么把该常量改成引号串并处理转义**(改产品代码)。

**4️⃣ 发布巡检硬钉 renderer 哈希**(`deploy/invariants-dingwei-gray.json:16`,desc 自述"当日最贵的那条断言")
- 内联进 `renderer-canary-sse.html` ⇒ 哈希变(发版时更新 `want=` 是**既定流程**,板件已记为发布阻断项)。
- 🔴 但**新增的独立资产文件目前没有任何巡检钉住** ⇒ 它会**静默漂移**。
  🔵 建议(报而不碰):若走独立资产,**同时新增一条 pin**,否则等于把"最贵的那条断言"绕过去。

**5️⃣ 产品里已经有一套自研 markdown 渲染**(`view-service/renderer-canary-sse.html:3533-3731`,约 200 行:
`renderMarkdown` → `renderMarkdownText` → `renderTable`/`renderList` → `renderInline` → `renderCode`/`highlightCode` → `wireCopyButtons`;
文件 sha256 `9b5656f4505c16bd…`,分支 tip `e6ca14a0`)
⇒ 若引入的组件**同时管展示**,产品里就有**两套 markdown 事实源**;
而 I 项的既定纪律是「正文唯一源 …**不搬、不复制**(防第二事实源)」(板件 2044-2050 段)。
🔵 这不是"选哪个组件"的问题,是**要不要让组件接管展示**的问题 —— 需要组长裁。

**6️⃣ 🔴🔴 图片口径已量清,而它有一道**选型救不了的坎****

| 事实 | 锚 |
|---|---|
| 上传 `POST /files/upload` 返回 `{"url": PublicBaseURL + "/f/" + ObjectKey, …}`,HTTP **201** | `internal/fileservice/http.go:82-88` |
| `/f/` 由 nginx 直接 `alias` 出,**内联白名单含** `jpg png gif webp avif heic bmp tiff mp4 webm … txt md csv json log py js go …` | `deploy/nginx-dingwei.conf:70-103` |
| ⇒ `<img src="https://…/f/….png">` **能直接显示**,不需要新造上传 ✅ | 同上 |
| 🔴 `/files/ref/` 那条**强制** `Content-Disposition: attachment` ⇒ **不能用它做图片预览**(会触发下载) | `internal/fileservice/http.go:235` |
| 🔴🔴 **保留期只有 7 天**:`DefaultRetention = 7 * 24 * time.Hour`,且有**契约测试钉住** | `internal/filepolicy/policy.go:7` · `policy_test.go:255-258` |

⇒ **spec 正文里嵌的图片,7 天后就是死链。** spec 是**长期文档**,这与 7 天保留期**直接冲突**。
🔴 **换任何一个编辑器都解决不了这一条。** 必须先裁(见 §七-①)。

---

## 三 主对照表(🔬 = 今天实测的数)

> 列按组长 ts-No.75 指定的六项。体积一律是 **`gzip -9` 后的字节**(不是 npm 报的 unpackedSize)。

| 候选(版本) | 许可证<br>🔬 registry 字段 | 🔬 最近发布 | 🔬 体积(gz) | 需改构建链? | 🔴 能否**完全自托管离线** | 导入文件 / 图片预览 | 与本仓耦合面 |
|---|---|---|---|---|---|---|---|
| **EasyMDE** 2.21.0 | MIT | **2026-05-03** | js **104KB** + css 3KB<br>(`dist/easymde.min.js` 320KB raw;dist 仅 0.3MB/2 文件) | **否**(UMD,`<script>` 直用;deps 5 已打包) | ⚠️ **默认不离线**:拼写检查开启时运行时 XHR `cdn.jsdelivr.net/…/en_US.aff\|.dic` 🔬(bundle 内实测)⇒ **必须 `spellChecker:false`** 或自托管词典;关掉后**无其它外网依赖** | 图片:✅ 拖拽/粘贴/文件选择器**全走 `imageUploadFunction(file,onSuccess,onError)`** 🔬(`src/js/easymde.js:1957/2007/2431`)—— **纯客户端回调,服务端 0 改动**,正好接我们的 `/files/upload`<br>🔴 非图片附件:**无** | **最小**:1 个 `<script>`+1 个 `<link>`(需 2️⃣ 新增静态路由)· 上传 0 处服务端改动 · 暗色需**自写 CSS**(见 §四) |
| **Vditor** 4.0.0 | MIT | **2026-08-30** | js **69KB**(`dist/index.min.js` 290KB raw)+ css 9KB<br>🔴 **但引擎另算**:`dist/js/lute/lute.min.js` **578KB gz**(3.6MB raw)<br>⇒ 最小可用 **≈656KB gz**;全功能 dist **21.9MB/530 文件** 🔬 | **否**(UMD;deps 1) | ⚠️ **默认不离线**:bundle 内实测 `CDN="https://unpkg.com/vditor@4.0.0"`,README:220 亦写明默认值 ⇒ **必须设 `options.cdn` 指向自托管路径**;🔵 好消息:**所有资产包内自带**(mathjax 6.5M · lute 3.6M · mermaid 3.5M · highlight.js 2.1M · graphviz 2.0M · katex 1.5M · echarts 1.0M · markmap 840K 🔬)⇒ 自托管**做得到**,代价是体积 | 图片:✅ `upload{url,max,linkToImgUrl,token,headers,fieldName,multiple,accept,…}` 🔬(`dist/types/index.d.ts:386-425`)<br>🔴 **但它期望自己的响应体 `msg.data.succMap`** 🔬(bundle 实测),而我们返回 `{url,name,size,sha256,…}` ⇒ **要一层适配**(服务端 shim 或客户端拦截)<br>附件:✅ `accept` 可配 | **中**:静态路由(2️⃣)· **上传适配 1 处** · 暗色 ✅ 有 `setTheme("dark"\|"classic")` 🔬(`dist/index.d.ts`)· 🔴 **默认 `cache.enable=true`**(types 注释"默认值: true" + bundle `cache:{enable:!0` 🔬)⇒ **默认往 localStorage 写草稿,必须显式关**(见 §四) |
| **Cherry Markdown** 0.11.10 | **Apache-2.0** | **2026-08-24** | 🔴 **包内无 `.min.js`**:`core.js` 2.26MB raw/**591KB gz**;`cherry-markdown.js` 12.7MB raw/**2.3MB gz** 🔬;css(min)40KB gz<br>dist 合计 **47.9MB/241 文件** | **否**(UMD 实测:`core.js` 头部 `define.amd … globalThis.Cherry`)<br>🔴 但**要自己压缩**才能上生产 ⇒ 那**就是**一步构建 | ✅ **包内零 CDN 引用** 🔬(`cdnrefs=0`) | 🔵 **口径最贴合**:`fileUpload: CherryFileUploadHandler` = `(file, callback) => …`**纯客户端回调** 🔬(`types/cherry.d.ts:81/135/890`)+ `fileUploadMulti` + `urlProcessor(url, srcType)` + `fileTypeLimitMap{video,audio,image,word,pdf,file}` 🔬 ⇒ **图片与非图片附件都有原生位**,且**不需要新造上传** | **中偏大**:静态路由(2️⃣)· 上传 0 处服务端改动 ✅ · 🔴 **反引号 1780** ⇒ console 内联不可行(3️⃣)· 暗色能力**未验**(§八-3) |
| **Editor.js** 2.31.7 | Apache-2.0 | **2026-09-17** | js **65KB**(`dist/editorjs.umd.js` 239KB raw) | 否(UMD;deps 3) | ✅ `cdnrefs=0` 🔬 | 🔴 **模型不对**:输出是 **block JSON,不是 markdown**;要 markdown 得自己写序列化器 ⇒ **等于自研**,违背"不自研" | 不适用(见 §四) |
| **展示侧组合**:marked 18.0.13 / markdown-it 15.0.2 / DOMPurify 3.4.15 | MIT / MIT / **(MPL-2.0 OR Apache-2.0)** | 2026-09-12 / 09-11 / 09-06 | marked **14KB** gz · markdown-it **47KB** gz · DOMPurify **11KB** gz 🔬 | 否(三者均 UMD) | ✅ 三者 `cdnrefs=0` 🔬 | 🔴 **只展示,不含编辑**(没有编辑 UI、没有导入、没有图片上传位) | 若只替掉自研渲染(5️⃣),这是**最轻**的一条;但**它不满足业主"编辑"那半** |
| `md-editor-v3` 7.0.0 | MIT | 2026-09-16 | 🔬 **无浏览器 bundle**(仅 `lib/es`、`lib/cjs`) | 🔴 **是**:`peerDependencies: vue` + deps 22 | — | — | **淘汰**(1️⃣:仓内无 Vue、无构建链) |
| `@uiw/react-md-editor` 4.1.2 | MIT | 2026-08-21 | `dist/mdeditor.js` **595KB** gz 🔬 | 🔴 **是**:`peer: react, react-dom` | — | — | **淘汰**(1️⃣) |
| `@milkdown/kit` 7.22.1 | MIT | 2026-08-12 | ESM,**deps 21** 🔬 | 🔴 **是**(纯 ESM 组装) | — | — | **淘汰**(1️⃣);🔵 且 `milkdown` 元包**没有 `latest` dist-tag** 🔬(实测 `latest=None`)⇒ `npm i milkdown` 拿不到可用版本,是个坑 |
| `@tiptap/core` 3.31.3 | MIT | 2026-09-04 | — | 🔴 是(ESM 组装,`peer: @tiptap/pm`) | — | — | **富文本(ProseMirror),markdown 靠扩展组装** ⇒ 违背"成品/不自研" |
| `ckeditor5` 48.5.1 | 🔴 **`SEE LICENSE IN LICENSE.md`**(非 SPDX)· deps **60** | 2026-09-16 | — | 是 | — | — | **淘汰**:许可证**不能只凭 registry 字段判商用**(§八-4/5) |
| `tinymce` 8.9.1 | 🔴 **`SEE LICENSE IN license.md`**(非 SPDX) | 2026-09-09 | — | 是 | — | — | **淘汰**:同上;且富文本非 markdown 原生 |

---

## 四 🔴 每个候选**做不到什么**(阴格)

> 组长判据:「**只有优点的候选说明你没查够**」。以下每条都带证据出处。

### EasyMDE 2.21.0
1. 🔴 **默认配置会打外网**:`spellChecker !== false` 即启用 🔬(`src/js/easymde.js:2153/2164`),启用后运行时
   `XMLHttpRequest → https://cdn.jsdelivr.net/codemirror.spell-checker/latest/en_US.aff|.dic` 🔬(bundle 内实测两处)。
   ⇒ 内网侧**必须显式 `spellChecker:false`**(或自托管词典)。**忘记关 = 每次编辑都发一个失败的外网请求。**
2. 🔴 **没有所见即所得**:只有"编辑 + 分屏预览"。业主若要 Word 式体验,它给不了。
3. 🔴 **没有自带暗色**:`dist` 只有 `easymde.min.css`(3KB gz)一份浅色样式 ⇒ **暗色要我们自己写 CSS**,
   而本仓刚做完主题全局化(`data-dw-theme`),**这块 CSS 得跟着主题走**(§八-3:**未验**)。
4. 🔴 **附件只支持图片**:上传位只接图片(拖拽/粘贴/选择器三条路都只走 `imageUploadFunction`)⇒
   spec 里若要挂 `.md`/`.pdf` 附件,**没有原生位**。
5. 🔵 **基于 CodeMirror 5**(打包在内),不是 CM6 ⇒ 大文档性能与可扩展性弱于 CM6 系;
   但**这正是它"零构建链"的原因**,不是纯粹的缺点。
6. 🔴 **导入文件 = 没有 UI**:只能我们自己在外面读文件后 `value(...)` 塞进去。

### Vditor 4.0.0
1. 🔴 **默认 `cdn` 指向 unpkg**:`CDN="https://unpkg.com/vditor@4.0.0"` 🔬(bundle 实测)+ README:220「配置自建 CDN 地址」。
   ⇒ 不改 `options.cdn`,**内网侧所有懒加载资产(含 lute 引擎)全部取不到 ⇒ 编辑器直接不工作**。
2. 🔴 **"69KB"是假象**:编辑器壳 69KB gz,但**渲染引擎 lute 另需 578KB gz**(运行时从 `options.cdn` 加载)
   ⇒ **最小可用 ≈656KB gz**;开数学/图表则再加 katex 1.5M、mermaid 3.5M、mathjax 6.5M(**未压缩体积** 🔬)。
3. 🔴 **默认往 localStorage 写草稿**:`cache.enable` 默认 **true**(types 注释逐字「默认值: true」+ bundle `cache:{enable:!0` 🔬)。
   ⇒ 与 I 项「正文唯一源 …不搬、不复制(防第二事实源)」**直接冲突**:
   **浏览器里会悄悄存在第二份 spec 正文**,而且它比服务端新。🔵 **必须显式关掉**(或明确定义它与唯一源的优先级)。
4. 🔴 **上传响应体不匹配**:它读 `msg.data.succMap` 🔬(bundle 实测),我们返回 `{url,name,size,sha256,media_type,expires_at,range}`(HTTP 201)
   ⇒ **要一层适配**(服务端 shim 或客户端拦截),这是**实打实的一处改动**。
5. 🔵 **默认 `mode:'wysiwyg'`**(types 注释)⇒ 若 spec 编辑要"所见即源码",得显式改 `mode:'sv'|'ir'`。
6. 🔴 **反引号 39** ⇒ 内联进 console 的 Go raw string **不可行**(§二-3)。

### Cherry Markdown 0.11.10
1. 🔴 **npm 包不发压缩版**:dist 里只有 `.min.css`,**没有任何 `.min.js`** 🔬(实测 `ls | grep min` 只出 CSS)。
   ⇒ 要上生产**必须自己压缩**,而"自己压缩"= **引入一步构建**(与 1️⃣「仓内零构建链」冲突),
   或者直接吞 **591KB gz(core)/2.3MB gz(full)未压缩产物**。
2. 🔴 **体积是三者最大**:dist 合计 **47.9MB/241 文件** 🔬;`deps 12` 🔬。
3. 🔴 **反引号 1780** ⇒ console 内联**完全不可行**(§二-3),必须走独立资产路由(§二-2)。
4. 🔵 **版本号 0.x**(0.11.10)⇒ 按语义化版本,**API 未承诺稳定**;长期维护是赌注(发布很勤,2026-08-24)。
5. 🔴 **暗色/主题联动未验**(§八-3)。
6. 🔵 **它是腾讯开源件** 🌐 —— 与本仓托管方同源,生态上是加分,**但这不是技术判据,我没让它影响排序**。

### Editor.js 2.31.7(列为对照,不推荐)
1. 🔴 **根本不是 markdown 编辑器**:输出 block JSON。要 markdown 得自己写双向序列化 ⇒ **等于自研**,违背"不自研"。
2. 🔵 优点是三者里最小的编辑壳(65KB gz)、零 CDN 引用 —— **但模型不对,优点不成立**。

### 展示侧组合(marked / markdown-it / DOMPurify)
1. 🔴 **没有编辑能力**:业主原话要的是「markdown **编辑展示**组件」⇒ 这一条**只覆盖"展示"半**。
2. 🔵 若采用,产品里会**同时存在**自研 `renderMarkdown`(:3533-3731)与第三方渲染 ⇒ **必须先裁 5️⃣**(谁接管展示),
   否则就是**第二事实源**,正是 I 项纪律要防的东西。
3. 🔴 **DOMPurify 的许可证是 `(MPL-2.0 OR Apache-2.0)`** —— 双许可,选 Apache-2.0 侧可商用;
   🔵 但**这是读 registry 字段,不是法务结论**(§八-4/5)。
4. 🔴 **marked/markdown-it 都不做消毒**:引入它们**必须同时引入 DOMPurify**,否则是 XSS 面扩大
   (现有自研渲染是否消毒,**我这次没查** —— 见 §八-11)。

---

## 五 已淘汰(依据一律是 🔬 **实测的 npm 最近发布日**,不是印象)

> 组长判据:「已停维护/近一年无发布的**直接标注并淘汰**」。今天 = 2026-09-21 ⇒ 线 = **2025-09-21**。

| 候选 | 🔬 最近发布 | 距今 | 处置 |
|---|---|---|---|
| `@toast-ui/editor` | **2023-02-17**(3.2.2) | **3 年 7 个月** | 🔴 **淘汰**。(🌐 佐证:上游 issue「tui.editor is No Longer Maintained」#3297 —— **仅佐证,判据是发布日**)🔵 顺带量了它的体积供参考:`toastui-editor.js` 940KB raw/190KB gz、css 106KB gz、**dist 内也无 `.min.js`** |
| `tui-editor`(旧名) | 2019-12-12 | 6 年 9 个月 | 🔴 淘汰(同一件的历史名) |
| `bytemd` | **2025-02-12**(1.22.0) | 19 个月 | 🔴 淘汰(差 7 个月才够一年线,但**已过线**;Svelte 绑定,还需构建链) |
| `simplemde` | 2016-06-14 | 10 年 | 🔴 淘汰(EasyMDE 就是它的续命 fork) |
| `hypermd` | 2018-10-07 | 7 年 11 个月 | 🔴 淘汰 |
| `stackedit-js` | 2018-08-21 | 8 年 | 🔴 淘汰;🔵 且**上游 StackEdit 应用的许可证我没核实**(npm 包标 MIT),**不要凭这一栏用** |
| `editormd` | — | — | 🔴 **npm 上取不到**(实测 HTTP 404)⇒ 无从评估 |
| `codemirror`(6 元包) | 2025-06-19 | 15 个月 | 🔵 **组装路线,不是成品**(且元包已过一年线;`@codemirror/lang-markdown` 6.5.2 是 2026-08-04,活的)⇒ 按"不自研"出局 |
| `prosemirror-markdown` 1.13.7 | 2026-08-31 | 活 | 🔵 同上:**是积木不是成品**(deps 3,需自己组装编辑器)⇒ 出局 |

---

## 六 选定后**要改哪几处**(📖 读码推出的清单,🔴 **不是改过一遍数出来的**)

| # | 改动点 | 锚 | EasyMDE | Vditor | Cherry |
|---|---|---|---|---|---|
| A | **资产落地方式**:内联 vs 新增静态路由 | §二-2/3 | 必须**新增路由**(反引号 73) | 必须**新增路由**(反引号 39) | 必须**新增路由**(反引号 1780) |
| B | `view_service.py` 新增静态路由 + 白名单 | `view_service.py:556-566` | 1 处 | 1 处 | 1 处 |
| C | nginx 路由(**生产侧,prodops 出单**) | `deploy/nginx-dingwei.conf` | 1 处 | 1 处 | 1 处 |
| D | **上传适配** | `fileservice/http.go:82-88` | **0 处**(客户端回调) | 🔴 **1 处**(响应体 `succMap` 适配) | **0 处**(客户端回调) |
| E | **关掉默认的外网/本地副作用** | §四 | `spellChecker:false` 1 处 | `cdn` + `cache.enable:false` **2 处** | 0 处(零 CDN) |
| F | **暗色对齐** `data-dw-theme` | 主题全局化那格 | 🔴 **自写 CSS**(无自带暗色) | ✅ `setTheme('dark')`(读 d.ts,**未实跑**) | 🔴 **未验** |
| G | **自研 markdown 的去留**(防第二事实源) | `renderer-canary-sse.html:3533-3731` | 编辑侧不影响展示 ⇒ **可不动** | 同 | 同 |
| H | invariants 新增资产 pin(🔵 建议) | `invariants-dingwei-gray.json:16` 同族 | 1 条 | 1 条 | 1 条 |
| I | console 侧挂载(I 项挂 `renderPlatform`) | 板件 2044 段 | 视 A 而定 | 视 A 而定 | 视 A 而定 |

🔵 **读法**:**A/B/C 三家一样**(都要新增静态路由 —— 因为本仓根本没有静态资产位),
**真正的差别只在 D/E/F 三行**:Cherry 与 EasyMDE 在"上传"上零服务端改动,
Vditor 多一处适配、且多两处"必须关掉的默认行为"。

---

## 七 🔴 三个必须先裁的问题(选型救不了)

**① 图片 7 天就死 —— 这是产品/基建决定**
`DefaultRetention = 7*24h`(`internal/filepolicy/policy.go:7`,契约测试 `policy_test.go:255-258` 钉住)。
spec 是长期文档 ⇒ **正文里嵌的图 7 天后全变死链**。可选:
  (a) 给"spec 内嵌图"开一条**长保留**的存储口径(需业主授权,涉及基建)
  (b) 图片**随 spec 版本一起入库**(与"正文唯一源"一致,但要新存储)
  (c) **本期先不做图片内嵌**,只做纯文本 markdown 编辑
🔵 **我倾向先按 (c) 开工、把 (a)/(b) 交业主裁** —— 因为 (a)(b) 都动基建,而 I 项的北极星是"多-spec 运行时管理与页面"。
🔴 **但这是建议,不是我的权限。**

**② 要不要让组件接管"展示"** —— 若接管,自研 `renderMarkdown`(:3533-3731)退役,**渲染结果会变**(业主看得见的观感变化);
若不接管,产品里长期两套 markdown。🔵 与 5️⃣ 同源,请裁。

**③ 编辑器挂哪一侧** —— console(Go raw string,反引号约束)还是独立页(view-service,无静态路由)。
挂载点不同,A/B/C/I 四处改动的形状就不同。板件说 I 复用 `console_platform.go` 的 builtin 项 ＋ `renderPlatform`,
🔵 但那格是"编组入口"的形状(点击开 pane),**编辑器是不是也走 pane,板件没写** ⇒ 请裁。

---

## 八 🔴 没验到什么

1. 🔴 **一个候选都没在浏览器里跑过。** 所有数字是**包级**实测(registry 元数据 + tarball 内文件 `gzip -9` + `grep`)。
   **渲染保真度、编辑手感、性能、与现有 CSS 的冲突、初始化是否报错 —— 一律未验。**
2. 🔴 **没验候选的 markdown 解析与自研 `renderMarkdown` 是否一致。** 产品里还有
   `mergeMetaIntoMarkdown`(`renderer-canary-sse.html:3400`)与超长摘要路径(`:3158`/`:3197`),
   换渲染器可能让这些**行为漂移**。⇒ 真要选,**下一步应该是一格"同一份 spec 正文,三种渲染器逐字比对"的实验**。
3. 🔴 **暗色一律没实跑。** `Vditor.setTheme('dark'|'classic')` 是**读 `.d.ts` 得出的**(存在这个 API ≠ 效果可接受);
   Cherry / EasyMDE 的暗色能力**连读码证据都不完整**(只确认 EasyMDE 包内没有第二份主题 CSS)。
4. 🔴 **许可证只取了 registry 字段,没做法务审查。**
5. 🔴 **"能否商用"这一列我没有权威依据** —— MIT/Apache-2.0 通常可商用,但那是**通识判断,不是法务结论**;
   `ckeditor5`/`tinymce` 的 `SEE LICENSE IN …` 是**非 SPDX**,我**不能**替法务解读。
6. 🔴 **体积是 `gzip -9` 单文件字节**,不是"页面首屏实际传输量":未算按需加载、HTTP/2、字体/图标、
   也未排除 `.map`(cherry 的 `cherry-markdown.js.map` 就躺在 dist 里,会占仓但不该上线)。
7. 🔴 **淘汰依据是 npm 最近发布日期(实测),不是"上游是否还在修安全问题"。**
   GitHub 活跃度、CVE 响应速度**未逐个核**(只有 toast-ui 一条有 🌐 佐证)。
8. 🔴 **"完全自托管离线"我验的是"包内自带资产 + 包内无 CDN 串"**,
   **没有断网实跑**证明它运行时不去外网取别的东西(字体、图标 sprite、语言包、Web Worker)。
   ⇒ **Vditor/Cherry 的"✅/⚠️"是包级证据,不是运行时证据。**
9. 🔵 **没做任何 PoC 集成**(组长明令不引入、不装依赖)⇒ §六"要改几处"是**读码清单**,不是改过一遍数出来的。
10. 🔵 **7 天保留期那条我只读了默认值与契约测试**,没有实测"7 天后 `/f/` 真的 404"
    (生产侧行为,**我不碰生产**)。
11. 🔴 **我没查现有自研 `renderMarkdown` 是否做 HTML 消毒** —— 那是"引入 marked/markdown-it 必须同时引入 DOMPurify"
    这句的**前提缺口**:如果自研那套本来就没消毒,那问题比选型大,而且**与本次调研无关但更该报**。
    ⇒ 🔵 **建议单开一格只读诊断**(不占本件)。
12. 🔵 **业主的验收口径我没拿到原文**:我只看到组长转述的「最好使用一套成熟的markdown编辑展示组件或开源成品处理」。
    "最好"是**倾向**还是**硬要求**、"编辑展示"是**一个组件管两头**还是**可以两个组件分管**,
    ⇒ **这两点会直接改变排序**(尤其影响 §七-②)。

---

## 九 板件待贴片段(逐字;按 ts-No.「三件回齐」⑤ 裁定,**由组长自己贴,我不动板件**)

```text
## ✅ ts-No.75 markdown 编辑组件选型调研 —— 交付(qwen38maxkhovvob4,只调研未引入)

调研件 `runbooks/RESEARCH-MARKDOWN-EDITOR-0.2.1.50-qwen38maxkhovvob4.md`(锚 <本次提交>)。
🔴 **仓内 package.json 仍为 0 个**,未引入/未装/未改产品/未碰发布线。

三家可用(体积一律 gzip -9 实测):EasyMDE 2.21.0(MIT,104KB)· Vditor 4.0.0(MIT,69KB+引擎 578KB)·
Cherry Markdown 0.11.10(Apache-2.0,包内**无 .min.js**,core 591KB)。
淘汰(判据=实测 npm 最近发布日,线=2025-09-21):toast-ui 2023-02 · bytemd 2025-02 · simplemde 2016 ·
hypermd/stackedit-js 2018 · tui-editor 2019 · editormd(npm 404);
框架绑定出局:md-editor-v3(Vue)· react-md-editor(React)· milkdown(ESM,deps 21,且元包无 latest tag)。

🔴 **本仓六道硬约束(与候选无关,换谁都要过)**:
  1 仓内**零构建链**(package.json 0 个)
  2 `view_service.py:556-566` **没有静态资产路由**(只认 5 条路径,其余 404)
  3 `console_auth.go:254` console 页是 **Go 原始字符串 ⇒ 不能含反引号**;
    实测反引号:DOMPurify 0 · Vditor 39 · EasyMDE 73 · Cherry **1780** ⇒ **三家都无法内联进 console**
  4 `invariants-dingwei-gray.json:16` 钉 renderer 哈希;**新增独立资产目前无人钉** ⇒ 建议补 pin
  5 产品已有自研 markdown(`renderer-canary-sse.html:3533-3731`,约 200 行)⇒ 组件若接管展示 = **第二事实源**
  6 🔴🔴 **图片直链只活 7 天**:`filepolicy/policy.go:7 DefaultRetention=7*24h`(契约测试钉住);
    `/files/ref/` 强制 attachment **不能做预览**,只有 nginx `/f/` 白名单那条能内联显示

🔴 **待裁三问(选型救不了)**:① 图片 7 天死链怎么办(长保留/随版本入库/本期不做内嵌)
② 要不要让组件接管"展示"(退役自研渲染 = 业主看得见的观感变化)③ 编辑器挂 console 侧还是独立页

🔴 **本件没有任何浏览器实跑证据**(全部是包级实测 + 读码);"完全自托管离线"验的是
**包内自带资产 + 包内无 CDN 串**,**没有断网实跑** ⇒ 详见调研件 §八 十二条"没验到什么"。
🔵 附带报一条**与本件无关但更该报**的:我**没查**现有自研 renderMarkdown 是否做 HTML 消毒
  ⇒ 建议单开一格只读诊断。
```

---

## 十 同批交付:㉒ L9「通过文本写盲区」补丁(组长 ts-No.「三件回齐」④,非可选)

**已落地并实跑验证**:`view-service/renderer-scroll-follow-regression.js` 的 L9,
**通过文本现在自己打印四条盲区**(不再只写在失败文本与交付件里)。

    实跑   通过 9 / 失败 0 · **rc=0**(实测,不是推断:rc 由外层 shell 记进日志)
    打印   ① 只比"已登记"12 项,登记集外的差别与抖动同量级、不可区分
           ② 抖动对照只覆盖 3 腿(A1/A2/C1)不是 8 腿;阴格命中时**先怀疑登记集**
           ③ 只对照了 (i) 一个修法,v2a/v2b/v3 没做逐项对照
           ④ 结论只及于本进程这份基线字节;锚点过期 ⇒ buildVariantI() **报错**(本件会红,别当产品回归)

🔵 顺带修了一处**我自己写错的坐标**:原通过文本引"交付件第四节",实际撤销依据与作废条件在 **§5.1** ⇒ 已改。
(与"跨席给坐标必须带锚"同源:**引自己的文档也会引错**。)
