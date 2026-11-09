# 第一期设计决策

详细设计与逐处改法唯一正文：../../../docs/design/SUBAGENT-ADDRESSING-PHASE1.md。

- AA-001/002：路由解析器独立于主身份API，最多三级，external历史两段主名保留。
- AA-003/007：结束墓碑、恢复中与不存在分开；生命周期参数显式配置，先封口再逐条退信，代次/原因跨重启。
- AA-004/008：可信原生threadId绑定来源；单broker分发、独立分道队列；能力未协商禁止降成父地址。
- AA-005/006：子提及跳过并提示但保留合法主候选；默认名与别名指同一节点，每会话获知自身与直接孩子。
- AA-009：加法存储迁移，r55同一driver按lane/generation隔离；回旧版先封口/隔离，不能旧claimer误投父。
- AA-010：一期只一级Codex helper线程，树JSON独立于旧根文本；View/Console授权/成本/缓存按节点代次。

新接口/状态/错误码全部映射：正文§2/3解析与注册/协议/错误→AA001～004、008；§4 broker/上下文→AA004/006/008/009；§5 lifecycle状态/reason/policy→AA007/008；§6 schema/回滚→AA009；§7树目录/展示/文件根作用域→AA010。当前为设计候选，不宣称产品已实现。

〔51〕-补5追加：AA006默认名改UUID随机末8位，冲突取更长尾段；同分钟双创建场景与前缀变异同步。接口依据仅保留钉版二进制/experimental schema；现状主spec的Purpose改为记录已有行为。bbfbf95b保持原提交不改写。

## M1 Hub 实施补充（2026-11-08）

派单S-SUBAGENT-P1-M1，以79a60a1e为父；具体协议、根授权顺序、sidecar、旧唯一索引加作用域及回退边界见../../../docs/design/SUBAGENT-P1-M1-HUB-PROTOCOL.md。映射AA001～004、008、009，未扩展Requirement数量。current conn_seq＋仅内存ready门防重连继承；source_type只对子投递追加节点代次摘要，保留旧索引和原消息ID；最终写与封口按连接互斥，不持Hub全局锁等待网络。

## M2 实施决定（2026-11-08）

对AA004/006/007/008/009复用r55同一驱动与对账护栏；inbox内部session存储命名空间编码lane/generation，行和回调保留显式作用域。创建submitted先于native写入，未知结果不重建；封口先事务draining及逐条退信。连接ACK才开放收信。详细状态、迁移及回退依据docs/design/SUBAGENT-P1-M2-HELPER.md；原生0.159.2旧主thread无法补装dynamicTools是已核实协议限制，不自动更换主席thread。M3展示与回退演练另派。

## M2 线协议收口（2026-11-09）

对应AA004/AA007/AA009：原生创建响应等待不持管理锁，父结束可先封口，迟到绑定保持draining/ended；native工具发送按thread/turn/callId和节点代次持久占用，未知结果不重发。M1现有delivery_ack_confirm无lane回显，helper接受投递同事务写helper_sub_agent_delivery_scope，确认唯一定位侧表后进入scoped inbox，重复身份拒绝而不从回执单查猜归属。完整to仅补#key传输封装，保留子路径。最终候选826a7c0ec，产品冻结eed6ac547；后续仅测试隔离、显式turn不等于task完成断言和库存数92同步。详细证据见docs/reviews/subagent-p1-m2-helper/REPORT.md。

## 〔51〕-补13：旧主会话控制工具硬门

AA008-S03：在任何节点intent落库前验证主thread的工具安装记录；首次树报告前也先核该门，旧主会话不输出快照。旧主的dynamic_tools及上下文显式标为不可用，原生控制调用也拒绝。固定、可公开的NativeToolUnavailable异常由broker透传中文重开提示，其余异常保留原泛化策略。只有用户显式重开才走已有thread/start安装流程，产品不清空或替换已存在的private_endpoint绑定。四个独立测试及四刀分别覆盖工具、无副作用错误、保留绑定、重开恢复。

## M3 View与回退实施

按〔51〕-补14在0b47a585上追加；规格AA010-S01～03。根授权后展开root/node/generation scope，独立预算/签名票据/事件/缓存/费用，旧文本目录只列根。新树JSON与只读Console树见docs/design/SUBAGENT-P1-M3-VIEW.md；子不获主输入/管理权。Hub迁移0076保留rollback barrier与逐投递隔离审计，helper feature-off有不启动原生的退信维护路径；不DROP、不删ACK/墓碑。混部和旧源claimer在隔离临时环境实跑；无部署授权。用户说明草稿docs/releases/r57-sub-agent-seat-guide-draft.md。

## M3 〔51〕-补15 全库门

对应 AA009-S03：cmd/subagent-rollback 新增互斥 --check-all；store.OpenSQLiteReadOnly 复用既有打开路径但用字面路径转义的 mode=ro＋query_only，不改 journal_mode、不自动迁移。CheckSubAgentRollback 使用 NOT EXISTS 扫描全部sidecar而非注册根，有to按接收根，否则from按来源根归组，未知／损坏根 fail closed。输出pending及roots/count/delivery_ids；有残留rc1、全空rc0、参数错误rc2。不使用immutable，须包含已提交WAL记录；只读连接可创建SQLite运行旁文件，DB与WAL内容不变。runbook第3和5步以同库自检rc0为降级前提，原发布授权边界保持。
