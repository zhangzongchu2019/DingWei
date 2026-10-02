"""Agent 间可靠投递的正文在 cli 模式下注入 CLI（组长 R-B12-SEATMODE 第 3 条，并入本席复现 S2b）。

真 helper（子进程 `python -m sessionhelper`）＋本地假 Hub＋假 CLI：假 Hub 只发 Hub 发给 helper 的帧，
持久投递外壳与 internal/m8/hub.go 可靠投递改写同形（meta.type=delivery、delivery_id、ack_required…）；
假 CLI 打印 codex 档的就绪提示符，把收到的每一行记进文件。

钉住两件事：
1 cli 模式下，来自别的 Agent 的可靠投递正文会注入 CLI（假 CLI 的记录里出现原文），并回 injected ACK；
2 现状：这类投递的结果不回信给发送方（routing.classify_reply_decision 把 type=delivery 外壳判 control）——
  只记现状，改不改由组长定；改了这一格要随之改。
需要 websockets>=15 与 pexpect（helper 运行依赖）；缺则跳过。
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
    import websockets
    import pexpect  # noqa: F401  (helper 子进程的 cli 适配器要用)
    _WS_OK = int(websockets.__version__.split(".")[0]) >= 15
except ImportError:  # pragma: no cover
    websockets = None
    _WS_OK = False

_PKG_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_HASH = "\x23"  # 地址分隔符（会话名与 key 之间）

_FAKE_CLI = r'''#!/usr/bin/env python3
import os, sys
log = open(os.environ["FAKECLI_LOG"], "a", buffering=1)
sys.stdout.write("Welcome to fake codex\n› "); sys.stdout.flush()
for line in sys.stdin:
    log.write("INJECTED " + line.rstrip("\n") + "\n")
    sys.stdout.write("fake-answer\n› "); sys.stdout.flush()
'''


@unittest.skipUnless(_WS_OK, "needs websockets>=15 and pexpect")
class DurableAgentDeliveryInjectsCLI(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="sh-durable-cli-")
        self.cli_log = os.path.join(self.tmp, "cli.log")
        self.cli = os.path.join(self.tmp, "fakecli.py")
        with open(self.cli, "w") as f:
            f.write(_FAKE_CLI)
        os.chmod(self.cli, 0o755)
        self.frames = []  # helper 发回的帧
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
            "SH_SECRET": "s", "SH_STATE_DIR": os.path.join(self.tmp, "state"), "SH_MCP_IPC_SOCK": os.path.join(self.tmp, "ipc.sock"),
            "SH_MODE": "cli", "SH_CLI": "codex", "SH_CLI_CMD": self.cli, "SH_CLI_CWD": home,
            "SH_CLI_READY_TIMEOUT": "20", "SH_CLI_REPLY_WAIT": "5", "FAKECLI_LOG": self.cli_log,
        }
        self.helper_log = open(os.path.join(self.tmp, "helper.log"), "w")
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
        self.fail(f"timed out waiting for: {what}")

    def _acks(self, delivery_id):
        return [f.get("meta", {}).get("state") for f in self.frames
                if f.get("meta", {}).get("type") == "delivery_ack" and f.get("meta", {}).get("delivery_id") == delivery_id]

    def test_durable_delivery_from_another_agent_is_injected_and_not_replied(self):
        self.assertTrue(self.connected.wait(20), "helper never connected")
        body = "TASK-durable-from-agent hello"
        env = {
            "id": "m-1", "from": "sender" + _HASH + "u_owner", "to": "seat" + _HASH + "u_owner", "body": body,
            "ts": int(time.time()),
            "meta": {"type": "delivery", "delivery_id": "dw-dlv_t1", "delivery_version": 1,
                     "ack_required": True, "source_message_id": "m-1", "reply_policy": "expect_reply"},
        }
        # 等 CLI 起来（cli 模式下 helper 连上即后台起 CLI）再投，免得落进 busy
        self._wait(lambda: any(f.get("meta", {}).get("type") == "terminal_output" for f in self.frames), 20,
                   "CLI started (terminal_output)")
        asyncio.run_coroutine_threadsafe(self.ws.send(json.dumps(env)), self.loop).result(5)
        self._wait(lambda: os.path.exists(self.cli_log) and body in open(self.cli_log).read(), 20,
                   "fake CLI received the injected body")
        self._wait(lambda: "injected" in self._acks("dw-dlv_t1"), 10, "injected ACK")
        self._wait(lambda: "processed" in self._acks("dw-dlv_t1"), 60, "processed ACK")
        replies = [f for f in self.frames if f.get("to") == "sender" + _HASH + "u_owner"]
        self.assertEqual(replies, [], "durable delivery result is currently not replied to the sending agent")
        with open(os.path.join(self.tmp, "helper.log")) as f:
            self.assertIn("suppress reply from=sender" + _HASH + "u_owner: control", f.read())


if __name__ == "__main__":
    unittest.main()
