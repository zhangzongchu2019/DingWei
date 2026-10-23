# S-PM-SPEC-FLOW REWORK verification

受审冻结源 `40f74e2f` 后追加的 REWORK 源码锚为 `3ffc121d20de8a2f3ecc39042fec5075dc3fe891`（tree `3241cb1337b82a6c0203b3d40e48907ef74bd946`）。

## 三项修复

- 定稿仍要求 `flowState == review_complete` 且 frozen revision 匹配；新增匹配 revision/SHA 但处于转审、未完成审查的负格。
- R8 的开始请求、criteria 确认、取消转交三个存储事务入口均拒绝已登记 PM flow spec；包含转审接收 Agent 的负格。
- presplit 与候选确认均走同一 opt-in 最终修订门：开关关闭时保留既有行为；开启时只接受 `final`、冻结 revision 与当前 revision 相同且 SHA 匹配的 PM flow 修订。

实现及测试范围详见源差集；定向测试为 store 与 m8 两包 `TestPMSpec|TestLegacyR8`，rc0。六项新增撤修各自完成 baseline rc0 → 指定标记 rc1 → 恢复 rc0，原始输出在 `withdrawals/`。

## 完整读数

- `go test ./... -count=1 -timeout=30m`：rc0，66 包（53 测试包、13 无测试文件）。
- `go vet ./...`：rc0。
- canonical：rc0，`BUILD_TESTS_OK python=59 shell=2`。
- helper：沿用既有隔离 HOME 全套 851 项通过；REWORK 未改 `tools/sessionhelper`，与既有 helper 证据输入逐字节相同。
- 候选 browser runner rc2；PM Spec Flow 15/0，390px／820px 覆盖。四个非 PM 失败套件在 r37 与候选相同：`renderer_0206_file_render`、`renderer_flood_stress`、`renderer_c2`、`console_specs_browser`。两轮 stdout/stderr 与逐套件 rc 保存在 `browser/`。r37 首轮 TMPDIR 不存在的启动失败单独保留，未用作基线结果。

全量机器可读读数见 `REWORK.json`。本次仅本地开发与测试，没有部署或现场操作。
