# 设计决策（待实现）

完整分析见docs/reviews/helper-codex-delivery-reconcile/ANALYSIS.md。本步没有产品实现。

- HCD-001：终态独立于输出/receipt。用精确thread/turn/clientId和原生起止时刻形成证据，经幂等持久合并推进原ACK代次；缺时刻不补造。使用原生startedAt/completedAt秒级精度，查询时刻另记；处理精度/有效数值和冲突。
- HCD-002：unknown以clientId在当前及可信历史绑定中只读查找；分页用原生opaque cursor，不用View cursor。无匹配/多匹配/查询不全均不重投、不判未执行。
- HCD-003：用户可见agentMessage补取沿用capability-gated稳定item替换，不写投递终态。正式证据路径不能复用输出文本作为完成证明。
- HCD-004：区分本地等忙、真正turn/start发出、已入场/终态待证；不能仅因等忙超时就替换健康传输，传输失活另走受控恢复。阶段名称、配置键及数据迁移本步不新增。
- HCD-005：跨对象持久pending/证据恢复，与实时事件先订阅后快照合并；终态稳定键幂等，delta同cursor不同片不丢。每轮有预算，超限保留unknown；不扩大历史thread到他席、不实现多thread寻址。

风险与工期：2～4人日最小实现，需新迁移/旧协议兼容则4～7人日，另加1～2人日独立复核。范围和上线均须后续派单，不从本分析推导授权。
