## Context
根 driver 当前 thread_id 属于运行态，SH_CODEX_BOUND_THREAD_ID 属于显式 fail-closed 策略；二者不能混成同一绑定要求。broker facade 普通绑定不可变，private 保存也禁止覆盖，换新必须单列受控入口。

## Decisions
- HRI-001：adapter 在旧投递 worker 已退出后、停止旧 driver 前后保留其最终根号；新 Codex driver 通过 inherit_recovery_thread 接收提示并使校验缓存失效，首投前验证。
- HRI-002：只对继承提示的未绑定根，在 missing_thread 或 ThreadUnavailable 校验失败时换新；传输与认证错误仍失败。输出固定告警和既有 SessionEvent state_change；只带旧新号及稳定原因码，不泄露异常正文。private 更新使用持 writer 的旧号比较后落盘。
- HRI-003：不改恢复原因、FIFO 隔离、unknown 原信、不重放/证据对账逻辑。
- HRI-004：根 runtime 工厂动态读取当前 root driver；子 lane 不借根号。broker 独立根换新入口先核 facade/旧号，收到成功新号后才改映射；普通 bind 及子分道白名单不变。换新响应未知时保留旧映射并不重试创建。

## Risks / Trade-offs
旧线程仍可能运行原信；继承不等于安全重投。换新成功但private落盘失败应停止入场，不能静默执行新根；相关证据须保留。helper 重启持久恢复不是本单新增能力。新线程创建成功不恢复丢失上下文。状态事件经现有通道上报，不修改Hub。

## Validation
同根read/resume、不可用根告警/事件、显式绑定拒收、broker隔离、unknown幂等；逐判据语义撤防；双PATH全量门、strict、diff检查。故障均在本地合成transport/临时绑定目录，不操作在线席位。
