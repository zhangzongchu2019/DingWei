"""Extract only recv_loop and pure classifiers; never import runtime/driver/config.

All WebSocket frames, helper state and business handlers are in-memory fakes.
No network, model, credential, home directory, inbox DB or real helper accesses.
"""
import ast
import asyncio
import contextlib
import copy
import io
import json
import re
from pathlib import Path
from types import SimpleNamespace
import unittest


PACKAGE = Path(__file__).resolve().parents[1] / "sessionhelper"


def extract(path, name, namespace, class_name=None):
    tree = ast.parse(path.read_text(encoding="utf-8"))
    body = tree.body
    if class_name:
        body = next(n for n in body if isinstance(n, ast.ClassDef) and n.name == class_name).body
    node = next(n for n in body if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and n.name == name)
    module = ast.fix_missing_locations(ast.Module(body=[copy.deepcopy(node)], type_ignores=[]))
    exec(compile(module, str(path), "exec"), namespace)
    return namespace[name]


NAMESPACE = {"asyncio": asyncio, "json": json, "re": re}
for name in ("is_mirror_control", "is_provision_envelope", "is_hub_notice_envelope", "is_no_mirror_envelope"):
    NAMESPACE[name] = lambda env: False
extract(PACKAGE / "routing.py", "is_spec_grant_response_envelope", NAMESPACE)
for name in ("is_online_directory_text", "is_client_onboarding_text"):
    extract(PACKAGE / "routing.py", name, NAMESPACE)
RECV = extract(PACKAGE / "app.py", "recv_loop", NAMESPACE, "SessionHelper")

GUIDE = "DingWei 客户端上线指南：sessionHelper 每分钟刷新在线清单"


class BufferedWS:
    def __init__(self, frames):
        self.frames = iter(frames)
        self.sent = []

    def __aiter__(self):
        return self

    async def __anext__(self):
        try:
            return json.dumps(next(self.frames))
        except StopIteration:
            raise StopAsyncIteration

    async def send(self, payload):
        self.sent.append(payload)


def helper():
    h = SimpleNamespace(processed=[], rosters=[], outbound_receipts=SimpleNamespace(receive=lambda _ws, _env: False))
    for name in ("is_file_capability_response", "is_terminal_interrupt", "is_terminal_input",
                 "is_terminal_resize", "is_terminal_refresh", "is_delivery", "is_delivery_cancel",
                 "is_agent_network_skill"):
        setattr(h, name, lambda env: False)
    h.write_online_list = h.rosters.append
    h.tree_snapshots = []
    h.write_online_tree = h.tree_snapshots.append
    h.remember_broadcast_mirror_decision = lambda env: None

    async def buffer(ws, env):
        return False

    async def business(ws, env):
        h.processed.append(env["id"])

    h.buffer_if_busy, h.process_inbound_message = buffer, business
    return h


def frame(body, ident="synthetic-guide"):
    return {"id": ident, "body": body, "from": "synthetic-hub", "to": "synthetic-helper"}


class ReceiveQuiescence(unittest.IsolatedAsyncioTestCase):
    async def test_spec_grant_response_uses_pure_classifier(self):
        h = helper()
        await RECV(h, BufferedWS([{**frame("control", "grant-1"), "meta": {"type": "spec_grant_response"}}]))
        self.assertEqual(h.processed, [], "retired control response must not reach business/model path")

    async def test_repeated_guides_are_not_injected_sent_or_logged_per_frame(self):
        h, ws = helper(), BufferedWS([frame(GUIDE) for _ in range(200)])
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            await RECV(h, ws)
        self.assertEqual(h.processed, [])
        self.assertEqual(ws.sent, [])
        self.assertEqual(output.getvalue().count("skipped cli injection for repeated client guide"), 1)

    async def test_buffered_guide_burst_does_not_starve_other_tasks(self):
        h, ws = helper(), BufferedWS([frame(GUIDE) for _ in range(200)])
        ticks = 0
        with contextlib.redirect_stdout(io.StringIO()):
            task = asyncio.create_task(RECV(h, ws))
            while not task.done():
                ticks += 1
                await asyncio.sleep(0)
            await task
        self.assertGreater(ticks, 2, "buffered guide burst monopolized the event loop")

    async def test_log_resets_per_connection_and_business_still_arrives(self):
        h, output = helper(), io.StringIO()
        with contextlib.redirect_stdout(output):
            for n in range(2):
                await RECV(h, BufferedWS([frame(GUIDE), frame(GUIDE), frame("business", str(n))]))
        self.assertEqual(h.processed, ["0", "1"])
        self.assertEqual(output.getvalue().count("skipped cli injection for repeated client guide"), 2)

    async def test_roster_control_remains_non_injecting(self):
        h = helper()
        text = "【DingWei 在线清单】**********synthetic-directory"
        await RECV(h, BufferedWS([frame(text)]))
        self.assertEqual(h.rosters, [text])
        self.assertEqual(h.tree_snapshots, [{}], "legacy directory resets the separate tree snapshot, never injects it")
        self.assertEqual(h.processed, [])

    async def test_normal_business_has_no_guide_warning(self):
        h, output = helper(), io.StringIO()
        with contextlib.redirect_stdout(output):
            await RECV(h, BufferedWS([frame("business", "business-1")]))
        self.assertEqual(h.processed, ["business-1"])
        self.assertNotIn("repeated client guide", output.getvalue())


if __name__ == "__main__":
    unittest.main()
