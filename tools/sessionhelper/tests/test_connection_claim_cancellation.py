"""AST-only claim cancellation regression with bounded synthetic threads."""
import ast
import asyncio
import copy
from pathlib import Path
import threading
from types import SimpleNamespace
import unittest

APP = Path(__file__).resolve().parents[1] / "sessionhelper" / "app.py"
namespace = {"asyncio": asyncio}
tree = ast.parse(APP.read_text())
for name in ("supervise_connection_loops", "claim_delivery_for_connection"):
    node = next(n for n in tree.body if isinstance(n, ast.AsyncFunctionDef) and n.name == name)
    exec(compile(ast.fix_missing_locations(ast.Module(body=[copy.deepcopy(node)], type_ignores=[])), str(APP), "exec"), namespace)
claim = namespace["claim_delivery_for_connection"]
supervise = namespace["supervise_connection_loops"]

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
