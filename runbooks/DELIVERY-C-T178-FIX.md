# C 线 T-178：503 可诊断性修复

更新时间（UTC）：`2026-09-18T23:42:50Z`

## 实施状态

- 分支：`coder56solzzc/c-t178-503-diagnosability`
- 基线：`fcac77c6c65cd442094ac2f6e07e697e120f8ad0`
- 实现锚：`f0c8aadcad5cb1f6c1f72901fd5cad2fff83dec4`
- 状态：代码与测试已完成，未合并、未部署；生产环境变量配置与重启不在本交付范围。

## 改动事实

- C1：四项浏览器依赖缺任一项时，启动日志以 `missing_env` 指名缺少的环境变量；四项齐全才注入 `HTTPFileBrowser`。
- C2：未注入时 503 文案为 `file browser disabled: startup configuration was not injected`；已注入但 audit 列表失败时为 `file browser audit request failed: <原始错误>`。
- C3：AST 装配形状断言只在 `run()` 函数体内查找 `configureAdminFileBrowser(...)` 调用，防止 helper 存在但未接入启动路径。

## 验证事实

- 最终 `go test ./... -count=1`：rc=0。
- C1 外科变异：只删除新 warn，执行 `go test -v ./... -count=1` 共 981 条，唯一转红 `TestConfigureAdminFileBrowserWarnsForEachMissingSetting`。
- C1 排他变异：退化为无论缺哪一项都列出全部四项，同一全仓命令执行 981 条，唯一转红同一测试；断言同时证明缺项被报出且其余三项未被误报。
- C2 外科变异：把 audit 失败文案改回未注入文案，执行同一全仓命令共 981 条，唯一转红 `TestAdminFilesPageDistinguishesMissingInjectionFromAuditFailure`。
- C3 外科变异：只删除 `run()` 中的注入调用，执行 `go test -v ./... -count=1` 共 982 条，唯一转红 `TestRunWiresAdminFileBrowserConfiguration`。
- 所有变异均已还原，最终提交不含变异。
