# D 路临时编组接口与持久状态

父提交 `c5152a213783e63626c161afefb34a7f45cfaea3`；业主规格为 openspec/changes/agent-http-mcp-gateway 的 GW-008、GW-010；技术依据为 rulings `5474e849eb6f7f81957ebe87e46505f15eb3d129` 的㊹-补12、补13、补14及组长 2026-11-07T02:04:13Z 改派。旧调研 af14c02c 的计数表建议已被明确覆盖为 PG 原生 SEQUENCE。D 前端冻结 `459105324f79629a3aac92cfb1de6e25547b1e31` 已原样合入。

## A 路消费者

`Service` 实现 `extseat.Groups`，只在 A 传入的 `repo.Tx` 内创建/反查/续期，不提交、不另开事务。取号 `nextval('np_temp_group_sequence')` 在该事务调用，但 PG 序号本身不随回滚退号；回滚留空号，重启不复用。创建回调恰一次，回调失败整体回滚；号决定外部名。内部对端可同时属于多个 AG。

`RecheckGroup` 按外部 `agent_id` 经成员表反查权威组；传入 Group/Generation 非零时只作为断言。组行锁串行化解散与发送/ACK，当前 generation 固定 1。锁内检查到期与双成员，内部 Agent 必须有效。A 另在返回后重核时钟与身份；D 扫描不承担请求授权正确性。

`TouchGroup` 的事件键由 A 给出（方向标识由其 delivery ID 前缀表达），D 持久去重；同键同 kind 不延寿，同键异 kind 返回 ErrIdempotencyKey。仅 accepted_send／first_received_ack 可续，时间及到期值单调。轮询、清单不触达。

## 0106 迁移与身份镜像

- SEQUENCE `np_temp_group_sequence`：bigint，1 起、最大 18 位、不循环；备份须包含其 last_value/is_called 状态，不能从组表最大号重置，回滚已烧号也不可复用。
- `np_temp_group`：code、seq、generation、created_by、created_by_union、created_by_display、owner_lc、external_agent_id、target_agent_id、created_at、last_used_at、expires_at、dissolved_at、dissolve_reason、notified_at。
- `np_temp_group_member`：code、agent_id、role、created_at；每组每角色唯一，外部 agent_id 部分唯一，内部成员允许多组。
- `np_temp_group_use`：code、event_key、kind、at；主键 code/event_key。

同事务写 `np_group` 镜像：short_name=AGn-temp，leader=内部对端，additions_closed/leader_only=true；双成员写 np_group_member。保留既有 AgentsCanCommunicate 判定。普通 Console 组列表按 ParseAGCode 排除 AG，普通组写入口拒绝 AG；只能经 D 解散，禁止绕过栅栏改镜像。解散删除镜像，权威成员保留历史。extwire.RequiredTables 收入三表；迁移/claim/cmd 棘轮同步追加0106。

## Console 与装配

三条路由均由 RequireConsolePrincipal 包裹，沿用新平台现核身份、必须改密与写 Origin 防护：

- GET /api/console/temp-groups → `{temp_groups:[...]}`。沿用既有 agent-groups 不分页口径，增加「内部对端属当前登录者且未吊销」过滤。创建者身份与管理员身份均不为个人侧栏增加豁免。
- GET /api/console/temp-groups/{code} → 属性对象，含 code/generation、external_full_name、created_by_display（创建时快照）、owner_lc、三时间、解散原因及恰两 members。成员含 agent_id/full_name/role/online；相关两侧成员主人或 active platform_admin 可查详情（个人列表仍仅按内部对端过滤）。
- POST /api/console/temp-groups/{code}/dissolve：`{expected_expires_at:<RFC3339>,reason:"owner"}`。前端二次确认后提交，服务端组行锁内重核权限与到期快照；无关人404、快照变化/已解散409、格式错误400。客户端 reason 不决定审计原因，整点后记录 expired。

D 不自行创建 SystemCtx；装配从 A 的既有 AccessCtx 注入，所有领域查询复用该上下文。

main 将真实 `*tempgroup.Service` 注入 wireR53，OAuth 路启用成功后接入 Hub、启动受 lifecycle 管理的 worker；缺密钥保持原关闭语义。worker 启动立即补扫，之后每分钟分批200项，瞬态失败记录通用错误并下轮重试，不记录凭据或正文。

## 解散通知与在途结算

TempGroupNotifier 接口：`TempGroupDissolved(ctx, code, reason)`；Hub 实现为发布完整目录快照，外部 HTTP 侧每次 Wait/Send/ACK 均现核终态并拒绝。不会把系统事件当新用户消息或新 ACK。`notified_at IS NULL` 是持久待通知标记。

通知前调用 A 公开 `SettleDissolvedGroup`，只由 A 更新自己的 queued/leased inbox 与 queued outbox 为 dead/group_unavailable；received 和已投影 helper 项不回退。D 不直接写 A 表。结算失败不标通知成功，重启继续重试；回调不在 D 组锁事务内调用，以免 A presence 反查组造成锁等待。通知后落标记前崩溃允许重复完整快照，接收者按终态幂等收敛；不声称跨PG/网络的 exactly-once。

## 交付与运行边界

0106 是追加迁移，已施加后禁止老二进制直接写外部席位表；回退先关 DW_R53_EXTSEAT 并停 worker/外部入口，保留权威行、墓碑、已烧序号与审计。没有删除历史/重置序号脚本。制品只经 build.sh；本次仅仓内实现与本地隔离测试，不部署、不执行生产迁移。真实飞书管理员配置与第三方 MCP 客户端现场互操作仍须后续集成验证。
