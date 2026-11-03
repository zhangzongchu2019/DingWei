# R15 pagination ordering rework

基线：`c54b79d22fc3`（prodops 只读复证锚点）。

## 修复前 Chrome 失败读数

以下是组长提供的 prodops 只读证据（未重跑，失败渠道 Chrome）：

```json
before={"top":300,"height":6075,"client":524,"overflowAnchor":"none"}
after={"top":11516,"height":12040,"client":524,"overflowAnchor":"none"}
anchor={"connected":true,"text":"…R15_HISTORY_SEQ_106","top":-11175.40625}
first={"text":"…R15_HISTORY_SEQ_102","top":-11416.40625}
```

`after.top` 等于 `height - client`，视图落到底部；新页 001–100 没有成为时间线开头，页面第一条仍是 102。锚点位移约 `-11175.4px`。同轮 bundled Chromium 之前通过，报告值 `anchor_delta_px=-0.25`。

## 处置

历史页的 `user_input` 在 live SSE 中会进入 C2 echo hold（默认 800ms），所以 `handleIncomingEvent` 返回时节点未必存在；随后立即计算 DOM 差集会漏掉待追加节点。历史加载现在在 `historyApplying` 时跳过该异步合并，回放后同步 `flushPendingRenders()`，然后将本页新节点整体插到加载前的时间线原首条之前。取锚点前也同步 flush，避免已有 turn-group 重绘替换锚点。

分页本身会扩展可见历史窗。分页应用期间不做普通 live timeline 的 200 节点淘汰；成功加载更早页后，只要用户仍离开底部阅读，就暂停 oldest-first 淘汰，避免后续 live 事件移除刚翻出的 `SEQ_001`。用户回到底部时恢复 200 节点上限并从最旧端淘汰。页面锚点仍取加载前视口首个可见节点，并以其顶部差值补偿 `scrollTop`。

## 回归与变异

`view-service/r15-session-history-browser-regression.js` 增加以下判据：

- 分页后的第一个直接时间线子节点含 `R15_HISTORY_SEQ_001`。
- `R15_HISTORY_SEQ_100` 的直接后继就是分页前时间线原首条。
- 原可见锚点移动不超过 ±2px。
- 翻页后再通过真实 `EventSource` 推送一条 live 事件，旧页首条仍为 `SEQ_001` 且锚点保持 ±2px。
- 页内包含同 `delivery_id` 的 `input_posted` 与 `user_input` 回显，匹配 `data-message-id` 的用户卡数量为 1。

`view-service/r15-session-history-mutations.js` 分别删除整体搬移、`scrollTop` 校正及离底阅读期间的淘汰暂停，检查失败分别发生在「timeline order invalid」、「scroll anchor shifted」和「post-live history prefix was evicted」。新 mutation suite 已登记在 `tools/run-browser-regressions.py`。

回归等待可观察的 `pendingRenders` 与 C2 merge buffer 清空，不使用固定睡眠，也不在测试中调用产品 flush。产品原先的「取锚前 flush」被删除后，断言仍绿；将该行重新插入的对照也通过，锚点读数同为 `0.34px`，因此该行没有可复现的保护作用，按“无证据代码不留”删除。

最终源码上的 Chrome、bundled Chromium 各连续 `10/10` 通过，逐轮同时记录分页后的和后续 live 到达后的锚点读数。完整逐轮输出见同目录 `chrome-10.log` 与 `bundled-10.log`。定向变异读数：

```text
PASS mutant=drop_history_node_move rc=2 red_at="timeline order invalid:"
PASS mutant=drop_scroll_anchor_correction rc=2 red_at="scroll anchor shifted"
PASS mutant=restore_live_eviction_during_paged_history rc=2 red_at="post-live history prefix was evicted:"
```

在 7ab03ea4 返工提交上，性能门按设计 SLO 于 20 Agent × 25 次发送测量。paired additional p99 为 `66.981426ms`，阈值 `10ms`，未通过；测试当时已改为超限退出失败，而不是只打印数值后报 PASS。相邻实测曾为 `40.604232ms`；此前记录亦超限（`68.909327ms`、`90.854643ms`）。原始读数及 rc=1 见 `perf-gate.log`。当时按冻结设计报告给组长等待裁定；下方记录 14:04:54Z 后续裁定及本次优化证据。

完整构建门：`PATH=/tmp/dw-filecard-build-venv/bin:$PATH scripts/run-build-tests.sh`，结束标记 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，原始最终日志见 `build-final.log`。直接使用系统 Python 的首次调用因 helper case discovery 与 locked dependency 环境不匹配失败；locked venv 重跑通过。浏览器运行命令使用 `NODE_PATH=/tmp/dw-filecard-playwright/node_modules`，Chrome 渠道另加 `PW_CHANNEL=chrome`；未设置 `PW_CHANNEL` 时使用 bundled Chromium。

此前 7ab03ea4 tip 的 OpenSpec strict validation 为 `18 passed, 0 failed`，locked build/test runner 标记 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；下节列出裁定后的最终复跑。

## 2026-11-03 性能门裁定后的追加优化

组长于 `14:04:54Z` 明确授权继续优化并冻结 10ms 门槛。本席选择同步组提交：`SessionHistory` 单写协程从容量 32 的队列收取最多 32 条请求，最多等 2ms；在单 SQLite 事务内按入队顺序分配 generation/seq、写入事件、更新字节计数并执行每 Agent/全局容量淘汰。调用方必须等该事务提交后才收到自己的 cursor，Hub 原有调用仍在 `Append` 返回后把同一 cursor 加入 live payload 并发布。组事务失败时先回滚，再按既有单事件事务逐项重试，隔离一条坏事件；关闭时拒绝新请求、排空队列后关闭 DB。WAL、`synchronous(NORMAL)`、实时发布时序、gap 失败语义及 10ms 门槛均未改。组提交实现与调用/关闭语义在 [session_history.go](../../../internal/store/session_history.go)；同会话并发 cursor 顺序和失败隔离回归见 [session_history_test.go](../../../internal/store/session_history_test.go)。

第一轮优化性能实施于 UTC `2026-11-03T14:12:08Z`；runner 使用 `env -i`，只注入 Go 缓存/模块路径、临时 HOME/TMPDIR 与 `DW_SESSION_HISTORY_PERF=1`，未继承 `SH_*`、`DINGWEI_*` 或环境配置。机核数 `16`；开跑 load average (1/5/15m) `(1.2534, 1.6460, 1.3257)`。20 Agent × 25 pushes 共 500 个样本，连续五轮原文（完整日志 `perf-five.log`）：

| Round | baseline p99 | enabled p99 | paired additional p99 | 门槛 |
|---|---:|---:|---:|---:|
| 1 | 1.542999ms | 5.588534ms | 5.584844ms | 10ms |
| 2 | 1.468267ms | 5.795954ms | 5.786655ms | 10ms |
| 3 | 1.670486ms | 6.028111ms | 6.025531ms | 10ms |
| 4 | 1.712262ms | 5.671506ms | 5.659986ms | 10ms |
| 5 | 1.744969ms | 5.867167ms | 5.858437ms | 10ms |

五轮皆 `rc=0` 且 paired p99 ≤10ms。性能门的两项变异原始输出在 `perf-mutations.log`：删除超限断言后，`TestSessionHistoryPerformanceGuardsAreLive` 以“over-budget performance sample was not rejected”失败；把计时起点挪至整个 handler（含持久化）返回之后，计时样本仅 80–140ns，性能用例以“too small to include the synchronous durable append”失败。两个源变异由 runner 逐项做、各自确认红后恢复源文件。并发组提交 cursor 测试另以 `-count=20` 通过。

性能门获准后的全包 Go、涉存储 race、完整构建门和 OpenSpec strict 结果以本节最终更新和提交附带日志为准；以上性能五轮是在优化源码及测试断言落地后执行。
