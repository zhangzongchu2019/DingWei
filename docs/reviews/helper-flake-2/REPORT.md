# S-HELPER-FLAKE-2 复现与测试修订报告

状态：完成。本报告仅覆盖 helper 测试与文档，不改产品文件。

## 任务与基线

- 基线：`501e5b8a`；任务分支：`togpt60lunazzc/helper-flake-2`。
- 来源：qwen `browser-flake-windows-v2`，tip `415b9bf5`，其三轮完整门日志位于该分支 `docs/reviews/browser-flake-windows/evidence-v2`。
- 本席命令：`env -i HOME=/home/dev PATH=/tmp/lockvenv/bin:/usr/bin:/bin scripts/run-build-tests.sh`。完整门两个 profile 并行执行；每个 profile 的 helper suite 计数须为 1270/1270，最终标记 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。

## 复现读数

qwen 三轮完整门中，每个 profile 对每个目标用例各执行一次，共 6 个机会。其读数如下：

| 用例 | qwen 失败／机会 | 失败率 | 失败条件 |
|---|---:|---:|---|
| `test_claude_stream_json_user_frame_boundary_counts_zero_one_two` | 2/6 | 33% | 第二业务帧仍带 GUIDE 前缀；另一轮同断言失败并伴随清理错误 |
| `test_delivery_worker_high_frequency_busy_window_processes_all` | 1/6 | 17% | processed 6 != 8 |
| `test_delivery_worker_recovers_when_injection_failure_turns_busy` | 1/6 | 17% | 注入失败后状态未到 processed |
| `test_slow_creation_does_not_block_parent_lifecycle_seal` | 1/6 | 17% | 0.2 秒 join 内生命周期封口未结束 |

本席在未经修订的 `501e5b8a` 上，四轮完整门均 rc=0；两 profile 每轮均为 1270/1270。每格累计 0/8（Claude 4 次、Codex 4 次）。qwen 的失败与本席 0/8 并列报告；本席样本不能排除低频抖动。第五轮启动后因采样计划收敛而在 profile 运行初期终止，不纳入样本。

完整门时间（UTC）与原始日志 SHA256：

| 轮次 | 开始～结束 | rc | 原始日志 SHA256 |
|---|---|---:|---|
| 1 | 00:54:58～01:04:42 | 0 | `74677b053c8790753769f7e81090fa9103caa6144448ee5ce427775706bb52bf` |
| 2 | 01:04:42～01:14:09 | 0 | `a68b9a4e2e091134003f75f9c7ec7327a8c61c129b53bbc0962b4cd0859d9f94` |
| 3 | 01:14:09～01:23:35 | 0 | `c308ac120c7665363149ced582bba5208033088a3870317b70345488315498eb` |
| 4 | 01:30:09～01:39:37 | 0 | `e835b65a9d2632e396ac06e539aa6fff31660ced2fc51443949aeb055f67a0dd` |

原始基线完整日志保存在执行机 `/tmp/helper-flake-2-evidence/full-gates/`，未将大日志纳入 Git。

## 定因与修订

1. `test_claude_stream_json_user_frame_boundary_counts_zero_one_two`：首业务帧被 adapter 接受后，`admitted` callback 才异步调用 `guide_store.consume`。测试只等 Claude stdin 第一帧写入，随即手动标记 terminal 并发送第二业务帧；当 admission worker 尚未提交 consumed 状态时，第二帧仍会合并一次性 GUIDE。修订在第二帧前等待 store 明确进入 `consumed`，不改变第二帧必须为 `SECOND` 的业务断言。
2. `test_delivery_worker_high_frequency_busy_window_processes_all`：固定 50ms 轮询和 6 秒总窗把 worker 的 busy 重试节奏、fake adapter 跨线程调用与测试调度耦合；轮询可能漏过 ACK 状态变化并在第八件完成前截断。修订用 FakeWS `asyncio.Event` 通知发送状态变化，仍要求 8 次 processed、原 handled ID 集合/顺序和 busy ACK。
3. `test_delivery_worker_recovers_when_injection_failure_turns_busy`：1.5 秒两段轮询在 fake idle 翻转、busy ACK、worker 再处理之间竞争。改为按 fake websocket 可观察状态变化等待 busy 与 processed；断言仍要求既有处理结果和状态序列。
4. `test_slow_creation_does_not_block_parent_lifecycle_seal`：被测实现先在锁内登记 submitted，再释放锁并执行慢 `thread/start` RPC；产品锁范围正确。旧测试以 `closer.join(.2)` 把 200ms 调度窗口当作生命周期锁断言，偶发产生假失败。改为 `close_finished` Event，最长 2 秒；仍验证 parent 进入 draining、reason 为 `parent_closed`、节点未注册且只发起一次 thread/start。

只改上述三份测试文件；产品文件未变。事件等待均有有限超时，原业务结果断言不放宽。

## 变异判别力

四把均在本机隔离临时改动产品文件后，仅运行对应单测；每把运行后恢复原文件，产品树无改动。变异日志位于执行机 `/tmp/helper-flake-2/mutations/`，未纳入 Git。

| 格 | 产品真坏变异 | 结果 |
|---|---|---|
| GUIDE 消费 | 移除 admission 成功后的 `guide_store.consume(guide_claim)` | rc=1；`test_claude_stream_json_user_frame_boundary_counts_zero_one_two` 在预期的 guide 状态 `consumed` 条件上失败，2.15s。 |
| 高频 busy | 移除忙态重试时 `delivery_inbox.release(delivery_id)` | rc=1；同格在 `processed == 8` 业务断言失败，实得 7，30.06s。 |
| 注入失败恢复 | 把忙态判定强制设为 `False`，令瞬态 busy 被终结为 failed | rc=1；`test_delivery_worker_recovers_when_injection_failure_turns_busy` 在 ACK 状态应为 `busy` 的业务断言失败，实得 `failed`，40.03s。 |
| 子线程封口 | 将慢 `thread/start` RPC 移进生命周期锁 | rc=1；`test_slow_creation_does_not_block_parent_lifecycle_seal` 在 `NATIVE_CREATE_BLOCKED_LIFECYCLE_SEAL` 断言失败，2.60s。 |

变异均在测试判据上转红；超时只作受限等待，判定依赖的业务状态仍为原断言。变异原始日志保存在执行机 `/tmp/helper-flake-2-evidence/mutations/`。

## 修订后全量门

- 命令：`env -i HOME=/home/dev PATH=/tmp/lockvenv/bin:/usr/bin:/bin scripts/run-build-tests.sh`。
- UTC：2026-11-18 01:44:46～01:54:12；rc=0。
- Claude 与 Codex profile 并行，各 1270/1270（97 文件，分组 1261+9）；最终 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。
- 四个目标格在两个 profile 都通过；完整原始日志 SHA256：`1cb5677ba88af8a97f2a11fc08815496e449762ff494cc6144deb6d333b5cee4`，保存在 `/tmp/helper-flake-2-evidence/final-gate.log`。

## 固定等待／跨线程状态静态扫描

以 AST 扫描 `tools/sessionhelper/tests/test_*.py`：96 个测试文件中，54 个测试方法包含正数常量 sleep，共 68 个调用点。此为筛查数量，不表示 54 格均有缺陷。人工复核优先级：

| 风险 | 格／坐标 | 风险原因与处理 |
|---|---|---|
| 高 | `test_guide_staging.py::test_claude_stream_json_user_frame_boundary_counts_zero_one_two` | 第一帧后状态回写由异步 admission 完成；原测试立即发送第二帧，改为等待 `guide_store` 的 `consumed` 可观察状态。 |
| 高 | `test_sessionhelper.py::test_delivery_worker_high_frequency_busy_window_processes_all` | 短轮询窗口观察 worker 与 fake adapter 的跨线程处理计数；改用 ACK 事件唤醒，仍要求 8 次 processed、原处理序列与 busy ACK。 |
| 高 | `test_sessionhelper.py::test_delivery_worker_recovers_when_injection_failure_turns_busy` | 原 1.5 秒轮询与状态检查竞争；改用 FakeWS 状态变化事件，仍要求 busy 后 processed 及原处理结果。 |
| 高 | `test_sub_agent_helper.py::test_slow_creation_does_not_block_parent_lifecycle_seal` | 0.2 秒 join 将调度延迟误判为锁阻塞；改等关闭完成事件，仍保留 2 秒硬上限和生命周期断言。 |
| 中 | `test_guide_staging.py:379-381 settle_guide_only` | 队列为空后固定静置 100ms 判断无额外 guide；若并发调度延迟，可能漏掉迟到 enqueue。建议后续改成显式 admission/enqueue 计数屏障。 |
| 中 | `test_sessionhelper.py:1093-1102, 1288-1292, 1334-1338, 1669-1675, 1747-1751, 1826-1830` | worker ACK、注入、无重复事件依赖短轮询或固定 quiet period；建议逐格引入事件/队列排空屏障，保留“不多不少”的业务断言。 |
| 中 | `test_sessionhelper.py:1359, 1399` | 用固定 sleep 模拟串行耗时与迟到回复；应保留模拟延迟，但观测侧可用 barrier/event 避免与固定期限耦合。 |
| 中 | `test_driver_failure_faces.py:168-170, 254-257, 278-281, 293-297` | 50ms 轮询收集异步失败事件，另有 1.5 秒收齐窗口；建议使用事件队列/明确 deadline 与 terminal 状态同步。 |
| 中 | `test_event_bridge_replay.py` reconnect/resubscribe 场景 | 固定 sleep 推断退避、重连或重放结果；需区分有意覆盖的定时策略与调度等待，优先暴露可观察的连接/回放状态。 |
| 中 | `test_cli_exit_visible.py` 多个启动、退出和 respawn 格 | 有固定等待与 subprocess 状态断言；部分是在模拟真实进程启动/重启，建议用 readiness 文件/IPC 状态代替睡眠后检查。 |
| 低 | `test_session_heartbeat.py:157-164` | 两次 10ms sleep 后检查 waiter 状态，存在轻微调度敏感；可改为 pending-count/event；9 秒慢处理器为预算语义模拟，保留。 |
| 低 | `test_reliable_delivery_reply.py:237-241` | 1.5 秒 quiet window 用于查重投／重复回复；可用可控时钟或明确重试时钟事件替代。 |
| 低／有意 | `test_spec_ipc_timeout.py`、`test_sub_agent_helper.py:535` 等 | 长 sleep 或超时等待用于验证产品超时、断线存活语义；不是单纯线程调度同步，除非环境测试预算需要，不建议机械删除。 |

除本次四格外，本单只列风险，不作修改。
