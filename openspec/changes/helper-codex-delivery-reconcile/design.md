# 设计决策

## 冻结依据

实现基于组长rulings〔51〕-补3，review/opus50tc2k/rulings fe4ad167990bb5bfd7593cf6145462f93de661d8，2026-11-08T10:58:25Z；同分支追加，写锁 /tmp/coder56-helper-reconcile-impl.lock。未实现Hub/View、多地址或子Agent路由，不改VERSION，不操作在线数据。

## HCD-001：证据与原代次

原生thread/turn/clientId、startedAt/completedAt、status、查询时刻、精确响应SHA256与inbox原ACK代次/dispatch intent/request ID快照放既有deliveries.detail的reconcile JSON后缀；不保存原消息内容、不增加schema。原生秒与浮点intent使用代码常量NATIVE_TIME_TOLERANCE_SECONDS=1.0，验证finite、非bool、正数、先后与未来时刻。inbox原事务内验证快照再幂等提交；已存在的turn/outcome不能替换。inProgress只推进原生admitted，缺结束时间不能推进processed。

## HCD-002：可信范围与只读预算

查询范围来自本席driver账本中所有实际dispatch thread（包含历史），私有endpoint仍承担当前binding/写者身份。先journal后turn/start的既有顺序保证没有账本记录的全新binding没有本投递；不扫描这种尚未materialized空thread，避免把其不可读误当成对历史匹配的否定。候选须属于原dispatch账本thread，已知turn不得换绑。所有查询页完成后才判断唯一；同一clientId在多个可信thread或页重复即歧义。无匹配仍pending，绝不重投。

同席账本路径共享对象锁与flock，最多一个对账请求；thread最近查询时刻复用detail持久保存，替换/进程重启不重置15秒间隔；单轮所有thread共用200条预算，达到200直接incomplete，即使已见匹配或无nextCursor也不推进。异常、无效/循环cursor、页超额保持pending。函数不start/resume，也不改当前thread。

## HCD-003：展示与证据分离

用户可见输出仍只复用_take_completed_item_backfill及原能力协商/稳定item逻辑。对账不以文本证明完成。history完整时间回放仅对已知持久thread/turn允许ACK证据；只补输出不写inbox。

## HCD-004：实际写出计时与本地队列

Stdio与WebSocket真实request写锁取得后、写出开始前触发本地提交时钟，包含真正写阻塞；等已有回合、endpoint admission和写锁本身不计入场期限。adapter同步worker等待时只读本地receipt，不在该等待循环增加网络对账。原native通知可先于响应给出入场；健康忙等待不受旧stall窗口截断。未发出且真实传输断开，原命令保留processing并受控替换后重试；只有driver账本完全没有该delivery才允许这一本地恢复，已有unknown journal永不重投。

## HCD-005：崩溃恢复与合并

先订阅consumer，再只读pending journal对账；完整证据先CAS落driver detail，再发独立终态event，inbox事务原代次验证后推进ACK。重启从detail恢复，不依赖旧对象id/通知；稳定thread/delivery/turn/status/原生结束时刻键在对象内去重，跨对象靠inbox事务幂等。普通receipt done不再让Codex用当前时刻补造processed；晚响应不能覆盖已落的对账终态证明。实时delta沿原对象去重，不按同cursor吞片。

原第一步分析及工期讨论保留在docs/reviews/helper-codex-delivery-reconcile/ANALYSIS.md，不能把历史“待实现”段落理解为当前实施状态。
