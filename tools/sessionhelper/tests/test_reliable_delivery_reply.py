"""Agent 间可靠投递回信不再被传输外壳判成 control（组长 R-AGENT-REPLY-CONTROL-IMPL-20261002154009，板 57f0e517）。

依据 runbooks/ANALYSIS-R-AGENT-REPLY-CONTROL-coder56solzzc-20261002.md 第二、四节：Hub 可靠投递把 meta.type 覆写成
delivery（internal/m8/hub.go），helper 的 classify_reply_decision 第一道 control 判据因此恒真 ⇒ expect_reply 的内容信永远不回。
改法：只对严格可靠外壳（type=delivery、delivery_version=1、ack_required 为真、delivery_id 非空）忽略 type=delivery 这一项，
kind=ack、正文保留 token、mirror、terminal、hop、reply_policy 照判。

本文件是分析第四节第 1–5 格；第 6、7 格（真 Hub＋两个真 helper）在 reply_origin_binary_smoke.py。
"""
import asyncio
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import unittest

try:
    from sessionhelper.routing import classify_reply_decision, is_reliable_delivery_wrapper, reply_carryforward_meta
except ImportError:  # pragma: no cover
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    from sessionhelper.routing import classify_reply_decision, is_reliable_delivery_wrapper, reply_carryforward_meta

try:
    import websockets
    _WS_OK = int(websockets.__version__.split(".")[0]) >= 15
except ImportError:  # pragma: no cover
    websockets = None
    _WS_OK = False

_PKG_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_HASH = "\x23"  # 地址分隔符（会话名与 key 之间）


def _wrapped(**meta) -> dict:
    """与 hub.go 可靠投递改写同形的外壳；meta 里给的键覆盖默认。"""
    base = {"type": "delivery", "delivery_id": "dw-dlv_0123", "delivery_version": 1, "ack_required": True,
            "source_message_id": "m-1"}
    base.update(meta)
    return {"id": "m-1", "from": "a" + _HASH + "k", "to": "b" + _HASH + "k", "body": "hello", "meta": base}


class ReliableWrapperReplyDecision(unittest.TestCase):
    # 第 1 格：可靠外壳＋expect_reply＋kind=content ⇒ 回
    def test_reliable_wrapper_expect_reply_content_is_allowed(self):
        d = classify_reply_decision(_wrapped(reply_policy="expect_reply", kind="content"))
        self.assertEqual((d.allow, d.reason), (True, "ok"))

    def test_reliable_wrapper_expect_reply_without_kind_is_allowed(self):
        # mcp_send 不给 kind 时 Hub 原样透传空 kind，与 content 同
        d = classify_reply_decision(_wrapped(reply_policy="expect_reply"))
        self.assertEqual((d.allow, d.reason), (True, "ok"))

    # 第 2 格：可靠外壳但不要回信 ⇒ 不回，reason=no_expect_reply
    def test_reliable_wrapper_without_expect_reply_is_not_replied(self):
        for policy in (None, "no_reply", "ack_only"):
            meta = {} if policy is None else {"reply_policy": policy}
            d = classify_reply_decision(_wrapped(**meta))
            self.assertEqual((d.allow, d.reason), (False, "no_expect_reply"), policy)

    # 第 3 格：真控制即使带 expect_reply 也是 control
    def test_real_control_types_stay_control_even_with_expect_reply(self):
        for t in ("delivery_ack", "delivery_cancel", "delivery_cancel_ack", "provision", "provision_ack",
                  "mirror_control"):
            d = classify_reply_decision(_wrapped(type=t, reply_policy="expect_reply"))
            self.assertEqual((d.allow, d.reason), (False, "control"), t)

    def test_kind_ack_inside_reliable_wrapper_stays_control(self):
        d = classify_reply_decision(_wrapped(kind="ack", reply_policy="expect_reply"))
        self.assertEqual((d.allow, d.reason), (False, "control"))

    def test_reserved_payload_token_inside_reliable_wrapper_stays_control(self):
        env = _wrapped(reply_policy="expect_reply")
        env["body"] = '{"schema": "mcs_receipt_v1"}'
        d = classify_reply_decision(env)
        self.assertEqual((d.allow, d.reason), (False, "control"))

    def test_protocol_class_control_inside_reliable_wrapper_stays_control(self):
        d = classify_reply_decision(_wrapped(protocol_class="control", reply_policy="expect_reply"))
        self.assertEqual((d.allow, d.reason), (False, "control"))

    # 第 4 格：畸形外壳不享受例外
    def test_malformed_wrappers_get_no_exception(self):
        cases = {
            "delivery_version=2": {"delivery_version": 2},
            "delivery_version='1'": {"delivery_version": "1"},
            "delivery_version=True": {"delivery_version": True},
            "delivery_version=1.0": {"delivery_version": 1.0},
            "delivery_version missing": {"delivery_version": None},
            "ack_required='true'": {"ack_required": "true"},
            "ack_required=1": {"ack_required": 1},
            "ack_required=False": {"ack_required": False},
            "delivery_id empty": {"delivery_id": ""},
            "delivery_id blank": {"delivery_id": "  "},
            "delivery_id missing": {"delivery_id": None},
            "delivery_id=None": {"delivery_id": "<explicit-none>"},
            "ack_required=None": {"ack_required": "<explicit-none>"},
        }
        for name, bad in cases.items():
            env = _wrapped(reply_policy="expect_reply", **bad)
            for k, v in bad.items():
                if v is None:
                    env["meta"].pop(k)
                elif v == "<explicit-none>":
                    env["meta"][k] = None
            self.assertFalse(is_reliable_delivery_wrapper(env), name)
            d = classify_reply_decision(env)
            self.assertEqual((d.allow, d.reason), (False, "control"), name)

    # 外壳例外之后其余判据照走（分析第三节 R3／R4）
    def test_mirror_terminal_and_hop_still_apply_inside_reliable_wrapper(self):
        self.assertEqual(classify_reply_decision(_wrapped(kind="mirror", reply_policy="expect_reply")).reason, "mirror")
        self.assertEqual(classify_reply_decision(_wrapped(kind="reply", conversation_terminal=True,
                                                          reply_policy="expect_reply")).reason,
                         "conversation_terminal")
        self.assertEqual(classify_reply_decision(_wrapped(hop_count=16, reply_policy="expect_reply")).reason,
                         "hop_backstop")
        self.assertEqual(classify_reply_decision(_wrapped(hop_count=3, max_hops=3,
                                                          reply_policy="expect_reply")).reason, "hop_backstop")

    def test_order_inside_reliable_wrapper_is_mirror_terminal_hop_policy(self):
        # 搭档 tosonnet55tc2k 前置意见第 3 条：顺序 mirror→terminal→hop→reply_policy 不变（几项同时成立时取最前一项）
        both = dict(kind="mirror", conversation_terminal=True, hop_count=16, reply_policy="no_reply")
        self.assertEqual(classify_reply_decision(_wrapped(**both)).reason, "mirror")
        both.pop("kind")
        self.assertEqual(classify_reply_decision(_wrapped(**both)).reason, "conversation_terminal")
        both.pop("conversation_terminal")
        self.assertEqual(classify_reply_decision(_wrapped(**both)).reason, "hop_backstop")
        both.pop("hop_count")
        self.assertEqual(classify_reply_decision(_wrapped(**both)).reason, "no_expect_reply")

    def test_reply_to_a_reliable_delivery_is_terminal_and_rewrapped_reply_stops(self):
        # 回信经 Hub 再包成可靠外壳：对端走到 conversation_terminal 停，不再回（两跳终止）
        inbound = _wrapped(reply_policy="expect_reply", hop_count=1, conversation_id="c-1")
        carry = reply_carryforward_meta(inbound)
        self.assertEqual((carry["kind"], carry["conversation_terminal"], carry["in_reply_to"], carry["hop_count"]),
                         ("reply", True, "m-1", 1))
        back = _wrapped(**carry)
        back["meta"]["delivery_id"] = "dw-dlv_4567"
        self.assertEqual(classify_reply_decision(back).reason, "conversation_terminal")


_ECHO_STARTED = "[sessionHelper] connected"


@unittest.skipUnless(_WS_OK, "needs websockets>=15")
class DurableWorkerRepliesToReliableDelivery(unittest.TestCase):
    """第 5 格：真 helper 子进程（echo 模式）＋本地假 Hub。可靠外壳经 accept_delivery → inbox → delivery_worker_loop →
    process_inbound_message：正文只处理一次、ACK 按序、恰好一封 kind=reply 回到 origin，带 conversation_terminal 与 in_reply_to。"""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="sh-reliable-reply-")
        self.frames = []
        self.connected = threading.Event()
        self.loop = asyncio.new_event_loop()
        self.ws = None
        ready = threading.Event()

        async def handler(ws):
            self.ws = ws
            self.connected.set()
            async for raw in ws:
                try:
                    self.frames.append(json.loads(raw))
                except ValueError:
                    pass

        async def serve():
            self.server = await websockets.serve(handler, "127.0.0.1", 0)
            self.port = self.server.sockets[0].getsockname()[1]
            ready.set()

        def run():
            self.loop.run_until_complete(serve())
            self.loop.run_forever()

        self.thread = threading.Thread(target=run, daemon=True)
        self.thread.start()
        self.assertTrue(ready.wait(5))
        home = os.path.join(self.tmp, "home")
        os.makedirs(home)
        env = {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": home,
            "SH_WS_BASE": f"ws://127.0.0.1:{self.port}", "SH_SESSION_NAME": "seat", "SH_KEY_ID": "u_owner",
            "SH_SECRET": "s", "SH_STATE_DIR": os.path.join(self.tmp, "state"),
            "SH_MCP_IPC_SOCK": os.path.join(self.tmp, "ipc.sock"), "SH_INBOX_DB": os.path.join(self.tmp, "inbox.db"),
            "SH_MODE": "echo", "SH_MIRROR_TO": "", "SH_COLLECT": "0", "SH_NO_DIRECTORY": "1", "SH_PROXY": "off",
        }
        self.helper_log_path = os.path.join(self.tmp, "helper.log")
        self.helper_log = open(self.helper_log_path, "w")
        self.helper = subprocess.Popen([sys.executable, "-m", "sessionhelper"], cwd=_PKG_DIR, env=env,
                                       stdout=self.helper_log, stderr=subprocess.STDOUT)

    def tearDown(self):
        self.helper.kill()
        self.helper.wait(timeout=10)
        self.helper_log.close()

        async def shutdown():
            self.server.close()
            await self.server.wait_closed()

        asyncio.run_coroutine_threadsafe(shutdown(), self.loop).result(10)
        self.loop.call_soon_threadsafe(self.loop.stop)
        self.thread.join(5)

    def _wait(self, cond, timeout, what):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if cond():
                return
            time.sleep(0.1)
        with open(self.helper_log_path) as f:
            self.fail(f"timed out waiting for: {what}\n--- helper log ---\n{f.read()}")

    def _acks(self, delivery_id):
        return [f["meta"].get("state") for f in self.frames
                if f.get("meta", {}).get("type") == "delivery_ack" and f["meta"].get("delivery_id") == delivery_id]

    def _replies(self, to):
        return [f for f in self.frames if f.get("to") == to and f.get("meta", {}).get("type") != "delivery_ack"]

    def test_reliable_expect_reply_gets_exactly_one_terminal_reply_to_origin(self):
        self.assertTrue(self.connected.wait(20), "helper never connected")
        sender = "sender" + _HASH + "u_owner"
        body = "TASK-reliable-expect-reply ping"
        env = {
            "id": "m-1", "from": sender, "to": "seat" + _HASH + "u_owner", "body": body, "ts": int(time.time()),
            "meta": {"type": "delivery", "delivery_id": "dw-dlv_r5", "delivery_version": 1, "ack_required": True,
                     "source_message_id": "m-1", "reply_policy": "expect_reply", "kind": "content",
                     "hop_count": 1, "conversation_id": "conv-5"},
        }
        self._wait(lambda: any(f.get("meta", {}).get("type") == "session_capabilities" for f in self.frames), 20,
                   "helper capabilities frame")
        asyncio.run_coroutine_threadsafe(self.ws.send(json.dumps(env)), self.loop).result(5)
        self._wait(lambda: "processed" in self._acks("dw-dlv_r5"), 20, "processed ACK")
        time.sleep(1.5)  # 静置：看有没有第二封回信或重复处理
        with open(self.helper_log_path) as f:
            log = f.read()
        self.assertNotIn(f"suppress reply from={sender}: control", log, "reliable wrapper must not be judged control")
        replies = self._replies(sender)
        self.assertEqual(len(replies), 1, replies)
        meta = replies[0]["meta"]
        self.assertEqual((meta.get("kind"), meta.get("conversation_terminal"), meta.get("in_reply_to"),
                          meta.get("hop_count"), meta.get("conversation_id")),
                         ("reply", True, "m-1", 1, "conv-5"))
        self.assertNotIn("type", meta, "reply must not inherit the inbound transport wrapper type")
        self.assertIn(body, replies[0]["body"])
        acks = self._acks("dw-dlv_r5")
        self.assertEqual(acks.count("processed"), 1, acks)
        self.assertEqual(acks[-1], "processed", acks)
        self.assertEqual(acks, sorted(set(acks), key=acks.index), "ACK states must not repeat: %r" % acks)


if __name__ == "__main__":
    unittest.main()
