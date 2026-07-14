#!/usr/bin/env python3
"""Canary: 验证"body in chunk"对真实多行消息永远不成立。

模拟 Claude Code / Codex 输入框对 bracketed paste 的两种折叠行为：
  1. 多行粘贴 → [Pasted text #1 +45 lines] 占位符（原文不出现）
  2. 长单行 → 按框宽折行 + │ 边框 → clean_output 不还原 → 匹配失败

用法: python3 canary_body_in_chunk.py
"""

import re
import sys

# ---- 取自 cli.py 的 clean_output (精确副本) ----
ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
CTRL_RE = re.compile(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]")


def clean_output(text: str) -> str:
    text = ANSI_RE.sub("", text)
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    text = CTRL_RE.sub("", text)
    lines = [ln.rstrip() for ln in text.splitlines()]
    return "\n".join(ln for ln in lines if ln.strip()).strip()


# ---- 真实形态的 manager 消息 (≥1KB, 多行, 中文) ----
REAL_MESSAGE = """## ⚠️ 2026-07-13 17:35 CST manager 更正 + 新的阻断级发现

### 先更正：16:35 那节 🔴 已过期，作废
我读到的是旧快照。当前盘上的 `cli.py::delivery_pty_status` 已经是"出现→消失"的跃迁判据
（saw_body → saw_body_absent_after → processed），`transcript_scan_tailers` 缓存和
`current_terminal_snapshot` 也都改了。**16:35 那节针对 `terminal_has_response_after` 的指控
不再成立，我撤回。**（这是我今天第 4 次拿活动中的盘当基线，已记教训。）

### 🔴 新的阻断级问题：`body in chunk` 这个匹配对真实消息**永远不成立**

当前判据全部建立在"能在 PTY 文本里看到 body 原文"之上：
has_body = body in chunk          # delivery_pty_status
needle in haystack                # screen_has_text

但我们的真实消息是长文本 + 多行（manager 发给你的每条都 1~3KB）。两件事会让它必然匹配失败：

1. 多行 → 走 bracketed paste ⇒ Claude Code / codex 的输入框把粘贴内容折叠成
   `[Pasted text #1 +45 lines]` 这样的占位符，body 原文根本不出现在屏幕上。

2. 即便单行，长文本在输入框里会按框宽折行，每行还带 │ 边框字符 ⇒
   clean_output 只剥 ANSI/控制字符，不还原折行 ⇒ body in chunk 依然为 False。

后果：saw_body 永远 False ⇒ screen_has_text 也 False ⇒ 状态恒为 unknown
⇒ 超时 ⇒ 重投 ⇒ 重复注入原样回来。我们绕了一圈回到 P1。

为什么你的 canary 过了：PTY_CONFIRM_REALCLI id=PTY3 ... PTY3_OK 是一条短的单行消息，
不触发 paste 折叠、不折行，所以 body in chunk 命中。测试消息不具代表性。
"""

CANARY_MSG = "PTY3_OK"  # 短单行 canary — 能过
CANARY_MSG_SHORT = "hello world"  # 另一条短单行


def simulate_pty_output_after_inject(body: str, cols: int = 140) -> str:
    """模拟 Claude Code 输入框在收到 bracketed paste 后的屏幕内容。"""
    is_multi = "\n" in body or "\r" in body

    if is_multi:
        # 多行 → bracketed paste → CLI 折叠为占位符
        line_count = len(body.splitlines())
        placeholder = f"[Pasted text #1 +{line_count} lines]"
        # Claude Code TUI 中，占位符出现在输入区域，前后是 prompt
        pty = f"""
╭──────────────────────────────────────────────────────────────────────╮
│ > {placeholder}                                                       │
╰──────────────────────────────────────────────────────────────────────╯
"""
    else:
        # 单行长文本 → 按框宽折行 + 边框
        wrapped_lines = []
        remaining = f"> {body}"
        while len(remaining) > cols - 3:
            wrapped_lines.append("│ " + remaining[: cols - 3])
            remaining = remaining[cols - 3 :]
        if remaining:
            wrapped_lines.append("│ " + remaining)
        pty = "\n".join(wrapped_lines)

    return pty


def check_body_in_chunk(body: str, pty_output: str) -> bool:
    """当前判据：body 原文是否出现在 PTY 输出中。"""
    return body in pty_output


def check_body_in_cleaned(body: str, pty_output: str) -> bool:
    """clean_output 后检查——同样失败，因为 clean_output 不还原折行/占位符。"""
    cleaned = clean_output(pty_output)
    return body in cleaned


def main():
    print("=" * 72)
    print("Canary: body-in-chunk 对真实多行消息的命中率")
    print("=" * 72)

    failures = 0
    tests = 0

    for label, body in [
        ("短单行 canary (PTY3_OK)", CANARY_MSG),
        ("短单行 hello", CANARY_MSG_SHORT),
        ("真实 manager 多行消息 (1.8KB, 中文)", REAL_MESSAGE),
    ]:
        tests += 1
        pty = simulate_pty_output_after_inject(body)
        hit_raw = check_body_in_chunk(body, pty)
        hit_cleaned = check_body_in_cleaned(body, pty)

        is_multi = "\n" in body
        size_kb = len(body.encode("utf-8")) / 1024

        print(f"\n--- 测试 {tests}: {label} ---")
        print(f"  大小: {size_kb:.1f}KB, 多行: {is_multi}, 行数: {len(body.splitlines())}")
        print(f"  body in PTY raw:  {hit_raw}  {'✅' if hit_raw else '❌ BLOCKING'}")
        print(f"  body in cleaned:  {hit_cleaned}  {'✅' if hit_cleaned else '❌ BLOCKING'}")

        # 展示 PTY 输出的前 200 字符
        preview = pty.strip()[:200]
        print(f"  PTY 输出预览: {preview!r}")

        if not hit_raw and not hit_cleaned:
            failures += 1

    print("\n" + "=" * 72)
    print(f"结论: {failures}/{tests} 条消息 body-in-chunk 匹配失败")
    if failures > 0:
        print("🔴 阻断: 真实多行消息的 body 原文永远不会出现在 PTY 输出中.")
        print("   原因: bracketed paste 被 CLI 折叠为 [Pasted text #N +K lines]")
        print("   短单行 canary 通过 ≠ 真实场景通过 (测试消息不具代表性)")
        print("=" * 72)
        sys.exit(1)
    else:
        print("✅ 所有消息 body-in-chunk 匹配成功")
        print("=" * 72)
        sys.exit(0)


if __name__ == "__main__":
    main()
