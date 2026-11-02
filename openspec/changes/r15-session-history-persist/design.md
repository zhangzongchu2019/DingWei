# Design

## Context

基线为 `1e17255f28ced042784337903914ca8c126ef4cf`（r48）。Hub 当前在内存 `eventRing` 保存 helper 上送的结构化 `session_event`；Console mux 与 View SSE 从 ring 建快照并续实时事件。Hub 重启失去进程内状态，helper 断线会关闭 terminal 并清环；现有序号含进程 epoch，不能直接当作跨进程持久游标。r48 中没有持久会话事件表。以下代码锚点均是该基线的文件:行号；实施期间若行漂移，须按语义复核，不沿用过期行号。

## Goals / Non-Goals

- 目标：以结构化会话事件为单位，为每个 Agent 保存有容量与 TTL 限制的历史；Hub 重启和 helper 重连后可按现有授权回放。
- 目标：定义持久序号、切点、gap、单条拒收、淘汰、撤权清理、数据库写失败语义和回退处理。
- 非目标：保存任意 terminal_output 原始帧或 Hub 日志；由 `session_delivery`／`message` 回填猜测历史；扩大读取权限；本设计授权实现之外的迁移执行、部署或环境操作。

## 默认关闭开关（r49 新功能通则）

开关名定为 `DW_SESSION_HISTORY_PERSIST_ENABLED`，Hub 启动时只在环境变量原始值**精确等于**字符串 `"1"` 时启用；未设置、空串、`0`、`true`、带空格的 `1` 或其它值都关闭。与 R5 的 `DW_OPENSPEC_READ_ENABLED`、R6 的 `DW_OPENSPEC_REVIEW_ENABLED` 同一关闭优先口径（R5 `openspec/changes/r5-render-openspec-readonly/design.md:48`；R6 `openspec/changes/r6-coordinate-review/design.md:78`）。开关在进程启动时采样，改变环境变量须重启 Hub 生效，不运行时热切换。

关闭时，`handleSessionEventEnvelopeContext` 走 r48 原有代码路径：不向 `session_history_event` 分配持久 seq、不插入/更新事件历史数据，不改 `eventRing`、现有 event seq、mux/SSE 消息体或推送顺序；这些既有事件输出按 r48 行为逐字节一致。新历史分页接口固定返回 HTTP 503、机器码 `history_feature_disabled`、消息「未启用」，不读取历史正文、数量或 cursor。历史库使用独立迁移目录和账本；关闭时运行状态控制表可记录开关状态与关闭区间，不写事件历史。R15-006 撤权/删除仍先撤销读取权，再异步清正文，属于隐私义务，不受功能开关抑制；关闭时 TTL 扫描可暂停，重新启用后必须先清过期记录再开放历史读取。

开—关—再开按进程重启处理：开态已存事件保留并继续受 TTL/容量/撤权规则约束；切到关态后到再开启前的事件不回填，关态期间历史接口明确未启用。单独的运行状态控制记录标注关闭起止；再开启时在旧记录和新写入间追加 `feature_disabled` gap 标记，generation 与持久 seq 均接续，不伪造缺失事件、不把关闭期解释成连续历史。gap 边界以前的历史仍可读；发生过关闭区间的分页/实时接续必须显示缺口。若从未有过历史，则开启后从空历史正常开始。

## r48 代码与数据现状

- `internal/m8/terminal.go:480-503,522-558`：helper `session_event` 经校验并剥除客户端 provenance 后进入 `publishSessionEvent`；当前分配进程内 seq、写 ring、再解锁扇出 mux/SSE 消费者。持久化挂点应放在 497 行调用与发布之间。
- `internal/m8/event_ring.go:3-9,45-57,79-92`：内存 ring 每会话 16 MiB、Hub 128 MiB／128000 条预算，超额从最旧端删；与 R15 磁盘预算分开。
- `internal/m8/terminal.go:873-904,922-953,955-1003`：View SSE 在同一 `h.mu` 临界区注册 reader、取 H、复制 ring；cursor 是 epoch-seq，epoch 不同为 `epoch_changed`，ring 覆盖不足为 `overflow`。
- `internal/m8/console_mux_frames.go:150-184`、`internal/m8/console_mux_transport.go:689-727`：mux 在锁下取得快照并注册 reader，之后按客户端 cursor 选择 replay；无 terminal source 当前无法建立该源。
- `internal/m8/terminal.go:177-203`、`internal/m8/hub.go:1255-1265`：连接结束的清理会调用 `closeTerminalLocked`，清空 ring、关闭 reader 并删除 terminal state；R15 改为仅清热态，不删除持久历史。
- `internal/store/sqlite.go:57-90,99-110,170-219,634-656`：主库启用 WAL、`synchronous(NORMAL)`、busy timeout 5000ms、单写连接；迁移文件按名排序并逐项记账。r48 内置主库迁移目前到 `0071_coordinate_review.sql`。R14/R10/R9 使用主库迁移号 0072～0074；R15 不进入该序列。
- `internal/store/migrations/0027_session_delivery.sql:1-19`：`session_delivery` 存投递 envelope/状态及目标索引，不等价于每条结构化事件的完整历史。
- `view-service/renderer-canary-sse.html:413-425,1099,1769-1792,1849-1875`：timeline 已是独立滚动容器；现有逻辑负责贴底和用户上翻后停止跟随，没有向上历史读取、prepend 锚点或到底/gap 提示。

## 存储结构与迁移

### 选择 A：SQLite 专用历史表（推荐）

在独立库 `workpulse-history.db` 中由专用迁移目录（建议 `internal/store/history_migrations/0001_session_history.sql`）只 `CREATE TABLE`／`CREATE INDEX`，不 `ALTER` 主库或其旧表；迁移账本也只存在历史库中。建议逻辑结构如下（字节数按 UTF-8 完整序列化事件加本表持久元数据计；1 MB=1,000,000 bytes、64 MB=64,000,000 bytes、2 GB=2,000,000,000 bytes；SQLite 页、索引、WAL 等物理开销不计入事件容量）：

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
  lost_event_count INTEGER NOT NULL DEFAULT 1,
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

### 主库容量影响与已选独立文件

若把全局 2 GB 历史压在现网 `workpulse.db` 上，会明显增大主库；新增和淘汰经单写者/WAL 增加 WAL 峰值、checkpoint 写放大及临时空间。在线备份和恢复都需复制/检查更大的主库，备份窗口与恢复耗时增长；淘汰产生 freelist，`VACUUM` 可能长时间重写数据库并需要接近一份库大小的额外空间，影响主库写可用性。

采用同机、同 SQLite 实现的独立 `workpulse-history.db`，以独立迁移目录和账本管理；隔离历史库 WAL、checkpoint、淘汰/VACUUM 与主库业务写锁。撤权/换属主不能跨库原子提交：每次读取实时核授权，先禁用旧属主读取，再异步、可重试地删除对应历史；删除失败必须可见并持续重试，恢复访问前不得有旧历史可读。备份与还原必须将主库和历史库作为一组操作，保证成对快照/恢复。最终 event_id、generation 与现有身份字段映射在实现阶段复核并写入 context。

## 写入路径、可见性与实时延迟

具体事件流是 `handleSessionEventEnvelopeContext` 的 helper `session_event`，不是 `terminal_output`。在 `terminal.go:497` 清理 provenance 后、`publishSessionEvent` 之前尝试同步持久事务；提交成功则顺序为**数据库事务提交 → ring → mux/SSE/其他消费者**。持久写失败或单条超限时，先记/排队记 gap，再照常调用原 `publishSessionEvent`，不因历史写结果改变 r48 实时推送。持久序号随事件传入发布层；ring 的进程 epoch 保留作热态诊断，不作历史游标。

完整序列化记录大于 1 MB 时不截断、不入事件表，但事件仍照常进入 r48 ring 和实时推送路径；历史侧记 `oversize` 缺口区间、`lost_event_count` 及不含正文的结构化日志。SQLite 插入/淘汰事务失败时回滚历史写，事件仍照常进入 ring、mux/SSE 并保持现有推送顺序；历史侧记 `persist_error` 缺口区间、计数及结构化日志。能提交历史库事务时将 gap row 与原因/区间/计数持久化；历史库本身暂不可写时，内存合并缺口计数并逐次写结构化日志，存储恢复后优先补记 gap 再开放历史查询。首期不改 helper 协议，不要求负确认或重试，可靠确认另案处理。历史查询在缺口边界返回 gap，页面显示「这段历史未保存」。历史库不可读期间接口明确返回存储不可用；不能把区间伪装成连续历史。日志不含正文，只含脱敏身份键、时间区间、原因和计数。

同步 SQLite 写使从事件接收到发布的额外延迟约等于事务、索引及必要淘汰耗时；历史写失败不阻止实时发布。记录写入/发布延迟直方图、失败计数与结构化 gap 日志，不记正文。隔离环境 20 个 Agent 同时推送时，开启 R15 后发布路径新增延迟 p99 必须≤10ms；测试覆盖 1KB、近 1MB、WAL checkpoint、淘汰及 busy/只读/超时。超门槛只报告读数并等待组长裁定，不自行改阈值或推迟实时事件。

## 容量、淘汰与撤权清理

- 单事件按完整序列化记录校验，超过 1,000,000 bytes **拒绝整条、不截断**；记录 gap，事件仍按 r48 实时路径推送。恰好等于限制可接收。
- 每 Agent（同一 owner 与稳定 Agent key 下的会话合计）≤64,000,000 bytes；全局 ≤2,000,000,000 bytes；最长保留 30×24 小时，取 `received_at_ms`。逻辑字节是 `record_bytes` 汇总，SQLite 页、索引、WAL、freelist 另算存储监控。
- 每次写事务按顺序执行：先删除所有到期记录；再若 Agent 超额，按 `received_at_ms, seq, session_name` 升序删除该 Agent 最旧记录至配额内；最后若全局超额，按 `received_at_ms, owner_key, agent_key_id, session_name, seq` 升序跨 Agent 删除至全局上限。容量与 TTL 谁先触发都只删最旧记录；同一时间戳用上述稳定键消除不确定性。每次淘汰同步推进对应代际 `gap_through_seq/oldest_seq`，不保存逐事件 gap 日志。
- 首期清理参数固定：每批最多 500 行；后台 TTL 清理每分钟启动一次，批间让出写锁，不做启动时全表扫描；每个清理事务最多 2 秒。容量淘汰也按该批大小有界执行，未完成的清理由后续批次继续，期间对受影响游标返回 gap/维护状态。
- 撤权/删除或换属主：权威 store/lifecycle 先使旧属主读取立即失效，随后向历史库异步派发可重试正文删除。删除失败必须可见、计数并持续重试；旧属主在确认删除前始终不可读。换属主时旧历史一律销毁，不转给新属主。读路径每次重新核验当前 owner/key。实现阶段定位具体吊销入口并登记文件:行；同时清理 ring、mux reader 和 renderer cache。

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

读取接口在 HTTP 路由层先解析登录主体，再通过现有 Console Agent ownership/auth 查询取稳定 owner 与 Agent key；请求中的路径 Agent 仅为查找键，不是授权凭据。Owner 归属变更时，旧 owner 读取立即拒绝，随后异步重试删除旧代际正文；旧历史一律销毁，不转给新 owner。删除失败对运维可见，且清理成功前持续拒绝旧 owner。

## 兼容、迁移与回退

R15 使用 `workpulse-history.db` 专用迁移目录与账本，不在主库增加 0075；主库编号 0072～0074 留给 R14/R10/R9。只从迁移后实际收到的合规结构化事件累计，不回填 `session_delivery` 或 `message`。历史库不改变 r48 主库 schema；r48 不接入历史库。主库与历史库备份、还原作为一组操作。应用回退不 DROP 历史表。原“r48 Hub 在带 0075 库上的启动兼容隔离实测”因本裁定不产生主库 0075 而不适用于 R15；不得据此推断其它消费方迁移的 r48 兼容性。

回退期间 r48 不写历史；再升级时沿用回退前的 generation/seq，新写事件继续累加 seq，并将回退开始/结束时刻记录成显式 history gap 区间。不得把空洞解释成连续；旧 cursor 跨过空洞时返回 gap 和「这段历史未保存」，再从 gap 后实际记录续读。历史库按 TTL/容量/撤权规则继续清理。

## 测试计划（设计验收，不在本设计任务执行）

每项同时做正向和反向场景；反向包括拒绝/失败边界，且每条目标变异必须让对应断言转红。所有 TTL/时间容量测试使用可注入虚拟时钟，禁止 wall-clock sleep。并发 race 采用穷尽分片：对每个列明的交错点枚举确定性调度分片并各跑 `-count=3`；固定随机 seed 和分片清单写入日志。

| 需求 | 正向 / 反向场景 | 测试层 | 目标变异（必须红） |
| --- | --- | --- | --- |
| R15-001 单位/新表 | 事件逐条升序持久并读取；旧 delivery/message 不被回填；正文/元数据计量边界 | store 集成 + Hub 集成 | 将存储单位改成 round 聚合或从 delivery 回填，断言条数/正文来源失败 |
| R15-002 重启/重连 | Hub 重启和 helper 重连后续 seq；generation 真实改变时旧游标 gap | store+Hub 进程集成 | 启动时重置 seq 或把 epoch 改变当 gap，连续性断言失败 |
| R15-003 切点 | 并发写入与 subscribe/query H 交错，对照源 ID 恰一次；反向注入订阅后丢事件应显 gap | Hub race 穷尽分片 `-count=3` | 把 register reader 移到查 H 之后，丢失计数断言失败 |
| R15-004 权限 | 本人授权读取；他人、伪造 owner/session/cursor、管理员越权拒绝且无侧信道 | HTTP handler + 授权集成 | 移除服务端 ownership 检查，跨 Agent 负例变绿/敏感断言失败 |
| R15-005 上限/TTL | 1,000,000 bytes 接受，超 1 byte 拒绝持久化但照常推送；Agent/global 逐次淘汰；30 天边界虚拟时钟 | store 边界/性质测试，容量和 TTL 用虚拟时钟 | 将 `>` 改 `>=` 或删除过期条件/实时发布，边界与推送断言失败 |
| R15-006 撤权 | 吊销/换属主后旧属主立即不可读，异步删除失败时可见并重试，最终清除正文/索引；旧历史不转给新属主 | store+HTTP 授权集成，撤权 race 穷尽分片 `-count=3` | 移除即时授权失效则旧属主读取断言失败；移除重试/正文删除则最终清除断言失败 |
| R15-007 迁移/回退 | 独立历史库迁移成功且主库无 R15 DDL；回退期间 r48 不写历史，再升级显 gap；主库与历史库成组备份/恢复 | history migration 集成 + 隔离回退测试 | 把历史 DDL 写入主库或回退后伪造连续，schema/gap 断言失败 |
| R15-008 分页/UI | 首屏、100 条和 4MiB 截断连续、2 秒 deadline、最早/缺口提示、锚点保持及实时 append；超预算、跨代际 cursor、越权拒绝 | API handler + 浏览器回归 | 删除 exclusive 边界、cursor 前进或锚点恢复，重复/跳过/偏移断言失败 |
| 写失败语义 | 注入 busy/只读/超时及超大事件，事件照常进入 r48 live 路径，缺口带原因/计数/日志，恢复后读取可见 gap，页面显示「这段历史未保存」 | Hub/store 故障注入 + API/UI 集成 | 吞掉写错且不记 gap，或拦截 live 发布，缺口/字节对照断言失败 |
| 性能 | 隔离环境 20 个 Agent 同时推送，开启 R15 后发布路径新增延迟 p99≤10ms；否则留读数上报组长 | 隔离性能基准 | 禁用发布延迟采样或把新增持久写移出计时区，p99 观测断言失败 |
| 默认关闭开关 | unset/空/`0`/`true`/` 1` 均关闭，精确 `1` 才启用；关闭时比对 r48 事件 bytes、ring、序号及 mux/SSE 次序；新分页接口返回固定「未启用」；开—关—再开保留旧记录、关态事件不回填并出现 gap | Hub 单元 + store/HTTP 集成，重启子进程配置矩阵 | 将开关判定变异为恒 `true`，默认关闭格必须红；删除 gap 标记时切换期连续性断言必须红 |

目标变异逐条留原始红测日志；本设计不声明任何实现或验收 PASS。清理参数与 10ms p99 性能门已按组长裁定冻结；若性能测量不达标，报告原始读数并暂停放行，等组长另行决定，不擅改阈值。

## 待组长定（本节事项均已裁定，逐项原文保留并标注）

1. 历史数据落 `workpulse.db` 专用表还是独立 `workpulse-history.db`（建议独立文件，见上节）；兼容清理的跨库事务方案。**已定（组长 2026-11-02）：原文保留。**采用同机独立 `workpulse-history.db`，独立迁移目录与账本，不进入主库 0075 序列；主库编号留给 R14/R10/R9。撤权/换属主先拒绝读取，再异步、可重试删除；失败可见并重试；主库与历史库备份/还原成组。
2. 主库写入失败/单条超大时当前 helper 协议能否提供可靠负确认和重试；否则先定协议升级窗口。**已定（组长 2026-11-02）：原文保留。**首期不改 helper 协议，不要求负确认/重试；事件照常实时推送，历史侧记录缺口区间、计数与日志；读接口返回 gap，页面显示「这段历史未保存」。可靠确认后续再做。
3. 每次清理 500 行、后台每分钟、事务 2 秒建议值及生产实时延迟 SLO。**已定（组长 2026-11-02）：原文保留。**采纳每批 500 行、后台每分钟、单事务 2 秒；隔离环境 20 Agent 同时推送时，开启 R15 后发布路径新增延迟 p99≤10ms；不达标只报读数，等待组长再定，不擅改门槛。
4. Owner 归属变更旧历史一律销毁的临时默认策略，及回退期间历史读的呈现策略。**已定（组长 2026-11-02）：原文保留。**换属主时旧历史一律销毁；回退期间 r48 不写历史，再升级显式呈现 gap，不当作连续历史。
5. 逻辑容量按十进制字节且不计 SQLite 页开销、event_id/generation 与现有身份字段的最终 schema 对照。**已定（组长 2026-11-02）：原文保留。**容量按十进制字节计，不计 SQLite 页开销；event_id、generation 和身份字段最终 schema 对照在实现阶段完成并写进 context。
