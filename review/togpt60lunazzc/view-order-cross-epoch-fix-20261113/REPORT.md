# S-VIEW-ORDER-LIVE-PAIRING 复核修正

- 执行者：togpt60lunazzc.zzc
- 复核请求：opus50tc2k.zzc，2026-11-13T04:50:09Z
- 基线：`4871e32b752b7650ffd365ab36e5c946983fcb48`
- 本次改动：跨 epoch 并组不再用外来 epoch 的 seq 改写既有组定位键；加入位置、时间单调和对应变异判据。
- 结论：同意复核指出的根因。配对本身正确，但跨 epoch 事件若以自己的 rank/seq 降低组定位键，`placeTimelineNode` 会按同 epoch seq 将整组移到较早 seq 的排队卡之后，造成可见时间倒挂。

## 实现与回归

`renderTurnEvent` 仅在 `rank === group.timelineEpochOrder` 时比较并更新组的 seq 定位键。跨 epoch 事件仍照常进入组内事件队列和排序，不会把原组 anchor 改成另一个 epoch 的键；显式 hub sequence 分段逻辑保持不变。

`checkCrossEpochTurnPairing` 使用较高 rank 的 history 分段、较低 rank 的 live epoch、live epoch 中 seq 更早且时间更晚的排队卡，以及一个要并入 history 组、但事件时间早于该卡的实时事件。正确实现读数：两组，实时事件进入时间最近的分段，整组在卡片前，节点时间 `[1000, 8800, 8900]` 单调不减。

renderer-epoch-order-regression.js 将两代 history 共用的 `turn_id` 改为 generation 各自独立的 ID，是因为原断言通过两个 timeline 子节点的索引比较 history 先后；相同 turn ID 会把两个 marker 合并到同一 visual turn group，两个索引相同，无法测出节点间 epoch 顺序。改夹具保留了原断言要测的跨 generation 节点顺序，也没有删除任何原有断言。它没有削弱旧判据：原夹具没有断言同一 turn 跨 generation 的并组行为；现在由 `checkCrossEpochTurnPairing` 专门用共享 turn ID 覆盖该行为和定位键保护。

## 回放读数

使用 `origin/opus50tc2k/view-order-live-judge` 中 replay-server（SSE `id` 原样输出，commit `0d6dc7477ca603fc3e9c2d59b4680f596325b757`）与 live-judge（commit `55bdcce5937ab366fb097cb2ecde1ac2a779c8aa`），在同一回放端口运行。判定器使用本报告 renderer，`--history-file` 指向各抓包 history，`wait-ms=2000`。

| 抓包 | 会话 | children / turnGroups | badTurns | tsInversions | 末节点 |
|---|---|---:|---|---:|---|
| `300515067`（long-20261113T0318Z） | opuszzcgmail.zzc | 58 / 9 | idx 46，ordinal 0 | 0 | message-card.user |
| `300515067`（long-20261113T0318Z） | opus50tc2k.zzc | 94 / 11 | idx 77，ordinal 0 | 0 | turn |
| `9af18013` | opuszzcgmail.zzc | 59 / 10 | idx 47，ordinal 0 | 0 | message-card.user |
| `9af18013` | opus50tc2k.zzc | 96 / 10 | idx 81，ordinal 0 | 0 | turn |

四次回放的 history 均为 100 行，duplicateInputs=0、misplacedCards=0。ordinal 0 是两份历史页首屏截断项；本次未增加其他 bad turn。长抓包判定器输出有五条既有 `[c2] 可取消条目缺 message_id` 告警，与复核反馈中所述一致。

## 验证

- `node view-service/renderer-timeline-order-regression.js --rounds=1`：通过，包含新位置与整轴单调断言。
- `node view-service/renderer-epoch-order-regression.js`：`EPOCH_CHECK_PASS`、`HISTORY_GENERATION_CHECK_PASS`。
- 变异 `--mutate-no-cross-epoch-pair`：红于 groupCount（错误地产生第三组）。
- 变异 `--mutate-wrong-cross-epoch-segment`：红于 liveGroup（并入错误分段）。
- 变异 `--mutate-cross-epoch-reposition`：红于“并组保持在排队卡之前”，同一读数的时间轴亦倒挂。
- 既有变异 `--mutate-cross-stream-time`：红于整轴时间单调断言。
- `tools/run-browser-regressions.py`：43/43 suite rc=0，aggregate_rc=0；包含 capture-input、renderer-timeline-order、renderer-epoch-order。renderer_c2 输出既有 T-166 四项已登记缺陷，套件 rc=0；本次未改中断确认代码。
- `scripts/run-build-tests.sh`，使用 `/tmp/dw-test-venv/bin` 中锁定运行时：`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；helper 两 profile 各 1203/1203。
- `OPENSPEC_TELEMETRY=0 npx --yes @fission-ai/openspec@1.16.0 validate --all --strict`：43 passed，0 failed。
- `git diff --check`：通过。

全量浏览器 runner 页面字节 SHA256：`4a8d8cc1b2af87087b85ca8e55f2ba0e055cb1b26f88974321867fbde664ce85`。

## 执行命令与时刻

- 本次复核与回放：2026-11-13 04:50Z–05:27Z。
- 定向回归与变异：`node view-service/renderer-timeline-order-regression.js --rounds=1 [mutation]`；`node view-service/renderer-epoch-order-regression.js`。
- 全量浏览器：`NODE_PATH=/tmp/view-order-cross-epoch/node_modules python3 tools/run-browser-regressions.py`，2026-11-13T05:06:14Z，aggregate rc=0。
- 构建门：`PATH=/tmp/dw-test-venv/bin:$PATH scripts/run-build-tests.sh`，rc=0。
- OpenSpec strict 命令如上，rc=0。
- 未部署。
