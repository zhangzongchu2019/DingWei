# 子 Agent 寻址第一期详细设计（待复核）

执行者 coder56solzzc.zzc；CODEX_THREAD_ID=01a1bacf-9317-7b91-bf86-0a0c88c834f6；to=opus50tc2k.zzc。2026-11-08 UTC。

基线 `opus50tc2k/r54-integration` / `6e2c836491f8ba506833d6f849fbe73530e13f92`。依据 `opus50tc2k/sub-lane-routing-proposal` / `b33f9ae6108e1e86778aee5e4040d3847f06dce8` 的提案与 AA-001～AA-006、rulings〔51〕及补1/补2；本次派单 2026-11-08T12:10:21Z 明确区分“不存在”与“已结束”。本提交只含设计、规格、隔离复现与证据，不含产品实现、版本变更或上线授权。

## 1. 范围与结论

第一期实现同一 Codex helper、同一 app-server 管理的多个并列 thread：主会话及一级子会话。每条分道有自己的 FIFO、驱动实例、业务上下文及显示/确认状态；共享一个 app-server 进程与受控写者。不把 Codex 原生 spawn-agent 产生的 thread 自动收编为 DingWei 子 Agent。二、三级寻址第一期能解析但拒收，第四级以上明确超层数。Claude 与原生自行派生子会话留第二期。

需要迁移：Hub 持久子树/墓碑与投递路径、helper 分道注册及账本作用域、View 的分道投影。仅内存字典不能在重启后保持“已结束”、防止旧代次复投与区分同封信发给两个子会话。本单只设计迁移，实施另派；此前 reconcile 的“不加迁移”仅约束前单，不替本单实施授权。

启用关键前置有三项：Hub/helper 双向能力协商；原生事件和发信来源按可信 threadId 绑定；旧 Hub 回滚前封住所有子分道。实测旧 Hub 会把父地址加未知 `sub_path` 的信投给父，因此任何组件都不得未经协商先把子目标改成父地址。

## 2. 全名、分道与身份

增加纯解析器，输出 `root_agent`、`owner`、`sub_path`；子路径按根到叶排列。`r21.r2.agent.user` 得到主地址 `agent.user`、路径 `[r2,r21]`。短主名由发送方可信身份补用户，不从正文推断。先检查段数（最多5段），再检查空段、字符、长度；不允许点号、路径分隔符、空白或 Unicode 混淆字符进入单段。新子段使用现有 Agent 名的字母数字与长度约束，禁止前缀/模糊匹配，不截取“最右两段”作为投递结果。

`identity.ResolveFullName` 继续只解析主身份；新增 route parser 先生成结构化地址，再用现有 identity API 解析主身份、编组与权限。不能全局放宽 identity、登录、认领、extseat、管理权限或任务执行者校验。子 Agent 不增加 Agent 身份、不获得新凭据；同一主 Agent 的子会话仍受同一任务单执行者约束。

兼容例外：现有 external 主名有 `short_AG3.user` 形式（`internal/extshared`），不符合 AA-001 对所有段均为字母数字的概括。保留既有两段 external 主身份流程；第一期不为 external 席开启子 Agent。规格中标明这项历史兼容，不借本设计修改外部席命名。新自托管 Codex 子名遵循 AA-001。

节点使用不透明 `node_id` 与持久 `lane_generation`；别名不是节点主键。默认名取模型会话 UUID 无连字符规范表示的末8位随机段，不能取承载时间戳的前8位。同父冲突时依次取末10、12位直至末32位；已有节点和旧别名不改名，完整UUID仍冲突则拒绝重复绑定，不无限重试。相同分钟创建的孩子须获得不同默认名，随机尾段碰撞也按上述规则处理。显式别名与默认名都登记到同一父下，大小写按现有规范统一，冲突拒绝，结束后仍保留名称。不得将旧别名重新绑定到另一个 thread，避免迟到信投新任务。完整模型 threadId 仅 helper 本地保存，不上报 Hub。

## 3. Hub 协议与拒收

### 3.1 能力和注册

新增协商能力 `sub_agents_v1`、`max_supported_depth=1`、`subtree_revision`；以本次已鉴权连接 `conn_seq` 为准，重连不继承旧连接能力。Hub 宣告支持后，helper 才可发送树快照；Hub ACK 快照持久化后节点才可接信。树项含 node_id、parent_node_id、canonical_name、aliases、state、generation、reason；主节点承载树 revision。增量必须引用前一 revision，不一致请求全量。首次可先只做全量快照，避免额外并发语义。

控制帧采用 `sub_agent_tree_report` / `sub_agent_tree_ack`，不使用业务正文。report 带 connection epoch、base/current revision 与完整节点列表；ACK 返回已持久 revision。相同版本相同摘要幂等ACK，版本倒退或同版本不同摘要拒绝并请求重同步；旧连接报告不能覆盖新连接。ended 对同一 node/generation 为终态，不能由重连快照复活。

离线不是结束，注册表未就绪不是不存在。重启恢复中对受影响目标返回可重试 `sub_agent_registry_unavailable`，不作阴性查无结论，不借机恢复/启动原生 turn。Hub 掉线保留节点及墓碑，重新认证后核对 helper 持久表和代次；缺失冲突留待恢复，不绑定任意新 thread。

### 3.2 信封与可信来源

公开发信仍传完整 `to`，例如 `r2.agent.user`（既有 `#key_id` 传输封装不改变身份语义）。Hub 完成解析、权限、树匹配和能力校验后，才生成内部根端点及保留结构：`target_full_name`、`sub_path`、`target_node_id`、`lane_generation`、`subtree_revision`。保留字段由 Hub 重建，外部传入的同名字段不可信；显式地址和结构不一致拒绝。正文保持原字节和原有正文哈希，不用正文前缀承载路径。

子 Agent 发信的 `from_full_name`、`from_node_id`、`from_generation` 由 helper 的可信原生 threadId 绑定产生，并受 Hub 当前连接注册表校验。不能接受模型参数、CLI 的 `--from-path`、任意 IPC JSON 或环境变量自报来源。回信使用原信持久来源节点/代次；结束的来源不转交父会话。

旧 Hub 不理解新元数据。新 send/helper 若对端没协商成功，必须保留完整子目标并拒绝子投递，绝不把目标降成主地址。新 Hub 遇旧 helper 直接报不支持。旧 Hub 对完整三段地址的身份解析会拒绝，但“主地址＋未知子路径”实测会误投，不能宣称所有旧组件天然安全。推广前需先覆盖拒收保护，混部矩阵通过后再启用创建能力。

### 3.3 错误及顺序

先鉴权/编组，再检查地址合法性、深度、对端能力及树匹配，避免向无权限用户枚举子树。权限失败沿用现有错误，不以子错误泄露状态。

| code | 条件 | 发送方提示/处理 |
|---|---|---|
| sub_agent_not_found | 已完整恢复的树没有该节点/别名 | 子 Agent 不存在；任何上级都不收信 |
| sub_agent_ended | 节点已封口或已结束 | 已结束（持久原因）；附是否仍在退出宽限 |
| sub_agent_depth_exceeded | 子层数大于3 | 超出层数上限 |
| sub_agent_unsupported | 未协商、非 Codex 席，或一期请求二/三级 | 对端不支持子 Agent；detail=phase_not_enabled 等 |
| sub_agent_registry_unavailable | 恢复中/快照未确认 | 状态暂不可用，可稍后重试；不等同不存在 |
| sub_agent_path_conflict | 显式路径与保留结构不符 | 拒收，不投父 |
| sub_agent_generation_mismatch | 旧代次排队/回信/事件 | 拒收并保留审计，不重绑 |
| sub_agent_alias_conflict | 同父默认名/别名已占用 | 创建拒绝；不得抢占墓碑 |
| sub_agent_policy_required | 创建缺少已解析的存活策略 | 要求管理端明确策略；不偷选建议默认值 |

失败走现有结构化 receipt/failure_notice，关联原 delivery 与节点，禁止 AMR 递归转发错误。新子信必须等 Hub 持久接收确认后才能显示“已送出”；超时显示不确定，不自动重发。展示完成不推进消费 ACK。

## 4. helper：共享进程，独立分道

### 4.1 app-server broker

在原生传输之上引入单一 broker：只初始化一次，独占 reader、writer、请求ID映射与通知解码。每个 LaneRpcFacade 暴露同一 CodexDriver 接口，但只收到自身绑定 threadId 的事件。不能把一个共享原始通知队列交给多个 driver 争抢；原生 server request 与 response 必须按 JSON-RPC 形状分开，即使双向 id 重合也不串线。缺少/未知 threadId 的事件有界隔离并记录，不猜主会话。

helper 本地受控 `sub_agent_control` 提供 create/close/task_completed/context 操作；调用来源仍由原生外层 threadId 或管理端鉴权绑定。第一期主会话/管理端可创建一级孩子，孩子只能报告自身完成/关闭，不得伪造父、关闭兄弟或创建可投递二三级。创建时强制解析策略，返回 node_id、完整名和 allocating/active 状态；重复 control request_id 幂等，崩溃后不再创建第二条 thread。

一个节点绑定一个真实 threadId 和代次；分道新建必须是管理流程显式 `thread/start`，不能把任意 `thread/list` 结果认领成孩子。主会话继续既有 private_endpoint 根绑定；不能用 `save_thread` 把子 thread 覆盖上去。broker 管物理写者与恢复，LaneFacade 管路由及自身生命周期。

每分道实例化独立 DriverAdapter/FIFO/CodexDriver，使用相同实现代码。broker 写队列按分道有界公平调度，保留短控制请求容量；本地尚未写出的 turn/start 只排队，不启动入场计时，也不因兄弟占用写者替换健康传输。模型执行不持有全局 turn 锁，两道可同时 inProgress。子分道停止只 interrupt 自己的 turn、释放自己的订阅与 facade，不 kill app-server、不替换健康 transport。物理进程真实失活/写者卡死才由席级 broker 做一次受控恢复；所有可能已写出的任务保留 unknown/pending，不重发。父关闭经管理层级联，不能靠销毁共享进程实现。

### 4.2 来源和会话告知

全局 send.py/既有共享 MCP IPC 没有可信原生 threadId，不能仅凭其调用内容判断哪个子会话在发信。保留它作为主席入口；子会话经 broker 受控工具适配器发信。钉版0.159.2 的实验 schema 含 `thread/start.dynamicTools`，原生 `item/tool/call` 参数含 threadId、turnId、callId；来源用原生外层字段绑定，模型 arguments 只含目标和正文。缺少可信绑定就拒绝子发信，不降级成父发信。正式实现必须增加真实工具调用、伪造 arguments、迟到 callId/旧代次及双向响应乱序测试；本次并行实验只验证传输能力，没有声称完成工具来源闭环。

新建 thread 的独立 developerInstructions 告知自身全名、父、直接孩子、能力、策略及 context revision。提供受控 `dingwei_self_context` 查询；树变化在下一正常 turn 的独立控制输入项更新上下文，原业务正文保持独立且字节不改。没有新业务时不额外启动模型 turn，也不打断长任务。`turn/start` 无 developerInstructions 字段，不假设每轮可覆盖同名参数。

接口依据仅为钉版0.159.2二进制与其 `--experimental` schema（见交审原文），不依赖未经核实的在线外链。`thread/unsubscribe` 不代表业务任务完成；`thread/archive` 不作为第一期自动清理策略。

### 4.3 与 r55 reconcile 共用代码

实现前先集成已冻结 r55 `3dc5cd77f683bd21653955c76f97647ce78034f3`（产品 anchor `35515ea1`），不能在 r54 复制另一份旧 driver。账本、查询和回调都加 lane_id/generation，尤其现有 `get_by_envelope` 的无作用域 LIMIT 1 不可跨分道取证。

r55 冻结不变量不变：真实 turn/start 发出才计入场期限；本地排队不 unknown；每席同时最多一个只读对账请求，同 thread 最短15s，单轮共享上限200条（不是每分道各200）；1s时间比较容差；触顶 incomplete/pending；对账函数不 start/resume；补取复用 `_take_completed_item_backfill`；显示完成不推 ACK。共享 broker 持席级对账 semaphore 和按 thread 计时表。证据必须同时匹配 thread、lane/generation、dispatch intent 及原 ack_generation；相同 clientId、跨 thread、旧代次、冲突终态均不能借别道证据推进状态。响应/通知乱序不得覆盖已持久终态。

## 5. 生命周期（参数，不擅定默认）

| 参数 | 组长建议（待业主定默认） | 设计语义 |
|---|---|---|
| exit_on_task_complete | 开 | 显式完成信号触发退出；不是 turn/completed 或模型说“完成” |
| idle_timeout | 24h | 无排队、无执行、无待工具、无歧义执行时累计空闲 |
| max_lifetime | 7d | 自节点创建绝对累计，重启/离线不清零 |
| platform_lifetime_cap | 14d | 强制上界；建议按节点计，父先结束则随父关闭 |
| exit_grace | 10min | 封口后等待执行确认的最大时间 |

创建必须解析五项策略并持久化；建议值不是运行默认。PM 需确定平台总上限究竟按节点还是整棵树计时；两种实现都以父关闭级联为硬约束。策略禁用项显式表示，不能以0秒混用“禁用”与“立即过期”。读 View、心跳、在线查询、只读对账不刷新 idle；合法新任务和执行进度才更新活动时刻。待执行证据不确定时禁止 idle 自动退出，但绝对寿命上限仍生效。

状态：allocating → active → draining → ended；online/offline 是独立运输属性。先持久 allocating，再创建/绑定 thread，树注册 ACK 后 active；失败回收未成活项并记录原因，不能偷偷借主 thread。重启从持久表恢复 deadline/reason，不启动重复任务。

显式 task_completed 在策略开启时、父 close 无条件、idle/maxlife/cap 到期均先原子封口 draining，再通知 Hub 关闭接收；新信统一提示已结束（原因）。父关闭对后代按叶到根执行；一期没有可投递二三级，仍保留级联数据结构和拒收校验。封口和队列扫描必须同一持久边界，避免“扫描后又入队”。

已入队且确定尚未发给原生的信逐条写失败 outbox，退给各自原发送节点，幂等关联 delivery_id；不只退一条汇总。可能写出/正在执行的信不能谎报“未执行”：保留原账本 pending/unknown，宽限内只对账；届满最多 interrupt 本分道 turn、unsubscribe，通知执行状态不确定，不制造 processed/ACK。发送方自身已结束时留可检索失败记录，不转父或自动激活。失败通知不进入业务 AMR，不递归退信。

ended 墓碑及别名持续保留，14d 存活上限不等于14d后删除墓碑；保留策略另定。关闭原因区分 completed、parent_closed、idle_timeout、max_lifetime、platform_cap、operator_closed、creation_failed，保存原因为提示依据，不把进程暂离线写成结束。

## 6. 存储迁移与回退

Hub core SQLite 承载路由/投递，identity 的 PostgreSQL 不增子 Agent，不造跨库外键。迁移编号由集成者在最新序列分配，不占用基线下一号。

| 库/表 | 最小改动 | 不变量 |
|---|---|---|
| Hub sub_agent_registry（新） | root_agent_id、node_id、parent、generation、state/reason、revision、conn_seq、策略/时刻；主节点行持树版本 | root权威引用；节点/树版本持久，不含模型threadId |
| Hub sub_agent_alias（新） | root/parent/alias唯一→node_id，墓碑不释放 | 同父无歧义，无旧名转新任务 |
| Hub session_delivery_sub_route（新） | delivery_id关联同库投递，from/to节点、代次、路径、规范全名及注册版本 | 与原投递同事务；无记录的旧信明确为主分道 |
| helper inbox库（新注册/别名表） | node/thread绑定、代次、策略/截止、关闭原因/恢复水位 | 封口与逐条退信outbox同库原子落地 |
| helper inbox/driver deliveries | ADD lane_id 默认空（主）、lane_generation 默认1及作用域索引 | 旧主记录原样可读；查询/claim/recover/回执全带作用域 |
| View投影SQLite（新scope表） | root_agent_id+node_id+generation键，独立cursor/cost | 不复用仅agent_id主键覆盖兄弟；旧主投影保留 |

保留现有 inbox `(session,delivery_id)` 和 driver `(thread_id,envelope_id)` 主键，避免重建历史；Hub delivery ID 的目标身份输入必须加入 node_id/generation，防同一 envelope 发兄弟时去重合并。driver 历史允许同 envelope 不同 thread，不能用唯一(lane,envelope)索引抹掉历史尝试。新增作用域索引不替代查询谓词；所有按 envelope 单查必须限定分道/代次。helper driver账本与inbox既有分库提交仍使用 r55 恢复证据链，不假装跨库原子。

回退优先新二进制关闭 feature，保留加法 schema、主流量照旧。回旧二进制前必须：停止创建和子投递；持久封口；逐条退回未执行信；宽限/对账处理 in-flight；确认旧 claimer 不可能读到目标为父端点的子投递。仍歧义的子信须显式隔离并保留审计，不能让旧进程接盘后按主信执行。无法完成封口/隔离就禁止降级（继续新版本 feature-off）。静默清空表、将 unknown 改 processed 或只回滚程序都不合格。

变更前静止写者后备份 Hub SQLite、helper注册/inbox、driver ledger、View投影并核一致水位/摘要；不上传私有库。回退不 DROP 新表、不删除 ACK/墓碑/失败 outbox。用备份恢复仅限另行授权；本设计不执行在线迁移或回滚。

## 7. 全仓解析与使用点清单

以基线 git tracked Go/Python/JS/HTML/shell 中 `ResolveFullName`、ValidAgentName/ShortName、点/井号分割、full_name/sessionName、提及、在线列表扫描交叉核对；原始搜索清单见 review/raw/source-inventory.txt。下面每行给出实际职责及改法。表中“主身份保持”是有意不扩权，不是遗漏。测试/历史证据、URL主机/JWT/版本号点分割不属于全名解析，不改。

| 位置（基线） | 当前行为 | 第一期开改法 |
|---|---|---|
| identity/model.go ValidShortName/ValidAgentName；service.go ResolveFullName | 两段身份；最后一点拆分后左段仍受验证 | 保持主身份 API；新增独立结构化 route parser |
| identity claims/onboarding/hosted/httpapi；extseat/service.go；feishulogin/handler.go validTargetAgentFullName | 认领、登录、外部授权创建主体 | 主身份保持，不给子路径建身份/发凭据 |
| extshared/extshared.go SplitAgentName | 下划线 external 主名 | 保留旧两段兼容；一期子能力关闭 |
| m8/hub.go parseAddress/sessionAddress/serviceKey | # 传输拆分，不验证点分层 | 不把传输函数当语义解析器；显式调用新 parser |
| m8/hub.go resolveOwnerSessionByShort、RouteEnvelope；newplat_session_name.go | dotted进入严格identity，失败不模糊回落 | 先提取root权威，再树逐段匹配；保留不存在即拒收 |
| m8/hub.go prepareSessionSelector、AgentWS From校验 | #/@目标与根连接发件人校验 | 目标走同一parser；来源只接受已注册连接的节点/代次 |
| m8/grouping.go resolveSessionByFullName | 兼容旧在线表 | 树来源独立，不以模糊/后缀命中兜底 |
| m8/amr.go amrTokenName、parseAMRCandidateBlock/Detailed、amrResolvedShort | token允许多点，子提及进候选 | 三段以上跳过并提示，保留其他合法根候选、原正文；显示勿丢路径 |
| m8/newplat_mention.go、view_mention_route.go、view_mention_reply.go；hub.go parseMemberMention/stripLeadingMentions | 提及/回程身份解析 | 提及只根；回复依据持久来源作用域，不截子名 |
| m8/newplat_view.go；terminal.go ViewRouting.resolve、authorizeViewUnlockOwner | View根身份、授权、旧名兼容 | 分离root principal与lane scope；子订阅不能借机取得主解锁管理权限 |
| m8/console_mux.go muxDecode/resolveScopes；console_mux_broker.js；consolemux/authorization.go/gate.go | 3段订阅拒绝；授权/席位以root ID及full_name处理 | 能力化子scope；键为root+node+generation，8订阅预算按真实scope计，过期/撤权覆盖全子树 |
| m8/newplat_directory.go；mcs/hubdriver/hubdriver.go OnlineList.alive | 根full_name列表；MCS用Contains判断存活 | 新树JSON版本；旧文本列表只保留根，避免子行使旧Contains误判 |
| m8/agent_disk_metrics.go、inbox_lag.go | 根资源/收件积压 | 席资源总量保留根，分道积压新增scope维度，不将主统计当孩子状态 |
| m8/console_task_flow.go、newplat_spec_agent.go、hosted_quota_notice.go、feishu_sticky.go | 任务/规格/配额/飞书粘连根身份 | 主身份保持；显示可带来源lane，业务任务执行者不拆成多个主体 |
| m8/coordinate_review.go；admin/admin.go parseMirrorAddress | #拆分与协同/镜像权限 | 主任务权限保持；镜像先只主分道，子请求明确unsupported，禁止截断 |
| m8/newplat_feishu.go；feishulogin | 新平台管理命令/登录目标 | 管理对象继续根身份，子投递只走结构化路径 |
| store/failure_notice.go；sqlite.go投递/ACK；model/model.go SessionDelivery/SessionEndpoint | 井号来源、根端点、现有delivery记录 | 路径sidecar原子保存，逐条退信按原来源，不凭body重建 |
| sessionhelper/send_dingwei.py（安装后send.py）complete_addr/ipc_send | 自动补#key，无层级验证 | 公开full to，纯解析预检；能力不足拒收；子收据需Hub持久确认，不提供任意from-path |
| sessionhelper/app.py _make_send_handler/confirmed_send、能力/在线输出 | root self_addr/单接收入口 | root入口保留，增加可信lane工具入口与分道dispatcher、树快照 |
| sessionhelper/protocol.py AddressBook/session_addr；routing.py agent_route_envelope/reply_target | 地址拼接与#指令允许点，未校验层级 | 复用规范解析；回复保存来源节点/代次，不从正文补路径 |
| sessionhelper/inbox.py、unified_queue.py、outbound_receipt.py | #拆分/显示、根session队列与回执 | 所有claim/replay/receipt按lane+generation，显示使用规范全名 |
| sessionhelper/config.py normalize_client_session_name/detect_full_session_name | 根注册名校验、运行全名探测 | 根SH_SESSION_NAME仍是根；子注册独立，不覆盖根配置 |
| sessionhelper/codexDriver.py、driver_adapter.py、private_endpoint.py | 单thread绑定/driver/事件流 | broker/facade+每lane driver；共用r55、不覆盖bound_thread |
| view-service/view_service.py _session、mention proxy、subject/fullname、cost projection | X-DW-Session原样取值；依赖Hub授权；agent_id成本键 | 服务端验证scope能力票据，成本/游标拆lane；不单信任header |
| view-service/renderer-canary-sse.html | sessionName展示及localStorage/BroadcastChannel键；主提及选择 | 规范全名+node代次隔离票据/缓存；树列表/ended原因；提及仍根候选 |
| fileservice/manager.go sessionPathPattern；local_store.go、issuer/http/grants | session路径最多一点；scope票据全等 | 一期文件对象归root会话命名空间，子上传由Hub可信工具映射root并保留来源审计；不把多点直接塞路径、不放宽目录正则。子消息仍遵守源代码仅Git规则 |

Console 与 View 不从显示文字反推身份。新树在线清单显示“根→子”，附 online/ended/reason 与能力；legacy文本不加孩子伪主席。分道成本与历史需独立展示；root汇总不应被误标为一个孩子的费用。文件服务不成为源码交换或新身份信道。

## 8. 最小复现、读数与实施验收计划

完整命令、UTC、原始读数、SHA清单见 `docs/reviews/subagent-addressing-design/REVIEW.md`。复现不改产品文件：Go overlay挂载临时测试，使用既有内存identity与自己的httptest WS；纯Python入口不发送IPC。原生实验用独立HOME/CODEX_HOME、独立stdio app-server和本地假SSE provider，无真实推理、无在线席位操作。

| 已运行项 | 观测 | 限制 |
|---|---|---|
| legacy观察模式7个Go控制+3个Python纯入口 | rc0；identity/Console及Hub完整三段目标拒绝且父未收；parseAddress/send.py/View不做语义验证；AMR接纳子候选；root+未知meta投父 | 仅基线行为，不说功能已实现 |
| 同套future断言 | 恰2个目标红：AA005_CHILD_MENTION_NOT_SKIPPED_WITH_VALID_ROOT_RETAINED；UNNEGOTIATED_SUBPATH_REACHED_PARENT | 有意基线红，不放默认测试发现路径 |
| native0.159.2单进程两thread | 两turn同时inProgress；中断并退订A后B仍inProgress；本地请求2次 | 未测模型工具调用或原生spawn-agent，不宣称二/三级可用 |

一期实施验收与一刀变异（每行移除对应保护应在所述断言红）：

| 规则 | 必测正/反例 | 一刀变异位置/目标红 |
|---|---|---|
| AA001/002解析 | 短主名/两段/三至五段；第六段；空段、非ASCII、旧external根 | 去深度检查→depth_exceeded红；去分段验证→malformed红 |
| AA003拒收 | 未知、墓碑原因、离线/恢复中、二三级phase拒收、同名兄弟、旧别名 | 加父fallback→parent_never_received红；删除墓碑→ended_distinct红 |
| AA004来源/权限 | 子→主→原子回信；伪造from/原生thread字段；跨组；任务单执行者 | 信任arguments.from→origin_spoof红；只root授权scope→cross_lane红 |
| AA005提及 | 短根/全根；子与根混合；正文哈希不变；只有子时提示 | 保留子候选→本次future目标红；整封丢弃→valid_root_retained红 |
| AA006树/告知 | 同一分钟创建两个孩子默认名不同；UUID随机尾段碰撞时按末8/10/12位加长；alias双入口、结束不复用、direct children上下文 | 改取前8位→same_minute_default_names_distinct红；删除尾段加长→suffix_collision_resolved红；别名重绑→late_mail_not_new_child红；未去连字符→name_charset红 |
| AA007生命周期 | 五参数显式、turn完成不结束、显式完成、父关闭、时钟/重启/idle、每封退信 | 用turn/completed退出→long_task_survives红；只退汇总→per_delivery_return红 |
| AA008协商/broker | 新旧Hub/helper四格；双lane并行、乱序、双向同id、未知thread、关闭A不伤B | 未协商改root→本次misroute目标红；共用通知队列→sibling_event_ownership红 |
| AA009持久/reconcile | 同env两子、旧代次/错thread/重复clientId/nullable时间/冲突终态；重启/分页201/展示不ACK | 去scope→cross_lane_evidence红；改1s/15s/200分别目标红；提前ACK→display_not_ack红 |
| AA010混部/显示 | legacy列表无孩子、树JSON、View/Console票据/缓存/cost独立、回滚封口隔离 | 去rollback barrier→legacy_replay_parent红；root ID成本去重→sibling_cost红 |

此外按存储故障点注入：注册未ACK、写封口前后、退信outbox已写未发送、回执已到driver未入inbox、Hub投递sidecar未写/事务回滚。每个点重启核“不重复执行、不假ACK、不丢逐条退信”。功能实现后必须给对应变异原文，本设计不伪造未来测试已绿。r55控制组及原生钉版组应保持，canonical与OpenSpec strict过门再交审。

## 9. 工作量与交付顺序

下列为人日区间，含实现和模块测试，不是本次设计工时；尚不含独立复核、上线值守或第二期。

| 分工 | 人日 | 主要成本 |
|---|---:|---|
| Hub | 4–6 | parser/能力/注册树/来源校验/拒收/目录/投递sidecar |
| helper | 6–10 | broker、可信工具来源、每lane驱动/队列/告知、生命周期及r55整合 |
| send.py | 0.5–1 | full to验证、能力失败与真实确认收据 |
| View/Console | 2–4 | scope授权/票据、树UI、历史/成本/缓存隔离 |
| AMR | 1–2 | 根提及边界、混合候选提示、来源回程 |
| 迁移与回退演练 | 2–3 | 多库加法迁移、旧数据、墓碑/封口、故障恢复 |
| 集成与变异 | 2–3 | 混部矩阵、乱序/并行、native原文、门禁 |
| 合计 | 17.5–29 | 独立复核另2–4；可信工具接口若需额外协议适配再加3–6 |

单实现者约18–29工作日；获授权后若三席分工、接口冻结无阻塞，约8–14工作日加独立复核。此处只是估算，不启动协作者或预派实现。

顺序：①确认生命周期默认和平台cap语义、冻结协议/迁移；②先上线兼容拒收保护（另行授权），覆盖旧组件；③集成r55，完成broker/来源与持久封口；④Hub注册/投递→helper分道→View/Console/AMR；⑤故障/混部/回退与逐项变异复核；⑥再申请版本集成与灰度。默认参数未定不阻塞设计复核，但未显式配置策略时不得创建运行子 Agent。
