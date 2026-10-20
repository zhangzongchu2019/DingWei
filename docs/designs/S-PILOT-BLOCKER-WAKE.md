# D2 · S-PILOT-BLOCKER-WAKE（一页设计，待审）

基 r32.1 `36afa6215f00bc39a992441ab0ff8fe81442aa3e`；前序 D1 `review/coder/pilot-spec-template＝1fac27d2223c4ed5296b50e05711bb8702eed8f3`，本分支不合其代码/文档。目标是有归属、有新证据才重核恢复的阻塞闭环；仅设计，不跑 dataConnector、改任务状态或启用自动执行。输入精确锚见 SOURCE-READS.json。

## 1 对象、登记与当前能力差距

R9 现有 `TaskFlowUpdate.blocker` 是字符串，执行状态只有 planned/in_progress/blocked/done，没有结构化所等人/条件、答复事件到恢复的关联。新增 `task_blocker`（不是 board_task）：id、task_id/spec_id/revision/SHA、task_version/assignment_version、generation、kind（需求不清/变更/技术受阻/缺资源）、where（具体卡点）、waiting_principal（稳定用户/Agent ID，不凭昵称）、expected_input（类型/接受谓词/证据锚）、resume_recheck[]、owner_of_blocker、created_at、expected_by、version、state、last_input_version、last_notice_version。缺归属进入统一待决队列的 blocker-needs-owner 类型；不自动都归 PM，也不创建独立页面/第二台账。

当前开发 Agent 可凭 R9 权限创建/更新自己的 blocker；spec owner 可分派所等人、裁定取消/改派但不能冒充开发者恢复执行；所等人仅可在该 blocker 上提交有身份的答复/材料，不因此获整任务/spec 阅读权。无平台身份的外部联系人先以 owner 确认的证据引用入账，不接受消息声称的名字为身份。登记要求同 D1 有效 spec 修订、gate/anchor/owner_session/ETA；缺授权/接口/技术事实要分别写，不用“等回复”四字过闸。同一 task 可多 blocker，任一未解决都不能恢复。

API 候选：Agent `POST /api/agent/console/tasks/{id}/blockers`、`POST .../blockers/{bid}/inputs`、`POST .../{bid}/recheck`（开始/失败/解决）、`GET .../blockers`；Console owner 用对应会话鉴权的管理路由分派/取消。Agent 侧保留专属凭据、单一 bearer、拒 cookie/混凭据/伪造 agent_id；MCP 拟 `dw_task_blocker_set`、`dw_task_blocker_input`、`dw_task_blocker_recheck`，只接对象/版本/证据，不接受 URL/命令/身份凭据；实现时统一版本并测旧端。

## 2 状态、事件与恢复重核

| 阻塞状态/事件 | 动作与条件 | R9 执行状态 |
|---|---|---|
| open → waiting | 登记 where/等谁/等什么/重核条件，形成规范化 fingerprint | 当前 in_progress 可原子改 blocked；planned 的开工阻塞不伪造“已认领”，保持 planned 并加不可开工门 |
| waiting + 合法新 input/event → ready_to_recheck | 答复到件、依赖证据变化或条件 adapter 的可验证状态变化；原始输入仅存引用/限量内容 | 仍 blocked/planned，不自动 resume/complete |
| ready_to_recheck → rechecking | 当前执行者认领一次重核 lease，版本 CAS；新输入在重核期间到达留待下一轮，不被旧版本吞掉 | 不变 |
| rechecking → resolved | 依次重核身份/组与当前指派、spec 修订/范围、依赖和 gate、材料真实性/业务条件、当前环境/隔离对象、有效授权/预算/停止处置；全部通过，记录每项证据与核对时间 | 最后一个 blocker 解决后，**当前执行者**可在同事务、再核权后发 R9 resume/start；无执行授权则不得恢复 |
| 重核失败/未知 | 记录结构化原因，回 waiting；输入未变不再唤醒，不把“尚无新证据”当新的进展反复汇报 | 保持 blocked/planned |
| 改派/移组/修订/撤权/取消 | 旧 generation/lease 失效，旧信号不得送前任；已改派的有效 blocker 由 owner 转交当前指派，需重新核输入；done 重开仍仅 owner 另令 | 沿 R9 现有失权/改派机制，不私自重开 done |

“有人回答了”只满足到件条件，不等于业务条件成立；真假/完整性需指定重核角色判断。自动条件仅支持已注册且有测试的有限 adapter（指定问题有效答复、指定依赖 gate 的明确版本、授权/资源记录有效期）；不执行任意表达式、链接或脚本。人工条件必须由授权的人确认，无法机器证明就维持待重核/blocked。

STOCK B06 未知提交例：新答复可唤醒**核对原任务**，绝不自动重派业务写、换 key、清 pending、close 或按库存同值认定原事件完成。已配置业务写上限和“唤醒成功”都不是新的动作授权。

## 3 无新输入不重复通知、离线与重启

归一化阻塞信息相同且 input_version 未变：更新请求返回原结果，不新增通知、不生成聊天阻塞回报。Hub 接受输入先按 `(source,event_id)` 去重；同 event_id 内容改变 409，不能顶替旧答复。每个实际新输入版本对应一个重核唤醒键 `(blocker_id,generation,input_version,assignment_version,recipient)`；重复 HTTP、worker 重试、外部 webhook 重放均复用同一逻辑事件。失败重试是同信封/同 delivery_id，不是再次宣布新输入。多个新事件可合并到最高版本，但保留全部 input 账本，重核只解决其读到的版本，之后到件不能被确认丢弃。

blocker 状态、输入去重、R9 状态/审计及 outbox 在同一个 SQLite 事务提交；worker 提交后才投递。发送前按 PG/R9 重核接收者仍当前且有组权限，已撤权的 payload 取消并由 owner 待决；新收件人重建新 assignment_version 事件。分页扫描待发送 outbox，不被前页离线席位堵死；重启 reconciliation 对“已登记未排通知/已投递未确认/lease 超时”按唯一键补登，不丢也不复制业务动作。PG 授权用既有组锁→Agent→owner 锁序，SQLite 提交前 PG 存活重核；跨库不做两阶段提交。提交结果未知返回 unknown＋稳定 request_id，先查回执，不盲重放。

新版 helper 宣告 `blocker_wake_v1`：持久化可靠信封 `task_blocker_ready`，正文只是安全显示摘要，事件元数据仅 task/blocker/generation/version 引用，不携带秘密或可执行原文。复用 driver 的持久队列/空闲边界，**一次入队 recheck 轮次**：driver 忙则排队，投递/入队回执只表示耐久入队而非重核/恢复成功；普通消息 processed 语义不改，重核结果另有事件；helper 持久去重，崩溃后按同 signal_id 查 journal，不重复开启已认领的重核 lease。固定内置重核入口从 Hub 再取已授权上下文，不把对方答复当用户指令注入。重核没有执行授权时只记缺项，不能自行做业务/配置/发布动作。

旧 helper 无此 capability：只做安全通知/升级提示或保留待投递，**不注入未知控制类型、不自动回信、不伪造已唤醒**。在线/离线重连不算新输入；不为“等不到人”每轮主动发阻塞消息。owner 的明示催办或 D3 的一次超时提醒是独立政策事件，有单独去重键，不改变此原则。无原生 driver 的模式只通知并显示需人工重核，不冒称自动执行已跑。

## 4 与六阶段和 R9 完成门的准确衔接

| 业务阶段 | 现有 R9/证据映射；不能跨越的门 |
|---|---|
| 请求 | 已确认候选落 planned＋指派；不是任务请求消息一来就认领 |
| 认领 | 当前开发 Agent start → in_progress，须依赖/准入/未解决 blocker 全过 |
| 完成申报 | 执行人登记交付锚/子例结果；只是提交，不直接业务签收 |
| 复核 | 当前 reviewer 用 confirm_case；R9 complete 调用 taskCaseProgress(...,false)，要求预期例非零且逐例已由当前开发登记、当前复核确认，随后才能 done；不能说 done 仅等于开发自报 |
| 验证 | 另记被授权环境中的验证 gate 及原始证据（工程/合成不替代正式平台） |
| 签收 | 由 D1 指定且接受责任的人类签收，绑定 task/spec 修订及证据；不由 Agent、ACK、done 或 PM 收到飞书自动产生 |

后两阶段及完成申报的证据记录是**拟补的 typed gate 记录**，不伪称当前 R9 已有六个状态；复用 task 同一 ID 增记录，不另造调度引擎。所有 start/resume/complete 入口（含旧 HTTP/MCP）在共享 store 层检查未解决 blocker，防止旧调用从 blocked 直接 complete 绕门。新试点启用前把既有 blocked 文本映射为 legacy-unsatisfied blocker；新字段缺失不默认为已解决。迁移适用范围显式记录到试点配置，不能悄悄修改其他项目任务。

## 5 存储、接口兼容与验收计划

后续 SQLite `schema_migrations` 候选 0058（D1 拟 0057，集成前统一排号）：增 blocker/input/lease/outbox 与 typed gate 记录；唯一键约束 request_id/输入事件/唤醒信号，使用现有可靠投递适配，不新增 board.task 引用、不恢复 datad。PG 仅增 repo 查询/锁方法、不另写身份库。旧二进制可读旧表但不知道 blocker 门，故启用后不得直接回滚到旧写路径；先冻结试点写/唤醒 worker，再用兼容版或待 owner 确认的导出方案，不删 blocker 账本。

验收/撤修：两 Agent/跨 owner 同组与越组鉴权；缺项、多个 blocker、文本旧端绕门；合法答复与语义不成立区别；相同 event_id 去重/变更拒；改派/移组/撤权/修订与答复/发送/重核的真 PG＋SQLite 双顺序；提交前后断连、重启补登、lease 过期、忙/离线、分页前页堵塞、ACK 不等于运行；无新输入零重复信封、最高版本合并不吞新输入；旧 helper 不执行通知、driver 单次队列；B06 原任务写 0、done 及签收不可越门。撤掉权限/版本/唯一键/恢复重核/共享完成门/控制类型围栏各须指定红；全仓 Go/vet/canonical、定向 race、真 PG、隔离 HOME helper 与受影响浏览器另跑，首败保留。本件只审设计，不运行以上门。
