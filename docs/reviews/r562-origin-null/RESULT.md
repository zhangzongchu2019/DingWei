# r56.2：授权页表单 POST 被自身来源守卫拒（Origin: null）

## 现象
2026-11-09T07:0xZ 业主用 Codex 桌面版在现网 r56.1（0.2.3.32）接入：打开 `/feishu/connect?attempt_id=…` 后提交，得 `{"error":"origin_rejected"}`。

## 成因
- 网关守卫 `internal/mcpgw/gateway.go`：请求带 Origin 头时必须是受信来源，`null` 一律 403 `origin_rejected`。
- 连接页 `internal/feishulogin/connect.go` 与网关守卫都发 `Referrer-Policy: no-referrer`。按 Fetch 规范，非 CORS 请求（导航式表单 POST）在 referrer policy 为 no-referrer 时把 Origin 序列化为 `null` ⇒ 页面自己的表单被自己的守卫拒。
- CLI 原生 E2E 与 Go 单测都没有经过真浏览器，所以 r56.1 门全绿未发现。

## 实证
现网只读（无效 attempt，不产生记录）：`Origin: https://dw123.cmjrt.com` 400 invalid_request（过守卫）；无 Origin 400；`Origin: null` 403 origin_rejected。
本机 Chromium（playwright 1.63，headless shell）：同源表单 POST 在页面 policy＝no-referrer 时 Origin＝'null'；same-origin 与 strict-origin-when-cross-origin 时为真实来源。原文 `raw/chromium-origin-by-referrer-policy.txt`，探针 `browser_origin_probe*.{py,js}`。

## 修法
两处 Referrer-Policy 改为 `same-origin`：同源请求带真实 Origin，跨站跳转（飞书）仍不带 referrer。来源守卫本身不放宽（`null` 仍 403）。

## 测试与变异
新增 `internal/mcpgw/origin_null_test.go`：连接页经网关输出的 policy＝same-origin；守卫端点 policy＝same-origin；`Origin: null` 仍 403。`internal/feishulogin/connect_test.go` 期望值同步。
变异（一次性工作树）：connect.go 改回 no-referrer ⇒ `ORIGIN_NULL_CONNECT_PAGE_POLICY ["no-referrer"]` 红；gateway.go 改回 ⇒ `ORIGIN_NULL_GUARD_POLICY ["no-referrer"]` 红。

## 范围
仅两行响应头＋测试＋版本位 0.2.3.33；无迁移、无新开关、View 不变。S-MCP-ZERO-CONSENT 的选择页同样经网关守卫，合入时随本修复一并生效。
