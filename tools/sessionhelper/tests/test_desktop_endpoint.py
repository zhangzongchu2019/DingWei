import json
import os
import socket
import tempfile
import unittest
from pathlib import Path
from websockets.sync.server import unix_serve
from sessionhelper.desktop_endpoint import probe_endpoint, EndpointProbeError


class EndpointTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='dw-', dir=os.path.realpath('/tmp'))
        self.path = str(Path(self.tmp.name) / 'control.sock')
        self.calls = []
    def tearDown(self):
        self.tmp.cleanup()
    def exercise(self, variant='normal'):
        def handler(ws):
            packet = json.loads(ws.recv());self.calls.append(packet)
            ws.send(json.dumps({'id': 1, 'result': {}}))
            self.calls.append(json.loads(ws.recv()))
            packet = json.loads(ws.recv());self.calls.append(packet)
            if variant == 'approval':
                ws.send(json.dumps({'id': 9, 'method': 'approval/request', 'params': {}}))
            elif variant == 'error':
                ws.send(json.dumps({'id': 2, 'error': {'message': 'private synthetic secret'}}))
            elif variant == 'replace':
                os.unlink(self.path)
                replacement = socket.socket(socket.AF_UNIX)
                replacement.bind(self.path)
                os.chmod(self.path, 0o600)
                ws.send(json.dumps({'id': 2, 'result': {'data': ['chosen']}}))
                replacement.close()
            else:
                ws.send(json.dumps({'method': 'item/delta', 'params': {'text': 'unrelated synthetic private text'}}))
                result = {'data': ['chosen', 'other']}
                if variant == 'pagination':result['nextCursor'] = 'more'
                ws.send(json.dumps({'id': 2, 'result': result}))
        with unix_serve(handler, self.path) as server:
            os.chmod(self.path, 0o600)
            import threading
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                return probe_endpoint(self.path, 'chosen')
            finally:
                server.shutdown();thread.join(2)
    def test_real_unix_handshake_no_history_or_turn(self):
        result = self.exercise()
        self.assertTrue(result['selected_thread_loaded'])
        self.assertEqual(result['loaded_thread_count'], 2)
        self.assertFalse(result['original_desktop_binding_verified'])
        self.assertNotIn('unrelated', json.dumps(result))
        self.assertEqual([x['method'] for x in self.calls], ['initialize', 'initialized', 'thread/loaded/list'])
    def test_reject_server_approval_without_reply(self):
        with self.assertRaisesRegex(EndpointProbeError, '^server_request_requires_owner$'):
            self.exercise('approval')
    def test_remote_error_text_not_exposed(self):
        with self.assertRaisesRegex(EndpointProbeError, '^request_rejected$'):
            self.exercise('error')
    def test_partial_loaded_list_not_success(self):
        with self.assertRaisesRegex(EndpointProbeError, '^loaded_list_incomplete_or_invalid$'):
            self.exercise('pagination')
    def test_socket_replacement_not_accepted(self):
        with self.assertRaisesRegex(EndpointProbeError, "^socket_replaced$"):
            self.exercise("replace")
    def test_unprivate_or_symlink_path_no_connection(self):
        sock = socket.socket(socket.AF_UNIX);sock.bind(self.path)
        try:
            os.chmod(self.path, 0o666)
            with self.assertRaisesRegex(EndpointProbeError, 'socket_not_private_owned'):
                probe_endpoint(self.path, 'chosen')
            os.chmod(self.path, 0o600)
            alias = str(Path(self.tmp.name) / 'alias');os.symlink(self.path, alias)
            with self.assertRaisesRegex(EndpointProbeError, 'symlink_path'):
                probe_endpoint(alias, 'chosen')
        finally:sock.close()


if __name__ == '__main__':unittest.main()
