"""Real stdio processes and Unix sockets: one error, survival, no resend."""
import json
import os
from pathlib import Path
import selectors
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from sessionhelper import mcp_shim

SENTINEL = 'fixture-private-path-token-DO-NOT-RETURN'


def request(request_id, tool='send_message', upload_path=''):
    args = {
        'send_message': {'to': 'fixture#fixture', 'body': 'fixture', 'reply_policy': 'no_reply'},
        'heartbeat_report': {'status': 'active'},
        'project_context': {'scope': 'config'},
        'dw_inbox': {'operation': 'status'},
        'dw_delivery_status': {'delivery_id': 'dw-dlv_'+'a'*32},
        'dw_spec_review_requests': {'request_id':'specreview_fixture'},
        'dw_spec_review_reply': {'request_id':'specreview_fixture','kind':'review','revision':1,'content_sha256':'a'*64,'issues':[]},
        'dw_task_blocker_list': {'task_id':'fixture'},
        'dw_task_blocker_set': {'task_id':'fixture','request_id':'stable','expected_version':1,'kind':'technical','where':'blocked','expected_input':'facts'},
        'dw_task_blocker_input': {'task_id':'fixture','blocker_id':'blk:one','request_id':'stable','expected_version':1,'event_id':'answer','body':'data','evidence_ref':'git:ref'},
        'dw_task_blocker_recheck': {'task_id':'fixture','blocker_id':'blk:one','request_id':'stable','expected_version':1,'operation':'begin_recheck','input_version':1},
        'dw_task_question_list': {},
        'dw_spec_template': {'action':'list'},
        'dw_spec_clarifications': {'action':'list','question_id':'q_fixture'},
        'dw_pm_spec_assist_request': {'request_id':'pmspecassist_fixture'},
        'dw_pm_spec_assist_reply': {'request_id':'pmspecassist_fixture','suggestions':[],'review_questions':[]},
        'dw_task_question_create': {'task_id':'fixture','request_id':'stable','expected_version':1,'kind':'technical','title':'question','observed_fact':'fact','question':'which','expected_answer':'evidence','entry_refs':['A1']},
        'dw_task_question_input': {'question_id':'q_fixture','request_id':'stable','expected_version':1,'event_id':'answer','body':'technical material','evidence_ref':'git:ref'},
        'dw_task_list': {'limit': 10},
        'dw_task_update': {'task_id':'fixture','request_id':'stable','expected_version':1,'operation':'start'},
        'dw_wait_message': {'wait_seconds': 1},
        'dw_reply_message': {'message_id': 'fixture', 'body': 'fixture'},
        'dw_spec_create': {'title': 'fixture', 'content_markdown': 'fixture',
                           'input_anchors': ['fixture'], 'acceptance': ['fixture']},
        'dw_spec_update': {'spec_id': 'fixture', 'title': 'fixture', 'content_markdown': 'fixture',
                           'input_anchors': ['fixture'], 'acceptance': ['fixture'], 'expected_revision': 1},
        'upload_file': {'path': upload_path},
    }[tool]
    return {'jsonrpc': '2.0', 'id': request_id, 'method': 'tools/call',
            'params': {'name': tool, 'arguments': args}}


class Peer:
    def __init__(self, path, mode):
        self.path, self.mode = path, mode
        self.accepted = 0
        self.requests = []
        self.errors = []
        self.stopped = threading.Event()
        self.listener = socket.socket(socket.AF_UNIX)
        self.listener.bind(path)
        self.listener.listen(16)
        self.listener.settimeout(.05)
        self.workers = []
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()

    def run(self):
        while not self.stopped.is_set():
            try:
                conn, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            self.accepted += 1
            mode = self.mode if self.accepted == 1 else 'success'
            worker = threading.Thread(target=self.handle, args=(conn, mode), daemon=True)
            self.workers.append(worker)
            worker.start()

    def handle(self, conn, mode):
        try:
            with conn:
                if mode == 'reset': return
                conn.settimeout(2)
                data = b''
                while b'\n' not in data:
                    chunk = conn.recv(65536)
                    if not chunk: return
                    data += chunk
                self.requests.append(json.loads(data))
                if mode == 'timeout':
                    self.stopped.wait(.5)
                    return
                if mode == 'eof': return
                responses = {
                    'badjson': b'{bad}\n', 'truncated': b'{"ok":true}',
                    'utf8': b'\xff\n', 'array': b'[]\n', 'null': b'null\n',
                    'scalar': b'1\n', 'missing_ok': b'{}\n', 'wrong_ok': b'{"ok":"true"}\n',
                    'bad_outcome': b'{"ok":true,"outcome":[]}\n',
                }
                if mode in responses:
                    conn.sendall(responses[mode])
                elif mode in ('rejected', 'unsafe_rejected', 'vsapi_rejected', 'upload_rejected'):
                    conn.sendall((json.dumps({'ok': False, 'error': SENTINEL,
                                             'reason': SENTINEL}) + '\n').encode())
                else:
                    conn.sendall(b'{"ok":true,"outcome":{"sent":true},"result":{"fixture":true},"state":"timeout"}\n')
        except (OSError, ValueError) as exc:
            self.errors.append(type(exc).__name__)

    def close(self):
        self.stopped.set()
        self.listener.close()
        self.thread.join(2)
        for worker in self.workers: worker.join(2)


class Child:
    def __init__(self, path, tmp):
        # Shorten only the test's socket deadline; execute the real serve loop.
        code = 'from sessionhelper import mcp_shim as m;m._IPC_TIMEOUT=.15;m.serve()'
        env = {'PATH': os.defpath, 'PYTHONPATH': str(ROOT), 'HOME': tmp,
               'PYTHONDONTWRITEBYTECODE': '1', 'DINGWEI_MCP_IPC_SOCK': path}
        self.process = subprocess.Popen([sys.executable, '-B', '-c', code], env=env,
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, bufsize=0)
        self.buffer = b''
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.process.stdout, selectors.EVENT_READ)

    def send(self, body):
        self.process.stdin.write((json.dumps(body) + '\n').encode())
        self.process.stdin.flush()

    def read(self):
        deadline = time.monotonic() + 4
        while b'\n' not in self.buffer:
            if time.monotonic() > deadline: raise AssertionError('shim response deadline')
            if self.selector.select(.1):
                chunk = os.read(self.process.stdout.fileno(), 65536)
                if not chunk: raise AssertionError('shim stdout closed')
                self.buffer += chunk
        line, self.buffer = self.buffer.split(b'\n', 1)
        return json.loads(line)

    def close(self):
        self.process.stdin.close()
        self.process.stdin = None
        try:
            tail, errors = self.process.communicate(timeout=3)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.communicate()
            raise
        finally:
            self.selector.close()
        return self.process.returncode, self.buffer + tail, errors


@unittest.skipUnless(hasattr(socket, 'AF_UNIX'), 'requires Unix sockets')
class ShimTransportTests(unittest.TestCase):
    def exercise(self, mode, expected, tool='send_message'):
        with tempfile.TemporaryDirectory(prefix='dw-mcp-', dir='/tmp') as tmp:
            path = tmp + '/ipc.sock'
            upload = str(Path(tmp) / 'fixture.txt')
            Path(upload).write_text('non-source fixture')
            peer = None
            if mode == 'refused':
                with socket.socket(socket.AF_UNIX) as stale: stale.bind(path)
            elif mode != 'missing':
                peer = Peer(path, mode)
            child = Child(path, tmp)
            try:
                child.send(request(1, tool, upload))
                reply = child.read()
                self.assertEqual(reply['id'], 1)
                self.assertNotIn('error', reply)
                self.assertTrue(reply['result']['isError'])
                error = json.loads(reply['result']['content'][0]['text'])
                self.assertEqual(error['error'], expected)
                self.assertFalse(error['retryable'])
                self.assertEqual(error['outcome'], 'unknown')
                self.assertNotIn(SENTINEL, json.dumps(reply))
                self.assertNotIn(tmp, json.dumps(reply))
                self.assertIsNone(child.process.poll())
                if peer is not None:
                    self.assertEqual(peer.accepted, 1, 'implicit retry before caller recovery')
                child.send({'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list'})
                self.assertEqual(child.read()['id'], 2)
                if peer is None:
                    if os.path.exists(path): os.unlink(path)
                    peer = Peer(path, 'success')
                child.send(request(3, tool, upload))
                recovered = child.read()
                self.assertEqual(recovered['id'], 3)
                self.assertNotIn('error', recovered)
                self.assertFalse(recovered['result'].get('isError', False))
                self.assertIsNone(child.process.poll())
                self.assertEqual(peer.accepted, 1 if mode in ('missing', 'refused') else 2)
            finally:
                code, tail, stderr = child.close()
                if peer is not None: peer.close()
            self.assertEqual(code, 0, stderr.decode())
            self.assertEqual(tail, b'', 'duplicate or unsolicited response')
            self.assertEqual(stderr, b'')
            self.assertFalse(peer.errors)

    def test_eighteen_failures_preserve_process_and_allow_explicit_recovery(self):
        cases = [
            ('missing', 'ipc_unavailable', 'send_message'),
            ('refused', 'ipc_unavailable', 'send_message'),
            ('reset', 'ipc_unavailable', 'send_message'),
            ('timeout', 'ipc_timeout', 'send_message'),
            ('rejected', 'helper_rejected', 'send_message'),
            ('eof', 'ipc_invalid_response', 'send_message'),
            ('badjson', 'ipc_invalid_response', 'send_message'),
            ('truncated', 'ipc_invalid_response', 'send_message'),
            ('utf8', 'ipc_invalid_response', 'send_message'),
            ('array', 'ipc_invalid_response', 'send_message'),
            ('null', 'ipc_invalid_response', 'send_message'),
            ('scalar', 'ipc_invalid_response', 'send_message'),
            ('missing_ok', 'ipc_invalid_response', 'send_message'),
            ('wrong_ok', 'ipc_invalid_response', 'send_message'),
            ('unsafe_rejected', 'helper_rejected', 'send_message'),
            ('bad_outcome', 'ipc_invalid_response', 'send_message'),
            ('vsapi_rejected', 'helper_rejected', 'heartbeat_report'),
            ('upload_rejected', 'helper_rejected', 'upload_file'),
        ]
        self.assertEqual(len(cases), 18)
        for mode, expected, tool in cases:
            with self.subTest(mode=mode, tool=tool): self.exercise(mode, expected, tool)
        print('MCP_TRANSPORT_READINGS errors=18 survived=18 recovery_same_process=18', flush=True)

    def test_all_registered_tools_return_sanitized_execution_errors(self):
        for tool in mcp_shim._handlers:
            with self.subTest(tool=tool): self.exercise('rejected', 'project_context_unsupported' if tool=='project_context' else 'blocker_unsupported_upgrade_helper' if tool.startswith('dw_task_blocker_') else 'question_unsupported_upgrade_helper' if tool.startswith('dw_task_question_') else 'template_unsupported_upgrade_helper' if tool=='dw_spec_template' else 'clarification_unsupported_upgrade_helper' if tool=='dw_spec_clarifications' else 'helper_rejected', tool)

    def test_unexpected_handler_error_is_contained_and_baseexception_is_not(self):
        def broken(args): raise RuntimeError(SENTINEL)
        with patch.dict(mcp_shim._handlers, {'send_message': broken}):
            result = mcp_shim._dispatch(mcp_shim.Request('2.0', 'tools/call',
                                       request(1)['params'], 1))
            self.assertTrue(result.result['isError'])
            self.assertNotIn(SENTINEL, json.dumps(result.result))
        for exception in (KeyboardInterrupt, SystemExit):
            def stopped(args): raise exception()
            with patch.dict(mcp_shim._handlers, {'send_message': stopped}):
                with self.assertRaises(exception):
                    mcp_shim._dispatch(mcp_shim.Request('2.0', 'tools/call', request(1)['params'], 1))

    def test_invalid_tool_arguments_remain_protocol_errors(self):
        response = mcp_shim._dispatch(mcp_shim.Request('2.0', 'tools/call',
                                     {'name': 'send_message', 'arguments': {}}, 1))
        self.assertEqual(response.error['code'], -32602)


if __name__ == '__main__':
    unittest.main()
