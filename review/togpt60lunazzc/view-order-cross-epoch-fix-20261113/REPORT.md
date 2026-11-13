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

## 2026-11-13 复核补跑

只复核 `togpt60lunazzc/view-order-cross-epoch-fix`；当前 renderer SHA 与上节相同。没有 cherry-pick 或引用 `d622f9c2` / `view-order-live-pairing-fix`。

- 带 SSE `id` 的单次推送回放（judge `0d6dc7477`、`55bdcce59`，等待 2000 ms）复核四条现成抓包：长抓包 `300515067` 的 Gmail 为 58 nodes、9 turn groups、49 input cards，唯一 `badTurns=(ordinal 0, prev C)`，`tsInversions=0`；同抓包 50tc2k 为 94 nodes、11 groups、83 cards，唯一 `(0,C)`、倒挂 0。短抓包 `9af18013` 的 Gmail 为 59 nodes、10 groups、49 cards，唯一 `(0,C)`、倒挂 0；50tc2k 为 96 nodes、10 groups、86 cards，唯一 `(0,C)`、倒挂 0。四路均 `misplacedCards=0`、`duplicateInputs=0`。这确认位置改正没有引入 d622 的输入卡大面积转组行为。
- 定向 Chrome：`NODE_PATH=/tmp/dw-filecard-playwright/node_modules PW_CHANNEL=chrome node view-service/renderer-timeline-order-regression.js --rounds=1` 与 `node view-service/renderer-epoch-order-regression.js` 通过。位置及配对相关变异 `--mutate-no-cross-epoch-pair`、`--mutate-wrong-cross-epoch-segment`、`--mutate-cross-epoch-reposition`、`--mutate-cross-stream-time` 均以 rc=1 命中对应断言；定位键变异具体红于“paired group must stay before the later queued card”，同一读数显示时间倒挂。
- 浏览器全量：2026-11-13T06:01:06Z，`NODE_PATH=/tmp/dw-filecard-playwright/node_modules PW_CHANNEL=chrome python3 tools/run-browser-regressions.py`，43/43 suites、aggregate rc=0。receipt 绑定 `bb22e77c4b76c09fdba8c4a9db47a7461187170a` 与 renderer SHA `4a8d8cc1b2af87087b85ca8e55f2ba0e055cb1b26f88974321867fbde664ce85`。
- OpenSpec strict：`env -i HOME=/home/dev PATH=/usr/local/bin:/usr/bin:/bin OPENSPEC_TELEMETRY=0 npm_config_cache=/home/dev/.npm npx --yes @fission-ai/openspec@1.16.0 validate --all --strict --no-interactive`，43 passed，0 failed。
- 构建门：`env -i HOME=/home/dev PATH=/tmp/dw-test-venv/bin:/usr/local/bin:/usr/bin:/bin TZ=UTC PYTHONDONTWRITEBYTECODE=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOPATH=/home/dev/go GOMODCACHE=/home/dev/go/pkg/mod GOCACHE=/home/dev/.cache/go-build TMPDIR=/tmp bash scripts/run-build-tests.sh`，2026-11-13 06:01–06:11 UTC，rc=0；`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，每个 helper profile 实跑 1203 项。此命令使用 `env -i`，PATH 首位 `/tmp/dw-test-venv/bin`，Python 为 `/tmp/dw-test-venv/bin/python3`。
- 先前“四个 helper 文件未收集到用例”的 rc=1，可追溯记录在仓库 `EVIDENCE.md` 的 2026-11-05 行：`TMPDIR=$(mktemp -d /tmp/r52to.XXXX) scripts/run-build-tests.sh`。该次没有 `env -i`、没有 PATH/venv 前置，沿用继承 PATH 并解析到 `/usr/bin/python3`；缺 `pexpect`、`websockets` 后 discovery 对 Claude 与 Codex 两 profile 都报那四个文件无用例。之后锁定 venv 的干净复跑通过。若所指 rc=1 是另一条未留档命令，则其原始环境无法从当前仓库证据核实；不把两者混为一谈。

`renderer-epoch-order-regression.js` 第 106 行的 fixture 改动理由见“实现与回归”：原来共享 turn_id 会把跨 generation 的两个 marker 合成同一 visual group，使比较两个 timeline 子节点位置的历史顺序判据失去辨别力；改为 generation 独立 ID 后仍测原来的两节点顺序。它没有删断言或放宽判据；共享 turn_id 的跨 epoch 并组另由 `checkCrossEpochTurnPairing` 覆盖。

结果仅供分支复核；未冻结、未部署。冻结前仍由 prodops 在隔离环境另跑构建门。
