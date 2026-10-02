"""CLI 子进程退出必须可见（组长 R-B12-CODEX-PKG 第 2 条）。

B12：Codex 打一屏（含就绪标记）后即退出，helper 无任何日志、仍把死进程当就绪，往 PTY 注入 3 次、142 秒后以
「注入未被 CLI 接收」收场、投递照回 processed。加固后：
1 子进程退出（任何原因）即打「[cli] exited code=… tail=…」（tail＝最后几行屏幕输出，脱敏）；
2 就绪只在子进程还活着时算数；
3 持久投递（handle_delivery）遇 CLI 起不来／已退出 ⇒ 抛 CLIUnavailable（worker 据此回 failed、可重试），不再返回假装注入的文字；
  实时路径 handle() 仍以文字给出原因（与原行为同）。
需要 pexpect（helper 运行依赖）。
"""
import contextlib
import io
import os
import tempfile
import time
import unittest

try:
    import pexpect  # noqa: F401
except ImportError:  # pragma: no cover
    pexpect = None

if pexpect is not None:
    from sessionhelper.cli import CLIAdapter, CLIUnavailable
    from sessionhelper.config import load_config

BASE_ENV = {"SH_SESSION_NAME": "seat", "SH_KEY_ID": "u_owner", "SH_SECRET": "s"}

_EXIT_NOW = """#!/usr/bin/env python3
import sys
sys.stdout.write("Welcome to fake codex \\u203a\\nError: no complete local package sk-THISISAFAKEKEY1234567890\\n"); sys.stdout.flush()
sys.exit(7)
"""
_EXIT_LATER = """#!/usr/bin/env python3
import sys, time
sys.stdout.write("Welcome to fake codex \\u203a\\n"); sys.stdout.flush()
time.sleep(4)
sys.stdout.write("bye from fake codex\\n"); sys.stdout.flush()
sys.exit(5)
"""


@unittest.skipUnless(pexpect is not None, "needs pexpect")
class CLIExitVisible(unittest.TestCase):
    def _cli(self, src: str) -> str:
        d = tempfile.mkdtemp(prefix="sh-cli-exit-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write(src)
        os.chmod(path, 0o755)
        return path

    def _adapter(self, cmd: str) -> "CLIAdapter":
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=cmd, SH_CLI_CWD=tempfile.gettempdir(),
                               SH_CLI_READY_TIMEOUT="20", SH_CLI_LAUNCH_RETRIES="2"))
        a = CLIAdapter(cfg)
        self.addCleanup(a.stop)
        return a

    def test_exit_during_startup_is_logged_with_code_and_redacted_tail(self):
        a = self._adapter(self._cli(_EXIT_NOW))
        out = io.StringIO()
        t0 = time.time()
        with contextlib.redirect_stdout(out):
            ok = a.start()
        log = out.getvalue()
        self.assertFalse(ok)
        self.assertLess(time.time() - t0, 10, "must not wait out cli_ready_timeout once the child is gone")
        self.assertIn("[cli] exited code=7 tail=", log)
        self.assertIn("no complete local package", log)
        self.assertNotIn("sk-THISISAFAKEKEY1234567890", log)
        self.assertIn("<redacted>", log)

    def test_durable_delivery_fails_explicitly_when_cli_cannot_stay_up(self):
        a = self._adapter(self._cli(_EXIT_NOW))
        injected = []
        with contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(CLIUnavailable) as cm:
                a.handle_delivery({"body": "task"}, on_injected=lambda: injected.append(1))
        self.assertIn("code=7", str(cm.exception))
        self.assertEqual(injected, [], "nothing may be reported as injected")
        # 实时路径仍以文字给出原因
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertIn("CLI未就绪", a.handle({"body": "task"}))

    def test_exit_after_ready_clears_ready_and_is_logged(self):
        a = self._adapter(self._cli(_EXIT_LATER))
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertTrue(a.start())
            self.assertTrue(a.ready.is_set())
            deadline = time.time() + 15
            while time.time() < deadline and "[cli] exited" not in out.getvalue():
                time.sleep(0.1)
        log = out.getvalue()
        self.assertIn("[cli] exited code=5 tail=", log)
        self.assertIn("bye from fake codex", log)
        self.assertFalse(a.ready.is_set(), "a dead child must not stay 'ready'")
        self.assertTrue(a.is_idle(), "a dead CLI is not 'busy': deliveries must reach handle_delivery and fail explicitly")

    def test_startup_exit_backs_off_instead_of_respawning_in_a_loop(self):
        a = self._adapter(self._cli(_EXIT_NOW))
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertFalse(a.start())
            attempts = a.launch_attempts
            self.assertFalse(a.start(), "still backing off")
        self.assertEqual(a.launch_attempts, attempts, "no respawn inside the backoff window")


_BANNER_PROMPT_EXIT = """#!/usr/bin/env python3
import sys
sys.stdout.write("OpenAI Codex (v0.159.2)\\n\\u203a \\nError: no complete local package\\n"); sys.stdout.flush()
sys.exit(1)
"""
_BANNER_STAYS = """#!/usr/bin/env python3
import sys, time
sys.stdout.write("OpenAI Codex (v0.159.2)\\nloading...\\n"); sys.stdout.flush()
time.sleep(30)
"""
_BANNER_THEN_PROMPT = """#!/usr/bin/env python3
import sys, time
sys.stdout.write("OpenAI Codex (v0.159.2)\\n"); sys.stdout.flush()
time.sleep(0.5)
sys.stdout.write("\\u203a "); sys.stdout.flush()
time.sleep(30)
"""
_CUSTOM_PROMPT = """#!/usr/bin/env python3
import sys, time
sys.stdout.write("booting\\nREADY-NOW\\n"); sys.stdout.flush()
time.sleep(30)
"""


@unittest.skipUnless(pexpect is not None, "needs pexpect")
class CodexReadiness(unittest.TestCase):
    """组长 R-B12-CODEX-PKG 修订第 4 条：横幅里的「Codex」字样不算就绪；就绪＝子进程活着且出现输入提示符。"""

    def _adapter(self, src: str, **extra) -> "CLIAdapter":
        d = tempfile.mkdtemp(prefix="sh-cli-ready-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write(src)
        os.chmod(path, 0o755)
        env = dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=tempfile.gettempdir(),
                   SH_CLI_READY_TIMEOUT="4", SH_CLI_LAUNCH_RETRIES="1", **extra)
        a = CLIAdapter(load_config(env))
        self.addCleanup(a.stop)
        sets = []
        orig = a.ready.set
        a.ready.set = lambda: (sets.append(time.time()), orig())[1]
        a._ready_sets = sets
        return a

    def test_banner_with_prompt_glyph_then_exit_is_never_ready(self):
        a = self._adapter(_BANNER_PROMPT_EXIT)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertFalse(a.start())
        self.assertEqual(a._ready_sets, [], "an exiting process must never be marked ready")

    def test_banner_alone_is_not_ready(self):
        a = self._adapter(_BANNER_STAYS)
        with contextlib.redirect_stdout(io.StringIO()):
            a.start()  # 等满 4 秒未就绪；子进程活着则保留（原行为）
            self.assertFalse(a.ready.is_set(), "the 'Codex' banner is not an input prompt")
            with self.assertRaises(CLIUnavailable):
                a.handle_delivery({"body": "task"}, on_injected=lambda: None)

    def test_prompt_after_banner_is_ready(self):
        a = self._adapter(_BANNER_THEN_PROMPT)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(a.start())
        self.assertTrue(a.ready.is_set())

    def test_ready_patterns_override(self):
        a = self._adapter(_CUSTOM_PROMPT, SH_CLI_READY_PATTERNS="READY-NOW")
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(a.start())
        self.assertTrue(a.ready.is_set())


if __name__ == "__main__":
    unittest.main()
