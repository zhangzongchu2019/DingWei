"""锁定版原生协议复现；私有 stdio 子进程，不连接现网或真实推理服务。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import sys
import tempfile
import threading
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools/sessionhelper'))
from sessionhelper.codexDriver import CodexProtocolDriver, StdioJsonRpc, JsonRpcError
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.inbox import DeliveryInbox

BINARY_SHA = '1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e'
BINARY = None
OUTPUT = None


class NativeReconcile(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='reconcile-', dir='/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name)
        self.ch = self.home / 'codex'
        self.ch.mkdir()
        (self.ch / 'config.toml').write_text('''model_provider="local_probe"
model="probe-no-inference"
[model_providers.local_probe]
name="local probe"
base_url="http://127.0.0.1:9/v1"
wire_api="responses"
requires_openai_auth=false
supports_websockets=false
request_max_retries=0
stream_max_retries=0
''')
        self.clean = {'HOME': str(self.home), 'CODEX_HOME': str(self.ch),
                      'PATH': '/usr/bin:/bin', 'RUST_LOG': 'error'}
        self.envpatch = patch.dict(os.environ, self.clean, clear=True)
        self.envpatch.start()
        self.addCleanup(self.envpatch.stop)
        self.rpc = StdioJsonRpc([BINARY, 'app-server', '--stdio'], cwd=str(self.home), env=self.clean)
        self.rpc.start()
        self.addCleanup(self.rpc.stop)
        self.calls = []
        original = self.rpc.request
        def request(method, params, timeout=30):
            self.calls.append({'method': method, 'params': params})
            try:
                result = original(method, params, timeout=timeout)
            except JsonRpcError as exc:
                self.responses.append({'method': method, 'error_code': exc.code, 'error_message': exc.message})
                raise
            self.responses.append({'method': method, 'result': result})
            return result
        self.responses = []
        self.rpc.request = request
        self.driver = self.new_driver()
        self.driver._initialize()
        self.tid = self.rpc.request('thread/start', {'cwd': str(self.home), 'ephemeral': False,
                    'sandbox': 'read-only', 'approvalPolicy': 'never'})['thread']['id']
        self.bind(self.driver, self.tid)
        self.inbox = DeliveryInbox(str(self.home / 'inbox.db'), session_name='fixture')
        self.did = 'dw-dlv_' + 'a' * 32
        env = {'id': 'fixture', 'body': 'LOCAL_PROTOCOL_TEST_ONLY', 'meta': {
            'delivery_id': self.did, 'delivery_ack_protocol': 2,
            'processing_mode': 'driver', 'ack_generation': 1}}
        self.inbox.accept(self.did, env)
        self.inbox.claim_next()
        self.inbox.begin_dispatch(self.did)
        self.addCleanup(self.save_evidence)

    def save_evidence(self):
        # 仅合成测试输入和本次子进程原生响应，不读取席位凭据/历史。
        (OUTPUT / (self._testMethodName + '.json')).write_text(json.dumps({
            'calls': self.calls, 'responses': self.responses,
            'lifecycle': self.inbox.lifecycle(self.did)}, indent=2) + '\n')

    def new_driver(self):
        d = CodexProtocolDriver(workdir=str(self.home), manage_daemon=False,
            store_path=str(self.home / 'driver.db'))
        d._rpc = self.rpc
        d._initialized = True
        def context(delivery_id):
            row = self.inbox.lifecycle(delivery_id)
            return {k: row[k] for k in ('ack_generation', 'dispatch_intent_at', 'request_id')} if row else None
        d.reconcile_context = context
        return d

    @staticmethod
    def bind(d, tid):
        d._thread_id = d._session_id = tid

    def adapter(self, d=None):
        d = d or self.driver
        with patch.object(DriverAdapter, '_build_driver', return_value=d):
            a = DriverAdapter(SimpleNamespace(busy_buffer_max=10, turn_terminal_timeout_seconds=1))
        a._which = 'codex'
        a._started = True
        a.delivery_lifecycle = self.inbox
        return a

    def history(self, tid=None):
        try:
            return self.rpc.request('thread/turns/list', {'threadId': tid or self.tid,
                    'limit': 100, 'itemsView': 'full', 'sortDirection': 'asc'})['data']
        except JsonRpcError as exc:
            if exc.code not in (-32601, -32600):
                raise
            try:
                return self.rpc.request('thread/read', {'threadId': tid or self.tid,
                    'includeTurns': True})['thread']['turns']
            except JsonRpcError as read_error:
                if read_error.code in (-32600, -32601):
                    return []  # 原生rollout尚未就绪；控制格也只保持unknown
                raise

    def finished(self, turn_id):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            turns = self.history()
            for t in turns:
                if t['id'] == turn_id and t['status'] in ('completed', 'failed', 'interrupted'):
                    self.assertIsInstance(t.get('startedAt'), int, 'NATIVE_FIXTURE_MISSING_STARTED_AT')
                    self.assertIsInstance(t.get('completedAt'), int, 'NATIVE_FIXTURE_MISSING_COMPLETED_AT')
                    if any(i.get('clientId') == self.did for i in t['items']):
                        return t
            time.sleep(.02)
        self.fail('NATIVE_FIXTURE_TERMINAL_TIMEOUT')

    def submitted(self, lost_response=False):
        if lost_response:
            self.driver._store.upsert(self.tid, self.did, '', 'fixture-hash', 'unknown',
                                      'model_admission_unknown: dispatch pending')
            # 原生请求已被接受，故意不把返回帧交给 driver，复现 EOF/超时窗。
            turn = self.rpc.request('turn/start', {'threadId': self.tid,
                'input': [{'type': 'text', 'text': 'LOCAL_PROTOCOL_TEST_ONLY'}],
                'clientUserMessageId': self.did})['turn']
            self.inbox.record_admission_unknown(self.did)
        else:
            receipt = self.driver.deliver(self.did, 'LOCAL_PROTOCOL_TEST_ONLY')
            self.assertIn(receipt.state, ('processing', 'accepted'))
            turn = {'id': self.driver._envelope_to_turn[self.did]}
        # 等原生userMessage落盘后只中断本测试的回合，避免测试自身抢在入场前中断。
        ready_deadline = time.monotonic() + 10
        while not any(i.get('clientId') == self.did for t in self.history() for i in t['items']):
            self.assertLess(time.monotonic(), ready_deadline, 'NATIVE_FIXTURE_USER_ITEM_TIMEOUT')
            time.sleep(.02)
        # 只中断本测试刚建的回合，得到确定的原生 interrupted 终态。
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                msg = self.rpc.notifications.get(timeout=.1)
            except queue.Empty:
                continue
            if msg.get('method') == 'turn/started' and msg.get('params', {}).get('turn', {}).get('id') == turn['id']:
                break
        else:
            self.fail('NATIVE_FIXTURE_START_NOTIFICATION_TIMEOUT')
        try:
            self.rpc.request('turn/interrupt', {'threadId': self.tid, 'turnId': turn['id']})
        except JsonRpcError as exc:
            if exc.code != -32600 or exc.message != 'no active turn to interrupt':
                raise
        return self.finished(turn['id'])

    def test_A_history_replay_preserves_native_times_for_ack(self):
        turn = self.submitted()
        self.inbox.record_admitted(self.did, turn['id'], turn['startedAt'])
        terminal = [e for e in self.driver._events_from_turn_history(self.history())
                    if e.data.get('change') == 'turn_completed']
        self.assertEqual(len(terminal), 1)
        self.adapter()._record_event_lifecycle(terminal[0])
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed',
                         'A_NATIVE_HISTORY_TERMINAL_NOT_COMMITTED')
        self.assertEqual(terminal[0].data.get('admitted_at'), turn['startedAt'])
        self.assertEqual(terminal[0].data.get('processed_at'), turn['completedAt'])

    def test_A_replacement_poll_and_resubscribe_recover_lost_terminal(self):
        turn = self.submitted()
        self.inbox.record_admitted(self.did, turn['id'], turn['startedAt'])
        # 新对象只有相同持久driver账本；旧对象的通知和内存时间不转移。
        replacement = self.new_driver()
        self.bind(replacement, self.tid)
        replacement.poll_receipt(self.did)
        replacement._stop_event.set()  # 有界读取本次无游标订阅的全部已有缓冲
        terminal = [e for e in replacement.events() if e.data.get('change') == 'turn_completed']
        for e in terminal:
            self.adapter(replacement)._record_event_lifecycle(e)
        self.assertTrue(terminal, 'A_REPLACEMENT_HAS_NO_TERMINAL_RECONCILIATION')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed')

    def assert_unknown_recovers(self, historical):
        turn = self.submitted(lost_response=True)
        if historical:
            current = self.rpc.request('thread/start', {'cwd': str(self.home), 'ephemeral': False,
                'sandbox': 'read-only', 'approvalPolicy': 'never'})['thread']['id']
            self.bind(self.driver, current)
        bound_before = self.driver._thread_id
        before_starts = sum(c['method'] == 'turn/start' for c in self.calls)
        receipt = self.driver.poll_receipt(self.did)
        self.assertEqual(self.driver._thread_id, bound_before, 'B_RECONCILIATION_REBOUND_ACTIVE_THREAD')
        self.assertEqual(sum(c['method'] == 'turn/start' for c in self.calls), before_starts,
                         'B_UNKNOWN_AUTOMATICALLY_RESUBMITTED')
        self.assertEqual(self.driver._envelope_to_turn.get(self.did), turn['id'],
                         'B_CLIENT_ID_NATIVE_TURN_NOT_RECONCILED')
        self.assertIn(receipt.state, ('done', 'failed', 'processing'))
        self.driver._stop_event.set()
        adapter = self.adapter()
        for event in self.driver.events():
            adapter._record_event_lifecycle(event)
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed',
                         'B_NATIVE_CORRELATION_DID_NOT_SETTLE_INBOX')
        self.assertEqual(self.inbox.lifecycle(self.did)['turn_outcome'], 'interrupted')

    def test_B_timeout_finds_client_id_on_bound_thread_without_resubmit(self):
        self.assert_unknown_recovers(False)

    def test_B_timeout_finds_client_id_on_historical_thread_without_rebind(self):
        self.assert_unknown_recovers(True)

    def test_native_second_terminal_after_fractional_live_admission(self):
        turn = self.submitted()
        self.inbox.record_admitted(self.did, turn['id'], time.time())
        replacement = self.new_driver()
        self.bind(replacement, self.tid)
        adapter = self.adapter(replacement)
        replacement.poll_receipt(self.did)
        replacement._stop_event.set()
        for event in replacement.events():
            adapter._record_event_lifecycle(event)
        row = self.inbox.lifecycle(self.did)
        self.assertEqual(row['state'], 'processed', 'NATIVE_SECONDS_REJECTED_AFTER_LIVE_RECEIPT')
        self.assertEqual(row['admitted_at'], turn['startedAt'])
        self.assertEqual(row['processed_at'], turn['completedAt'])

    def test_control_no_matching_turn_stays_unknown_without_resubmit(self):
        self.driver._store.upsert(self.tid, self.did, '', 'fixture-hash', 'unknown',
                                  'model_admission_unknown: dispatch pending')
        self.inbox.record_admission_unknown(self.did)
        self.assertEqual(self.history(), [])
        # 基线内部receipt把unknown映射为failed；持久账本仍必须保持unknown。
        self.assertIn(self.driver.poll_receipt(self.did).state, ('unknown', 'failed'))
        self.assertEqual(self.driver._store.get_by_envelope(self.did).state, 'unknown')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'admission_unknown')
        self.assertNotIn('turn/start', [c['method'] for c in self.calls])

    def test_control_output_only_terminal_cannot_advance_ack(self):
        turn = self.submitted()
        self.inbox.record_admitted(self.did, turn['id'], turn['startedAt'])
        from sessionhelper.contract import SessionEvent
        e = SessionEvent(kind='state_change', data={'change': 'turn_completed',
            'status': turn['status']}, delivery_id=self.did, turn_id=turn['id'])
        self.adapter()._record_event_lifecycle(e)
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'admitted')

    def test_control_complete_native_evidence_can_settle_terminal(self):
        turn = self.submitted()
        from sessionhelper.contract import SessionEvent
        e = SessionEvent(kind='state_change', data={'change': 'turn_completed',
            'status': turn['status'], 'admitted_at': turn['startedAt'],
            'processed_at': turn['completedAt']}, delivery_id=self.did, turn_id=turn['id'])
        self.adapter()._record_event_lifecycle(e)
        self.assertEqual(self.inbox.lifecycle(self.did)['state'], 'processed')
        self.assertEqual(self.inbox.lifecycle(self.did)['turn_outcome'], 'interrupted')


def main():
    global BINARY, OUTPUT
    p = argparse.ArgumentParser()
    p.add_argument('--binary', required=True)
    p.add_argument('--output', required=True)
    p.add_argument('tests', nargs='*')
    args = p.parse_args()
    BINARY = str(Path(args.binary).resolve())
    assert hashlib.sha256(Path(BINARY).read_bytes()).hexdigest() == BINARY_SHA, 'wrong pinned binary'
    OUTPUT = Path(args.output).resolve()
    OUTPUT.mkdir(parents=True, exist_ok=False)
    suite = unittest.defaultTestLoader.loadTestsFromNames(args.tests, module=sys.modules[__name__]) if args.tests else unittest.defaultTestLoader.loadTestsFromTestCase(NativeReconcile)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    (OUTPUT / 'RESULT.json').write_text(json.dumps({'tests': result.testsRun,
        'failures': len(result.failures), 'errors': len(result.errors), 'skips': len(result.skipped),
        'binary_sha256': BINARY_SHA, 'baseline': '6e2c8364',
        'product_code_modified': True, 'external_inference': False}, indent=2) + '\n')
    raise SystemExit(not result.wasSuccessful())


if __name__ == '__main__':
    main()
