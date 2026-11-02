# Design

## Context

基线是 `c248506cf120c3af2a06eeaeeae3038f596537b1`。Hub 当前在内存 `eventRing` 保存 helper 上送的结构化 `session_event`，Console mux 与 View SSE 都从该 ring 建立快照并续接实时事件；重启失去进程内状态，helper 断开会关闭 terminal 并清环。现有序号含进程内 epoch，无法直接代表跨进程的持久游标。只读生产事实见 proposal 所链报告；报告未读取事件正文，也没有证明 `session_delivery` 是完整历史。

## Goals / Non-Goals

- 目标：为每个 Agent 提供最近 N 条结构化消息／回合的有界持久历史；Hub 重启和 helper 重连后，已持久化的记录仍可按授权回放。
- 目标：保持授权边界、游标连续性、实时切点去重和淘汰 gap 可观察；定义数据容量、TTL、撤权清理及回退路径。
- 非目标：持久化任意终端帧或 Hub 日志；从 `session_delivery`／`message` 推断未保存的历史；扩大 Agent／Console 可见权限；实现外部归档或跨账户搜索。

## 代码与数据现状

- `internal/m8/terminal.go:38-41`：terminal token TTL 8 小时、终端 buffer 1 MiB、每会话 eventRing 上限 1000 帧。
- `internal/m8/terminal.go:470-493,512-525`：结构化 `session_event` 经 Hub 接收后发布；序号递增、事件进入 ring 并扇出订阅者。
- `internal/m8/event_ring.go:4-8,45-57,79-92`：每会话 16 MiB、Hub 128 MiB／128000 条预算；超预算从最旧端淘汰。
- `internal/m8/terminal.go:855-894,912-943,945-1015`：View SSE 在同一锁切点注册实时订阅并复制 ring；epoch/seq cursor 决定回放，epoch 不同报告 `epoch_changed`，不足 ring 范围报告 `overflow`。
- `internal/m8/console_mux_frames.go:150-180` 与 `internal/m8/console_mux_transport.go:698-717`：Console mux 同样快照内存 ring 并依据所请求 cursor 选 replay；缺少 terminal source 时不能建立该源。
- `internal/m8/hub.go:1241-1255` 与 `internal/m8/terminal.go:177-202`：当前 helper 连接结束时删除 session client 并调用 `closeTerminalLocked`；该路径清理 ring、关闭 mux readers 并标记 `epoch_changed`。
- `internal/m8/console_mux.go:345-388`、`internal/m8/console_mux_broker.js:24`：cursor 不可直接继续时 Hub 返回 `resync_required` 等控制结果；浏览器收到后提示历史需显式重新同步。
- `internal/store/migrations/0027_session_delivery.sql`：`session_delivery` 是投递状态表；`message` 是聊天消息存储。只读报告确认两表虽有数据，但当前回放路径不读它们，且不存在持久化 terminal event-ring 表。

## 存储候选

### A. SQLite 新增专用历史表（推荐）

新增按稳定 Agent 身份和会话键索引的历史表，单行保存逻辑序号／事件 ID、接收时间、事件类型、正文及必要的来源／schema 元数据；另有可查询的最新序号和历史代际元数据。写入可与去重标识同事务提交，读路径按游标有界分页，过期／超量淘汰按索引删除。优点是明确分离“事件历史”与“消息投递”，可单独约束权限、大小、TTL 和 schema；缺点是热路径新增 SQLite 写负载及迁移。

### B. 复用 `session_delivery`

尝试从 delivery envelope 重建会话事件。此表表达 Hub 投递及 ACK 状态，覆盖面、顺序、保留周期与结构化 `session_event` 回放语义并不等价；对无投递的会话事件、更新／重试、副作用元数据和已清理记录均不能假定完整。除非后续勘察证明有稳定一对一键、正文、权限与保留契约，此路线不推荐。

### C. 复用 `message`

该表以聊天实体为中心，而不是 Agent 结构化事件流；把它当作 Agent 事件历史会混淆不同来源和身份，也可能漏掉非聊天事件。本路线不推荐。

## 写入时机与热路径

写入入口是 `handleSessionEventEnvelopeContext` 中已校验来源并剥除客户端 provenance 后的结构化事件，接着才发布到 ring 和调用后续消费者。候选策略：

1. **同步事务后发布（推荐作为一致性基线）**：先完成有界数据库写入／去重／淘汰，再给实时 ring 和订阅者发布。数据库错误时不得静默宣称该事件已在持久历史中；需明确定义实时是否仍可发布及客户端可观察的持久化缺口。优点是崩溃边界和顺序清楚；风险是 SQLite 写锁、磁盘延迟直接进入 Hub 热路径，必须设置超时、批量／索引评估和延迟指标。
2. **有界有序 writer 队列**：Hub 仅入队，后台按 Agent 顺序写入。可降低单事件 I/O 等待，但进程崩溃会丢失尚未刷盘队列，队列满还需要背压或显式 gap；只有在接受并观测该窗口、定义 flush/shutdown 行为后才可采用。

不得以无界异步 goroutine 或内存队列作为“持久化完成”的依据。无论选何策略，实时流必须能识别尚未持久提交的游标，避免客户端把未落盘事件的 cursor 当成可恢复 checkpoint。

## 回放协议与 eventRing 衔接

候选方案是将持久历史作为先行 replay source，在取得一个原子高水位 H 后注册／接续实时订阅；eventRing 只作为热缓存或同进程实时缓冲，不再作为历史权威。cursor 至少包含稳定 Agent/session 身份代际和持久单调序号；可另带 schema version。读取顺序为：授权 → 查询游标之后且不大于 H 的持久记录 → 发送连续记录 → 从 H+1 接实时流，并以 event ID/序号去重。H 的读取与实时订阅建立须通过锁、数据库事务／outbox offset 或等价的可证明切点，不能先查库再裸订阅造成丢失窗口。

PM-R15-08 推荐将 Hub 进程 epoch 与持久历史代际分离：单纯 Hub 重启且身份代际未变时沿用持久 cursor，不要求显式 resync；helper 重连也不清除已存历史。发生 TTL／容量淘汰、删除／撤权、数据库不可用、代际变化或 cursor 早于最旧记录时，发出可识别的 history gap，并按明确恢复流程要求 resync。当前 `epoch_changed`、`overflow`、`resync_required` 的 Console mux 与 View SSE 语义需兼容升级；不应在实现中仅删除旧通知来制造“连续”的假象。

## 容量、清理与隐私候选

- 共同硬界：每 Agent 最多 1000 条、16 MiB；单条最多 256 KiB；Hub 全局最多 128 MiB；另加 PM-R15-03 的 TTL（推荐 30 天）。各界同时生效，超界从最旧记录淘汰并记录不含正文的 gap 元数据。
- 清理：新写入时按 Agent 条数／字节同步淘汰；全局容量按全局最旧项淘汰；TTL 由有界周期任务清理。启动时不得做无界全表扫描或长事务；删除／撤权事件触发按 Agent 分批删除并保证授权立即失效。
- 可见性：每次读取均从认证主体解析稳定 owner/Agent identity 并执行现有权限检查；不接受客户端自报 owner 作为唯一授权条件。默认不增加管理员或跨 Agent 搜索权限。
- 加密：先核实 SQLite 数据目录与备份的静态加密。若无法证明部署层保护覆盖数据库和备份，不能把“磁盘加密已启用”当作事实；应用层加密须另有密钥轮换、恢复、销毁和索引字段设计，需 PM 定值。
- 敏感内容：若保存当前结构化事件原文，工具调用参数、结果和错误可能包含敏感内容；沿用现有同权限显示不等于风险已消除。PM-R15-07 决定是否保留工具细节，且日志／审计不得复制正文。

## 迁移与回退

新增向前迁移创建专用表和索引，不从 `session_delivery`／`message` 猜测回填；部署后只累计新收到且符合策略的事件。回退应用版本时，新表可暂时保留但旧版本不读取；回滚不得自动 DROP 历史表。若 PM 选择撤权即删除，回退流程仍须能执行删除义务；schema 删除、历史清空和备份到期分别列为独立审批与证据，不混在普通代码回滚中。

## Risks / Trade-offs

- 同步落盘提供清晰恢复语义但增加热路径延迟；异步方案吞吐更高但有丢失窗口，需要明确可观测 gap 与关闭时 flush 保障。
- 历史比内存 ring 更持久，也扩大泄露和备份残留的影响；TTL、访问控制、撤权删除和静态加密必须一并验收。
- 持久游标与现有进程 epoch 不兼容时，错误切点可能导致重复或缺失。实现须以高水位／实时订阅原子衔接以及重放对照测试证明连续性。

## PM Decisions

见 proposal 的 PM-R15-01～08；未决前不得将推荐值描述为业主已经批准的实现定值。
