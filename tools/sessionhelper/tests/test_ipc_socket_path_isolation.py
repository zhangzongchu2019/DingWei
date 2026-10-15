"""Real _run_once and loopback WebSocket; no production Hub, credentials or CLI."""
import asyncio
import contextlib
import io
import json
import os
import tempfile
from types import SimpleNamespace
import unittest

import websockets
from sessionhelper.app import SessionHelper
from sessionhelper.ipc_listener import IPC_SOCKET_PATH_MAX_BYTES


class IPCStartupIsolationTests(unittest.IsolatedAsyncioTestCase):
    async def test_long_ipc_path_does_not_break_hub_connection_or_message_loops(self):
        connected, ipc_done = asyncio.Event(), asyncio.Event()
        exchanged = []

        async def hub(ws):
            capabilities = json.loads(await ws.recv())
            self.assertEqual(capabilities, {"type": "synthetic-capabilities"})
            connected.set()
            exchanged.append(await ws.recv())
            await ws.send("synthetic-inbound-after-ipc-failure")

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
                for name in ("_make_send_handler", "_make_file_handler", "_make_desktop_inbox_handler", "_make_spec_write_handler"):
                    setattr(helper, name, lambda ws: None)

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

                async def idle(*args):
                    await asyncio.Future()

                helper._ipc_server, helper.recv_loop = optional_ipc, business
                for name in ("online_list_heartbeat_loop", "delivery_worker_loop", "pending_drain_loop",
                             "file_inbound_worker_loop", "terminal_loop", "session_event_loop",
                             "agent_disk_metrics_loop", "outbox_loop", "mirror_loop"):
                    setattr(helper, name, idle)
                error_log = io.StringIO()
                with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(error_log):
                    await asyncio.wait_for(SessionHelper._run_once(helper), 3)
                self.assertEqual(exchanged, ["synthetic-outbound-after-ipc-failure", "synthetic-inbound-after-ipc-failure"])
                self.assertEqual(error_log.getvalue().count("[ipc_listener] ERROR"), 1)
                self.assertNotIn(path, error_log.getvalue())
                self.assertFalse(helper.ipc_bound.is_set())
                self.assertFalse(os.path.exists(path))


if __name__ == "__main__":
    unittest.main()
