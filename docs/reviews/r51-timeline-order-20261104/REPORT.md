# S-R51-TIMELINE-ORDER evidence

## Scope

基于 G6-FIX 冻结件 `113ac4befcbfafe9f392ab679d7c3eab137f9129`，仅改 View renderer、浏览器回归、R15 对应变异测试、SUITES 登记、OpenSpec 与本报告。未改 Hub/helper、协议、迁移、版本位。

## Implementation

- `view-service/renderer-canary-sse.html`：新增 `timelineTimestamp` / `placeTimelineNode`；turn-group、user card、mention reply、assistant card 经共用定位；history page 对新建节点复用同一函数，保留滚动锚点补偿。
- `view-service/renderer-timeline-order-regression.js`：本地 HTTP + Chromium 真实 renderer/history fixture，覆盖输入进入 waiting → Agent 回复 → 接收确认、真实页面 reload 后历史重建、空闲对照。
- `tools/run-browser-regressions.py`：登记新 browser suite。
- `view-service/r15-session-history-mutations.js`：变异锚随历史节点统一定位方式更新；分页顺序、滚动锚点、读历史时不淘汰三刀均仍在各自断言上转红。

## Readings

- 新回归空闲 10 轮，Playwright bundled Chromium：`late_accept_before_reply=20`（含首载与刷新重建）、`reload_replay=10`、`idle_order=10`，全绿。
- 新回归空闲 10 轮，系统 Chrome：同上全绿。
- 新回归负载 10 轮：16 个并行 CPU-bound worker（16 online CPUs；`/sys/fs/cgroup/cpu.max=max 100000`）；`/proc/loadavg` 起始 `1.74 1.26 0.91`，结束 `5.31 2.05 1.17`；同一 10 轮矩阵全绿。
- 改前 renderer（基线 `113ac4be` 原始字节）同回归：rc 1，红在 `timeline order invalid: late accepted user card must be before same-turn reply`。
- 共用定位函数变异为 `appendChild`：rc 1，红在同一原判据文案。
- `renderer-markdown-image-regression.js`：15 pass / 0 fail，含既有五刀观察量；新 renderer SHA-256 `11ac0dae90cb73874a79d116054723ed242c33b33a87d960913a3a6c4075b944`。
- `tests/file-card-streaming.cjs`：12 pass / 0 fail。
- `delivery-ack-v2-browser-regression.js`：bundled Chromium 与系统 Chrome 各 `assertions=46, failed=0`。
- `r15-session-history-browser-regression.js`：bundled Chromium、系统 Chrome 各 pass；cursor、100+100 页、实时衔接、锚点位移 `0.34px`、配对去重与错误状态覆盖通过。
- `r15-session-history-mutations.js`：三刀均 rc 2 并在各自既有判据变红：`timeline order invalid`、`scroll anchor shifted`、`post-live history prefix was evicted`。
- `openspec validate r51-console-timeline-order --strict`（OpenSpec 1.16.0）：valid。
- renderer SHA 钉值核查：旧 G6 SHA `8033c1be94f15502…`、此前 SHA `d88e1889…` 及新 SHA 在仓内未发现静态部署期望值引用；运行时 `NEW_RENDERER_SHA` 由后续发布 GO 另行钉定。
- `scripts/run-build-tests.sh`：2026-11-04T23:10Z 左右开始，2026-11-04T23:19Z 结束，rc=0，末行为 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。在 `env -i`、独立临时 HOME、锁定 `/tmp/dw-filecard-build-venv` 下运行；helper 两 profile 各 `1097/1097`。

## Handoff

当前变更分支为 `review/togpt60lunazzc/r51-timeline-order-20261104`。交件前将补全构建门证据、最终 SHA/tree、推送及 `ls-remote` 对照。
