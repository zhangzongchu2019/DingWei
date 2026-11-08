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
