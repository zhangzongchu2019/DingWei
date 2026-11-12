## Context
Hub 只寻址席位，根 helper 的首次投递原先可 thread/start。M2 子 lane 已有运行时明确的 bound_thread_id，不能从根配置继承。

## Decisions
- HBT-001：Config.codex_bound_thread_id 从 SH_CODEX_BOUND_THREAD_ID 读取并去首尾空白。根工厂仅非空时传 bound_thread_id 与 require_bound_thread=True；driver 保存不可变根 ID。_ensure_thread 的根分支只 read/resume 校验，绝不执行 thread/start；原 _validate_bound_thread 的 ID、cwd、状态、canAcceptDirectInput 校验保留。兼容 alias 不能替换根 ID。
- HBT-001：已知未入场失败沿用清洗后的错误元数据，前缀为 bound_thread_unavailable；adapter 显示相同原因并 record_rejected，不发 injected/admitted ACK。发出 turn/start 后结果未知仍是 model_admission_unknown，不因绑定配置改变证据规则。失败回执持久幂等，不自动重新提交。
- HBT-002：private 在取得唯一 writer 后、连接 app transport 前读取 binding.json，若非空且不同，ValueError 的 bound_thread_conflict 文案显示配置 ID 与文件 ID，释放 writer，不覆盖。匹配或无绑定时继续；新绑定仅在现有校验通过后 save_thread。startup 失败由原启动路径向上抛出。
- HBT-003：配置只在根工厂消费，generic driver 不读该环境变量。M2 重建根 driver 时保留 require_bound_thread，子 driver 不传该要求；运行时 thread ID、native tools 记录、broker 准入白名单完全沿用。

## Risks / Trade-offs
既有 thread 不保证已安装 M2 控制工具；保留旧根工具不可用的拒绝行为。private 配置与旧文件不同会明确停机；本功能不提供强制覆盖或自动迁移接口。具体人工步骤见 docs/sessionhelper/CODEX-BOUND-THREAD.md。

## Validation
a/b/d/g 的假 app-server 请求流断言、c 的原有单测、撤掉 fail-closed 分支的变异、双 profile canonical 门；原始日志与可复跑变异脚本放 docs/reviews/helper-bound-thread。
