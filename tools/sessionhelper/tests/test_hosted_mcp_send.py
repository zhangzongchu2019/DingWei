"""Hosted opt-in IPC: real app/listener, stdio shim and SCM_RIGHTS uploads.

All credentials and services are synthetic and confined to temporary directories.
"""
import asyncio
import contextlib
import io
import json
import os
from pathlib import Path
import selectors
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

try:
    import test_file_upload as fx
except ImportError:
    from tests import test_file_upload as fx
from sessionhelper import mcp_shim
from sessionhelper.config import load_config
from sessionhelper.ipc_listener import _handle_connection


def setUpModule():
    fx._HOME.start()


def tearDownModule():
    fx._HOME.finish()


class HostedSendTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.layout = fx.Layout(Path(self.tmp.name))
        self.service = fx.FakeFileService()
        self.helper = fx.make_helper(self.layout, SH_IPC_SEND_BODY_ONLY='1')
        self.ws = fx.TicketWS(self.helper, self.service.base)
        self.sent = []
        self.fail = False
        self.stdout = contextlib.redirect_stdout(io.StringIO())
        self.stdout.__enter__()
        original = self.helper._make_send_handler(self.ws)
        async def recording(to, body, meta):
            self.sent.append((to, body, meta))
            if self.fail:
                raise OSError('SYNTHETIC-SECRET /private/credential must not escape')
            return await original(to, body, meta)
        self.harness = fx.UploadHarness(self.helper, self.ws, recording)

    def tearDown(self):
        self.harness.close()
        self.stdout.__exit__(None, None, None)
        self.service.close()
        self.layout.cleanup()
        self.tmp.cleanup()

    def send(self, **kw):
        return fx.raw_ipc(self.layout.sock, dict(type='mcp_send', to='peer', body='hello', **kw))

    def test_body_and_reply_metadata_use_existing_sender(self):
        result = self.send(reply_to='peer#k', in_reply_to='id', reply_policy='no_reply', kind='reply')
        self.assertTrue(result['ok'])
        self.assertTrue(result['outcome']['sent'])
        self.assertIn('#', result['outcome']['to'])
        self.assertEqual(self.sent, [('peer', 'hello', dict(reply_to='peer#k', in_reply_to='id', reply_policy='no_reply', kind='reply'))])

    def test_secret_paths_denied_before_any_listener_open(self):
        for path in (self.layout.auth, self.layout.cred, self.layout.sock + '.secret', '/etc/passwd'):
            with self.subTest(path=str(path)), mock.patch('builtins.open', side_effect=AssertionError('no file reads')):
                result = self.send(file=str(path))
            self.assertEqual(result, {'ok': False, 'error': 'operation_not_allowed'})
        self.assertEqual(self.sent, [])

    def test_unrelated_operations_never_dispatch(self):
        for kind in ('vsapi_v1', 'file_access_v1', 'spec_write_v1', 'desktop_inbox_v1', 'arbitrary'):
            result = fx.raw_ipc(self.layout.sock, dict(type=kind, path=str(self.layout.auth), operation='status'))
            self.assertEqual(result, {'ok': False, 'error': 'operation_not_allowed'})
        self.assertEqual(self.sent, [])
        self.assertTrue(self.send()['ok'], 'listener remains usable')

    def test_invalid_shapes_and_unknown_metadata(self):
        for patch in ({'to': []}, {'body': {}}, {'body': ' '}, {'request_id': []}, {'kind': {}},
                      {'extra': str(self.layout.auth)}, {'file': ''}, {'body': 'x' * 65537}):
            payload = dict(type='mcp_send', to='peer', body='hi'); payload.update(patch)
            result = fx.raw_ipc(self.layout.sock, payload)
            self.assertFalse(result['ok'])
            self.assertNotIn('private', json.dumps(result))
        self.assertEqual(self.sent, [])

    def test_upload_stays_available_and_secret_upload_stays_denied(self):
        path = self.layout.root / 'report.txt'; path.write_text('public report')
        result = fx.dingwei_file.upload_file(self.layout.sock, str(path))
        self.assertEqual(result['size'], 13)
        self.assertEqual(len(self.service.requests), 1)
        with self.assertRaises(Exception):
            fx.dingwei_file.upload_file(self.layout.sock, str(self.layout.auth))
        self.assertEqual(len(self.service.requests), 1)
        self.assertEqual(self.sent, [])

    def test_fd_send_denied_and_fd_closed(self):
        with self.layout.auth.open('rb') as f:
            result = fx.raw_ipc(self.layout.sock, dict(type='mcp_send', to='peer', body='hi'), fd=f.fileno())
        self.assertEqual(result['error'], 'operation_not_allowed')
        self.assertEqual(self.sent, [])

    def test_error_is_sanitized_one_call_no_retry_then_recovers(self):
        self.fail = True
        result = self.send()
        self.assertEqual(result, {'ok': False, 'error': 'send_failed'})
        self.assertEqual(len(self.sent), 1)
        self.fail = False
        self.assertTrue(self.send()['ok'])
        self.assertEqual(len(self.sent), 2)

    def test_stdio_rejects_file_without_open_or_send_and_survives(self):
        env = dict(PATH=os.defpath, HOME=self.tmp.name, PYTHONPATH=str(Path(__file__).resolve().parents[1]),
                   SH_IPC_SEND_BODY_ONLY='1', DINGWEI_MCP_IPC_SOCK=self.layout.sock)
        # The trap executes only after imports: attempts to read a file via shim must fail before open.
        code = 'from sessionhelper import mcp_shim as m;import builtins;builtins.open=lambda *a,**k:(_ for _ in ()).throw(AssertionError("file read trap"));m.serve()'
        child = subprocess.Popen([sys.executable, '-B', '-c', code], env=env, stdin=subprocess.PIPE,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        def call(request):
            child.stdin.write(json.dumps(request)+'\n'); child.stdin.flush()
            with selectors.DefaultSelector() as sel:
                sel.register(child.stdout, selectors.EVENT_READ)
                self.assertTrue(sel.select(5), 'shim response deadline')
            return json.loads(child.stdout.readline())
        def tool(ident, name, args):
            return call(dict(jsonrpc='2.0', id=ident, method='tools/call', params=dict(name=name, arguments=args)))
        try:
            listing = call(dict(jsonrpc='2.0', id=1, method='tools/list'))['result']['tools']
            self.assertEqual({t['name'] for t in listing}, {'send_message', 'upload_file'})
            schema = next(t['inputSchema'] for t in listing if t['name']=='send_message')
            self.assertNotIn('file', schema['properties'])
            self.assertEqual(schema['required'], ['to', 'body'])
            for ident, args in enumerate((dict(to='peer', file=str(self.layout.auth)),
                                          dict(to='peer', body='hi', file='')), 2):
                result = tool(ident, 'send_message', args)['result']
                self.assertTrue(result['isError'])
                self.assertNotIn(str(self.layout.auth), json.dumps(result))
                self.assertFalse(json.loads(result['content'][0]['text'])['retryable'])
            self.assertTrue(tool(4, 'heartbeat_report', dict(status='active'))['result']['isError'])
            self.assertEqual(self.sent, [])
            self.fail = True
            self.assertTrue(tool(5, 'send_message', dict(to='peer', body='hi'))['result']['isError'])
            self.assertEqual(len(self.sent), 1)
            self.fail = False
            self.assertFalse(tool(6, 'send_message', dict(to='peer', body='hi'))['result'].get('isError', False))
            self.assertEqual(len(self.sent), 2)
            unknown = tool(7, '/private/SYNTHETIC-SECRET', {})['result']
            self.assertTrue(unknown['isError'])
            self.assertNotIn('SYNTHETIC-SECRET', json.dumps(unknown))
            self.assertIsNone(child.poll())
        finally:
            child.stdin.close(); child.wait(timeout=5)
            err = child.stderr.read(); child.stdout.close(); child.stderr.close()
        self.assertEqual(child.returncode, 0, err)
        self.assertNotIn('file read trap', err)


class ModesTest(unittest.TestCase):
    def test_mode_configuration_and_bad_value(self):
        env = dict(SH_SESSION_NAME='fixture', SH_KEY_ID='fixture', SH_SECRET='synthetic')
        self.assertFalse(load_config(env).ipc_send_body_only)
        self.assertTrue(load_config(dict(env, SH_IPC_SEND_BODY_ONLY='1')).ipc_send_body_only)
        with self.assertRaisesRegex(ValueError, '^invalid SH_IPC_SEND_BODY_ONLY$'):
            load_config(dict(env, SH_IPC_SEND_BODY_ONLY='/private/secret'))

    def test_legacy_upload_only_has_priority_when_both_set(self):
        class Pipe:
            async def readline(self): return b'{"type":"mcp_send","to":"peer","body":"hi"}\n'
            def write(self, data): self.data = data
            async def drain(self): pass
        pipe = Pipe()
        async def forbidden(*a): raise AssertionError('must not send')
        asyncio.run(_handle_connection(pipe, pipe, forbidden, upload_only=True, send_body_only=True))
        self.assertFalse(json.loads(pipe.data)['ok'])


if __name__ == '__main__':
    unittest.main()
