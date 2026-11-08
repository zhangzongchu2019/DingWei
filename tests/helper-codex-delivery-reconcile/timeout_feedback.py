"""长回合等待窗口的确定性协议夹具；压缩超时，不启动helper或联网。"""
import json
import os
from pathlib import Path
import sys
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'tools/sessionhelper'))
from sessionhelper.codexDriver import CodexProtocolDriver
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.inbox import DeliveryInbox


class BusyProtocol:
    """形状对齐原生thread/read及thread/turns/list；读忙回合后等待测试屏障。"""
    def __init__(self, cwd, entered, release):
        self.cwd, self.entered, self.release = cwd, entered, release
        self.calls = []
    def healthy(self):
        return True
    def request(self, method, params, timeout=0):
        self.calls.append(method)
        if method == 'thread/read':
            return {'thread': {'id': 'bound', 'cwd': self.cwd,
                    'status': {'type': 'active'}, 'canAcceptDirectInput': True}}
        if method == 'thread/turns/list':
            if params['limit'] == 5:
                return {'data': [{'id': 'long-existing', 'status': 'inProgress'}]}
            self.entered.set()
            if not self.release.wait(3):
                raise AssertionError('FIXTURE_BARRIER_NOT_RELEASED')
            raise ConnectionError('仅结束本测试的等候工作线程，不关闭在线传输')
        raise AssertionError('UNEXPECTED_MUTATING_RPC ' + method)


class TimeoutFeedback(unittest.TestCase):
    def test_busy_wait_without_turn_start_must_not_replace_healthy_transport(self):
        with tempfile.TemporaryDirectory(prefix='timeout-feedback-', dir='/tmp') as tmp:
            clean = {'HOME': tmp, 'CODEX_HOME': tmp + '/codex', 'PATH': '/usr/bin:/bin'}
            with patch.dict(os.environ, clean, clear=True):
                entered, release = threading.Event(), threading.Event()
                rpc = BusyProtocol(tmp, entered, release)
                d = CodexProtocolDriver(workdir=tmp, store_path=tmp + '/driver.db')
                d._rpc, d._initialized = rpc, True
                d._thread_id = d._session_id = 'bound'
                inbox = DeliveryInbox(tmp + '/inbox.db', session_name='fixture')
                did = 'dw-dlv_' + 'b' * 32
                env = {'id': 'fixture', 'body': 'synthetic', 'meta': {'delivery_id': did,
                    'delivery_ack_protocol': 2, 'processing_mode': 'driver', 'ack_generation': 1}}
                inbox.accept(did, env); inbox.claim_next(); inbox.begin_dispatch(did)
                with patch.object(DriverAdapter, '_build_driver', return_value=d):
                    a = DriverAdapter(SimpleNamespace(busy_buffer_max=5, turn_terminal_timeout_seconds=1))
                a._which = 'codex'; a._started = True; a.delivery_lifecycle = inbox
                a._admission_timeout = .06
                a.handle_delivery(env); cmd = a._commands.start_next()
                # 不做真实替换：观察生产代码是否请求替换；finally释放并join自己的worker。
                with patch.object(a, '_recover_transport') as recovery:
                    try:
                        result = a._bounded_deliver(cmd)
                        assert entered.is_set(), 'FIXTURE_DID_NOT_REACH_BUSY_PROTOCOL_READ'
                        assert result is None, 'FIXTURE_DID_NOT_EXPIRE_ADMISSION'
                        assert 'turn/start' not in rpc.calls, 'FIXTURE_UNEXPECTED_MODEL_SUBMISSION'
                        assert inbox.lifecycle(did)['state'] == 'admission_unknown'
                        print(json.dumps({'protocol_methods': rpc.calls, 'turn_start_count': 0,
                            'replacement_requests': recovery.call_count, 'new_delivery_state': inbox.lifecycle(did)['state'],
                            'active_turn': 'long-existing', 'transport_healthy': rpc.healthy()}), flush=True)
                        self.assertEqual(recovery.call_count, 0,
                            'BUSY_WAIT_TIMEOUT_REQUESTS_TRANSPORT_REPLACEMENT_BEFORE_ANY_TURN_START')
                    finally:
                        release.set()
                        worker = getattr(cmd, '_delivery_worker', None)
                        if worker is not None:
                            assert worker.wait(3), 'FIXTURE_WORKER_NOT_JOINED'


if __name__ == '__main__':
    unittest.main(verbosity=2)
