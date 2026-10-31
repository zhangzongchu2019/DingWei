"""Real private Unix IPC -> actual helper HTTP handler -> loopback fake Hub."""
import asyncio
import contextlib
import json
from pathlib import Path
import tempfile
import threading
import types
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

from sessionhelper import ipc_listener, mcp_shim
from sessionhelper.app import SessionHelper
from sessionhelper.ipc_limits import (IPC_REQUEST_MAX_BYTES, SPEC_BODY_MAX_BYTES,
                                      encode_spec_body)


def spec_body(size=70760):
    body = {"title": "中文规格", "content_markdown": "中" * 16943 + "x",
            "acceptance": [{"id": f"A{i:02}", "text": "核对标准", "owner_ref": "中",
                            "source": "ai", "cases": ["核验中文正文和证据"]} for i in range(41)],
            "input_anchors": [{"kind": "owner", "ref": "测试授权"}], "expected_revision": 1}
    padding = size - len(encode_spec_body(body))
    assert padding >= 0
    # The reported reproduction has 50,830 bytes of Chinese markdown and 41
    # criteria. Distribute ASCII padding across evidence rows, keeping that shape.
    for i, criterion in enumerate(body["acceptance"]):
        criterion["cases"][0] += "x" * (padding // 41 + (i < padding % 41))
    assert len(encode_spec_body(body)) == size
    return body


class SpecWriteIPCLimit(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.http_bodies, self.sends = [], []
        outer = self

        class Hub(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):
                raw = self.rfile.read(int(self.headers["Content-Length"]))
                outer.http_bodies.append(raw)
                self.send_response(201 if self.command == "POST" else 200)
                self.end_headers()
                self.wfile.write(b'{"id":"spec-local","current_revision":1,"revision":2}')

            do_PATCH = do_POST

        self.hub = ThreadingHTTPServer(("127.0.0.1", 0), Hub)
        self.thread = threading.Thread(target=self.hub.serve_forever, daemon=True)
        self.thread.start()
        owner = types.SimpleNamespace(enroll_credential=object(), _connection=lambda: (
            f"ws://127.0.0.1:{self.hub.server_port}/ws/agent", {"Authorization": "Bearer synthetic-limit-test"}))
        self.handler = SessionHelper._make_spec_write_handler(owner, None)

    async def asyncTearDown(self):
        await asyncio.to_thread(self.hub.shutdown)
        self.hub.server_close()
        self.thread.join()

    @contextlib.asynccontextmanager
    async def ipc(self, **mode):
        with tempfile.TemporaryDirectory(prefix="spec-limit-", dir="/tmp") as directory:
            path = str(Path(directory) / "ipc.sock")
            bound = asyncio.Event()

            async def send(to, body, meta):
                self.sends.append((to, body, meta))
                return {"sent": True}

            task = asyncio.create_task(ipc_listener.serve_ipc(
                path, send, spec_handler=self.handler, bound=bound, **mode))
            await asyncio.wait_for(bound.wait(), 2)
            try:
                with patch.object(mcp_shim, "_IPC_SOCK", path):
                    yield path
            finally:
                task.cancel()
                with contextlib.suppress(asyncio.CancelledError):
                    await task

    async def raw_request(self, path, payload):
        reader, writer = await asyncio.open_unix_connection(path)
        try:
            writer.write(json.dumps(payload, ensure_ascii=False).encode() + b"\n")
            await writer.drain()
            return json.loads(await asyncio.wait_for(reader.readline(), 3))
        finally:
            writer.close()
            await writer.wait_closed()

    def tool(self, body, update=True):
        args = dict(body, spec_id="spec-local") if update else body
        response = mcp_shim._dispatch(mcp_shim.Request("2.0", "tools/call", {
            "name": "dw_spec_update" if update else "dw_spec_create", "arguments": args}, 1))
        self.assertIsNone(response.error)
        return json.loads(response.result["content"][0]["text"])

    def assert_rejected(self, response, actual, limit):
        result = response["result"]
        self.assertEqual(result["code"], "request_too_large")
        self.assertEqual(result["outcome"], "rejected")
        self.assertFalse(result["retryable"])
        self.assertEqual((result["actual_bytes"], result["limit_bytes"]), (actual, limit))

    async def test_reported_70760_byte_update_reaches_hub(self):
        body = spec_body()
        self.assertEqual(len(body["content_markdown"].encode()), 50830)
        self.assertEqual(len(body["acceptance"]), 41)
        async with self.ipc():
            result = await asyncio.to_thread(self.tool, body)
        self.assertEqual(result["result"]["status"], "ok", "SPEC_70K_DID_NOT_REACH_HUB")
        self.assertEqual(self.http_bodies, [encode_spec_body(body)])

    async def test_exact_two_mib_create_reaches_hub(self):
        body = spec_body(SPEC_BODY_MAX_BYTES)
        async with self.ipc():
            result = await asyncio.to_thread(self.tool, body, False)
        self.assertEqual(result["result"]["status"], "ok", "SPEC_2MIB_DID_NOT_REACH_HUB")
        self.assertEqual(self.http_bodies, [encode_spec_body(body)])

    async def test_shim_rejects_body_limit_plus_one_without_socket_or_http(self):
        with patch.object(mcp_shim._socket, "socket", side_effect=AssertionError("oversize dispatched")):
            result = self.tool(spec_body(SPEC_BODY_MAX_BYTES + 1))
        self.assert_rejected(result, SPEC_BODY_MAX_BYTES + 1, SPEC_BODY_MAX_BYTES)
        self.assertEqual(self.http_bodies, [])

    async def test_raw_ipc_rejects_body_limit_plus_one_before_http(self):
        async with self.ipc() as path:
            result = await self.raw_request(path, {"type": "spec_write_v1", "action": "update",
                "protocol": "direct-v1", "spec_id": "spec-local", "body": spec_body(SPEC_BODY_MAX_BYTES + 1)})
        self.assert_rejected(result, SPEC_BODY_MAX_BYTES + 1, SPEC_BODY_MAX_BYTES)
        self.assertEqual(self.http_bodies, [])

    async def test_non_spec_keeps_exact_64k_boundary(self):
        async with self.ipc() as path:
            for size in (IPC_REQUEST_MAX_BYTES, IPC_REQUEST_MAX_BYTES + 1):
                payload = {"type": "mcp_send", "to": "fixture", "body": ""}
                payload["body"] = "x" * (size - len(json.dumps(payload).encode()))
                result = await self.raw_request(path, payload)
                if size == IPC_REQUEST_MAX_BYTES:
                    self.assertTrue(result["ok"])
                    self.assertTrue(result["outcome"]["sent"])
                else:
                    self.assert_rejected(result, size, IPC_REQUEST_MAX_BYTES)
        self.assertEqual(len(self.sends), 1)
        self.assertEqual(self.http_bodies, [])

    async def test_restricted_modes_do_not_gain_spec_write_permission(self):
        payload = {"type": "spec_write_v1", "action": "create", "protocol": "direct-v1", "body": spec_body()}
        for mode in ({"upload_only": True}, {"send_body_only": True}):
            with self.subTest(mode=mode):
                async with self.ipc(**mode) as path:
                    result = await self.raw_request(path, payload)
                self.assertTrue(not result["ok"] or result.get("result", {}).get("outcome") == "rejected")
        self.assertEqual(self.http_bodies, [])


if __name__ == "__main__":
    unittest.main()
