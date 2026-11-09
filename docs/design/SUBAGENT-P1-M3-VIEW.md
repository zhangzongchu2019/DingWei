# r57 子 Agent M3：展示、混部与回退

基线 coder56solzzc/subagent-p1-m2-helper / 0b47a5857cc331ee5dc8012a002fd561ae2279f0。依据 d9a43e6a §6/7、AA010 与 rulings〔51〕-补14。实施授权不包含部署、预发、在线席位操作或真实推理。

## 作用域和授权

根仍为唯一 identity principal；子不是新账户。Console 请求先按根向现有实时身份授权器认证，再由 Hub 当前连接能力和已ACK注册树展开子scope。每个根/子分别占一个订阅槽，合计最多8；相同根不能折算成一个。传输 Agent.ID 对子scope为 SHA256(root_id NUL node_id NUL generation) 的不透明 sub-v1 标识；独立 Session 键只用于内存事件环，不会向真实 helper 注册新根。

helper 只从各自 DriverAdapter 队列提取子事件，外层来源由节点绑定形成。Hub 校验当前连接、路径、node与generation，冲突消费丢弃、不落父。输入/回执观察依据持久投递sidecar选择投影，不能从事件正文认来源。模型事件、费用、游标不混到父或兄弟。

Hub签名投影主体含 root_agent_id/node_id/lane_generation；浏览器来头全部清除重建。View服务先验目标密钥签名/nonce/时效，再核subject与session和scope哈希。子页面身份缓存及BroadcastChannel按scope ID隔离。聚合桥仍需双向服务签名及feed/scope绑定，帧新增的scope三元组同样比对。独立子SSE将已验签主体带回内部事件端点，由Hub再次核签与viewer会话绑定；发每帧前及每秒核当前用户会话、根身份、连接/树。根撤权清除全树mux订阅，失效租约停止写出。

子页面保持只读；主输入授权、管理、文件权限不扩大。子页提及查询复用根编组权威，只返回根身份候选；结构化子投递仍用受控工具。旧主投影和缓存键保留。

View SQLite加法表sub_agent_scope/sub_agent_cursor/sub_agent_cost明确保存三元组；原mux_cost及旧种子/seen历史保留，子费用使用独立scope ID去重。完成事件的cost/seen/cursor同事务；普通事件只推进自身cursor。paused成本模式继续拒写费用目录。

## 树目录

旧在线清单正文格式保持只列根，绝不追加子行。online_directory控制信加tree_version=1与trees全量替换；helper另写原清单路径加.tree.json后缀，收到旧Hub快照则清空树JSON。树不进入模型业务正文。

GET /api/console/agent-trees 按实时用户会话取自己的根与持久节点，返回version=1；不从header认属主。Console服务端能力旗标决定是否读取此端点；旧Hub页面无旗标时只显示原根列表。树显示根→子、在线/离线、ended与原因；失败时停止子订阅并显示状态暂不可用。树有墓碑不代表可交互。

## 五格混部

旧Hub锚为79a60a1e351c207ff0b46bdf9151e846ec83f5d4（含先行兼容拒收保护，非未保护的历史Hub）。旧helper指未协商子能力的M1前行为。实测脚本与原文随交审证据归档。

| Hub | helper / View | 结果与用户提示 |
|---|---|---|
| 旧 | 旧helper | 根保持；完整子地址被身份解析拒收，不截成父 |
| 旧 | 新helper | 根保持；无sub_agents_v1 ACK，创建/子信提示状态暂不可用或对端不支持；不装作创建成功 |
| 新 | 旧helper | 根保持；子信sub_agent_unsupported，不向父投递 |
| 新 | 新helper | 先协商/树ACK，一级子投递成功；保留节点代次字段；未确认前registry_unavailable |
| 旧 | 新View组件 | 根展示保留；无树能力时不探子端点；无签名子scope返回拒绝，不用header冒充根或子 |

tests/subagent-p1-m3/run_old_hub.py 使用旧锚实际HTTP/WS处理器加只读测试overlay；新两格使用实际新Hub WS处理器。helper未协商创建门、旧主重开门由M2和M3专项保留。浏览器是生产Console源码加隔离HTTP依赖，不能冒充线上浏览器验收。

## 隔离回退及运维前置

本次只在自建临时库演练，未操作在线数据。先新二进制feature-off：Hub DW_SUB_AGENTS_V1=0，helper SH_SUB_AGENTS_V1=0。根业务保持，创建/子投递拒收。helper检测历史注册树时进入仅退信维护路径，使用正常seal原子封口、逐封outbox退信，不创建broker/线程，不发布可用能力。未执行信返回原发送节点；可能写出者unknown保留。退信在发送前记uncertain，只有Hub持久接收才记returned；超时不自动重发。不可达来源保留undeliverable审计，不改投父。

真正回旧二进制前，运维须另获发布授权并完成：

1. 停止所有相关写者；为Hub core、helper inbox/注册、driver ledger、View投影做一致水位备份，记录摘要；库仅留受控本地，不上传。
2. 用新版本关闭创建/子投递；执行helper prepare_rollback，确认returns_pending=0。pending/uncertain尚未查明时禁止降级；绝对不能手改成returned。歧义执行行保留其lane namespace、unknown账本及原ACK。
3. 对Hub每个持久根执行新工具：go run ./cmd/subagent-rollback --hub-db <隔离或已停写的明确路径> --root-agent-id <ID> --writers-stopped --feature-off --helper-sealed。参数是运维前置确认，不替代授权。工具不会联网，也不会自动迁移或恢复库。
4. 事务内：Hub所有未执行子信逐条写原失败通知队列并终止原信；已送出/执行不明信不改成processed；所有子投递移到保留隔离session。回给本树子来源的通知同样隔离，不能被旧claimer投父。新增rollback及逐delivery审计表；重跑幂等。墓碑/ACK/别名/sidecar均保留。退信不可达时原信/审计仍可检索，不伪造接收。
5. 使用旧源claimer实读备份的演练副本，证明根仍可读、子一封也读不到，核所有根的子sidecar均已隔离；不能完成则保留新二进制feature-off。上旧程序仍需独立发布批准，本单不执行。

0076为纯加法；legacy迁移补账复用exact-DDL预检。回退不DROP、不删除ACK、墓碑、失败通知或rollback barrier。barrier阻止重连重建树，重新前进启用另行设计/授权，不通过删表/清标记恢复。

run_old_claimer.py 先由新源准备隔离库，再由79a60a1e旧源实际ClaimSessionDelivery读取同一库，原文附commit/UTC/rc。helper测试用旧根claim语义读取已封口lane库；尚未确定的执行保持unknown，不冒称安全重放。
