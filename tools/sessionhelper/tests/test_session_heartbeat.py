"""R60-③ heartbeat_report: Hub self-report over the session WS, never datad.

Covers the capability gate (an old Hub must not receive an unknown control
subtype), nonce correlation, the fixed reply contract, definite negatives, and
the rule that a missing reply stays unknown instead of being resent.
"""
import asyncio
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from sessionhelper import mcp_shim
from sessionhelper.app import SessionHelper
from sessionhelper.session_heartbeat import (
    CAPABILITY_HEADER, CAPABILITY_VALUE, REQUEST_TYPE, RESULT_TYPE, SCHEMA,
    SessionHeartbeatReporter, heartbeat_payload)


def hub_ws(capable=True):
    headers = {CAPABILITY_HEADER: CAPABILITY_VALUE} if capable else {}
    return SimpleNamespace(response=SimpleNamespace(headers=headers))


class HeartbeatPayloadTest(unittest.TestCase):
    def test_fixed_schema_and_stripped_optional_text(self):
        self.assertEqual(heartbeat_payload({'status': ' blocked ', 'task': ' r60 ', 'eta': '', 'blocker': None}),
                         {'schema': SCHEMA, 'status': 'blocked', 'task': 'r60'})

    def test_rejects_unknown_status_oversize_and_control_text(self):
        for args in ({'status': 'paused'}, {'status': ''}, {'status': 5}, 'nope',
                     {'status': 'active', 'task': 'x' * 129},
                     {'status': 'active', 'blocker': 'a\nb'},
                     {'status': 'active', 'task': 7},
                     {'status': 'active', 'schema': 'x'}):
            with self.subTest(args=repr(args)[:40]), self.assertRaises(ValueError):
                heartbeat_payload(args)


class HeartbeatReporterTest(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.reporter = SessionHeartbeatReporter(['seat'])
        self.frames = []
        outer = self

        class WS:
            response = SimpleNamespace(headers={CAPABILITY_HEADER: CAPABILITY_VALUE})

            async def send(inner, raw):
                env = json.loads(raw)
                outer.frames.append(env)
                if outer.on_send:
                    outer.on_send(inner, env)

        self.ws = WS()
        self.on_send = None

    def reply(self, env, result=None, **overrides):
        frame = {'from': 'workpulse#fixture', 'to': 'seat#fixture',
                 'meta': {'type': RESULT_TYPE, 'nonce': env['meta']['nonce'],
                          'result': {'ok': True, 'status': 'recorded', 'recorded_at_ms': 123}
                          if result is None else result}}
        frame.update(overrides)
        return frame

    async def test_capability_gate_never_sends_to_a_hub_without_the_subtype(self):
        silent = hub_ws(capable=False)
        self.assertEqual(await self.reporter.report(silent, 'seat#fixture', 'fixture',
                                                    heartbeat_payload({'status': 'active'})),
                         {'ok': False, 'status': 'unsupported'})
        self.assertEqual(self.frames, [])

    async def test_correlated_reply_is_recorded_and_reply_contract_is_fixed(self):
        self.on_send = lambda ws, env: self.reporter.receive(ws, self.reply(
            env, {'ok': True, 'status': 'recorded', 'recorded_at_ms': 1794845140000,
                  'session': 'seat', 'token': 'secret', 'note': 'ignored'}))
        got = await self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                         heartbeat_payload({'status': 'busy', 'task': 'r60'}))
        self.assertEqual(got, {'ok': True, 'status': 'recorded', 'recorded_at_ms': 1794845140000})
        self.assertEqual(self.reporter.pending, {})
        self.assertEqual(len(self.frames), 1)
        frame = self.frames[0]
        self.assertEqual(frame['to'], 'workpulse#fixture')
        self.assertEqual(frame['from'], 'seat#fixture')
        self.assertEqual(frame['meta']['type'], REQUEST_TYPE)
        self.assertTrue(frame['meta']['no_mirror'])
        self.assertRegex(frame['meta']['nonce'], r'^[0-9a-f]{32}$')
        self.assertEqual(json.loads(frame['body']),
                         {'schema': SCHEMA, 'status': 'busy', 'task': 'r60'})

    async def test_throttled_and_invalid_are_definite_not_unknown(self):
        for result in ({'ok': False, 'status': 'throttled', 'retry_after_ms': 900},
                       {'ok': False, 'status': 'invalid'},
                       {'ok': False, 'status': 'unavailable'}):
            with self.subTest(status=result['status']):
                self.on_send = lambda ws, env, result=result: self.reporter.receive(ws, self.reply(env, result))
                self.assertEqual(await self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                                            heartbeat_payload({'status': 'active'})), result)

    async def test_uncorrelated_replies_never_resolve_and_never_resend(self):
        def bad(ws, env):
            for change in ('nonce', 'connection', 'sender', 'target', 'type', 'result'):
                frame = self.reply(env)
                conn = ws
                if change == 'nonce':
                    frame['meta']['nonce'] = 'b' * 32
                if change == 'connection':
                    conn = object()
                if change == 'sender':
                    frame['from'] = 'peer#fixture'
                if change == 'target':
                    frame['to'] = 'other#fixture'
                if change == 'type':
                    frame['meta']['type'] = 'delivery_status_result_v1'
                if change == 'result':
                    frame['meta']['result'] = 'recorded'
                if change == 'type':
                    self.assertFalse(self.reporter.receive(conn, frame))
                else:
                    self.assertTrue(self.reporter.receive(conn, frame))
        self.on_send = bad
        self.assertEqual(await self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                                    heartbeat_payload({'status': 'active'}), timeout=.02),
                         {'ok': False, 'status': 'unknown'})
        self.assertEqual(len(self.frames), 1)
        self.assertEqual(self.reporter.pending, {})

    async def test_no_reply_is_unknown_and_is_not_resent(self):
        self.assertEqual(await self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                                    heartbeat_payload({'status': 'idle'}), timeout=.02),
                         {'ok': False, 'status': 'unknown'})
        self.assertEqual(len(self.frames), 1)

    async def test_send_failure_and_cancellation_never_resend(self):
        from websockets.exceptions import ConnectionClosedError

        def closed(ws, env):
            raise ConnectionClosedError(None, None)
        self.on_send = closed
        self.assertEqual(await self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                                    heartbeat_payload({'status': 'active'})),
                         {'ok': False, 'status': 'unknown'})
        self.assertEqual(len(self.frames), 1)

        self.on_send = None
        task = asyncio.create_task(self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                                        heartbeat_payload({'status': 'active'})))
        await asyncio.sleep(0)
        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task
        self.assertEqual(self.reporter.pending, {})
        self.assertEqual(len(self.frames), 2)

    async def test_closed_connection_releases_waiter_as_unknown(self):
        task = asyncio.create_task(self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                                        heartbeat_payload({'status': 'active'}), timeout=5))
        await asyncio.sleep(0.01)
        self.assertEqual(len(self.reporter.pending), 1)
        self.reporter.abort(object())  # another connection's loss releases nothing
        await asyncio.sleep(0.01)
        self.assertFalse(task.done())
        self.assertEqual(len(self.reporter.pending), 1)
        self.reporter.abort(self.ws)
        # Released by this disconnect, not by the five second timeout.
        self.assertEqual(await asyncio.wait_for(task, 0.5), {'ok': False, 'status': 'unknown'})
        self.assertEqual(self.reporter.pending, {})
        self.assertEqual(len(self.frames), 1)  # released, never resent

    async def test_in_flight_limit_is_definite(self):
        self.reporter.pending = {f'{i:032x}': (self.ws, None) for i in range(8)}
        self.assertEqual(await self.reporter.report(self.ws, 'seat#fixture', 'fixture',
                                                    heartbeat_payload({'status': 'active'})),
                         {'ok': False, 'status': 'busy'})
        self.assertEqual(self.frames, [])


class HeartbeatHandlerWiringTest(unittest.IsolatedAsyncioTestCase):
    def helper(self):
        helper = SessionHelper.__new__(SessionHelper)
        helper.session_heartbeat = SessionHeartbeatReporter(['seat'])
        helper.book = SimpleNamespace(key_id='fixture', self_addr='seat#fixture')
        helper.cfg = SimpleNamespace(key_id='fixture')
        return helper

    async def test_handler_reports_over_the_live_connection(self):
        helper = self.helper()
        sent = []

        class WS:
            response = SimpleNamespace(headers={CAPABILITY_HEADER: CAPABILITY_VALUE})

            async def send(inner, raw):
                sent.append(json.loads(raw))
        got = await helper._make_heartbeat_handler(WS())({'type': 'heartbeat_v1', 'status': 'blocked',
                                                           'blocker': 'waiting for review'})
        self.assertEqual(got['status'], 'unknown')  # no Hub in this fixture
        self.assertEqual(len(sent), 1)
        self.assertEqual(json.loads(sent[0]['body'])['blocker'], 'waiting for review')

    async def test_handler_answers_invalid_arguments_without_a_frame(self):
        helper = self.helper()
        sent = []

        class WS:
            response = SimpleNamespace(headers={CAPABILITY_HEADER: CAPABILITY_VALUE})

            async def send(inner, raw):
                sent.append(raw)
        self.assertEqual(await helper._make_heartbeat_handler(WS())({'status': 'paused'}),
                         {'ok': False, 'status': 'invalid'})
        self.assertEqual(sent, [])

    async def test_recv_loop_consumes_the_control_reply(self):
        helper = self.helper()
        ws = hub_ws()
        future = asyncio.get_running_loop().create_future()
        helper.session_heartbeat.pending['a' * 32] = (ws, future)
        env = {'from': 'workpulse#fixture', 'to': 'seat#fixture',
               'meta': {'type': RESULT_TYPE, 'nonce': 'a' * 32, 'result': {'ok': True, 'status': 'recorded'}}}
        # Consumed (never model input) but a stale connection cannot resolve it.
        self.assertTrue(helper.session_heartbeat.receive(object(), env))
        self.assertFalse(future.done())
        self.assertTrue(helper.session_heartbeat.receive(ws, env))
        self.assertEqual(future.result(), {'ok': True, 'status': 'recorded'})
        # Cleanup belongs to the awaiting reporter, not to the receive path.


class HeartbeatIpcRoundTripTest(unittest.IsolatedAsyncioTestCase):
    async def roundtrip(self, handler, call=None, **serve_kwargs):
        from sessionhelper.ipc_listener import serve_ipc

        async def send(*args):
            raise AssertionError('heartbeat must not send a message')
        with tempfile.TemporaryDirectory(prefix='hb-ipc-') as root:
            sock = str(Path(root) / 'ipc.sock')
            bound = asyncio.Event()
            task = asyncio.create_task(serve_ipc(sock, send, bound=bound,
                                                 heartbeat_handler=handler, **serve_kwargs))
            try:
                await asyncio.wait_for(bound.wait(), 2)
                with patch.object(mcp_shim, '_IPC_SOCK', sock):
                    invoke = call or (lambda: mcp_shim._heartbeat_report({'status': 'active', 'task': 'r60'}))
                    return await asyncio.to_thread(invoke)
            finally:
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass

    async def test_shim_returns_the_hub_outcome_not_helper_rejected(self):
        calls = []

        async def handler(args):
            calls.append(args)
            return {'ok': True, 'status': 'recorded', 'recorded_at_ms': 7}
        got = await self.roundtrip(handler)
        self.assertEqual(got, {'ok': True, 'status': 'recorded', 'recorded_at_ms': 7})
        self.assertEqual(calls, [{'type': 'heartbeat_v1', 'request_id': calls[0]['request_id'],
                                  'status': 'active', 'task': 'r60'}])

    async def test_unsupported_hub_is_a_definite_answer(self):
        async def handler(args):
            return {'ok': False, 'status': 'unsupported'}
        self.assertEqual(await self.roundtrip(handler), {'ok': False, 'status': 'unsupported'})

    async def test_unknown_ipc_key_is_refused_before_the_handler(self):
        calls = []

        async def handler(args):
            calls.append(args)
            return {'ok': True, 'status': 'recorded'}
        with self.assertRaises(mcp_shim._IPCFailure) as caught:
            await self.roundtrip(handler, call=lambda: mcp_shim._ipc_request(
                {'type': 'heartbeat_v1', 'status': 'active', 'agent': 'other'}))
        self.assertEqual(caught.exception.category, 'helper_rejected')
        self.assertEqual(calls, [])

    async def test_raising_handler_is_a_definite_invalid_not_unknown(self):
        async def raising(args):
            raise ValueError('synthetic-private-detail')
        self.assertEqual(await self.roundtrip(raising), {'ok': False, 'status': 'invalid'})

    async def test_missing_handler_and_slow_handler_stay_definite(self):
        self.assertEqual(await self.roundtrip(None), {'ok': False, 'status': 'unsupported'})

        async def slow(args):
            await asyncio.sleep(9)
        self.assertEqual(await self.roundtrip(slow), {'ok': False, 'status': 'unknown'})

    async def test_restricted_sockets_refuse_the_heartbeat(self):
        from sessionhelper.ipc_listener import serve_ipc
        for mode in ('upload_only', 'send_body_only'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory(prefix='hb-ipc-') as root:
                sock = str(Path(root) / 'ipc.sock')
                bound = asyncio.Event()

                async def send(*args):
                    raise AssertionError('must not send')
                task = asyncio.create_task(serve_ipc(sock, send, bound=bound, upload_only=mode == 'upload_only',
                                                     send_body_only=mode == 'send_body_only',
                                                     heartbeat_handler=lambda args: None))
                try:
                    await asyncio.wait_for(bound.wait(), 2)
                    with patch.object(mcp_shim, '_IPC_SOCK', sock):
                        with self.assertRaises(mcp_shim._IPCFailure):
                            await asyncio.to_thread(mcp_shim._heartbeat_report, {'status': 'active'})
                finally:
                    task.cancel()
                    try:
                        await task
                    except asyncio.CancelledError:
                        pass

    async def test_shim_rejects_invalid_arguments_before_ipc(self):
        with patch.object(mcp_shim, '_ipc_request', side_effect=AssertionError('ipc reached')) as ipc:
            for args in ({'status': 'paused'}, {}, {'status': 'active', 'task': 'x' * 129},
                         {'status': 'active', 'extra': 1}):
                with self.subTest(args=repr(args)[:32]), self.assertRaises(ValueError):
                    mcp_shim._heartbeat_report(args)
        self.assertIsNone(ipc.side_effect and None)


if __name__ == '__main__':
    unittest.main()
