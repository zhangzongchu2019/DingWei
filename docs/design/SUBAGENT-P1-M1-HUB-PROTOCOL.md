# 子 Agent 第一期 M1：Hub 协议与存储补充

本件落实 S-SUBAGENT-P1-M1，父锚 `79a60a1e351c207ff0b46bdf9151e846ec83f5d4`，设计锚 `d9a43e6ae038dfe0bd3ff7ace93aa0d9cd99c0f5`。依据〔51〕-补2、补10、补11；不改变主身份 API、凭据、编组或任务执行者。M2 的 helper broker／分道 driver／生命周期和 M3 的 View／Console／混部演练另派。本分支不修改 helper，不上报真实树，不部署。

## 协商与全量树

具备专属凭据、当前 active selfhost 根身份的 Codex 连接，以 session_capabilities 控制帧声明：

```json
{"type":"session_capabilities","delivery_ack_v1":true,"sub_agents_v1":true,"max_supported_depth":1,"subtree_revision":0}
```

Hub 回复 `sub_agent_capabilities`，含 sub_agents_v1、max_supported_depth、当前已鉴权 conn_seq、已持久 subtree_revision 和 resync_required。能力存在于当前 sessionClient；连接替换、Hub 重启后不继承。未支持／未协商继续拒收兼容保护中定义的保留字段；无子路由的旧根业务保持原路径。能力撤回立即停止该连接的子路由。

helper 后续须在收到 Hub 支持后，提交控制帧 `sub_agent_tree_report`。字段为 conn_seq、base_revision、subtree_revision、nodes；不是业务正文。nodes 为全量，根节点隐式存在，一级节点的 parent_node_id 为空。节点含 node_id、canonical_name、aliases、state、generation、reason、created_at、可选 policy。模型原生 threadId 不属于 Hub 节点结构，不传 Hub。

Hub 计算规范摘要（节点按 node_id、别名排序），同版本同摘要幂等 ACK。推进须 base_revision 等于当前版本且新版本严格增加；倒退／同版异摘要拒绝，ACK 带 resync_required。旧连接或报告 conn_seq 不符拒绝。ACK 的 ok 只有数据库事务提交后才为 true，当前连接也只有此时可接子信。拒绝后必须重同步。

快照不得省略已持久节点；遗漏不能删除墓碑。node_id、generation、规范名、创建时刻、策略不可重绑。active 不回 allocating，draining／ended 不复活；新 thread 必须使用新 node_id 和未占用名称。别名永久占用，同父默认名与别名共用唯一域；未再次出现在快照中的旧别名仍保留，不能绑给新节点。M1 不生成默认名、不执行生命周期时钟；这些由 M2 按批准的完成即退、1h／2d／5d／10min 默认实现。

## 业务路由、来源与确认

公开 to 保留完整地址。解析器输出根与根到叶路径，最多五段；第一期仅一级可投。先按根身份验证发件人与目标编组，之后才返回子路径、能力和注册状态；不能全局放宽 identity。两段 external 主名使用 extshared 的原校验；其子能力不开放。

Hub 根据当前连接与持久树重建 sub_path、target_full_name、target_node_id、lane_generation、subtree_revision。显式路径和提交的保留结构不符返回 path_conflict；目标旧代次为 generation_mismatch。接收信封中的 to 使用规范完整子名，物理 WS 与队列端点仍为根，正文逐字节保持。

子来源只接受当前已鉴权 helper 连接上的 from_full_name／from_node_id／from_generation 声明，并校验其属于该连接注册的节点和代次。helper M2 必须在可信原生 threadId 绑定后产生这些字段，不能把模型 arguments、普通 IPC、环境变量当来源。Hub 侧校验不代替该原生信任边界。发送方的根身份承担编组授权，接收方看到完整子来源；持久 sidecar 保存原来源节点。普通 Hub 调用或未协商连接不能自带保留字段制造来源。

子投递要求可靠内容队列。delivery_id 对原 envelope_id、目标物理端点、来源及目标 node_id／generation 做确定性哈希；内存 claim 同样区分子目标，不能让兄弟合并。ACK／cancel ACK 还须带与持久投递相同的 target_node_id 和 lane_generation；旧代次、兄弟、已封口节点不能推进状态，原 ACK v2 的 ack_generation 等门继续生效。

入队同时验证来源与目标绑定；sidecar 和原投递同事务。出队及最终物理写入前再次检查当前连接能力／注册树／节点代次；最终写与树封口按连接互斥，网络写带 5s 上限，不占 Hub 全局锁。来源身份在排队后可跨运输重连保留，但这不能授权旧连接注入新消息。缺 sidecar／存储能力或未完成重同步时拒绝写出，不解释为主分道。节点结束／代次失效使原投递终结并留下审计；失败通知按原来源 sidecar 逐条创建，原来源已结束时不转父、不重活、不递归通知。

错误沿用设计 §3.3 的 `sub_agent_*` 码：not_found、ended、depth_exceeded、unsupported、registry_unavailable、path_conflict、generation_mismatch；创建／快照另有 alias_conflict。未支持和恢复中不合并成不存在。SQLite 等非协议故障在 Hub 边界变为 registry_unavailable。

## 加法迁移与旧索引兼容

本基线最新 Hub core SQLite 迁移为 0074，本单新增 `0075_sub_agent_registry.sql`：

- sub_agent_registry：以 root_agent_id／node_id 为键；空 node_id 行保存根的 revision／digest／conn_seq，普通行保存节点状态、代次、原因、创建时刻与策略。
- sub_agent_alias：root／parent／alias 唯一，指向同库节点；不释放墓碑名称。
- session_delivery_sub_route：delivery_id 外键及完整来源／目标 scope JSON；与 session_delivery 同事务保存。

既有唯一索引 idx_session_delivery_source 按 source_type／source_message_id／key_id／session_name 去重，只改变 delivery_id 仍会合并兄弟。为保持原索引与原消息 ID，本单仅对子投递将 source_type 写为原类别加 `/subagent/` 和来源／目标节点代次摘要。旧根类别保持原值；失败通知识别 session_inbound 的子作用域类别。确定性 delivery_id 处理同目标跨重启重投；相同 envelope 给不同兄弟则有独立 source_type／ID／sidecar。此方案不删除旧索引、不重建旧表、不修改旧行。

迁移使用现有精确 DDL／摘要台账预检；重复启动不重放。旧投递无 sidecar 即主分道，但带子保留字段而缺 sidecar 的损坏行必须隔离。测试构造迁移前数据库，逐字段比较旧投递前后相同，并重新打开数据库读取旧行和新作用域。

## 回退操作说明（未执行）

新二进制可配置 `DW_SUB_AGENTS_V1=0` 停止协商和子路由；根流量继续原路径，加法表与审计保留。该开关会阻止子队列写出，不能用它冒充清队列、生命周期封口或成功回退演练。

回旧二进制之前仍必须先停止创建／接子信、持久封口、逐条退回确定未执行项、宽限内处理 in-flight，并隔离仍歧义子投递。旧二进制不理解 sidecar，根队列中仍有子信时可能按父执行；因此未完成隔离就禁止降级，保留新版本 feature-off。M2／M3 另交实际封口／退信／回退演练。本单没有在线操作，不执行数据库备份恢复，不 DROP 新表／索引，不删除墓碑／ACK／失败通知。

规格映射：解析及拒收→AA001～003；可信来源／回程／编组→AA004；协商／控制帧→AA008；事务／去重／ACK／恢复及回退→AA009。AA005 的先行保护保持，AA006／007／010 的 helper／展示项不在本单宣称完成。
