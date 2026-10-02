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

    def test_exit_tail_redacts_auth_scheme_json_and_flags(self):
        # 组长 WORKGROUPS 第十节第 3 条（复验 tosonnet55tc2k 对 b2c91a8a）：Authorization: Bearer 曾把令牌露出；
        # 补 JSON 形态与命令行旗标。各值互不相同，逐个断言不外露。
        a = self._adapter(self._cli("""#!/usr/bin/env python3
import sys
sys.stdout.write("Authorization: Bearer tokA-bearer-1\\n"
                 "authorization=Basic dXNlcjpwYXNzQg\\n"
                 '{"access_token":"tokC-json-3","api_key": "tokD-json-4"}\\n'
                 "codex --api-key tokE-flag-5 exec\\n"
                 "codex --api-key=tokF-flag-6\\n")
sys.stdout.flush()
sys.exit(4)
"""))
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            a.start()
        log = out.getvalue()
        self.assertIn("[cli] exited code=4", log)
        for leaked in ("tokA-bearer-1", "dXNlcjpwYXNzQg", "tokC-json-3", "tokD-json-4", "tokE-flag-5", "tokF-flag-6"):
            self.assertNotIn(leaked, log)
        self.assertIn("Authorization: Bearer <redacted>", log)  # 方案词留着、令牌仍被遮
        self.assertIn("Basic <redacted>", log)
        self.assertIn('"access_token":<redacted>', log)
        self.assertIn("--api-key <redacted> exec", log)

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
            a.start()  # 启动期遇拦路即返回（约 1.5 秒），此时「›」那块还没来
            time.sleep(3)  # 等「›」那块到达并被读到
        self.assertIn("\u203a", a.screen)
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

    def test_trust_folder_and_update_screens_are_blocking(self):
        # 组长 R-B12-CODEX-AUTH-II 补：需回车继续的屏会吞掉第一次注入，不得判就绪
        for text in ("\u203a Trust this folder? (y/n)\n", "\u203a Update available: 0.160.0 -> press enter\n"):
            a = self._alive_screen(text)
            self.assertFalse(a.ready.is_set(), text)

    def test_block_patterns_can_be_extended_by_env(self):
        # SH_CLI_BLOCK_PATTERNS 在默认拦路标记之外追加
        a = self._alive_screen("\u203a MAINTENANCE WINDOW\n", SH_CLI_BLOCK_PATTERNS="maintenance window")
        self.assertFalse(a.ready.is_set())
        b = self._alive_screen("\u203a MAINTENANCE WINDOW\n")
        self.assertTrue(b.ready.is_set(), "without the extra marker the same screen is a prompt (control)")

    def test_block_markers_match_without_spaces(self):
        # 全屏 TUI 用光标定位代替空格：抓屏里读出来是「3.ProvideyourownAPIkey」
        self.assertIn(_squash("Provide your own API key"), _squash("3.ProvideyourownAPIkey"))

    def test_real_package_error_screen_then_exit_fails_fast(self):
        # prodops 2a3afdcc 原屏（横幅＋「no complete local package」）回放后即退出 ⇒ 不就绪、持久投递立即以 CLIUnavailable 失败
        fixture = os.path.join(os.path.dirname(_FIXTURE), "codex-0.159.2-package-error.pty.txt")
        d = tempfile.mkdtemp(prefix="sh-cli-pkg-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write("#!/usr/bin/env python3\nimport sys\nsys.stdout.buffer.write(open(%r, 'rb').read()); sys.stdout.flush()\nsys.exit(1)\n" % fixture)
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="20", SH_CLI_LAUNCH_RETRIES="2")))
        self.addCleanup(a.stop)
        out = io.StringIO()
        t0 = time.time()
        with contextlib.redirect_stdout(out):
            with self.assertRaises(CLIUnavailable):
                a.handle_delivery({"body": "task"}, on_injected=lambda: None)
        self.assertLess(time.time() - t0, 15)
        self.assertIn("[cli] exited code=1", out.getvalue())
        self.assertFalse(a.ready.is_set())

    def test_same_screen_without_menu_is_ready(self):
        a = self._adapter(menu_removed=True)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(a.start())
        self.assertTrue(a.ready.is_set(), "banner + '› Ask Codex…' composer alone is the input prompt")



_FIXDIR = os.path.dirname(_FIXTURE)
# 回放用例一律用 full-pty 件（组长 WORKGROUPS 第十节第 2 条）；前三件取自板 934fa246 树下 evidence/b12-codex-auth，健康主页取 41eecebb
_TRUST = os.path.join(_FIXDIR, "codex-0.159.2-trust-folder.pty.txt")  # ii-cr-60s-before-ok
_TRUST_THEN_UPDATE = os.path.join(_FIXDIR, "codex-0.159.2-trust-then-update-home.pty.txt")  # ii-cr-90s-after-ok
_LOGIN_THEN_KEY = os.path.join(_FIXDIR, "codex-0.159.2-login-then-api-key-detected.pty.txt")  # i-cr-90s-after-ok
_HEALTHY = os.path.join(_FIXDIR, "codex-0.159.2-healthy-home.pty.txt")  # b12-auth-ii-trust 60s（真 401 那轮）

# 假 CLI：回放抓屏（可只取 cut 标记起的后半段＝回车之后那一屏），之后可再隔 later_delay 秒吐一段 later 文字，然后一直活着
_REPLAY_TAIL = """#!/usr/bin/env python3
import sys, time
data = open(%r, "rb").read()
cut = %r
if cut:
    data = data[data.index(cut):]
sys.stdout.buffer.write(data); sys.stdout.flush()
later = %r
if later:
    time.sleep(%r)
    sys.stdout.write(later); sys.stdout.flush()
time.sleep(60)
"""


@unittest.skipUnless(pexpect is not None, "needs pexpect")
class CodexStartupBlockers(unittest.TestCase):
    """组长 WORKGROUPS 第十节第 1、2 条：拦路标记只在启动期（首次就绪之前）判，启动期一遇即 start 立即失败；
    就绪之后正文里出现拦路字样不得撤销就绪。拦路清单补 Trust this folder／Update available／Detected OPENAI_API_KEY。"""

    def _adapter(self, fixture, cut=b"", later="", later_delay=0.0, timeout="20"):
        d = tempfile.mkdtemp(prefix="sh-cli-startup-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write(_REPLAY_TAIL % (fixture, cut, later, later_delay))
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT=timeout, SH_CLI_LAUNCH_RETRIES="1")))
        self.addCleanup(a.stop)
        return a

    def _blocked_fast(self, a, marker):
        out = io.StringIO()
        t0 = time.time()
        with contextlib.redirect_stdout(out):
            with self.assertRaises(CLIUnavailable) as cm:
                a.handle_delivery({"body": "task"}, on_injected=lambda: None)
        took = time.time() - t0
        log = out.getvalue()
        # 就绪超时给的是 20 秒：拦路屏必须立即失败返回，不等满（startup_grace 1.5 秒＋读屏）
        self.assertLess(took, 8, "blocked screen must fail start immediately, not wait for the ready timeout: %.1fs" % took)
        self.assertEqual(a.blocked_by, marker)
        self.assertIn("[cli] blocked at screen: %r" % marker, log)
        self.assertIn("CLI blocked at screen", log)
        self.assertIn(marker, str(cm.exception))
        self.assertFalse(a.ready.is_set())

    def test_real_trust_folder_screen_fails_start_immediately(self):
        self._blocked_fast(self._adapter(_TRUST), "Trust this folder")

    def test_real_update_available_home_is_blocking(self):
        # 934fa246 ii-cr 90 秒屏里回车越过信任页之后那一屏（主页＋更新提示框）
        self._blocked_fast(self._adapter(_TRUST_THEN_UPDATE, cut=b"\x1b[1;1H\x1b[J\x1b[?2026h\x1b[2;3H\x1b[38;5;75;49m>_"),
                           "Update available")

    def test_real_api_key_detected_screen_is_blocking(self):
        # 934fa246 i-cr 90 秒屏里回车之后的 API key 输入框（检测到环境里的 key、待保存）
        self._blocked_fast(self._adapter(_LOGIN_THEN_KEY, cut=b"\x1b[3;1H>\x1b[3;3H\x1b[1mUse your own OpenAI API key"),
                           "Detected OPENAI_API_KEY")

    def test_leaving_trust_page_unblocks_on_full_redraw(self):
        # tosonnet55tc2k 复验 54238415：有人在终端里按回车离开信任页后仍被压着不就绪 ⇒ 按「当前屏幕」判。
        # 真屏：先回放 934fa246 ii-cr-60s 信任页；3 秒后送 ii-cr-90s 里回车之后的整屏重画（ESC[1;1H ESC[J 起，到更新提示框之前）
        d = tempfile.mkdtemp(prefix="sh-cli-leave-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write("""#!/usr/bin/env python3
import sys, time
sys.stdout.buffer.write(open(%r, "rb").read()); sys.stdout.flush()
time.sleep(3)
d = open(%r, "rb").read()
a = d.index(b"\\x1b[1;1H\\x1b[J\\x1b[?2026h\\x1b[2;3H\\x1b[38;5;75;49m>_", d.index(b"Trust and continue"))
e = d.index("\\x1b[1;1H\\x1b[2m\\u256d".encode(), a)
sys.stdout.buffer.write(d[a:e]); sys.stdout.flush()
time.sleep(60)
""" % (_TRUST, _TRUST_THEN_UPDATE))
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="20", SH_CLI_LAUNCH_RETRIES="1")))
        self.addCleanup(a.stop)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            a.start()
            self.assertEqual(a.blocked_by, "Trust this folder")
            self.assertFalse(a.ready.is_set())
            end = time.time() + 10
            while time.time() < end and not a.ready.is_set():
                time.sleep(0.1)
        self.assertTrue(a.ready.is_set(), "home redrawn after leaving the trust page must become ready")
        self.assertEqual(a.blocked_by, "")
        self.assertIn("[cli] left blocking screen: 'Trust this folder'", out.getvalue())

    def test_restarted_child_does_not_inherit_previous_screen(self):
        # 第一个子进程停在拦路屏（无整屏重画）后退出；经 start_pty 起的第二个正常 ⇒ 就绪（当前屏幕随新子进程清空）
        d = tempfile.mkdtemp(prefix="sh-cli-restart2-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write(_RESPAWN_PLAN % ([("1. Sign in with ChatGPT \u203a ", "", 0, 3), ("\u203a Ask Codex to do anything ", "", 0, 60)],))
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="10", SH_CLI_LAUNCH_RETRIES="1")))
        self.addCleanup(a.stop)
        with contextlib.redirect_stdout(io.StringIO()):
            a.start()
            self.assertEqual(a.blocked_by, "Sign in with ChatGPT")
            end = time.time() + 10
            while time.time() < end and not a._child_gone():
                time.sleep(0.1)
            self.assertTrue(a._child_gone())
            time.sleep(0.5)
            end = time.time() + 15
            while time.time() < end and not a.start():
                time.sleep(0.5)
        self.assertTrue(a.ready.is_set(), "second child shows only the prompt")
        self.assertEqual(a.blocked_by, "")

    def test_healthy_home_stays_ready_when_answer_mentions_blocker_text(self):
        # tosonnet55tc2k 实测：就绪后回答里一句 Sign in with ChatGPT 就把健康座席判死
        a = self._adapter(_HEALTHY, later="\r\n\u2022 To log in, choose Sign in with ChatGPT or Provide your own API key."
                                          " Trust this folder. Update available. Press enter to continue\r\n",
                          later_delay=3.5)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertTrue(a.start())
            self.assertTrue(a.ready.is_set(), "real healthy home (41eecebb) must be ready")
            time.sleep(4.5)  # 等假 CLI 吐出含拦路字样的「回答」并被读线程读到
        self.assertIn("Sign in with ChatGPT", " ".join(a.output_tail))
        self.assertTrue(a.ready.is_set(), "blocker words in an answer after startup must not revoke readiness")
        self.assertEqual(a.blocked_by, "")
        self.assertNotIn("blocked at screen", out.getvalue())


    def test_restarted_child_is_judged_afresh_during_its_startup(self):
        # transcript 档：第一个子进程就绪、吐过输出（启动期已过）后退出；下次投递经 start_pty 起第二个，停在登录菜单
        # ⇒ 必须判拦路、立即失败（启动期标志随新子进程重置）
        d = tempfile.mkdtemp(prefix="sh-cli-restart-")
        path = os.path.join(d, "fakecli.py")
        with open(_FIXTURE, encoding="utf-8") as f:
            menu = f.read()
        with open(path, "w") as f:
            f.write(_RESPAWN_PLAN % ([("\u203a Ask Codex to do anything ", "hello\n", 4.0, 3), (menu, "", 0, 60)],))
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="codex", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="20", SH_CLI_LAUNCH_RETRIES="1")))
        self.addCleanup(a.stop)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(a.start())
            end = time.time() + 12
            while time.time() < end and a.ready.is_set():
                time.sleep(0.1)
            self.assertFalse(a.ready.is_set(), "first child should have exited")
            self.assertTrue(a.startup_done, "first child had passed its startup")
        self._blocked_fast(a, "Sign in with ChatGPT")

_RESPAWN_PLAN = """#!/usr/bin/env python3
import os, sys, time
n_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "runs")
n = int(open(n_path).read()) if os.path.exists(n_path) else 0
open(n_path, "w").write(str(n + 1))
plan = %r
text, later, later_delay, exit_after = plan[min(n, len(plan) - 1)]
sys.stdout.write(text); sys.stdout.flush()
if later:
    time.sleep(later_delay)
    sys.stdout.write(later); sys.stdout.flush()
time.sleep(exit_after)
"""


@unittest.skipUnless(pexpect is not None, "needs pexpect")
class PtyRespawnStartupWindow(unittest.TestCase):
    """PTY 输出档 CLI 退出后由读线程自动重生（不经 start_pty）：每个新子进程各有自己的启动期。"""

    def _adapter(self, plan):
        d = tempfile.mkdtemp(prefix="sh-cli-respawn-")
        path = os.path.join(d, "fakecli.py")
        with open(path, "w") as f:
            f.write(_RESPAWN_PLAN % (plan,))
        os.chmod(path, 0o755)
        a = CLIAdapter(load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="gemini", SH_CLI_CMD=path, SH_CLI_CWD=d,
                                        SH_CLI_READY_TIMEOUT="10", SH_CLI_LAUNCH_RETRIES="1",
                                        SH_CLI_BLOCK_PATTERNS="please sign in")))
        self.addCleanup(a.stop)
        return a

    def _wait(self, cond, limit):
        end = time.time() + limit
        while time.time() < end and not cond():
            time.sleep(0.1)
        return cond()

    def test_respawned_child_ends_its_own_startup(self):
        # 第一个子进程正常就绪后退出；重生的第二个就绪 3 秒后回答里出现拦路字样 ⇒ 仍就绪
        a = self._adapter([("Welcome > ", "", 0, 3), ("Welcome > ", "answer: please sign in to the site\n", 3.5, 60)])
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(a.start())
            self.assertTrue(self._wait(lambda: a.respawn_count >= 1, 10), "first child should exit and respawn")
            time.sleep(9)  # 重生退避 2 秒＋就绪＋3.5 秒后吐「回答」
        self.assertIn("please sign in", " ".join(a.output_tail))
        self.assertTrue(a.ready.is_set(), "respawned child past its startup must not be revoked by answer text")

    def test_respawned_child_is_judged_afresh_during_its_startup(self):
        # 第一个子进程启动期遇拦路屏后退出；重生的第二个正常 ⇒ 就绪（拦路状态随子进程重置）
        # 再反过来：第一个正常、第二个启动即拦路 ⇒ 不就绪（启动期标志随子进程重置）
        a = self._adapter([("please sign in > ", "", 0, 3), ("Welcome > ", "", 0, 60)])
        with contextlib.redirect_stdout(io.StringIO()):
            a.start()
            self.assertTrue(self._wait(lambda: a.ready.is_set(), 15), "healthy respawned child must become ready")
        self.assertEqual(a.blocked_by, "")
        # 第一个子进程就绪且静默之后还吐过一段输出（读线程据此结束其启动期），否则「重生不重置启动期」测不出来
        b = self._adapter([("Welcome > ", "hello\n", 4.0, 3), ("please sign in > ", "", 0, 60)])
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(b.start())
            self.assertTrue(self._wait(lambda: b.respawn_count >= 1, 12))
            self.assertTrue(self._wait(lambda: b.blocked_by != "", 10), "respawned child's startup screen is judged")
        self.assertFalse(b.ready.is_set())


if __name__ == "__main__":
    unittest.main()
