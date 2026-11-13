## ADDED Requirements

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
