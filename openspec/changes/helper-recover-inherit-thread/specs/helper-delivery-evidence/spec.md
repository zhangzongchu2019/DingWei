## ADDED Requirements

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
