"""Admission after reconnect, durable uncertainty, and immutable historical binding.

All transports are local fakes; the separate real_protocol.py checks the exact
0.159.2 server. No credentials, managed daemon, or external model are used here.
"""
import asyncio
import contextlib
import json
from contextlib import nullcontext
import io
import os
from pathlib import Path
import queue
import sys
import tempfile
import threading
import types
import unittest
from unittest import mock

from sessionhelper.codexDriver import (CodexProtocolDriver, JsonRpcError, StdioJsonRpc,
                                      UnixWebSocketJsonRpc, _presubmit_validation_is_retryable)
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.inbox import DeliveryInbox
from sessionhelper.daemon_identity import DaemonIdentityError
from sessionhelper.app import SessionHelper
from sessionhelper.config import load_config
try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tests._home_isolation import HomeIsolation

_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


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
            error = self.errors[method]
            if isinstance(error, list):
                error = error.pop(0) if error else None
            if error is not None:
                raise error
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
        self.rpc.errors['thread/read'] = [TimeoutError('private error body'), None]
        waiting = []
        boundaries = []
        self.driver._busy_admission.set_callbacks(on_waiting=lambda did: waiting.append(did) or True,
                                                   before_submit=lambda did: boundaries.append(did) or True)
        with mock.patch('sessionhelper.codexDriver.PRESUBMIT_VALIDATION_BACKOFF', (0,)):
            receipt = self.driver.deliver('one', 'input')
        self.assertEqual(receipt.state, 'processing', 'TRANSIENT_READ_DID_NOT_RESUME')
        self.assertEqual(waiting, ['one'], 'TRANSIENT_READ_NOT_PERSISTED_AS_BUSY_WAIT')
        self.assertEqual(boundaries, ['one'], 'TRANSIENT_READ_DID_NOT_CROSS_BOUNDARY_AFTER_VALIDATION')
        self.assertEqual(self.methods().count('thread/read'), 2)
        self.assertEqual(self.methods().count('turn/start'), 1)
        self.assertLess(self.methods().index('thread/read'), self.methods().index('turn/start'))

    def test_rpc_replacement_invalidates_same_generation(self):
        self.driver._verified_binding = self.driver._binding_key()
        self.rpc = RPC(self.tmp.name)
        self.rpc.errors['thread/read'] = [ConnectionError('closed'), None]
        self.driver._rpc = self.rpc
        with mock.patch('sessionhelper.codexDriver.PRESUBMIT_VALIDATION_BACKOFF', (0,)):
            receipt = self.driver.deliver('one', 'input')
        self.assertEqual(receipt.state, 'processing', 'TRANSIENT_REPLACEMENT_READ_DID_NOT_RESUME')
        self.assertEqual(self.methods().count('thread/read'), 2)
        self.assertEqual(self.methods().count('turn/start'), 1)

    def test_missing_thread_never_creates_or_replays_and_retains_ledger(self):
        self.rpc.loaded = False
        self.rpc.errors['thread/resume'] = JsonRpcError('thread/resume', {'code': -32600, 'message': 'no rollout found for thread id bound-thread'})
        receipt = self.driver.deliver('one', 'input')
        self.assertEqual(receipt.state, 'queued', 'MISSING_THREAD_DROPPED_FIFO_HEAD')
        self.assertIn('missing_thread', receipt.detail)
        self.assertTrue(self.driver.presubmit_validation_hold('one'))
        self.assertNotIn('turn/start', self.methods(), 'MISSING_THREAD_ADMITTED')
        self.assertNotIn('thread/start', self.methods(), 'MISSING_THREAD_REPLACED')
        record = self.driver._store.get_by_envelope('one')
        self.assertIsNone(record, 'MISSING_THREAD_TERMINALIZED')
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
                self.assertEqual(receipt.state, 'queued', 'INVALID_BINDING_DROPPED_FIFO_HEAD')
                self.assertTrue(self.driver.presubmit_validation_hold(str((field, value))))
                self.assertNotIn('turn/start', self.methods())

    def test_validation_errors_are_classified_without_guessing_or_private_text(self):
        for i, (error, category) in enumerate([
            (JsonRpcError('thread/read', {'code': 401, 'message': 'private token'}), 'authentication'),
            (JsonRpcError('thread/read', {'code': -32600, 'message': 'invalid parameter'}), 'rpc_error'),
        ]):
            with self.subTest(category=category):
                self.rpc.calls.clear()
                self.driver._verified_binding = None
                self.rpc.errors['thread/read'] = error
                receipt = self.driver.deliver(str(i), 'input')
                self.assertEqual(receipt.state, 'queued')
                self.assertTrue(self.driver.presubmit_validation_hold(str(i)))
                self.assertIn(category, receipt.detail, 'RPC_CLASSIFICATION_LOST')
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
        # The original transport died during validation. Keeping the admission
        # lock must not turn this unsent delivery into a native submission.
        self.assertEqual(outcomes[0].state, 'queued', 'DISCONNECTED_PREFLIGHT_MUST_STAY_LOCAL_QUEUED')
        self.assertEqual(outcomes[0].source, 'local_queue')
        self.assertNotIn('turn/start', [method for method, _ in self.rpc.calls])
        self.assertTrue(replaced.is_set())

    def _adapter_failure(self, reason):
        if reason in ('session_unavailable', 'bound_thread_unavailable'):
            if reason == 'bound_thread_unavailable':
                self.driver._required_bound_thread_id = 'bound-thread'
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
        if reason in ('session_unavailable', 'bound_thread_unavailable'):
            self.assertEqual(failed, [], 'PRESUBMIT_MISMATCH_TERMINALIZED')
            self.assertIn(command.delivery_id, adapter._commands._by_delivery,
                          'PRESUBMIT_MISMATCH_LEFT_FIFO')
            self.assertTrue(self.driver.presubmit_validation_hold(command.delivery_id),
                            'PRESUBMIT_MISMATCH_OPERATOR_HOLD_MISSING')
            self.assertEqual(admitted, [])
            self.assertEqual(injected, [])
            self.assertTrue(adapter._term_q.empty())
            return
        self.assertEqual(len(failed), 1, 'ADMISSION_FAILURE_EVENT_NOT_VISIBLE')
        self.assertEqual(failed[0]['delivery_id'], command.delivery_id)
        self.assertEqual(failed[0]['data']['outcome'], 'failed')
        self.assertEqual(failed[0]['kind'], 'state_change')
        self.assertEqual(admitted, [])
        self.assertEqual(injected, [])
        self.assertFalse(any(x.get('kind') == 'assistant_text' for x in events))
        self.assertTrue(adapter._term_q.empty())

    def test_adapter_presubmit_mismatch_stays_queued_without_terminal_failure(self):
        self._adapter_failure('session_unavailable')

    def test_adapter_bound_thread_mismatch_stays_queued_without_terminal_failure(self):
        self._adapter_failure('bound_thread_unavailable')

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
    def test_presubmit_retry_classifier_is_narrow_and_read_only_only(self):
        self.assertTrue(_presubmit_validation_is_retryable(TimeoutError('synthetic')))
        self.assertTrue(_presubmit_validation_is_retryable(ConnectionError('synthetic')))
        self.assertTrue(_presubmit_validation_is_retryable(OSError(104,'reset')))
        self.assertFalse(_presubmit_validation_is_retryable(FileNotFoundError(2,'missing')))
        self.assertTrue(_presubmit_validation_is_retryable(
            JsonRpcError('thread/read',{'code':-32603,'message':'temporary'})))
        self.assertTrue(_presubmit_validation_is_retryable(
            JsonRpcError('thread/resume',{'code':-32001,'message':'temporary'})))
        self.assertFalse(_presubmit_validation_is_retryable(
            JsonRpcError('turn/start',{'code':-32603,'message':'ambiguous'})))
        self.assertFalse(_presubmit_validation_is_retryable(
            JsonRpcError('thread/read',{'code':401,'message':'401 Unauthorized'})))
        self.assertFalse(_presubmit_validation_is_retryable(DaemonIdentityError('exe_mismatch')))

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



class ConfiguredRootBinding(unittest.TestCase):
    def setUp(self):
        from sessionhelper.config import load_config
        # Transports are fake, but the real endpoint Spec validates its binary.
        # Use the fixture interpreter instead of requiring a host Codex install.
        codex_binary = mock.patch('sessionhelper.codexDriver._CODEX_BIN', sys.executable)
        codex_binary.start()
        self.addCleanup(codex_binary.stop)
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.env = dict(SH_DRIVER='codex', SH_SESSION_NAME='fixture', SH_OWNER='owner',
                        SH_KEY_ID='key', SH_SECRET='synthetic', SH_STATE_DIR=self.tmp.name,
                        SH_CLI_CWD=self.tmp.name, SH_CODEX_BOUND_THREAD_ID='bound-thread')
        with mock.patch.dict(os.environ, self.env):
            self.adapter = DriverAdapter(load_config(self.env))
        self.driver = self.adapter.driver
        self.rpc = RPC(self.tmp.name)
        self.driver._rpc, self.driver._initialized = self.rpc, True

    def methods(self):
        return [method for method, _ in self.rpc.calls]

    def test_configured_root_read_before_exact_turn(self):
        self.assertEqual(self.driver._thread_id, 'bound-thread')
        receipt = self.driver.deliver('bound-one', 'input')
        self.assertEqual(receipt.state, 'processing')
        self.assertEqual(self.methods(), ['thread/read', 'thread/turns/list', 'turn/start'])
        self.assertEqual(self.rpc.calls[-1][1]['threadId'], 'bound-thread', 'ROOT_BINDING_IGNORED')

    def test_configured_root_resumes_exact_thread(self):
        self.rpc.loaded = False
        self.assertEqual(self.driver.deliver('resume-one', 'input').state, 'processing')
        self.assertEqual(self.methods(), ['thread/read', 'thread/resume', 'thread/turns/list', 'turn/start'])
        self.assertEqual(self.rpc.calls[1][1]['threadId'], 'bound-thread')
        self.assertNotIn('thread/start', self.methods())

    def test_unavailable_never_creates_and_retains_operator_held_fifo_head(self):
        self.rpc.loaded = False
        self.rpc.errors['thread/resume'] = JsonRpcError('thread/resume', {
            'code': -32600, 'message': 'no rollout found for thread id bound-thread'})
        receipt = self.driver.deliver('missing', 'input')
        self.assertEqual(receipt.state, 'queued', 'BOUND_UNAVAILABLE_DROPPED_FIFO_HEAD')
        self.assertIn('missing_thread', receipt.detail)
        self.assertTrue(self.driver.presubmit_validation_hold('missing'))
        self.assertNotIn('thread/start', self.methods(), 'BOUND_UNAVAILABLE_CREATED_THREAD')
        self.assertNotIn('turn/start', self.methods())
        self.assertIsNone(self.driver._store.get('bound-thread', 'missing'))
        before = list(self.rpc.calls)
        self.rpc.errors.clear()
        self.assertEqual(self.driver.deliver('missing', 'input'), receipt)
        self.assertEqual(self.rpc.calls, before, 'OPERATOR_HELD_DELIVERY_RETRIED_WITHOUT_RESTART')

    def test_invalid_binding_remains_at_fifo_head_without_new_thread(self):
        for field, value in [('id', 'another-thread'), ('cwd', '/different-cwd'),
                             ('status', {'type': 'archived'}), ('canAcceptDirectInput', False)]:
            with self.subTest(field=field):
                thread = dict(id='bound-thread', cwd=self.tmp.name,
                              status={'type': 'idle'}, canAcceptDirectInput=True)
                thread[field] = value
                self.rpc.result_override = {'thread': thread}
                self.driver._verified_binding = None
                receipt = self.driver.deliver('invalid-' + field, 'input')
                self.assertEqual(receipt.state, 'queued')
                self.assertTrue(self.driver.presubmit_validation_hold('invalid-' + field))
                self.assertIn('invalid_thread_binding', receipt.detail)
        self.assertNotIn('thread/start', self.methods())
        self.assertNotIn('turn/start', self.methods())

    def test_reconnect_revalidates_and_alias_cannot_replace_pin(self):
        self.assertEqual(self.driver.deliver('first', 'input').state, 'processing')
        self.driver._active_turn_id = ''
        self.driver._connection_generation += 1
        self.driver._thread_id = self.driver._session_id = 'historical-alias'
        self.rpc.errors['thread/read'] = [TimeoutError('private details'), None]
        with mock.patch('sessionhelper.codexDriver.PRESUBMIT_VALIDATION_BACKOFF', (0,)):
            receipt = self.driver.deliver('second', 'input')
        self.assertEqual(receipt.state, 'processing')
        self.assertEqual([p.get('threadId') for m,p in self.rpc.calls if m == 'thread/read'],
                         ['bound-thread', 'bound-thread', 'bound-thread'])
        self.assertEqual(self.rpc.calls[-1][1]['threadId'], 'bound-thread')
        self.assertEqual(self.methods().count('turn/start'), 2)

    def test_unset_factory_preserves_legacy_constructor_arguments(self):
        from sessionhelper.config import load_config
        env = {k: v for k, v in self.env.items() if k != 'SH_CODEX_BOUND_THREAD_ID'}
        with mock.patch.dict(os.environ, env, clear=True), \
             mock.patch('sessionhelper.codexDriver.CodexProtocolDriver') as factory:
            DriverAdapter(load_config(env))
        self.assertNotIn('bound_thread_id', factory.call_args.kwargs)
        self.assertNotIn('require_bound_thread', factory.call_args.kwargs)

    def test_private_binding_conflict_stops_startup_and_reports_both_ids(self):
        endpoint = mock.Mock()
        endpoint.bound_thread.return_value = 'persisted-other-thread'
        driver = CodexProtocolDriver(workdir=self.tmp.name,
            store_path=str(Path(self.tmp.name) / 'private.db'),
            bound_thread_id='configured-thread', require_bound_thread=True,
            endpoint_mode='private', endpoint_state_dir=self.tmp.name,
            endpoint_seat='fixture', endpoint_codex_home=self.tmp.name,
            daemon_identity_check='enforce')
        with mock.patch('sessionhelper.codexDriver.PrivateEndpointClient', return_value=endpoint), \
             mock.patch('sessionhelper.codexDriver.UnixWebSocketJsonRpc',
                        side_effect=AssertionError('CONFLICT_REACHED_APP_SERVER')) as transport:
            with self.assertRaisesRegex(ValueError, 'configured-thread.*persisted-other-thread'):
                driver.start()
        transport.assert_not_called()
        endpoint.save_thread.assert_not_called()
        endpoint.release_writer.assert_called_once_with()
        self.assertFalse(driver.health())

    def test_private_matching_or_empty_binding_validates_before_saving(self):
        for saved in ('', 'bound-thread'):
            with self.subTest(saved=saved):
                endpoint = mock.Mock()
                endpoint.ensure.return_value = {'child': {'pid': os.getpid(), 'starttime': 1}}
                endpoint.admission.side_effect = lambda _: nullcontext()
                endpoint.spec.root = Path(self.tmp.name)
                endpoint.bound_thread.return_value = saved
                rpc = RPC(self.tmp.name)
                rpc.start = lambda: None
                rpc.stop = lambda: None
                rpc.peer_identity = rpc.identity_warning = None
                driver = CodexProtocolDriver(workdir=self.tmp.name,
                    store_path=str(Path(self.tmp.name) / ('private-' + (saved or 'empty') + '.db')),
                    bound_thread_id='bound-thread', require_bound_thread=True,
                    endpoint_mode='private', endpoint_state_dir=self.tmp.name,
                    endpoint_seat='fixture', endpoint_codex_home=self.tmp.name,
                    daemon_identity_check='enforce')
                with mock.patch('sessionhelper.codexDriver.PrivateEndpointClient', return_value=endpoint), \
                     mock.patch('sessionhelper.codexDriver.UnixWebSocketJsonRpc', return_value=rpc), \
                     mock.patch.object(driver, '_pump_notifications'):
                    driver.start()
                    endpoint.bound_thread.assert_called_once_with()
                    endpoint.save_thread.assert_not_called()
                    self.assertEqual(driver.deliver('private-' + saved, 'input').state, 'processing')
                    endpoint.save_thread.assert_called_once_with('bound-thread')
                    self.assertNotIn('thread/start', [method for method, _ in rpc.calls])
                    driver.stop()

    def test_configured_transport_unavailable_waits_without_submission(self):
        self.rpc.live = False
        with mock.patch.object(self.driver, 'start', side_effect=ConnectionError('closed')):
            receipt = self.driver.deliver('offline', 'input')
        self.assertEqual(receipt.state, 'queued')
        self.assertIn('presubmit validation transport unavailable', receipt.detail)
        self.assertEqual(self.rpc.calls, [])
        row = self.driver._store.get('bound-thread', 'offline')
        self.assertIsNone(row, 'N1C_TRANSIENT_PRESUBMIT_WROTE_TERMINAL_RECEIPT')




class RecoveryRPC(RPC):
    def __init__(self, cwd, mode='ready'):
        super().__init__(cwd)
        self.mode = mode
        self.transient_remaining = 1

    def request(self, method, params, timeout=0):
        if method in ('thread/read', 'thread/resume'):
            self.calls.append((method, params))
            old = params['threadId'] == 'bound-thread'
            if old and self.mode == 'missing' and method == 'thread/resume':
                raise JsonRpcError(method, {'code': -32600, 'message': 'no rollout found for thread id bound-thread'})
            if old and self.mode == 'auth':
                raise JsonRpcError(method, {'code': 401, 'message': '401 Unauthorized synthetic'})
            if old and self.mode == 'rpc' and self.transient_remaining:
                self.transient_remaining -= 1
                raise JsonRpcError(method, {'code': -32603, 'message': 'synthetic unknown server error'})
            if old and self.mode == 'resume_rpc' and method == 'thread/resume' and self.transient_remaining:
                self.transient_remaining -= 1
                raise JsonRpcError(method, {'code': -32001, 'message': 'synthetic temporary resume error'})
            if old and self.mode == 'disconnected' and self.transient_remaining:
                self.transient_remaining -= 1
                raise ConnectionError('synthetic disconnected')
            unloaded = old and self.mode in ('missing', 'not_loaded', 'resume_rpc') and method == 'thread/read'
            return {'thread': {'id': params['threadId'],
                'cwd': '/different/synthetic/cwd' if old and self.mode == 'cwd' else self.cwd,
                'status': {'type': 'notLoaded' if unloaded else 'idle'}, 'canAcceptDirectInput': True}}
        if method == 'thread/start':
            self.calls.append((method, params))
            return {'thread': {'id': 'replacement-thread'}}
        return super().request(method, params, timeout)


class RecoveryThreadInheritance(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.store = str(Path(self.tmp.name)/'ledger.db')

    def driver(self, *, required=False, lane=''):
        return CodexProtocolDriver(workdir=self.tmp.name, store_path=self.store,
            bound_thread_id='bound-thread' if required or lane else '',
            require_bound_thread=required, lane_id=lane)

    def pair(self, mode='ready', required=False, lane=''):
        old = self.driver(required=required, lane=lane)
        old._thread_id = old._session_id = 'bound-thread'
        new = self.driver(required=required, lane=lane)
        new.inherit_recovery_thread(old)
        rpc = RecoveryRPC(self.tmp.name, mode)
        new._rpc, new._initialized = rpc, True
        return old, new, rpc

    def test_adapter_recovery_inherits_final_thread_and_never_replays_unknown(self):
        from sessionhelper.config import Config
        old, replacement, rpc = self.pair('not_loaded')
        old._store.upsert('bound-thread', 'uncertain', '', 'hash', 'unknown', 'model_admission_unknown')
        # Erase the direct test setup inheritance: this must come from adapter.
        replacement._thread_id = replacement._session_id = replacement._recovery_thread_id = ''
        old.abort_transport = mock.Mock()
        old.stop = mock.Mock()
        replacement.start = mock.Mock()
        with mock.patch.dict(os.environ, {'SH_DRIVER': 'codex'}):
            adapter = DriverAdapter(Config(session_name='test',key_id='k',secret='synthetic',state_dir=self.tmp.name),
                                    driver=old, driver_factory=lambda: replacement)
        adapter._recover_transport(types.SimpleNamespace(delivery_id='uncertain'))
        self.assertTrue(adapter._transport_recovery.wait(3), 'HRI001_RECOVERY_COMPLETED')
        self.assertIs(adapter.driver, replacement)
        self.assertEqual(replacement._thread_id, 'bound-thread', 'HRI001_INHERIT_FINAL_ROOT')
        self.assertEqual(replacement.deliver('uncertain', 'original').state, 'failed')
        self.assertFalse(rpc.calls, 'HRI003_UNKNOWN_REPLAYED')
        self.assertEqual(replacement.deliver('next', 'new input').state, 'processing')
        methods = [m for m,_ in rpc.calls]
        self.assertNotIn('thread/start', methods, 'HRI001_UNEXPECTED_NEW_THREAD')
        self.assertLess(methods.index('thread/read'), methods.index('thread/resume'))
        self.assertLess(methods.index('thread/resume'), methods.index('turn/start'))
        self.assertEqual(rpc.calls[-1][1]['threadId'], 'bound-thread')
        self.assertEqual([p['clientUserMessageId'] for m,p in rpc.calls if m=='turn/start'], ['next'])

    def test_loaded_root_is_validated_and_continued_without_create(self):
        _, new, rpc = self.pair()
        self.assertEqual(new.deliver('next','new').state, 'processing')
        methods = [m for m,_ in rpc.calls]
        self.assertLess(methods.index('thread/read'), methods.index('turn/start'))
        self.assertNotIn('thread/start', methods)
        self.assertEqual(new._thread_id, 'bound-thread')

    def test_missing_or_wrong_cwd_fails_closed_without_thread_replacement(self):
        for mode in ('missing','cwd'):
            with self.subTest(mode=mode):
                _,new,rpc = self.pair(mode)
                with mock.patch('sessionhelper.codexDriver.PRESUBMIT_VALIDATION_BACKOFF', (0,)):
                    receipt = new.deliver('new-'+mode,'input')
                self.assertEqual(receipt.state,'queued','N1C_INVALID_BINDING_DROPPED_FIFO_HEAD')
                self.assertTrue(new.presubmit_validation_hold('new-'+mode))
                self.assertEqual(new._thread_id,'bound-thread','N1C_MISMATCH_CHANGED_THREAD')
                events=[e for e in new._event_log if e.data.get('change')=='delivery_thread_replaced']
                self.assertEqual(len(events),0,'N1C_MISMATCH_CHANGED_THREAD')
                methods=[m for m,_ in rpc.calls]
                self.assertNotIn('thread/start',methods,'N1C_MISMATCH_CHANGED_THREAD')
                self.assertNotIn('turn/start',methods,'N1C_MISMATCH_SUBMITTED')

    def test_configured_root_and_child_still_refuse_unavailable_thread(self):
        for required,lane in [(True,''),(False,'child-lane')]:
            with self.subTest(required=required,lane=lane):
                _,new,rpc=self.pair('missing',required,lane)
                receipt=new.deliver('rejected-'+str(required),'input')
                self.assertEqual(receipt.state,'queued','N1C_BOUND_OR_CHILD_DROPPED_FIFO_HEAD')
                self.assertTrue(new.presubmit_validation_hold('rejected-'+str(required)))
                self.assertNotIn('thread/start',[m for m,_ in rpc.calls],'HRI002_BOUND_OR_CHILD_OPENED')
                self.assertFalse(any(e.data.get('change')=='delivery_thread_replaced' for e in new._event_log))

    def test_auth_stays_fail_closed_and_temporary_read_errors_retry_same_thread(self):
        for mode in ('auth','rpc','disconnected','resume_rpc'):
            _,new,rpc=self.pair(mode)
            with mock.patch('sessionhelper.codexDriver.PRESUBMIT_VALIDATION_BACKOFF', (0,)):
                receipt = new.deliver('failure-'+mode,'input')
            self.assertEqual(receipt.state,'queued' if mode == 'auth' else 'processing')
            if mode == 'auth':
                self.assertTrue(new.presubmit_validation_hold('failure-'+mode))
            self.assertNotIn('thread/start',[m for m,_ in rpc.calls],'HRI002_UNKNOWN_ERROR_ROTATED')
            self.assertEqual([p.get('threadId') for m,p in rpc.calls if m in ('thread/read','thread/resume')],
                             ['bound-thread'] * sum(1 for m,_ in rpc.calls if m in ('thread/read','thread/resume')),
                             'N1C_RETRY_CHANGED_THREAD')

    def test_transient_validation_wait_is_durable_fifo_and_submits_once_after_recovery(self):
        _,new,rpc=self.pair('ready')
        path=str(Path(self.tmp.name)/'inbox.sqlite')
        inbox=DeliveryInbox(path,session_name='fixture',capacity=5)
        first='dw-dlv_'+'a'*32;second='dw-dlv_'+'b'*32
        for did,body in ((first,'original'),(second,'later')):
            inbox.accept(did,{'from':'source','body':body,'meta':{
                'delivery_id':did,'delivery_ack_protocol':2,'processing_mode':'driver','ack_generation':1}})
        self.assertEqual(inbox.claim_next()[0],first)
        self.assertTrue(inbox.begin_dispatch(first))
        rpc.mode='ready'
        original_request=rpc.request
        retry_entered=threading.Event();release_retry=threading.Event();failed_once={'value':False}
        def transient_read(method,params,timeout=0):
            if method=='thread/read' and not failed_once['value']:
                failed_once['value']=True
                rpc.calls.append((method,params))
                raise TimeoutError('synthetic pre-submit timeout')
            if method=='thread/read':
                retry_entered.set()
                self.assertTrue(release_retry.wait(3),'N1C_RETRY_TEST_RELEASE_TIMEOUT')
            return original_request(method,params,timeout)
        rpc.request=transient_read
        new._busy_admission.set_callbacks(
            on_waiting=lambda did: inbox.mark_busy_waiting(did),
            before_submit=lambda did: inbox.mark_submission_may_have_started(did))
        result=[]
        with mock.patch('sessionhelper.codexDriver.PRESUBMIT_VALIDATION_BACKOFF',(0,)):
            worker=threading.Thread(target=lambda:result.append(new.deliver(first,'original')))
            worker.start()
            self.assertTrue(retry_entered.wait(2),'N1C_RETRY_NOT_ENTERED')
            row=inbox.lifecycle(first)
            self.assertIsNotNone(row['busy_wait_started_at'],'N1C_WAIT_MARKER_NOT_DURABLE')
            self.assertIsNone(row['submission_may_have_started_at'],'N1C_WAIT_CROSSED_SUBMISSION_BOUNDARY')
            self.assertEqual(inbox.lifecycle(second)['state'],'received','N1C_FIFO_LOST_LATER_ITEM')
            self.assertNotIn('turn/start',[m for m,_ in rpc.calls],'N1C_TURN_STARTED_BEFORE_VALIDATION')
            release_retry.set()
            worker.join(2)
        self.assertFalse(worker.is_alive(),'N1C_RETRY_DID_NOT_FINISH')
        self.assertEqual(result[0].state,'processing')
        self.assertIsNotNone(inbox.lifecycle(first)['submission_may_have_started_at'])
        self.assertEqual([m for m,_ in rpc.calls].count('turn/start'),1,'N1C_TURN_STARTED_MORE_THAN_ONCE')
        self.assertNotIn('thread/start',[m for m,_ in rpc.calls],'N1C_RETRY_CHANGED_THREAD')

    def test_mismatched_recovery_binding_never_rotates_to_new_thread(self):
        _,new,rpc=self.pair('cwd')
        with mock.patch('sessionhelper.codexDriver.PRESUBMIT_VALIDATION_BACKOFF', (0,)):
            self.assertEqual(new.deliver('mismatch','input').state,'queued')
            calls_before=list(rpc.calls)
            self.assertEqual(new.deliver('mismatch','input').state,'queued')
            self.assertEqual(rpc.calls,calls_before,'N1C_OPERATOR_HOLD_RETRIED_WITHOUT_RESTART')
            self.assertEqual(new.deliver('later','input').state,'queued')
        self.assertEqual(new._thread_id,'bound-thread','N1C_MISMATCH_CHANGED_THREAD')
        self.assertNotIn('thread/start',[m for m,_ in rpc.calls],'N1C_MISMATCH_CHANGED_THREAD')
        self.assertNotIn('turn/start',[m for m,_ in rpc.calls],'N1C_MISMATCH_SUBMITTED')

    def test_lane_identity_mismatch_is_rejected(self):
        old=self.driver(lane='child'); new=self.driver()
        with self.assertRaises(ValueError):new.inherit_recovery_thread(old)


class LiveInitializeRetry(unittest.TestCase):
    def test_healthy_transport_after_initialize_timeout_still_initializes(self):
        with tempfile.TemporaryDirectory() as tmp:
            class RPC:
                def __init__(self):
                    self.calls = []

                def healthy(self):
                    return True

                def stop(self):
                    pass

                def notify(self, *args):
                    pass

                def request(self, method, params, timeout=0):
                    self.calls.append(method)
                    if self.calls.count('initialize') == 1:
                        raise TimeoutError('synthetic first initialize timeout')
                    return {}

            rpc = RPC()
            driver = CodexProtocolDriver(
                workdir=tmp,
                store_path=str(Path(tmp) / 'driver.db'),
                bound_thread_id='bound-thread',
                require_bound_thread=True,
            )

            def start_locked():
                driver._rpc = rpc
                driver._initialize()

            driver._start_locked = start_locked
            try:
                with self.assertRaises(TimeoutError):
                    driver.start()
                self.assertFalse(driver.health())
                driver.start()
                self.assertTrue(driver.health(), 'N1C_LIVE_TRANSPORT_SKIPPED_INITIALIZE_RETRY')
                self.assertEqual(rpc.calls, ['initialize', 'initialize'])
                driver.start()
                self.assertEqual(rpc.calls, ['initialize', 'initialize'])
            finally:
                driver.stop()



class FirstDeliveryInitializeWait(unittest.TestCase):
    def test_real_delivery_worker_persists_initialize_wait_then_submits_once(self):
        class RPC:
            def __init__(self, workdir, error):
                self.workdir = workdir
                self.errors = [error]
                self.calls = []

            def healthy(self):
                return True

            def stop(self):
                pass

            def notify(self, *args):
                pass

            def request(self, method, params, timeout=0):
                self.calls.append((method, params))
                if method == 'initialize' and self.errors:
                    raise self.errors.pop(0)
                if method in ('thread/read', 'thread/resume'):
                    return {'thread': {'id': 'bound-thread', 'cwd': self.workdir,
                                       'status': {'type': 'idle'}, 'canAcceptDirectInput': True}}
                if method == 'thread/turns/list':
                    return {'data': []}
                if method == 'turn/start':
                    return {'turn': {'id': 'turn-' + params['clientUserMessageId'], 'status': 'inProgress'}}
                if method == 'thread/start':
                    return {'thread': {'id': 'bound-thread'}}
                return {}

        class WS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        errors = (
            TimeoutError('synthetic initialize timeout'),
            JsonRpcError('initialize', {'code': -32603, 'message': 'synthetic temporary unavailable'}),
        )
        for index, error in enumerate(errors):
            with self.subTest(error=type(error).__name__), tempfile.TemporaryDirectory() as tmp:
                rpc = RPC(tmp, error)

                def make_driver():
                    driver = CodexProtocolDriver(
                        workdir=tmp, store_path=str(Path(tmp) / 'driver.db'),
                        bound_thread_id='bound-thread', require_bound_thread=True)

                    def start_locked():
                        driver._rpc = rpc
                        driver._initialize()

                    driver._start_locked = start_locked
                    return driver

                first = make_driver()
                replacement = make_driver()
                cfg = load_config({
                    'SH_SESSION_NAME': 'seat', 'SH_OWNER': 'review', 'SH_KEY_ID': 'review-0001',
                    'SH_SECRET': 'synthetic-local-only', 'SH_DRIVER': 'codex', 'SH_MODE': 'driver',
                    'SH_INBOX_DB': str(Path(tmp) / 'inbox.db'), 'SH_STATE_DIR': str(Path(tmp) / 'state'),
                    'SH_CLI_CWD': tmp, 'SH_CODEX_HOME': str(Path(tmp) / 'codex'),
                    'SH_PROVISION_STATE_FILE': str(Path(tmp) / 'update.json'),
                    'SH_PROVISION_VERSIONS_FILE': str(Path(tmp) / 'versions.json'),
                    'SH_COLLECT': '0', 'SH_PROXY': 'off',
                })
                with mock.patch.dict(os.environ, {'SH_DRIVER': 'codex'}):
                    with mock.patch.object(DriverAdapter, '_build_driver', return_value=first):
                        helper = SessionHelper(cfg)
                helper.upload_addendum_due = lambda: ''
                helper.adapter._build_driver = lambda _which: replacement
                delivery_id = 'dw-dlv_' + str(index + 1) * 32
                env = {'id': 'source', 'from': 'peer-review', 'to': 'seat-review', 'body': 'first',
                       'meta': {'delivery_id': delivery_id, 'delivery_ack_protocol': 2,
                                'processing_mode': 'driver', 'ack_generation': 1}}
                helper.delivery_inbox.accept(delivery_id, env)
                for ack in helper.delivery_inbox.pending_delivery_acks():
                    helper.delivery_inbox.confirm_delivery_ack(ack['payload'] | {'type': 'delivery_ack_confirm'})

                async def exercise():
                    ws = WS()
                    worker = asyncio.create_task(helper.delivery_worker_loop(ws))
                    try:
                        for _ in range(500):
                            row = await asyncio.to_thread(helper.delivery_inbox.lifecycle, delivery_id)
                            if row and row.get('admitted_at') is not None:
                                break
                            await asyncio.sleep(.02)
                        self.assertIsNotNone(row.get('busy_wait_started_at'),
                                             'N1C_STARTUP_TRANSIENT_MISSING_DURABLE_WAIT')
                        self.assertIsNotNone(row.get('submission_may_have_started_at'),
                                             'N1C_STARTUP_RECOVERY_NEVER_REACHED_SUBMISSION')
                        self.assertIsNotNone(row.get('admitted_at'),
                                             'N1C_STARTUP_RECOVERY_DROPPED_OR_STALLED_DELIVERY')
                        self.assertEqual([method for method, _ in rpc.calls].count('turn/start'), 1,
                                         'N1C_STARTUP_RECOVERY_SUBMITTED_MORE_THAN_ONCE')
                        self.assertNotIn('thread/start', [method for method, _ in rpc.calls],
                                         'N1C_STARTUP_RECOVERY_ROTATED_BOUND_THREAD')
                        acks = [item['payload'] for item in helper.delivery_inbox.pending_delivery_acks()]
                        self.assertFalse(any(ack.get('state') == 'failed' for ack in acks),
                                         'N1C_STARTUP_TRANSIENT_EMITTED_TERMINAL_FAILED_ACK')
                    finally:
                        worker.cancel()
                        with contextlib.suppress(asyncio.CancelledError):
                            await worker
                        helper.adapter._lane_stop.set()
                        helper.adapter._lane_closing.set()
                        helper.adapter._work_available.set()
                        helper.adapter._bridge_wake.set()
                        if getattr(helper.adapter, '_worker_thread', None):
                            helper.adapter._worker_thread.join(2)
                        if getattr(helper.adapter, '_bridge_thread', None):
                            helper.adapter._bridge_thread.join(2)
                        current = helper.adapter.driver
                        current.stop()
                        if current is not first:
                            first.stop()

                asyncio.run(exercise())


if __name__ == '__main__':
    unittest.main()
