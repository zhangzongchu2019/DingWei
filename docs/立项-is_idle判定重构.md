# 立项：`is_idle` 判定重构（策略化）

- **作者**：manager（zzc-manager-2642）
- **日期**：2026-07-13
- **状态**：待排期（不进「会话间消息统一投递」第 1 期）
- **关联**：《客户端接入规范》《设计-provision自动升级通道》

---

## 1. 问题（已坐实）

`is_idle()`（`cli.py:195`）对**所有** CLI 一律用 `last_output_at`（PTY 输出这个代理）：
```python
def is_idle(self):
    if self.last_output_at <= 0: return False   # 从未产生被保留的输出 → 永远"忙"
    return time.time() - self.last_output_at >= settle_seconds
```
`last_output_at` 只在 `_read_loop` 里、且 `clean_output(raw)` **非空**时才更新。TUI（opencode）的 PTY 输出是纯 ANSI 重绘 → 被清成空 → `last_output_at` 恒 0 → **闭环死锁**（从未输出→永远忙→永不注入→永不输出）。

**活体证据**：2026-07-13 reviewer（opencode）整天没回任何消息，收 10 条、入队 5 条、出队 0 条。

**判据不是"哪个 CLI"，是"PTY 输出被 `clean_output` 清成空"**——TUI 类通病，opencode 只是首个实例。claude/codex 靠启动横幅 bump `last_output_at`，是**"碰巧能用"**。

---

## 2. 根因

**用 PTY 字节流去猜一个应用层状态。** PTY 输出对 TUI 是重绘噪音，对文本 CLI 才碰巧相关。

**正解**：每种 CLI 用它**真正的忙闲信号源**，`is_idle` 不该硬编码一种。

---

## 3. 方案：`is_idle` 策略化

`CLIProfile` 增加 `idle_strategy`，`is_idle()` 按 profile 分派（忙闲源优劣序见接入规范）：

| strategy | 判定 | 适配 |
|---|---|---|
| `stream_json_event` | 最近一条 stream-json 事件是 `done`/`result` = 闲 | headless CLI（Qoder/CodeBuddy） |
| `process_alive` | 子进程 `poll() is not None` = 闲 | headless（不用流式时） |
| `transcript` | transcript 最后一轮 assistant 且完成 = 闲 | claude/codex |
| `opencode_db` | db 无进行中 turn = 闲 | opencode |
| ~~`pty_output`~~ | 现有 `last_output_at`，**仅限确认输出实文本的 CLI**；无源 CLI 禁用 | 存量兜底，逐步淘汰 |

---

## 4. 本次范围（收窄——不做通用探活）

> **张宗楚 2026-07-13 拍板"未验证客户端不接入" ⇒ "未知客户端"类别取消 ⇒ 不需要通用探活自愈机制。** 只需给"已验证支持的 CLI"配对策略。

**必做**：
1. **修 opencode**：`is_idle` 用 `opencode_db` 源（`collect_opencode_db_reply` 已在读这个库，复用其查询）。→ 消除《接入规范》记录的存量违规。
2. **禁用 aider/cline/gemini**：`CLI_PROFILES` 移除或标 disabled（无 `output_source`，禁止裸接）。
3. **claude/codex 明确化**：从"碰巧靠 PTY"改注为 `transcript` strategy（或至少补测确认 PTY 兜底对它们安全），登记准入清单。

**不做**（被准入规则取消）：
- ❌ 通用定时探活自愈（往未知 CLI 注入探测逼输出）
- ❌ 绕过队列直写 PTY 的探针

**关联但独立立项**：`stream_json_event` / `process_alive` 策略 = headless driver 的一部分 → 见 backlog「headless JSON driver」，随 Qoder/CodeBuddy 接入一起做。

---

## 5. 验收

- [ ] opencode 会话：CLI 启动后**从不产生输出**，消息仍能在 CLI 真正空闲时被注入（不再死锁）。
- [ ] opencode 会话：CLI **真正在忙**（有进行中 turn）时，消息**不被注入打断**。
- [ ] claude/codex：回归——原有行为不变。
- [ ] `CLI_PROFILES` 中无 `output_source=None` 的启用条目。
- [ ] **canary 验证**：给一个原本会死锁的 opencode 会话发消息，`pending` 不再单调增长、无 `ALERT_DEADLOCK_SUSPECT`。

---

## 6. 流程

设计（本文）→ 设计评审（shenceclaude）→ 开发（dev55）→ 代码复核（reviewer）→ 真机验收（tester）→ 部署（manager）。
**部署方式**：`0.1.2.5` 全网升级后，此修复用 **provision `update_self` 远程推**（含推给正在死锁的会话——provision 不受 `is_idle` 死锁阻塞，见《设计-provision自动升级通道》§3）。
