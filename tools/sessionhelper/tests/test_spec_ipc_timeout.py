"""Real local Unix IPC timings; no Hub, grants, network HTTP or production writes."""
import asyncio
import contextlib
import json
import tempfile
import threading
import time
import types
import unittest
from pathlib import Path
from unittest.mock import patch

from sessionhelper import ipc_listener, mcp_shim


def body(update=False):
    value = dict(title="draft", content_markdown="body", acceptance=[], input_anchors=[])
    if update:
        value.update(spec_id="spec-test", expected_revision=1)
    return value


class SpecIPCTimeouts(unittest.IsolatedAsyncioTestCase):
    @contextlib.asynccontextmanager
    async def server(self, handler):
        with tempfile.TemporaryDirectory(prefix="spec-o3-") as directory:
            path = str(Path(directory) / "ipc.sock")
            bound = asyncio.Event()
            async def unused(*args):
                raise AssertionError("unexpected send")
            task = asyncio.create_task(ipc_listener.serve_ipc(path, unused, spec_handler=handler, bound=bound))
            await asyncio.wait_for(bound.wait(), 2)
            try:
                with patch.object(mcp_shim, "_IPC_SOCK", path):
                    yield
            finally:
                task.cancel()
                with contextlib.suppress(asyncio.CancelledError):
                    await task

    async def test_create_and_update_success_after_real_twenty_seconds(self):
        async def handler(args):
            await asyncio.sleep(20)
            return dict(status="ok", spec_id="spec-test", revision=2, action=args["action"])
        started = time.monotonic()
        async with self.server(handler):
            results = await asyncio.gather(
                asyncio.to_thread(mcp_shim._dw_spec_create, body()),
                asyncio.to_thread(mcp_shim._dw_spec_update, body(True)),
            )
        self.assertGreaterEqual(time.monotonic() - started, 20)
        for result in results:
            self.assertTrue(result["ok"])
            self.assertEqual(result["result"]["status"], "ok")
        self.assertEqual({r["result"]["action"] for r in results}, {"create", "update"})

    async def test_helper_over_budget_returns_explicit_result_not_socket_timeout(self):
        cancelled = asyncio.Event()
        async def handler(args):
            try:
                await asyncio.sleep(31)
            finally:
                cancelled.set()
        started = time.monotonic()
        async with self.server(handler):
            response = await asyncio.to_thread(mcp_shim._dw_spec_create, body())
        self.assertGreaterEqual(time.monotonic() - started, 30)
        self.assertTrue(cancelled.is_set())
        self.assertTrue(response["ok"])
        self.assertEqual(response["result"]["code"], "spec_write_deadline_exceeded")
        self.assertEqual(response["result"]["outcome"], "unknown")
        self.assertFalse(response["result"]["retryable"])

    async def test_blocking_agent_http_does_not_block_ipc_deadline(self):
        # Actual App handler, synthetic grant and intercepted HTTP only. A held
        # worker must not hold the IPC event loop or fabricate a write rollback.
        from sessionhelper.app import SessionHelper
        entered, release = threading.Event(), threading.Event()
        async def granted(*args):
            return {"status": "granted", "capability": "test-only"}
        owner = types.SimpleNamespace(enroll_credential=object(),
                                      _connection=lambda: ("wss://hub.invalid/v2c", {}))
        def blocked_http(*args, **kwargs):
            entered.set()
            release.wait(3)
            raise OSError("test-only late failure")
        handler = SessionHelper._make_spec_write_handler(owner, None)
        try:
            with patch("urllib.request.urlopen", side_effect=blocked_http), patch.object(ipc_listener, "_SPEC_HANDLER_TIMEOUT", .1):
                async with self.server(handler):
                    result = await asyncio.to_thread(mcp_shim._dw_spec_create, body())
                self.assertTrue(entered.is_set())
                self.assertFalse(release.is_set())
                self.assertEqual(result["result"]["code"], "spec_write_deadline_exceeded")
        finally:
            release.set()


class OtherToolBudget(unittest.TestCase):
    def test_only_spec_tools_override_five_second_default(self):
        fake = types.SimpleNamespace(settimeout=lambda n: seen.append(n), connect=lambda p: None,
                                     sendall=lambda b: None, recv=lambda n: b'{"ok":true,"result":{}}\n', close=lambda: None)
        seen = []
        with patch.object(mcp_shim._socket, "socket", return_value=fake):
            mcp_shim._ipc_request({"type": "other"})
            mcp_shim._dw_spec_create(body())
            mcp_shim._dw_spec_update(body(True))
        self.assertEqual(seen, [5.0, 35.0, 35.0])
        self.assertGreater(mcp_shim._SPEC_IPC_TIMEOUT, ipc_listener._SPEC_HANDLER_TIMEOUT)
        self.assertGreaterEqual(ipc_listener._SPEC_HANDLER_TIMEOUT, 10 + 15 + 5)


if __name__ == "__main__":
    unittest.main()
