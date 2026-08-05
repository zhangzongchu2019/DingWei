"""发方 wiring 单测:shim IPC 顶层四字段(reply_to/in_reply_to/reply_policy/kind)
→ 出站 envelope.meta 同名 key(契约:shence 08-03 定,顶层并列/无嵌套/存在才取)。

覆盖两层:
  1. ipc_listener.extract_reply_origin_meta 纯函数(取哪些 key/怎么取)
  2. _handle_connection → send_handler 透传 + envelope() 组包端到端
"""
import asyncio
import json
import unittest

from sessionhelper.ipc_listener import (
    REPLY_ORIGIN_KEYS,
    extract_reply_origin_meta,
    _handle_connection,
)
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


if __name__ == "__main__":
    unittest.main()
