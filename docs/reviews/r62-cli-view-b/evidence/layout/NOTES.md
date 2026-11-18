# 版式校准取证（S-R62-CLI-VIEW-B 设计期，mock provider，无真凭据）

目的：设计页 §三 版式参照的完成态实证。方法：本地 mock OpenAI 兼容 provider（mock_provider.py，固定短答「收到。」）＋隔离 HOME/CODEX_HOME/workdir＋tmux 直启 TUI（不经 helper）＋capture-pane 周期截屏。提示词与勘察同：「请只回复两个字：收到。不要调用任何工具，不要读写任何文件。」

## 关键发现：两版均已弃 chat wire
- d-0170-chat-rejected／d-0159-chat-rejected：0.170.0 与 0.159.2 显式 `wire_api = "chat"` 均启动即退，错误逐字同：「Error: `wire_api = "chat"` is no longer supported. How to fix: set `wire_api = "responses"`…（discussions/7782）」。
- 推论：勘察 T1/T2 的隔离 config **不含 wire_api 键** ⇒ 走默认＝responses。即勘察两版实际都以 **responses 线**对接 token-plan compatible-mode 端点（该端点实现了 /responses，故模型答出）。勘察 REPORT §3.2「wire_api 默认、chat 兼容端点」表述不准确——端点是 OpenAI 兼容，但 wire 是 responses。勘察分支已冻结不改写，更正记于本件；CLI-A-TUI 单定因时以此为准。

## 完成态读数（responses mock 流）
- d-0170b（0.170.0）：20:10:03Z 起 → 注入 → after-3 流式「• 收到」→ after-5 完成「• 收到。」＋「Worked for <1s • 12:10」＋输入框恢复「› Ask Codex to do anything」；rollout 含 task_complete（1 命中）。
- d-0159b（0.159.2）：20:14:23Z 起 → 同形完成（横幅 v0.159.2，多一行 onboarding 提示「What's the first thing on our napkin sketch?」，状态行不回显末条 prompt）；rollout 含 task_complete（1 命中）。
- 版式四区实证：① 横幅「  >_ OpenAI Codex (vX)」＋workdir 行；② transcript：用户行「› <正文>」、助手行「• <正文>」（流式增量）、回合尾「  Worked for <时长> • <时钟>」；③ 输入区「› Ask Codex to do anything」占位恢复；④ 底状态「<model> default · <workdir> [· 末条 prompt]」＋「← for agents · ? for shortcuts」＋告警指示。
- 工作态（转轮）未及截屏（mock 0.6s 答完，快于 1s 截屏周期）；工作态版式以勘察 T2 真 provider 帧为据（Working 转轮＋任务标题）。

## 中性观察（不预定因）
- 形状良好的 mock responses 流下两版 TUI 均正常收尾渲染。真 provider 直启对照归 S-R62-CLI-A-TUI（排在设计之后），本件不作定因结论。

## 安全
- env -i 白名单；探针 env 仅 MOCK_API_KEY=dummy-layout-only，**真 QWEN_API_KEY 从未进入探针**；交件前以 donor 环境真值对全证据目录扫描＝0 命中。
- codex-home（含 shell_snapshots 环境快照）整体不入 git；入 git 仅截屏/rollout/脚本/时刻戳/驱动日志。rollout 内容为 mock 会话（provider=mock、base_instructions 为 codex 标准段），无平台注入段、无凭据。
- tmux 独立 socket（-L clilayout），不碰在线 tmux；负载单进程级、每臂约 70s；未触在线 helper/daemon/Hub。

## 件清单
- mock_provider.py／layout_probe.sh（可复跑：bash layout_probe.sh <tag> <binary> responses）。
- d-0170b／d-0159b：final.txt（末屏）、after-3.txt（流式）、after-5.txt（完成）、screen-1.txt（横幅）、pane-raw.log（原始 ANSI 流）、rollout-*.jsonl、start/injected/end、driver.log。
- d-0170-chat-rejected／d-0159-chat-rejected：pane-raw.log（chat 弃用错误原文）、screen-1.txt。
