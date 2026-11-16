# Tasks

- [x] Add a renderer regression for the Hub-sequenced canonical input → injection update → admitted echo sequence, asserting one final turn input and preserved Agent icon.
- [x] Add negative regressions for absent, untrusted, and non-Hub-attested canonical provenance.
- [x] Add a correlation regression proving a different delivery cannot inherit the icon.
- [x] Exercise history/replay merging with the same event sequence and confirm SSE ordering metadata remains intact.
- [x] Exercise the actual `/history` bootstrap response (not a direct `historyApplying` toggle), with Hub cursor generation/sequence mapping to the three SSE IDs; assert admitted history render retains the canonical icon.
- [x] Implement the renderer-only provenance retention at both live and replay merge paths; do not modify Hub code.

## 交接记录

- 日期：2026-11-16；执行者：togpt60lunazzc；commit：18b50c1a93b7e92b299eb53c613da497e612838f。
- 完成：renderer 仅在精确 delivery 命中的 primary canonical provenance 已由现有归一化器判为 trusted 时，保留其来源信息到 admitted turn；9 项新增浏览器检查通过，OpenSpec strict rc=0；以 a0 基线跑完 43 套、修复分支跑完 44 套，均 aggregate rc=0。
- 阻塞：仓库 `scripts/run-build-tests.sh` 在 helper discovery 阶段失败，报告 4 个既有 helper 测试模块未被收集；同一结果在 d2cc47c7 基线复现，未修改 helper 代码。
- 下一步：组长安排 renderer 单文件换件时，使用本分支的 renderer 文件；部署前按发布流程复核文件 SHA 与回滚点。

## History-path addendum

- Four-artifact history bootstrap replay with the same serialized Hub rows and `PROVENANCE_ICONS_V1=true`: 329eb9f9 → 1 icon; 2e1e4c9d → 1; c22bb63f → 0; a0e28049 → 0. The canonical card remains visible in the first two; the later two render only the admitted turn input.
- Commit-by-commit replay of renderer changes in the 2e1e4c9d..c22bb63f interval first changes the count at b425568ef. Commit 3184933ef is not the introduction point.
- Regression now checks both live dispatch and actual history bootstrap independently; 9/9 checks pass.
