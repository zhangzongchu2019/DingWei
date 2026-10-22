"""Completed Codex item backfill stays view-only and is capability gated."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from sessionhelper.codexDriver import CodexProtocolDriver
from sessionhelper.contract import SessionEvent
from sessionhelper.config import load_config
from sessionhelper.driver_adapter import DriverAdapter

try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tests._home_isolation import HomeIsolation

_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


class Rpc:
    def __init__(self):
        self.calls = []
        self.alive = True
        self.result = {"data": []}
        self.error = None

    def healthy(self):
        return self.alive

    def stop(self):
        self.alive = False

    def request(self, method, params, timeout=0):
        self.calls.append((method, params, timeout))
        if self.error:
            raise self.error
        if method == "thread/turns/list":
            return self.result
        return {}

    def notify(self, *_args):
        pass


class CodexItemBackfill(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="item-backfill-")
        self.addCleanup(self.tmp.cleanup)
        self.driver = CodexProtocolDriver(
            workdir=self.tmp.name,
            store_path=str(Path(self.tmp.name) / "ledger.sqlite3"),
            view_item_backfill=True,
        )
        self.rpc = Rpc()
        self.driver._rpc = self.rpc
        self.driver._initialized = True
        self.driver._thread_id = self.driver._session_id = "thread-bound"
        self.driver._connection_generation = 2
        self.driver._backfill_pending = True
        self.driver._validate_bound_thread = mock.Mock()

    def test_reconnect_backfill_subscribes_first_and_replaces_completed_item(self):
        self.rpc.result = {"data": [
            {"id": "turn-active", "status": "inProgress", "items": [
                {"id": "user-2", "type": "userMessage", "clientId": "delivery-2"},
                {"id": "item-active", "type": "agentMessage", "status": "completed", "text": "completed in active turn"},
                {"id": "item-open", "type": "agentMessage", "status": "inProgress", "text": "partial"},
            ]},
            {"id": "turn-done", "status": "completed", "items": [
                {"id": "user-1", "type": "userMessage", "clientId": "delivery-1"},
                {"id": "item-done", "type": "agentMessage", "text": "full answer"},
            ]},
        ]}
        seen_subscribed = []
        original_request = self.rpc.request

        def request(method, params, timeout=0):
            if method == "thread/turns/list":
                seen_subscribed.append(bool(self.driver._consumers))
            return original_request(method, params, timeout)

        self.rpc.request = request
        delta = SessionEvent(kind="assistant_text", text="partial delta", data={"thread_id": "thread-bound", "item_id": "item-done"},
                             session_id="thread-bound", cursor="live:1", delivery_id="delivery-1", turn_id="turn-done")
        self.driver._emit_event(delta)
        before_state = dict(self.driver._envelope_states)
        before_turns = dict(self.driver._turn_to_envelope)
        with mock.patch.object(self.driver, "_remember_delivery", side_effect=AssertionError("BACKFILL_MUTATED_DELIVERY")), \
             mock.patch.object(self.driver._store, "update_state", side_effect=AssertionError("BACKFILL_ACK_OR_RECEIPT_WRITE")):
            stream = self.driver.events()
            got = [next(stream), next(stream), next(stream)]
            stream.close()
        self.assertEqual(seen_subscribed, [True], "BACKFILL_SUBSCRIBE_AFTER_SNAPSHOT")
        self.driver._validate_bound_thread.assert_called_once_with()
        call = next(c for c in self.rpc.calls if c[0] == "thread/turns/list")
        self.assertEqual(call[1], {"threadId": "thread-bound", "limit": 20, "itemsView": "full", "sortDirection": "desc"})
        recovered = [ev for ev in got if ev.data.get("backfill") is True]
        self.assertEqual([(ev.turn_id, ev.data["item_id"], ev.text) for ev in recovered], [
            ("turn-done", "item-done", "full answer"),
            ("turn-active", "item-active", "completed in active turn"),
        ], "BACKFILL_MISSING_OR_INCOMPLETE_ITEM_FILTER_WITHDRAWN")
        self.assertEqual(recovered[0].cursor, "turn:turn-done:item:0002")
        self.assertEqual(recovered[0].delivery_id, "delivery-1")
        self.assertTrue(all(ev.data["thread_id"] == "thread-bound" and ev.data["replay"] for ev in recovered))
        self.assertEqual(self.driver._envelope_states, before_state)
        self.assertEqual(self.driver._turn_to_envelope, before_turns)
        self.assertNotIn("turn/start", [c[0] for c in self.rpc.calls])
        self.assertNotIn("thread/inject_items", [c[0] for c in self.rpc.calls])

    def test_capability_defaults_off_and_nonmatching_value_does_not_read_history(self):
        disabled = CodexProtocolDriver(store_path=str(Path(self.tmp.name) / "off.db"))
        disabled._thread_id = "thread-bound"
        self.assertEqual(disabled._take_completed_item_backfill(), [])
        self.assertEqual(self.rpc.calls, [])
        self.assertFalse(disabled._view_item_backfill)
        env = {"SH_DRIVER": "codex", "SH_SESSION_NAME": "fixture", "SH_OWNER": "owner",
               "SH_KEY_ID": "key", "SH_SECRET": "secret", "SH_PROVISION_NO_EXIT": "1",
               "SH_VIEW_ITEM_BACKFILL": "item-backfill/v1"}
        with mock.patch.dict(os.environ, env):
            adapter = DriverAdapter(load_config(env))
        self.assertTrue(adapter.driver._view_item_backfill, "DECLARED_CAPABILITY_NOT_ENABLED")
        with mock.patch.dict(os.environ, {**env, "SH_VIEW_ITEM_BACKFILL": "item-backfill/v2"}):
            old_view = DriverAdapter(load_config(env))
        self.assertFalse(old_view.driver._view_item_backfill, "UNKNOWN_CAPABILITY_VALUE_ENABLED")

    def test_only_reconnect_marks_backfill_pending(self):
        self.driver._backfill_pending = False
        self.rpc.alive = False
        def reconnect():
            self.driver._rpc = self.rpc
            self.driver._connection_generation += 1
            self.rpc.alive = True
        self.driver._start_locked = reconnect
        self.driver.start()
        self.assertTrue(self.driver._backfill_pending, "RECONNECT_BACKFILL_NOT_SCHEDULED")
        disabled = CodexProtocolDriver(store_path=str(Path(self.tmp.name) / "disabled.db"))
        disabled._thread_id = "thread-bound"
        disabled._connection_generation = 1
        disabled._rpc = self.rpc
        disabled._start_locked = lambda: None
        self.rpc.alive = False
        disabled.start()
        self.assertFalse(disabled._backfill_pending, "CAPABILITY_GATE_BYPASSED")

    def test_wrong_bound_thread_fails_closed_without_history_read(self):
        self.rpc.result = {"data": [{"id": "turn-secret", "status": "completed", "items": [
            {"id": "item-secret", "type": "agentMessage", "text": "must not leak"},
        ]}]}
        self.driver._validate_bound_thread.side_effect = RuntimeError("binding mismatch")
        self.assertEqual(self.driver._take_completed_item_backfill(), [])
        self.driver._validate_bound_thread.assert_called_once_with()
        self.assertEqual(self.rpc.calls, [], "WRONG_THREAD_HISTORY_READ")
        self.assertNotIn("turn/start", [c[0] for c in self.rpc.calls])

    def test_incomplete_agent_item_is_not_backfilled(self):
        self.rpc.result = {"data": [{"id": "turn-open", "status": "inProgress", "items": [
            {"id": "item-done", "type": "agentMessage", "status": "completed", "text": "complete"},
            {"id": "item-open", "type": "agentMessage", "status": "inProgress", "text": "partial"},
            {"id": "item-unknown", "type": "agentMessage", "text": "unconfirmed"},
        ]}]}
        recovered = self.driver._take_completed_item_backfill()
        self.assertEqual([(ev.data["item_id"], ev.text) for ev in recovered], [("item-done", "complete")],
                         "BACKFILL_INCOMPLETE_ITEM_LEAKED")

    def test_history_failure_is_view_only_and_never_starts_or_acks_a_turn(self):
        self.rpc.error = TimeoutError("private rpc details")
        before_state = dict(self.driver._envelope_states)
        before_turns = dict(self.driver._turn_to_envelope)
        with mock.patch.object(self.driver, "_remember_delivery", side_effect=AssertionError("BACKFILL_MUTATED_DELIVERY")), \
             mock.patch.object(self.driver._store, "update_state", side_effect=AssertionError("BACKFILL_ACK_OR_RECEIPT_WRITE")):
            self.assertEqual(self.driver._take_completed_item_backfill(), [])
        self.assertEqual(self.driver._envelope_states, before_state)
        self.assertEqual(self.driver._turn_to_envelope, before_turns)
        self.assertEqual([c[0] for c in self.rpc.calls], ["thread/turns/list"])
        self.assertNotIn("turn/start", [c[0] for c in self.rpc.calls])
        self.assertNotIn("thread/inject_items", [c[0] for c in self.rpc.calls])

    def test_delta_identity_and_adapter_backfill_suppression(self):
        event = self.driver._map_notification({"method": "item/agentMessage/delta", "params": {
            "threadId": "thread-bound", "turnId": "turn-x", "itemId": "item-x", "delta": "chunk",
        }})
        self.assertEqual(event.data["item_id"], "item-x")
        self.assertEqual(event.data["thread_id"], "thread-bound")
        adapter = object.__new__(DriverAdapter)
        adapter._commands = mock.Mock()
        adapter._commands.origin_delivery_id.return_value = ""
        backfill = SessionEvent(kind="assistant_text", text="whole body", data={"backfill": True},
                                session_id="thread-bound", delivery_id="delivery-x", turn_id="turn-x")
        self.assertEqual(adapter._format_event(backfill), "", "BACKFILL_APPENDED_TO_TERMINAL")
        encoded = json.loads(adapter._event_json(backfill))
        self.assertTrue(encoded["data"]["backfill"])


if __name__ == "__main__":
    unittest.main()
