# A 线交付件 · 0.2.0.6 view 文件上传与按类型显示 · `opus50tc2k`

- **基线**：`27abfcf032db271a414eeea9ed08685c81b16a12`（附录 B1 = H 发版锚 = 现网跑的那一版）
- **契约读到**：`3a453e4f867d5d2f37416613e622c4fde867846f`（附录 D 为 B 线归档/清理裁定，对 A 线无影响）
- 上一版读到：`094dfce98b3d38934694aace1de8fd2845f705c0`（附录 A 六项 · **附录 B2 改裁为 (b′)** · 附录 C · 硬规丙）
- **交付分支**：`opus50tc2k/a-0.2.0.6-file-render`
- 🔴 **未合并、未推 `tasks/v0.1-B` 的产品代码** —— `stagegate 0.2.0.6` 仍红。
- 本件为第二版：第一版按原裁定 (b) 的扩展名分派实现；组行改裁 (b′) 后已按 (b′) 重写，并保留降级分支。

## 1. 改了什么（只动契约允许的那一个产品文件）

`view-service/renderer-canary-sse.html`。

**① 上传入口（§3.1）**：粘贴由「只接 `image/*`」扩到**任意类型且一次多个**；新增**可见的文件选择按钮**
`📎 文件`（`<input type=file multiple>`，复用了源码里早就写好却没人用的 `.file-label` 样式）；新增**整页拖放**，
一次拖入多个全收。单个上限 `52428800`（附录 A4）。多文件顺序上传，**一个失败不打断其余**
（票据一次一张、按 `max_bytes` 绑定本次大小，并发会互相挤掉）。

**② 正文区分派（§3.2 · 附录 A5 · 附录 B2 (b′)）**：扫正文中本站 `/f/` 直链，按**三级依据**定类型：

| 级 | 依据 | 何时用 | 标记 |
|---|---|---|---|
| ① | 上传响应的 `media_type` | 本页本次上传，手上就有 §2 四字段 | `data-dispatch="upload"` |
| ② | **HEAD 读服务端声明的 `Content-Type` / `Content-Disposition`** | 收到的消息（接收方没见过上传响应） | `data-dispatch="head"` |
| ③ | 🔴 **降级**：URL 扩展名（附录 A5 白名单反向映射） | HEAD 被拒 / 网络出错 | `data-dispatch="ext"` |
| — | HEAD 返回 404 ⇒ 文件已过 7 天保留期被清理 | 如实说「已不可取」，不画坏图 | `data-dispatch="gone"` |

分派结果：`image/*`→`<img>`、`video/*`→`<video controls>`、`application/pdf`→`<iframe>`、
`text/plain`→`<pre>`（超限截断＋下载链接）、**其它一切及 `attachment` ⇒ 下载链接**。

🔴 **降级分支为什么在**：组长明示 nginx `limit_except GET` 放行 `HEAD` **只从文档知道、未实测**
（已派 `prodops` 在 H 收尾实测）。实测通过前这条分支必须存在 —— 本件用例 ④c 专门测它。

附件区挂在**卡片**上而不是正文里 —— 正文每个流式 chunk 都重写 `innerHTML`，挂正文里会让播放中的 `<video>` 被反复重建。

## 2. 🔴 我自选的假设（组长要求逐条列出）

| # | 假设 | 为什么这么选 | 错了的代价 |
|---|---|---|---|
| S1 | 文本内联上限 **262144 字节（256 KiB）** | §3.2 只写「要有上限」没给数 | 改一个常量 |
| S2 | 降级用的扩展名表 = A5 白名单反向映射，`jpg/jpeg` 等别名是我补的；表外（含 `.bin`）→下载链接 | B2 要求「同一张表的反向映射」 | 仅影响 HEAD 不可用时；落入下载兜底，不报错 |
| S3 | 只认**与页面同源**且路径以 `/f/` 开头的 URL | 附录 A1 前缀；不对外站 URL 发任何请求（含 HEAD） | 直链换 host 需同步放宽 |
| S4 | 上传响应**必须**有绝对 https `url`，**不回落**旧 `file_ref` | §2 定的是 url；回落会掩盖 B 线未改完 | B 上线前上传必失败，见 §5 |
| S5 | 浏览器给不出 MIME 时按 `application/octet-stream` 上传，不按文件名猜 | §2「不靠扩展名猜」的原始含义 | 无 |
| S6 | 截断判定取 `Content-Range` 的 `end+1 < total`，**不以「回了 206」为准** | 见 §4 | 无 |
| S7 | 内联失败（图片加载错 / 视频解不了码 / 无 PDF 阅读器）一律降级为下载链接 | 业主「不能显示的，直接给出下载链接」 | 无 |
| S8 | HEAD 非 404 的失败（405/403/网络错）一律走扩展名降级；**只有 404 判为「已清理」** | 404 是「确实没有」，其余是「问不到」，两者表现不该相同 | 若 nginx 对缺件返 403，会被当成降级而非过期 |
| S9 | 同一 URL 全页只发一次 HEAD（Promise 级缓存） | 流式重渲每 chunk 都会重扫正文 | 无 |

## 3. 证据（值，不给结论）

### 3.1 交付物哈希

    d4fd2abc69ee214b0dfb82c0b852cdf8482c093272c4e3f4441d4cd86251711c  view-service/renderer-canary-sse.html
    54deca9839f99e1ec9585c6c2a0e7eff904e96848ede541fad96ba3f51d3db5c  view-service/renderer-0206-file-render-regression.js
    2f4757ce8568c7b4745cbffc58f78dee4339861856addeac33d252841237fe15  view-service/renderer-0206-contract-mock.py
    07d5515ba72605e237d1a745f34dc28cc8d5b8dc17ebe0c4db38087156347032  view-service/renderer-t147-paste-regression.js

### 3.2 功能级验收：真浏览器、真文件、真上传（13/13）

    ①  上传入口在界面上可见（文件选择按钮），不是只有隐藏的 paste 监听器
    ②  文件选择一次传 6 个不同类型（图片/视频/pdf/文本/office/未知二进制）
    ③  正文区渲染：图片 <img>、视频 <video>、pdf <iframe>、文本 <pre> 内联
    ④  兜底：office 与未知二进制渲染成下载链接（<a download>），不报错也不留裸 URL
    ④a 接收方只拿到 URL（刷新后 hints 清空）⇒ 靠 HEAD 读服务端声明分派      ← (b′) 的接收方那一半
    ④c HEAD 被拒（405）时按 URL 扩展名降级分派，且标出「已降级」            ← 降级分支
    ④d 文件已被清理（HEAD 404）时给出「已不可取」，不是坏掉的 <img> 也不是裸 URL ← 过期链接
    ④e 扩展名与 Content-Type 【故意不一致】时以服务端声明的头为准          ← 唯一能分辨 (b)/(b′) 的用例
    ④b mp4（本 chromium 无 h264 解码器）降级为下载链接，不报错、不留裸 URL
    ⑤  拖放一次拖入 2 个文件，两个都上传
    ⑥  粘贴一个 pdf（非图片）也上传 —— 原实现只接 image/*
    ⑦  大文本内联截断并给下载链接（不是整篇灌进正文）
    ⑧  超过 50 MiB（52428801 字节）在发请求前拒绝，不发上传请求

单元级 `renderer-t147-paste-regression.js` 7/7（从产品源码抽取函数，不复制实现）；
其中上限值 `52428800` 是**从源码常量读出来断言**的，防「测试写死、产品写别的数」。

截图在 `runbooks/evidence-A-0.2.0.6/`：

    fd64df677d4ab57fa1a76dbe7bad16b48831e7dde635bd190b0cb47d2c58deb6  01-upload-entry-visible.png
    2ddb5852eca7c53a34a1eef19e9c679b0c775ba9738cf8eb64fa1ec6d697bf9e  02-body-rendered-all-types.png
    6d6a984875392ed6c1954d0c488528795a9bccfd35ae8e0616d308577c9c8c70  02a-inline-image.png
    1556ccbf5bd1eba16fd1888fbde52391c7876b3ee4d45cea8fd888f4f5d5e1e1  02a-inline-video.png
    c418fd41f2f0249d05e955a83869907297b771a58061bbbd5a0bd1deca4ca840  02a-inline-pdf.png
    b866200c0f26d5effa2dfaefb88daa03253e6fb8526de0a3ef3cc830d14ee654  02b-receiver-side-by-head.png
    368237b96b8143cfc65ed997cf8719dbe6008b277e846e1eb474877d10c00bba  02c-degraded-by-extension.png
    6c963e079a6e6d65c6c0503940d2166310fdaf95d9908bb4d03a6f334aad9b48  02d-header-beats-extension.png
    5565785e15d713ac6ba240381f2420bc1fdb4341888020d30d44107eb4532853  03-large-text-truncated.png

### 3.3 回归对照（改后 vs 未改的基线 `27abfcf0`，同环境同命令）

    node 组 6/6 PASS（t086 / t138 / t141 / t145 / t146 / t147）
    浏览器组 PASS：g2 · failcard · page-identity · provenance-icon · t054 · t074
    浏览器组 FAIL：renderer-regression · longtext · sse-smoke · sse-reconnect · c2

🔴 **后五项在【未改动的基线上逐项同样 FAIL】**（c2 缺 `renderer-c2.html`；其余缺本机 fixture 服务），
即与本次改动无关。基线副本 `/tmp/baseline/view-service`，可复核。

### 3.4 样本

`view-service/fixtures-0206/`（含 `MANIFEST.sha256`）。两个大件不入仓、按需生成：
`sample-large.txt` 820000 字节、`oversize-52428801.bin` 52428801 字节。

## 4. 🔴 一个只有「打开界面看一眼」才会发现的 bug（已修）

第一版把「服务端回了 `206`」当截断依据。**Range 覆盖整个小文件时服务端同样回 206**，
于是 1736 字节的文本被标成「已截断，全文 1.7 KiB」。
**当时自动化断言是绿的**（只断言了「大文本要有截断说明」），**是看截图发现的**。
已改为按 `Content-Range` 的 `end+1 < total` 判定，并补了「小文本不得出现截断说明」的反向断言。

## 5. 未闭合 / 属于别的线（我停在这里，没动）

1. **上传依赖 B 线返回 `url`**。基线 `internal/fileservice/http.go:70-75` 现返回
   `file_ref`＋`name`＋`size`＋`sha256`＋`media_type`＋`expires_at`＋`range`：四字段里已有三个，
   B 的增量实质只是 `file_ref` → `url`。B 未上线前 A 侧上传按 S4 报错（有意不回落）。
2. **附录 C 已裁的上传端 `{"error"}`**：A 侧已写成「能读到 `{"error"}` 就用它，读不到就如实给状态码」，
   不编原因。B 线落地后无需 A 侧再改。
3. **`HEAD` 是否真被 nginx 放行 —— 本件仍证明不了**（我用的是自写替身）。
   🔵 组长 2026-09-18 给出一条生产侧观测：不带票据的 `HEAD` 返 **404（应用层 `hide()`）**，
   而 nginx 拒绝方法会返 **403** ⇒ **请求到达了应用 ⇒ HEAD 被放行**。
   该观测出自组长，不是我跑的，正式实测仍挂在 `prodops`。**即便实测不过，④c 的降级分支已测到。**
4. **A-T08（直链无鉴权可达 + `nosniff` + `no-referrer`）本件证明不了**：替身按 A5 出头，
   只证明页面能正确消费这些头，**不证明生产 nginx 会这么出**。须评审在真环境验。
5. **评审方案 `runbooks/REVIEW-PLAN-A-openapi4aiterrazzc.md` 锚在 `d26bfd70`，早于附录 B/C**。
   其「分派断言只读取响应的 `media_type`」「专造扩展名与 media_type 不一致的夹具」两条，
   组长已定稿新版（含 `A-T08` 与接收侧 `A-R01/02/03`），其中「**权威头优先：扩展名与 `Content-Type` 不一致**」
   是唯一能分辨 (b)/(b′) 的用例 —— **本件 ④e 就是我这侧对该场景的自测**（`.bin` 声明成 `image/png` ⇒ 内联；
   `.png` 声明成 `octet-stream` ⇒ 下载链接）。评审仍会独立重跑，我不直接联系评审方。
6. **本机 chromium 是开源构建，无 h264 解码器**：`video/mp4` 只验到「降级为下载链接」，
   `video/webm`（VP9/Opus）验到真播放（时长 3s、画面 320×240）。**headless 也无内置 PDF 阅读器**，
   `<iframe>` 内为空白，但下载链接始终在。真环境用 Chrome/Edge 可验 mp4 与 PDF 内联。
