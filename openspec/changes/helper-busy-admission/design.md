## Context
c0777e4b冻结继承线程第一项，追加普通提交不改原交件。已读helper-delivery-evidence上下文与规格、helper-bound-thread/helper-recover-inherit-thread进行中件；时间线沿已归档r56.1→r58→本候选，不据此宣称上线。

## Decisions
- HCD-004：Codex沿用原生read确认旧turn；Claude旧active.terminal_seen未置位即等待，不再180秒后冒险写下一信。仅确认旧回合的驱动分支设置BusyAdmission.waiting；adapter据waiting、submission_started为空、health及transport_recovery_required联合判断豁免。Claude新增真实stdin写出起点，账本期限与两驱动实际写出计时一致。
- HBA-001：每driver持BusyAdmission，waiting/cancelled/submitting状态共用锁。before_submit在任何本信持久提交意图/模型写出前运行，cancel_waiting仅撤销waiting；取消状态保留到driver结束，避免同ID后续调用清空取消。adapter超限后reason=busy_admission_timeout记录transport unknown，不重建健康驱动、不重投。旧worker会退出，取消与提交竞态由同锁判定；失败取消交回原入场规则。
- HBA-002：事件复用adapter._event_q，kind=state_change与data.change，lifecycle=waiting适配现有View等待区判定；不伪造injected/processed，不改变ACK版本与Hub；每30秒一条保持等待可见。View原waiting区新增固定等待文案，不消费任意status_text为HTML；按显示归一键合并前缀，保持canonical输入和权限，处理等待先于primary及迟到等待。复用现有renderer-model-admission-regression.js并登记browser runner。
- HBA-003：故障优先于豁免；Claude显式跟踪_write_started，Codex保留原RPC写入健康判定。写出后无论旧回合状态如何，均无健康等待豁免，unknown仍只读按原证据对账。

## Risks / Trade-offs
忙等待上限从首次驱动证据被adapter观察起算，有20ms轮询精度；不覆盖前置read RPC的一般超时，也不重定义终态watchdog。driver级取消状态只在同进程内存在；跨进程inbox dispatch intent仍阻断自动重放。上限unknown是保守的运维可见状态，即使本机已知取消成功，也不新增自动重试入口。

## Validation
现有test_dispatch_lifecycle模块补两驱动90秒/1800秒边界/故障、原子取消与已提交边界/配置。虚拟协议时钟与临时SQLite，真实driver等待门和adapter；每条语义撤防须红在对应断言。View浏览器26项及逐刀撤防。两种PATH全量run-build-tests、strict、diff检查；具体读数见交件报告。
