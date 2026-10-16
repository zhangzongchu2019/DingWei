import asyncio
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from sessionhelper.desktop_pilot import Pilot
from sessionhelper.mcp_shim import _desktop_inbox, _desktop_wait, _desktop_reply, _send_message
from sessionhelper.setup import profile, write_profile


class DesktopPilotTest(unittest.TestCase):
    def test_wait_pilot_posts_only_after_real_ipc_wait_and_never_fakes_hub_receipt(self):
        async def exercise(root):
            pilot = Pilot(write_profile(profile("desktop", "codex", "https://fixture.invalid", "fixtureuser", "fx", "pmfixture", root)), message_after_wait=True)
            stop, ready = asyncio.Event(), asyncio.Event()
            task = asyncio.create_task(pilot.run(stop, ready))
            await asyncio.wait_for(ready.wait(), 5)
            try:
                self.assertEqual(pilot.adapter.request({"operation":"status"})["unread"], 0)
                self.assertFalse(pilot.report["fixture_posted"])
                with patch("sessionhelper.mcp_shim._IPC_SOCK", pilot.socket):
                    result = await asyncio.to_thread(_desktop_wait, {"wait_seconds":5})
                    self.assertEqual(result["message"]["id"], pilot.message_id)
                    self.assertFalse(result["model_execution_verified"])
                    with self.assertRaisesRegex(RuntimeError, "helper_rejected"):
                        await asyncio.to_thread(_desktop_reply, {"message_id":pilot.message_id,"body":"DW_DESKTOP_OK"})
                    reply = await asyncio.to_thread(_send_message, {"to":pilot.target,"body":"DW_DESKTOP_OK","in_reply_to":pilot.message_id})
                    self.assertFalse(reply["sent_to_hub"])
                self.assertEqual(pilot.report["wait_return_count"],1)
                self.assertEqual(pilot.report["hub_connections"],0)
                self.assertEqual(pilot.adapter.request({"operation":"status"})["unread"],1)
            finally:
                stop.set();await task
            self.assertFalse(pilot.adapter._wait_guard.locked())
            self.assertFalse(Path(pilot.socket).exists())
        with tempfile.TemporaryDirectory(prefix="dwp-",dir="/tmp") as root:
            asyncio.run(exercise(root))

    def test_paused_wait_pilot_does_not_create_fixture(self):
        async def exercise(root):
            pilot = Pilot(write_profile(profile("desktop", "codex", "https://fixture.invalid", "fixtureuser", "fx", "pmfixture", root)), message_after_wait=True)
            stop, ready = asyncio.Event(), asyncio.Event()
            task = asyncio.create_task(pilot.run(stop, ready))
            await asyncio.wait_for(ready.wait(),5)
            try:
                pilot.adapter.request({"operation":"pause"})
                with patch("sessionhelper.mcp_shim._IPC_SOCK",pilot.socket):
                    result = await asyncio.to_thread(_desktop_wait,{"wait_seconds":5})
                self.assertEqual(result["state"],"paused")
                self.assertFalse(pilot.report["fixture_posted"])
                self.assertEqual(pilot.adapter.request({"operation":"status"})["unread"],0)
            finally:
                stop.set();await task
            self.assertFalse(pilot.adapter._wait_guard.locked())
        with tempfile.TemporaryDirectory(prefix="dwp-",dir="/tmp") as root:
            asyncio.run(exercise(root))

    def test_real_local_read_reply_no_hub_and_owned_cleanup(self):
        async def exercise(root):
            values = profile("desktop", "codex", "https://fixture.invalid", "fixtureuser", "fx", "pmfixture", root)
            pilot = Pilot(write_profile(values))
            stop, ready = asyncio.Event(), asyncio.Event()
            task = asyncio.create_task(pilot.run(stop, ready))
            await asyncio.wait_for(ready.wait(), 5)
            try:
                with patch("sessionhelper.mcp_shim._IPC_SOCK", pilot.socket):
                    await asyncio.to_thread(_desktop_inbox, {"operation": "read", "message_id": pilot.message_id})
                    result = await asyncio.to_thread(_send_message, {"to": pilot.target, "body": "DW_DESKTOP_OK", "in_reply_to": pilot.message_id})
                    self.assertFalse(result["sent_to_hub"])
                    with self.assertRaises(RuntimeError):
                        await asyncio.to_thread(_send_message, {"to": "real-target", "body": "DW_DESKTOP_OK", "in_reply_to": pilot.message_id})
                report = json.loads(pilot.report_path.read_text())
                self.assertEqual((report["read_count"], report["reply_count"], report["hub_connections"]), (1, 1, 0))
                self.assertEqual(report["native_auto_notify"], "NOT_VERIFIED")
                self.assertEqual(pilot.report_path.stat().st_mode & 0o777, 0o600)
            finally:
                stop.set(); await task
            self.assertFalse(os.path.lexists(pilot.socket))
            self.assertFalse((Path(root) / "desktop-pilot.lock").exists())
            self.assertTrue(pilot.report_path.exists())
        with tempfile.TemporaryDirectory(prefix="dwp-", dir="/tmp") as root:
            asyncio.run(exercise(root))

    def test_cleanup_preserves_concurrent_replacement(self):
        async def exercise(root):
            pilot = Pilot(write_profile(profile("desktop", "codex", "https://fixture.invalid", "fixtureuser", "fx", "pmfixture", root)))
            stop, ready = asyncio.Event(), asyncio.Event()
            task = asyncio.create_task(pilot.run(stop, ready))
            await asyncio.wait_for(ready.wait(), 5)
            Path(pilot.socket).unlink()
            Path(pilot.socket).write_text("preserve-concurrent-owner")
            stop.set(); await task
            self.assertEqual(Path(pilot.socket).read_text(), "preserve-concurrent-owner")
        with tempfile.TemporaryDirectory(prefix="dwp-", dir="/tmp") as root:
            asyncio.run(exercise(root))

    def test_wrong_profile_name_never_read(self):
        with tempfile.TemporaryDirectory(prefix="dwp-", dir="/tmp") as root:
            path = Path(root) / "credential.json"
            path.write_text("preserve-private-fixture")
            path.chmod(0o600)
            with patch("pathlib.Path.read_text", side_effect=AssertionError("credentials must not be read")):
                with self.assertRaises(ValueError): Pilot(path)

    def test_existing_credentials_or_socket_never_replaced(self):
        with tempfile.TemporaryDirectory(prefix="dwp-", dir="/tmp") as root:
            path = write_profile(profile("desktop", "codex", "https://fixture.invalid", "fixtureuser", "fx", "pmfixture", root))
            for name in ("credential.json", "dingwei-mcp.sock"):
                owned = Path(root) / name
                owned.write_text("preserve")
                with self.assertRaises(ValueError): Pilot(path)
                self.assertEqual(owned.read_text(), "preserve")
                owned.unlink()


if __name__ == "__main__":
    unittest.main()
