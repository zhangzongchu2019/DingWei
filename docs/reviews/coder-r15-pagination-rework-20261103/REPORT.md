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

性能门按设计 SLO 在 20 Agent × 25 次发送上测量。最终 paired additional p99 为 `66.981426ms`，阈值 `10ms`，未通过；测试现会以失败状态退出，而非只打印超限读数后报 PASS。相邻实测曾为 `40.604232ms`；此前记录亦超限（`68.909327ms`、`90.854643ms`）。原始最终读数及 rc=1 见 `perf-gate.log`。设计要求超限报告给组长并等待裁定，故本次不改 SLO 或持久化／发布语义；继续优化预计需另 30–45 分钟分析实现，待组长决定后启动。

完整构建门：`PATH=/tmp/dw-filecard-build-venv/bin:$PATH scripts/run-build-tests.sh`，结束标记 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，原始最终日志见 `build-final.log`。直接使用系统 Python 的首次调用因 helper case discovery 与 locked dependency 环境不匹配失败；locked venv 重跑通过。浏览器运行命令使用 `NODE_PATH=/tmp/dw-filecard-playwright/node_modules`，Chrome 渠道另加 `PW_CHANNEL=chrome`；未设置 `PW_CHANNEL` 时使用 bundled Chromium。

OpenSpec strict validation: `18 passed, 0 failed`. The full locked build/test runner completed with `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`; the `internal/m8` integration suite also ended `PASS`. `git diff --check` is clean. The performance SLO is the sole failing gate and remains explicitly reported above.
