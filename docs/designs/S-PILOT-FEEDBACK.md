# D3 · S-PILOT-FEEDBACK（一页设计，待审）

基 r32.1 `36afa6215f00bc39a992441ab0ff8fe81442aa3e`；依赖 D1 `1fac27d2223c4ed5296b50e05711bb8702eed8f3`、D2 `b059611038447bed09869c899e708398ac5b6108` 的固定设计，独立分支不合实现。输入 SpecDoc v3.77、盘点及 dataConnector `2fa84f17` 两草稿，精确锚见 SOURCE-READS.json。本件仅设计，不向陈玉玲/飞书发消息、不调整编组、不建真实问题或任务、不上线。

## 1 问题对象与职责

新增 `spec_task_question`，与 R8 gate_review.issues（Agent 预审候选）明确区分，不覆盖审查 verdict。字段：question_id、spec_id/revision/content_sha、可选 task_id（有任务时强绑定）、blocker_id/generation（D2）、type（**需求不清/变更/技术受阻/缺资源**）、title、observed_fact、question、expected_answer、input_anchors、requester、responsible_principal、pm_contact_binding、priority、version、state、created_at、first_substantive_answer_at、answered_at、resolved_at、due_at、reminded_at；上下文只存经授权的摘要与 Git/文档锚，凭据/真实业务私密数据不入正文或卡片。

当前任务开发/复核 Agent 仅可在自己仍获授权的任务挂问题；当前 spec 编辑人可挂 spec 级问题。权限沿身份/组/当前指派重核，不凭提交的 owner/group/名字。spec 级问题没有任务时入同一 typed 待决队列，稍后由 owner 确认关联，不能自动创建或认领任务。请求者给出事实/冲突/选项/影响/建议，技术事实未知须显式写证据缺口，不能伪装为 PM 单选可裁定的事项。

四类问题均进入 PM 的集中待答视图/飞书通知；需求/变更由 PM/owner 对业务语义答复，技术受阻/缺资源须分派到实际技术/资源责任人，PM 可转派/协调但不被自动视为技术证明者。没有指定人进入 needs-owner，不默认为 chenyuling 或全组。陈玉玲（chenyuling.cyl）是已知产品联系人，不等于她已授飞书身份绑定、全 spec 阅读或业务最终签收权。

## 2 接口、身份与飞书通道（拟实现）

Console 人类接口：`POST /api/console/specs/{id}/questions`、`GET .../questions`、`POST /api/console/questions/{qid}/answers`、`PATCH .../{qid}`（明确分派/关闭/重开操作及 expected_version）。Agent 对应只在 `/api/agent/console/...` 使用专属 bearer，拒 cookie/混凭据/伪造身份；MCP 拟 `dw_task_question_create/list`，人类答复/业务裁定/签收不下放 Agent。技术材料可由获该问题输入权限的 Agent 提交为 input，**不是人类 PM 答复或裁定**。

问题对象可给 PM 当前用户绑定的**单问题最小授权**（只读该摘要、答该问题），由 owner 明确授权并可撤销，不自动授全组、整 spec、源码仓或任务权限，也不修改 dataconn 冻结成员。个人/群收件目标由现有平台 identity→飞书稳定用户/会话绑定解析；绑定不存在就待配置并报 owner，不按展示名猜收件人、不发广域群。复用现有飞书适配基础；**当前 CLI 镜像不等于已有卡片/回调能力**，卡片发送、集中视图及安全回复适配属于待实现，不据历史文档宣称现网可用。

卡片只含问题 ID、类型、当前安全摘要、期待答复与受控平台链接；多条待答合并成个人列表分页，ID/版本保持独立。不在 URL 塞 Agent bearer、共享 spec capability 或凭据；链接仍要求平台人类登录和问题授权。可从飞书卡片回调答复时，验证 provider 签名/加密、应用/tenant、时间窗/nonce/event_id，再把平台绑定的**稳定人类身份**作为 actor 重核问题授权；未知绑定、转发卡片、改 qid/revision、过期回调或重复事件改变内容均拒绝，不凭飞书显示名或“来自 PM”文本认人。未具安全回调能力时只发安全链接，人在 Console 集中答复，不能抓群聊天猜答案。

## 3 流转、自动回写与 D2 唤醒

`open/needs_owner → awaiting_answer → answered_pending_recheck → resolved`；另有 cancelled/superseded，关闭/重开须有权限与理由。答复是追加版本记录，不覆盖原问题、输入锚/历史意见；ack/“收到”单列 acknowledgment，不计实质首答、不解除阻塞。人工关闭仍需明确结论/理由，不能等同任务完成。

合法答复提交时，同一 SQLite 事务：校验 question/spec/task/blocker 当前版本与身份 → 写 immutable answer/event 去重 → **追加任务活动记录**（未关联任务则仅 spec 活动）→ 写 D2 blocker input → 状态 ready_to_recheck/answered_pending_recheck → D2 唯一唤醒 outbox。自动“回写”是这条可追踪的活动/输入，不覆盖任务正文、验收、spec final 内容或 R8 gate review。worker 重试/重启用 D2 `(blocker,generation,input_version,assignment_version,recipient)` 唯一键，不另写一套唤醒调度；旧 helper 安全提示或待升级，不把 PM 原话当用户命令执行。

答复到件不等于条件成立：当前执行者被一次唤醒后仍走 D2 全部重核，材料不足回 waiting 且不因同一答案反复报阻塞。一个 blocker 可关联多个问题，全部必需问题/其他条件满足才可解决；答一个问题不能自动清别的 blocker。恢复 start/resume/complete 与签收仍走原授权/门，不做业务写或发布。B06 的答复只能触发原任务只读核对，不自动重试未知提交。

问题类型为“变更”或答复改变范围/验收/环境约束时，生成**待确认变更提案**与影响任务列表；已认领 spec 退待完善/冻结受影响执行，按 D1 新修订重审与 owner 再确认，旧 evidence/门留旧修订。迟到答复若绑定旧 revision/assignment/generation，只追加其获授权历史问题记录或按已撤权拒绝；**不唤醒新任务/前任，不改新修订**。转交与回调并发以版本 CAS/事务先后判，冲突 409 不部分回写。

## 4 一小时响应、提醒与可靠性

Hub 以事务内服务端 UTC `created_at` 起算到 `first_substantive_answer_at`（首份经认证、字段完整的 substantive answer；这仍不保证业务内容足够），另记送达耗时、完整答复耗时、重核/解决耗时。外部自报时间不倒写 SLA，幂等重放不重置开始时间；ack、分派、编辑标题、重新投递不算首答。未解决/信息不足与超时是不同状态。时间异常单列告警，不用负数或默认 0 掩盖。

默认 **60 分钟实时时钟**，PM 定期看/离线不暂停计时；如 owner 另定工作时段策略，须新政策版本、明确生效范围，不追溯改旧计时。到 due_at 仍无实质答复，向该 spec/项目配置的组长发一次提醒（缺组长配置报待决），唯一键 `(question_id,generation,policy_version,remind_60m)`；提醒摘要列待谁/等什么/已等待多久，不自动升级为业务授权或刷屏催 PM。owner 明示重开须新 generation＋理由，才能重启计时，不允许普通修改偷偷延长 SLA。

答复与超时 worker 的两种提交顺序必须可测：发送提醒前再次核 state/version/首答时间；先有答复则取消未发提醒，先投递提醒后到答复则保留提醒历史、记录后答，不能伪造从未超时。worker 晚启动需补扫过期项，单问题仍仅一个逻辑提醒。无新输入不新增 D2 唤醒，SLA 提醒作为独立政策事件且不恢复任务。

question/answer/task 活动/D2 input/outbox 与审计同事务、PG 身份锁与当前授权重核按 D2；失败整事务回滚，未知提交以稳定 request_id 查回执。飞书 outbox 单独记录 queued/sent/confirmed/unknown/failed，不把“发送 API 返回”当人已读/已答。重试使用原 event/message 幂等键与指数退避，离线保留；适配器若不能提供确定幂等/查回执，丢响应记 unknown 待核，不宣称物理卡片 exactly-once、不换新 ID 无限制重发。外部重复卡片也不能造成多个问题/答案/唤醒/提醒。回调失败不自动发答复，避免消息振荡。

## 5 存储、权限与验收

后续 SQLite `schema_migrations` 候选 0059（D1=0057、D2=0058，集成前核冲突）：questions、answers/events、question-only 人类授权关联、task/spec 活动及飞书 outbox/SLA 唯一键；复用 D2 blocker/input/内部 wake outbox，不再建第二套。PG 继续当前身份/组绑定；若缺人类飞书绑定基础需独立 identity 迁移设计，不挤进 SQLite token 字段，不存明文凭据。问题最小授权每次读答重核，可撤回；提交后发送前再过滤当前授权，组/owner 停用不继续泄露摘要。限长、分页、Cache-Control:no-store、限速；正文/环境值/回调签名不入日志，非源码材料按现有保密边界存。

旧二进制可读原 spec/task 表却不认识问题/唤醒/新完成门，故启用后不能直接回滚写业务；先停试点新增写/外部发送/wake worker，再用兼容回滚版或 owner 明示处置，不删历史问题/超时/失败证据。新 helper/MCP/Hub capability 与版本按实现集成统一，不以 r32.1 名义声称这些新工具已存在。

实施后验收/撤修：四类型与 needs-owner；PM 有限对象权限、越组/伪造/撤权/转发卡片；签名/租户/时窗/重放与同 ID 变内容；Console/飞书并发双答/转交/修订；答复事务中任一步失败零部分回写；D2 唯一醒、旧 helper 不注入、不影响 ACK；多问题只部分答仍 blocked、业务变更不能覆盖 final；59:59 不提醒/60:00 仅一次、ack 不止表、重启逾期补扫、答复/提醒双顺序、无外部绑定/离线/unknown 回执。撤掉任一授权/事务/去重/时限/只追加/不自动授权门须指定红。全仓 Go/vet/canonical、race、真 PG、隔离 HOME helper、受影响 Console/飞书适配器离线假服务兼容门另跑；真实飞书冒烟与通知外部人员须另令。本件无任何上述执行。
