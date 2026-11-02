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
- On restart, compare current UTC wall time to the persisted absolute expiry; never add 30 minutes at startup. Revalidate the principal and the current target/connection before routing. If current time is earlier than the persisted selection time or clock consistency cannot be established, do not use the sticky target; return a visible re-selection response. The baseline has no persistent clock watermark, so this safeguard must be implemented and tested; exact response/clock-skew tolerance is **待组长定**.
- Each successful key-state change increments an integer `generation` in the same SQLite transaction as the state update. Each operation records its observed generation and uses compare-and-set; an older completion whose expected generation no longer matches cannot overwrite a newer binding. Agent replies, ACKs, duplicate source messages, and candidate responses never increment generation or renew the expiry. Use persisted source message identity for idempotence, not event timestamps or in-memory order alone. Evidence of current source message fields and dedup constraint: `internal/model/model.go:80-96`; `internal/store/migrations/0001_init.sql:34-50`.

### Same-key concurrency and trace order

Serialize each `(user_id_lc, bot_channel_id)` decision through one durable per-key operation lane. The transaction that allocates the next `operation_seq` and captures `base_generation` is the linearization point; record source message ID, operation kind (explicit/select-only/no-prefix), resolved target Agent ID, base/result generation, decision, and terminal status, but never message body. Do not use Feishu timestamps to order concurrent messages. The SQLite write/compare-and-set result defines the order and is inspectable after restart.

| Serial order | Expected behavior |
|---|---|
| Explicit `#A body` succeeds, then an unprefixed message | First operation routes only to A and commits A as the next generation; second reads that generation, rechecks A, and routes to A. |
| Unprefixed message precedes explicit `#A body` | The first uses the previously valid target only if the current generation is valid; with no valid target it returns candidates and sends no business message. The later explicit operation may then select A. The first result is not replayed. |
| No-prefix and explicit message overlap, or an earlier route has unknown outcome | The operation-sequence winner is processed first. The other waits; it rereads the committed generation after the first terminal result. If the first remains unresolved, do not route by sticky default or silently choose another Agent. Log both IDs and the blocking outcome. |
| Late completion / duplicate delivery | Apply only if `base_generation` still matches; otherwise record `stale_completion` and do not change the new generation. Duplicate `(BotChannelID, FeishuMsgID)` returns the recorded operation outcome and must not send again or renew TTL. |

The code currently has no R14 per-key operation lane or persistent generation. SQLite WAL plus a single connection pool serializes writes within this Hub process, but does not by itself provide a cross-Hub shared authority or a source-message ordering contract. Evidence: `internal/store/sqlite.go:57-66`; `internal/store/migrations/0001_init.sql:34-50`. Whether deployment guarantees exactly one Hub against one authoritative SQLite file, and the behavior if it does not, is **待组长定** before implementation.

### Delivery, state, and failure reconciliation

Do not claim a cross-action transaction between sticky state and Agent delivery. The existing `RouteEnvelope` can enqueue a durable `session_delivery` and drain it, or use direct WebSocket write; successful return means accepted/queued or written, not helper injection or Agent execution. Evidence: `internal/m8/hub.go:4201-4219,4267-4346,4357-4373`.

Recommended state machine:

1. In a short SQLite transaction, insert a metadata-only operation row keyed by inbound identity and source message ID, assign `operation_seq` and `base_generation`, and mark it `pending`. Do not update the active sticky state yet.
2. Resolve the requested/current stable Agent ID and current authorization/online state. If invalid, fail visibly and terminally reject; leave state and expiry unchanged.
3. Send the business envelope through the existing route path once. For a selection-only message, skip business delivery entirely. A successful business route or successful selection-only action may then complete a transaction that compare-and-sets the new state/generation/expiry and records terminal `accepted`; route failure records `rejected` and leaves the old state unchanged.
4. If routing returned success but the state commit fails, or a crash leaves the operation `pending`, mark/interpret the outcome as `unknown`; reconcile by source message and durable delivery ID where available. Never blindly resend the body. Until reconciled, block implicit sticky routing for K and present a visible temporary/confirmation response. If reconciliation cannot establish the result, require a fresh explicit action; do not silently route to the old or another Agent.

Persist the intent before routing so a crash cannot erase the fact that a route may have occurred. Where the existing route is direct/non-durable, source-message reconciliation may be insufficient; T-05 must either require a durable delivery ID for this path or preserve `unknown` and fail closed. No design language here promises exactly-once cross-system delivery. Whether a durable-only route is acceptable for all R14 messages, and the exact user-facing treatment of an unresolved operation, are **待组长定**. Existing route paths and receipts must remain otherwise unchanged.

### Persistence schema, migration, cleanup, and rollback

PM06 explicitly selects SQLite persistence, so the recommendation is to store R14 state in the Hub's existing SQLite main database; do not create a PG mirror/table. The Identity PG store remains authoritative for user/Agent lookup, but it is a separate database and cannot provide a cross-database foreign key or atomic transaction. Evidence: `internal/store/sqlite.go:57-66`; `internal/identity/pgschema.sql:6-65`.

Proposed additive migration: `internal/store/migrations/0072_feishu_agent_sticky.sql`, after current `0071_coordinate_review.sql`. Draft tables (final names/types/checks to be reviewed before implementation):

| Table | Proposed columns/keys |
|---|---|
| `r14_feishu_sticky_state` | `user_id_lc TEXT`, `bot_channel_id TEXT`, `agent_id TEXT`, `generation INTEGER`, `selected_at_utc TEXT`, `expires_at_utc TEXT`, `last_source_message_id TEXT`, `updated_at_utc TEXT`; primary key `(user_id_lc,bot_channel_id)`. No cross-database FK; no message body, open_id, credential, or mirror/session fields. Index `r14_sticky_state_expiry(expires_at_utc)` for bounded cleanup. |
| `r14_feishu_sticky_operation` | `operation_seq INTEGER PRIMARY KEY AUTOINCREMENT`, `user_id_lc`, `bot_channel_id`, `source_message_id`, `operation_kind`, `base_generation`, `result_generation`, `target_agent_id`, `decision`, `status`, `created_at_utc`, `completed_at_utc`; unique `(bot_channel_id,source_message_id)`, index `(user_id_lc,bot_channel_id,operation_seq)`. Metadata only, no body/open_id. |

Migration is additive `CREATE TABLE`/`CREATE INDEX` only; do not alter existing tables or migrate existing messages into sticky state. The runner sorts migrations, runs a migration transactionally with its digest ledger, and returns an error through `Migrate` on failure; the Hub startup caller must propagate it rather than run with half the schema. Evidence: `internal/store/sqlite.go:99-103,180-219,335-350,636-652,732-747`; current r48 migration `internal/store/migrations/0071_coordinate_review.sql:1-40`.

30 minutes is routing validity, not automatically a data-retention policy: an expired target may still be needed to place the last available target first in the candidate list. Proposed cleanup: retain only one state row per key; purge completed operation metadata after 30 days; preserve pending/unknown operations until reconciled; purge state after 30 days without activity, with immediate invalidation on confirmed principal/Agent revocation. **The 30-day retention and identity-removal cleanup trigger are recommendations, not PM decisions; 待组长定.** If approved, expiry index supports periodic bounded cleanup. Do not delete a row at the 30-minute TTL if doing so changes the PM03 previous-target candidate ordering.

Older Hub compatibility is code-inferred, not runtime-tested: an additive new table and a future `schema_migrations` ledger row are not read by a binary whose embedded migration list ends at 0071; the migration runner iterates its embedded files and reads the ledger without rejecting unknown future rows. Evidence: `internal/store/sqlite.go:180-219,732-747`. A downgraded Hub would use old routing and ignore sticky state. Rollback must not drop migration objects; before returning to the new binary, stale bindings must be cleared or allowed to expire, and pending/unknown operations must be handled explicitly. **The approved rollback runbook/cleanup method is 待组长定; old-binary startup compatibility still needs a later isolated test.**

### T-10 short-name duplicate preflight

| Entry | Pre-submit check insertion point | Required behavior and current fallback |
|---|---|---|
| Self-host enrollment approval | `internal/m8/console_auth.go:922-930`, after `name` is read at 923 and before approve POST at 927-929 | Check the normalized short name under the current owner against both active and suspended `np_agent` rows. On collision, show a clear message and do not send the approve request. |
| Hosted Agent creation | `internal/m8/console_auth.go:1324-1338`, after `body.name` is read at 1326 and before create POST at 1334-1336 | Same owner-scoped check and behavior; do not block a different owner's same name. |

The authoritative fallback is the existing PG partial unique index `(owner_lc,name_lc) WHERE state IN ('active','suspended')`; `insertAgent` maps its conflict to `ErrNameTaken`. Evidence: `internal/identity/pgschema.sql:63-65`; `internal/store/migrate/migrations/0100_identity_baseline.sql:74-76`; `internal/identity/pgstore.go:616-627`. Legacy `session_endpoint(key_id,session_name)` names are outside this identity index and remain outside R14/T-10. The current `HandleConsoleAgents` implementation reads `Repo.ListConsoleAgents` and returns only `id/session/online`; it does not itself prove owner-scoped PG `np_agent` short-name/state coverage. Evidence: `internal/m8/console_auth.go:1654-1682`. It must not be assumed authoritative for the PG index unless implementation proves its rows and suspended-state coverage. Whether T-10 may add a narrow owner-scoped name lookup or must reuse a verified Identity-backed read is **待组长定**. The index remains the final concurrent-race protection regardless of UI preflight.

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
| S023 | Browser + Identity API integration | Remove either entry's pre-submit check; duplicate active/suspended name must stop request before POST, while DB index still rejects a concurrent bypass. |

### Pending group-lead decisions

1. Confirm one authoritative Hub/SQLite writer for the R14 key space; otherwise define shared-store/coordination before implementation.
2. Approve retention duration and purge semantics for expired previous-target state, terminal operation rows, and identity/Agent revocation.
3. Set wall-clock rollback/skew handling beyond the fail-closed proposal, including whether a trusted external clock is required.
4. Decide handling when Feishu supplies no stable `FeishuMsgID`, and the exact user-visible recovery for `pending/unknown` delivery/state outcomes.
5. Approve the T-10 authoritative short-name preflight lookup source (existing Identity-backed API vs a narrow read endpoint); the legacy Console list is not presumed equivalent.
6. Approve whether older-Hub rollback requires an explicit state cleanup command or a defined wait/expiry procedure. Migration is additive and old-binary compatibility remains to be tested in an isolated future task.
