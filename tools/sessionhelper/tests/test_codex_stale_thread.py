"""Admission after reconnect, durable uncertainty, and immutable historical binding.

All transports are local fakes; the separate real_protocol.py checks the exact
0.159.2 server. No credentials, managed daemon, or external model are used here.
"""
import json
import io
import os
from pathlib import Path
import queue
import tempfile
import threading
import types
import unittest
from unittest import mock

from sessionhelper.codexDriver import CodexProtocolDriver, JsonRpcError, StdioJsonRpc, UnixWebSocketJsonRpc
from sessionhelper.driver_adapter import DriverAdapter


class RPC:
    def __init__(self, cwd):
        self.cwd, self.calls, self.errors = cwd, [], {}
        self.loaded = True
        self.live = True
        self.thread = 'bound-thread'
        self.result_override = None

    def healthy(self):
        return self.live

    def notify(self, *args):
        pass

    def request(self, method, params, timeout=0):
        self.calls.append((method, params))
        if method in self.errors:
            raise self.errors[method]
        if method == 'initialize':
            return {}
        if method in ('thread/read', 'thread/resume'):
            if method == 'thread/resume':
                self.loaded = True
            if self.result_override is not None:
                return self.result_override
            return {'thread': {'id': self.thread, 'cwd': self.cwd,
                               'status': {'type': 'idle' if self.loaded else 'notLoaded'},
                               'canAcceptDirectInput': True}}
        if method == 'thread/turns/list':
            return {'data': []}
        if method == 'turn/start':
            return {'turn': {'id': 'turn-' + params['clientUserMessageId'], 'status': 'inProgress'}}
        if method == 'thread/start':
            return {'thread': {'id': 'bound-thread'}}
        return {}


class CodexStaleThread(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.driver = CodexProtocolDriver(workdir=self.tmp.name, store_path=str(Path(self.tmp.name) / 'ledger.db'))
        self.rpc = RPC(self.tmp.name)
        self.driver._rpc = self.rpc
        self.driver._initialized = True
        self.driver._thread_id = self.driver._session_id = 'bound-thread'

    def methods(self):
        return [m for m, _ in self.rpc.calls]

    def test_loaded_thread_validated_before_admission(self):
        self.assertEqual(self.driver.deliver('one', 'input').state, 'processing')
        self.assertIn('thread/read', self.methods(), 'THREAD_ADMISSION_UNVALIDATED')
        self.assertLess(self.methods().index('thread/read'), self.methods().index('turn/start'), 'THREAD_ADMISSION_UNVALIDATED')
        self.assertNotIn('thread/resume', self.methods())
        self.assertNotIn('thread/start', self.methods())

    def test_reconnect_restores_exact_persisted_thread_before_turn(self):
        self.rpc.loaded = False
        receipt = self.driver.deliver('one', 'input')
        self.assertEqual(receipt.state, 'processing', 'THREAD_RESUME_NOT_PERFORMED')
        self.assertLess(self.methods().index('thread/resume'), self.methods().index('turn/start'), 'THREAD_RESUME_NOT_PERFORMED')
        self.assertEqual(self.rpc.calls[self.methods().index('thread/resume')][1], {'threadId': 'bound-thread', 'excludeTurns': True})
        self.assertEqual(self.rpc.calls[-1][1]['threadId'], 'bound-thread')
        self.assertNotIn('thread/start', self.methods())

    def test_read_not_loaded_error_is_resumable_not_missing(self):
        self.rpc.errors['thread/read'] = JsonRpcError('thread/read', {'code': -32600, 'message': 'thread not loaded: bound-thread'})
        self.assertEqual(self.driver.deliver('one', 'input').state, 'processing', 'NOT_LOADED_MISCLASSIFIED')
        self.assertIn('thread/resume', self.methods())

    def test_same_rpc_new_connection_generation_requires_validation(self):
        self.driver._verified_binding = self.driver._binding_key()
        self.driver._initialize()
        self.rpc.errors['thread/read'] = TimeoutError('private error body')
        receipt = self.driver.deliver('one', 'input')
        self.assertEqual(receipt.state, 'failed', 'CONNECTION_GENERATION_NOT_INVALIDATED')
        self.assertIn('error_class=timeout', receipt.detail)
        self.assertNotIn('turn/start', self.methods())

    def test_rpc_replacement_invalidates_same_generation(self):
        self.driver._verified_binding = self.driver._binding_key()
        self.rpc = RPC(self.tmp.name)
        self.rpc.errors['thread/read'] = ConnectionError('closed')
        self.driver._rpc = self.rpc
        receipt = self.driver.deliver('one', 'input')
        self.assertEqual(receipt.state, 'failed', 'REPLACED_CONNECTION_NOT_VALIDATED')
        self.assertIn('error_class=disconnected', receipt.detail)
        self.assertNotIn('turn/start', self.methods())

    def test_missing_thread_never_creates_or_replays_and_retains_ledger(self):
        self.rpc.loaded = False
        self.rpc.errors['thread/resume'] = JsonRpcError('thread/resume', {'code': -32600, 'message': 'no rollout found for thread id bound-thread'})
        receipt = self.driver.deliver('one', 'input')
        self.assertEqual(receipt.state, 'failed', 'MISSING_THREAD_ADMITTED')
        self.assertTrue(receipt.detail.startswith('session_unavailable:'))
        self.assertIn('error_class=missing_thread', receipt.detail)
        self.assertNotIn('turn/start', self.methods(), 'MISSING_THREAD_ADMITTED')
        self.assertNotIn('thread/start', self.methods(), 'MISSING_THREAD_REPLACED')
        record = self.driver._store.get_by_envelope('one')
        self.assertEqual(record.thread_id, 'bound-thread')
        self.assertEqual(record.state, 'failed')
        before = list(self.rpc.calls)
        self.driver.deliver('one', 'input')
        self.assertEqual(self.rpc.calls, before, 'MISSING_THREAD_REPLAYED')
        self.assertEqual(self.driver._thread_id, 'bound-thread')

    def test_binding_result_identity_directory_and_admission_are_strict(self):
        for field, value in [('id', 'other'), ('cwd', '/other'), ('cwd', None), ('status', {'type': 'systemError'}),
                             ('canAcceptDirectInput', False), ('canAcceptDirectInput', 1), ('canAcceptDirectInput', None)]:
            with self.subTest(field=field, value=value):
                self.rpc.calls.clear()
                self.driver._verified_binding = None
                thread = {'id': 'bound-thread', 'cwd': self.tmp.name, 'status': {'type': 'idle'}, 'canAcceptDirectInput': True}
                thread[field] = value
                self.rpc.result_override = {'thread': thread}
                receipt = self.driver.deliver(str((field, value)), 'input')
                self.assertEqual(receipt.state, 'failed', 'INVALID_BINDING_ADMITTED')
                self.assertNotIn('turn/start', self.methods())

    def test_validation_errors_are_classified_without_guessing_or_private_text(self):
        for i, (error, category) in enumerate([
            (TimeoutError('private bearer text'), 'timeout'), (ConnectionError('private closed'), 'disconnected'),
            (JsonRpcError('thread/read', {'code': 401, 'message': 'private token'}), 'authentication'),
            (JsonRpcError('thread/read', {'code': -32600, 'message': 'invalid parameter'}), 'rpc_error'),
        ]):
            with self.subTest(category=category):
                self.rpc.calls.clear()
                self.driver._verified_binding = None
                self.rpc.errors['thread/read'] = error
                receipt = self.driver.deliver(str(i), 'input')
                self.assertEqual(receipt.state, 'failed')
                self.assertIn('error_class=' + category, receipt.detail, 'RPC_CLASSIFICATION_LOST')
                self.assertNotIn('private', receipt.detail)
                self.assertNotIn('thread/resume', self.methods())
                self.assertNotIn('turn/start', self.methods())

    def test_missing_turn_start_invalidates_binding_without_new_thread(self):
        self.rpc.errors['turn/start'] = JsonRpcError('turn/start', {'code': -32600, 'message': 'thread not found: bound-thread'})
        receipt = self.driver.deliver('one', 'input')
        self.assertIn('error_class=missing_thread', receipt.detail, 'TURN_MISSING_NOT_CLASSIFIED')
        self.assertIsNone(self.driver._verified_binding)
        self.assertNotIn('thread/start', self.methods())
        self.assertEqual(self.driver._store.get_by_envelope('one').state, 'failed')

    def test_timeout_and_disconnect_after_dispatch_never_resubmit_across_restart(self):
        for i, error in enumerate([TimeoutError('unknown acceptance'), ConnectionError('EOF')]):
            with self.subTest(error=type(error).__name__):
                self.driver._active_turn_id = ''
                self.rpc.errors['turn/start'] = error
                eid = 'uncertain-' + str(i)
                receipt = self.driver.deliver(eid, 'input')
                self.assertEqual(receipt.state, 'failed')
                self.assertTrue(receipt.detail.startswith('model_admission_unknown:'))
                record = self.driver._store.get_by_envelope(eid)
                self.assertEqual(record.state, 'unknown', 'UNCERTAIN_ADMISSION_NOT_DURABLE')
                before = len([m for m in self.methods() if m == 'turn/start'])
                self.driver.deliver(eid, 'input')
                self.assertEqual(len([m for m in self.methods() if m == 'turn/start']), before, 'UNCERTAIN_ADMISSION_REPLAYED')
                restarted = CodexProtocolDriver(store_path=str(Path(self.tmp.name) / 'ledger.db'))
                restarted._request = mock.Mock(side_effect=AssertionError('must not contact server'))
                restarted.start = mock.Mock(side_effect=AssertionError('must not create connection'))
                self.assertEqual(restarted.deliver(eid, 'input').state, 'failed')
                self.assertEqual(restarted._thread_id, '')
                self.assertEqual(len([m for m in self.methods() if m == 'turn/start']), before, 'UNCERTAIN_ADMISSION_REPLAYED')

    def test_unclassified_turn_error_is_uncertain_and_never_replayed(self):
        self.rpc.errors['turn/start'] = JsonRpcError('turn/start', {'code': -32603, 'message': 'internal failure after unknown work'})
        receipt = self.driver.deliver('one', 'input')
        self.assertTrue(receipt.detail.startswith('model_admission_unknown:'), 'RPC_ERROR_AMBIGUITY_LOST')
        self.assertIn('error_class=rpc_error', receipt.detail)
        self.assertEqual(self.driver._store.get_by_envelope('one').state, 'unknown', 'RPC_ERROR_AMBIGUITY_LOST')
        self.driver.deliver('one', 'input')
        self.assertEqual(self.methods().count('turn/start'), 1, 'RPC_ERROR_AMBIGUITY_REPLAYED')

    def test_pending_marker_precedes_dispatch_and_malformed_success_is_uncertain(self):
        original = self.rpc.request
        observed = []
        def request(method, params, timeout=0):
            if method == 'turn/start':
                record = self.driver._store.get_by_envelope('one')
                observed.append(record.state if record else None)
                return {'turn': {}}
            return original(method, params, timeout)
        self.rpc.request = request
        receipt = self.driver.deliver('one', 'input')
        self.assertEqual(observed, ['unknown'], 'DISPATCH_NOT_DURABLE')
        self.assertTrue(receipt.detail.startswith('model_admission_unknown:'))
        self.assertEqual(self.driver._store.get_by_envelope('one').state, 'unknown')

    def test_historical_receipts_do_not_change_active_binding_or_create_thread(self):
        for state in ['processing', 'done', 'failed']:
            with self.subTest(state=state):
                eid = 'old-' + state
                self.driver._store.upsert('historical-thread', eid, 'old-turn', 'hash', state, 'historical')
                self.driver._verified_binding = self.driver._binding_key()
                binding = self.driver._verified_binding
                self.driver.deliver(eid, 'input')
                self.assertEqual((self.driver._thread_id, self.driver._session_id), ('bound-thread', 'bound-thread'), 'HISTORICAL_RECEIPT_REBOUND_ACTIVE')
                self.assertEqual(self.driver._verified_binding, binding)
                self.assertEqual(self.methods(), [], 'HISTORICAL_ACCEPTED_REPLAYED')
        self.driver._thread_id = self.driver._session_id = ''
        self.driver.start = mock.Mock(side_effect=AssertionError('must not start'))
        self.driver.deliver('old-done', 'input')
        self.assertEqual(self.driver._thread_id, '', 'HISTORICAL_RECEIPT_CREATED_THREAD')

    def test_historical_poll_and_state_update_use_record_thread(self):
        self.driver._store.upsert('historical-thread', 'old', 'old-turn', 'hash', 'processing', '')
        calls = []
        def read(turn_id, **kw):
            calls.append((turn_id, kw))
            return 'failed', 'explicit failure'
        self.driver._read_turn_state = read
        receipt = self.driver.poll_receipt('old')
        self.assertEqual(calls, [('old-turn', {'thread_id': 'historical-thread'})], 'HISTORICAL_POLL_WRONG_THREAD')
        self.assertEqual(receipt.state, 'failed')
        self.assertEqual(self.driver._store.get('historical-thread', 'old').state, 'failed', 'HISTORICAL_UPDATE_WRONG_THREAD')
        self.assertEqual(self.driver._thread_id, 'bound-thread')

    def test_historical_interrupt_confirm_reads_same_record_thread(self):
        self.driver._store.upsert('historical-thread', 'old', 'old-turn', 'hash', 'processing', '')
        self.driver._envelope_to_turn['old'] = 'old-turn'
        reads = []
        def read(turn, **kw):
            reads.append(kw)
            return 'failed', 'interrupted'
        self.driver._read_turn_state = read
        self.assertTrue(self.driver.interrupt_delivery('old', 'old-turn'))
        self.assertEqual(self.rpc.calls[0][1]['threadId'], 'historical-thread')
        self.assertEqual(reads, [{'thread_id': 'historical-thread'}], 'HISTORICAL_INTERRUPT_WRONG_THREAD')

    def test_new_thread_is_created_only_for_initial_unbound_session(self):
        self.driver._thread_id = self.driver._session_id = ''
        self.assertEqual(self.driver.deliver('first', 'input').state, 'processing')
        self.assertEqual(self.methods().count('thread/start'), 1)
        self.assertNotIn('thread/resume', self.methods())

    def test_reconnect_cannot_replace_rpc_between_validation_and_dispatch(self):
        entered, release, replaced = threading.Event(), threading.Event(), threading.Event()
        original = self.rpc.request
        def request(method, params, timeout=0):
            if method == 'thread/read':
                entered.set()
                self.assertTrue(release.wait(3))
            if method == 'turn/start':
                self.assertFalse(replaced.is_set(), 'RECONNECT_CROSSED_ADMISSION_LOCK')
            return original(method, params, timeout)
        self.rpc.request = request
        outcomes = []
        delivery = threading.Thread(target=lambda: outcomes.append(self.driver.deliver('one', 'input')))
        delivery.start()
        self.assertTrue(entered.wait(3))
        self.rpc.live = False
        self.rpc.stop = lambda: None
        def start_locked():
            replaced.set()
        self.driver._start_locked = start_locked
        reconnect = threading.Thread(target=self.driver.start)
        reconnect.start()
        self.assertFalse(replaced.wait(0.05))
        release.set()
        delivery.join(3)
        reconnect.join(3)
        self.assertFalse(delivery.is_alive())
        self.assertFalse(reconnect.is_alive())
        self.assertEqual(outcomes[0].state, 'processing')
        self.assertTrue(replaced.is_set())

    def _adapter_failure(self, reason):
        if reason == 'session_unavailable':
            self.rpc.loaded = False
            self.rpc.errors['thread/resume'] = JsonRpcError('thread/resume', {'code': -32600, 'message': 'no rollout found for thread id bound-thread'})
        else:
            self.rpc.errors['turn/start'] = TimeoutError('unknown acceptance')
        cfg = types.SimpleNamespace(busy_buffer_max=20, turn_terminal_timeout_seconds=2)
        with mock.patch.object(DriverAdapter, '_build_driver', return_value=self.driver):
            adapter = DriverAdapter(cfg)
        adapter._started = True  # run one queued command synchronously, no background threads
        injected, admitted = [], []
        adapter.handle_delivery({'id': 'one', 'body': 'input', 'meta': {}},
                                on_injected=lambda: injected.append(True), on_admitted=lambda: admitted.append(True))
        self.assertEqual(injected, [], 'ENQUEUE_IS_NOT_MODEL_ADMISSION')
        command = next(iter(adapter._commands._by_delivery.values()))
        adapter._deliver_regular_command(command)
        events = []
        while not adapter._event_q.empty():
            events.append(json.loads(adapter._event_q.get_nowait()))
        failed = [x for x in events if x.get('data', {}).get('reason') == reason]
        self.assertEqual(len(failed), 1, 'ADMISSION_FAILURE_EVENT_NOT_VISIBLE')
        self.assertEqual(failed[0]['delivery_id'], command.delivery_id)
        self.assertEqual(failed[0]['data']['outcome'], 'failed')
        self.assertEqual(failed[0]['kind'], 'state_change')
        self.assertEqual(admitted, [])
        self.assertEqual(injected, [])
        self.assertFalse(any(x.get('kind') == 'assistant_text' for x in events))
        self.assertTrue(adapter._term_q.empty())

    def test_adapter_rejected_admission_has_no_injected_ack_and_publishes_failure(self):
        self._adapter_failure('session_unavailable')

    def test_adapter_uncertain_admission_is_visible_and_not_a_success(self):
        self._adapter_failure('model_admission_unknown')


class StructuredAdmissionOutput(unittest.IsolatedAsyncioTestCase):
    async def test_session_event_loop_forwards_failure_with_delivery_correlation(self):
        from sessionhelper.app import SessionHelper
        class EndProbe(Exception):
            pass
        payload = json.dumps({'kind': 'state_change', 'delivery_id': 'dw-local',
                              'data': {'change': 'turn_completed', 'outcome': 'failed', 'reason': 'session_unavailable'}})
        helper = object.__new__(SessionHelper)
        helper.adapter = types.SimpleNamespace(next_session_event=lambda timeout: payload)
        helper.cfg = types.SimpleNamespace(session_name='local-fixture', key_id='local-fixture')
        helper.book = types.SimpleNamespace(self_addr='local-fixture')
        sent = []
        class WS:
            async def send(self, frame):
                sent.append(json.loads(frame))
                raise EndProbe()
        with mock.patch.dict(os.environ, {'SH_EMIT_SESSION_EVENT': '1'}):
            with self.assertRaises(EndProbe):
                await helper.session_event_loop(WS())
        self.assertEqual(len(sent), 1, 'SESSION_EVENT_FAILURE_NOT_FORWARDED')
        self.assertEqual(sent[0]['meta']['type'], 'session_event')
        self.assertEqual(json.loads(sent[0]['body'])['delivery_id'], 'dw-local')
        self.assertEqual(json.loads(sent[0]['body'])['data']['reason'], 'session_unavailable')
        self.assertNotEqual(sent[0]['meta']['type'], 'delivery_ack')


class RPCErrorShapes(unittest.TestCase):
    def test_missing_thread_requires_exact_method_code_and_id(self):
        for method, code, message in [('turn/start', -32600, 'thread not found: bound-thread'), ('thread/resume', -32600, 'no rollout found for thread id bound-thread')]:
            self.assertEqual(JsonRpcError(method, {'code': code, 'message': message}).classify('bound-thread'), 'missing_thread', 'MISSING_ERROR_NOT_RECOGNIZED')
            for wrong in [{'code': -32000, 'message': message}, {'code': str(code), 'message': message}, {'code': code, 'message': message + ' extra'}]:
                self.assertEqual(JsonRpcError(method, wrong).classify('bound-thread'), 'rpc_error', 'MISSING_ERROR_OVERMATCHED')
            self.assertEqual(JsonRpcError(method, {'code': code, 'message': message}).classify('different'), 'rpc_error')

    def test_real_reader_eof_is_disconnect_and_pending_send_failure_is_cleaned(self):
        for cls in [StdioJsonRpc, UnixWebSocketJsonRpc]:
            with self.subTest(transport=cls.__name__):
                rpc = cls(['unused'], cwd='/tmp') if cls is StdioJsonRpc else cls('/unused')
                if cls is StdioJsonRpc:
                    rpc._proc = types.SimpleNamespace(poll=lambda: None, stdout=io.StringIO(''),
                        stdin=types.SimpleNamespace(write=lambda text: rpc._read_stdout(), flush=lambda: None))
                else:
                    rpc._recv_text = lambda: None
                    rpc._send_text = lambda text: rpc._read_loop()
                try:
                    rpc.request('turn/start', {}, timeout=0.05)
                except Exception as error:
                    self.assertIsInstance(error, ConnectionError, 'EOF_MISCLASSIFIED_AS_TIMEOUT')
                else:
                    self.fail('EOF_MISCLASSIFIED_AS_TIMEOUT: no exception')
                self.assertEqual(rpc._pending, {})
                def fail(*args):
                    raise BrokenPipeError('send failed')
                if cls is StdioJsonRpc:
                    rpc._proc.stdin.write = fail
                else:
                    rpc._send_text = fail
                with self.assertRaises(BrokenPipeError):
                    rpc.request('turn/start', {}, timeout=0.05)
                self.assertEqual(rpc._pending, {}, 'FAILED_SEND_LEAKS_PENDING_REQUEST')

    def test_both_transports_preserve_rpc_error_without_private_payload_in_string(self):
        for cls in [StdioJsonRpc, UnixWebSocketJsonRpc]:
            with self.subTest(transport=cls.__name__):
                rpc = cls(['unused'], cwd='/tmp') if cls is StdioJsonRpc else cls('/unused')
                def send(text):
                    msg = json.loads(text)
                    rpc._pending[msg['id']].put({'id': msg['id'], 'error': {'code': -32600, 'message': 'thread not found: bound-thread', 'data': 'PRIVATE_TEST_PAYLOAD'}})
                rpc._send_text = send
                rpc._proc = types.SimpleNamespace(poll=lambda: None, stdin=types.SimpleNamespace(write=send, flush=lambda: None))
                caught = None
                try:
                    rpc.request('turn/start', {'threadId': 'bound-thread'})
                except Exception as error:
                    caught = error
                self.assertIsInstance(caught, JsonRpcError, 'RPC_ERROR_SHAPE_DISCARDED')
                self.assertEqual(caught.classify('bound-thread'), 'missing_thread', 'RPC_ERROR_SHAPE_DISCARDED')
                self.assertNotIn('PRIVATE', str(caught))
                self.assertNotIn('bound-thread', str(caught), 'RAW_RPC_ERROR_EXPOSED')


if __name__ == '__main__':
    unittest.main()
