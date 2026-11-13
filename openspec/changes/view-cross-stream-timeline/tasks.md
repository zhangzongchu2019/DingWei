# Tasks

- [x] T-01 将独立卡与回合组放入统一的事件时间/epoch-seq排序流程。
- [x] T-02 增加整条 timeline 单调断言、七张卡逐张相邻回合断言及同 epoch 时钟偏差用例。
- [x] T-03 更新 `console-timeline-order` 的 context 与规格。
- [x] T-04 通过完整构建测试门、OpenSpec strict 校验，提交并推送修复。

## 交接记录

- 日期：2026-11-13；执行者：togpt60lunazzc；commit：本次修复提交。完成：目标真实 Chrome 回归通过；`--mutate-cross-stream-time` 在整时间轴单调判据失败；`npx @fission-ai/openspec@1.16.0 validate --all --strict` 43/43 通过；`scripts/run-build-tests.sh` rc=0。评审方 `out/view-input-20261113/replay3.cjs` 未随工作树提供，新增 7 张逐张断言的混合轴 fixture。阻塞：无。下一步：评审席在推送 tip 复跑真实两路判定器。
