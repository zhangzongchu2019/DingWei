import asyncio
import json
import os
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

from sessionhelper.app import SessionHelper
from sessionhelper.config import load_config
from sessionhelper.cli import CLIAdapter
from sessionhelper.contract import DeliveryReceipt
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.guide_state import (
    GUIDE_CONTEXT_FOOTER,
    GUIDE_CONTEXT_HEADER,
    StagedGuideStore,
)

try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tests._home_isolation import HomeIsolation


_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


BASE_ENV = {
    "SH_SESSION_NAME": "guide",
    "SH_OWNER": "zzc",
    "SH_KEY_ID": "FB-2642",
    "SH_SECRET": "secret",
    "SH_PROVISION_NO_EXIT": "1",
}


class FakeWS:
    def __init__(self, envs=()):
        self.envs = list(envs)
        self.sent = []

    def __aiter__(self):
        return self

    async def __anext__(self):
        if not self.envs:
            raise StopAsyncIteration
        return json.dumps(self.envs.pop(0), ensure_ascii=False)

    async def send(self, payload):
        self.sent.append(json.loads(payload))


class CountingAdapter:
    """Fake CLI/driver contract: tests count submissions, never start a process."""

    def __init__(self, label="cli", *, admit=True, fail=False):
        self.label = label
        self.admit = admit
        self.fail = fail
        self.calls = []

    def handle(self, env):
        self.calls.append(dict(env))
        return "standalone guide answer"

    def handle_delivery(self, env, on_injected=None, on_admitted=None, on_admission_failed=None):
        self.calls.append(dict(env))
        if self.fail:
            if on_admission_failed:
                on_admission_failed()
            raise RuntimeError("fixture admission failure")
        if on_injected:
            on_injected()
        if self.admit and on_admitted:
            on_admitted()
        return "business answer"


def guide_env(body="GUIDE", version="v1"):
    return {
        "id": "guide-frame",
        "from": "workpulse#FB-2642",
        "to": "guide#FB-2642",
        "body": body,
        "meta": {"type": "agent_network_skill", "system": True, "guide_version": version},
    }


def business_env(body="DO WORK", source="agent", eid="business-1"):
    return {
        "id": eid,
        "from": "peer#FB-2642",
        "to": "guide#FB-2642",
        "body": body,
        "meta": {"source_kind": source, "reply_policy": "no_reply"},
    }


class GuideStoreTest(unittest.TestCase):
    def test_stage_is_persistent_versioned_hashed_and_private(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "guide.json")
            first = StagedGuideStore(path, "seat-A")
            self.assertTrue(first.stage("GUIDE", "v1"))
            state = first.state()
            self.assertEqual((state["identity"], state["version"], state["state"]), ("seat-A", "v1", "staged"))
            self.assertEqual(len(state["body_hash"]), 64)
            self.assertEqual(os.stat(path).st_mode & 0o777, 0o600)
            restarted = StagedGuideStore(path, "seat-A")
            self.assertEqual(restarted.claim().body, "GUIDE")

    def test_consumed_same_version_does_not_restage_but_new_version_does(self):
        with tempfile.TemporaryDirectory() as tmp:
            store = StagedGuideStore(str(Path(tmp) / "guide.json"), "seat-A")
            store.stage("GUIDE", "v1")
            claim = store.claim()
            self.assertTrue(store.consume(claim))
            self.assertTrue(store.stage("GUIDE", "v1"))
            self.assertIsNone(store.claim())
            self.assertTrue(store.stage("GUIDE changed", "v2"))
            self.assertEqual(store.claim().body, "GUIDE changed")

    def test_identity_mismatch_cannot_consume_another_seat_guide(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "guide.json")
            StagedGuideStore(path, "seat-A").stage("GUIDE", "v1")
            self.assertIsNone(StagedGuideStore(path, "seat-B").claim())


class GuideSessionHelperTest(unittest.TestCase):
    def make_helper(self, tmp, adapter):
        cfg = load_config(dict(
            BASE_ENV,
            SH_MODE="driver",
            SH_INBOX_DB=str(Path(tmp) / "inbox.db"),
            SH_PROVISION_STATE_FILE=str(Path(tmp) / "provision.json"),
            SH_PROVISION_VERSIONS_FILE=str(Path(tmp) / "versions.json"),
        ))
        with mock.patch.object(SessionHelper, "_build_adapter", return_value=adapter):
            return SessionHelper(cfg)

    def test_guide_only_zero_calls_and_ack_after_durable_stage_for_all_paths(self):
        for label in ("codex-turn-start", "claude-stream-json", "traditional-cli"):
            with self.subTest(path=label), tempfile.TemporaryDirectory() as tmp:
                adapter = CountingAdapter(label)
                helper = self.make_helper(tmp, adapter)
                ws = FakeWS([guide_env()])
                asyncio.run(helper.recv_loop(ws))
                self.assertEqual(len(adapter.calls), 0, "guide-only must not submit a model turn")
                self.assertEqual(helper.guide_store.state()["state"], "staged")
                self.assertEqual([x["meta"]["type"] for x in ws.sent], ["agent_network_skill_ack"])

    def test_stage_failure_sends_no_ack(self):
        with tempfile.TemporaryDirectory() as tmp:
            helper = self.make_helper(tmp, CountingAdapter())
            with mock.patch.object(helper.guide_store, "stage", side_effect=OSError("disk full")):
                ws = FakeWS([guide_env()])
                asyncio.run(helper.recv_loop(ws))
            self.assertEqual(ws.sent, [])

    def test_guide_plus_first_task_one_call_second_task_total_two_for_all_paths(self):
        for label in ("codex-turn-start", "claude-stream-json", "traditional-cli"):
            with self.subTest(path=label), tempfile.TemporaryDirectory() as tmp:
                adapter = CountingAdapter(label)
                helper = self.make_helper(tmp, adapter)
                self.assertTrue(asyncio.run(helper.inject_agent_network_skill(guide_env())))
                asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
                self.assertEqual(len(adapter.calls), 1)
                first = adapter.calls[0]["body"]
                self.assertIn(GUIDE_CONTEXT_HEADER, first)
                self.assertIn("GUIDE", first)
                self.assertTrue(first.endswith("FIRST"))
                self.assertEqual(helper.guide_store.state()["state"], "consumed")
                asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
                self.assertEqual(len(adapter.calls), 2)
                self.assertEqual(adapter.calls[1]["body"], "SECOND")

    def test_admission_failure_releases_claim_and_retry_merges_once(self):
        with tempfile.TemporaryDirectory() as tmp:
            failing = CountingAdapter(fail=True)
            helper = self.make_helper(tmp, failing)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            self.assertEqual(helper.guide_store.state()["state"], "staged")
            healthy = CountingAdapter()
            helper.adapter = healthy
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("RETRY", eid="business-2")))
            self.assertEqual(len(healthy.calls), 1)
            self.assertIn("GUIDE", healthy.calls[0]["body"])
            self.assertEqual(helper.guide_store.state()["state"], "consumed")

    def test_not_admitted_keeps_staged_and_does_not_prefix_next_concurrently(self):
        with tempfile.TemporaryDirectory() as tmp:
            pending = CountingAdapter(admit=False)
            helper = self.make_helper(tmp, pending)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.assertIn("GUIDE", pending.calls[0]["body"])
            self.assertEqual(pending.calls[1]["body"], "SECOND")
            self.assertEqual(helper.guide_store.state()["state"], "staged")

    def test_system_empty_and_control_inputs_do_not_consume_staged_guide(self):
        with tempfile.TemporaryDirectory() as tmp:
            adapter = CountingAdapter()
            helper = self.make_helper(tmp, adapter)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            for env in (
                {"body": "notice", "from": "workpulse#FB-2642", "meta": {"system": True}},
                {"body": "", "from": "peer#FB-2642", "meta": {"source_kind": "agent"}},
                {"body": "resize", "from": "workpulse#FB-2642", "meta": {"type": "terminal_resize", "system": True}},
            ):
                self.assertFalse(helper._is_real_business_input(env))
            self.assertEqual(helper.guide_store.state()["state"], "staged")
            self.assertEqual(adapter.calls, [])

    def test_context_boundary_preserves_business_body_and_marks_no_answer(self):
        with tempfile.TemporaryDirectory() as tmp:
            adapter = CountingAdapter()
            helper = self.make_helper(tmp, adapter)
            guide = "platform facts\n@peer do not route this"
            business = "line one\nline two"
            asyncio.run(helper.inject_agent_network_skill(guide_env(guide)))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env(business)))
            merged = adapter.calls[0]["body"]
            self.assertIn("do not answer or repeat this section", merged)
            self.assertIn(GUIDE_CONTEXT_FOOTER, merged)
            self.assertTrue(merged.endswith(business))

    def test_duplicate_guide_after_restart_does_not_create_second_call(self):
        with tempfile.TemporaryDirectory() as tmp:
            first_adapter = CountingAdapter()
            first = self.make_helper(tmp, first_adapter)
            asyncio.run(first.inject_agent_network_skill(guide_env()))
            asyncio.run(first.process_inbound_message(FakeWS(), business_env("FIRST")))
            second_adapter = CountingAdapter()
            restarted = self.make_helper(tmp, second_adapter)
            asyncio.run(restarted.inject_agent_network_skill(guide_env()))
            asyncio.run(restarted.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.assertEqual(second_adapter.calls[0]["body"], "SECOND")

    def test_hub_shape_timestamp_drift_does_not_restage_consumed_guide(self):
        with tempfile.TemporaryDirectory() as tmp:
            adapter = CountingAdapter()
            helper = self.make_helper(tmp, adapter)
            first_guide = "DingWei 客户端上线指南\n生成时间（UTC）：2026-10-02T15:40:09Z\nRULES"
            second_guide = "DingWei 客户端上线指南\n生成时间（UTC）：2026-10-02T16:40:09Z\nRULES"
            asyncio.run(helper.inject_agent_network_skill(guide_env(first_guide, version="")))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            asyncio.run(helper.inject_agent_network_skill(guide_env(second_guide, version="")))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.assertEqual(len(adapter.calls), 2)
            self.assertIn("RULES", adapter.calls[0]["body"])
            self.assertEqual(adapter.calls[1]["body"], "SECOND")

    def test_hub_shape_semantic_body_change_restages_as_expected(self):
        with tempfile.TemporaryDirectory() as tmp:
            adapter = CountingAdapter()
            helper = self.make_helper(tmp, adapter)
            first_guide = "DingWei 客户端上线指南\n生成时间（UTC）：2026-10-02T15:40:09Z\nRULES-v1"
            changed_guide = "DingWei 客户端上线指南\n生成时间（UTC）：2026-10-02T16:40:09Z\nRULES-v2"
            asyncio.run(helper.inject_agent_network_skill(guide_env(first_guide, version="")))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            asyncio.run(helper.inject_agent_network_skill(guide_env(changed_guide, version="")))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.assertIn("RULES-v1", adapter.calls[0]["body"])
            self.assertIn("RULES-v2", adapter.calls[1]["body"])
            self.assertTrue(adapter.calls[1]["body"].endswith("SECOND"))


class FakeProtocolDriver:
    def __init__(self, state="processing", delay=0.0):
        self.state = state
        self.delay = delay
        self.calls = []
        self.turns = {}

    def start(self):
        return None

    def deliver(self, delivery_id, text):
        if self.delay:
            threading.Event().wait(self.delay)
        self.calls.append(text)
        turn = f"turn-{len(self.calls)}"
        self.turns[delivery_id] = turn
        detail = f"turn={turn}" if self.state != "failed" else "rejected before turn"
        return DeliveryReceipt(delivery_id, self.state, "fixture", detail)

    def poll_receipt(self, delivery_id):
        return DeliveryReceipt(delivery_id, "done", "fixture", f"turn={self.turns[delivery_id]}")

    def events(self, since=None):
        return iter(())


class DriverAdmissionCallbackTest(unittest.TestCase):
    def make_adapter(self, driver):
        cfg = load_config(dict(BASE_ENV, SH_MODE="driver", SH_BUSY_BUFFER_MAX="3"))
        with mock.patch.object(DriverAdapter, "_build_driver", return_value=driver):
            adapter = DriverAdapter(cfg)
        adapter.start()
        return adapter

    def test_callback_fires_only_after_protocol_turn_admission(self):
        for label in ("codex", "claude"):
            with self.subTest(driver=label):
                driver = FakeProtocolDriver()
                adapter = self.make_adapter(driver)
                admitted = threading.Event()
                adapter.handle_delivery(business_env("WORK"), None, admitted.set)
                self.assertTrue(admitted.wait(1))
                self.assertEqual(driver.calls, ["WORK"])

    def test_protocol_failure_before_turn_releases_admission(self):
        driver = FakeProtocolDriver(state="failed")
        adapter = self.make_adapter(driver)
        admitted = threading.Event()
        failed = threading.Event()
        adapter.handle_delivery(business_env("WORK"), None, admitted.set, failed.set)
        self.assertTrue(failed.wait(1))
        self.assertFalse(admitted.is_set())

    def test_slow_protocol_admission_does_not_inject_on_enqueue_return(self):
        driver = FakeProtocolDriver(delay=0.3)
        adapter = self.make_adapter(driver)
        order = []
        admitted = threading.Event()

        def on_admitted():
            order.append("admitted")
            admitted.set()

        adapter.handle_delivery(business_env("WORK"), lambda: order.append("injected"), on_admitted)
        order.append("enqueue_returned")
        self.assertEqual(order, ["enqueue_returned"])
        self.assertTrue(admitted.wait(1))
        self.assertEqual(order, ["enqueue_returned", "injected", "admitted"])

    def test_duplicate_has_no_new_injection_and_releases_unadmitted_claim(self):
        driver = FakeProtocolDriver()
        adapter = self.make_adapter(driver)
        first_injected = threading.Event()
        adapter.handle_delivery(business_env("FIRST", eid="same"), first_injected.set)
        self.assertTrue(first_injected.wait(1))
        duplicate_calls = []
        adapter.handle_delivery(
            business_env("DUP", eid="same"),
            lambda: duplicate_calls.append("injected"),
            lambda: duplicate_calls.append("admitted"),
            lambda: duplicate_calls.append("admission_failed"),
        )
        self.assertEqual(duplicate_calls, ["admission_failed"])


class GuideRealAdapterBoundaryTest(unittest.TestCase):
    @staticmethod
    def wait_for(predicate, timeout=2.0):
        deadline = __import__("time").time() + timeout
        while __import__("time").time() < deadline:
            if predicate():
                return
            threading.Event().wait(0.01)
        raise AssertionError("timed out waiting for adapter boundary")

    @classmethod
    def settle_guide_only(cls, adapter):
        """Wait for any wrongly enqueued standalone guide turn to become visible."""
        cls.wait_for(lambda: adapter._commands.depth == 0)
        threading.Event().wait(0.1)

    def make_driver_helper(self, tmp, driver):
        cfg = load_config(dict(
            BASE_ENV,
            SH_MODE="driver",
            SH_INBOX_DB=str(Path(tmp) / "inbox.db"),
            SH_PROVISION_STATE_FILE=str(Path(tmp) / "provision.json"),
            SH_PROVISION_VERSIONS_FILE=str(Path(tmp) / "versions.json"),
        ))
        with mock.patch.object(DriverAdapter, "_build_driver", return_value=driver):
            adapter = DriverAdapter(cfg)
        adapter.start()
        with mock.patch.object(SessionHelper, "_build_adapter", return_value=adapter):
            helper = SessionHelper(cfg)
        return helper, adapter

    def test_real_driver_adapter_boundary_counts_zero_one_two(self):
        with tempfile.TemporaryDirectory() as tmp:
            driver = FakeProtocolDriver()
            helper, adapter = self.make_driver_helper(tmp, driver)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            self.assertEqual(driver.calls, [], "guide-only must not reach Driver.deliver")
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            self.wait_for(lambda: len(driver.calls) == 1)
            self.assertIn("GUIDE", driver.calls[0])
            self.assertTrue(driver.calls[0].endswith("FIRST"))
            self.wait_for(lambda: helper.guide_store.state().get("state") == "consumed")
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.wait_for(lambda: len(driver.calls) == 2)
            self.assertEqual(driver.calls[1], "SECOND")
            self.wait_for(lambda: adapter._commands.depth == 0)

    def test_duplicate_releases_claim_for_next_business_command(self):
        with tempfile.TemporaryDirectory() as tmp:
            driver = FakeProtocolDriver()
            helper, adapter = self.make_driver_helper(tmp, driver)
            injected = []
            asyncio.run(helper.process_inbound_message(
                FakeWS(), business_env("BEFORE", eid="same"),
                on_injected=lambda: injected.append("same-1"),
            ))
            self.wait_for(lambda: len(driver.calls) == 1)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            asyncio.run(helper.process_inbound_message(
                FakeWS(), business_env("DUP", eid="same"),
                on_injected=lambda: injected.append("same-2"),
            ))
            self.assertEqual(injected, ["same-1"],
                             "duplicate must replay durable ACKs without synthesizing admission")
            self.assertIsNone(helper.guide_store._claimed)
            asyncio.run(helper.process_inbound_message(
                FakeWS(), business_env("THIRD", eid="third"),
                on_injected=lambda: injected.append("third"),
            ))
            self.wait_for(lambda: len(driver.calls) == 2)
            self.assertIn("GUIDE", driver.calls[1])
            self.assertTrue(driver.calls[1].endswith("THIRD"))
            self.assertEqual(driver.calls[1].count("GUIDE"), 1)
            self.assertEqual(injected, ["same-1", "third"])
            self.assertEqual(helper.guide_store.state()["state"], "consumed")
            self.wait_for(lambda: adapter._commands.depth == 0)

    def test_driver_deliver_exception_releases_claim_for_next_command(self):
        class RaiseOnceDriver(FakeProtocolDriver):
            def deliver(self, delivery_id, text):
                self.calls.append(text)
                if len(self.calls) == 1:
                    raise RuntimeError("fixture deliver boom")
                turn = f"turn-{len(self.calls)}"
                self.turns[delivery_id] = turn
                return DeliveryReceipt(delivery_id, "processing", "fixture", f"turn={turn}")

        with tempfile.TemporaryDirectory() as tmp:
            driver = RaiseOnceDriver()
            helper, adapter = self.make_driver_helper(tmp, driver)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            self.wait_for(lambda: len(driver.calls) == 1)
            self.wait_for(lambda: helper.guide_store._claimed is None)
            self.assertEqual(helper.guide_store.state()["state"], "staged")
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.wait_for(lambda: len(driver.calls) == 2)
            self.assertIn("GUIDE", driver.calls[1])
            self.assertTrue(driver.calls[1].endswith("SECOND"))
            self.wait_for(lambda: helper.guide_store.state().get("state") == "consumed")
            self.wait_for(lambda: adapter._commands.depth == 0)

    def test_driver_not_admitted_releases_claim_for_next_command(self):
        class RejectOnceDriver(FakeProtocolDriver):
            def deliver(self, delivery_id, text):
                self.calls.append(text)
                if len(self.calls) == 1:
                    return DeliveryReceipt(delivery_id, "failed", "fixture", "rejected before turn")
                turn = f"turn-{len(self.calls)}"
                self.turns[delivery_id] = turn
                return DeliveryReceipt(delivery_id, "processing", "fixture", f"turn={turn}")

        with tempfile.TemporaryDirectory() as tmp:
            driver = RejectOnceDriver()
            helper, adapter = self.make_driver_helper(tmp, driver)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            self.wait_for(lambda: len(driver.calls) == 1)
            self.wait_for(lambda: helper.guide_store._claimed is None)
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.wait_for(lambda: len(driver.calls) == 2)
            self.assertIn("GUIDE", driver.calls[1])
            self.wait_for(lambda: helper.guide_store.state().get("state") == "consumed")
            self.wait_for(lambda: adapter._commands.depth == 0)

    def test_queue_full_releases_claim_for_next_command(self):
        class BlockingDriver(FakeProtocolDriver):
            def __init__(self):
                super().__init__()
                self.started = threading.Event()
                self.release = threading.Event()

            def deliver(self, delivery_id, text):
                self.calls.append(text)
                if len(self.calls) == 1:
                    self.started.set()
                    self.release.wait(2)
                turn = f"turn-{len(self.calls)}"
                self.turns[delivery_id] = turn
                return DeliveryReceipt(delivery_id, "processing", "fixture", f"turn={turn}")

        with tempfile.TemporaryDirectory() as tmp:
            driver = BlockingDriver()
            helper, adapter = self.make_driver_helper(tmp, driver)
            adapter._commands.capacity = 1
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("BLOCK", eid="block")))
            self.assertTrue(driver.started.wait(1))
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FULL", eid="full")))
            self.assertIsNone(helper.guide_store._claimed)
            self.assertEqual(helper.guide_store.state()["state"], "staged")
            driver.release.set()
            self.wait_for(lambda: adapter._commands.depth == 0)
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("AFTER", eid="after")))
            self.wait_for(lambda: len(driver.calls) == 2)
            self.assertIn("GUIDE", driver.calls[1])
            self.assertTrue(driver.calls[1].endswith("AFTER"))
            self.wait_for(lambda: adapter._commands.depth == 0)

    def test_real_cli_adapter_fake_pty_counts_zero_one_two(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(
                BASE_ENV,
                SH_MODE="cli",
                SH_CLI="aider",
                SH_ASYNC_REPLY="1",
                SH_INBOX_DB=str(Path(tmp) / "inbox.db"),
                SH_PROVISION_STATE_FILE=str(Path(tmp) / "provision.json"),
                SH_PROVISION_VERSIONS_FILE=str(Path(tmp) / "versions.json"),
            ))
            adapter = CLIAdapter(cfg)
            text_writes = []
            enter_submits = []
            class FakeChild:
                def send(self, text):
                    text_writes.append(text)
            adapter.ready.set()
            adapter.start = lambda: True
            adapter._child_gone = lambda: False
            adapter.wait_for_web_idle = lambda: None
            adapter.child = FakeChild()
            adapter.submit_enter = lambda: enter_submits.append("enter")
            with mock.patch.object(SessionHelper, "_build_adapter", return_value=adapter):
                helper = SessionHelper(cfg)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            self.assertEqual(enter_submits, [], "guide-only must not submit Enter")
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            self.assertEqual(len(enter_submits), 1)
            self.assertIn("GUIDE", text_writes[0])
            self.assertIn("FIRST", text_writes[0])
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.assertEqual(len(enter_submits), 2)
            self.assertIn("SECOND", text_writes[1])

    def test_codex_turn_start_rpc_boundary_counts_zero_one_two(self):
        from sessionhelper.codexDriver import CodexProtocolDriver

        with tempfile.TemporaryDirectory() as tmp:
            driver = CodexProtocolDriver(workdir=tmp, store_path=str(Path(tmp) / "codex.sqlite3"))
            driver._thread_id = "thread-guide"
            calls = []
            probes = []
            driver.health = lambda: True
            driver.start = lambda: None
            driver._sync_thread_alias = lambda: None
            driver._wait_until_idle_for_delivery = lambda: ""
            driver._read_turn_state = lambda _turn: ("done", "done")
            driver.poll_receipt = lambda delivery_id: DeliveryReceipt(
                delivery_id, "done", "fixture", f"turn={driver._envelope_to_turn[delivery_id]}"
            )
            def request(method, params, timeout=0):
                if method == "thread/read":
                    probes.append((method, params))
                    return {"thread": {"id": "thread-guide", "cwd": tmp,
                                       "status": {"type": "idle"}, "canAcceptDirectInput": True}}
                calls.append((method, params))
                return {"turn": {"id": f"turn-{len(calls)}", "status": "inProgress"}}
            driver._request = request
            helper, adapter = self.make_driver_helper(tmp, driver)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            self.settle_guide_only(adapter)
            self.assertEqual(calls, [])
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            self.wait_for(lambda: len(calls) == 1)
            self.assertEqual(len(probes), 1, "bound thread must be validated before the first business turn")
            self.assertEqual(calls[0][0], "turn/start")
            self.assertIn("GUIDE", calls[0][1]["input"][0]["text"])
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.wait_for(lambda: len(calls) == 2)
            self.assertEqual(calls[1][0], "turn/start")
            self.assertEqual(calls[1][1]["input"][0]["text"], "SECOND")
            self.wait_for(lambda: adapter._commands.depth == 0)

    def test_claude_stream_json_user_frame_boundary_counts_zero_one_two(self):
        from sessionhelper.claudeDriver import ClaudeProtocolDriver

        class FakeStdin:
            def __init__(self, driver):
                self.driver = driver
                self.frames = []
            def write(self, raw):
                self.frames.append(json.loads(raw))
                self.driver._active.accepted.set()
            def flush(self):
                return None
        class FakeProc:
            def __init__(self, driver):
                self.stdin = FakeStdin(driver)
            def poll(self):
                return None

        with tempfile.TemporaryDirectory() as tmp:
            driver = ClaudeProtocolDriver(store_path=str(Path(tmp) / "claude.sqlite3"))
            driver._proc = FakeProc(driver)
            driver.health = lambda: True
            driver.poll_receipt = lambda delivery_id: DeliveryReceipt(
                delivery_id, "done", "fixture", f"turn={driver._store.get(delivery_id).turn_id}"
            )
            helper, adapter = self.make_driver_helper(tmp, driver)
            asyncio.run(helper.inject_agent_network_skill(guide_env()))
            self.settle_guide_only(adapter)
            self.assertEqual(driver._proc.stdin.frames, [])
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("FIRST")))
            self.wait_for(lambda: len(driver._proc.stdin.frames) == 1)
            first = driver._proc.stdin.frames[0]
            self.assertEqual(first["type"], "user")
            self.assertIn("GUIDE", first["message"]["content"][0]["text"])
            driver._active.terminal_seen.set()
            asyncio.run(helper.process_inbound_message(FakeWS(), business_env("SECOND", eid="business-2")))
            self.wait_for(lambda: len(driver._proc.stdin.frames) == 2)
            second = driver._proc.stdin.frames[1]
            self.assertEqual(second["type"], "user")
            self.assertEqual(second["message"]["content"][0]["text"], "SECOND")
            self.wait_for(lambda: adapter._commands.depth == 0)


if __name__ == "__main__":
    unittest.main()
