# R32 spec／任务流：一页设计（交审，尚未实现）

作者 coder56solzzc；2026-10-19。基线 `integration/spec5ff-r31-1`＝`ef439b82b1550e9ca5c179edf61be78666c5fff0`，tree `32236e6a6a647f51b5a672bb293d64ed1884ff47`。需求依据 `review/opus50tc2k/rulings`＝`262143af04aa55148e5e5dfdf6b3ea5ac62733c4` 中 `docs/specs/SPEC-R31-hosted-entrance-and-spec-tasks.md` R8／R9、A23–A29，以及 `docs/specs/SPEC-R32-spec-task-flow-BREAKDOWN.md`。顺序 **W9.1→W9.2／W9.3→W9.4／W9.5→W8.1→W8.2／W8.3**；设计须经 togpt60 PASS 才实现；不部署、不操作宿主。

## 契约与验收／撤修计划

| 包 | 拟冻结的接口与行为 | 正向、拒绝与指定撤修必红 |
|---|---|---|
| W9.1／A26 | 保留 presplit 请求／回包通道，结果任务新增可选成对 `developer_agent`、`reviewer_agent`（准确全名），批次带 `group_code`。只存候选 JSON；现有 `CompleteConsoleSpecPresplit` 不再立即 INSERT 正式任务。所有者经 `POST /api/console/specs/{specID}/presplit/{requestID}/confirm` 确认或修改整个候选批次，提交冻结 revision、候选 version 与 request_id；事务内重验权限、图无环、依赖与 Agent 对，才原子创建任务、指派、审计、通知 outbox。无建议可留空对，之后由所有者确认指派；单边／同一 Agent 拒。 | 回包未确认零正式任务；确认一次建全批、重试不重建；revision／版本漂移409。同组正例、跨组／失效 Agent／伪造全名拒；离线可指派并显示提示。撤确认门、同组门、不同 Agent 门分别红。 |
| W9.2／A27 | Agent HTTP：`GET /api/agent/console/tasks`（cursor、limit 1–100、可选 spec_id/status），`PATCH .../tasks/{taskID}`；MCP `dw_task_list`、`dw_task_update`。列表仅当前 Agent 为开发或审查的任务，带角色、spec 标题／冻结 revision、DoD、依赖、状态、blocker、用例进度与 version；不返回他人来源 IP 或凭据。状态操作仅当前开发 Agent：planned→in_progress；in_progress↔blocked；in_progress／blocked→done。开始／恢复／完成时依赖须全部 done；blocked 必填原因，完成清原因且须全用例已确认；done 不由 Agent 重开。 | 自己可读写、他人403、缺凭据401、非法迁移／版本冲突／依赖未完成409；失败无状态／审计半写。撤当前执行人、依赖、迁移门分别红；并发改派后旧执行人写入必拒。 |
| W9.3／A28 | `POST .../tasks/{taskID}/cases/mark`、`.../confirm`；MCP 仍用 `dw_task_update(operation=mark_case／confirm_case)`，状态操作为 start／block／resume／complete。接口化 `MarkConsoleSpecTaskCase`／`ConfirmConsoleSpecTaskCase`，以冻结 acceptance_id＋case_name 标识，mark 表示开发登记该格已通过，confirm 表示审查复核通过；本批不新增任意测试用例或失败结果枚举。只认预期用例，开发不能自确认。改派、mark、confirm、进度与完成判定共用任务写事务及 version；再次 mark 清旧确认，改派清既有登记／确认，done 改派或改用例先由人显式重开。 | mark 后0计数、reviewer confirm 后计1；错误角色／不存在格／未登记即确认拒；mark 重做后计数归零；全确认才 done。撤确认门、自确认门、改派失效门分别红；确认与改派并发只允许一种完整顺序。 |
| W9.4／A27 | 任务指派与 outbox 同一 SQLite 事务；事件 `kind=task_assigned`，载 task_id、assignment_version、role、assigned／unassigned、工具提示。唯一键 `(task_id,assignment_version,event,recipient_agent_id,role)`，相同指派不加版本、不重通知；改派通知旧／新开发与审查各自身份变化。稳定消息 ID 经既有可靠 delivery 排队、重试、去重；发送失败保留 outbox，重启继续，不将 helper 接收当任务完成。 | 离线重连补送、发送前／后崩溃重放只一条逻辑通知；改派双方收到；失效收件人记录终止原因供所有者查看，不静默丢。撤事务 outbox、去重、离线持久化分别红。 |
| W9.5／A29 | 复用看板带身份隔离、编辑保护的单次轮询；现为60秒，活动页改5秒，隐藏页停止、恢复立即取数；本页写成功即刷新，401／403停止。状态、阻塞原因、已确认／总用例从同一任务快照投影，不用客户端乐观累加。 | Agent 更新后下一轮显示一致；改派与重登记不显示旧确认；慢响应不得覆盖新身份／新页面，编辑不丢。撤刷新、确认计数与响应代次门分别红。 |
| W8.1／A23–A25 | 人通过 `POST /api/console/specs/{specID}/agent-review` 选在线 Agent。请求 `console_spec_review_request` 带 request_id、revision、内容摘要、正文与验收标准；结果 `console_spec_review_result` 只接受该请求绑定的实际认证 Agent／端点，忽略自报身份。绑定当前编辑人、revision、deadline（180秒）；离线明确拒绝，超时／重启明确失败。issues 原子写 gate review，全部 outcome=pending，review_agent_id 为实际回信 Agent；人经现有 gate-review 标真问题／误报并回应后才能 finalize。 | 回包可见、伪造／迟到／重复／旧 revision 不污染新审查；Agent 无人类 cookie，不得调用 finalize 或标人类结论。撤实际回信人、deadline、pending／人工 finalize 门分别红。 |
| W8.2／W8.3／A23–A25 | 同通道 `console_spec_criteria_request／result`，人发起 `POST .../agent-criteria`；候选 criteria／cases 暂存，`POST .../agent-criteria/{requestID}/confirm` 由发起时的当前人类编辑人确认，校 expected_revision 与 owner_ref 逐字存在正文后原子建新 revision。请求待回最长180秒，候选待确认最长15分钟；期间编辑、transfer、finalize 均409，显式取消／过期释放，取消后回信拒。其他时间沿用 transfer 轮流编辑，不新增多人同时写。 | 未确认不覆盖原修订；取消、超时和错误回包不留永久编辑锁；并发修改／转交／确认只一方成功。撤人工确认、owner_ref、编辑锁分别红。 |

## 权限、事务、兼容边界

- **身份与同组**：人类写路由沿用 Console cookie、同源校验，拒 Agent bearer 混用；Agent 新路由只接受专用 Agent bearer，复核 Agent 与所有者 active，不接受 cookie、共享 key、5分钟人签 capability 或 body 自报身份。MCP 经新增 `task_flow_v1` 本地 IPC 到 helper，再请求已配置 Hub；凭据不进 shim／日志，禁止任意目标 URL。写需 request_id＋expected_version；同 ID 同载荷返回既有结果，不同载荷409；超时回 outcome_unknown，先查询、不盲重试。
- **组与归属口径**：以新平台 identity 的组码／active 成员为准，非“同属一个用户”。所有者只能选自己创建或拥有 active 成员的可见组；开发／审查均须为该组 active 成员，可跨用户，关闭添加不撤成员资格。确认／改派时复核并存解析后的稳定 Agent ID 和组码；之后任务授权是当前显式指派，不因离线撤销，也不因退组自动转派；停用 Agent／所有者始终拒。组删除／退组后的新确认和改派拒。跨用户 SQLite Agent 投影按真实 identity ID 同步，不能靠全名猜旧记录。
- **并发边界**：任务、用例、指派版本、请求幂等、审计和 outbox 在同一 SQLite 写事务；审计失败整体回滚。人类旧看板／任务修改入口也走共用状态／依赖／确认门，不能旁路完成。PG identity 鉴权独立于 SQLite：在 identity 存储层增加行锁授权事务，锁认证 Agent／用户以及确认时的组／成员，持有至 SQLite 提交，相关停用／退组／删组路径遵循同一锁顺序；身份提交失败拒绝响应并按 request_id 查任务结果，不声称跨库原子提交。测试覆盖授权变更先完成即拒、任务先线性化可提交两种次序。
- **可靠信封与旧 helper**：`task_assigned` 放在 meta.kind，外层沿既有可靠 delivery；不设会绕过可靠队列的 system=true，不用未知 meta.type 直接替代 delivery 外壳。Hub 仅内部 outbox 可生成该 kind，普通 Agent 不能伪造系统指派。新 helper 能识别并展示通知；旧 helper 仍收到普通可读正文和升级提示，no_reply／conversation_terminal 禁自动回复，不当控制命令、不得自动修改任务状态。用精确 r31.1 helper 做兼容接收／ACK／不循环回信测试；未知 kind 不得造成丢信或重放执行。
- **版本与 driver inbox**：拟统一发布 `VERSION`、helper `__version__`、MCP serverInfo 为 **0.2.3.5**，构建前再核版本占用；旧 Hub 对新工具明确不支持，旧 helper 无新工具时提示升级，不静默降权。此批一并修 driver `dw_inbox` 的 desktop_inbox_unavailable：status／list／read 查询本席 DeliveryInbox，ack 只记独立“已读”标记；不改 accepted_to_cli、重试、取消或 Hub processed ACK。driver 的 pause／resume／wait／reply 返回明确 mode_operation_unsupported，不伪装桌面暂停／已回复；desktop 现行为保持。分页、跨席隔离、重启已读保留与读取不吞自动投递均测。

## 迁移、回退与交件门

spec／任务目前属于 `internal/store` **SQLite schema_migrations**（文件名＋digest），不是 hosted／identity 的 PG 账本。拟新增 `0055_console_task_flow_r32.sql`（presplit 候选、任务版本／指派代次、请求幂等、审计／通知 outbox）及 `0056_console_spec_agent_review_r32.sql`（review／criteria 请求与候选）；优先新增旁表，必须加旧表列时给默认值，不修改0054及更早迁移。helper 本地 inbox 另走现有 `_migrate_schema`，新增已读旁表；不改 delivery 状态 CHECK。实现前核编号冲突。空库、精确 r31.1 旧库带任务升级、重复迁移／重启、失败回滚均须实测；旧二进制的实际启动和任务／spec 读取单独验证，不能仅凭 additive 宣称通过。

旧二进制可读不等于保留确认门／指派授权／预审锁：降级前必须关闭 R32 的人／Agent 写入口和任务驱动，取消或完成 pending review／criteria／presplit 请求，冻结 outbox；恢复旧版只能受控只读，需可写则交保留这些门的兼容回退版。不删新表或回写账本；通知须保留去重标识，不以降级重新发送。r31.1 部署线不由本设计改变。

实现交件：按上表 A23–A29 路由／真实 PG identity＋SQLite 事务／真实可靠投递／MCP→IPC→HTTP／浏览器链路证据；每个撤修先正格绿、只撤对应门命中指定断言红、恢复再绿，禁止其他断言抢先假红。交精确 commit／tree、环境、原始 stdout／stderr／rc 与限域。全门为 go vet、默认并行 `go test ./... -count=1 -timeout=30m`（完整包账本）、canonical、helper Python／MCP 全套、hosted／console-browser／enroll／groups-vega 及新增 spec-task 浏览器；本机与 dev-env ubuntu／umask0002 各验证任务通道，保留 r30／r31.1 兼容格。**本次仅文档设计，无实现或新测试通过声明。**
