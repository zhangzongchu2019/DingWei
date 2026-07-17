import asyncio
import json
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

from sessionhelper.claudeDriver import ClaudeProtocolDriver, _TurnState
from sessionhelper.codexDriver import CodexProtocolDriver
from sessionhelper.config import load_config
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.openCodeDriver import OpenCodeProtocolDriver
from sessionhelper.app import SessionHelper


BASE_ENV = {
    "SH_SESSION_NAME": "dev",
    "SH_OWNER": "owner1",
    "SH_KEY_ID": "FB-0000",
    "SH_SECRET": "secret",
    "SH_HUB_URL": "ws://127.0.0.1:1/ws",
}


class _Ws:
    def __init__(self):
        self.sent = []

    async def send(self, value):
        self.sent.append(json.loads(value))


class DriverInterruptTest(unittest.TestCase):
    def test_claude_restart_hits_durable_ledger_without_stdin(self):
        with tempfile.TemporaryDirectory() as td:
            store = str(Path(td) / "ledger.sqlite3")
            first = ClaudeProtocolDriver(store_path=store)
            first._store.upsert("delivery-1", "turn-1", "hash", "processing", "sent")
            restarted = ClaudeProtocolDriver(store_path=store)
            restarted.health = lambda: True
            receipt = restarted.deliver("delivery-1", "same command")
            self.assertEqual(receipt.state, "processing")
            self.assertIn("idempotent hit", receipt.detail)

    def test_claude_interrupt_rejects_stale_target(self):
        with tempfile.TemporaryDirectory() as td:
            driver = ClaudeProtocolDriver(store_path=str(Path(td) / "ledger.sqlite3"))
            driver._active = _TurnState("delivery-1", "turn-1", threading.Event(), threading.Event())
            self.assertFalse(driver.interrupt_delivery("delivery-1", "turn-stale"))

    def test_codex_interrupt_uses_delivery_thread_turn_mapping(self):
        with tempfile.TemporaryDirectory() as td:
            driver = CodexProtocolDriver(store_path=str(Path(td) / "ledger.sqlite3"))
            driver._thread_id = "thread-exact"
            driver._store.upsert("thread-exact", "delivery-1", "turn-exact", "hash", "processing", "")
            driver._envelope_to_turn["delivery-1"] = "turn-exact"
            calls = []
            driver._request = lambda method, params, timeout=0: calls.append((method, params)) or {}
            driver._read_turn_state = lambda turn_id: ("failed", "interrupted")
            self.assertTrue(driver.interrupt_delivery("delivery-1", "turn-exact"))
            self.assertEqual(calls[0], ("turn/interrupt", {"threadId": "thread-exact", "turnId": "turn-exact"}))
            self.assertFalse(driver.interrupt_delivery("delivery-1", "turn-stale"))

    def test_codex_restart_hits_durable_ledger_without_turn_start(self):
        with tempfile.TemporaryDirectory() as td:
            store = str(Path(td) / "ledger.sqlite3")
            first = CodexProtocolDriver(store_path=store)
            first._store.upsert("thread-1", "delivery-1", "turn-1", "hash", "processing", "sent")
            restarted = CodexProtocolDriver(store_path=store)
            restarted._thread_id = "fresh-thread"
            restarted._session_id = "fresh-thread"
            restarted.health = lambda: True
            restarted._request = mock.Mock(side_effect=AssertionError("must not start a second turn"))
            receipt = restarted.deliver("delivery-1", "same command")
            self.assertEqual(receipt.state, "processing")
            self.assertEqual(restarted._thread_id, "thread-1")
            restarted._request.assert_not_called()

    def test_opencode_restart_hits_durable_ledger_without_post(self):
        with tempfile.TemporaryDirectory() as td:
            store = str(Path(td) / "ledger.sqlite3")
            first = OpenCodeProtocolDriver("pw", manage_server=False, store_path=store)
            first._store.upsert("delivery-1", "session-1", "msg-1", "accepted", "admitted")
            restarted = OpenCodeProtocolDriver("pw", manage_server=False, store_path=store)
            restarted._request = mock.Mock(side_effect=AssertionError("must not redeliver"))
            receipt = restarted.deliver("delivery-1", "same command")
            self.assertEqual(receipt.state, "accepted")
            restarted._request.assert_not_called()
            self.assertEqual(restarted._envelope_to_msg["delivery-1"], "msg-1")


class HelperInterruptTest(unittest.TestCase):
    def test_queue_cas_and_targeted_driver_interrupt(self):
        class Driver:
            def __init__(self):
                self.calls = []

            def interrupt_delivery(self, delivery_id, turn_id):
                self.calls.append((delivery_id, turn_id))
                return True

        adapter = DriverAdapter.__new__(DriverAdapter)
        from sessionhelper.unified_queue import UnifiedCommandQueue
        adapter._commands = UnifiedCommandQueue(3)
        adapter.driver = Driver()
        adapter._interrupt_confirmed = set()
        command = adapter._commands.enqueue({"id": "d1", "body": "one", "meta": {}}).command
        adapter._commands.start_next()
        adapter._commands.admit(command.delivery_id, "t1")
        self.assertEqual(adapter.interrupt_current(command.delivery_id, "stale")["state"], "stale")
        self.assertTrue(adapter.interrupt_current(command.delivery_id, "t1")["ok"])
        self.assertEqual(adapter.driver.calls, [(command.delivery_id, "t1")])

    def test_recv_control_path_sends_ack_without_fifo_wait(self):
        class Adapter:
            def interrupt_current(self, delivery_id, turn_id):
                return {"ok": True, "state": "interrupt_requested", "delivery_id": delivery_id, "turn_id": turn_id}

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="driver")))
        helper.adapter = Adapter()
        ws = _Ws()
        env = {"meta": {"type": "terminal_interrupt", "delivery_id": "d1", "turn_id": "t1"}}
        asyncio.run(helper.handle_terminal_interrupt(ws, env))
        self.assertEqual(ws.sent[0]["meta"]["type"], "terminal_interrupt_ack")
        self.assertTrue(ws.sent[0]["meta"]["ok"])


if __name__ == "__main__":
    unittest.main()
