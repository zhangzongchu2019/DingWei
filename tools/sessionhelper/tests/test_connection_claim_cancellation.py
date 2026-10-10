"""AST-only claim cancellation regression with bounded synthetic threads."""
import ast
import asyncio
import copy
import json
from pathlib import Path
import threading
from types import SimpleNamespace
import unittest

APP = Path(__file__).resolve().parents[1] / "sessionhelper" / "app.py"
namespace = {"asyncio": asyncio, "json": json}
tree = ast.parse(APP.read_text())
for name in ("supervise_connection_loops", "claim_delivery_for_connection"):
    node = next(n for n in tree.body if isinstance(n, ast.AsyncFunctionDef) and n.name == name)
    exec(compile(ast.fix_missing_locations(ast.Module(body=[copy.deepcopy(node)], type_ignores=[])), str(APP), "exec"), namespace)
claim = namespace["claim_delivery_for_connection"]
supervise = namespace["supervise_connection_loops"]
helper_class = next(n for n in tree.body if isinstance(n, ast.ClassDef) and n.name == "SessionHelper")
worker_node = next(n for n in helper_class.body if isinstance(n, ast.AsyncFunctionDef) and n.name == "delivery_worker_loop")
exec(compile(ast.fix_missing_locations(ast.Module(body=[copy.deepcopy(worker_node)], type_ignores=[])), str(APP), "exec"), namespace)
worker = namespace["delivery_worker_loop"]

class ClaimCancellation(unittest.IsolatedAsyncioTestCase):
    async def test_clean_eof_waits_for_claim_and_releases_before_return(self):
        started, finish = threading.Event(), threading.Event()
        state = {"value": "received", "injected": False}
        def claim_next():
            started.set()
            if not finish.wait(1.0):
                raise TimeoutError("synthetic claim not released")
            state["value"] = "processing"
            return "synthetic-delivery", {}
        def release(ident):
            self.assertEqual(ident, "synthetic-delivery")
            state["value"] = "received"
        inbox = SimpleNamespace(claim_next=claim_next, release=release)
        async def recv():
            self.assertTrue(await asyncio.to_thread(started.wait, 1.0))
        async def worker():
            await claim(inbox)
            state["injected"] = True
        task = asyncio.create_task(supervise([recv(), worker()]))
        try:
            self.assertTrue(await asyncio.to_thread(started.wait, 1.0))
            await asyncio.sleep(0.02)
            self.assertFalse(task.done(), "cleanup did not wait for in-flight claim")
            finish.set()
            await asyncio.wait_for(task, 1.0)
            self.assertEqual(state, {"value": "received", "injected": False})
        finally:
            finish.set()
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_actual_worker_releases_cancelled_claim(self):
        started, finish, thread_done = threading.Event(), threading.Event(), threading.Event()
        state = {"value": "received", "injected": False}
        def claim_next():
            started.set()
            if not finish.wait(1.0):
                raise TimeoutError("synthetic claim not released")
            state["value"] = "processing"
            thread_done.set()
            return "synthetic-delivery", {}
        def release(ident):
            self.assertEqual(ident, "synthetic-delivery")
            state["value"] = "received"
        async def process(*args, **kwargs):
            state["injected"] = True
        h = SimpleNamespace(delivery_inbox=SimpleNamespace(claim_next=claim_next, release=release),
                            cfg=SimpleNamespace(mode="driver"), process_inbound_message=process)
        task = asyncio.create_task(worker(h, None))
        try:
            self.assertTrue(await asyncio.to_thread(started.wait, 1.0))
            task.cancel()
            finish.set()
            with self.assertRaises(asyncio.CancelledError):
                await asyncio.wait_for(task, 1.0)
            self.assertTrue(await asyncio.to_thread(thread_done.wait, 1.0))
            self.assertEqual(state, {"value": "received", "injected": False})
        finally:
            finish.set()
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_actual_worker_cancelled_during_idle_check_releases(self):
        idle_started, idle_finish = threading.Event(), threading.Event()
        state = {"value": "received", "injected": False}
        def claim_next():
            state["value"] = "processing"
            return "synthetic-delivery", {}
        def is_idle():
            idle_started.set()
            if not idle_finish.wait(1.0):
                raise TimeoutError("synthetic idle check not released")
            return True
        def release(ident):
            self.assertEqual(ident, "synthetic-delivery")
            state["value"] = "received"
        async def process(*args, **kwargs):
            state["injected"] = True
        h = SimpleNamespace(delivery_inbox=SimpleNamespace(claim_next=claim_next, release=release),
                            cfg=SimpleNamespace(mode="cli"), adapter=SimpleNamespace(is_idle=is_idle),
                            process_inbound_message=process)
        task = asyncio.create_task(worker(h, None))
        try:
            self.assertTrue(await asyncio.to_thread(idle_started.wait, 1.0))
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await asyncio.wait_for(task, 1.0)
            self.assertEqual(state, {"value": "received", "injected": False})
        finally:
            idle_finish.set()
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_actual_worker_cancellation_after_dispatch_does_not_requeue(self):
        processing_started = asyncio.Event()
        released = []
        async def process(*args, **kwargs):
            processing_started.set()
            await asyncio.Future()
        h = SimpleNamespace(delivery_inbox=SimpleNamespace(claim_next=lambda: ("D", {}), release=released.append),
                            cfg=SimpleNamespace(mode="driver"), process_inbound_message=process)
        task = asyncio.create_task(worker(h, None))
        try:
            await asyncio.wait_for(processing_started.wait(), 1.0)
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await asyncio.wait_for(task, 1.0)
            self.assertEqual(released, [], "already-dispatched work must not be auto-replayed")
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_busy_ack_or_cooldown_cancel_does_not_release_other_claim(self):
        for block_ack in (True, False):
            with self.subTest(block_ack=block_ack):
                ready = asyncio.Event()
                state = {"value": "received", "release_calls": 0, "injected": False}
                def claim_next():
                    state["value"] = "our-processing"
                    return "D", {}
                def release(ident):
                    self.assertEqual(ident, "D")
                    state["release_calls"] += 1
                    state["value"] = "received"
                async def send(payload):
                    # A second claimant acquired the row after our busy release.
                    state["value"] = "other-processing"
                    ready.set()
                    if block_ack:
                        await asyncio.Future()
                async def process(*args, **kwargs):
                    state["injected"] = True
                h = SimpleNamespace(delivery_inbox=SimpleNamespace(claim_next=claim_next, release=release),
                                    cfg=SimpleNamespace(mode="cli"), adapter=SimpleNamespace(is_idle=lambda: False),
                                    delivery_busy_acked=set(), delivery_ack=lambda *a, **k: {},
                                    process_inbound_message=process)
                task = asyncio.create_task(worker(h, SimpleNamespace(send=send)))
                try:
                    await asyncio.wait_for(ready.wait(), 1.0)
                    self.assertEqual(state["release_calls"], 1)
                    task.cancel()
                    with self.assertRaises(asyncio.CancelledError):
                        await asyncio.wait_for(task, 1.0)
                    self.assertEqual(state, {"value": "other-processing", "release_calls": 1, "injected": False})
                finally:
                    task.cancel()
                    await asyncio.gather(task, return_exceptions=True)

    async def test_cancel_during_busy_release_waits_without_second_release(self):
        started, finish = threading.Event(), threading.Event()
        lock = threading.Lock()
        state = {"value": "our-processing", "release_calls": 0}
        def release(ident):
            self.assertEqual(ident, "D")
            with lock:
                state["release_calls"] += 1
                call = state["release_calls"]
            if call == 1:
                started.set()
                if not finish.wait(1.0):
                    raise TimeoutError("synthetic busy release not unblocked")
                state["value"] = "other-processing"
            else:
                state["value"] = "received"
        h = SimpleNamespace(delivery_inbox=SimpleNamespace(claim_next=lambda: ("D", {}), release=release),
                            cfg=SimpleNamespace(mode="cli"), adapter=SimpleNamespace(is_idle=lambda: False),
                            delivery_busy_acked=set())
        task = asyncio.create_task(worker(h, None))
        try:
            self.assertTrue(await asyncio.to_thread(started.wait, 1.0))
            task.cancel()
            await asyncio.sleep(0.02)
            self.assertFalse(task.done(), "cancellation returned while busy release still ran")
            self.assertEqual(state["release_calls"], 1, "cancel recovery launched a second release")
            finish.set()
            with self.assertRaises(asyncio.CancelledError):
                await asyncio.wait_for(task, 1.0)
            self.assertEqual(state, {"value": "other-processing", "release_calls": 1})
        finally:
            finish.set()
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_success_keeps_claim_for_dispatch(self):
        released = []
        inbox = SimpleNamespace(claim_next=lambda: ("D", {}), release=released.append)
        self.assertEqual(await asyncio.wait_for(claim(inbox), 1.0), ("D", {}))
        self.assertEqual(released, [])

    async def test_cancel_empty_claim_does_not_release(self):
        started, finish = threading.Event(), threading.Event()
        released = []
        def claim_next():
            started.set()
            if not finish.wait(1.0):
                raise TimeoutError("synthetic claim not released")
            return None
        task = asyncio.create_task(claim(SimpleNamespace(claim_next=claim_next, release=released.append)))
        try:
            self.assertTrue(await asyncio.to_thread(started.wait, 1.0))
            task.cancel()
            finish.set()
            with self.assertRaises(asyncio.CancelledError):
                await asyncio.wait_for(task, 1.0)
            self.assertEqual(released, [])
        finally:
            finish.set()
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
