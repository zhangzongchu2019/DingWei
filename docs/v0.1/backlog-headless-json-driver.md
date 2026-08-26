# Backlog：通用 headless JSON driver

- **作者**：manager（zzc-manager-2642）／**日期**：2026-07-13／**状态**：backlog
- **关联**：《客户端接入规范》《立项-is_idle判定重构》《设计-provision自动升级通道》

## 1. 是什么
给 helper 增加 **headless/oneshot** 驱动模型：每条消息 = 一次 `cli -p --output-format stream-json --dangerously-skip-permissions` 子进程，stdin 送消息、stdout 读结构化 JSON、进程退出=完成。与现有「长期交互 PTY + 注入」driver 并存，按 profile 选。

## 2. 为什么
- **一个 driver 适配一代 CLI**：`-p --output-format json` 是 claude code 开创、这一代通用约定（Qoder/CodeBuddy 选项几乎逐字一致）。
- **天然规避 is_idle 死锁**：忙闲靠 stream-json 事件 / 进程存活，不碰 `last_output_at` 闭环。
- **中断简单**：kill 子进程即可，不需 Ctrl-C 抢占。

## 3. 实现要点（详见《客户端接入规范》§5.6）
全双工管道 `stdin=PIPE`+`stdout=PIPE`+`stderr=STDOUT`（合并防死锁）；后台线程 `readline()`，绝不用 `communicate()`；行缓冲 `bufsize=1`，块缓冲用 `-u`/`stdbuf -oL`；状态机解析 stream-json `type`（thinking/tool_call/assistant=忙，done/result=闲）+ `psutil` 查健康。🔴 `--dangerously-skip-permissions` 的信任边界：确认 CLI 跑在预期沙箱/工作目录内。

## 4. 🔴 必须一起解决 A：多轮上下文续接
headless 每条消息独立进程 → 无状态。验证目标 CLI 的 `--resume`/`--continue`/`--session-id`；session id 存哪/怎么管/会不会失效。**无续接机制 = 每条消息失忆 → 比 is_idle 更根本。**

## 5. 🔴 必须一起解决 B：web-view 范式切换（张宗楚 2026-07-13 提出）
**冲突（已核实）**：web-view 现镜像 raw PTY 终端画面（`cli.py:526` `_read_loop`→`terminal_q`→view；网页输入 `write_terminal_input` 直写 PTY）。headless 无交互式 PTY → 无终端画面 → **web-view 黑屏**。
**⇒ 不能盲目全迁 headless。按会话用途分**：需人盯屏/接管 → 交互 PTY（web-view 终端镜像）；纯后台自动化 → headless。
**headless 会话的 web-view 换范式**：镜像 raw PTY → 渲染对话流（用 stream-json 事件渲染【消息/思考/工具/回复/完成】，比原始 TUI 更清晰）；网页输入从「直写 PTY」→「作为一条新消息发给 CLI」。代价：重做 web-view 渲染层+输入路径。

## 6. 范围与依赖
提供 is_idle 策略 `stream_json_event`/`process_alive`；首个目标 Qoder、CodeBuddy（走接入规范验证清单）；不阻塞也不被会话间投递三期阻塞；部署靠 provision `update_self` 远程推（前提 0.1.2.5 全网升级已完成）。
