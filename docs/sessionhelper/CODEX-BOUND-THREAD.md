# Codex 根会话绑定

适用 r58 helper driver 模式，配置项 `SH_CODEX_BOUND_THREAD_ID`。只选择根 Codex thread；Hub 仍投递席位，子 Agent 分道仍由 M2 运行时选择各自 thread。空值或未设置保持原行为，Claude/OpenCode 不使用此项。

## 取得目标 ID
在**目标 Codex 会话内**执行 `printenv CODEX_THREAD_ID`，记录完整值；不要取另一个新终端或 helper 父进程的值。核实这正是处理任务、持有目标工作树的会话。配置 `SH_CLI_CWD` 须与该 thread 的 cwd 一致，并使用可读该 thread 的原 CODEX_HOME、同用户和既有 app-server。helper 会核 read/resume 返回的 ID、cwd、状态与 canAcceptDirectInput；配置值不是绕过校验的凭据。

## 设置与换绑
1. 停止该席位 helper，按现有服务管理方式确认旧 helper 已退出、writer 已释放；保留账本、凭据和 owner 现场。
2. 在该 helper 原有启动配置（受控 env 文件或服务 Environment）设置 `SH_CODEX_BOUND_THREAD_ID=<完整目标ID>`，并核对原 CODEX_HOME 与 SH_CLI_CWD。仅 export 到无关交互 shell 不会修改既有服务环境。
3. 启动同一 helper。首投递会 read，必要时 resume **同一 ID**，成功后 turn/start 只能指向该 ID。
4. legacy 模式换绑：再次停 helper → 修改配置 → 启动。不热改、不同时启动第二个 helper。删除该配置恢复原有未设置行为；历史账本不删除。

private 模式：配置与当前 binding.json 相同可启；文件尚无绑定时，经校验成功后才保存指定 ID。若已有不同 ID，启动输出 `bound_thread_conflict: SH_CODEX_BOUND_THREAD_ID=...; binding.json=...` 并停止启动，不覆盖旧文件。此时保留现场并按独立 private endpoint 维护流程处理旧绑定；本功能**不提供强制换绑/自动清除命令**。不得靠删除整个状态目录、账本或 owner 文件绕过冲突。停 helper 后只改配置不足以覆盖 private 的旧绑定，这个拒绝是有意保留的保护。

## 故障和分道
thread 不存在、无法恢复、cwd 不同或拒绝直接输入时，该投递为 failed，detail/事件 reason 含 `bound_thread_unavailable`；不会 thread/start 新建会话。修复目标会话或配置后使用新的显式投递；相同信封的失败回执持久幂等，不自动重放。已发出的 turn/start 结果不明仍为 `model_admission_unknown`，不冒称未入场。

绑定既有根 thread 不会安装 M2 dynamic tools。原来没有子 Agent 控制工具的根仍须按 M2 规则重开具备工具的根；已经记录工具能力的根可照常创建子 lane，子 lane 不继承此配置。试点和部署另由 prodops 按 r58 合批流程实施；本变更本身不启动或重启任何在线席位。
