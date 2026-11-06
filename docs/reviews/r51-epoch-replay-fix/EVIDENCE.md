# S-R51-EPOCH-REPLAY-FIX 复核证据

## 结论与范围

基线 `f7f31fca1d1f1468285138578c3f6fafacfb5537`（tree `150f51fea5e2de620912d1d66d35780935782865`）。调查没有证实派单描述的“换纪元后生产 DOM 历史被清空”：原始 renderer 在 S5 中仍保留旧内容；红因回归探针只读 `turnGroups` 的最新分段，而 epoch 分段实现让此兼容映射有意指向最后一段。此提交修正探针并添加 DOM 正文断言，同时登记浏览器 runner 缺少的 `TIMELINE_DEBUG` 环境覆盖名。未改生产 renderer、Hub、helper 或协议；因此不能将其表述为生产清空修复。

## 代码改动

- `view-service/renderer-replay-seq-regression.js`：从 `turnGroupSegments` 汇总同回合正文（旧实现兼容 fallback 到 `turnGroups`）；S5 同时读取 DOM 中各 `.event-content` 并断言历史正文与 `[NEW]` 都只出现一次，且中性重启提示保持不变。
- `tools/run-browser-regressions.py`：将 renderer timeline suite 读取的 `TIMELINE_DEBUG` 加入 `OVERRIDE_ENV_NAMES`，令全量 runner 可通过覆盖自检并运行。

## 根因核查

1. 基线 `f7f31fca1d1f1468285138578c3f6fafacfb5537` 上旧探针 S5 红：探针读 `turnGroups.get(t)`，只看见 `[NEW]`。
2. 同一基线的 renderer DOM 与分段模型保留旧片段；改探针为聚合所有 `turnGroupSegments` 后，DOM 断言及模型断言都得到 `[002]…[030][NEW]`。
3. `92949a79c555a46906065ad461c318265a3d9e0a` 的 diff 增加 epoch 分组/排序，并把 `turnGroups` 设为 `segments.at(-1)`；该提交没有引入跨 epoch 清空历史逻辑。
4. renderer 的重连清空路径仅在未见序号流时触发；本 S5 使用带 Hub epoch/seq 的 SSE 事件。移除实验性的额外 clear guard 后，正确探针仍通过。

## 定向回归与 parity

| 门 | 结果 |
|---|---|
| `NODE_PATH=/tmp/r52-browser/node_modules node view-service/renderer-replay-seq-regression.js` | PASS，13/13；S5 检查旧 DOM 正文保留、新正文唯一及中性提示 |
| `node view-service/renderer-timeline-order-regression.js` | PASS |
| `node view-service/renderer-epoch-order-regression.js` | `EPOCH_CHECK_PASS`、`HISTORY_GENERATION_CHECK_PASS` |
| `node view-service/md-render-core-parity.js` | PASS，99/99 输出字节一致 |
| `python3 view-service/test_md_render_core.py` | PASS，12 tests |
| `go test ./internal/m8 -run 'TestConsoleSpecEmbeddedMarkdownCoreIsPinned|TestConsoleSpec' -count=1` | PASS，40 cases pass / 0 fail / 0 skip，rc=0 |
| `python3 tools/run-browser-regressions.py --selftest-env-coverage` | PASS |

## 全量浏览器门

命令：`NODE_PATH=/tmp/r52-browser/node_modules python3 tools/run-browser-regressions.py --receipt /tmp/r51-epoch-replay-fix-browser-receipt.txt`，生成时间 `2026-11-06T12:40:13Z`，`aggregate_rc=0`。以下每个 suite 均 `rc=0`：

`openspec_binding_admin`, `r14_sticky_browser`, `coordinate_review_browser`, `openspec_read_browser`, `renderer_0206_file_render`, `console_browser`, `renderer_in_console_iframe`, `renderer_sse_reconnect`, `r15_session_history_pagination`, `renderer_timeline_order`, `renderer_epoch_order`, `r15_session_history_mutations`, `renderer_failcard`, `renderer_t054_errorboard`, `renderer_t074_delivery_id`, `renderer_card_status`, `renderer_provenance_icon`, `renderer_storage_denied`, `terminal_storage_denied`, `renderer_t086_empty_ring_e2e`, `renderer_replay_seq`, `renderer_upload_ticket_timeout`, `renderer_flood_stress`, `renderer_c2`, `console_specs_browser`, `console_spec_workflow`, `console_task_flow_r32`, `pm_spec_flow`, `md_render_core_sync`, `md_render_core_parity`, `renderer_markdown_image`, `renderer_markdown_injection`, `renderer_failure_dock_embedded`, `renderer_failure_dock`。

无红 suite，无需在 eaf8f01c 上做红项对照。`renderer_c2` 输出中有既有 T-166 KNOWN-DEFECT 诊断（hard pass 25、known defect 4），suite 本身仍 rc=0；全量 receipt 位于 `/tmp/r51-epoch-replay-fix-browser-receipt.txt`。浏览器 renderer SHA 为 `2bf883a284cb7f791d03cda14d000bf021cfffe4151b25e62d99c1d9fa786b3a`；本任务未改 renderer，因此无需更新 `console_specs_assets_test.go` 的 SHA 钉值。

## 变异读数

派单要求“恢复 92949a79 中导致清空的生产逻辑”使 S5 变红。代码核查未找到该提交引入的清空逻辑，故不能诚实构造或声称该生产变异已检测。现有证据是旧探针变异：将 S5 读取逻辑变异为只读 `turnGroups.get(t)`，replay suite `OLD_PROBE_MUTATION_RC=2`，仅 S5 在模型断言处红，观测 `[NEW]`；先行的 DOM 正文断言通过，说明旧内容仍在页面中。源码在 `finally` 中恢复，恢复后前述 13/13 正常回归已 PASS。此变异证明原红点来自探针兼容映射盲区，不是用户可见内容丢失。

## `run-build-tests.sh`

命令：`PATH=/tmp/r52lv/bin:$PATH TMPDIR=/tmp/r51erf.Uemyzu scripts/run-build-tests.sh`；PASS，摘要 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，`rc=0`。两 profile 均 1097/1097；短 TMPDIR 为 `/tmp/r51erf.Uemyzu`。

## 交付坐标

待提交与推送后补齐：branch、tip、tree、`git ls-remote` 核验值及最终 UTC 时间。

## 本席续接远端分支后的最终复核（2026-11-06）

按单写者规则查询时，目标分支已存在，tip 为 `b982bf83759a316067da7b2a8280646523e69502`。已 fetch 并在远端提交之上续接；保留远端已提交的 `turnGroupSegments` 聚合及 DOM 正文断言实现，没有推送本地重叠修改。

最终 tip 产品字节与基线 `f7f31fca` 再次逐字节核对一致：

- `view-service/renderer-canary-sse.html`：`2bf883a284cb7f791d03cda14d000bf021cfffe4151b25e62d99c1d9fa786b3a`
- `view-service/md-render-core.js`：`408d1bcf06e3f78198e3943fc41f3938ba20120d4148a7d3fa234e44d6c5545a`

最终 tip browser runner 在 2026-11-06T13:03:00Z 执行，命令与环境覆盖见上节，35 个 suite 全部 `rc=0`、`aggregate_rc=0`、无跳过项。逐项结果收据 `raw/browser-runner-final-receipt.txt` 列出每个 suite 的名称和 rc；完整终端输出为 `raw/browser-runner-final.log`。其中 `renderer_c2` 有 4 项 T-166 已知缺陷诊断，但该 suite 自身 rc=0。

最终 tip 的真丢段 renderer 变异：在 epoch 变化处理时移除页面所有旧 `.turn-group[data-turn-key="t-1"]` 节点，再接收 `[NEW]`。命令使用 `RENDERER_OVERRIDE=/tmp/r51-true-old-segment-drop-final.html` 执行 `node view-service/renderer-replay-seq-regression.js`。结果 S1–S4、S6–S13 PASS；S5 单独 FAIL，读到 `rendered DOM lost content: [NEW]`，缺少 `[002]`…`[030]`；总退出码 2。完整读数及退出码分别见 `raw/true-old-segment-drop-final.log`、`raw/true-old-segment-drop-final.rc`。该变异修改 renderer 副本实际删除旧页面段，确认 DOM 判据可识别真实可见丢段。

最终 tip build/test 门使用仓库已建立的隔离环境运行：

```sh
PATH=/tmp/r52lv/bin:$PATH TMPDIR=/tmp/r51erf.Uemyzu scripts/run-build-tests.sh
```

结果 PASS，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，退出码 0；claude、codex 两个 helper profile 各完成 1097 个预期测试。原始日志：`raw/build-tests-final.log`。环境名单自检也 PASS，见 `raw/env-registry-selftest.log`。

以上复核都在远端 tip `b982bf83759a316067da7b2a8280646523e69502` 上完成。本席将此补充证据作为追加提交推到同一目标分支，不改写现有提交。
