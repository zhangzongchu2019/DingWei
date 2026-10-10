"""AST-only terminal startup ownership; stdlib and in-memory adapters only."""
import ast
import asyncio
import inspect
import json
from pathlib import Path
from types import SimpleNamespace
import unittest


def terminal_method():
    tree = ast.parse((Path(__file__).parents[1] / "sessionhelper/app.py").read_text())
    cls = next(n for n in tree.body if isinstance(n, ast.ClassDef) and n.name == "SessionHelper")
    method = next(n for n in cls.body if isinstance(n, ast.AsyncFunctionDef) and n.name == "terminal_loop")
    async def to_thread(fn, *args):
        result = fn(*args)
        return await result if inspect.isawaitable(result) else result
    facade = SimpleNamespace(to_thread=to_thread, Future=asyncio.Future,
        create_task=asyncio.create_task, gather=asyncio.gather, get_running_loop=asyncio.get_running_loop)
    ns = {"asyncio": facade, "json": json,
          "session_addr": lambda *args: "fixture",
          "envelope": lambda to, body, origin, meta: {"body": body}}
    module = ast.fix_missing_locations(ast.Module(body=[method], type_ignores=[]))
    exec(compile(module, "app.py:terminal_loop", "exec"), ns)
    return ns["terminal_loop"]


def helper(adapter):
    return SimpleNamespace(adapter=adapter, cfg=SimpleNamespace(mode="driver",session_name="fixture",key_id="fixture"),book=SimpleNamespace(self_addr="fixture"))


class StartFailed(Exception):
    pass


class TerminalStartTest(unittest.IsolatedAsyncioTestCase):
    async def test_start_failure_propagates_and_is_retrieved(self):
        errors=[]
        loop=asyncio.get_running_loop();old=loop.get_exception_handler()
        loop.set_exception_handler(lambda loop, context: errors.append(context))
        async def start(): raise StartFailed("synthetic")
        async def chunk(timeout):
            await asyncio.sleep(0.001)
            return ""
        try:
            with self.assertRaises(StartFailed):
                try:
                    await asyncio.wait_for(terminal_method()(helper(SimpleNamespace(start=start,next_terminal_chunk=chunk)),None),0.5)
                except TimeoutError:
                    self.fail("startup failure was never propagated")
            await asyncio.sleep(0)
            self.assertEqual(errors,[])
        finally:
            loop.set_exception_handler(old)

    async def test_cancellation_reaps_pending_start_coroutine(self):
        entered=asyncio.Event();cleaned=asyncio.Event()
        async def start():
            entered.set()
            try: await asyncio.Future()
            finally:
                await asyncio.sleep(0.01)
                cleaned.set()
        async def chunk(timeout): await asyncio.Future()
        task=asyncio.create_task(terminal_method()(helper(SimpleNamespace(start=start,next_terminal_chunk=chunk)),None))
        await asyncio.wait_for(entered.wait(),0.5)
        task.cancel()
        with self.assertRaises(asyncio.CancelledError): await asyncio.wait_for(task,0.5)
        self.assertTrue(cleaned.is_set())

    async def test_startup_output_is_visible_before_start_finishes(self):
        allow_start=asyncio.Event();sent=asyncio.Event();completed=asyncio.Event();calls=[]
        async def start():
            calls.append("start")
            await allow_start.wait();completed.set()
        first=True
        async def chunk(timeout):
            nonlocal first
            if first: first=False;return "booting"
            await asyncio.sleep(min(timeout,0.02));return ""
        class WS:
            async def send(self,value):
                self.assertion=json.loads(value)["body"]
                if completed.is_set(): raise AssertionError("startup output was delayed")
                sent.set()
        ws=WS();task=asyncio.create_task(terminal_method()(helper(SimpleNamespace(start=start,next_terminal_chunk=chunk)),ws))
        try:
            try:
                await asyncio.wait_for(sent.wait(),0.5)
            except TimeoutError:
                self.fail("startup output was blocked until start finished")
            self.assertEqual(ws.assertion,"booting")
            self.assertEqual(calls,["start"])
            allow_start.set();await asyncio.wait_for(completed.wait(),0.5)
        finally:
            task.cancel()
            with self.assertRaises(asyncio.CancelledError): await asyncio.wait_for(task,0.5)

    async def test_send_failure_keeps_original_exception_and_reaps_start(self):
        entered=asyncio.Event();cleaned=asyncio.Event()
        class SendFailed(Exception):
            pass
        async def start():
            entered.set()
            try:
                await asyncio.Future()
            finally:
                await asyncio.sleep(0.01)
                cleaned.set()
        first=True
        async def chunk(timeout):
            nonlocal first
            if first:
                first=False
                return "booting"
            await asyncio.sleep(min(timeout,0.02))
            return ""
        class WS:
            async def send(self,value):
                raise SendFailed("synthetic connection failure")
        try:
            await asyncio.wait_for(terminal_method()(helper(SimpleNamespace(start=start,next_terminal_chunk=chunk)),WS()),0.5)
        except SendFailed:
            pass
        except asyncio.CancelledError:
            self.fail("startup cancellation masked the original send failure")
        else:
            self.fail("send failure was not propagated")
        self.assertTrue(entered.is_set())
        self.assertTrue(cleaned.is_set())

    async def test_completed_start_is_called_once_per_terminal_loop(self):
        calls=[];sent=asyncio.Event()
        async def start(): calls.append(1)
        async def chunk(timeout): await asyncio.sleep(0.01);return "output"
        class WS:
            async def send(self,value): sent.set()
        task=asyncio.create_task(terminal_method()(helper(SimpleNamespace(start=start,next_terminal_chunk=chunk)),WS()))
        try:
            await asyncio.wait_for(sent.wait(),0.5)
            self.assertEqual(calls,[1])
        finally:
            task.cancel()
            with self.assertRaises(asyncio.CancelledError): await asyncio.wait_for(task,0.5)


if __name__ == "__main__":
    unittest.main()
