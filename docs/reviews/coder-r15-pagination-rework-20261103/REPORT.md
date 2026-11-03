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

分页本身会增加可见历史窗，因此分页应用期间跳过普通 live timeline 的 200 节点淘汰。否则在 100 条最新历史、一个 live 尾项和 100 条旧页的夹具中，逐条接收会先淘汰 `SEQ_101`，再造成 DOM 顺序缺口。页面锚点仍取加载前视口首个可见节点，并以其顶部差值补偿 `scrollTop`。

## 回归与变异

`view-service/r15-session-history-browser-regression.js` 增加以下判据：

- 分页后的第一个直接时间线子节点含 `R15_HISTORY_SEQ_001`。
- `R15_HISTORY_SEQ_100` 的直接后继就是分页前时间线原首条。
- 原可见锚点移动不超过 ±2px。

`view-service/r15-session-history-mutations.js` 分别删除整体搬移和 `scrollTop` 校正，检查失败分别发生在「timeline order invalid」和「scroll anchor shifted」。新 mutation suite 已登记在 `tools/run-browser-regressions.py`。

单轮与复跑结果均为 Chrome、bundled Chromium 各 `10/10` 通过；每轮读数 `anchor_delta_px=0.00`。完整逐轮输出见同目录 `chrome-10.log` 与 `bundled-10.log`。定向变异读数：

```text
PASS mutant=drop_history_node_move rc=2 red_at="timeline order invalid:"
PASS mutant=drop_scroll_anchor_correction rc=2 red_at="scroll anchor shifted"
```

完整构建门：`PATH=/tmp/dw-filecard-build-venv/bin:$PATH scripts/run-build-tests.sh`，结束标记 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。直接使用系统 Python 的首次调用因 helper case discovery 与 locked dependency 环境不匹配失败；locked venv 重跑通过。浏览器运行命令使用 `NODE_PATH=/tmp/dw-filecard-playwright/node_modules`，Chrome 渠道另加 `PW_CHANNEL=chrome`；未设置 `PW_CHANNEL` 时使用 bundled Chromium。
