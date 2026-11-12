# S-VIEW-REFRESH-MISSING-OUTPUT — 真实 payload 标识复现

日期：2026-11-12 UTC；修复调查分支：`togpt60lunazzc/view-refresh-missing-output-fix`，基于 r59 `8528ed8cf7e43c1bec97102927c92975ed714609`。  
r57 renderer `329eb9f9c45d2d785a0429b01eaa0fa5264fe15d` SHA256 `8426af56da05145108a4db9b3dd0025d13bdf7a06c01fb02c2a4c456f38d672c`；r59 renderer SHA256 `8d39be9b7f9cf635da5cf5c4757a2e8030224a5ded9c1aa4f5c1aaa018a57d71`。本轮仅新增合成复现和报告，没有改 renderer/Hub/helper 产品代码。

## 结论

按 prodops 补报的真实 payload 形状构造 47 行同历史键、同 generation、seq 连续的合成页面后，r57 与 r59 在**刷新首屏、实时到达、向上翻页**三种路径都显示全部 12 条 assistant 文本。另在同一页把 Claude 输出的 payload.session_id 按 24 小时证据变成 `bc19…`、`1be22…`、`83e9…` 三段，两个 renderer 仍显示全部输出。**没有复现输入/输出 session_id 差异导致刷新丢输出，也没有复现跨 turn 分组隐藏输出；当前无可证实的 renderer 修复。**

此结果只覆盖所列合成响应体。历史页的原始实际完整 payload、record_bytes/gap 补报仍待 prodops；未知的其余 4 个 turn_id 取值在夹具里用占位符，但按已知统计精确保持 7 个输入/输出非空 turn_id 对应及空串 queued 输入。拿到剩余原值后可直接替换占位符再跑。

## prodops 真实标识事实

来源为 `review/prodops/view-history-readonly-20261112` 的 session-id 补报，commit `e429f9536ba5f6d96a58a0185ae860b7be5b3c93`（补报正文承接 `d48c6fd6595a551bd351078f6cc8b5b0d7ea0817`；文件及原始摘要保存在 `raw/prodops-*`）。

- 原窗口 47 行共享 Hub 历史键 `owner=u_zhangzongchu`、同 agent/session_name `opus50tc2k`、generation `0fda970aaff3905c61684736ec808452`，seq `3907–3953` 连续。用户输入 `session_id=""` 共 14 条；其中 queued 7 条 `turn_id=""`，admitted 7 条 turn_id 非空。Claude 侧 assistant_text/tool_call/state_change 均为 `session_id=83e9a02a-e451-488e-a699-c62fa95ae27e`，计数分别 12/13/8。`session_id` 是 payload 字段，Hub history 按 storage session/generation 查询，不按该 payload 字段筛选。
- user_input 与 assistant_text 的非空 turn_id 交集为 7。补报给出的前三个值一致包括 `8d0b99cd394a4e6dbd3c6e87e09594c3`、`fb86893a830448d7bd5450c307fe8d2e`；assistant 第三个给出 `123f55eecf9a487e976eb3dd540697e6`。其余实际值/逐 seq crosswalk 未提供，本夹具用占位 ID 保持既定交集关系，不冒称已知真实值。
- 前 24 小时出现三段非空 Claude session_id：`bc19e89c-41f7-4109-ae86-660ac1ef39dc`（至 01:50Z）、`1be22a87-c900-4378-87b7-2c862ffc7c1d`（02:00–04:36Z）、`83e9a02a-e451-488e-a699-c62fa95ae27e`（04:57Z 起）；输入空串贯穿。当前 2 小时窗口的全部 Claude 侧行是 83e9 值。合成 session-switch 变体专门将这些三个真实值放入同一展示页，检验 renderer 是否因 payload ID 换代重置。

## 复现构造与结果

[`repro-real-identities.js`](repro-real-identities.js) 从两指定 Git commit 读取 renderer 原始 HTML，启动 loopback mock Hub 与浏览器；不连接现网、不读生产 DB。事件 kind/seq 顺序逐条对应 prodops 47 行。event body 全为合成内容；身份字段按补报赋值。刷新用 history 返回全部 47 行。实时场景先由 history 返回 14 条输入，再由 SSE 送达 33 条工具/assistant/终态。分页场景首屏返回当前 47 行，点击“向上翻页”后请求 `before=3907`，前页放入带 bc19/1be22 两个 payload session_id 的诊断记录。session-switch 场景将同一 47 行的 Claude payload session_id 分段设为三个真实 ID。旧分页记录的 seq 和时间是构造值，**不表示 prodops 实际旧行的坐标**。

Playwright `1.58.2`、Node `v22.23.1`、系统 Chrome。完整 stdout 与运行时间在 `raw/real-identities-repro.stdout.txt`、`raw/real-identities-run-metadata.txt`。两版本 × 四情形结果均为全部 12 个 assistant marker 可见；分页情形也显示两条 older-page 输出：

| Renderer | 刷新首屏 | 实时到达 | 向上翻页 | 页内 session_id 切换 |
|---|---|---|---|---|
| r57 `8426af56` | 12/12 可见 | 12/12 可见 | 12/12 当前 + older 输出可见 | 12/12 可见 |
| r59 `8d39be9b` | 12/12 可见 | 12/12 可见 | 12/12 当前 + older 输出可见 | 12/12 可见 |

当前响应 JSON 和分页 JSON 在 `raw/refresh-page-latest.json`、`raw/realtime-page-latest.json`、`raw/pagination-page-latest.json`、`raw/pagination-page-3907.json`、`raw/session-switch-page-latest.json`。`raw/real-identities-manifest.json` 记录精确/占位字段边界。

## 代码定位与分类

- History 页面对每条 JSON event 解析后设置 `_history={generation,seq,event_id}`、`_hubSeq`，以 **Hub generation** 设置 timeline epoch，再调用 `handleIncomingEvent()`：`view-service/renderer-canary-sse.html:4681–4688`。历史分页点击把 `historyBefore` 作为 `before` 传回同一 endpoint：`4653–4660, 4725`。这两处没有读取 payload `session_id` 来筛行或重置历史。
- 实时 SSE 的 event id epoch/seq 在 `onmessage` 处理，和 history high-water 做幂等/缺口管理：`5199–5260`。这是 Hub stream epoch，不是 payload `session_id`。
- Turn grouping key 为 `ev.turn_id || ev.delivery_id`：`2899–2901`；事件分流到 turn group 在 `3691–3715`；`rebuildTurn()` 单独取 `assistant_text` 行及终态 `state_change`，完成态不删除 assistant 区：`4001–4025`。payload `session_id` 只作为 `stableAssistantItemKey()` 的 threadId 候选（`2907–2912`），且必须同时有 threadId、turnId、itemId 才参与 item 幂等键；Claude 本任务事件无 item_id。因此 session ID 变化不会按会话过滤/清空输出。真实 turn ID 匹配的 7 个 admitted 输入会与 Claude 内容归同 turn；queued 空 turn 输入以 delivery 独立成卡，也不阻断 Claude 输出。
- History Hub Page 的查询键是 owner/agent/session_name/generation，最新页按 seq 反序 LIMIT 100，再按升序返回，额外受 4 MiB 限制；源码 `internal/store/session_history.go:718–805`。相同 storage key/generation 的 47 条远低于限制，prodops 补报未发现该窗口内 generation 更换；record_bytes/gap 的进一步读数仍待到达。

因此在本轮真实身份形状下，**a（payload session_id 导致 Hub query filter）与 b（renderer session/turn/epoch 过滤）均未得到支持**。下一步需用 prodops 即将交付的 record_bytes/gap 和真实 API/浏览器 HAR 核查首屏响应是否实际含全部 assistant 行，另核对 r57 单文件替换件是否仍与报告的 SHA 对应（本地 repo r57 与 r59 SHA 已分别精确匹配给定值）。如果故障时 body 完整但 DOM 缺文，需按具体 event_id/seq 做现场重放，当前 fixture 不产生缺失，不能猜改 renderer。

## 建议回归与语义变异

当前复现脚本已形成非变异的基线断言：每版三种路径与 session-switch 全部 12 个 assistant marker 必须出现在 timeline；分页还需保留两个旧页 marker。建议若未来从现场快照复现出缺陷，再把对应实际 event shape 纳入永久回归。

1. `REAL_SHAPE_HISTORY_OUTPUTS_VISIBLE`：空 session_id + queued 空 turn 输入、7 个 admitted/output turn 关联、Claude 非空 session_id、completion 的同键 history 全量回放，断言 assistant 12/12。**一刀变异：**在 history dispatcher 加 `ev.session_id===当前 payload session` 过滤；assistant 12 条应全丢，断言失败。
2. `REAL_SHAPE_LIVE_OUTPUTS_VISIBLE`：输入 history 后实时发送 Claude-side rows，assistant 12/12。**一刀变异：**dispatch 对 session_id 相较输入基线变化时 reset conversation；断言失败。
3. `REAL_SHAPE_PAGINATION_PRESERVES_BOTH_PAGES`：同 generation 分页切换 payload session IDs 后当前与 older markers 均保留。**一刀变异：**将 payload session_id 变化错误处理为 epoch change 并清 timeline；旧页/新页保留断言失败。
4. `TURN_ID_JOIN_IS_EXACT`：admitted/input-output 7 个共同 turn 合并，queued `turn_id=""` 输入不吞输出。**一刀变异：**对空 turn_id 使用全局常量作为 turn group key；多条 queued 输入错误合并，输入 marker 数量/独立性断言失败。

等待 prodops `75bb84bb8f92` 的 payload key shape、generation/gap、record_bytes 后，补入证据再判断是否应改 Hub、renderer 或 helper。当前未实施产品修复。
