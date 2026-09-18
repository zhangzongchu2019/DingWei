# A 线交付件 · 0.2.0.6 view 文件上传与按类型显示 · `opus50tc2k`

- **基线**：`27abfcf032db271a414eeea9ed08685c81b16a12`（附录 B1 裁定 = H 发版锚 = 现网跑的那一版）
- **契约读到**：`b5085588d701f59db9679f8e656172569adccc3a`（含附录 A 六项、附录 B 两项裁定、硬规丙）
- **交付分支**：`opus50tc2k/a-0.2.0.6-file-render`
- 🔴 **未合并、未推 `tasks/v0.1-B` 的产品代码** —— `stagegate 0.2.0.6` 仍红，按组长要求只停在自己分支。
- 执行时间：2026-09-18T10:30Z ~ 11:05Z

## 1. 改了什么（只动契约允许的那一个产品文件）

`view-service/renderer-canary-sse.html` — 371 增 / 23 删。

**① 上传入口（§3.1）**：粘贴由「只接 `image/*`」扩到**任意类型且一次多个**；新增**可见的文件选择按钮**
`📎 文件`（`<input type=file multiple>`，复用了源码里早就写好却没人用的 `.file-label` 样式）；新增**整页拖放**，
一次拖入多个全收。单个上限 `52428800`（附录 A4，50 MiB = 50<<20）。多文件顺序上传，
**一个失败不打断其余**（票据一次一张，并发会互相挤掉）。

**② 正文区分派（§3.2 + 附录 B2）**：扫正文中本站 `/f/` 直链 →
本页本次上传用响应 `media_type`，**收到的消息用 URL 扩展名** →
`image/*`→`<img>`、`video/*`→`<video controls>`、`pdf`→`<iframe>`、`text/plain`→`<pre>`（超限截断＋下载链接）、
**其它一切→下载链接**。附件区挂在**卡片**上而不是正文里 —— 正文每个流式 chunk 都重写 `innerHTML`，
挂正文里会让播放中的 `<video>` 被反复重建。

## 2. 🔴 我自选的假设（组长要求逐条列出）

| # | 假设 | 为什么这么选 | 错了的代价 |
|---|---|---|---|
| S1 | 文本内联上限取 **262144 字节（256 KiB）** | §3.2 只写「要有上限」没给数；256 KiB 约等于 6000 行，够读又不至于卡渲染 | 改一个常量，无结构影响 |
| S2 | 扩展名白名单 = 附录 A5 白名单的反向映射：`png/jpg/jpeg/gif/webp`→图片、`mp4/webm`→视频、`pdf`→pdf、`txt`→文本，**其余（含 `.bin`）→下载链接** | A5 只正向写了 media_type→扩展名；B2 要求「同一张表的反向映射」，jpg/jpeg 等别名是我补的 | 若 B 线实际落盘用了别的扩展名，该类型会掉进下载兜底（不会报错、不会裸 URL） |
| S3 | 只认 **与页面同源** 且路径以 `/f/` 开头的 URL | 附录 A1 的前缀；不对任意外站 URL 发任何请求 | 若直链换到别的 host，需同步放宽 |
| S4 | 上传响应**必须**有绝对 https `url`，**不回落**旧的 `file_ref` | §2 定的是 url；回落会掩盖 B 线未改完 | B 线未上线前上传必失败——这是有意的，见 §5 |
| S5 | 浏览器给不出 MIME 时按 `application/octet-stream` 上传，不按文件名猜 | §2「不靠扩展名猜」的原始含义（上传者可控 ⇒ 不可信） | 无 |
| S6 | 截断判定取 `Content-Range` 的 `end+1 < total`，**不以「回了 206」为准** | 见 §4 的那个真 bug | 无 |
| S7 | 内联渲染失败（图片加载错、视频解不了码、无 PDF 阅读器）一律**降级为下载链接** | 业主「不能显示的，直接给出下载链接」 | 无 |

## 3. 证据（值，不给结论）

### 3.1 交付物哈希

    0df6243025c0b514d9c806bb00cea69d6b36c558b9680200cb072cf6e6f8fd0c  view-service/renderer-canary-sse.html
    57d9fc67608034de2ae06adb48a64f6f619cbdc6908f8d4384d4b74068ba3929  view-service/renderer-0206-file-render-regression.js
    2f4757ce8568c7b4745cbffc58f78dee4339861856addeac33d252841237fe15  view-service/renderer-0206-contract-mock.py
    07d5515ba72605e237d1a745f34dc28cc8d5b8dc17ebe0c4db38087156347032  view-service/renderer-t147-paste-regression.js

### 3.2 功能级验收：真浏览器、真文件、真上传（10/10）

Playwright 1.45.3 chromium，跑 `view-service/renderer-0206-file-render-regression.js`：

    ① 上传入口在界面上可见（文件选择按钮），不是只有隐藏的 paste 监听器
    ② 文件选择一次传 6 个不同类型（图片/视频/pdf/文本/office/未知二进制）
    ③ 正文区渲染：图片 <img>、视频 <video>、pdf <iframe>、文本 <pre> 内联
    ④ 兜底：office 与未知二进制渲染成下载链接（<a download>），不报错也不留裸 URL
    ④a 接收方只拿到 URL（页面刷新后 hints 清空）仍按扩展名正确分派   ← 附录 B2 的「互传」那一半
    ④b mp4（本 chromium 无 h264 解码器）降级为下载链接，不报错、不留裸 URL
    ⑤ 拖放一次拖入 2 个文件，两个都上传
    ⑥ 粘贴一个 pdf（非图片）也上传 —— 原实现只接 image/*
    ⑦ 大文本内联截断并给下载链接（不是整篇灌进正文）
    ⑧ 超过 50 MiB（52428801 字节）在发请求前拒绝，不发上传请求

单元级 `renderer-t147-paste-regression.js`（从产品源码抽取函数，不复制实现）7/7，
其中上限值 `52428800` 是**从源码常量读出来断言**的，防「测试写死、产品写别的数」。

截图在 `runbooks/evidence-A-0.2.0.6/`：

    b254708f84871b3169108faee62c7d80541f81e6ec6db1df099e93c4766f6da4  01-upload-entry-visible.png
    44441aa08892d9549c8744692d053b873b9f6f2acb9b3a70e2cb735aa6bf2b40  02-body-rendered-all-types.png
    6d6a984875392ed6c1954d0c488528795a9bccfd35ae8e0616d308577c9c8c70  02a-inline-image.png
    1556ccbf5bd1eba16fd1888fbde52391c7876b3ee4d45cea8fd888f4f5d5e1e1  02a-inline-video.png
    c418fd41f2f0249d05e955a83869907297b771a58061bbbd5a0bd1deca4ca840  02a-inline-pdf.png
    901e7b76933993b428b4df9395392c715ec5b1d36354174c93ee51b73c61080e  02b-receiver-side-by-extension.png
    7e84d4e032bfa3e06aac5741565361f8a5b97c1c03941f4d8bce79b884bf6641  03-large-text-truncated.png

### 3.3 回归对照（改后 vs 未改的基线 `27abfcf0`，同一套命令同一个环境）

    node 组 6/6 全 PASS（t086 / t138 / t141 / t145 / t146 / t147）
    浏览器组：g2 · failcard · page-identity · provenance-icon · t054 · t074  →  改后 PASS / 基线 PASS
              renderer-regression · longtext · sse-smoke · sse-reconnect · c2  →  改后 FAIL / 基线 FAIL

🔴 **后五项在【未改动的基线上同样 FAIL】**，即它们与本次改动无关（c2 是缺 `renderer-c2.html` 文件；
其余为本机缺相应 fixture 服务）。基线副本在 `/tmp/baseline/view-service`，可复核。

### 3.4 样本

`view-service/fixtures-0206/`（含 `MANIFEST.sha256`）。两个大件不入仓、由验收方按需生成：
`sample-large.txt` 820000 字节、`oversize-52428801.bin` 52428801 字节。

## 4. 🔴 一个只有「打开界面看一眼」才会发现的 bug（已修）

第一版把「服务端回了 `206`」当截断依据。**Range 覆盖整个小文件时服务端同样回 206**，
于是 1736 字节的文本也被标成「已截断，全文 1.7 KiB」。
自动化断言当时是绿的（我只断言了「大文本要有截断说明」），**是看截图发现的**。
已改为按 `Content-Range` 的 `end+1 < total` 判定，并补了「小文本不得出现截断说明」的断言。

## 5. 未闭合 / 需要别的线的（我停在这里，没动别人的线）

1. **上传现在依赖 B 线返回 `url`**。t022 的 `internal/fileservice/http.go:70-75` 目前返回的是
   `file_ref`＋`name`＋`size`＋`sha256`＋`media_type`＋`expires_at`＋`range` ——
   四个字段里**已有三个**，B 的增量实质只是 `file_ref` → `url`。B 未上线前，A 侧上传会按 S4 报错。
2. **`internal/fileservice/http.go:216` 的 `hide()`** 把上传失败一律返成 `404 not found`、无 `{"error"}` 正文，
   与 §2 失败口径不符。A 侧已写成「能读到 `{"error"}` 就用它，读不到就如实给状态码」，不编原因。
3. **A-T08（直链无鉴权可达 + `nosniff` + `no-referrer`）本次证明不了**：我用的是自己写的契约替身
   `renderer-0206-contract-mock.py`，它按 A5 出头，但那只证明我的页面能正确消费这些头，
   **不证明生产 nginx 会这么出**。必须由评审在 B 线到位后的真环境验。
4. **评审方案 `runbooks/REVIEW-PLAN-A-openapi4aiterrazzc.md` 锚在 `d26bfd70`，早于附录 B**。
   其「执行前置条件与统一观测」第 3 条要求「分派断言只读取响应的 `media_type`」并专门构造
   「文件名扩展名与 media_type 不一致」的夹具 —— **这条已被附录 B2 改掉**（接收方一侧改按 URL 扩展名分派）。
   请组长决定是否让评审按 `b5085588` 重取后再执行，**我不直接联系评审方**。
5. **本机 chromium 是开源构建，无 h264 解码器**，`video/mp4` 只能验到「降级为下载链接」；
   `video/webm`（VP9/Opus）验到了真播放（时长 3s、画面 320x240）。真环境用 Chrome/Edge 可验 mp4。
   **同理 headless 无内置 PDF 阅读器**，`<iframe>` 里是空白，但下载链接始终在。
