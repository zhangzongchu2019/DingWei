# Tasks

- [x] T-01 实现精确localhost动态端口回调、grant元数据兼容及精确CSP来源，并保持Codex身份限制。
- [x] T-02 使用Claude Code CIMD原文夹具补齐Resolve、回调、grant/token和CSP单测，运行真实Chromium两链及Codex回归。
- [x] T-03 执行四个目标变异，记录各自命中断言的红色结果。
- [x] T-04 执行mcpgw、feishulogin、m8、全仓Go、run-build-tests与OpenSpec strict门，记录原始日志和跳过项。
- [x] T-05 单一提交推送分支，核对远端tip/tree并交独立复核。

## 交接记录

- 2026-11-09，togpt60lunazzc.zzc；实现进行中，尚无提交。完成：`ee6b0998`基线隔离分支；Claude 原文夹具、Resolve/回调/grant/token/CSP测试、两条真实Chromium Claude链及既有浏览器链均通过；四刀各在目标断言判红；OpenSpec strict 32/32；`HUB_TEST_PG_STRICT=1 DW_BROWSER_E2E=1 NODE_PATH=/tmp/dw-filecard-playwright/node_modules go test -p 1 -count=1 ./internal/mcpgw ./internal/feishulogin` rc0；m8 `-p 1 -count=1` rc0，223.273s。阻塞：全仓go test、run-build-tests尚在执行。下一步：完整门通过后写入确切日志和协调时间，单一提交、普通推送并核远端坐标。
