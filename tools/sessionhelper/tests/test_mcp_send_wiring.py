"""发方 wiring 单测:shim IPC 顶层四字段(reply_to/in_reply_to/reply_policy/kind)
→ 出站 envelope.meta 同名 key(契约:shence 08-03 定,顶层并列/无嵌套/存在才取)。

覆盖两层:
  1. ipc_listener.extract_reply_origin_meta 纯函数(取哪些 key/怎么取)
  2. _handle_connection → send_handler 透传 + envelope() 组包端到端
"""
import asyncio
import ast
import inspect
import json
import os
import socket
import tempfile
import textwrap
import threading
from types import SimpleNamespace
import unittest
from unittest import mock

from sessionhelper.ipc_listener import (
    REPLY_ORIGIN_KEYS,
    extract_reply_origin_meta,
    _handle_connection,
)
from sessionhelper import mcp_shim
from sessionhelper.app import SessionHelper
from sessionhelper.protocol import envelope


class ExtractReplyOriginMetaTest(unittest.TestCase):
    def test_all_four_present(self):
        msg = {"type": "mcp_send", "to": "a#k", "body": "hi",
               "reply_to": "feishu#c1#u1", "in_reply_to": "m-123",
               "reply_policy": "no_reply", "kind": "reply"}
        self.assertEqual(extract_reply_origin_meta(msg), {
            "reply_to": "feishu#c1#u1", "in_reply_to": "m-123",
            "reply_policy": "no_reply", "kind": "reply"})

    def test_subset_present(self):
        msg = {"type": "mcp_send", "to": "a#k", "body": "hi", "in_reply_to": "m-9"}
        self.assertEqual(extract_reply_origin_meta(msg), {"in_reply_to": "m-9"})

    def test_none_present_is_empty(self):
        self.assertEqual(extract_reply_origin_meta({"to": "a#k", "body": "hi"}), {})

    def test_explicit_null_treated_as_absent(self):
        msg = {"to": "a#k", "body": "hi", "reply_to": None, "kind": "content"}
        self.assertEqual(extract_reply_origin_meta(msg), {"kind": "content"})

    def test_unknown_top_level_keys_not_copied(self):
        msg = {"to": "a#k", "body": "hi", "kind": "reply",
               "hop_count": 3, "conversation_id": "c-1", "dedup_key": "d-1"}
        # hop/conversation_id/dedup_key 归 helper/hub 自填(§9 分层),shim 不得注入
        self.assertEqual(extract_reply_origin_meta(msg), {"kind": "reply"})

    def test_key_set_matches_contract(self):
        self.assertEqual(set(REPLY_ORIGIN_KEYS),
                         {"reply_to", "in_reply_to", "reply_policy", "kind"})


class _CapturePipe:
    """最小 StreamReader/StreamWriter 替身,喂一条请求、收一条响应。"""

    def __init__(self, request: dict):
        self._line = (json.dumps(request) + "\n").encode("utf-8")
        self.written = b""

    async def readline(self):
        line, self._line = self._line, b""
        return line

    def write(self, data):
        self.written += data

    async def drain(self):
        pass


class HandleConnectionWiringTest(unittest.TestCase):
    def _run(self, request: dict):
        pipe = _CapturePipe(request)
        calls = []

        async def send_handler(to, body, meta):
            calls.append((to, body, meta))
            return {"sent": True, "to": to}

        asyncio.run(_handle_connection(pipe, pipe, send_handler))
        resp = json.loads(pipe.written.decode("utf-8"))
        return calls, resp

    def test_four_fields_reach_send_handler(self):
        calls, resp = self._run({
            "type": "mcp_send", "to": "a#k", "body": "hi", "request_id": "r1",
            "reply_to": "feishu#c1#u1", "in_reply_to": "m-1",
            "reply_policy": "no_reply", "kind": "reply"})
        self.assertTrue(resp["ok"])
        self.assertEqual(calls, [("a#k", "hi", {
            "reply_to": "feishu#c1#u1", "in_reply_to": "m-1",
            "reply_policy": "no_reply", "kind": "reply"})])

    def test_plain_send_gets_empty_meta(self):
        calls, resp = self._run({"type": "mcp_send", "to": "a#k",
                                 "body": "hi", "request_id": "r2"})
        self.assertTrue(resp["ok"])
        self.assertEqual(calls, [("a#k", "hi", {})])

    def test_envelope_meta_carries_fields_verbatim(self):
        # app.py _send 的组包路径:envelope(to, body, from, meta or None)
        meta = {"reply_to": "sess#x", "kind": "reply"}
        env = envelope("a#k", "hi", "b#k", meta or None)
        self.assertEqual(env["meta"], meta)
        env2 = envelope("a#k", "hi", "b#k", {} or None)
        self.assertEqual(env2["meta"], {})

    def test_vsapi_without_datad_is_explicitly_unconfigured(self):
        helper = object.__new__(SessionHelper)
        helper.cfg = SimpleNamespace(datad_url="", datad_token="")
        handler = helper._make_vsapi_handler()
        with self.assertRaisesRegex(RuntimeError, "未配置.*datad_url") as caught:
            asyncio.run(handler("POST", "/v1/sessions/me/heartbeat", {"status": "active"}))
        self.assertNotIn("Transport closed", str(caught.exception))
        self.assertNotIn("NoneType", str(caught.exception))

    def test_run_once_always_installs_vsapi_handler(self):
        """装配闸:变异回 `... if datad_url else None` 必须红。

        R-V2-IMAGE-R5 起 IPC 服务由 _ipc_server 装配（_run_once 只起它）：装配点随之移到 _ipc_server。
        """
        self.assertIn("self._ipc_server(ws)", inspect.getsource(SessionHelper._run_once))
        tree = ast.parse(textwrap.dedent(inspect.getsource(SessionHelper._ipc_server)))
        assignments = [node for node in ast.walk(tree)
                       if isinstance(node, ast.Assign)
                       and any(isinstance(target, ast.Name) and target.id == "vsapi_handler"
                               for target in node.targets)]
        self.assertEqual(len(assignments), 1, "_ipc_server 应仅装配一次 vsapi_handler")
        self.assertIsInstance(assignments[0].value, ast.Call,
                              "vsapi_handler 必须始终由 factory 生成,不得未配置时退回 None")

    def test_mcp_vsapi_receives_one_line_error_instead_of_transport_close(self):
        """真 Unix socket 腿:两个 MCP 工具都须收到明确错误,不能被吞成断链。"""
        cases = (
            ("heartbeat_report", lambda: mcp_shim._heartbeat_report({"status": "active"})),
            ("project_context", lambda: mcp_shim._project_context({"scope": "config"})),
        )
        for name, invoke in cases:
            with self.subTest(tool=name), tempfile.TemporaryDirectory() as tmp:
                socket_path = os.path.join(tmp, "mcp.sock")
                ready = threading.Event()

                def server():
                    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as listener:
                        listener.bind(socket_path)
                        listener.listen(1)
                        ready.set()
                        conn, _ = listener.accept()
                        with conn:
                            conn.recv(4096)
                            conn.sendall(json.dumps({
                                "ok": False,
                                "error": "vsapi 未配置:datad_url 未配置",
                            }, ensure_ascii=False).encode("utf-8") + b"\n")

                worker = threading.Thread(target=server, daemon=True)
                worker.start()
                self.assertTrue(ready.wait(2), "Unix socket fixture did not start")
                with mock.patch.object(mcp_shim, "_IPC_SOCK", socket_path), \
                     mock.patch.object(mcp_shim, "_IPC_TIMEOUT", 1.0):
                    with self.assertRaisesRegex(RuntimeError, "未配置.*datad_url") as caught:
                        invoke()
                self.assertNotIn("Transport closed", str(caught.exception))
                worker.join(timeout=2)
                self.assertFalse(worker.is_alive(), "Unix socket fixture did not finish")


class DatadEnvWiringTest(unittest.TestCase):
    """T18 补漏回归钉:datad_url/datad_token 必须从 env 接入 load_config
    (0f92e90/5aa58a2 只加字段未接 env,cfg.datad_* 恒空 ⇒ vsapi 管道死禁用)。"""

    BASE = {
        "SH_SESSION_NAME": "home",
        "SH_OWNER": "zzc",
        "SH_KEY_ID": "FB-2642",
        "SH_SECRET": "secret",
    }

    def test_datad_env_wired(self):
        from sessionhelper.config import load_config
        cfg = load_config(dict(self.BASE,
                               DATAD_URL="https://dingwei.wegoab.com/datad",
                               DATAD_TOKEN="tok-abc"))
        self.assertEqual(cfg.datad_url, "https://dingwei.wegoab.com/datad")
        self.assertEqual(cfg.datad_token, "tok-abc")

    def test_sh_prefix_takes_precedence(self):
        from sessionhelper.config import load_config
        cfg = load_config(dict(self.BASE,
                               SH_DATAD_URL="http://127.0.0.1:8796",
                               DATAD_URL="https://other",
                               SH_DATAD_TOKEN="tok-sh", DATAD_TOKEN="tok-plain"))
        self.assertEqual(cfg.datad_url, "http://127.0.0.1:8796")
        self.assertEqual(cfg.datad_token, "tok-sh")

    def test_absent_stays_empty(self):
        from sessionhelper.config import load_config
        cfg = load_config(dict(self.BASE))
        self.assertEqual(cfg.datad_url, "")
        self.assertEqual(cfg.datad_token, "")


if __name__ == "__main__":
    unittest.main()
