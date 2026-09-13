import json
import os
import sys
import tempfile
import threading
import unittest
from unittest import mock


sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))

from sessionhelper import claudeDriver, codexDriver, openCodeDriver


class T094IdleStallTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp.cleanup()

    def test_claude_progress_refreshes_idle_but_absolute_limit_still_wins(self):
        driver = claudeDriver.ClaudeProtocolDriver(store_path=os.path.join(self.tmp.name, "claude.sqlite"))
        driver._stall_timeout = 90
        driver._absolute_timeout = 900
        active = claudeDriver._TurnState(
            envelope_id="env-c", turn_id="turn-c",
            accepted=threading.Event(), terminal_seen=threading.Event(), submitted_at=10,
        )
        active.accepted.set()
        driver._active = active
        driver._store.upsert("env-c", "turn-c", "hash", "accepted", "test")

        with mock.patch.object(claudeDriver.time, "monotonic", return_value=210):
            driver._handle_frame({"type": "assistant", "message": {"content": [{"type": "text", "text": "progress"}]}})
        with mock.patch.object(claudeDriver.time, "monotonic", return_value=220):
            self.assertEqual("processing", driver.poll_receipt("env-c").state)
        active.pending_tool_ids.add("still-running")
        with mock.patch.object(claudeDriver.time, "monotonic", return_value=911):
            receipt = driver.poll_receipt("env-c")
        self.assertEqual("failed", receipt.state)
        self.assertIn("ABSOLUTE_TIMEOUT", receipt.detail)

    def test_claude_idle_timeout_has_distinct_detail(self):
        driver = claudeDriver.ClaudeProtocolDriver(store_path=os.path.join(self.tmp.name, "claude-idle.sqlite"))
        driver._stall_timeout = 90
        active = claudeDriver._TurnState(
            envelope_id="env-c-idle", turn_id="turn-c-idle",
            accepted=threading.Event(), terminal_seen=threading.Event(), submitted_at=10,
        )
        driver._active = active
        driver._store.upsert("env-c-idle", "turn-c-idle", "hash", "accepted", "test")
        with mock.patch.object(claudeDriver.time, "monotonic", return_value=101):
            receipt = driver.poll_receipt("env-c-idle")
        self.assertEqual("failed", receipt.state)
        self.assertIn("STALLED", receipt.detail)
        self.assertNotIn("ABSOLUTE", receipt.detail)

    def test_claude_inflight_tool_pauses_only_idle_gate(self):
        driver = claudeDriver.ClaudeProtocolDriver(store_path=os.path.join(self.tmp.name, "claude-tool.sqlite"))
        active = claudeDriver._TurnState(
            envelope_id="env-c-tool", turn_id="turn-c-tool",
            accepted=threading.Event(), terminal_seen=threading.Event(), submitted_at=10,
        )
        driver._active = active
        driver._store.upsert("env-c-tool", "turn-c-tool", "hash", "accepted", "test")
        with mock.patch.object(claudeDriver.time, "monotonic", return_value=20):
            driver._handle_frame({"type": "assistant", "message": {"content": [
                {"type": "tool_use", "id": "tool-c", "name": "slow"},
            ]}})
        with mock.patch.object(claudeDriver.time, "monotonic", return_value=200):
            self.assertEqual("processing", driver.poll_receipt("env-c-tool").state)
        self.assertEqual({"tool-c"}, active.pending_tool_ids)
        with mock.patch.object(claudeDriver.time, "monotonic", return_value=201):
            driver._handle_frame({"type": "user", "message": {"content": [
                {"type": "tool_result", "tool_use_id": "tool-c", "content": "done"},
            ]}})
        with mock.patch.object(claudeDriver.time, "monotonic", return_value=292):
            self.assertEqual("failed", driver.poll_receipt("env-c-tool").state)

    def test_claude_concurrent_tools_do_not_clear_each_other(self):
        driver = claudeDriver.ClaudeProtocolDriver(store_path=os.path.join(self.tmp.name, "claude-concurrent.sqlite"))
        active = claudeDriver._TurnState(
            envelope_id="env-c-many", turn_id="turn-c-many",
            accepted=threading.Event(), terminal_seen=threading.Event(), submitted_at=10,
        )
        driver._active = active
        driver._store.upsert("env-c-many", "turn-c-many", "hash", "accepted", "test")
        driver._handle_frame({"type": "assistant", "message": {"content": [
            {"type": "tool_use", "id": "tool-one", "name": "slow"},
            {"type": "tool_use", "id": "tool-two", "name": "slow"},
        ]}})
        driver._handle_frame({"type": "user", "message": {"content": [
            {"type": "tool_result", "tool_use_id": "tool-one", "content": "done"},
        ]}})
        self.assertEqual({"tool-two"}, active.pending_tool_ids)

    def _codex_driver(self):
        driver = codexDriver.CodexProtocolDriver(
            manage_daemon=False, store_path=os.path.join(self.tmp.name, "codex.sqlite"), stall_timeout=90,
        )
        driver._thread_id = "thread-x"
        driver._session_id = "thread-x"
        driver._envelope_states["env-x"] = "accepted"
        driver._delivery_timestamps["env-x"] = 10
        driver._absolute_timeout = 900
        driver._turn_to_envelope["turn-x"] = "env-x"
        return driver

    def test_codex_attributable_delta_refreshes_idle(self):
        driver = self._codex_driver()
        with mock.patch.object(codexDriver.time, "monotonic", return_value=210):
            event = driver._map_notification({
                "method": "item/agentMessage/delta",
                "params": {"threadId": "thread-x", "turnId": "turn-x", "itemId": "item-x", "delta": "progress"},
            })
        self.assertEqual("env-x", event.delivery_id)
        with mock.patch.object(codexDriver.time, "monotonic", return_value=220):
            self.assertEqual("accepted", driver.poll_receipt("env-x").state)

    def test_codex_idle_and_absolute_details_differ(self):
        idle = self._codex_driver()
        with mock.patch.object(codexDriver.time, "monotonic", return_value=101):
            idle_receipt = idle.poll_receipt("env-x")
        absolute = self._codex_driver()
        absolute._delivery_last_event_at["env-x"] = 905
        absolute._delivery_pending_tools["env-x"] = {"still-running"}
        with mock.patch.object(codexDriver.time, "monotonic", return_value=911):
            absolute_receipt = absolute.poll_receipt("env-x")
        self.assertIn("idle stalled", idle_receipt.detail)
        self.assertIn("absolute turn timeout", absolute_receipt.detail)

    def test_codex_inflight_tool_pauses_only_idle_gate(self):
        driver = self._codex_driver()
        driver._map_notification({
            "method": "item/started",
            "params": {"threadId": "thread-x", "turnId": "turn-x", "item": {
                "id": "tool-x", "type": "commandExecution",
            }},
        })
        with mock.patch.object(codexDriver.time, "monotonic", return_value=200):
            self.assertEqual("accepted", driver.poll_receipt("env-x").state)
        self.assertEqual({"tool-x"}, driver._delivery_pending_tools["env-x"])
        with mock.patch.object(codexDriver.time, "monotonic", return_value=201):
            driver._map_notification({
                "method": "item/completed",
                "params": {"threadId": "thread-x", "turnId": "turn-x", "item": {
                    "id": "tool-x", "type": "commandExecution",
                }},
            })
        with mock.patch.object(codexDriver.time, "monotonic", return_value=292):
            self.assertEqual("failed", driver.poll_receipt("env-x").state)

    def test_codex_anonymous_concurrent_tools_are_counted_independently(self):
        driver = self._codex_driver()
        start = {"method": "item/started", "params": {
            "threadId": "thread-x", "turnId": "turn-x",
            "item": {"type": "commandExecution"},
        }}
        driver._map_notification(start)
        driver._map_notification(start)
        self.assertEqual(2, len(driver._delivery_pending_tools["env-x"]))
        driver._map_notification({"method": "item/completed", "params": {
            "threadId": "thread-x", "turnId": "turn-x",
            "item": {"type": "commandExecution"},
        }})
        self.assertEqual(1, len(driver._delivery_pending_tools["env-x"]))

    def test_codex_anonymous_completion_never_clears_named_tool(self):
        driver = self._codex_driver()
        driver._map_notification({"method": "item/started", "params": {
            "threadId": "thread-x", "turnId": "turn-x",
            "item": {"type": "commandExecution"},
        }})
        driver._map_notification({"method": "item/started", "params": {
            "threadId": "thread-x", "turnId": "turn-x",
            "item": {"id": "named-tool", "type": "commandExecution"},
        }})
        driver._map_notification({"method": "item/completed", "params": {
            "threadId": "thread-x", "turnId": "turn-x",
            "item": {"type": "commandExecution"},
        }})
        self.assertEqual({"named-tool"}, driver._delivery_pending_tools["env-x"])

    def _opencode_driver(self):
        driver = openCodeDriver.OpenCodeProtocolDriver(
            password="test", manage_server=False,
            store_path=os.path.join(self.tmp.name, "opencode.sqlite"), stall_timeout=90,
        )
        driver._session_id = "session-o"
        driver._envelope_states["env-o"] = "accepted"
        driver._delivery_timestamps["env-o"] = 10
        driver._absolute_timeout = 900
        driver._msg_to_envelope["msg-o"] = "env-o"
        driver._current_delivery = "env-o"
        driver._request = mock.Mock(return_value=(200, json.dumps({"data": []})))
        return driver

    def test_opencode_attributable_text_refreshes_idle(self):
        driver = self._opencode_driver()
        driver._map_sse_event("", json.dumps({
            "type": "session.step.started", "data": {"assistantMessageID": "asst-o"},
        }), "")
        with mock.patch.object(openCodeDriver.time, "monotonic", return_value=210):
            event = driver._map_sse_event("", json.dumps({
                "type": "session.text.ended",
                "data": {"assistantMessageID": "asst-o", "text": "progress"},
            }), "")
        self.assertEqual("env-o", event.delivery_id)
        with mock.patch.object(openCodeDriver.time, "monotonic", return_value=220):
            self.assertEqual("accepted", driver.poll_receipt("env-o").state)

    def test_opencode_idle_and_absolute_details_differ(self):
        idle = self._opencode_driver()
        with mock.patch.object(openCodeDriver.time, "monotonic", return_value=101):
            idle_receipt = idle.poll_receipt("env-o")
        absolute = self._opencode_driver()
        absolute._delivery_last_event_at["env-o"] = 905
        absolute._delivery_pending_tools["env-o"] = {"still-running"}
        with mock.patch.object(openCodeDriver.time, "monotonic", return_value=911):
            absolute_receipt = absolute.poll_receipt("env-o")
        self.assertIn("idle stalled", idle_receipt.detail)
        self.assertIn("absolute turn timeout", absolute_receipt.detail)

    def test_opencode_inflight_tool_pauses_only_idle_gate(self):
        driver = self._opencode_driver()
        driver._map_sse_event("", json.dumps({
            "type": "session.step.started", "data": {"assistantMessageID": "asst-o"},
        }), "")
        driver._map_sse_event("", json.dumps({
            "type": "session.tool.started",
            "data": {"assistantMessageID": "asst-o", "callID": "tool-o", "name": "slow"},
        }), "")
        with mock.patch.object(openCodeDriver.time, "monotonic", return_value=200):
            self.assertEqual("accepted", driver.poll_receipt("env-o").state)
        self.assertEqual({"tool-o"}, driver._delivery_pending_tools["env-o"])
        with mock.patch.object(openCodeDriver.time, "monotonic", return_value=201):
            driver._map_sse_event("", json.dumps({
                "type": "session.tool.ended",
                "data": {"assistantMessageID": "asst-o", "callID": "tool-o", "name": "slow"},
            }), "")
        with mock.patch.object(openCodeDriver.time, "monotonic", return_value=292):
            self.assertEqual("failed", driver.poll_receipt("env-o").state)

    def test_opencode_same_name_anonymous_tools_are_counted_independently(self):
        driver = self._opencode_driver()
        driver._map_sse_event("", json.dumps({
            "type": "session.step.started", "data": {"assistantMessageID": "asst-o"},
        }), "")
        start = json.dumps({
            "type": "session.tool.started",
            "data": {"assistantMessageID": "asst-o", "name": "same-name"},
        })
        driver._map_sse_event("", start, "")
        driver._map_sse_event("", start, "")
        self.assertEqual(2, len(driver._delivery_pending_tools["env-o"]))
        driver._map_sse_event("", json.dumps({
            "type": "session.tool.ended",
            "data": {"assistantMessageID": "asst-o", "name": "same-name"},
        }), "")
        self.assertEqual(1, len(driver._delivery_pending_tools["env-o"]))

    def test_opencode_anonymous_completion_never_clears_named_tool(self):
        driver = self._opencode_driver()
        driver._map_sse_event("", json.dumps({
            "type": "session.step.started", "data": {"assistantMessageID": "asst-o"},
        }), "")
        driver._map_sse_event("", json.dumps({
            "type": "session.tool.started",
            "data": {"assistantMessageID": "asst-o", "name": "anonymous"},
        }), "")
        driver._map_sse_event("", json.dumps({
            "type": "session.tool.started",
            "data": {"assistantMessageID": "asst-o", "callID": "named-tool", "name": "named"},
        }), "")
        driver._map_sse_event("", json.dumps({
            "type": "session.tool.ended",
            "data": {"assistantMessageID": "asst-o", "name": "anonymous"},
        }), "")
        self.assertEqual({"named-tool"}, driver._delivery_pending_tools["env-o"])


if __name__ == "__main__":
    unittest.main()
