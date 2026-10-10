"""AST-only reconnect checks: synthetic clock and websocket exceptions, no network."""
import ast
import asyncio
import contextlib
import copy
import io
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

APP = Path(__file__).resolve().parents[1] / "sessionhelper" / "app.py"

class InvalidStatus(Exception):
    pass

class ConnectionClosedError(Exception):
    pass

class StopRun(BaseException):
    pass

class ReconnectBackoff(unittest.IsolatedAsyncioTestCase):
    async def run_schedule(self, durations, errors=None, producer=False):
        clock, sleeps = [0.0], []
        errors = errors or {}
        async def sleep(delay):
            sleeps.append(delay)
            if len(sleeps) >= len(durations):
                raise StopRun()
        namespace = {"asyncio": SimpleNamespace(sleep=sleep), "time": SimpleNamespace(monotonic=lambda: clock[0])}
        tree = ast.parse(APP.read_text())
        cls = next(n for n in tree.body if isinstance(n, ast.ClassDef) and n.name == "SessionHelper")
        node = next(n for n in cls.body if isinstance(n, ast.AsyncFunctionDef) and n.name == "run_forever")
        exec(compile(ast.fix_missing_locations(ast.Module(body=[copy.deepcopy(node)], type_ignores=[])), str(APP), "exec"), namespace)
        calls = [0]
        h = SimpleNamespace(cfg=SimpleNamespace(reconnect_min=1.0, reconnect_max=8.0, producer=producer), enroll_credential=None)
        async def run_once():
            i = calls[0]
            calls[0] += 1
            h._connection_started_at = clock[0]
            clock[0] += durations[i]
            if i in errors:
                raise errors[i]
        h._run_once = run_once
        with patch.dict("sys.modules", {"websockets": SimpleNamespace(InvalidStatus=InvalidStatus, ConnectionClosedError=ConnectionClosedError)}), contextlib.redirect_stdout(io.StringIO()):
            if producer:
                await asyncio.wait_for(namespace["run_forever"](h), 1.0)
            else:
                with self.assertRaises(StopRun):
                    await asyncio.wait_for(namespace["run_forever"](h), 1.0)
        return sleeps, calls[0]

    async def test_short_clean_closes_back_off_to_cap(self):
        sleeps, _ = await self.run_schedule([0.1] * 5)
        self.assertEqual(sleeps, [1, 2, 4, 8, 8])

    async def test_healthy_connection_resets_delay(self):
        sleeps, _ = await self.run_schedule([0.1, 0.1, 60.0, 0.1])
        self.assertEqual(sleeps, [1, 2, 1, 2])

    async def test_healthy_then_error_resets_delay(self):
        sleeps, _ = await self.run_schedule([0.1, 0.1, 60.0], {2: RuntimeError("synthetic")})
        self.assertEqual(sleeps, [1, 2, 1])

    async def test_replaced_still_has_ten_second_floor(self):
        sleeps, _ = await self.run_schedule([60.0], {0: ConnectionClosedError("replaced")})
        self.assertEqual(sleeps, [10])

    async def test_producer_finishes_without_reconnect(self):
        sleeps, calls = await self.run_schedule([0.1], producer=True)
        self.assertEqual((sleeps, calls), ([], 1))
