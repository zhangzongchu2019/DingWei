# Context

## 当前代码事实

- `view-service/renderer-canary-sse.html` 由浏览器实时流与会话历史接口共同驱动时间线。
- canonical `input_posted` 在 `helper_queued` 等阶段留在 waiting list；`delivery_update` 到达 `cli_injected` 后才转为用户卡。
- Agent 输出由 `renderTurnEvent` 建立回合容器，用户卡原先在 `renderUser` 中直接追加。
- 历史分页通过 `loadSessionHistory` 调用 `handleIncomingEvent` 回放，并需保持既有可视锚点。
- 当前基线为 R51 G6-FIX 冻结提交 `113ac4befcbfafe9f392ab679d7c3eab137f9129`。

## 代码与测试位置

- 共用定位逻辑：`view-service/renderer-canary-sse.html` 的 timeline node helpers。
- 回归：`view-service/renderer-timeline-order-regression.js`，并登记在 `tools/run-browser-regressions.py`。
- 浏览器测试使用本地 HTTP fixture 提供真实 renderer、History JSON 和 EventSource，不依赖线上 Hub。

## 环境与依赖

- Node.js 与仓库既有 Playwright/Chromium 测试环境；无新增依赖。
- 负载轮次使用本机 CPU 饱和进程，并记录 `/proc/loadavg`。
