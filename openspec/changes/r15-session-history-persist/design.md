# Design

## Context

基线为 `1e17255f28ced042784337903914ca8c126ef4cf`（r48）。Hub 当前在内存 `eventRing` 保存 helper 上送的结构化 `session_event`；Console mux 与 View SSE 从 ring 建快照并续实时事件。Hub 重启失去进程内状态，helper 断线会关闭 terminal 并清环；现有序号含进程 epoch，不能直接当作跨进程持久游标。r48 中没有持久会话事件表。以下代码锚点均是该基线的文件:行号；实施期间若行漂移，须按语义复核，不沿用过期行号。

## Goals / Non-Goals

- 目标：以结构化会话事件为单位，为每个 Agent 保存有容量与 TTL 限制的历史；Hub 重启和 helper 重连后可按现有授权回放。
- 目标：定义持久序号、切点、gap、单条拒收、淘汰、撤权清理、数据库写失败语义和回退处理。
- 非目标：保存任意 terminal_output 原始帧或 Hub 日志；由 `session_delivery`／`message` 回填猜测历史；扩大读取权限；本设计授权实现之外的迁移执行、部署或环境操作。

## 默认关闭开关（r49 新功能通则）

开关名定为 `DW_SESSION_HISTORY_PERSIST_ENABLED`，Hub 启动时只在环境变量原始值**精确等于**字符串 `"1"` 时启用；未设置、空串、`0`、`true`、带空格的 `1` 或其它值都关闭。与 R5 的 `DW_OPENSPEC_READ_ENABLED`、R6 的 `DW_OPENSPEC_REVIEW_ENABLED` 同一关闭优先口径（R5 `openspec/changes/r5-render-openspec-readonly/design.md:48`；R6 `openspec/changes/r6-coordinate-review/design.md:78`）。开关在进程启动时采样，改变环境变量须重启 Hub 生效，不运行时热切换。

关闭时，`handleSessionEventEnvelopeContext` 走 r48 原有代码路径：不向 `session_history_event` 分配持久 seq、不插入/更新历史事件数据，不改 `eventRing`、现有 event seq、mux/SSE 消息体或推送顺序；这些既有事件输出按 r48 行为逐字节一致。新历史分页接口固定返回 HTTP 503、机器码 `history_feature_disabled`、消息「未启用」，不读取历史正文、数量或 cursor。0075 迁移可以安装空 schema；独立运行状态表只记录开关状态与关闭区间，不写事件历史。R15-006 撤权/删除的强制正文清除仍执行，属于隐私义务，不受功能开关抑制；关闭时 TTL 扫描可暂停，重新启用后必须先清过期记录再开放历史读取。

开—关—再开按进程重启处理：开态已存事件保留并继续受 TTL/容量/撤权规则约束；切到关态后到再开启前的事件不回填，关态期间历史接口明确未启用。单独的运行状态控制记录标注关闭起止；再开启时在旧记录和新写入间追加 `feature_disabled` gap 标记，generation 与持久 seq 均接续，不伪造缺失事件、不把关闭期解释成连续历史。gap 边界以前的历史仍可读；发生过关闭区间的分页/实时接续必须显示缺口。若从未有过历史，则开启后从空历史正常开始。

## r48 代码与数据现状

- `internal/m8/terminal.go:480-503,522-558`：helper `session_event` 经校验并剥除客户端 provenance 后进入 `publishSessionEvent`；当前分配进程内 seq、写 ring、再解锁扇出 mux/SSE 消费者。持久化挂点应放在 497 行调用与发布之间。
- `internal/m8/event_ring.go:3-9,45-57,79-92`：内存 ring 每会话 16 MiB、Hub 128 MiB／128000 条预算，超额从最旧端删；与 R15 磁盘预算分开。
- `internal/m8/terminal.go:873-904,922-953,955-1003`：View SSE 在同一 `h.mu` 临界区注册 reader、取 H、复制 ring；cursor 是 epoch-seq，epoch 不同为 `epoch_changed`，ring 覆盖不足为 `overflow`。
- `internal/m8/console_mux_frames.go:150-184`、`internal/m8/console_mux_transport.go:689-727`：mux 在锁下取得快照并注册 reader，之后按客户端 cursor 选择 replay；无 terminal source 当前无法建立该源。
- `internal/m8/terminal.go:177-203`、`internal/m8/hub.go:1255-1265`：连接结束的清理会调用 `closeTerminalLocked`，清空 ring、关闭 reader 并删除 terminal state；R15 改为仅清热态，不删除持久历史。
- `internal/store/sqlite.go:57-90,99-110,170-219,634-656`：主库启用 WAL、`synchronous(NORMAL)`、busy timeout 5000ms、单写连接；迁移文件按名排序并逐项记账。r48 内置迁移目前到 `0071_coordinate_review.sql`。本单编号约定 `0075_session_history.sql`；R14 的 0072、R10 的 0073、R9 的 0074 与本单冲突的发布排序需集成时统一。
- `internal/store/migrations/0027_session_delivery.sql:1-19`：`session_delivery` 存投递 envelope/状态及目标索引，不等价于每条结构化事件的完整历史。
- `view-service/renderer-canary-sse.html:413-425,1099,1769-1792,1849-1875`：timeline 已是独立滚动容器；现有逻辑负责贴底和用户上翻后停止跟随，没有向上历史读取、prepend 锚点或到底/gap 提示。

## 存储结构与迁移

### 选择 A：SQLite 专用历史表（推荐）

在 `internal/store/migrations/0075_session_history.sql` 只 `CREATE TABLE`／`CREATE INDEX`，不 `ALTER` 或改写旧表。建议逻辑结构如下（字节数按 UTF-8 完整序列化事件加本表持久元数据计；1 MB=1,000,000 bytes、64 MB=64,000,000 bytes、2 GB=2,000,000,000 bytes；此计量口径是建议冻结项，物理 SQLite 开销另计）：

```sql
CREATE TABLE session_history_generation (
  owner_key TEXT NOT NULL,
  agent_key_id TEXT NOT NULL,
  session_name TEXT NOT NULL,
  generation TEXT NOT NULL,
  next_seq INTEGER NOT NULL,
  oldest_seq INTEGER NOT NULL DEFAULT 0,
  newest_seq INTEGER NOT NULL DEFAULT 0,
  gap_through_seq INTEGER NOT NULL DEFAULT 0,
  retained_bytes INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (owner_key, agent_key_id, session_name)
);
CREATE TABLE session_history_event (
  owner_key TEXT NOT NULL,
  agent_key_id TEXT NOT NULL,
  session_name TEXT NOT NULL,
  generation TEXT NOT NULL,
  seq INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  received_at_ms INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  payload_json BLOB NOT NULL,
  record_bytes INTEGER NOT NULL,
  PRIMARY KEY (owner_key, agent_key_id, session_name, generation, seq),
  UNIQUE (event_id)
);
CREATE INDEX idx_history_owner_time
  ON session_history_event(owner_key, received_at_ms, agent_key_id, session_name, seq);
CREATE INDEX idx_history_agent_time
  ON session_history_event(owner_key, agent_key_id, session_name, received_at_ms, seq);
CREATE INDEX idx_history_expiry
  ON session_history_event(received_at_ms, owner_key, agent_key_id, session_name, seq);
CREATE TABLE session_history_gap (
  owner_key TEXT NOT NULL,
  agent_key_id TEXT NOT NULL,
  session_name TEXT NOT NULL,
  generation TEXT NOT NULL,
  after_seq INTEGER NOT NULL,
  before_seq INTEGER,
  started_at_ms INTEGER NOT NULL,
  ended_at_ms INTEGER,
  reason TEXT NOT NULL,
  PRIMARY KEY (owner_key, agent_key_id, session_name, generation, started_at_ms)
);
CREATE TABLE session_history_runtime_state (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  enabled INTEGER NOT NULL,
  changed_at_ms INTEGER NOT NULL,
  disabled_since_ms INTEGER
);
```

generation 行是当前身份/会话代际的唯一高水位和淘汰边界；撤权/删除时必须清事件正文与可关联索引，代际标记更新或移除，使同名重建获得新随机 generation。稳定排序键为 `owner_key, agent_key_id, session_name, generation, seq`；事件 ID 在接收时生成，作为幂等键。具体字段/约束与现有身份模型的映射在实现前由实现者复核，不允许以客户端自报 owner 作为键来源。

`session_history_gap` 只保存非正文缺口边界；运行状态表是控制元数据，不是事件历史。Hub 启动时比较上次记录的 enabled 状态并更新当前状态：关闭启动记录 `disabled_since_ms`，再启用时将此区间收束为所有已有代际的 gap 边界，并在新事件写入后补齐 `before_seq`。这些状态行不能包含事件内容。异常关机后状态仍可从上次持久状态推断；启用进程的普通重启不产生 `feature_disabled` gap。

建议同一个向前事务内：读取并锁定 generation；按序分配 `seq`；插事件；更新高水位和计量；按 TTL、Agent 配额、全局配额顺序删旧记录并推进 gap；提交。回滚则不产生已分配的持久 seq。逻辑计量不把 SQLite freelist、页碎片或 WAL 算作事件容量；主库文件可因此超过 2 GB。

### 主库 2 GB 与独立文件的影响（待组长定）

把全局 2 GB 历史压在现网 `workpulse.db` 上会让主库长期接近或达到该容量之外的体量；每次新增／淘汰仍经单写者和 WAL，WAL 峰值、checkpoint 写放大与磁盘临时空间需按活跃写入率实测。SQLite 备份若为在线一致性备份仍需复制/检查大主库，备份窗口和恢复耗时会随主库变大；大量逐行淘汰产生 freelist，`VACUUM` 需要长时间独占/重写并额外接近一份库大小的临时空间，可能影响主库写可用性。普通淘汰不应触发频繁全库 VACUUM；需监控 DB/WAL/free pages，维护窗口另决策。

建议为历史使用同机、同 SQLite 实现的独立 `workpulse-history.db`，隔离 WAL、备份窗口、VACUUM 与主业务写锁；其表仍用独立迁移版本/账本或经核实的独立 schema runner。代价是跨库事务不能原子覆盖身份撤权与历史删除，需要先禁止授权/读取、再同步删除并可重试；备份与还原也要作为一组操作管理。**待组长定：**A1 主库内独立表、享有同一事务/撤权原子性，但承受主库/WAL/备份膨胀；A2 独立库文件（推荐），隔离容量影响但接受双库清理编排与备份一致性设计。批准前仅冻结“专用历史表”结构，不冻结文件位置。

## 写入路径、可见性与实时延迟

具体事件流是 `handleSessionEventEnvelopeContext` 的 helper `session_event`，不是 `terminal_output`。在 `terminal.go:497` 清理 provenance 后、`publishSessionEvent` 之前完成序号分配和同步持久事务；成功后才调用 `publishSessionEvent`，因此顺序为：**数据库事务提交 → ring → mux/SSE/其他消费者**。持久序号随事件传入发布层；ring 的进程 epoch 保留作热态诊断，不作历史游标。

若校验拒收（大于 1 MB），事件不进入持久表，产生只含稳定键、seq、原因码、时间的 gap 元数据；不得记录正文。数据库写失败则事务回滚、不把事件发布为成功的实时历史事件，不 ACK 该事件的持久接收；调用方按现有事件传输的失败/重试机制显式报告 `history_persist_unavailable`，客户端可见不可用/gap 状态并保留重试能力。禁止吞掉 error 后照常发 ring/SSE。若当前 helper 协议不能负向确认该消息，需先扩展可重试确认契约再实现，列为 T-03 前阻塞项。

同步 SQLite 写使从事件接收到发布的额外延迟约等于事务、索引及必要淘汰耗时，并受 busy timeout 约束；不得假设可忽略。增加写入耗时直方图、失败计数、队列无关的 commit 延迟告警，不打正文。测试比较同一负载下写前/写后端到端 p50/p95/p99 与最大值，覆盖 1KB、近 1MB、并发多 Agent、WAL checkpoint、超时/磁盘只读；报告绝对值和增量，限值待组长结合现网 SLO 定。

## 容量、淘汰与撤权清理

- 单事件按完整序列化记录校验，超过 1,000,000 bytes **拒绝整条、不截断**；记录 gap，实时流按上节失败语义处理。恰好等于限制可接收。
- 每 Agent（同一 owner 与稳定 Agent key 下的会话合计）≤64,000,000 bytes；全局 ≤2,000,000,000 bytes；最长保留 30×24 小时，取 `received_at_ms`。逻辑字节是 `record_bytes` 汇总，SQLite 页、索引、WAL、freelist 另算存储监控。
- 每次写事务按顺序执行：先删除所有到期记录；再若 Agent 超额，按 `received_at_ms, seq, session_name` 升序删除该 Agent 最旧记录至配额内；最后若全局超额，按 `received_at_ms, owner_key, agent_key_id, session_name, seq` 升序跨 Agent 删除至全局上限。容量与 TTL 谁先触发都只删最旧记录；同一时间戳用上述稳定键消除不确定性。每次淘汰同步推进对应代际 `gap_through_seq/oldest_seq`，不保存逐事件 gap 日志。
- 单批最多 500 行删除；同一写事务可重复小批到恢复限制，但单事务总预算 2 秒，超出则回滚本次新事件并显式报告容量维护失败；后台 TTL 清理每批最多 500 行、批间让出写锁，至少每分钟启动一次并在启动后异步启动，不做启动时全表扫描。阈值/间隔属实施建议，若基准测量无法满足须列待组长定。
- 撤权 / 删除的位置：在现有身份凭据吊销、账号解绑/删除的**权威 store 事务/生命周期服务**中，先令 Agent 不可授权，再同步清该身份的历史事件、代际行和索引，再提交；读取授权每次重新校验当前 active owner/key。若采用独立库导致无法跨库原子事务，则先提交权限禁用，再同步删除历史，删除失败保持禁用并进入可观测重试队列；不可先恢复访问。具体吊销入口由实现勘察确定并登记文件:行。内存 ring、mux reader 和 renderer cache 同时清理/撤销，不能只删除 SQLite 行。

## 序号、代际、重启与分页

- `generation` 是随机不可复用的历史代际，代表同一稳定 owner/Agent/session 身份生命周期；撤权、身份删除或身份重新绑定时换代。`seq` 是该代际内从 1 开始的持久单调整数，由同一事务原子分配，不能复用；eventRing 的 `eventEpoch` 与其 `eventSeq` 是进程态兼容层，不得映射为持久 generation/seq。
- 新订阅在 Hub 锁内先注册 live reader，再从持久层取代际与 H（或用等效同事务/锁协议封闭竞态）；已提交至 H 的记录按 seq 回放，live 只放行 `seq > H`，重叠以 generation+seq 去重。不能先查 H 再注册 reader。重启后读取库内 generation/H，纯 Hub 重启或同 Agent helper 重连不会换代，不需 resync；generation 变更或 seq 小于 `oldest_seq`、不大于淘汰边界时发 history gap。数据库不可用返回显式 unavailable，不伪装空历史。
- 向上分页仍按 PM-R15-09：一页最多 100 条事件、JSON 序列化响应体≤4 MiB、服务端查询 deadline 2 秒。首屏按 `seq DESC LIMIT 100` 取最新页后反转升序返回，并同时返回 H、generation、`has_older` 和 `gap_before`；向上使用 exclusive `before_seq`，按 `seq < before_seq ORDER BY seq DESC LIMIT 100` 取连续前缀，再反转。累计序列化响应长度时如下一条会超 4 MiB，则不放入该条，cursor 指向它的 seq，下一页从该 seq 前继续，不跳过；最大合法单条小于页预算，故空页只有无更早记录或 deadline/错误。
- 游标绑定认证后的 Agent、generation、`before_seq`，不包含/依赖 Hub epoch。过期或被淘汰游标返回边界 gap 与最旧可读 seq；无更早且无 gap 才 `has_older=false`。首屏取 H 后从 H+1 接实时；翻页不移动 H。页面 prepend 以首个可见 event_id/seq 和像素偏移恢复位置，实时 append 不得改变锚点。

## 权限矩阵

| 主体/操作 | 读历史 | 分页游标/元数据 | 写/删/改历史 |
| --- | --- | --- | --- |
| Agent 所属账号中通过现有 Console 授权的本人 | 允许，只限所授权 Agent | 仅返回该 Agent 的 generation、边界、cursor、计数 | 禁止客户端直接写删改 |
| 其他账号/其他 Agent | 一律拒绝，优先与不存在 Agent 使用同一 404/空拒绝外观，不泄露正文、计数或游标 | 不返回 | 禁止 |
| 平台管理员但不属于该 Agent 授权范围 | 拒绝；R15 不加跨 Agent 特权 | 不返回 | 禁止 |
| 已吊销、已解绑或已删除身份 | 立即拒绝；旧凭据/cursor 不可重用 | 不返回旧代际信息 | 生命周期清理器可删，留非正文审计 |

读取接口在 HTTP 路由层先解析登录主体，再通过现有 Console Agent ownership/auth 查询取稳定 owner 与 Agent key；请求中的路径 Agent 仅为查找键，不是授权凭据。Owner 归属变更视为撤销旧授权：先使旧 owner 对该 Agent 的读取失效并清旧代际正文；新 owner 只从新 generation 开始读，**不得将旧历史转给新 owner**。是否需要在转移后保留旧 owner 可读的历史是 **待组长定**；在裁定前按最小披露策略彻底清除旧代际。

## 兼容、迁移与回退

0075 只创建新表/索引；迁移正式编号在集成时协调 0072 R14、0073 R10、0074 R9。只从迁移后实际收到的合规结构化事件累计，不回填 `session_delivery` 或 `message`。应用回退不 DROP 历史表。**r48 Hub 在带 0075 库上的启动兼容性不可由代码阅读推断：必须在隔离副本上用精确 r48 二进制/源码版本和带迁移数据库做启停实测，记录启动结果、表/行是否改动、完整日志和库 hash；生产及预发不作此试验。**

回退期间 r48 不写历史；再升级时沿用回退前的 generation/seq，新写事件继续累加 seq，但记录回退开始/结束时刻为显式 history gap 区间。不得把该空洞解释成连续，旧 cursor 跨过空洞时返回 gap，然后从 gap 后实际记录续读；是否在回退期间拒绝新历史读或呈现有界降级状态列为待组长定。历史表按 TTL/容量/撤权规则继续清理。

## 测试计划（设计验收，不在本设计任务执行）

每项同时做正向和反向场景；反向包括拒绝/失败边界，且每条目标变异必须让对应断言转红。所有 TTL/时间容量测试使用可注入虚拟时钟，禁止 wall-clock sleep。并发 race 采用穷尽分片：对每个列明的交错点枚举确定性调度分片并各跑 `-count=3`；固定随机 seed 和分片清单写入日志。

| 需求 | 正向 / 反向场景 | 测试层 | 目标变异（必须红） |
| --- | --- | --- | --- |
| R15-001 单位/新表 | 事件逐条升序持久并读取；旧 delivery/message 不被回填；正文/元数据计量边界 | store 集成 + Hub 集成 | 将存储单位改成 round 聚合或从 delivery 回填，断言条数/正文来源失败 |
| R15-002 重启/重连 | Hub 重启和 helper 重连后续 seq；generation 真实改变时旧游标 gap | store+Hub 进程集成 | 启动时重置 seq 或把 epoch 改变当 gap，连续性断言失败 |
| R15-003 切点 | 并发写入与 subscribe/query H 交错，对照源 ID 恰一次；反向注入订阅后丢事件应显 gap | Hub race 穷尽分片 `-count=3` | 把 register reader 移到查 H 之后，丢失计数断言失败 |
| R15-004 权限 | 本人授权读取；他人、伪造 owner/session/cursor、管理员越权拒绝且无侧信道 | HTTP handler + 授权集成 | 移除服务端 ownership 检查，跨 Agent 负例变绿/敏感断言失败 |
| R15-005 上限/TTL | 1,000,000 bytes 接受，超 1 byte 拒绝不截断；Agent/global 逐次淘汰；30 天边界虚拟时钟 | store 边界/性质测试，容量和 TTL 用虚拟时钟 | 将 `>` 改 `>=` 或删除过期条件，边界样本断言失败 |
| R15-006 撤权 | 吊销前本人可读；后续旧凭据和旧 cursor 均不可读，表正文/索引被删；同名重建新代际 | store+HTTP 授权事务集成，撤权 race 穷尽分片 `-count=3` | 删除异步化或遗漏正文 DELETE，提交后读取/行数断言失败 |
| R15-007 迁移/回退 | 空库及有旧业务表迁移成功、不改旧表；回退保留历史；隔离实测 r48 起动 | migration 集成 + 隔离兼容矩阵 | 在迁移加入旧表 ALTER/DROP，schema snapshot 断言失败 |
| R15-008 分页/UI | 首屏、100 条和 4MiB 截断连续、2 秒 deadline、最早/缺口提示、锚点保持及实时 append；超预算、跨代际 cursor、越权拒绝 | API handler + 浏览器回归 | 删除 exclusive 边界、cursor 前进或锚点恢复，重复/跳过/偏移断言失败 |
| 写失败语义 | 注入 busy/只读/超时，事件不被标记已持久、不静默进 live，客户端看到 unavailable 并可重试 | Hub/store 故障注入 | 吞掉写错误并继续发布，持久/live 对照断言失败 |
| 性能 | 同负载旧/新分支对比 p50/p95/p99/max；多 Agent、大事件、checkpoint 和淘汰测量 | 隔离性能基准 | 禁用事务延迟采样或提前发布，延迟/持久完整性断言失败 |
| 默认关闭开关 | unset/空/`0`/`true`/` 1` 均关闭，精确 `1` 才启用；关闭时比对 r48 事件 bytes、ring、序号及 mux/SSE 次序；新分页接口返回固定「未启用」；开—关—再开保留旧记录、关态事件不回填并出现 gap | Hub 单元 + store/HTTP 集成，重启子进程配置矩阵 | 将开关判定变异为恒 `true`，默认关闭格必须红；删除 gap 标记时切换期连续性断言必须红 |

目标变异逐条留原始红测日志；本设计不声明任何实现或验收 PASS。预算数值、批量删除锁持有时间及热路径 SLO 若测量不能支持，列入待组长定，不在实现中擅改定值。

## 待组长定

1. 历史数据落 `workpulse.db` 专用表还是独立 `workpulse-history.db`（建议独立文件，见上节）；兼容清理的跨库事务方案。
2. 主库写入失败/单条超大时当前 helper 协议能否提供可靠负确认和重试；否则先定协议升级窗口。
3. 每次清理 500 行、后台每分钟、事务 2 秒建议值及生产实时延迟 SLO。
4. Owner 归属变更旧历史一律销毁的临时默认策略，及回退期间历史读的呈现策略。
5. 逻辑容量按十进制字节且不计 SQLite 页开销、event_id/generation 与现有身份字段的最终 schema 对照。
