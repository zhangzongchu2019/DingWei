# helper-delivery-evidence Specification

## Purpose
helper（sessionHelper）在原生通知丢失、传输替换或入场超时之后，只凭完整的原生证据（精确 thread／turn 关联与原生起止时刻）推进本地投递终态与 ACK；证据不全时保持未决、不补造时间、不重投。适用于 Codex 驱动、driver adapter 与收件生命周期；不改变 Hub、View 或协议版本。

## Requirements

### Requirement: HCD-001 只凭完整原生证据推进终态
Helper SHALL 以精确thread/turn及clientUserMessageId关联和原生startedAt/completedAt核实终态，并MUST按原ACK协议代次幂等提交；缺失时间不得由当前时钟、文本、usage或文件时间补造。completed/interrupted/failed分别记outcome。秒级原生时刻与浮点dispatch intent比较使用1秒容差，非有限数、bool、负数、倒序或超出容差的旧时刻 MUST 拒绝。证据快照绑定原ACK代次与dispatch intent，旧快照不推进新代次。

#### Scenario: HCD-S001 终态通知丢失
- **WHEN** 原生history保留完整关联及起止时间，但实时通知丢失
- **THEN** 对账可持久补齐原终态；同一证据重复输入不重复ACK

#### Scenario: HCD-S002 缺时间的输出补取
- **WHEN** 只有回合完成文字或原生时间为空
- **THEN** 不推进投递ACK，保留未确认状态

### Requirement: HCD-002 入场未知只读按原ID对账
Helper MUST 在当前及可信历史绑定thread按userMessage.clientId精确查询原clientUserMessageId，只读使用原生opaque分页，不调用turn/start或resume；无匹配、歧义或查询失败 SHALL 保留unknown，永不自动重投。

#### Scenario: HCD-S003 响应丢失但原生已入场
- **WHEN** 超时后本地没有turn ID，但历史绑定thread存在唯一匹配原生turn
- **THEN** 按原生证据恢复关联，既不新建turn也不改变当前绑定

### Requirement: HCD-003 输出补取与ACK证明分离
Helper SHALL 沿用能力协商的稳定item替换输出补取；输出补取 MUST 不写inbox终态或合成可推进ACK的缺证据terminal。

#### Scenario: HCD-S004 完成输出可见但终态缺证
- **WHEN** 历史agentMessage可补显示但起止证据不全
- **THEN** 允许受控显示输出，仍不把投递标为processed

### Requirement: HCD-004 忙等待不能无条件替换健康传输
Helper SHALL 区分健康目标的旧回合等待与实际提交后的入场未知。Codex 和 Claude MUST 在确知本信尚未提交、旧回合仍执行且传输健康无卡住写入时暂停原入场计时；空闲后仅一次入场。实际提交后 SHALL 使用真实写出起点计时，不因旧回合等待导致账本入场期限提前到期。等待超限按 HBA-001 处理；未知入场不得自动重投。

#### Scenario: HCD-S005 旧回合长时间运行
- **WHEN** 新信等待健康旧回合90秒后旧回合结束
- **THEN** 两种驱动均不提前unknown、不替换健康传输，随后只提交一次并可记录admitted

### Requirement: HCD-005 恢复有界且不扩寻址
Helper MUST 对跨driver对象的pending证据恢复保留持久来源，按稳定终态键幂等合并，并SHALL限制对账范围与预算：同席单请求互斥（含跨进程）、同thread间隔至少15秒、单轮至多200条turn，达到200即incomplete保留pending；扫描不全不能给阴性结论，不改变正文或子Agent的寻址语义。

#### Scenario: HCD-S006 重连或分页预算耗尽
- **WHEN** driver被替换，或查找尚未遍历所有可信历史分页
- **THEN** 不把旧对象id/View游标当原生连续证明，未完成对账仍保留pending/unknown

#### Scenario: HCD-S007 重复clientId与200条边界
- **WHEN** 可信账本绑定范围出现重复clientId，或单轮读取恰好200条且匹配已出现
- **THEN** 不推进ACK；分页完整性与全范围唯一性确认前保持pending，不能依据首个匹配或不完整负扫描自动重投

#### Scenario: HCD-S008 重启与旧代次证据
- **WHEN** 完整证据已落既有driver账本但inbox尚未提交时进程重启，或随后ACK代次已经变化
- **THEN** 新进程无需旧通知对象即可重放原证据；仅原代次可幂等落账，冲突终态不覆盖原outcome，不加schema迁移

### Requirement: HBT-001 根 thread 显式绑定且不可替代
Helper MUST 在 SH_CODEX_BOUND_THREAD_ID 非空时以该 ID 构造根 Codex driver；首投递前沿用 thread/read、必要时 thread/resume 及 ID/cwd/状态/canAcceptDirectInput 校验。不可用 SHALL 返回 failed，detail 含 bound_thread_unavailable，MUST NOT thread/start 新建替代；已发出投递的未知结果仍保留 model_admission_unknown 和原幂等规则。

#### Scenario: HBT-S001 指定 thread 可用或需恢复
- **WHEN** 操作员设置根绑定，指定 thread 可用或 read 显示未加载而 resume 成功
- **THEN** turn/start.threadId 为指定 ID，校验先于投递，无 thread/start

#### Scenario: HBT-S002 指定 thread 不可用
- **WHEN** read/resume 失败，或 ID、cwd、状态、直接输入能力不符
- **THEN** 回执 failed 且含 bound_thread_unavailable，既不新建 thread 也不投递 turn；重复信封返回持久失败

### Requirement: HBT-002 private 绑定冲突停止启动
Helper MUST 优先采用显式根配置；private binding.json 已有不同 ID 时 MUST 启动报错，文案包含两个 ID，不自动覆盖文件。无冲突时，仅校验通过的指定 ID 可保存为当前绑定。

#### Scenario: HBT-S003 private 文件与配置冲突
- **WHEN** 当前 owner 的 binding.json 有不同于配置的 ID
- **THEN** helper driver 启动失败、释放 writer，错误显示两个 ID，文件未覆盖、app transport 未连接

### Requirement: HBT-003 保留未配置与子分道行为
Helper SHALL 在未配置或配置为空时保留原根工厂参数与行为。根绑定 MUST 不影响 M2 子 lane 运行时 ID、broker 白名单与原生工具准入；根 driver 重建仍保持配置。操作文档 MUST 说明取得 ID、设置、停 helper 后换绑及 private 冲突处置边界。

#### Scenario: HBT-S004 未配置与子 lane
- **WHEN** 未设置配置，或已配置根且在既有工具具备的根下创建子 lane
- **THEN** 原 codexDriver 回归全绿；子 lane 使用自己的 thread，不能向根 thread 写入；根重建保留根配置

### Requirement: HRI-001 恢复继承与首投校验
Helper MUST 在恢复替换 Codex driver 时继承旧 driver 当前根线程；新驱动首投前 SHALL 以既有 thread/read、notLoaded 后 thread/resume、ID/cwd/入场状态校验验证原线程，成功时禁止 thread/start。继承仅为运行时提示，不代替显式绑定配置。

#### Scenario: HRI-S001 恢复后同线程继续
- **WHEN** 传输恢复且旧根可 read 或 resume 后可用
- **THEN** 下一封信只向旧根 turn/start，不新建线程

### Requirement: HRI-002 未绑定不可用根显式换新
Helper MAY 仅在继承根校验出现 missing_thread 或 ThreadUnavailable 时换新；MUST 输出固定格式 [delivery][THREAD_REPLACED] old_thread=旧号 new_thread=新号 reason=原因码，并发 state_change 事件 change=delivery_thread_replaced，含相同 old_thread_id/new_thread_id/reason。原因码 SHALL 为 missing_thread 或 thread_unavailable；认证/连接/其它RPC未知错误不得触发换新。显式绑定根与子分道 MUST fail-closed，private 绑定仅在持有 writer 且旧号匹配时更新。

#### Scenario: HRI-S002 不可用且未绑定
- **WHEN** 继承根 resume 报 no rollout found 或 ID/cwd/入场状态不符
- **THEN** 新开根后告警和事件各一次且含旧新号/原因码；不导出原始错误文本

#### Scenario: HRI-S003 绑定或未知错误
- **WHEN** 配置显式绑定或当前为子分道，或校验失败仅为认证/连接未知
- **THEN** 拒收入场，不新建替代线程

### Requirement: HRI-003 未知投递不重放
Helper MUST 保留触发恢复的原信 unknown 语义，不因继承成功或换新而自动重发原信。原信可能已在旧线程执行，SHALL 仅沿用原证据对账，不由线程继承补造入场/终态。

#### Scenario: HRI-S004 恢复后的历史投递
- **WHEN** 原信已处未知且驱动完成替换
- **THEN** 原信不再次 turn/start；新信可在校验后的根执行

### Requirement: HRI-004 broker 根子分道隔离
Helper MUST 从当前 root driver 动态取得恢复根号，不捕获首次根号作为永久恢复目标；broker MAY 仅对当前 root facade 的旧绑定作受控换新，MUST 不允许子 facade 换根、借用根号或放宽普通 bind 的不可变限制。

#### Scenario: HRI-S005 根与子分道分别恢复
- **WHEN** 根已换新后再恢复且子分道仍存活
- **THEN** 根沿用最新根号，子分道仍绑定自己的号；跨分道请求拒绝

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
