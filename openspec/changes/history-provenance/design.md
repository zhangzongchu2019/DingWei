# 实现设计

## 1. 读码基线与边界

基线为 R60 读码锚 `9818b979`；历史路径也以先前提案 86c86025 的 `329eb9f9` / `d2cc47c7` 文件比对为准。产品实现须在 R60 最终锚后另派，本 change 仅定义设计，不改 Hub、renderer 或 schema。

| 坐标 | 当前职责 |
| --- | --- |
| `internal/m8/terminal.go:570-604` | helper `session_event` 入口先剥 helper provenance，再调用 `SessionHistory.Append`，随后发布实时事件。 |
| `internal/m8/terminal.go:823-896` | `publishInputObservation` 与 `inputObservationEvent` 生成 canonical `input_posted`；只发实时 SSE，primary 的可信 Hub provenance 在这里可取得。 |
| `internal/store/session_history.go:23-29,107-124` | 当前 history 每事件 1 MB、每 agent 64 MB、全局 2 GB、30 天保留；独立 history SQLite，单连接。 |
| `internal/store/session_history.go:154-190` | history 自有 `history_migrations/*.sql` 与 digest ledger，和应用库 `internal/store/migrations` 分开。 |
| `internal/store/session_history.go:267-320,382-450` | bounded group-commit Append；`appendBatchTx` 在同一 SQL transaction 写事件和更新 byte counters。 |
| `internal/store/session_history.go:565-690,857-930` | retention、agent/global 容量淘汰与 agent 删除队列。sidecar 生命周期应并入这些同一事务边界。 |
| `internal/store/session_history.go:697-838` | `Page` 读取 100 条以内页并保持 generation/high_water/gaps/before_seq envelope。 |
| `internal/m8/terminal.go:291-356` | `/view/{session}/history` 负责会话 owner 鉴权、调用 Page、原样 JSON 返回。 |
| `tools/sessionhelper/sessionhelper/unified_queue.py:183-205,239-250` | admitted `user_input` 带 delivery_id、turn_id 和可信 Hub delivery 的 `origin_delivery_id`；排队 echo 的 turn_id 为空。 |
| `view-service/renderer-canary-sse.html:2093-2158,2227-2245,2716-2749,3620-3655,4745-4810` | 当前 renderer 解析 history event，按现有 trusted＋Hub attestation 规则绘图；不得从相邻事件推断。 |

N-3 仅保存可信 primary provenance 的最小 sidecar，不另存完整 input_posted 正文、不追加第二条 canonical 历史正文。实时路径原有行为保持；历史侧只在同一 delivery 的 admitted user_input 上精确增强。

## 2. Sidecar schema、关联键与 migration 序号

在 history DB 增加 `session_history_provenance` sidecar，主键为 `(owner_key, agent_key_id, session_name, generation, delivery_id)`。建议列：`source_message_id`、`source_kind`、`display_name`、`trust_decision`、`attested_by`、`observed_at_ms`、`expires_at_ms`、`record_bytes`。仅存 Hub 从 canonical envelope 派生且 `role=primary`、`trust_decision=trusted` 的记录；不存 payload/body、open_id、bot_channel_id、客户端自报 provenance。

精确关联由五元组约束：目标 `(owner_key, agent_key_id, session_name, current generation)` 与可靠 helper echo 的 `origin_delivery_id` 必须唯一命中 sidecar `delivery_id`；helper echo 的 `delivery_id` 也必须与其 `origin_delivery_id` 完全相等。只处理 `kind=user_input`、`data.lifecycle=admitted` 且 `turn_id` 非空的 echo。任何缺 ID、ID 不相等、跨 session/generation、角色不是 primary、重复冲突或来源不可信都不附加 provenance。helper 事件中自带的 provenance 在既有 `stripClientProvenance` 步骤无条件剥除。

新 DDL 放入 `internal/store/history_migrations/0078_history_provenance.sql`。这是独立 history DB 的 migration stream，当前应用库 `internal/store/migrations` 最新为 `0077_outbound_intent.sql`。实现前需对 R60 最终锚复查所有 migration；若 R60 新增任何占用 0078 的有序迁移，N-3 顺延至下一个未使用号（如 0079），不重排、不修改已发迁移。history DB 的 digest ledger 仍按文件名逐项校验。

索引：主键覆盖唯一精确关联；另建 `(expires_at_ms, owner_key, agent_key_id, session_name)` 清理索引。若需要按来源消息审计，再加非唯一 `(owner_key, agent_key_id, source_message_id)`，避免多余索引常驻。禁止仅用 message_id、session label 或全局 delivery_id 关联。

## 3. 写入与事务边界

1. `routeToSession` 已先成功 `EnqueueSessionDelivery`，随后调用 `publishInputObservation`（`hub.go:4495`）。在该 observation 分支中，Hub 从自身原始 envelope 生成 canonical provenance；在发布实时 SSE 前，调用 history store `RememberInputProvenance`，写入 sidecar。此操作只对 `input_posted` primary 且 history enabled 生效。sidecar 写入失败只丢历史图标证据并记录结构化错误；不能阻断、改写或重试原始投递。
2. helper 的 user_input 到达时，Hub 保持现有“剥掉客户端 provenance”顺序，把 `(owner,agent,session,generation,origin_delivery_id)` 作为 Append 的可选关联参数。扩展 group-commit request，使 `appendBatchTx` 在本次 SQL transaction 内查询精确 sidecar、生成标准 `provenance` 对象、重新计算实际 payload/record_bytes，再插入增强后的 event、更新 counters 并删除已消费 sidecar。event 写入、sidecar 消费与 byte accounting 同 commit；transaction 回滚则三者均不变。
3. sidecar 与源 delivery DB 不是同一 SQLite 数据库，不能承诺跨库原子性。以“先 durable enqueue，再写 sidecar，再发布 input_posted，再投 helper”为顺序；sidecar 写失败 fail closed。重放 input_posted 采用同键幂等插入；若同键不同 source message 或 provenance 内容，则标为冲突并保持不可用于渲染，禁止 last-write-wins。
4. 如果 helper echo 先于 canonical sidecar 到达，当前 Append 持久化无 provenance 的原 user_input；之后不追溯更新、不靠第二次历史回放修饰。Hub 顺序通常先发布 primary observation 再 drain delivery，但异常乱序仍 fail closed。此选择避免后到的、歧义的来源改变已提交历史。

## 4. 保留期、容量与删除

- sidecar 的 `expires_at_ms` 最长为 `observed_at + SessionHistoryRetain`（当前 30 天）；消费后立即删除。generation 变更、history 禁用清理、过期批次和 agent 数据删除均在相同 history DB transaction 中清理 sidecar。
- 单 sidecar 记录上限 4 KiB；正文和身份标识不入 sidecar。Sidecar bytes 必须计入现有每 Agent 64 MB 与全局 2 GB history budget，而非作为额外无限预算。写 sidecar 前检查当前 counters；没有预算时不存 sidecar、实时投递继续，后续刷新不显示图标。
- 写入、消费、删除与 quota eviction 更新 `session_history_generation.retained_bytes` 及 `session_history_runtime_state.retained_bytes`。N-3 实现必须扩展过期、agent/global eviction 选择器：清除 payload event 同步清掉对应 provenance；尚未配对的 sidecar 也可按最早 `received_at_ms` 被淘汰，不生成含来源/正文的历史 gap。清理 sidecar 不得将历史容量统计变负或留下孤儿。
- event payload 经 provenance 增强后仍受 1 MB 单事件限制。超限时该 event 按既有 oversize/gap 处理，并在同一失败清理路径移除或保留可重试 sidecar 的选择须定稿；推荐回滚本次消费、保留 sidecar 至 TTL，但不得重复写入一条事件。

## 5. 存量与分页、renderer 回放

存量 history row 与无 sidecar 的 helper echo 保持原 JSON，不回填、不按 `data.source`、display_name、正文、delivery_update、SSE 顺序或相邻卡片猜来源。缺来源、关联不唯一、sidecar 过期、信任链失败一律不显示来源图标。

`Page` 与 `/view/{session}/history` 保持现有分页 envelope：`generation`、`high_water`、`events[{cursor,event_id,received_at,event}]`、`gaps`、`has_older`、`before_seq`、`gap_before`。provenance 嵌在 event JSON 的 `user_input.provenance`，不增加必需的新顶层字段，也不复制 `input_posted.payload`。GET 鉴权和 generation/before 分页语义不变。

renderer 只对已经通过 `normalizeEvent` 的标准 provenance 画图：`kind=agent` 或 timer 归为 agent、`trust_decision=trusted`，且 `attested_by=hub` 或既有 attestation chain 含 Hub。禁止把单独 `data.source=agent/timer` 升格为可信 provenance。history 与 live 共用同一个 admitted `user_input` 卡时，按现有 canonical/card identity 去重，图标数恰为 1；实时 input_posted 内存已消失不影响 history replay。

历史对象格式说明：renderer `329eb9f9` 的 history parser 对 `row.event` 走 `JSON.parse`，而现行 `/history` 返回对象型 `json.RawMessage`；该版本不能消费当前对象 envelope，是已有不兼容。`d2cc47c7` parser 已支持对象与 JSON string。N-3 保持既有 API，不宣称修复该旧 renderer；如需回放兼容须另案扩展 renderer/API 协商。新字段不改变旧 renderer 对已可解析实时事件的行为。

## 6. 实现回归与删保护

1. 使用真实 Hub 输入与 helper admitted event，Agent 与 timer 各一例，保存 SSE id、message_id、delivery_id、origin_delivery_id、turn_id、history DB row、GET response 和刷新后 DOM 读数；两例每卡 trusted 图标数均为 1。
2. 仅 queued echo 不增 provenance；admitted echo 在完全一致五元组关联时才增。复制 delivery、跨 session、跨 generation、不同 origin ID、copy/control observation、来源缺失/不可信与 duplicate conflict 均为 0 图标。
3. migration 升级、r60 同时新增 migration 的顺延、history quota 满、4 KiB sidecar 上限、TTL、global/agent eviction、禁用/删除队列和 Append transaction rollback 均有测试。
4. 变异删除 Hub sidecar 持久化或 user_input 附加字段时，Agent/timer DOM `icon_count==1` 业务断言红；删除 renderer Hub trust 判定时，未 attested fixture 的 `icon_count==0` 业务断言红；删除关联键任一组成或允许 delivery_id 与 origin_delivery_id 不同，精确关联格红；删除 TTL/quota cleanup 时预算/清理断言红。

## 7. N-3 实施定稿（2026-11-18 派单）

- 实施基点main 206d41b26dab646c0e50a111eed5a45ccf7c72f6；首提交c3b3db27c原样带入本设计，不修改历史设计字节。
- history migrate以fs.Glob、sort.Strings逐文件执行并校验同名digest，没有连续编号检查。采用设计指定的独立0078_history_provenance.sql；不改应用库0077。未来占号须先同步，不重排已发布文件。
- Sidecar增删通过history DB触发器在同事务更新generation/global counters；Append在原group-commit事务内精确查询、增强、计算实际record_bytes并消费。agent/global eviction选择器合并event和sidecar，queued事件被淘汰也同步删其精确delivery证据；sidecar本身淘汰不生成正文gap。
- 超限选择已按派单定稿：增强后超过1,000,000 bytes时返回既有oversize错误、回滚整个本次event/消费/counter，保留sidecar至TTL；既有gap路径继续，不追加第二条事件。
- helper关联字段遵循真实协议：kind/delivery_id/origin_delivery_id/turn_id在顶层，仅data.lifecycle在data；queued、空turn、不同ID、跨五元组或冲突证据均关闭图标。没有有效sidecar的JSON保留原字节，未追溯更新旧事件。
- 来源记录仅使用当前认证连接的owner/stable Agent ID快照，不在可能持有identity授权锁的route回调内再次借PG连接。无法取得目标稳定身份（例如当时目标未连接）或写sidecar失败时记录固定原因并fail-closed，无图标；不影响已经durable enqueue的投递，也不在后续echo里从正文/地址/临近卡片重建来源。
- SQLite事件和sidecar的事务原子性不延伸到应用库投递，二者仍独立；原可靠投递回执时序不变。history关闭时清sidecar，删除队列及TTL每批分别有界处理sidecar；恢复/迁移测试覆盖已有0001 ledger升级及digest拒绝。
