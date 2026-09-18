# C 线 T-178：503 可诊断性修复

更新时间（UTC）：`2026-09-18T22:55:01Z`

## 实施状态

- 分支：`coder56solzzc/c-t178-503-diagnosability`
- 基线：`fcac77c6c65cd442094ac2f6e07e697e120f8ad0`
- 实现锚：`119abf7b25654178000d3498363c8c9f8ee1f7cc`
- 状态：代码与测试已完成，未合并、未部署；生产环境变量配置与重启不在本交付范围。

## 改动事实

- C1：四项浏览器依赖缺任一项时，启动日志以 `missing_env` 指名缺少的环境变量；四项齐全才注入 `HTTPFileBrowser`。
- C2：未注入时 503 文案为 `file browser disabled: startup configuration was not injected`；已注入但 audit 列表失败时为 `file browser audit request failed: <原始错误>`。

## 验证事实

- 最终 `go test ./... -count=1`：rc=0。
- C1 外科变异：只删除新 warn，执行 `go test -v ./... -count=1` 共 981 条，唯一转红 `TestConfigureAdminFileBrowserWarnsForEachMissingSetting`。
- C2 外科变异：把 audit 失败文案改回未注入文案，执行同一全仓命令共 981 条，唯一转红 `TestAdminFilesPageDistinguishesMissingInjectionFromAuditFailure`。
- 两发变异均已还原，最终提交不含变异。
