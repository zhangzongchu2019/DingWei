import asyncio
import json
import threading
import time
import unittest
from unittest import mock

from sessionhelper.config import load_config
from sessionhelper.contract import DeliveryReceipt
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.app import SessionHelper
from sessionhelper.unified_queue import UnifiedCommandQueue


class UnifiedCommandQueueTest(unittest.TestCase):
    def setUp(self):
        ticks = iter(range(1000, 1100))
        self.queue = UnifiedCommandQueue(3, clock=lambda: float(next(ticks)))

    def test_three_sources_share_fifo_and_schema(self):
        fixtures = [
            ({"id": "view-1", "body": "用户第一行\n用户第二行", "meta": {"source_kind": "user"}}, "user"),
            ({"id": "agent-1", "from": "manager#FB-0000", "body": "agent", "meta": {}}, "agent"),
            ({
                "id": "feishu-1",
                "from": "oc_group#FB-0000#ExampleBot",
                "body": "飞书第一行\n飞书第二行",
                "meta": {
                    "source_kind": "feishu",
                    "source_chat_type": "group",
                    "source_sender_openid": "ou_example",
                    "source_display_name": "示例用户",
                    "source_bot_channel_id": "bot-1",
                },
            }, "feishu"),
        ]
        queued = [self.queue.enqueue(env) for env, _ in fixtures]
        self.assertEqual([item.state for item in queued], ["queued"] * 3)
        self.assertEqual([item.command.queue_seq for item in queued], [1, 2, 3])
        self.assertEqual([item.event["data"]["source"] for item in queued], [kind for _, kind in fixtures])
        self.assertEqual(queued[0].event["text"], "用户第一行\n用户第二行")
        self.assertEqual(queued[2].event["data"]["display_name"], "示例用户")
        self.assertEqual(queued[2].event["data"]["open_id"], "ou_example")
        self.assertTrue(all(item.event["turn_id"] == "" for item in queued))

        admitted = []
        for index in range(3):
            command = self.queue.start_next()
            admitted.append(self.queue.admit(command.delivery_id, f"turn-{index + 1}"))
            self.queue.finish(command.delivery_id, "completed")
        self.assertEqual([event["data"]["queue_seq"] for event in admitted], [1, 2, 3])
        self.assertEqual([event["turn_id"] for event in admitted], ["turn-1", "turn-2", "turn-3"])
        self.assertEqual(
            [event["delivery_id"] for event in admitted],
            [item.event["delivery_id"] for item in queued],
        )

    def test_lifecycle_is_idempotent(self):
        env = {"id": "same", "from": "manager#FB-0000", "body": "once", "meta": {}}
        first = self.queue.enqueue(env)
        duplicate = self.queue.enqueue(env)
        self.assertEqual(duplicate.state, "duplicate")
        self.assertIs(duplicate.command, first.command)
        self.assertIsNone(duplicate.event)
        self.assertEqual(self.queue.depth, 1)

        command = self.queue.start_next()
        admitted = self.queue.admit(command.delivery_id, "turn-1")
        self.assertIsNotNone(admitted)
        self.assertIsNone(self.queue.admit(command.delivery_id, "turn-1"))
        self.assertIsNone(self.queue.admit(command.delivery_id, ""))

    def test_rejects_new_without_dropping_visible_fifo(self):
        queue = UnifiedCommandQueue(2, clock=lambda: 1.0)
        one = queue.enqueue({"id": "1", "from": "a#key", "body": "one", "meta": {}})
        two = queue.enqueue({"id": "2", "from": "b#key", "body": "two", "meta": {}})
        rejected = queue.enqueue({"id": "3", "from": "c#key", "body": "three", "meta": {}})
        self.assertEqual(rejected.state, "rejected")
        self.assertEqual(rejected.reason, "queue_full")
        self.assertEqual(rejected.event["data"]["change"], "queue_rejected")
        self.assertEqual([item.delivery_id for item in queue.pending], [one.command.delivery_id, two.command.delivery_id])

    def test_only_current_head_can_be_admitted_or_finished(self):
        one = self.queue.enqueue({"id": "1", "from": "a#key", "body": "one", "meta": {}}).command
        two = self.queue.enqueue({"id": "2", "from": "b#key", "body": "two", "meta": {}}).command
        self.assertEqual(self.queue.start_next().delivery_id, one.delivery_id)
        self.assertIsNone(self.queue.admit(two.delivery_id, "turn-2"))
        self.assertIsNone(self.queue.finish(two.delivery_id, "completed"))
        self.assertEqual(self.queue.current.delivery_id, one.delivery_id)


class FakeDriver:
    def __init__(self, *, block_first: threading.Event | None = None, never_terminal: bool = False):
        self.block_first = block_first
        self.never_terminal = never_terminal
        self.deliveries = []
        self.turns = {}

    def start(self):
        return None

    def deliver(self, delivery_id, text):
        self.deliveries.append((delivery_id, text))
        if self.block_first is not None and len(self.deliveries) == 1:
            self.block_first.wait(2)
        turn_id = f"turn-{len(self.deliveries)}"
        self.turns[delivery_id] = turn_id
        return DeliveryReceipt(delivery_id, "processing", "fixture", f"turn={turn_id}")

    def poll_receipt(self, delivery_id):
        turn_id = self.turns[delivery_id]
        state = "processing" if self.never_terminal else "done"
        return DeliveryReceipt(delivery_id, state, "fixture", f"turn={turn_id}")

    def events(self, since=None):
        return iter(())


class DriverAdapterUnifiedQueueTest(unittest.TestCase):
    def make_adapter(self, driver, **env):
        values = {
            "SH_SESSION_NAME": "dev",
            "SH_OWNER": "owner1",
            "SH_KEY_ID": "FB-0000",
            "SH_SECRET": "secret",
            "SH_MODE": "driver",
            "SH_BUSY_BUFFER_MAX": "10",
            "SH_TURN_TERMINAL_TIMEOUT_SECONDS": "1",
            **env,
        }
        with mock.patch.object(DriverAdapter, "_build_driver", return_value=driver):
            adapter = DriverAdapter(load_config(values))
        adapter.start()
        return adapter

    @staticmethod
    def events_until(adapter, count, timeout=2):
        deadline = time.time() + timeout
        out = []
        while len(out) < count and time.time() < deadline:
            payload = adapter.next_session_event(0.05)
            if payload:
                out.append(json.loads(payload))
        return out

    def test_view_path_emits_one_queued_admitted_pair_and_one_delivery(self):
        driver = FakeDriver()
        adapter = self.make_adapter(driver)
        result = adapter.enqueue_human("第一行\n第二行")
        duplicate = adapter.enqueue_envelope({
            "body": result.command.text,
            "meta": {"delivery_id": result.command.delivery_id, "source_kind": "user"},
        })
        events = self.events_until(adapter, 2)

        self.assertEqual([event["data"]["lifecycle"] for event in events], ["queued", "admitted"])
        self.assertEqual(events[0]["delivery_id"], events[1]["delivery_id"])
        self.assertEqual(events[0]["turn_id"], "")
        self.assertTrue(events[1]["turn_id"])
        self.assertEqual(events[0]["text"], "第一行\n第二行")
        self.assertEqual(duplicate.state, "duplicate")
        self.assertEqual(len(driver.deliveries), 1)

    def test_three_sources_use_one_adapter_fifo(self):
        driver = FakeDriver()
        adapter = self.make_adapter(driver)
        adapter.enqueue_human("user")
        adapter.enqueue_envelope({"id": "agent", "from": "manager#FB-0000", "body": "agent", "meta": {}})
        adapter.enqueue_envelope({
            "id": "feishu", "from": "oc_x#FB-0000#ExampleBot", "body": "feishu",
            "meta": {"source_kind": "feishu", "source_chat_type": "group",
                     "source_sender_openid": "ou_example", "source_display_name": "示例用户"},
        })
        events = self.events_until(adapter, 6)
        queued = [event for event in events if event.get("data", {}).get("lifecycle") == "queued"]
        admitted = [event for event in events if event.get("data", {}).get("lifecycle") == "admitted"]
        self.assertEqual([event["data"]["queue_seq"] for event in queued], [1, 2, 3])
        self.assertEqual([event["data"]["source"] for event in queued], ["user", "agent", "feishu"])
        self.assertEqual([text for _, text in driver.deliveries], ["user", "agent", "feishu"])
        self.assertEqual([event["delivery_id"] for event in admitted], [event["delivery_id"] for event in queued])

    def test_blocking_deliver_does_not_block_enqueue(self):
        release = threading.Event()
        driver = FakeDriver(block_first=release)
        adapter = self.make_adapter(driver)
        adapter.enqueue_human("one")
        deadline = time.time() + 1
        while not driver.deliveries and time.time() < deadline:
            time.sleep(0.01)
        started = time.monotonic()
        second = adapter.enqueue_envelope({"id": "two", "from": "manager#FB-0000", "body": "two", "meta": {}})
        elapsed = time.monotonic() - started
        self.assertEqual(second.state, "queued")
        self.assertLess(elapsed, 0.1)
        release.set()

    def test_terminal_watchdog_fails_current_and_advances(self):
        driver = FakeDriver(never_terminal=True)
        adapter = self.make_adapter(driver, SH_TURN_TERMINAL_TIMEOUT_SECONDS="0.15")
        first = adapter.enqueue_human("one").command
        adapter.enqueue_envelope({"id": "two", "from": "manager#FB-0000", "body": "two", "meta": {}})
        events = self.events_until(adapter, 5, timeout=2)
        timeout_events = [
            event for event in events
            if event.get("data", {}).get("reason") == "terminal_timeout"
            and event["delivery_id"] == first.delivery_id
        ]
        self.assertEqual(len(timeout_events), 1)
        self.assertEqual(timeout_events[0]["data"]["outcome"], "failed")
        self.assertEqual([text for _, text in driver.deliveries][:2], ["one", "two"])

    def test_sessionhelper_driver_mode_routes_directly_to_unified_queue(self):
        class FakeAdapter:
            def __init__(self):
                self.queue = UnifiedCommandQueue(2, clock=lambda: 1.0)

            def is_idle(self):
                return False

            def enqueue_envelope(self, env):
                return self.queue.enqueue(env)

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        fake = FakeAdapter()
        config = load_config({
            "SH_SESSION_NAME": "dev", "SH_OWNER": "owner1", "SH_KEY_ID": "FB-0000",
            "SH_SECRET": "secret", "SH_MODE": "driver", "SH_BUSY_BUFFER_MAX": "2",
        })
        with mock.patch.object(SessionHelper, "_build_adapter", return_value=fake):
            helper = SessionHelper(config)
        ws = FakeWS()
        env = {"id": "agent", "from": "manager#FB-0000", "to": "dev#FB-0000", "body": "work", "meta": {}}
        self.assertTrue(asyncio.run(helper.buffer_if_busy(ws, env)))
        self.assertEqual([item.text for item in fake.queue.pending], ["work"])
        self.assertEqual(len(helper.pending_inbound), 0)


if __name__ == "__main__":
    unittest.main()
