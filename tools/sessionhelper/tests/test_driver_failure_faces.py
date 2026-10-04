"""R-V2-HOSTED-DRIVER-IMPL-AMEND-20261004143259 异议 2（kimi 侧）：driver 模式三失败面
——凭据无效／codex app-server 起不来／首次运行卡死（无终态事件）——各须：
helper 日志读出可见原因＋投递以带原因的 failed 结束（不得静默挂起）。

真 helper 投递链（DriverAdapter＋真 CodexProtocolDriver）＋假 codex（SH_CODEX_BIN 指到
本文件的假 app-server：行分隔 JSON-RPC，行为由 FAKE_CODEX_BEHAVIOR 选）。"""
import contextlib
import io
import json
import os
import stat
import sys
import tempfile
import textwrap
import time
import unittest
from pathlib import Path

from sessionhelper.config import load_config
from sessionhelper.driver_adapter import DriverAdapter

try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tests._home_isolation import HomeIsolation

_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


FAKE_CODEX = textwrap.dedent(
    """
    import json, os, sys
    BEHAVIOR = os.environ.get("FAKE_CODEX_BEHAVIOR", "ok")
    if BEHAVIOR == "crash" and False:
        pass
    for line in sys.stdin:
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue
        rid = msg.get("id")
        method = msg.get("method", "")
        if rid is None:
            continue  # notification（initialized 等）
        if method == "initialize":
            if BEHAVIOR == "crash":
                out = {"id": rid, "error": {"code": -32001, "message": "app-server cannot start: config corrupt (FAKE_START_BAD)"}}
            else:
                out = {"id": rid, "result": {"serverInfo": {"name": "fake", "version": "0"}}}
        elif method == "thread/start":
            if BEHAVIOR == "authfail":
                out = {"id": rid, "error": {"code": -32000, "message": "401 Unauthorized: invalid auth token (FAKE_AUTH_BAD)"}}
            else:
                out = {"id": rid, "result": {"thread": {"id": "th_fake"}}}
        elif method == "turn/start":
            if BEHAVIOR == "turnauthfail":
                # 真凭据无效路径（B1d 复核裁定 3）：daemon 正常起、turn 请求才回鉴权错
                out = {"id": rid, "error": {"code": -32000, "message": "401 Unauthorized: token expired (FAKE_TURN_AUTH_BAD)"}}
            else:
                # stall：受理但永不发终态事件（卡交互屏的 driver 面）
                out = {"id": rid, "result": {"turn": {"id": "turn_fake", "status": "inProgress"}}}
        elif method == "turn/start" and False:
            pass
        else:
            out = {"id": rid, "result": {}}
        sys.stdout.write(json.dumps(out) + "\\n")
        sys.stdout.flush()
        if method == "turn/start" and BEHAVIOR == "turnfail-event":
            # 真 codex 的 turn 失败通知形态：turn/completed 带 status=failed
            note = {"method": "turn/completed", "params": {"threadId": "th_fake",
                    "turn": {"id": "turn_fake", "status": "failed", "error": {"message": "turn failed: FAKE_TURN_BOOM"}}}}
            sys.stdout.write(json.dumps(note) + "\\n")
            sys.stdout.flush()
    """
).lstrip()

BASE_ENV = {
    "SH_SESSION_NAME": "dev",
    "SH_OWNER": "zzc",
    "SH_KEY_ID": "FB-2642",
    "SH_SECRET": "secret",
    "SH_HUB_URL": "ws://127.0.0.1:1/ws",
    "SH_TURN_TERMINAL_TIMEOUT_SECONDS": "1",  # 卡死面：1s 无终态即判（不真等 1800s）
}


class DriverFailureFacesTest(unittest.TestCase):
    def _mk_fake_codex(self, td: str, behavior: str) -> str:
        path = os.path.join(td, "fake_codex.py")
        Path(path).write_text(FAKE_CODEX)
        os.chmod(path, stat.S_IRUSR | stat.S_IXUSR)
        return path

    def _mk_adapter(self, td: str, behavior: str) -> DriverAdapter:
        from unittest import mock

        fake = self._mk_fake_codex(td, behavior)
        # 本测试进程自己就泡在 SH_* env 里（helper 里跑 helper）——driver_adapter 的 env_get
        # 直读 os.environ ⇒ 必须显式钉死相关键，否则读到本席真值（实测泄漏成 driver=claude）。
        patcher = mock.patch.dict(os.environ, {
            "SH_DRIVER": "codex",
            "SH_CODEX_BIN": sys.executable,  # python 本体当"codex"（真命令经 _patch_cmd 换成脚本）
            "FAKE_CODEX_BEHAVIOR": behavior,
            "SH_CLI_CWD": td,
            "SH_TURN_TERMINAL_TIMEOUT_SECONDS": "1",
        })
        patcher.start()
        self.addCleanup(patcher.stop)
        env = dict(BASE_ENV, SH_MODE="driver", SH_CLI_CWD=td)
        cfg = load_config(env)
        adapter = DriverAdapter(cfg)
        import sessionhelper.codexDriver as codex_mod

        codex_mod._CODEX_BIN = sys.executable
        # 直传 stdio（默认 daemon_ws 要起真 daemon）；假 codex 是脚本 ⇒ 命令换成 [python, fake_codex.py, ...]
        adapter.driver._workdir = td
        adapter.driver._transport_mode = "direct"
        adapter.driver._manage_daemon = False
        self._patch_cmd([sys.executable, fake])
        self.addCleanup(self._unpatch_cmd)
        return adapter

    _orig_rpc_init = None

    @classmethod
    def _patch_cmd(cls, prefix):
        """把 StdioJsonRpc 的 app-server 命令换成 [python, fake_codex.py, ...]（cmd[0] 是 _CODEX_BIN）。"""
        import sessionhelper.codexDriver as codex_mod

        if cls._orig_rpc_init is None:
            cls._orig_rpc_init = codex_mod.StdioJsonRpc.__init__
        def patched(self, cmd, *, cwd, env=None):
            cls._orig_rpc_init(self, prefix + cmd[1:], cwd=cwd, env=env)
        codex_mod.StdioJsonRpc.__init__ = patched

    @classmethod
    def _unpatch_cmd(cls):
        import sessionhelper.codexDriver as codex_mod

        if cls._orig_rpc_init is not None:
            codex_mod.StdioJsonRpc.__init__ = cls._orig_rpc_init
            cls._orig_rpc_init = None

    def _drive_one(self, adapter: DriverAdapter) -> tuple[str, str, str]:
        """投递一条，等到 failed；返回（终态, 事件面 reason, 捕获日志）。"""
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            adapter.enqueue_envelope({"body": "ping", "meta": {"source_kind": "user"}})
            deadline = time.monotonic() + 15
            state = ""
            while time.monotonic() < deadline:
                cur = adapter._commands._current
                if cur is not None and cur.state == "failed":
                    state = cur.state
                    break
                # 已完成的会从 _current 挪走——看 by_delivery
                for c in list(adapter._commands._by_delivery.values()):
                    if c.state == "failed":
                        state = "failed"
                if state:
                    break
                time.sleep(0.05)
        logs = buf.getvalue()
        # 事件面（session_event 里带原因的失败）
        events = []
        while not adapter._event_q.empty():
            events.append(adapter._event_q.get_nowait())
        reasons = json.dumps(events, ensure_ascii=False)
        return state, reasons, logs

    def test_face1_invalid_credentials(self):
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk_adapter(td, "authfail")
            state, reasons, logs = self._drive_one(adapter)
            self.assertEqual(state, "failed", f"投递须以 failed 结束；日志：{logs[:400]}")
            self.assertIn("FAKE_AUTH_BAD", logs, "helper 日志须读出可见原因（401 无效凭据）")
            self.assertIn("deliver_failed", reasons, "事件面须带原因（deliver_failed）")

    def test_face2_appserver_wont_start(self):
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk_adapter(td, "crash")
            state, reasons, logs = self._drive_one(adapter)
            self.assertEqual(state, "failed", f"投递须以 failed 结束；日志：{logs[:400]}")
            self.assertIn("[FAILED]", logs, "helper 日志须读出可见失败")
            self.assertIn("FAKE_START_BAD", logs, "helper 日志须读出起不来的原因")
            self.assertIn("driver_start_failed", reasons)

    def test_face1b_invalid_credential_at_turn(self):
        # B1d 复核裁定 3：daemon 正常起、turn 请求才回 JSON-RPC 鉴权错
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk_adapter(td, "turnauthfail")
            state, reasons, logs = self._drive_one(adapter)
            self.assertEqual(state, "failed", f"投递须以 failed 结束；日志：{logs[:400]}")
            # helper 日志的可见原因：adapter 打印 detail 前 60 字（截断是已知小项——
            # 登记修类，不挡），鉴权失败落在头部 ⇒ "turn/start failed"＋"401" 可见。
            self.assertIn("turn/start failed", logs, "helper 日志须读出失败点")
            self.assertIn("401", logs, "helper 日志须读出鉴权失败原因（401）")
            # 「带原因的 failed」的载体＝队列终态 failed＋receipt detail（含 401 全文）；
            # 事件面在本路径不另发 terminal failure（与面 2 的 driver_start_failed 不同路径）。

    def test_face3_stuck_no_terminal_event(self):
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk_adapter(td, "stall")
            state, reasons, logs = self._drive_one(adapter)
            self.assertEqual(state, "failed", f"卡死须以 failed 结束（不是静默挂起）；日志：{logs[:400]}")
            self.assertIn("TIMEOUT", logs, "helper 日志须读出 terminal timeout")
            self.assertIn("terminal_timeout", reasons)


if __name__ == "__main__":
    unittest.main()

class HubDeliveryPathTest(unittest.TestCase):
    """复验须改 2：经 handle_delivery（Hub 真投递路径 adapter.handle_delivery，app.py:765）的格。
    并在格内钉死 Hub 可见终态的事实口径：
    - 面 2（start 失败）：handle_delivery 抛出（adapter 不吞）⇒ app delivery_worker_loop
      的 except 兜住 ⇒ Hub 收 failed ACK（reason=cli_injection_failed）——既有路径，不经我方 catch。
    - 面 1b/3（start 成功、turn 阶段失败）：handle_delivery 正常返回（入队即 injected ACK），
      之后的失败只走 session_event（turn_completed outcome=failed 带 reason）——
      Hub 投递态是 injected/processed，失败对 Hub 的投递面不可见（仅事件面）。"""

    def _mk(self, td, behavior):
        rig = DriverFailureFacesTest()
        rig._testMethodName = "_mk"
        rig._outcome = None
        return rig._mk_adapter(td, behavior)

    def test_handle_delivery_turn_auth_failure(self):
        # turnauthfail＝turn/start 直接回错（同步 failed）；事件面（session_event）由
        # turnfail-event 格覆盖（假件发 turn/completed(failed) 通知）。
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk(td, "turnauthfail")
            injected = []
            # handle_delivery 正常返回（不抛）＋on_injected 被调（⇒ Hub 先收 injected ACK）
            out = adapter.handle_delivery(
                {"body": "ping", "meta": {"source_kind": "user"}},
                on_injected=lambda: injected.append(True),
            )
            self.assertEqual(out, "")
            self.assertTrue(injected, "入队成功 ⇒ injected ACK（Hub 先见 injected）")
            # worker 随后判 failed：队列终态＋事件面带原因；Hub 投递态停 injected/processed
            deadline = time.monotonic() + 15
            state = ""
            while time.monotonic() < deadline:
                for c in list(adapter._commands._by_delivery.values()):
                    if c.state == "failed":
                        state = "failed"
                if state:
                    break
                time.sleep(0.05)
            self.assertEqual(state, "failed")
            events = []
            while not adapter._event_q.empty():
                events.append(adapter._event_q.get_nowait())
            blob = json.dumps(events, ensure_ascii=False)
            # 本路径失败载体＝队列终态 failed＋日志（session_event 的 turn_completed
            # 由 turn/completed 通知驱动，本行为不发通知 ⇒ 不在此断言）

    def test_handle_delivery_turn_failed_event_surfaces(self):
        # 事件面：turn/completed(status=failed) 通知 ⇒ session_event 带 turn_completed
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk(td, "turnfail-event")
            adapter.handle_delivery({"body": "ping", "meta": {"source_kind": "user"}},
                                    on_injected=lambda: None)
            deadline = time.monotonic() + 15
            blob = ""
            while time.monotonic() < deadline:
                events = []
                while not adapter._event_q.empty():
                    events.append(adapter._event_q.get_nowait())
                blob = json.dumps(events, ensure_ascii=False)
                if "turn_completed" in blob:
                    break
                time.sleep(0.05)
            self.assertIn("turn_completed", blob, "事件面须见 turn_completed（Hub 据 session_event 记指标）")
            self.assertIn("failed", blob)

    def test_handle_delivery_start_failure_raises_for_app_catch(self):
        # 面 2 经 handle_delivery：start 失败原样抛出——app 层（delivery_worker_loop
        # 的 except Exception）兜住并发 failed ACK（cli_injection_failed）。
        # adapter 这层不吞（吞了 app 就没法区分「起不来」与「已受理」）。
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk(td, "crash")
            with self.assertRaises(Exception) as cm:
                adapter.handle_delivery({"body": "ping", "meta": {"source_kind": "user"}})
            self.assertIn("FAKE_START_BAD", str(cm.exception))

    def test_enqueue_human_start_failure_fails_with_reason(self):
        # 小提示①：enqueue_human 的 catch 也要有格——crash 时投递以带原因 failed 结束
        with tempfile.TemporaryDirectory() as td:
            adapter = self._mk(td, "crash")
            result = adapter.enqueue_human("ping")  # 不得抛出
            self.assertIsNotNone(result.command)
            self.assertEqual(result.command.state, "failed")
            events = []
            while not adapter._event_q.empty():
                events.append(adapter._event_q.get_nowait())
            self.assertIn("driver_start_failed", json.dumps(events, ensure_ascii=False))
