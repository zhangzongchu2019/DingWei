# Design

## ㊿-补132 修订来源

本次 S-R62-EXTSEAT-AMEND 基于设计 `124d24ee3818762f977920d5a015e4dfdca80663`，落实 `review/opus50tc2k/rulings` 的 `148f1be7`、㊿-补132③：加入普通编组后保留临时配对；注册可不指定目标、不建临时配对；内部 Agent 飞书抄送默认关，外部仍按㊿-补131默认开。以下是后续切片的设计合同，不是当前产品行为声明。本单不修改或解冻 TTL `3639406c`，不改产品、迁移或部署。

## Baseline checked

Read-only source baseline is main `206d41b26dab646c0e50a111eed5a45ccf7c72f6`.

- `internal/extshared/extshared.go:17-27` defines token 14 days, temporary AG 3 days, and external message 24 hours. `internal/extseat/service.go:61-66` defaults the pending queue limit to 1000; `internal/extseat/service.go:77-89` and `internal/extseat/inbox.go:149-165` apply `MessageTTL` to both directions.
- `internal/extshared/extshared.go:55-80` restricts external short names and `SplitAgentName` to ASCII. `internal/extseat/service.go:68-70,110-150` binds identity to provider/tenant/issuer/subject and builds an ASCII short-name plus AG address. `internal/feishulogin/types.go:93-100` currently exposes only name, en_name, open_id, union_id, tenant_key. `np_external_principal.display_name` exists in migration `0105_extseat.sql:4-10`; `np_agent` has no separate display-name field in `internal/identity/model.go:138-153` or migration `0100_identity_baseline.sql`.
- `internal/m8/amr.go:1376-1377,1503` accepts ASCII mention tokens only. `extshared.SplitAgentName` is used by `internal/m8/extseat.go:30`, `internal/identity/service.go:428`, and `internal/subagent/route.go:39`; `agent-http-mcp-gateway` spec explicitly keeps public IDs and external short names ASCII.
- Temporary pairs are stored in `np_temp_group` and `np_temp_group_member` (`internal/store/migrate/migrations/0106_tempgroup.sql:1-24`). Ordinary groups use `np_group` and `np_group_member` (`0100_identity_baseline.sql:98-116`). Ordinary reachability is evaluated through `identity.AgentsCanCommunicate` and `internal/m8/newplat_grouping.go:12-53`; external traffic also has dedicated paths in `internal/m8/extseat.go` and `internal/extseat/inbox.go`. Console group listing currently skips external seats (`internal/m8/newplat_groups.go:111-179`).
- Current external Feishu copy is enabled by default through `DW_MCP_FEISHU_COPY_ENABLED`, queues the full body, and sends a private Feishu message (`internal/extseat/feishu_copy.go:26-61,66-84`; `cmd/workpulse/r53_extseat.go:189-227`; migration `0111_extseat_feishu_copy.sql`). It only models external seats and their Feishu principal; internal Agent-owner identity can be resolved through `np_identity` (`0100_identity_baseline.sql:33-38`) but may have zero or multiple Feishu identities.
- External message records live in `np_extseat_inbox`, `np_extseat_outbox`, and `np_extseat_send_key` (`0105_extseat.sql:30-58`). Expiration marks inbox/outbox work dead; the inspected path has no bounded 500-record retention cleanup. `send_key` carries the client idempotency key and expires on the message lifetime.
- MCP gateway rejects `upload_file` with 404 (`internal/mcpgw/gateway_test.go:446`). Hub file upload capability is separately issued for authenticated helper WS sessions (`internal/m8/file_capability.go:252-365`) and requires an online session. External MCP scopes in `0105_extseat.sql:21-28` do not include file upload.

## Invariants

1. Stable address and authorization keys remain opaque ASCII identifiers. Display names are presentation data only and never identify, authorize, merge, or route an Agent.
2. A canonical full address or an authenticated Agent picker selects a recipient. A Unicode display-name match alone must never trigger an AMR send.
3. 所有 Agent 通信入口使用同一授权判断：先核调用者身份、双方稳定 Agent ID/有效状态及租户准入，再核真实普通组或有效临时配对。临时配对只授权它的两个成员，不能扩展到其它组员。显式指定组的请求只核指定组，不跨组回退。
4. Feishu copy is a one-way notification. Feishu replies do not become MCP/Hub messages. Copy failure does not fail or delay the primary message.
5. Retention is bounded without deleting active work or weakening send idempotency. If active rows occupy the per-seat limit, reject new work visibly.

## Lifetime policy

- External token sliding lifetime remains 14 days.
- Temporary AG sliding lifetime becomes 7 days. Preserve the current renewal events (accepted sends and first valid received ACK); polling does not extend it. Ordinary `np_group` membership does not expire because that is the existing internal group model.
- External inbox/outbox message expiry becomes 7 days from Hub acceptance. Token or group renewal does not extend an individual message. At expiry, unreceived work becomes terminal-expired and is no longer delivered.
- Temporary AG expiry disables only the bootstrap pair. It does not revoke the external Agent or remove ordinary group memberships. Token expiry disables authentication but preserves the Agent and its memberships; reauthentication of the same verified principal resumes the same Agent identity. Expired messages remain expired.

## Name and mention design

保留已有席位的canonical ASCII地址及唯一性，不因普通组/临时配对变化重命名。无AG新席位的身份和兼容地址扩展见可选配对入口，不把现有AG名字编码当成永久强制开户条件。Add a separate Unicode `display_name` to the Agent presentation model, populated from verified Feishu `name` with a documented fallback to `en_name`; do not request department permission. Keep the value bounded, normalized for storage/display, and HTML-escaped. Do not apply compatibility folding or transliteration to the canonical address.

The display may collide. Agent lists and group members show the Unicode name plus the immutable canonical full address and, where relevant, a tenant label. The address remains the full mention form. AMR must not resolve a bare Unicode display name. A future mention picker may render Unicode while submitting an exact Agent ID/address; that is a separate UI/API contract with ambiguity and spoofing tests.

For admitted cross-tenant external Agents, show an administrator-assigned tenant label beside the Unicode name and canonical address. The label comes from a controlled tenant-admission registry; do not render raw `tenant_key` as the public label. Never show `open_id`, `union_id`, issuer credentials, or bearer data. Tenant admission remains an explicit allowlist decision; grouping does not implicitly admit a tenant or merge principals. Duplicate labels or names are disambiguated by tenant label and canonical address.

## Ordinary groups and temporary AG coexistence

按㊿-补132③，临时配对和普通编组并存。首次及后续加入普通组只增加相应 membership，不解散、转换或续期原临时 AG；离开普通组也不重建临时 AG。临时 AG 继续受自己七天时钟、原目标和成员身份约束。

### 授权选择与绑定

对于未显式指定组、按稳定 Agent 选择收件人的直发，共同前置检查通过后，以下任一授权成立即可：①双方共享有效的真实普通 `np_group`；②双方恰为同一有效临时 AG 的原有两个稳定成员。配对授权仅覆盖原目标，不能把原目标所在普通组的其它成员算作临时组成员。外部 Agent 仍不能自行入组或邀请任意对象，成员变更只由有权 owner/admin 执行。

两路同时有效时，无显式组直发优先选择真实普通组，多个普通组按稳定组引用的固定顺序选择；否则选有效 AG。这样普通组流量不会隐式刷新临时配对时钟。此优先序是本修订的技术设计选择，不冒称业主原话；受理后不得因路径变化静默换组。

对于显式指定 AG 或普通组的请求，只校验该组与目标；该组已失效时不得利用另一条授权把原请求改投其它组。受理前由服务器确定并记录实际授权类型、稳定组引用及成员身份，后续重核不能根据显示名或客户端伪造字段扩大授权。临时 AG 在 `np_group` 的可达性镜像不是独立的普通组授权：即使镜像成员尚未清理，AG 已过期/解散也不能借镜像放行。实现须按服务端组类型/关联识别，不能仅凭组码字符串猜测。

旧 AG-only 调用即使没有显式 JSON 组字段，也仍以已认证 seat 绑定的 AG 为上下文，按显式组处理。不能把“字段未传”解释成新的跨组直发授权；新的普通组/稳定Agent选择入口须在切片三接口版本设计中明确表达。

API、MCP、AMR、内部路由、外部 outbox 投影和 inbox 创建均使用这份规则；`AgentsCanCommunicate` 不能仅查到临时组镜像就绕过 AG 时效。普通组和 AG 由各自管理者维护，不合并编号或把 AG 数值转换成普通组。内部普通编组既有权限、跨租户独立准入和稳定身份检查保留。

| 变化 | 临时配对 | 普通组 | 新通信的结果 |
|---|---|---|---|
| 加入一个或多个普通组 | 原配对保留，期限不因入组刷新 | 加入对应 membership | 无显式组直发可用任一合法路径；显式组只核该组 |
| 离开一个/最后一个普通组 | 有效原配对不受此操作删除；已过期者不重建 | 只移除指定 membership | 普通组路径停止；原配对仍有效时仅可与原目标通信；否则拒绝 |
| 临时 AG 到期或显式解散 | 该配对授权停止，轮询不续期 | 已有普通组关系不变 | 仍可经有效普通组通信；绑定失效 AG 的请求拒绝，不偷换路径 |
| token 到期 | AG 行保留且独立计时 | 成员行保留 | MCP 鉴权停止；同主体重认证恢复原身份，不能借此续消息或重建失效配对 |

## Optional temporary pairing onboarding

飞书身份验证和租户准入通过后，入口允许不指定目标并明确选择“不建临时配对”。签发席位/令牌不等于授予通信权限：不创建 AG、默认目标、`AG0` 或虚假普通组。已有配对席位的 ASCII 地址不重命名；无 AG 席位也必须有稳定 Agent ID，身份分配不得依赖创建临时组。其新地址编码/旧客户端兼容在切片三接口设计中冻结，不把可选 AG 重新变成开户必填条件。

未配对且尚未加入任何普通组时，MCP 登录/工具发现可用，但通信工具 `send/send_message`、`recv/wait_for_messages`、`check/list/list_online`、`clear/ack` 拒绝执行，沿用现有拒绝映射 HTTP 403、协议错误码 1003、reason `not_permitted`（外层 envelope 随现有 codec）。拒绝不创建消息/租约、不返回其它成员或正文、不续 AG，也不产生“信件作废”通知——此时根本没有受理信件。不能把“没有创建 AG”误报成某个已存在 AG 的 `group_dissolved`。

Console 对有权查看者显示“已验证／未编组”、临时配对“未创建”、目标“—”，可展示稳定身份及管理员入普通组入口；不显示不存在的 AG，也不自动选择目标。管理员加入真实普通组后，普通组范围的通信按统一规则放行；未指定 AG 的接口必须能够表达普通组授权，旧版仅支持 AG 的请求不能擅自映射到任意普通组。无权 owner/admin 仍不能代替本人或给自己扩权。

## 已投影件边界 PM01（切片三）

已知基线边界：PG 未投影层重核组权限，失效时拒绝/结算；已进入 SQLite `session_delivery` 的件可能在原目标上线后继续投递，组过期/解散并不自动撤回。TTL 片已将此登记为内外普通编组共同处理的“失去授权后在途件”，本修订不改变它、不写正向测试把该行为固定为目标合同。消息七天 TTL 扩大了潜在在途窗口，仍归切片三。

并存后的区别是：AG 到期不代表真实普通组失权，普通组成员移除也不代表有效原配对失权。只撤销相应授权路径，不能以另一条路径偷偷替换已绑定组的旧件、换目标或延长消息。AG 路径的有效受理/首次ACK才按既有规则续 AG；普通组消息不续 AG，因此普通组消息未到期而 AG 已到期并不矛盾。切片三须统一决定：绑定路径丧失权限后的未发送投影件如何处理、是否及如何重新授权、终态与通知的幂等。当前只钉安全面：未投影且所选组失权不发正文；此处不宣称已投影件已撤回，也不新增发送前 AG 围栏。

## Feishu summary copy and controls

内部与外部共用每 Agent 偏好及耐久通知 outbox，但缺省值不同：内部 Agent 默认关（㊿-补132③），即使 owner 有唯一 verified 飞书映射也不自动发送；外部 Agent 默认开（㊿-补131），且仍要求收件人映射唯一可信。已有显式 `on/off` 优先于缺省值；本修订不覆盖用户选择。内部用户显式开启后才发送，外部关闭后不再建新 copy。环境变量只能作 rollout gate，不能把内部默认关强制变开或覆盖 opt-out。命令仍只认 verified owner 与严格语法，例如 `Agent抄送 开 <canonical Agent>` / `Agent抄送 关 <canonical Agent>`；单 Agent 可省略 selector，多 Agent 歧义不改设置，重复命令幂等。

For external Agents, the existing verified external principal is the copy destination. For internal Agents, resolve the owner through `np_agent.owner_lc` → active `np_user` → verified `np_identity` Feishu links. If no link or multiple links exist, do not guess; offer an owner-selected primary link. The account preference applies to the named Agent, so a person with several Agents can control them separately.

The copy is a concise, deterministic summary, not the full message: sender/recipient, group, timestamp, bounded excerpt, and an authenticated Hub/View reference if a stable authorized link is available. Never include tokens, file contents, full message bodies, hidden prompts, or open IDs. Replies to copy messages are suppressed from re-entering MCP. Copy queue writes share the primary-message transaction where possible; a separate worker sends with retries, and copy errors never roll back the primary send.

The current `np_extseat_feishu_copy` table is specialized to external seats and stores full bodies. Prefer a generic notification outbox plus per-Agent preference records, or an explicitly reviewed generalization of that table. Do not send internal copies until the owner identity mapping and bot issuer are unambiguous. The environment flag may gate rollout but cannot override a user's persisted opt-out.

## 500-message Hub retention

Define the cap as 500 retained Hub message records per external Agent across both inbound and outbound directions. A conversation involving two external Agents consumes one record for each participant. The cap is independent of helper-side history and Feishu notification records. Retained records are available only through the existing authenticated external MCP surface; this proposal does not create a new public history endpoint.

On enqueue, take a per-seat transactional lock, first expire overdue records, then count that Agent's retained records. If the count is at the cap, prune oldest terminal records until a slot exists. Never prune queued, leased, or otherwise in-flight work before it reaches a terminal state; if all 500 are active, return the existing visible `queue_full` response. Keep a compact send-key/idempotency tombstone for at least the full 7-day message TTL even if the body record is pruned. Cleanup is batch-bounded and indexed by seat, terminal state, expiry/creation time; no global full-table scan.

This ordering prevents concurrent enqueues from exceeding the cap and prevents a retry from duplicating a message after its body is pruned. Metrics should report per-seat active count, retained count, prunes, and visible capacity rejects without logging message contents.

## Migration and slice order

Baseline main has migrations through `0111_extseat_feishu_copy.sql`. The following are planning slots only; implementation must re-check the final main and reserve actual migration numbers before coding.

1. Lifetime constants and tests: no schema migration.
2. Unicode presentation name: migration `0112` adds nullable `np_agent.display_name`; backfill external seats from the verified stored profile where available, otherwise leave null and render canonical address. No address rename.
3. 普通组并存与可选配对：`0113` 仅为计划槽，评审可选 AG 关联、开户与授权解耦、授权路径标识、tenant admission registry/受控展示标签及所需索引。不再计划 bootstrap-conversion 标记，不新增强制解散事务。复用普通组与临时组模型，不复制组数据、不造 AG、不把 AG 数值换普通组码；最终 DDL/迁移编号须以实现时主干重新核定，本单无 SQL。
4. Summary-copy preferences and generalized outbox: migration `0114` for per-Agent setting and generic durable notifications, preserving the extseat queue until old work drains.
5. Retention: migration `0115` for the per-seat retention ledger/counters and indexes if query-and-lock cannot safely enforce the limit on the final schema. Prefer no new table only if concurrency tests prove the invariant. These numbers shift if main gains migrations.
6. File-upload feasibility: no migration and no implementation.

## Rollback

- Lifetime: revert new-message and new-group constants; do not shorten already-persisted expiries in a rollback migration.
- Display: retain the new column/data; fall back to canonical ASCII addresses. Do not rewrite addresses back from display names.
- 编组回退：可停新开户/新成员变更入口，保留普通组、原配对、稳定身份及审计，不为回退重建过期 AG 或删除现有授权。旧构建若强制要求 AG/不支持无配对席位，不能直接换旧构建承载这些新状态；须先验证兼容回退件或停入口保留数据，不能以造假 AG 代替兼容设计。
- Copy: stop creation/sending of new notifications; preserve preferences/outbox state for later resume. Sent Feishu messages cannot be recalled by this rollback.
- Retention: disable pruning first; deleted message bodies are not recoverable. Maintain the 7-day send-key tombstone until its expiry.
- File upload: no change to roll back.

## Verification and mutation criteria

- Lifetime boundary tests at exactly seven days and just before/after; token remains 14 days; polling does not renew temporary AG; message expiry is independent.
- Unicode names round-trip and render safely, while address uniqueness, ASCII SplitAgentName, and current AMR routing remain stable. Duplicate Unicode names and cross-tenant same-name members render different canonical selectors.
- 编组矩阵：加入普通组后原配对仍在且不续期；直发两条授权路径与显式组不回退；临时配对不能投第三人；过期 AG 镜像不能放行；离开一个/最后一个普通组时有效原配对保留、无有效路径则拒绝；AG到期只停AG、token到期停MCP但保留成员；跨租户未准入拒绝。无配对注册成功但未入组时所有通信工具403/1003/not_permitted、无正文/写入/续期，Console如实显示；入普通组后仅按合法组通信。删除任一相应授权检查、强制入组即解散AG、缺目标即自动建AG或跨显式组回退，都必须在对应业务断言红；均为后续实现门，本单不冒称已跑产品格。
- 抄送矩阵：内部缺省关（有verified映射也不发）、内部显式开才发、外部缺省开且唯一可信映射才发；显式偏好优先、无/多映射不猜、summary不等于全文、单Agent简写/多Agent歧义、幂等、worker恢复及无回环。反转内部/外部缺省、绕过opt-out或发送全文，分别必须有对应红断言；本单仅修设计。
- Retention: mixed inbound/outbound exactly 500; 501st prunes only oldest terminal record; 500 active rows return visible queue_full; concurrent enqueues stay bounded; same client id within seven days remains idempotent after body pruning. Mutations removing lock/cap or pruning active work must fail.
- Upload feasibility only: document protocol, issuer/scope, file-size, malware/content checks, storage/retention, recipient ACL, signed-link expiry, resource use, and why the helper-only Hub ticket path cannot directly serve external MCP today. No upload endpoint or file token is added in this change.
