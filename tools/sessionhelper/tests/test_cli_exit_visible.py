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
    from sessionhelper.app import delivery_failure_ack_fields
    from sessionhelper.cli import CLIAdapter, CLIInjectionNotReceived, CLIUnavailable, _squash
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
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            with self.assertRaises(CLIUnavailable) as cm:
                a.handle_delivery({"body": "task"}, on_injected=lambda: injected.append(1))
        self.assertIn("code=7", str(cm.exception))
        self.assertIn("[cli] delivery failed reason=cli_unavailable retryable=true", out.getvalue())
        self.assertEqual(delivery_failure_ack_fields(cm.exception), ("cli_unavailable", True))
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

    def test_exit_tail_redacts_key_value_secrets(self):
        # 复验 tosonnet55tc2k：短串或「键名=值」形态也要遮
        a = self._adapter(self._cli("""#!/usr/bin/env python3
import sys
sys.stdout.write("OPENAI_API_KEY=abcd1234efgh5678\\naccess_token: tok-test-invalid-b12\\nrefresh_token=rt-test-invalid-b12\\n")
sys.stdout.flush()
sys.exit(3)
"""))
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            a.start()
        log = out.getvalue()
        self.assertIn("[cli] exited code=3", log)
        for leaked in ("abcd1234efgh5678", "tok-test-invalid-b12", "rt-test-invalid-b12"):
            self.assertNotIn(leaked, log)
        self.assertIn("OPENAI_API_KEY=<redacted>", log)
        self.assertIn("access_token: <redacted>", log)

    def test_start_checks_liveness_even_when_ready_is_set(self):
        # 就绪已置但子进程已死、读线程尚未察觉（这里不起读线程）：start() 必须先记退出、不得直接返回真
        a = self._adapter(self._cli(_EXIT_NOW))
        dead = pexpect.spawn("/bin/sh", ["-c", "exit 4"], encoding="utf-8")
        deadline = time.time() + 5
        while dead.isalive() and time.time() < deadline:
            time.sleep(0.05)
        a.child = dead
        a.ready.set()
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertFalse(a.start(), "a dead child must not pass as ready")
        self.assertIn("[cli] exited code=4", out.getvalue())

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
sys.stdout.write(">_ OpenAI Codex (v0.159.2)\\nloading...\\n"); sys.stdout.flush()
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


_PROMPT_NO_TRANSCRIPT = """#!/usr/bin/env python3
import sys
sys.stdout.write("\\u203a "); sys.stdout.flush()
for line in sys.stdin:
    sys.stdout.write("\\u203a "); sys.stdout.flush()
"""


@unittest.skipUnless(pexpect is not None, "needs pexpect")
class InjectionNotReceivedFails(unittest.TestCase):
    """组长 R-B12-CODEX-PKG 第 4 条第 2 项：「注入未被接收」以失败结束（Hub 记 failed）、原因进 helper 日志；
    不可重试——正文已敲进 CLI，重投会重复。"""

    def test_no_session_file_fails_not_processed(self):
        d = tempfile.mkdtemp(prefix="sh-cli-nouser-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write(_PROMPT_NO_TRANSCRIPT)
        os.chmod(path, 0o755)
        codex_home = os.path.join(d, "codex-home")  # 空目录：没有任何会话文件
        os.makedirs(codex_home)
        env = dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                   SH_CLI_READY_TIMEOUT="10", SH_CLI_REPLY_WAIT="1", SH_CLI_USER_ACK_WAIT="1")
        old = os.environ.get("CODEX_HOME")
        os.environ["CODEX_HOME"] = codex_home
        self.addCleanup(lambda: os.environ.__setitem__("CODEX_HOME", old) if old is not None else os.environ.pop("CODEX_HOME", None))
        a = CLIAdapter(load_config(env))
        self.addCleanup(a.stop)
        out = io.StringIO()
        injected = []
        with contextlib.redirect_stdout(out):
            with self.assertRaises(CLIInjectionNotReceived) as cm:
                a.handle_delivery({"body": "task"}, on_injected=lambda: injected.append(1))
        self.assertIn("注入未被 CLI 接收", str(cm.exception))
        self.assertTrue(injected, "the text was written into the CLI")
        self.assertIn("[cli] delivery failed reason=cli_injection_not_received retryable=false", out.getvalue())
        self.assertEqual(delivery_failure_ack_fields(cm.exception), ("cli_injection_not_received", False))
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertIn("注入未被 CLI 接收", a.handle({"body": "task"}))  # 实时路径仍以文字给原因

    def test_generic_failure_keeps_old_ack_fields(self):
        self.assertEqual(delivery_failure_ack_fields(RuntimeError("x")), ("cli_injection_failed", True))


_FIXTURE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixtures", "codex-0.159.2-login-menu.pty.txt")
_REPLAY_SCREEN = """#!/usr/bin/env python3
import sys, time
data = open(%r, "rb").read()
if %r:
    data = data.split(b"Welcome", 1)[0]  # 对照：只留横幅与「› Ask Codex to do anything?」输入框，去掉登录菜单
sys.stdout.buffer.write(data); sys.stdout.flush()
time.sleep(30)
"""


@unittest.skipUnless(pexpect is not None, "needs pexpect")
class CodexLoginMenuNotReady(unittest.TestCase):
    """组长 R-B12-CODEX-PKG 第 4 条补：0.159.2 登录选择菜单（屏上同时画着「› Ask Codex to do anything?」）不得判就绪。
    假 CLI 原样回放 prodops 00e5020e 的 60 秒抓屏（evidence/codex-nodaemon-probe，已脱敏），进程一直活着。"""

    def _adapter(self, menu_removed: bool):
        d = tempfile.mkdtemp(prefix="sh-cli-menu-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write(_REPLAY_SCREEN % (_FIXTURE, menu_removed))
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="4", SH_CLI_LAUNCH_RETRIES="1")))
        self.addCleanup(a.stop)
        return a

    def test_login_menu_is_not_ready(self):
        a = self._adapter(menu_removed=False)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            a.start()
            self.assertFalse(a.ready.is_set(), "login selection menu must not count as ready")
            with self.assertRaises(CLIUnavailable) as cm:
                a.handle_delivery({"body": "task"}, on_injected=lambda: None)
        self.assertIn("[cli] blocked at screen:", out.getvalue())
        self.assertIn("拦路界面", str(cm.exception))

    def test_partial_redraw_with_prompt_does_not_unblock(self):
        # 菜单之后再单独送来一块只含「›」的局部重绘：本子进程仍不得判就绪
        d = tempfile.mkdtemp(prefix="sh-cli-menu2-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write("""#!/usr/bin/env python3
import sys, time
sys.stdout.write("Welcome to Codex\\n1. Sign in with ChatGPT\\nPress enter to continue\\n"); sys.stdout.flush()
time.sleep(2.5)
sys.stdout.write("\\u203a "); sys.stdout.flush()
time.sleep(30)
""")
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="6", SH_CLI_LAUNCH_RETRIES="1")))
        self.addCleanup(a.stop)
        with contextlib.redirect_stdout(io.StringIO()):
            a.start()
        self.assertFalse(a.ready.is_set())

    def _alive_screen(self, text: str, **extra):
        d = tempfile.mkdtemp(prefix="sh-cli-block-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write("#!/usr/bin/env python3\nimport sys, time\nsys.stdout.write(%r); sys.stdout.flush()\ntime.sleep(30)\n" % text)
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="3", SH_CLI_LAUNCH_RETRIES="1", **extra)))
        self.addCleanup(a.stop)
        with contextlib.redirect_stdout(io.StringIO()):
            a.start()
        return a

    def test_trust_prompt_is_blocking(self):
        # 复验 tosonnet55tc2k：目录信任确认屏（Do you trust …）同样不算就绪
        a = self._alive_screen("\u203a Do you trust the files in this folder?\n")
        self.assertFalse(a.ready.is_set())

    def test_block_patterns_can_be_extended_by_env(self):
        # SH_CLI_BLOCK_PATTERNS 在默认拦路标记之外追加
        a = self._alive_screen("\u203a MAINTENANCE WINDOW\n", SH_CLI_BLOCK_PATTERNS="maintenance window")
        self.assertFalse(a.ready.is_set())
        b = self._alive_screen("\u203a MAINTENANCE WINDOW\n")
        self.assertTrue(b.ready.is_set(), "without the extra marker the same screen is a prompt (control)")

    def test_block_markers_match_without_spaces(self):
        # 全屏 TUI 用光标定位代替空格：抓屏里读出来是「3.ProvideyourownAPIkey」
        self.assertIn(_squash("Provide your own API key"), _squash("3.ProvideyourownAPIkey"))

    def test_same_screen_without_menu_is_ready(self):
        a = self._adapter(menu_removed=True)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(a.start())
        self.assertTrue(a.ready.is_set(), "banner + '› Ask Codex…' composer alone is the input prompt")


if __name__ == "__main__":
    unittest.main()
