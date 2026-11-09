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

## 追加：第二个阻断——CSP form-action 'self' 拦截跳往飞书的 303（本席写入 2026-11-09T07:59:13Z）
coder56 在 S-MCP-ZERO-CONSENT 真 Chromium 测试中发现：Origin 修好后 connect POST 已得 303 且 Origin 正确，但连接页 CSP `form-action 'self'` 使浏览器拒绝跟随 303 到飞书授权源（浏览器按 form-action 校验重定向链）。本席本机 Chromium 独立复现：policy 仅 'self' 时同源 POST 被 303 到它源即被拦、页面不动、报 violates form-action 'self'；form-action 加上目标源后正常落地。原文 `raw/chromium-form-action-redirect.txt`，探针 `browser_csp_probe*.{py,js}`。
修法：连接页 CSP 的 form-action 改为 `'self'` ＋ 飞书授权端点的精确源。源在 NewHandler 时由已配置 OAuth 客户端生成一次授权地址并解析得到（不联网），只接受 https、无用户信息、主机名仅字母数字点横线（可带端口）；任何异常退回仅 'self'。默认生产配置得 `https://accounts.feishu.cn`。default-src、frame-ancestors、base-uri 不变；来源守卫不变。
测试：`internal/feishulogin/connect_csp_test.go`（默认得精确源且其它指令保留；http、注入、带用户信息、非法 URL 均退回 'self'）；`internal/mcpgw/origin_null_test.go` 增一格经网关输出的连接页 CSP。
变异：Handler 改回固定旧 CSP ⇒ `CSP_FORM_ACTION_MISSING_FEISHU` 与 `CSP_GATEWAY_FORM_ACTION` 红；去掉 https 校验 ⇒ `CSP_ORIGIN_ACCEPTED` 红。
未覆盖：本席未做「真 Hub＋真浏览器＋飞书替身」全链路；该项在独立复核 S-R562-CR 中要求。
