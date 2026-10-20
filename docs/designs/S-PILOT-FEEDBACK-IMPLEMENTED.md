# D3 轻量研发问题反馈实现说明

基线为 D2 可审候选 2e6b9506（其父系包含 project_context 可审候选及 r32.1）；本件不表示前两件已通过独立审查，不部署、不升级席位、不向真实飞书发送。

PM 三步：从飞书个人通知的安全链接登录平台；集中查看获准问题并答复，技术/资源问题可转给已有问题读权的任务开发/审查 Agent 或 spec 所有者/当前编辑人；看答复追加记录及 D2 重核状态。业务决定、任务重核、业务签收分别记录。没有可靠回调时不解析飞书任意聊天，直接回复通知不会写库。

接口：Console 页面 `/console/questions`；集合/详情 `/api/console/questions`；spec 下 `questions`、`question-context`、`question-policy`；Agent 集合/详情 `/api/agent/console/questions`、任务下 `questions` 与问题下 `inputs`。MCP 新增 `dw_task_question_create/list/input`，仅当前任务提问、获准问题只读和技术材料输入。人类 PM 答复、所有者联系人配置/变更裁定不提供 Agent 工具。helper/Hub 保持同一试点批次 0.2.3.7，锁与镜像依赖不变。

授权：每次调用在 PG 下锁定组、Agent、当前 active 用户与飞书身份绑定；SQLite 再核 spec/任务修订及指派、对象角色与 CAS，提交前重核 PG。问题指定 PM/责任人仅获得问题必要字段，不能读取全任务/spec/source。所有者可显式指定 active 平台用户/Agent；PM 仅能转给原本已获准角色，不能更换 PM/组长或扩大外部授权。转派问题与关联 D2 等待人同事务更新，旧等待人不保留 D2 写权。移组/吊销/停用立即在下次调用拒绝。所有者可查看失效范围历史、限域取消，不用旧答复唤醒新范围。

通知：复用现有飞书网关，稳定绑定 issuer 与 personal open_id，不按姓名/群名猜人；原生 SDK 传 UUIDv5 请求去重键。消息仅含类型与登录链接，不含事实、原文、证据附件、密钥或个人联系 ID。缺绑定/发送器/HTTPS 公共入口时为 pending_config，不能宣称送达。provider accepted 只表示接口接受，非本人已读。请求前持久化 dispatching；崩溃或无可靠回执为 unknown，不自动重新发一次。

时间：服务器 UTC 登记起 60 分钟，无实质答复提醒配置组长一次；确认收到不算首答。PM 的实质答复，或明确转派给技术/资源责任 Agent 的非确认技术材料，记录首答。更换责任人/组长不重置时钟，已发送/unknown 提醒不再产生第二条。所有者有理由重开终结问题才产生新 generation。相同问题的 PG advisory 锁覆盖最后核权、发送及本地回执，形成答复先提交则取消提醒、发送先进行则答复等待的实际顺序；网络调用不持有 SQLite 写事务。发送有 8 秒上下文上限，持久 outbox 及 keyset 轮询避免未配置首屏饿死后续对象。

回写：0059_pilot_questions.sql 走 SQLite schema_migrations，同事务建问题、答复/别名去重、变更提案、审计、关联任务输入与 D2 唤醒 outbox。未关联任务的仅为 spec 问题，已完成任务反馈只追加、不重开/唤醒。相同答复换 event_id 仍只保存一份逻辑答复、唤醒一次；同 event_id 改内容拒绝。未知结果客户端保留原 request_id，不自动换新笔。

业务变更：不修改旧 spec；先追加 pending 提案，所有旧 spec/修订任务的启动、恢复、完成及 D2 重核受阻。只有所有者带理由拒绝，或沿原 R8 人类确认流程生成真实新修订后链接确认；确认新修订后旧任务继续受阻，不偷换范围。所有者可以在旧范围失效后对旧提案作显式裁定，但不会把历史答复写入新任务；取消问题仅停止等待通知，保留未裁定提案及旧范围阻断，不静默驳回业务变更。问题 closed/resolved 不是业务签收；生产/安全事项自动完整档，不能通过改问题类型降档。

UI：5 秒只读轮询，不更新正在编辑表单的 CAS 版本；未知提交复用原标识；切问题按问题隔离草稿；401/403 清除私有内容和草稿。摘要列表、答复及提案历史有分页和响应预算；所有用户内容用 textContent。联系人候选从该 spec 所有者获准的 question-context 返回稳定平台 ID，不把显示名当身份。

兼容：0059 为追加表；真实 r32.1 旧 Store 可启动并读取新库，保留新表/账本，不构成旧写路径兼容证明。回滚必须先冻结试点问题/任务写入和 worker，保留数据库与 outbox，不能让旧二进制绕过新增变更门。旧 helper 不识别新 IPC 工具时只提示升级；D3 不向 helper 增加一条会注入指令的通知信封，任务唤醒完全复用 D2 的受限核验提示。

验收/限域及每项撤修读数见 docs/reviews/coder-pilot-feedback-impl/REPORT.md；任何本地 PASS 均不代表飞书真实投递或生产验收。

## 第85条附注闭合

- 真实processQuestions回归：QuestionSender同时给非空provider ID和error，通知unknown；再次process发送次数仍1。错误绝不被当作成功凭据，也不盲重发；保留provider原语UUID。
- Prepare后的最后PG check失败且本次确定未调用provider：临时失败回queued、撤权cancelled。Prepare本身未提交则临时失败pending_config、撤权cancelled。恢复方法以outbox ID/问题/代次/收件人CAS；已sent/unknown/cancelled不回排队，过去dispatching且本次没有新Prepare证明时只能unknown，不能声称未发送。
- question-context不再列全平台active用户。挂R9任务时按当前任务编组；仅挂spec时按owner创建或其当前active Agent所在编组并集；当前PG成员/用户/Agent锁内重核owner仍相关。仅owner可额外用allow_user明确输入最多20个稳定平台用户ID；已有policy的PM/leader属于owner先前显式选择，保留为允许人选。这只返回最小ID/显示名，不建立问题/任务授权；实际configure/assign仍重新鉴权。
- contacts以稳定user ID升序、limit1–100（默认50）、5分钟加密游标分页；游标绑定actor/spec/task/显式集合/limit和当前授权摘要，变动409，不能沿旧游标读过期组。空白/旧协议游标不能枚举整个平台。当前最多200候选组，超出失败关闭；每页仍扫描相关组成员以生成摘要，不宣称增量索引优化。页面逐页载入，极大名单提示精确ID选择；未配置不猜姓名。
- pm_user/leader_user在HTTP身份查找前及store幂等hash/写入前统一trim并小写，直接store调用同样规范；不改已发布schema/锁/版本，不做历史库改写。
