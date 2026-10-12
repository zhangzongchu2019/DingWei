import asyncio
import json
import os
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from sessionhelper.desktop import DesktopAdapter
from sessionhelper.ipc_listener import serve_ipc
from sessionhelper import mcp_shim


class DesktopTest(unittest.TestCase):
    def test_persistent_inbox_no_false_injection(self):
        with tempfile.TemporaryDirectory() as root:
            cfg = SimpleNamespace(inbox_db=os.path.join(root, "fixture"))
            adapter = DesktopAdapter(cfg)
            injected = []
            envelope = {"id": "fixture-message", "from": "sender", "body": "fixture question", "meta": {"delivery_id": "fixture-delivery"}}
            adapter.handle_delivery(envelope, lambda: injected.append(True))
            adapter.handle_delivery(envelope)
            reopened = DesktopAdapter(cfg)
            self.assertEqual(injected, [])
            self.assertEqual(reopened.request({"operation": "status"})["unread"], 1)
            self.assertEqual(reopened.request({"operation": "read", "message_id": "fixture-message"})["message"]["body"], "fixture question")
            reopened.request({"operation": "ack", "message_id": "fixture-message"})
            self.assertEqual(reopened.request({"operation": "list"})["messages"], [])
            self.assertEqual(reopened.request({"operation": "status"})["native_auto_notify"], "NOT_VERIFIED")
            reopened.request({"operation": "pause"})
            self.assertTrue(DesktopAdapter(cfg).request({"operation": "status"})["paused"])

    def test_inbox_rejects_symlink_without_touching_target(self):
        with tempfile.TemporaryDirectory() as root:
            target = os.path.join(root, "owned-file")
            with open(target, "w") as output:
                output.write("preserve")
            os.symlink(target, os.path.join(root, "fixture.desktop.sqlite"))
            with self.assertRaises(ValueError):
                DesktopAdapter(SimpleNamespace(inbox_db=os.path.join(root, "fixture")))
            with open(target) as source:
                self.assertEqual(source.read(), "preserve")

    def test_real_local_ipc_mcp_inbox(self):
        async def exercise(root):
            adapter = DesktopAdapter(SimpleNamespace(inbox_db=os.path.join(root, "fixture")))
            adapter.handle_delivery({"id": "fixture-message", "from": "sender", "body": "test"})
            path = os.path.join(root, "ipc.sock")
            bound = asyncio.Event()
            async def send(*args):
                raise AssertionError("inbox read must not send to Hub")
            async def inbox(args):
                return adapter.request(args)
            task = asyncio.create_task(serve_ipc(path, send, bound=bound, inbox_handler=inbox))
            try:
                await asyncio.wait_for(bound.wait(), 2)
                previous = mcp_shim._IPC_SOCK
                mcp_shim._IPC_SOCK = path
                try:
                    result = await asyncio.to_thread(mcp_shim._desktop_inbox, {"operation": "read", "message_id": "fixture-message"})
                    self.assertEqual(result["message"]["body"], "test")
                finally:
                    mcp_shim._IPC_SOCK = previous
            finally:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)
        with tempfile.TemporaryDirectory() as root:
            asyncio.run(exercise(root))

    def test_inbox_mcp_cannot_override_ipc_operation(self):
        with patch("sessionhelper.mcp_shim._ipc_request") as ipc:
            for args in ({"operation": "status", "type": "mcp_send"}, {"operation": "list", "limit": True}, {"operation": "send"}):
                with self.assertRaises(ValueError):
                    mcp_shim._desktop_inbox(args)
            ipc.assert_not_called()


if __name__ == "__main__":
    unittest.main()
