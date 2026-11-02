# Design

## Context

现状逐项证据见specs/feishu-agent-sticky/context.md，固定r46 2af469dd。当前已有单本人在线active免前缀、多人候选和显式selector/歧义拒绝，没有粘滞状态。代码层`#全名 正文`可解析，但纯多行候选回贴会被拆成首目标+其它行正文；不是新功能的验收结果。

## Goals / Non-Goals

- 按可信业主账号+机器人通道隔离30分钟默认，显式意图优先、无可用默认时不猜人，保留单Agent便捷路径。
- 提供可复核的计时、异常、复制、并发/重启及兼容边界；PM01～08按proposal定值冻结；本单不授权实现。
- 不改群聊、AMR、跨成员路由、系统命令/自定义prefix、sync/mirror或其它渠道；不查询生产DB、不apply/archive。

## Decisions

### 1. 身份、集合与优先级（R14-001、002、007、009）

键K为`(Identity解析的业主UserIDLC, msg.BotChannelID)`，不是显示昵称、消息中的#名字、全局最后对象或单独SenderOpenID。BotChannelID是入站通道身份，不让正文伪造issuer；无可信绑定/业主inactive时沿现有拒绝，不用旧账号降级扩权。首期只接新平台Identity，旧身份维持既有行为（PM08）。

可路由集合E为本人当前连接仍存在且身份active的Agent，在线/停用逐次重核；不把同组他人或历史数据库Active行混进来。目标用稳定Agent ID识别、展示时实时求全名，避免名字被撤销后重建的新Agent继承旧默认；身份查询失败时不以空集伪装“恰好只剩另一个”。

保留澄清答复、系统/安全/登录、同步、roster、成员显式提及、已配prefix等既有优先级；R14默认只在最终普通私聊默认目标决策挂接。候选块回贴/仅选择命令在显式selector处理前作确定性识别，但不得抢占原保留命令或扫描普通业务正文中的井号。群聊及Agent输出/系统通知不进入该状态机。

### 2. 有限状态与决策表（R14-003～007）

下列“无记录/有效/过期/不可用”仅为内部逻辑分类，不是已存在的API状态或新增外部错误码。按PM01～08已定方案，每条业务消息只选择一个目标：

| 入站情形 | 处理 | 状态/时钟 |
|---|---|---|
| 已有命令、跨成员显式路由或非私聊 | 沿原路径 | 不写K；跨成员不成为“本人默认” |
| 全文纯候选块（至少两行，每行仅一个完整`#全名`，可带本功能自己的提示标题） | 按PM07仅提示选一行，无业务投递 | 不更新K或续期 |
| 明确本人目标且唯一/在线/active | 按原路由受理正文；PM05已定的仅选择形式则不发送空正文 | 成功受理/成功选择后设置目标并重置30分钟；失败不切换 |
| 显式无效/有歧义/外属主目标 | 原失败/歧义反馈及本人候选 | 不回退旧默认、不续期、不猜其它人 |
| 原目标已离线/停用/撤权/身份变动 | 按PM02回候选并提示不可用，即便E只剩其它一个也不自动投它 | 旧默认不可再用；仅保留仍可合法显示的上次参考信息 |
| 无已知失效目标且E恰有1个 | 单Agent免前缀，沿当前便捷行为 | 受理后记录该目标；登记总数/旧身份边界见PM08 |
| E有多个、K有效且目标仍在E | 无前缀续聊该目标 | 按PM01刷新该目标时钟 |
| 无记录或过期、E有多个 | 回复候选；上次仍在E则置首 | 不发送原业务、不自动选择、不因列候选续期 |
| E为空或身份/状态存储读取失败 | 清晰反馈无可用目标/暂不可确定，不走其它Agent | 不随机回退，不恢复失效默认 |

原文要求超时列候选与单Agent免前缀同时成立：多Agent过期列候选；只剩原本人目标且仍可用时按单Agent特例免前缀。若原目标不可用但唯一的是别人，按PM02/08拒绝误投。

### 3. 时间、成功边界与乱序（R14-003、004、008）

时长固定30分钟。PM01以Hub成功受理目标消息/成功明确切换时刻为锚t，命中须`0≤now−t<30min`，恰好30分钟过期；仅收到上游重放、候选提示、Agent主动/迟到回复、delivery ACK、健康流量、系统命令不刷新。Agent回复不是用户更改选择的指令，A的迟到回复不能覆盖已切换B。Agent最后回复或两者较晚不再是选项；Agent回复、迟到消息不续期。

不信任客户端自报时间。进程内可用单调耗时辅助，PM06要求跨重启保存时，保留UTC锚/原绝对到期时间；恢复不能再加30分钟。时间异常/回拨不能把旧默认延长成无限期，无法确定时回候选重新确认；边界/时钟故障纳未来验收。

### 4. 候选格式与井号往返（R14-005、006）

候选顺序：当前合法可选的上次Agent排首，其余按当前全名稳定字典序；没有上次或不在E则其余正常排序。离线提示是否展示依PM03，已换属主/撤权者不从旧记录泄露名字。使用身份权威全名，逐行ASCII`#`紧接名字，不用编号/项目符号、Markdown标题`# 名字`、代码围栏或旧地址后缀。

拟议纯text回复示例（这里只说明格式，不固定候选身份）：

    默认会话已过期，请选一个 Agent：
    #agent-a.owner
    #agent-b.owner

用户可以整体复制候选，但这不是多选语法。按PM07，回贴完整纯候选块应提示“请选择一行并补充消息”，不投首项、不广播、不把余行发给首项。消息开头的井号名按已定PM05处理：短名先精确匹配本人Agent，再匹配唯一前缀；多个前缀候选时列出候选、不猜。显式全名按全名匹配，可落到其他用户Agent，但继续经既有授权与归属核验。只写井号名无正文时只切换，业务投递为0；没有“切换”关键词语法。井号名必须在消息开头，正文中间的井号不作选择语法。业务正文内的代码/井号不因候选检测被全局改写。先前因过期/不可用未发送的业务正文不缓存后自动重投，防止选择后意外重做写操作。

Hub候选回复通过现有Feishu纯text回复路径返回，不注入Agent、AMR或群聊；井号属于保留路由语法，系统回复不得变成新的提及任务。代码层字符保留不代替真实飞书复制验收：未来要覆盖桌面/移动端、CRLF/LF、全段复制、单行粘贴和业务正文中的Markdown。

### 5. 存储与一致性（R14-008）

PM06已定为SQLite持久化小元数据。逻辑字段只需K、稳定Agent ID、锚/绝对到期、修订序号、最近源消息幂等标识及必要失效状态；不存对话正文/凭据，不借mirror_to或session_endpoint.last_seen_at。可复用SQLite原语或另建专用表，表名、迁移号和仓库接口留实施设计冻结，本单不写DDL或真实app_config键。

同K的明确切换/续聊须可串行化，跨K独立；同消息重投不能重复发送、重置到期或把旧A覆盖新B。推荐把可靠受理与状态变更放同一事务/持久outbox边界；r46现有RouteEnvelope与通用CAS尚不是此原子接口，实现前须核可行性。若业务已受理而记录保存失败，必须明确反馈默认未成功变更并阻止不确定的后续自动续聊，不得虚报切换成功或盲目重投；故障恢复先对账源消息/投递ID。状态读取失败不静默退化成另一个单Agent默认。

重启后先恢复原时间/代际并逐次验证当前owner、Agent状态和连接，不能拿持久的session_endpoint.Active当实时在线；改名由稳定ID关联当前全名，撤销后同名重建不是同一对象。PM06已定持久化；必须测试重启后保留原到期时间并重新核验在线状态。

### 6. 可观测性与未来验收（R14-009）

记录源消息ID、可信owner/channel关联、决策类别（显式/粘滞/唯一/候选/拒绝）、实际稳定目标、有效/过期判定与更新结果即可；限制内部可见，不记录正文、secret或用户内容。投递沿原渠道/来源上下文，不把“受理”当“Agent已执行”。不新增外部路由或错误码。

以下全部**未执行**，只描述PM已定值后的未来验收，不能视为产品行为已通过：

| 格/需求 | 正向与失败/反证 |
|---|---|
| K隔离 R14-001/002 | 两owner×两bot交叉；切A不影响其它K；同组他人、未绑定、停用主体均拒绝；撤owner/channel过滤须在串话断言红 |
| 明确选择 R14-003 | 指定B替A并重置，歧义/外属主/保存失败不切；仅选择不发空业务；撤显式优先在实际去向断言红 |
| 时钟 R14-004 | 29:59.999命中、30:00过期，重复消息/Agent迟到/ACK不续期；撤边界或代际保护须目标断言红 |
| 不可用 R14-005/007 | A离线/停用/撤权/同名重建，B唯一时不误投；只有可用A时保留免前缀；撤可用性重核在错误目标计数红 |
| 候选 R14-006 | 原上次合法者首位、其余稳定、逐行#全名；整块往返投递0、单行按已定PM05；撤块识别/排序/井号分别在目标格红，不接受仅脚本报错 |
| 持久与并发 R14-008 | 同K并发显式A/B及晚到A回复、重试、崩溃窗口、DB故障；按修订防回滚，重启剩余TTL不变；撤幂等/原到期/稳定ID分别红 |
| 兼容与通道 R14-009 | 系统/roster/sync、旧prefix、群聊/其它渠道仍原样；真实飞书桌面/移动端复制与回入日志逐格；候选不能进入AMR或群发 |

测试应使用固定时钟、隔离临时SQLite/HOME/loopback和假飞书收发，不现实等待30分钟；真实飞书与重启验收须另获授权，不向现网发探测。只读源码勘察、OpenSpec strict和既有build gate均不算上述产品格通过。

## Risks / Trade-offs

- 单在线Agent与名下登记总数不同，已有失效目标时便捷单选可能误投；PM02/08必须按已定值共同实现。
- Agent回复驱动时钟会受迟到/主动通知影响，建议业主消息时钟；持久状态须防旧回复/重放改默认。
- 纯文本可复制不等于整块自动选择；当前井号selector和AMR共享语法，需明确协议边界。
- 持久化保留便捷性但引入失败一致性和数据生命周期；仅存元数据，过期历史保留/清理期限随实施设计交审，不在本单删除数据。

## Migration Plan

本提案已完成PM01～08定值，待独立审阅；实现须另派，在本变更冻结后补主规格/代码context、持久化设计及场景/变异。保持旧显式选择兼容，隔离测试、全仓strict与构建门通过后交独立验收，再另请预发/生产GO。本提案不apply/archive、不创建迁移、不自行改r46/r47或机器人配置。

## Follow-up Verification

时钟、失效目标反馈、离线候选、群聊范围、井号切换、重启保留、整段回贴与单Agent/旧身份边界按proposal PM01～08定值。身份/在线枚举的源码事实已核，真实飞书格式和持久更新原子性仍待后续验证。短名唯一性入口任务与前缀验收见PM05及R14-S021～023。

## T-04 Implementation Design Freeze (r48 baseline)

This section freezes a proposed implementation design for review only. The cited code facts are from `1e17255f28ced042784337903914ca8c126ef4cf`; detailed inventory is in `specs/feishu-agent-sticky/context.md`. It does not authorize implementation, migration, apply/archive, or real Feishu probing. Any item marked pending group-lead decision must be resolved before T-05/T-06.

### Stable identity and field changes

- Use `K = (resolved np_user.user_id_lc, BotChannelID)` for the sticky key. Resolve every inbound private Feishu message through `(provider="feishu", issuer=BotChannelID, subject=SenderOpenID)` first; `SenderOpenID` is an authenticated lookup input, not an independent/global key. Apply R14 only when ingress provenance is trusted (`IngressFeishuWSAuthenticated` or `IngressWebhookVerified`); the current identity resolver itself only checks non-empty IDs and mapping, so the R14 entry point must enforce provenance. The current Console principal is the same new-platform user ID; do not use legacy `console_principal.id`. Evidence: `internal/model/model.go:16-31,80-96`, `internal/feishu/feishu.go:319-332,444-485`, `internal/m8/newplat_feishu_inbound.go:12-18`, `internal/m8/newplat_view.go:223-244`, `internal/identity/pgschema.sql:6-25`.
- Store the chosen target as stable `np_agent.id`, never as short/full name, `session_endpoint`, or `mirror_to`. On every use, resolve that ID and verify its current `owner_lc` equals K's principal, its state is `active`, and its actual Hub connection is still online. Current connection enumeration uses owner plus names and rechecks active state; implementation must bridge it to stable IDs rather than trusting a stale name. Evidence: `internal/identity/model.go:135-155`, `internal/identity/pgschema.sql:45-65`, `internal/m8/newplat_feishu_inbound.go:21-41`, `internal/identity/service.go:286-295,416-435`.
- Display name/short name may change without changing agent ID; refresh display from Identity. A changed owner, revoked/suspended state, missing ID, inactive user, or failed identity lookup invalidates use and must fail closed. If an open_id is remapped to the same user, the K remains; if it maps to a different user, that user has a separate K and must not inherit the old one. A changed BotChannelID is a distinct K; do not copy state. Require an explicit new selection when a key has no valid binding. No fallback through old account identity. Evidence: `internal/identity/pgschema.sql:6-25,45-65`; `internal/m8/newplat_feishu_inbound.go:12-18,33-40`.
- Only private chats read/write K. Do not treat `session_endpoint(key_id,session_name)` as a sticky record: its key is an Agent endpoint and its `last_seen_at` is connection activity; do not infer conversation intent from `mirror_to`. Evidence: `internal/m8/hub.go:4048-4053`; `internal/store/migrations/0003_session_endpoint.sql:1-10`.

### Time, restart, and generation

- PM01 fixes the validity window at 30 minutes from Hub acceptance of the owner's last successfully accepted business message to that target, or a successful explicit switch; Agent replies do not renew it. Store `selected_at_utc` and `expires_at_utc = selected_at_utc + 30m` using the Hub's `h.now().UTC()` value at the accepted event. The production source is the process wall clock (`time.Now()`), with `NowFunc` only a test seam; it is not a DB clock or persistent monotonic clock. Evidence: `internal/m8/console_auth.go:53-58`; `internal/m8/hub.go:119-124`; frozen requirement R14-004 / PM01.
- On restart, compare current UTC wall time to the persisted absolute expiry; never add 30 minutes at startup. Revalidate the principal and current target/connection before routing. Do not introduce an external clock or tolerance. If current UTC is earlier than `selected_at_utc` (clock rollback) or at/after `expires_at_utc` (PM01 makes the exact 30-minute boundary expired), do not use sticky routing; return visible candidates or a re-selection prompt. Record the clock condition in logs only; do not alert. Evidence that the baseline uses process wall clock and has no sticky clock watermark: `internal/m8/console_auth.go:53-58`; `internal/m8/hub.go:119-124`.
- Each successful key-state change increments an integer `generation` in the same SQLite transaction as the state update. Each operation records its observed generation and uses compare-and-set; an older completion whose expected generation no longer matches cannot overwrite a newer binding. Agent replies, ACKs, duplicate source messages, and candidate responses never increment generation or renew the expiry. Use persisted source message identity for idempotence, not event timestamps or in-memory order alone. Evidence of current source message fields and dedup constraint: `internal/model/model.go:80-96`; `internal/store/migrations/0001_init.sql:34-50`.

### Same-key concurrency and trace order

Serialize each `(user_id_lc, bot_channel_id)` decision through one durable per-key operation lane. The transaction that allocates the next `operation_seq` and captures `base_generation` is the linearization point; record source message ID, operation kind (explicit/select-only/no-prefix), resolved target Agent ID, base/result generation, decision, and terminal status, but never message body. Do not use Feishu timestamps to order concurrent messages. The SQLite write/compare-and-set result defines the order and is inspectable after restart.

| Serial order | Expected behavior |
|---|---|
| Explicit `#A body` succeeds, then an unprefixed message | First operation routes only to A and commits A as the next generation; second reads that generation, rechecks A, and routes to A. |
| Unprefixed message precedes explicit `#A body` | The first uses the previously valid target only if the current generation is valid; with no valid target it returns candidates and sends no business message. The later explicit operation may then select A. The first result is not replayed. |
| No-prefix and explicit message overlap, or an earlier route has unknown outcome | The operation-sequence winner is processed first. The other waits; it rereads the committed generation after the first terminal result. If the first remains unresolved, do not route by sticky default or silently choose another Agent. Log both IDs and the blocking outcome. |
| Late completion / duplicate delivery | Apply only if `base_generation` still matches; otherwise record `stale_completion` and do not change the new generation. Duplicate `(BotChannelID, FeishuMsgID)` returns the recorded operation outcome and must not send again or renew TTL. |

R14 的权威范围已定为每个环境一个 Hub 进程写一个 SQLite 文件：dw123 现网和预发各自一个 systemd 单元；R14 以单 Hub/单文件为不变量，不做跨 Hub 协调。SQLite WAL 与单连接池写者供该单实例内每键车道使用；它们不构成跨 Hub 协调。T-07 不测多 Hub。代码事实：`internal/store/sqlite.go:57-66`；`internal/store/migrations/0001_init.sql:34-50`。单键消息仍按已定义的 `operation_seq` 分配顺序，不用 Feishu 时间戳排序。

### Delivery, state, and failure reconciliation

Do not claim a cross-action transaction between sticky state and Agent delivery. The existing `RouteEnvelope` can enqueue a durable `session_delivery` and drain it, or use direct WebSocket write; successful return means accepted/queued or written, not helper injection or Agent execution. Evidence: `internal/m8/hub.go:4201-4219,4267-4346,4357-4373`.

Recommended state machine:

1. In a short SQLite transaction, insert a metadata-only operation row keyed by inbound identity and source message ID, assign `operation_seq` and `base_generation`, and mark it `pending`. Do not update the active sticky state yet.
2. Resolve the requested/current stable Agent ID and current authorization/online state. If invalid, fail visibly and terminally reject; leave state and expiry unchanged.
3. Send the business envelope through the existing route path once. For a selection-only message, skip business delivery entirely. A successful business route or successful selection-only action may then complete a transaction that compare-and-sets the new state/generation/expiry and records terminal `accepted`; route failure records `rejected` and leaves the old state unchanged.
4. A successful direct WebSocket write remains `accepted` under current route semantics. If the business message was routed but sticky-state commit failed, or a process crash left a `pending` operation, keep the result unresolved; reconcile by durable delivery ID when one exists. Do not require every R14 message to use durable delivery and never blindly resend. If a `pending` operation has no reconciliation evidence after 10 minutes, mark it terminal `unknown`; retain `pending`/`unknown` operation rows until reconciliation completes. For any key with `pending`/`unknown`, pause implicit sticky routing and reply visibly: 「上一条消息的投递结果未确认，请显式指定 Agent 或从候选中选择」. Only explicit operations are accepted while the key is paused; after an `unknown` is reconciled, resume implicit routing only if the binding still passes authorization and time checks.

**已定（组长 2026-11-02，迟到对账只结案）**：若已投递但状态提交失败的旧操作，后来由 durable delivery ID 证实 Hub 已受理，只将原操作记为 `resolved`，保存实际 delivery ID 与对账时刻。MUST NOT 补写目标、续期、增代际或重发正文；当前合法绑定保持不变。该旧操作造成的隐式路由暂停可在结案后解除，但是否可续聊仍逐次检查当前绑定的授权与原时间窗。没有 durable 证据仍保持 `unknown`，只接受显式操作。新增隔离专格 `TestR14LateReconciliationPreservesNewerBinding`：选 B 后旧 A 才结案，B 的稳定 ID、generation、selected_at 与 expires_at 全部不变；对账写回旧目标的变异必须红在 `LATE_RECONCILE_OVERWROTE_BINDING`。裁定入站 UTC 为 2026-11-02T11:44:34Z；本会话只读查到已到盘未注入后收悉，不将 helper 的 admission_unknown 当作组长未答复。

If `FeishuMsgID` is absent, bypass R14 without reading or writing sticky state. An explicit-prefix message follows the old routing path; an unprefixed message returns candidates and is not routed to the remembered/default target (even when that would otherwise be a sticky hit).

Persist the intent before routing so a crash cannot erase the fact that a route may have occurred. If a route fails before acceptance, record `rejected` and preserve the previous binding/expiry. A successful direct write is recorded `accepted` under current semantics; do not claim helper injection or Agent execution. Only a route accepted followed by sticky-state commit failure, or a crash leaving a `pending` row, becomes `unknown` when it cannot be reconciled. A durable delivery ID is used for reconciliation when available, but there is no durable-only requirement. No design language promises cross-action atomicity or exactly-once external execution. Existing route paths and receipts otherwise remain unchanged. Evidence: `internal/m8/hub.go:4201-4219,4267-4346,4357-4373`.

### Persistence schema, migration, cleanup, and rollback

PM06 explicitly selects SQLite persistence, so the recommendation is to store R14 state in the Hub's existing SQLite main database; do not create a PG mirror/table. The Identity PG store remains authoritative for user/Agent lookup, but it is a separate database and cannot provide a cross-database foreign key or atomic transaction. Evidence: `internal/store/sqlite.go:57-66`; `internal/identity/pgschema.sql:6-65`.

Proposed additive migration: `internal/store/migrations/0072_feishu_agent_sticky.sql`, after current `0071_coordinate_review.sql`. Draft tables (final names/types/checks to be reviewed before implementation):

| Table | Proposed columns/keys |
|---|---|
| `r14_feishu_sticky_state` | `user_id_lc TEXT`, `bot_channel_id TEXT`, `agent_id TEXT`, `generation INTEGER`, `selected_at_utc TEXT`, `expires_at_utc TEXT`, `last_source_message_id TEXT`, `updated_at_utc TEXT`; primary key `(user_id_lc,bot_channel_id)`. No cross-database FK; no message body, open_id, credential, or mirror/session fields. Index `r14_sticky_state_expiry(expires_at_utc)` for expiry lookup and `r14_sticky_state_activity(updated_at_utc)` for bounded activity cleanup. |
| `r14_feishu_sticky_operation` | `operation_seq INTEGER PRIMARY KEY AUTOINCREMENT`, `user_id_lc`, `bot_channel_id`, `source_message_id`, `operation_kind`, `base_generation`, `result_generation`, `target_agent_id`, `decision`, `status`, `created_at_utc`, `completed_at_utc`; unique `(bot_channel_id,source_message_id)`, index `(user_id_lc,bot_channel_id,operation_seq)`, and cleanup index `(status,completed_at_utc)`. Metadata only, no body/open_id. |

Migration is additive `CREATE TABLE`/`CREATE INDEX` only; do not alter existing tables or migrate existing messages into sticky state. The runner sorts migrations, runs a migration transactionally with its digest ledger, and returns an error through `Migrate` on failure; the Hub startup caller must propagate it rather than run with half the schema. Evidence: `internal/store/sqlite.go:99-103,180-219,335-350,636-652,732-747`; current r48 migration `internal/store/migrations/0071_coordinate_review.sql:1-40`.

Retention/cleanup is **已定（组长 2026-11-02）**: keep one state row per key; remove it 30 days after its last activity (`updated_at_utc`, refreshed only by an accepted user message or successful explicit switch, not by candidate prompts or Agent replies). Remove terminal operation rows 30 days after completion. Keep `pending`/`unknown` rows until reconciliation completes; once resolved, retain the terminal row for 30 days from completion. If the principal is revoked/disabled or the target Agent is revoked/suspended/changes owner, invalidate immediately on use (fail closed) and delete the state row in the next cleanup run. Run cleanup hourly in bounded batches; implementation records the fixed per-run batch cap and tests that it is enforced. The 30-minute routing expiry does not itself delete the row because the previous target is still needed for candidate ordering. Use the activity index for state cleanup and the terminal status/completion index for operation cleanup.

Older Hub startup against the additive 0072 schema **must be tested in T-07**, not inferred from source. On rollback the old Hub leaves new state tables untouched and runs legacy routing. On the subsequent upgrade GO, include one reviewed SQL transaction that invalidates all old bindings by setting `expires_at_utc=updated_at_utc` and converts every `pending` operation to `unknown`; retain rows, do not add a cleanup command and do not drop 0072 objects. Illustrative SQL, to be reviewed with the actual finalized schema before any GO (not run in this task):

```sql
BEGIN IMMEDIATE;
UPDATE r14_feishu_sticky_state SET expires_at_utc=updated_at_utc;
UPDATE r14_feishu_sticky_operation SET status='unknown' WHERE status='pending';
COMMIT;
```

The prior design only inferred that a pre-0072 binary might ignore later ledger rows; that inference is not acceptance evidence. R14-SQLite migration to 0072, starting the isolated old Hub on that migrated DB, and the rollback SQL effects are explicit T-07 isolation tests. Evidence for migration iteration/ledger behavior (not a compatibility proof): `internal/store/sqlite.go:180-219,732-747`.

### T-10 short-name duplicate preflight

| Entry | Pre-submit check insertion point | Required behavior and current fallback |
|---|---|---|
| Self-host enrollment approval | `internal/m8/console_auth.go:922-930`, after `name` is read at 923 and before approve POST at 927-929 | Check the normalized short name under the current owner against both active and suspended `np_agent` rows. On collision, show a clear message and do not send the approve request. |
| Hosted Agent creation | `internal/m8/console_auth.go:1324-1338`, after `body.name` is read at 1326 and before create POST at 1334-1336 | Same owner-scoped check and behavior; do not block a different owner's same name. |

The one permitted new R14 interface is a narrow, read-only duplicate-check endpoint backed by Identity PG. It derives the caller's owner from the authenticated Console principal, accepts only the normalized `name_lc` to check, and returns a boolean indicating whether that name is used by an `active` or `suspended` `np_agent` under that owner. It returns no names or Agent identifiers and has no caller-selectable owner, so it cannot query across owners. Both entry points call it before submit and fail closed if the check errors or reports a duplicate. The existing PG partial unique index `(owner_lc,name_lc) WHERE state IN ('active','suspended')` remains the final concurrent-race protection; `insertAgent` maps its conflict to `ErrNameTaken`. Evidence: `internal/identity/pgschema.sql:63-65`; `internal/store/migrate/migrations/0100_identity_baseline.sql:74-76`; `internal/identity/pgstore.go:616-627`. Legacy `session_endpoint(key_id,session_name)` names are outside this identity index and remain outside R14/T-10. The current `HandleConsoleAgents` implementation reads `Repo.ListConsoleAgents` and returns only `id/session/online`; it does not provide authoritative coverage of owner-scoped PG `np_agent` short names and suspended state, so the legacy Console list is not the preflight source. Evidence: `internal/m8/console_auth.go:1654-1682`.

The endpoint authorization contract is: unauthenticated requests receive 401; an authenticated user can check only their own owner-scoped name; a same-name Agent belonging to another owner neither leaks its identity nor makes the caller's name unavailable. The handler has no owner input; owner is derived from the authenticated principal. Test both active and suspended collisions for the caller, a same-name other-owner row, unauthenticated access, and an attempted cross-owner query. Mutations that remove owner scoping or either page's pre-submit check must fail the corresponding isolation/prevent-submit assertions. The unique PG index still rejects a concurrent bypass.

### T-07 isolated test plan for R14-S001–S023

Use Go unit tests for pure matching/time/format decisions; Hub plus isolated SQLite/Identity fixtures for state, restart, authorization and failures; browser tests only for Console short-name preflight and local mocked Feishu text rendering. Mutations below must fail at the named semantic assertion, not merely produce a syntax/build error. Real Feishu desktop/mobile copy-and-return remains R14 T-08 and is not part of this task.

| Scenario | Test layer | One required semantic mutation (must turn this row red) |
|---|---|---|
| S001 | Hub/SQLite integration | Remove `bot_channel_id` from the state key; a second bot inherits the first bot's Agent. |
| S002 | Hub identity integration | On identity lookup failure, fall back to the previous/default Agent; assert business delivery is zero. |
| S003 | Go unit + Hub integration | Include same-group other-owner Agent in the personal set; assert it never becomes implicit target. |
| S004 | Hub + Identity integration | Trust stale `session_endpoint.active` or skip current Agent-state check; assert suspended/offline target is rejected. |
| S005 | Hub/SQLite integration with route fault | Commit explicit selection before rejected route; assert no default/expiry change on failure. |
| S006 | Go unit + Hub integration | On explicit target error, fall through to old sticky target; assert no business delivery. |
| S007 | Go unit with fake clock | Change `<30m` to `<=30m`; exact 30:00 case must fail. |
| S008 | Hub/SQLite integration | Permit an older generation to commit after a newer explicit switch; assert target remains newer. |
| S009 | Hub/Identity integration | If old Agent is unavailable and another is sole online Agent, auto-route to the latter; assert zero delivery until explicit choice. |
| S010 | Store failure integration | On state-read error, treat as empty/one-Agent state; assert visible failure and zero delivery. |
| S011 | Go unit | Remove prior-valid-target-first ordering or use unstable sort; assert exact candidate order and `#full.name` format. |
| S012 | Go parser + Hub integration | Parse a pasted multi-line candidate block as first target plus body; assert target delivery and state mutation are both zero. |
| S013 | Hub integration | Remove the exactly-one-current-online path; assert the single-owner available Agent remains prefix-free. |
| S014 | Hub + Identity integration | Count registered/offline or another owner’s Agent; assert only the current owner’s online-active set determines this path. |
| S015 | Restart integration with temp SQLite | Reset expiry to `now+30m` during startup; assert original absolute expiry is preserved. |
| S016 | Concurrent Hub/SQLite integration + race | Remove generation CAS or source-message uniqueness; assert stale overwrite or duplicate route is detected. |
| S017 | Hub regression integration | Put sticky handling before command/selector dispatch; assert system, sync, group and explicit routes retain their existing path. |
| S018 | Go text/parser test + browser against local mock only | Feed system candidate text into AMR/Agent routing or strip `#`/line breaks; assert neither occurs. Real Feishu copy is deferred to T-08. |
| S019 | Hub/SQLite fault-injection integration | Hide a post-route sticky-commit failure as success or auto-replay; assert visible `unknown`/blocked-key outcome and no duplicate. |
| S020 | Identity/Hub integration | Key by mutable Agent name rather than stable ID; assert revoked/recreated same-name Agent does not inherit old binding. |
| S021 | Go unit using fixed names | Prefer prefix over exact match; `#ts3dev55` must choose exact and `#ts3dev` must return both candidates. |
| S022 | Go parser + Hub authorization integration | Accept middle-of-body selector, send blank-body business envelope, or skip cross-owner authorization; assert each is rejected/selection-only as specified. |
| S023 | Browser + Identity API integration | Remove either entry's pre-submit check; duplicate active/suspended name must stop request before POST, while DB index still rejects a concurrent bypass. Exercise endpoint authorization: own active/suspended collision returns `true`; same-name other-owner data does not affect the caller and no names/IDs are returned; unauthenticated access is 401; cross-owner input cannot select another owner. Mutations removing owner scoping or a page check must fail these semantic assertions. |

Additional isolated T-07 migration/rollback test (not an S024): apply 0072 to a disposable copy of an r48 database, start the isolated r48 Hub binary against it and record successful startup/exit behavior, then exercise the reviewed rollback SQL to confirm all state expiries are invalidated and all pending operations become unknown without dropping rows/tables. This is a required compatibility test, not a claim that the old Hub is already compatible; do not test multi-Hub coordination.

### Pending group-lead decisions

1. 原待定点（保留）：“Confirm one authoritative Hub/SQLite writer for the R14 key space; otherwise define shared-store/coordination before implementation.” **已定（组长 2026-11-02）**：每个环境只有一个 Hub 进程写一个 SQLite 文件（dw123 现网、预发各一个 systemd 单元）；R14 以此为不变量，不做跨 Hub 协调；T-07 不测多 Hub。
2. 原待定点（保留）：“Approve retention duration and purge semantics for expired previous-target state, terminal operation rows, and identity/Agent revocation.” **已定（组长 2026-11-02）**：每键一行状态，最后活动 30 天后清除；终态操作记录完成 30 天后清除；`pending`/`unknown` 保留到对账完成。principal 被吊销/停用，或 Agent 被吊销/停用/换属主时，使用时立即失效（fail closed），下一轮清理删除。每小时清理一次，分批且每批有上限；与 R15 的 30 天口径一致。
3. 原待定点（保留）：“Set wall-clock rollback/skew handling beyond the fail-closed proposal, including whether a trusted external clock is required.” **已定（组长 2026-11-02）**：不引入外部时钟；当前 UTC 早于 `selected_at_utc`（任何回拨）或晚于 `expires_at_utc` 时，不走粘滞，改回可见候选或重选提示，不设容差；只记录、不告警。PM01 的精确到期边界为 `current UTC >= expires_at_utc`，在 30 分钟整时已过期。
4. 原待定点（保留）：“Decide handling when Feishu supplies no stable `FeishuMsgID`, and the exact user-visible recovery for `pending`/`unknown` delivery/state outcomes.” **已定（组长 2026-11-02）**：没有 `FeishuMsgID` 的消息不走 R14，不读写粘滞状态；有显式前缀按旧路径路由，无前缀回候选，不按默认对象投递。存在 `pending`/`unknown` 时暂停该键隐式粘滞路由，并显示「上一条消息的投递结果未确认，请显式指定 Agent 或从候选中选择」；有 durable delivery ID 就据此对账。`pending` 超过 10 分钟仍无证据转终态 `unknown`；此后只接受显式操作，直到对账解决。无需所有 R14 消息走 durable 路由；直写成功按现有语义记 `accepted`。只有已路由但状态提交失败，或进程崩溃留下的记录才算 unknown。
5. 原待定点（保留）：“Approve the T-10 authoritative short-name preflight lookup source (existing Identity-backed API vs a narrow read endpoint); the legacy Console list is not presumed equivalent.” **已定（组长 2026-11-02）**：新增一个窄只读接口，以 Identity PG 为准，只查当前登录者本人 owner 下 `name_lc` 是否被 `active` 或 `suspended` 的 `np_agent` 占用，只回布尔值，不列名字、不跨 owner；两个入口都调用。PG 部分唯一索引仍为最终兜底。旧 Console 列表不是权威来源。这是 R14 范围内唯一允许新增的接口；必须覆盖本人、他人 owner、未登录权限格及对应变异。
6. 原待定点（保留）：“Approve whether older-Hub rollback requires an explicit state cleanup command or a defined wait/expiry procedure. Migration is additive and old-binary compatibility remains to be tested in an isolated future task.” **已定（组长 2026-11-02）**：不新增清理命令；旧 Hub 期间新表保留不动。之后再升回带 R14 的 Hub 时，切换 GO 加入一条经审定 SQL：将全部状态行的 `expires_at_utc` 置为 `updated_at_utc`，并将所有 `pending` 记为 `unknown`。旧 Hub 能否在带 0072 的库上启动须在 T-07 隔离实测，不凭代码推断。

## T-05 / T-06 / T-10 实现落点（第 108 条授权）

本节对应实现源码提交 `59e859e1f3bb4099e8f15da246070b549d598263`；最终源码分支和验证证据以交接记录及交件 tip/tree 为准。上文 T-04 的“仅设计、不授权实现”是当时边界，本单已另派 T-05、T-06、T-10、T-07；T-08 真实飞书及 T-09 独立验收仍不在本单。

- R14-001/002/007：私聊挂点留在原 `dispatchSelector` 与最终 `dispatchDefaultPersonalSession`；只对可信 Feishu 入站及新平台身份接线。每次检查真实连接绑定的 `metricsAgentID == np_agent.id`、属主及 active。R14 信封保存限制性目标元数据；入队及恢复 drain 再核同一稳定 ID，防旧消息送给同名新 Agent。既有非 R14 信封不经过该附加检查；跨成员全名复用原跨成员授权与投递路径，不读写本人 K。
- R14-003/004/005/006：同 K 引用计数互斥车道在决策、意图、投递、提交全过程串行；SQLite 分配 operation_seq 为持久追溯顺序。短名精确优先，再作唯一前缀；仅选择不发空业务。纯多行候选回贴、候选提示、系统命令及重复源不更新绑定。严格 UTC 窗口 `selected_at <= now < expires_at`，回拨无容差；状态时钟字段损坏按读取失败处理，不将零值伪装有效锚。
- R14-008：0072 仅建 `r14_feishu_sticky_state`、`r14_feishu_sticky_operation` 及索引。operation 草案定稿增补 `delivery_id TEXT NOT NULL DEFAULT ''`、`source_envelope_id TEXT NOT NULL DEFAULT ''`，均为必要元数据，不存正文/open_id/凭据。后者保留原路由信封 ID，用于 enqueue 成功但 delivery ID 尚未落回的崩溃窗口；不改原路由/回执 ID。按源信封 ID、目标稳定 ID、principal 三者查原 `session_delivery.envelope_json` 取证；不得据此重发。
- 成功状态提交与 operation 终态在一个 SQLite 事务内完成，跨投递动作不原子。CAS 不因状态行被清理后重建而复用仍可完成的旧代际：无状态时从保留的 operation 元数据取代际上界；旧完成仍不能覆盖新绑定。拒绝投递不更新默认，受理后状态提交失败显式回未确认，并保留 pending/unknown。
- 超时和清理：owned 后台 worker 随 Hub 生命周期退出，每分钟以至多 200 条将已满 10 分钟的 pending 转 unknown；每小时作一次状态/终态保留清理。单次最多检查 200 个状态，游标轮转防活跃前页饿死后页；失效删除尝试与 30 天保留删除合计上限 200。撤销/停用/换属主使用时立即拒绝；后续轮转清理删除，pending/unknown 不按保留期删除。只有真实对账后从完成时刻保留 30 天。清理过程不续期，TTL 过期本身不抹掉合法上次候选信息。
- R14-009 / T-10：唯一新增接口为 `GET /api/console/agent-name-used?name_lc=<normalized>`，仅新平台注册，经过现有 Console principal guard。owner 取登录态；只返回 JSON boolean，拒绝 owner 参数与额外查询字段，不返回名称/ID。权威查询为 Identity PG 的 active/suspended Agent；查不到与已 revoked 返回 false，读取失败 503，未登录 401。两入口在提交前调用并在错误时停止；原 PG 部分唯一索引继续兜底并发。旧平台页面关闭本前置检查，旧 `session_endpoint` 名称不纳入。

### 隔离验证与边界

测试使用临时 SQLite、embedded PG、固定 h.now、localhost 真实 Hub WebSocket 与假 Feishu 文本传输；不使用真实业主/Agent、机器人凭据或线上 DB。Go 单元覆盖纯匹配/候选/时间；Hub 集成覆盖 S001～S023 的身份、状态、路由、失败、重启、并发与 API 授权；本地浏览器覆盖 T-10 两入口及 S018 文本 mock，不能代替 T-08 的真实桌面/手机回环。

兼容测试先从精确 r48 源构建仅供隔离测试的旧 Hub（二进制不是发布件），由旧 Hub 创建 r48 库后复制，再应用本单 0072、用旧 Hub 启动副本。另造 0072 对象冲突验证迁移事务失败不半提交且旧 Hub 仍可启动；按上文审定 SQL 验全部绑定失效、全部 pending 转 unknown、表和行不丢。无部署或生产回滚动作；未来 GO 仍须独立授权。

启动时将当前 SQLite capability 在启动 goroutine 前捕获，分钟/小时维护始终使用该固定引用，符合单 Hub/单文件不变量。首轮 race 报告发现后台启动读与测试故障注入替换 Hub.Repo 的竞态，修复提交 `59e859e1f3bb4099e8f15da246070b549d598263`；原始失败保留，最终门以修复后的复跑为准。


### T-07 implementation handoff

开发侧完成 T-05/T-06/T-10/T-07，测试补充锚 `e7d7e6ca35d612f5671a57ea584ee74633bee608`；所有最终命令/源码锚与日志 SHA256 在证据分支记录。相关场景 race 覆盖 m8/store/workpulse，m8 另全包 -v；同键不同消息的显式切换/无前缀并发专格持久记录 operation_seq、base/result generation 和实际目标。旧 r48 Hub 兼容夹具等待原进程 READY 后才发送退出信号；这仅修测试同步，不修改旧 Hub 启动/退出。T-08 真实飞书与 T-09 独立验收保持后续另派。


### 返修追加：进程内来源与默认关闭（已定（组长 2026-11-02））

裁定 431d53d3223c（F-01）、2386dfd99fd5（开关）：`DW_FEISHU_STICKY_ENABLED` 只认精确 `1`，默认关闭；另以与 main 相同的 `TrimSpace(FCS_RPC_URL)` 判定是否跨进程。两条件同时成立才允许R14粘滞。对应 R14-001 / S024～025，不改 FCS、newplat_feishu、spec_clarification_reply、terminal 等既有消费者的来源处理。FCS来源认证仍是另项风险，不声称本件修复RPC身份认证。

关闭时入口立即返回未处理、投递检查对任何元数据放行；清理worker不启动，直接/定时清理也在R14存取前返回。0072照常迁移；T-10与本开关无关（R14-009 / S026）。开启但FCS模式时记录 reason=fcs_rpc_mode；显式交回旧路由，无前缀只回本人当前候选，不按单Agent或旧绑定默认投递，不读写粘滞；worker也不启动。

### 返修：共享依赖与迁移补账

S002 是测试在存续连接中替换 Identity.Store、与断线清理读并发：包装器在连接建立前装配，故障以atomic.Bool切换。生产唯一Identity装配发生在wireNewPlatform、监听Serve之前，未发现运行期替换入口；身份业务状态变化不替换Store指针。0072因0068～0071补账分支清除legacy标志而重放CREATE；改为复用精确DDL核验，完整匹配只记账、全缺事务建表+账、部分或异构拒绝。旧70个迁移文件字节不变，r48同构库副本的70行旧账version/digest/applied_at排序哈希不变，升级只加0072一行，重复迁移及旧Hub启动核验另附日志。

### 返修：旧连接用例的基线与测试同步

a0c97a2a1322要求的九格及镜像在r48同命令race count=3均绿，原文保留，不称基线红。其Hub.Identity均nil，未进入R14路由；serveSession连接注册、初始引导帧与发布顺序对r48未改。原R14唯一新增全局时序副作用是New启动sticky维护worker；本轮默认关闭时已不启动。r48镜像屏障用例证明引导帧先到、运行时连接尚未发布是已有允许状态。正常建连夹具补等同代连接发布，刻意测试发布前的镜像用例使用raw建连；这是测试准备条件，不把引导帧当ready协议，也不改产品发布时刻。

6921ce22db01裁定：helper offer CAS冲突属于r48同代码的既有产品并发，另单S-R49-HELPER-OFFER-RACE修，在R14交件后、R10前执行；本件不改helper_update.go。两格保留两边原文及r48同命令三轮读数，未复现也如实报告；其余race仍须通过，不扩大既有豁免。
