# Design

## Context

基线是 `c248506cf120c3af2a06eeaeeae3038f596537b1`。Hub 当前在内存 `eventRing` 保存 helper 上送的结构化 `session_event`，Console mux 与 View SSE 都从该 ring 建立快照并续接实时事件；重启失去进程内状态，helper 断开会关闭 terminal 并清环。现有序号含进程内 epoch，无法直接代表跨进程的持久游标。只读生产事实见 proposal 所链报告；报告未读取事件正文，也没有证明 `session_delivery` 是完整历史。

## Goals / Non-Goals

- 目标：以结构化会话事件为存储单位，为每个 Agent 提供受容量与时长约束的持久历史；Hub 重启和 helper 重连后，已持久化记录仍可按授权回放；回合视图由读取层聚合。
- 目标：保持授权边界、游标连续性、实时切点去重和淘汰 gap 可观察；定义数据容量、TTL、撤权清理及回退路径。
- 非目标：持久化任意终端帧或 Hub 日志；从 `session_delivery`／`message` 推断未保存的历史；扩大 Agent／Console 可见权限；实现外部归档或跨账户搜索。

## 代码与数据现状

- `internal/m8/terminal.go:38-41`：terminal token TTL 8 小时、终端 buffer 1 MiB、每会话 eventRing 上限 1000 帧。
- `internal/m8/terminal.go:470-493,512-525`：结构化 `session_event` 经 Hub 接收后发布；序号递增、事件进入 ring 并扇出订阅者。
- `internal/m8/event_ring.go:4-8,45-57,79-92`：Hub 内存 eventRing 每会话 16 MiB、全局 128 MiB／128000 帧预算；超预算从最旧端淘汰。此预算是进程内热缓存，与持久化上限分开。
- `internal/m8/terminal.go:855-894,912-943,945-1015`：View SSE 在同一锁切点注册实时订阅并复制 ring；epoch/seq cursor 决定回放，epoch 不同报告 `epoch_changed`，不足 ring 范围报告 `overflow`。
- `internal/m8/console_mux_frames.go:150-180` 与 `internal/m8/console_mux_transport.go:698-717`：Console mux 同样快照内存 ring 并依据所请求 cursor 选 replay；缺少 terminal source 时不能建立该源。
- `internal/m8/hub.go:1241-1255` 与 `internal/m8/terminal.go:177-202`：当前 helper 连接结束时删除 session client 并调用 `closeTerminalLocked`；该路径清理 ring、关闭 mux readers 并标记 `epoch_changed`。
- `internal/m8/console_mux.go:345-388`、`internal/m8/console_mux_broker.js:24`：cursor 不可直接继续时 Hub 返回 `resync_required` 等控制结果；浏览器收到后提示历史需显式重新同步。
- `internal/store/migrations/0027_session_delivery.sql`：`session_delivery` 是投递状态表；`message` 是聊天消息存储。只读报告确认两表虽有数据，但当前回放路径不读它们，且不存在持久化 terminal event-ring 表。
- `view-service/renderer-canary-sse.html:1099`：Console 对话通过 `section.timeline#timeline` 呈现；`:413-425` 将 `.timeline` 设为独立 `overflow-y:auto` 滚动容器。
- `view-service/renderer-canary-sse.html:1769-1792,1849-1870`：现有前端计算是否接近底部、追加内容后保持贴底，并根据真实用户手势释放自动跟随；滚动监听没有向上取历史请求、较早页加载状态或“没有更早记录”处理。向上分页加载是新增前端工作量。

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

按 PM-R15-08，Hub 进程 epoch 与持久历史代际分离：单纯 Hub 重启且身份代际未变时沿用持久 cursor，不要求显式 resync；helper 重连也不清除已存历史。发生 TTL／容量淘汰、删除／撤权、数据库不可用、代际变化或 cursor 早于最旧记录时，发出可识别的 history gap，并按明确恢复流程要求 resync。当前 `epoch_changed`、`overflow`、`resync_required` 的 Console mux 与 View SSE 语义需兼容升级；不应在实现中仅删除旧通知来制造“连续”的假象。

## Console 历史分页与实时衔接

### 交互

- 打开对话时只请求并显示最近一页，同时从该页的持久化高水位 H 继续实时流；页面不预取全部历史。
- 当 `.timeline` 滚动到顶端且没有同会话的旧页请求在途时，按最早已显示逻辑序号之前的 exclusive cursor 请求更早一页；用户继续上拉可重复，直到 API 声明没有更早记录。
- 请求前记录首个可见历史条目的稳定事件 ID／逻辑序号和其相对滚动容器的像素偏移；将旧页 prepend 并完成布局后，恢复该锚点偏移。若事件实时追加到列表底部，不改变这次 prepend 的锚点，也不把已上翻用户拉回底部。
- `has_older=false` 时在列表顶部给出明确文案「没有更早的记录」。容量或 TTL 淘汰形成的 gap 在对应 cursor 边界处显示缺口提示，不伪装成“最早记录”。

### 读取接口候选与游标

建议提供只读分页读取路径，例如 `GET /api/console/agents/{agent}/history`；首屏不带 cursor 返回最新一页，继续向前用 `before_seq=<exclusive logical sequence>` 请求。响应按逻辑序号升序返回 `events`、可选 `next_before_seq`、`has_older`、历史高水位 `high_water_seq` 和边界 `gap_before` 元数据。Cursor 是该 Agent 持久历史代际内的单调逻辑序号，不使用进程 epoch；旧代际 cursor 不匹配时返回可见 gap／resync 状态。每次请求必须从认证身份解析 Agent 并执行 PM-R15-04 所定的同一 Console 所属账号授权，不能把路径中的 Agent 字段当授权凭据。

页面大小尚未由业主明确，列 PM-R15-09 待定；推荐每页最多 50 个事件、序列化响应总字节不超过 4 MiB，单请求服务端 deadline 2 秒。达到条数或字节上限时只返回已连续读取的前缀及下一条之前的 cursor，不跳过记录；返回记录为 0 且边界无更早事件时才标记 `has_older=false`。单条持久记录最大 1 MB，因此响应字节预算必须允许至少一条最大合法记录和协议元数据。具体字节计量口径随实现协议写明。

### 与实时流切点

首屏请求在一个可证明的切点取得高水位 H：首屏仅含 `seq ≤ H` 的最近页，实时订阅从 `seq > H` 开始；历史翻页只取小于当前最早已显示序号的记录，不重置 H，也不重复订阅。若采用 subscribe-first，Hub 先建立实时 reader 并取得同一历史高水位，再缓冲 H 之后事件直到页面应用首屏；实现必须证明不存在“先查完历史、后订阅 live”的漏帧窗口。页内与 SSE 并发抵达的相同事件按持久事件 ID／逻辑序号幂等合并，不能按 assistant delta 共用的客户端 item cursor 去重。

Hub 重启后，持久序号与历史代际继续使用：首屏请求返回的 H 和 `before_seq` 均不受进程 epoch 改变；实时订阅按新 Hub 的稳定持久序号从 H 后接续。只有 cursor 早于容量／30 天淘汰形成的边界、撤权或真实存储缺口时才显示 gap／要求 resync。分页窗口中的 gap 与当前实时 SSE 的 gap 需要共用可解释的边界标识，避免把重启误报为丢失。

## 容量、清理与隐私定值

- 持久化硬界按 PM-R15-02：单条 ≤1 MB；每 Agent ≤64 MB；全局 ≤2 GB。单位的精确字节换算应在实现规格中写明。持久化上限与 Hub 内存 eventRing 预算（每会话 16 MiB／全局 128 MiB）彼此独立。
- 清理规则：新事件先校验完整序列化历史记录（结构化事件正文及持久化元数据）的字节数与 30 天时限；超过单条 1 MB 时拒绝整条持久化，不截断，并生成不含正文的 gap 状态。计量单位和 SQLite 页面／索引等物理开销的核算方式须在实现规格中写明。插入导致某 Agent 超过 64 MB 时，按该 Agent 的接收时间／序号从最旧记录开始淘汰，直到恢复上限。超过全局 2 GB 时，在所有 Agent 的可淘汰记录中按接收时间全局最旧优先删除；时间相同按稳定 owner/session key 与序号作确定性 tie-break，每次被影响的 Agent 都更新有界的 gap 水位／计数，不为每次淘汰无限累积元数据。TTL 到期也删除最旧到期记录；容量与时长限制同时生效，先触发者先淘汰。
- 公平性建议：保留每 Agent 64 MB 硬配额，限制单一活跃 Agent 占用全局容量；全局压力下按全局最旧时间逐条回收，不采用单一 Agent 优先级。保存各 Agent 占用、淘汰量和 gap 计数，若有 Agent 长期被全局淘汰挤压，再以证据评估配额／预留策略，不预先加入未经批准的配额池。
- eventRing 建议：首期维持现有进程内每会话 16 MiB／全局 128 MiB 与 128000 帧定值，不随持久化预算扩大。它是低延迟内存热缓存，持久历史负责跨重启和较长回放；报告观察到活跃会话约 2.5 小时可达到 1000 帧，说明 ring 的回放窗口仍是独立限制。改 ring 预算会影响 Hub 内存，不属于本次 PM 定值；若持久化回放接入后仍有性能或体验证据，再另案评估。
- TTL：按 PM-R15-03 最多 30 天；后台按有界批次删除过期记录。启动时不得做无界全表扫描或长事务。
- 可见性：按 PM-R15-04，每次读取都从认证主体解析稳定 owner/Agent identity 并执行现有 Console 授权；不接受客户端自报 owner 作为唯一授权条件，不增加管理员或跨 Agent 搜索权限。
- 吊销／删除：按 PM-R15-05 立即使相关历史不可读取并清理正文与索引；审计仅保留非正文元数据。删除必须同时覆盖持久表、热缓存和可关联索引，身份重建不得恢复旧代际正文。
- 存储与加密：按 PM-R15-06，正文存 dw123 本机数据库，不做静态加密。不得把“卷／备份加密未证实”设为上线前置；该定值会使数据库正文以明文静态保存，访问控制和撤权清理仍须严格执行。
- 敏感内容：按 PM-R15-07 原样保存结构化事件，包括工具调用细节；正文和工具字段采用同一可见性、保护与删除策略。日志／审计不复制正文。脱敏规则另定，不由实现者擅自改写事件内容。

## 迁移与回退

新增向前迁移创建专用表和索引，不从 `session_delivery`／`message` 猜测回填；部署后只累计新收到且符合策略的事件。回退应用版本时，新表可暂时保留但旧版本不读取；回滚不得自动 DROP 历史表。若 PM 选择撤权即删除，回退流程仍须能执行删除义务；schema 删除、历史清空和备份到期分别列为独立审批与证据，不混在普通代码回滚中。

## Risks / Trade-offs

- 同步落盘提供清晰恢复语义但增加热路径延迟；异步方案吞吐更高但有丢失窗口，需要明确可观测 gap 与关闭时 flush 保障。
- 历史比内存 ring 更持久，也扩大泄露和备份残留的影响；业主定值不做静态加密，因此访问控制、主机数据库访问边界、TTL 和撤权删除尤其需要验收。
- 持久游标与现有进程 epoch 不兼容时，错误切点可能导致重复或缺失。实现须以高水位／实时订阅原子衔接以及重放对照测试证明连续性。

## PM Decisions

PM-R15-01～08 已按 rulings ㉚～㉜ 定值；㉝ 已定向上滚动加载更早历史、保持位置并提示到底／缺口，逐项见 proposal。PM-R15-09 页大小和请求预算尚未由业主定值，推荐值见分页接口段。存储算法、游标切点及全局淘汰细节仍属于本 design 的实现方案，需在实现前评审冻结；不得把未评审的内部算法描述成业主裁定。
