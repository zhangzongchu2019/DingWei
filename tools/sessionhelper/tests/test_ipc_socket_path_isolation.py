"""Actual _run_once/reconnects and loopback WS; no production Hub, credentials or CLI."""
import asyncio
import contextlib
import io
import json
import os
import tempfile
from types import MethodType, SimpleNamespace
import unittest
from unittest import mock

import websockets
from sessionhelper.app import SessionHelper
from sessionhelper.ipc_listener import IPC_SOCKET_PATH_MAX_BYTES


class IPCStartupIsolationTests(unittest.IsolatedAsyncioTestCase):
    async def scenario(self, unexpected_failure=False):
        connected, ipc_done = asyncio.Event(), asyncio.Event()
        exchanged, connections = [], []

        async def hub(ws):
            self.assertEqual(json.loads(await ws.recv()), {"type": "synthetic-capabilities"})
            connections.append(ws)
            connected.set()
            exchanged.append(await ws.recv())
            await ws.send("synthetic-inbound-after-ipc-failure")
            await ws.close()  # Actual disconnect, then caller makes a new connection.

        async with websockets.serve(hub, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            with tempfile.TemporaryDirectory(dir="/tmp", prefix="ipc-life-") as root:
                path = root + "/" + "x" * (IPC_SOCKET_PATH_MAX_BYTES + 1 - len(os.fsencode(root + "/")))
                helper = SimpleNamespace(
                    cfg=SimpleNamespace(proxy="", debug=False, open_timeout=2,
                                        mode="driver", producer=False,
                                        mcp_ipc_sock=path, ipc_upload_only=False),
                    book=SimpleNamespace(self_addr="synthetic-helper"),
                    provisioner=SimpleNamespace(confirm_update_connected=lambda: None),
                    outbound_receipts=SimpleNamespace(abort=lambda ws: None),
                    upload_policy=SimpleNamespace(enabled=False), ipc_bound=asyncio.Event(),
                    _connection=lambda: (f"ws://127.0.0.1:{port}", {}),
                    session_capabilities_envelope=lambda _: {"type": "synthetic-capabilities"},
                    _make_vsapi_handler=lambda: None,
                )
                for name in ("_make_send_handler", "_make_file_handler", "_make_desktop_inbox_handler", "_make_spec_write_handler", "_make_delivery_status_handler"):
                    setattr(helper, name, lambda ws: None)

                helper._make_task_flow_handler = lambda: None
                helper._make_project_context_handler = lambda: None
                helper._bind_connection_agent_identity = MethodType(
                    SessionHelper._bind_connection_agent_identity, helper)

                async def optional_ipc(ws):
                    try:
                        await SessionHelper._ipc_server(helper, ws)
                    finally:
                        ipc_done.set()

                async def business(ws):
                    await connected.wait()
                    await ipc_done.wait()
                    self.assertFalse(helper.ipc_bound.is_set())
                    await ws.send("synthetic-outbound-after-ipc-failure")
                    exchanged.append(await ws.recv())
                    await ws.wait_closed()

                async def idle(*args):
                    await asyncio.Future()

                helper._ipc_server, helper.recv_loop = optional_ipc, business
                for name in ("online_list_heartbeat_loop", "delivery_worker_loop", "delivery_ack_outbox_loop", "pending_drain_loop",
                             "file_inbound_worker_loop", "terminal_loop", "session_event_loop",
                             "agent_disk_metrics_loop", "inbox_progress_loop", "outbox_loop", "mirror_loop"):
                    setattr(helper, name, idle)
                error_log = io.StringIO()
                async def failed_listener(*args, **kwargs):
                    raise OSError("synthetic-private-path-DO-NOT-LOG")
                patch = mock.patch("sessionhelper.ipc_listener.serve_ipc", failed_listener) if unexpected_failure else contextlib.nullcontext()
                with patch, contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(error_log):
                    for _ in range(2):
                        connected.clear()
                        ipc_done.clear()
                        await asyncio.wait_for(SessionHelper._run_once(helper), 3)
                        self.assertFalse(helper.ipc_bound.is_set())
                self.assertEqual(len(connections), 2)
                self.assertIsNot(connections[0], connections[1])
                self.assertEqual(exchanged, ["synthetic-outbound-after-ipc-failure", "synthetic-inbound-after-ipc-failure"] * 2)
                self.assertNotIn(path, error_log.getvalue())
                self.assertNotIn("synthetic-private-path-DO-NOT-LOG", error_log.getvalue())
                self.assertFalse(os.path.exists(path))
                if unexpected_failure:
                    self.assertEqual(error_log.getvalue().count("listener task failed (OSError)"), 2)
                else:
                    # The same fixed invalid config is diagnosed once, not on every reconnect.
                    self.assertEqual(error_log.getvalue().count("[ipc_listener] ERROR"), 1)
                    self.assertIn("108 bytes; maximum 107 bytes" if IPC_SOCKET_PATH_MAX_BYTES == 107 else "104 bytes; maximum 103 bytes", error_log.getvalue())

    async def test_long_path_messages_and_reconnect_continue_with_one_error(self):
        await self.scenario()

    async def test_unexpected_ipc_task_error_is_consumed_during_cleanup(self):
        await self.scenario(unexpected_failure=True)


if __name__ == "__main__":
    unittest.main()
