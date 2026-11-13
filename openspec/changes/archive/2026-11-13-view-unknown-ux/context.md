# 变更上下文

## 当前基线

- 页面：`view-service/renderer-canary-sse.html`
- 页面浏览器回归：`view-service/renderer-delivery-status-regression.js`
- 浏览器 runner：`tools/run-browser-regressions.py`，新增脚本须登记 `SUITES`。
- View 健康版本：`view-service/view_service.py::_health_version` 从部署环境 `VS_VERSION` 读取。
- 页面状态逻辑：renderer 内 `refreshInboxLagLabel`、`renderWaitingDelivery`、`r40PaintQueuedWait`；本变更新增 `UNKNOWN_DELIVERY_MAX_AGE_MS`、`__viewUnknownUxNow` 测试缝及页面本地 delivery 集合。
- Hub 控制台版本角标：`internal/m8/console_auth.go::HandleConsolePage` 以 Hub 产品版本和 commit 替换 HTML 模板占位符；View renderer 不包含对应角标。

## 环境与约束

- 不修改 Hub、sessionHelper、服务端协议或发布版本号。
- 浏览器测试用自托管 renderer 与拦截的 HTTP 请求，不连接在线 Hub。
- 临时浏览器依赖安装在仓外；runner 中 `mcp_zero_consent` 子进程固定先选 `/usr/bin/node`，需使用支持 Node 18 的临时 Playwright 才能在本机完成该格。
- 测试通过可注入时钟推进 24 小时时限，不等待真实时间。
