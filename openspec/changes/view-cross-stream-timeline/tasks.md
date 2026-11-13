# Tasks

- [x] T-01 将独立卡与回合组放入统一的事件时间/epoch-seq排序流程。
- [x] T-02 增加整条 timeline 单调断言、七张卡逐张相邻回合断言及同 epoch 时钟偏差用例。
- [x] T-03 更新 `console-timeline-order` 的 context 与规格。
- [x] T-04 通过完整构建测试门、OpenSpec strict 校验，提交并推送修复。
- [x] T-05 跨 epoch 并组时仅同 rank 事件可改写组定位键，并断言整组仍在更晚排队卡之前、整轴时间单调。
- [x] T-06 增加恢复跨 epoch 键改写的变异刀；原因与 history-generation fixture 的 turn_id 修订一并交接评审。
- [x] T-07 复跑两代抓包及 badTurns/时间倒挂判定，完成三套定向回归、浏览器全量、构建与 OpenSpec strict 门。

## 交接记录

- 日期：2026-11-13；执行者：togpt60lunazzc；commit：本次修复提交。完成：目标真实 Chrome 回归通过；`--mutate-cross-stream-time` 在整时间轴单调判据失败；`npx @fission-ai/openspec@1.16.0 validate --all --strict` 43/43 通过；`scripts/run-build-tests.sh` rc=0。评审方 `out/view-input-20261113/replay3.cjs` 未随工作树提供，新增 7 张逐张断言的混合轴 fixture。阻塞：无。下一步：评审席在推送 tip 复跑真实两路判定器。
- 日期：2026-11-13；执行者：togpt60lunazzc；commit：待提交。完成：跨 epoch 并组的位置回归及恢复错误定位键改写的变异刀已加入；定向 Chrome 回归通过，变异按位置与单调断言转红。阻塞：两路真流回放、epoch 与全量套件尚待执行。下一步：完成全部复核门并推送供复核，不上线。
- 日期：2026-11-13；执行者：togpt60lunazzc；commit：待提交。完成：长抓包（300515067）与 9af18013 两会话均仅剩 badTurns ordinal 0、tsInversions 0；三套定向浏览器回归与全量 43 套通过；构建测试门双 profile 1203/1203、Python 27、shell 2 通过；OpenSpec strict 43/43 通过；全部相关变异按对应断言转红。阻塞：无。下一步：推送提交供 opus50tc2k.zzc 复核；不部署。
