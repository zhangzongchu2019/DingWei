## MODIFIED Requirements

### Requirement: HCD-004 忙等待不能无条件替换健康传输
Helper SHALL 区分健康目标的旧回合等待与实际提交后的入场未知。Codex 和 Claude MUST 在确知本信尚未提交、旧回合仍执行且传输健康无卡住写入时暂停原入场计时；空闲后仅一次入场。实际提交后 SHALL 使用真实写出起点计时，不因旧回合等待导致账本入场期限提前到期。等待超限按 HBA-001 处理；未知入场不得自动重投。

#### Scenario: HCD-S005 旧回合长时间运行
- **WHEN** 新信等待健康旧回合90秒后旧回合结束
- **THEN** 两种驱动均不提前unknown、不替换健康传输，随后只提交一次并可记录admitted

## ADDED Requirements

### Requirement: HBA-001 有界等待与原子取消
Helper MUST 从首次确认本地忙等待起按 SH_BUSY_ADMISSION_MAX_SECONDS 控制上限，默认1800秒；超限 SHALL 先原子取消确知未提交的等待者，再记unknown且输出固定一行 [delivery][ALERT][BUSY_ADMISSION_TIMEOUT] id=投递号 driver=驱动名 limit_seconds=上限; no automatic replay。取消与进入提交边界 MUST 互斥；后台worker及同ID后续调用不得越过取消重新入场。若并发已跨提交边界，MUST 按现行入场未知规则，不假定取消成功。

#### Scenario: HBA-S001 上限前后
- **WHEN** 旧回合持续执行到上限前一秒、再超过上限
- **THEN** 上限前仍processing且未提交，上限后unknown且固定告警；释放旧回合后无晚到提交

#### Scenario: HBA-S002 并发提交与取消
- **WHEN** 本信已进入提交边界或写出
- **THEN** 本地忙取消失败，不把可能入场的信重新放入等待或重投

### Requirement: HBA-002 等待状态复用事件通道
Helper SHALL 在健康忙等待期间经现有事件队列输出 state_change，data.change=delivery_waiting_agent_idle、lifecycle/state=waiting、reason=agent_busy、status_text=排队等待 Agent 空闲，附原delivery_id、等待秒数和上限；长等待每30秒最多一条，cursor含该等待周期编号。等待事件 MUST 不宣称已入场或unknown，不修改Hub协议。View SHALL 显示明确等待文案，保留canonical原输入、权限与关联；前缀差异及事件先后顺序不得新增重复输入，已入场后的迟到等待事件不得重新排队。

#### Scenario: HBA-S003 旧回合未结束
- **WHEN** 本信尚未提交且确认健康旧回合执行
- **THEN** 既有事件队列可取得waiting状态与原投递号，View显示排队等待文案且不改原输入；等待期间不发unknown

### Requirement: HBA-003 传输故障不获忙等待豁免
Helper MUST 在传输失活、实际写入卡住或本信已可能提交时保留现行 admission_timeout、受控恢复和未知不重放边界；忙状态不得遮蔽故障，不为不明前置RPC等待伪造旧回合忙证据。

#### Scenario: HBA-S004 忙时传输失活或卡住
- **WHEN** 两驱动的本地等待伴随真实传输失活或写入超时
- **THEN** 不继续健康忙等待豁免；Codex确知未提交时沿旧逻辑defer与恢复，Claude沿旧未知/恢复路径；不重复提交
