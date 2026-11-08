"""只读恢复的证据边界；全部使用私有账本与原生形状协议夹具。"""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from sessionhelper.codexDriver import CodexProtocolDriver, NATIVE_TIME_TOLERANCE_SECONDS
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.inbox import DeliveryInbox


class Protocol:
    def __init__(self, pages):
        self.pages = pages
        self.calls = []

    def healthy(self):
        return True

    def request(self, method, params, timeout=30):
        self.calls.append((method, copy.deepcopy(params)))
        if method != 'thread/turns/list':
            raise AssertionError('READ_ONLY_RECONCILE_MUTATED_NATIVE')
        return copy.deepcopy(self.pages.get(params.get('cursor'), {'data': []}))


class ReconcileBoundaries(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='reconcile-boundary-', dir='/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name)
        self.clean = {'HOME': str(self.path), 'CODEX_HOME': str(self.path / 'codex'), 'PATH': '/usr/bin:/bin'}
        env = patch.dict(os.environ, self.clean, clear=True)
        env.start(); self.addCleanup(env.stop)
        self.did = 'dw-dlv_' + 'e' * 32
        self.inbox = DeliveryInbox(str(self.path / 'inbox.db'), session_name='fixture')
        envelope = {'body': 'synthetic', 'meta': {'delivery_id': self.did,
            'delivery_ack_protocol': 2, 'processing_mode': 'driver', 'ack_generation': 1}}
        self.inbox.accept(self.did, envelope); self.inbox.claim_next(); self.inbox.begin_dispatch(self.did)
        self.inbox.record_admission_unknown(self.did)
        self.driver = self.new_driver()
        self.driver._store.upsert('bound', self.did, '', 'hash', 'unknown', 'model_admission_unknown')
        self.turn = {'id': 'native-turn', 'status': 'interrupted',
            'startedAt': int(time.time()), 'completedAt': int(time.time()),
            'items': [{'type': 'userMessage', 'clientId': self.did}]}
        self.rpc = Protocol({None: {'data': [self.turn]}})
        self.driver._rpc = self.rpc
        self.adapter = self.new_adapter(self.driver)

    def new_driver(self):
        d = CodexProtocolDriver(workdir=str(self.path), store_path=str(self.path / 'driver.db'))
        d._initialized = True
        d._thread_id = d._session_id = 'bound'
        return d

    def new_adapter(self, driver):
        with patch.object(DriverAdapter, '_build_driver', return_value=driver):
            adapter = DriverAdapter(SimpleNamespace(busy_buffer_max=5, turn_terminal_timeout_seconds=1))
        adapter._which = 'codex'
        adapter.delivery_lifecycle = self.inbox
        return adapter

    def events(self, driver=None):
        driver = driver or self.driver
        with driver._event_log_lock:
            return list(driver._event_log)

    def scan(self):
        result = self.driver.reconcile_pending(self.did)
        for event in self.events():
            self.adapter._record_event_lifecycle(event)
        return result

    def assert_pending(self, marker):
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'admission_unknown', marker)
        self.assertFalse(self.events(), marker)

    def test_unique_complete_evidence_recovers_and_is_idempotent(self):
        self.assertEqual(self.scan(), 'complete')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed', 'UNIQUE_COMPLETE_NOT_RECOVERED')
        before = len(self.events())
        self.scan()
        self.assertEqual(len(self.events()), before, 'TERMINAL_DUPLICATED')
        self.assertEqual(len(self.rpc.calls), 1, 'TERMINAL_REQUERIED')
        self.assertTrue(all(method == 'thread/turns/list' for method, _ in self.rpc.calls))

    def test_in_progress_native_admission_then_terminal(self):
        self.turn['status'] = 'inProgress'
        self.turn['completedAt'] = None
        self.scan()
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'admitted', 'IN_PROGRESS_NOT_ADMITTED')
        self.turn['status'] = 'interrupted'
        self.turn['completedAt'] = int(time.time())
        with patch('sessionhelper.codexDriver.time.time', return_value=time.time() + 15.01):
            self.scan()
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed', 'PENDING_TERMINAL_NOT_RECOVERED')

    def test_healthy_busy_wait_has_no_stall_deadline(self):
        self.driver._active_turn_id = 'long-existing'
        self.driver._stall_timeout = .001
        with patch.object(self.driver, '_read_turn_state', side_effect=[('processing', 'busy'), ('processing', 'busy'), ('done', 'complete')]), patch('sessionhelper.codexDriver.time.sleep'), patch('sessionhelper.codexDriver.time.monotonic', side_effect=range(0, 10000, 100)):
            self.assertEqual(self.driver._wait_until_idle_for_delivery(), '', 'HEALTHY_BUSY_TURN_EXPIRED_LOCAL_QUEUE')

    def test_local_queue_does_not_use_dispatch_intent_clock(self):
        command = SimpleNamespace(delivery_id=self.did, _dispatch_started=time.monotonic() - 120)
        self.assertEqual(self.adapter._admission_elapsed(command), 0, 'LOCAL_QUEUE_USED_DISPATCH_CLOCK')
        self.driver._submission_started[self.did] = time.monotonic() - .05
        self.assertGreater(self.adapter._admission_elapsed(command), .04, 'ACTUAL_SUBMISSION_CLOCK_NOT_USED')

    def test_turn_start_clock_begins_after_transport_write_lock(self):
        from sessionhelper.codexDriver import StdioJsonRpc
        # 真实request写锁路径，只有stdin是无副作用内存夹具。
        rpc = StdioJsonRpc(['unused'], cwd=str(self.path))
        rpc._proc = SimpleNamespace(stdin=SimpleNamespace(write=lambda _: None, flush=lambda: None), poll=lambda: None)
        rpc.on_turn_start = self.driver._turn_start_writing
        errors = []
        def request():
            try:
                rpc.request('turn/start', {'clientUserMessageId': self.did}, timeout=.02)
            except TimeoutError as exc:
                errors.append(exc)
        rpc._write_lock.acquire()
        worker = threading.Thread(target=request)
        worker.start()
        try:
            time.sleep(.03)
            self.assertIsNone(self.driver.submission_started(self.did), 'WRITE_LOCK_WAIT_STARTED_ADMISSION_CLOCK')
            released = time.monotonic()
        finally:
            rpc._write_lock.release()
            worker.join(2)
        self.assertFalse(worker.is_alive())
        self.assertTrue(errors)
        started = self.driver.submission_started(self.did)
        self.assertIsNotNone(started, 'TURN_START_CLOCK_NOT_RECORDED')
        self.assertGreaterEqual(started, released, 'WRITE_LOCK_WAIT_STARTED_ADMISSION_CLOCK')

    def test_unknown_without_driver_row_never_automatically_resubmits(self):
        # 丢失/历史空账本不是重投许可；仅当前processing本地排队可继续。
        with self.driver._store._connect() as db:
            db.execute('DELETE FROM deliveries')
        with patch.object(self.driver, 'deliver', side_effect=AssertionError('UNKNOWN_AUTOMATICALLY_RESUBMITTED')) as deliver:
            self.adapter._restore_delivery_commands()
            command = self.adapter._commands.start_next()
            self.adapter._deliver_regular_command(command)
        self.assertFalse(deliver.called, 'UNKNOWN_AUTOMATICALLY_RESUBMITTED')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'admission_unknown')

    def test_healthy_response_wait_does_not_replace_transport(self):
        command = SimpleNamespace(delivery_id=self.did)
        # 自己的driver/RPC都是夹具；即使变异撤保护也不操作在线进程。
        with patch.object(self.driver, 'abort_transport') as abort, patch.object(self.driver, 'stop'), patch.object(self.driver, 'start'), patch.object(self.adapter, '_build_driver', return_value=self.driver):
            self.adapter._recover_transport(command)
            recovery = self.adapter._transport_recovery
            if recovery is not None:
                recovery.wait(1)
            self.assertFalse(abort.called, 'HEALTHY_TRANSPORT_REPLACED_FOR_RESPONSE_WAIT')
        self.rpc._write_started = time.monotonic() - 61
        self.assertTrue(self.driver.transport_recovery_required(60), 'STUCK_WRITER_NOT_DETECTED')
        self.rpc._write_started = None
        self.assertFalse(self.driver.transport_recovery_required(60), 'HEALTHY_WRITER_MISCLASSIFIED')

    def test_native_seconds_replace_fractional_receipt_observation(self):
        # 通知/响应先记录浮点观察；同秒原生终态的结束时刻整数可比它小。
        observed = time.time()
        self.inbox.record_admitted(self.did, 'native-turn', observed)
        self.scan()
        row = self.inbox.lifecycle(self.did)
        self.assertEqual(row['state'], 'processed', 'NATIVE_SECONDS_REJECTED_AFTER_LIVE_RECEIPT')
        self.assertEqual(row['admitted_at'], self.turn['startedAt'])
        self.assertEqual(row['processed_at'], self.turn['completedAt'])

    def test_duplicate_client_id_is_ambiguous(self):
        other = copy.deepcopy(self.turn); other['id'] = 'duplicate'
        self.rpc.pages[None]['data'].append(other)
        self.scan()
        self.assert_pending('DUPLICATE_CLIENT_ID_ADVANCED_ACK')

    def test_duplicate_client_id_on_historical_binding_is_ambiguous(self):
        self.driver._store.upsert('historical', self.did, '', 'hash', 'unknown')
        self.scan()  # 相同原生clientId在两个可信thread各匹配一次。
        self.assert_pending('HISTORICAL_DUPLICATE_ADVANCED_ACK')
        self.assertEqual({p['threadId'] for _, p in self.rpc.calls}, {'bound', 'historical'})

    def test_page_limit_200_is_incomplete_even_if_match_seen(self):
        fillers = [{'id': 'other-' + str(i), 'items': []} for i in range(199)]
        self.rpc.pages[None] = {'data': [self.turn] + fillers, 'nextCursor': 'opaque-after-200'}
        self.assertEqual(self.scan(), 'incomplete', 'TURN_BUDGET_NOT_INCOMPLETE')
        self.assert_pending('TURN_BUDGET_ADVANCED_ACK')
        self.assertEqual(len(self.rpc.calls), 1)

    def test_opaque_cursor_and_whole_scan_uniqueness(self):
        self.rpc.pages[None] = {'data': [self.turn], 'nextCursor': 'opaque/second'}
        other = copy.deepcopy(self.turn); other['id'] = 'second-page-duplicate'
        self.rpc.pages['opaque/second'] = {'data': [other]}
        self.scan()
        self.assert_pending('PAGINATION_EARLY_MATCH_ADVANCED_ACK')
        self.assertEqual(self.rpc.calls[1][1]['cursor'], 'opaque/second')

    def test_nullable_and_nonfinite_times_keep_pending(self):
        for value in (None, float('nan'), float('inf'), True, -1):
            with self.subTest(value=value):
                # 每格独立对象/账本，不能让15s限流掩盖时间验证。
                with patch('sessionhelper.codexDriver.RECONCILE_THREAD_INTERVAL_SECONDS', 0):
                    self.turn['completedAt'] = value
                    self.scan()
                    self.assert_pending('INVALID_TIME_ADVANCED_ACK')
        self.assertEqual(len(self.rpc.calls), 5)

    def test_wrong_thread_and_wrong_turn_keep_pending(self):
        self.turn['threadId'] = 'foreign'
        self.scan()
        self.assert_pending('WRONG_THREAD_ADVANCED_ACK')
        with patch('sessionhelper.codexDriver.RECONCILE_THREAD_INTERVAL_SECONDS', 0):
            self.turn.pop('threadId')
            self.driver._store.upsert('bound', self.did, 'different-turn', 'hash', 'processing')
            self.scan()
            self.assert_pending('WRONG_TURN_ADVANCED_ACK')

    def test_old_generation_snapshot_cannot_advance_ack(self):
        context = self.adapter._reconcile_context(self.did)
        self.driver.reconcile_context = lambda _: {**context, 'ack_generation': 99}
        self.scan()
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'admission_unknown', 'OLD_GENERATION_ADVANCED_ACK')

    def test_native_second_precision_one_second_tolerance(self):
        intent = self.inbox.lifecycle(self.did)['dispatch_intent_at']
        try:
            self.inbox.record_native_terminal(self.did, 'native-turn', intent - .999, intent, 'interrupted')
        except ValueError:
            self.fail('SECOND_PRECISION_REJECTED')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed', 'SECOND_PRECISION_REJECTED')

    def test_before_intent_more_than_one_second_rejected(self):
        intent = self.inbox.lifecycle(self.did)['dispatch_intent_at']
        with self.assertRaises(ValueError, msg='OLD_TIME_ADVANCED_ACK'):
            self.inbox.record_native_terminal(self.did, 'native-turn', intent - 1.001, intent, 'completed')

    def test_conflicting_terminal_never_replaces_first_outcome(self):
        self.scan()
        event = copy.deepcopy(self.events()[0]); event.data['status'] = 'completed'
        self.adapter._record_event_lifecycle(event)
        self.assertEqual(self.inbox.lifecycle(self.did)['turn_outcome'], 'interrupted', 'CONFLICTING_OUTCOME_REPLACED')

    def test_restart_process_replays_persisted_proof_without_rpc(self):
        self.driver.reconcile_pending(self.did)
        # 原进程在证据落driver账本后、inbox事务前退出。新解释器不继承对象、游标或通知。
        code = '''import json,sys
from sessionhelper.codexDriver import CodexProtocolDriver
d=CodexProtocolDriver(workdir=sys.argv[1],store_path=sys.argv[1]+'/driver.db')
d.reconcile_pending()
print(json.dumps([{'session_id':e.session_id,'delivery_id':e.delivery_id,'turn_id':e.turn_id,'kind':e.kind,'data':e.data} for e in d._event_log]))
'''
        environment = {**self.clean, 'PYTHONPATH': os.pathsep.join(sys.path)}
        run = subprocess.run([sys.executable, '-c', code, str(self.path)], env=environment,
                             text=True, capture_output=True, timeout=10)
        self.assertEqual(run.returncode, 0, run.stderr)
        from sessionhelper.contract import SessionEvent
        for value in json.loads(run.stdout):
            self.adapter._record_event_lifecycle(SessionEvent(**value))
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed', 'PROCESS_RESTART_LOST_PROOF')

    def test_notification_before_response_and_duplicate_merge(self):
        # 终态通知在turn/start响应映射建立之前，delivery_id为空；不应凭空归属。
        event = self.driver._map_notification({'method': 'turn/completed', 'params': {
            'threadId': 'bound', 'turn': copy.deepcopy(self.turn)}})
        self.adapter._record_event_lifecycle(event)
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'admission_unknown')
        self.scan()  # 精确clientId原生只读对账补关联。
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed', 'REORDERED_NOTIFICATION_LOST_TERMINAL')
        self.adapter._record_event_lifecycle(self.events()[0])
        self.assertEqual(self.inbox.lifecycle(self.did)['turn_outcome'], 'interrupted')

    def test_thread_rate_limit_survives_driver_replacement(self):
        self.rpc.pages[None] = {'data': []}
        self.scan()
        replacement = self.new_driver(); replacement._rpc = self.rpc
        replacement.reconcile_pending(self.did)
        self.assertEqual(len(self.rpc.calls), 1, 'REPLACEMENT_RESET_THREAD_RATE_LIMIT')
        with patch('sessionhelper.codexDriver.time.time', return_value=time.time() + 15.01):
            replacement.reconcile_pending(self.did)
        self.assertEqual(len(self.rpc.calls), 2, 'THREAD_RATE_LIMIT_NEVER_RELEASED')

    def test_one_reconcile_request_per_seat_across_processes(self):
        code = "import fcntl,sys; f=open(sys.argv[1],'a'); fcntl.flock(f,fcntl.LOCK_EX); print('locked',flush=True); sys.stdin.read(1)"
        process = subprocess.Popen([sys.executable, '-c', code, str(self.path / 'driver.db.reconcile.lock')],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=self.clean)
        try:
            self.assertEqual(process.stdout.readline().strip(), 'locked')
            self.assertEqual(self.driver.reconcile_pending(self.did), 'pending', 'SEAT_RECONCILE_OVERLAPPED')
            self.assertEqual(self.rpc.calls, [], 'SEAT_RECONCILE_OVERLAPPED')
        finally:
            process.communicate('x', timeout=5)

    def test_one_reconcile_request_per_seat_across_objects(self):
        entered, release = threading.Event(), threading.Event()
        original = self.rpc.request
        def blocking(*args, **kwargs):
            entered.set()
            self.assertTrue(release.wait(3))
            return original(*args, **kwargs)
        self.rpc.request = blocking
        worker = threading.Thread(target=self.driver.reconcile_pending, args=(self.did,))
        worker.start()
        try:
            self.assertTrue(entered.wait(1))
            replacement = self.new_driver(); replacement._rpc = self.rpc
            self.assertEqual(replacement.reconcile_pending(self.did), 'pending', 'SEAT_RECONCILE_OVERLAPPED')
        finally:
            release.set(); worker.join(3)
        self.assertFalse(worker.is_alive())
        self.assertEqual(len(self.rpc.calls), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
