"""Kernel peer checks and real local Unix/WebSocket wiring; no live seats/model."""
import json
import queue
import types
import os
from pathlib import Path
import socket
import struct
import sys
import tempfile
import threading
import unittest
from unittest import mock

from sessionhelper import daemon_identity as identity
from sessionhelper.codexDriver import CodexProtocolDriver, UnixWebSocketJsonRpc


class PeerChecks(unittest.TestCase):
    def setUp(self):
        self.left, self.right = socket.socketpair()
        self.addCleanup(self.left.close); self.addCleanup(self.right.close)
        self.pid = os.getpid()
        self.start = identity.process_starttime(self.pid)

    def reject(self, reason, **kwargs):
        sock = kwargs.pop('sock', self.left)
        with self.assertRaises(identity.DaemonIdentityError, msg='IDENTITY_ACCEPTED_' + reason) as caught:
            identity.verify_peer(sock, kwargs.pop('expected_exe', sys.executable), **kwargs)
        self.assertEqual(caught.exception.reason, reason, 'IDENTITY_WRONG_REASON_' + reason)
        self.assertEqual(str(caught.exception), 'daemon_identity_' + reason)

    def test_real_kernel_uid_exe_and_optional_owned_binding(self):
        result = identity.verify_peer(self.left, sys.executable)
        self.assertEqual((result.pid, result.uid, result.starttime, result.scope),
                         (self.pid, os.getuid(), self.start, 'uid_exe_only'), 'KERNEL_IDENTITY_LOST')
        owned = identity.OwnedDaemonIdentity(self.pid, self.start)
        self.assertEqual(identity.verify_peer(self.left, sys.executable, owned).scope,
                         'owned_pid_starttime', 'OWNED_BINDING_NOT_RECORDED')

    def test_foreign_uid_rejected(self):
        peer = mock.Mock(); peer.getsockopt.return_value = struct.pack('iII', self.pid, os.getuid() + 1, os.getgid())
        self.reject('uid_mismatch', sock=peer)

    def test_unverifiable_peer_and_unsupported_platform(self):
        for value in (b'', b'x', struct.pack('iII', 0, os.getuid(), os.getgid()), None):
            peer = mock.Mock(); peer.getsockopt.return_value = value
            self.reject('peer_unverifiable', sock=peer)
        peer = mock.Mock(); peer.getsockopt.side_effect = OSError('PRIVATE')
        self.reject('peer_unverifiable', sock=peer)
        with mock.patch.object(identity.sys, 'platform', 'darwin'):
            self.reject('unsupported')

    def test_wrong_executable_and_missing_expected_file(self):
        self.reject('exe_mismatch', expected_exe='/bin/true')
        self.reject('expected_exe_unverifiable', expected_exe='/absent/private-path')
        with tempfile.TemporaryDirectory() as tmp:
            wrapper = Path(tmp) / 'codex'; wrapper.write_text('#!/bin/sh\nexec /bin/true\n'); wrapper.chmod(0o700)
            self.reject('exe_mismatch', expected_exe=str(wrapper))
            wrapper.chmod(0o600)
            self.reject('expected_exe_unverifiable', expected_exe=str(wrapper))

    def test_proc_exe_missing_deleted_or_stat_denied_is_unverifiable(self):
        real_readlink = os.readlink
        def unreadable(path, *args, **kwargs):
            if str(path) == '/proc/' + str(self.pid) + '/exe': raise PermissionError('PRIVATE')
            return real_readlink(path, *args, **kwargs)
        with mock.patch.object(identity.os, 'readlink', side_effect=unreadable):
            self.reject('exe_unverifiable')
        def deleted(path, *args, **kwargs):
            if str(path) == '/proc/' + str(self.pid) + '/exe': return '/private/codex (deleted)'
            return real_readlink(path, *args, **kwargs)
        with mock.patch.object(identity.os, 'readlink', side_effect=deleted):
            self.reject('exe_unverifiable')
        real_stat = Path.stat
        def denied(path, *args, **kwargs):
            if str(path) == '/proc/' + str(self.pid) + '/exe': raise PermissionError('PRIVATE')
            return real_stat(path, *args, **kwargs)
        with mock.patch.object(Path, 'stat', denied): self.reject('exe_unverifiable')

    def test_starttime_missing_and_process_change_rejected(self):
        with mock.patch.object(identity, 'process_starttime', side_effect=identity.DaemonIdentityError('starttime_unverifiable')):
            self.reject('starttime_unverifiable')
        with mock.patch.object(identity, 'process_starttime', side_effect=[self.start, self.start + 1]):
            self.reject('process_changed')

    def test_owned_pid_starttime_and_types_are_strict(self):
        self.reject('owned_pid_mismatch', owned=identity.OwnedDaemonIdentity(self.pid + 1, self.start))
        self.reject('owned_starttime_mismatch', owned=identity.OwnedDaemonIdentity(self.pid, self.start + 1))
        for owned in ({'pid': self.pid}, identity.OwnedDaemonIdentity(True, self.start),
                      identity.OwnedDaemonIdentity(self.pid, True), identity.OwnedDaemonIdentity(0, self.start)):
            self.reject('owned_binding_invalid', owned=owned)

    def test_stat_parser_handles_parentheses_and_fails_closed(self):
        raw = f'{self.pid} (comm ) with spaces)' + ' R' + ' 0' * 18 + ' 321'
        with mock.patch.object(Path, 'read_text', return_value=raw):
            self.assertEqual(identity.process_starttime(self.pid), 321, 'STARTTIME_FIELD_WRONG')
        for raw in ('bad', f'{self.pid + 1} (comm)' + ' R' + ' 0' * 18 + ' 321',
                    f'{self.pid} (comm) R', f'{self.pid} (comm)' + ' R' + ' 0' * 19):
            with mock.patch.object(Path, 'read_text', return_value=raw):
                with self.assertRaises(identity.DaemonIdentityError, msg='BAD_PROC_STAT_ACCEPTED'):
                    identity.process_starttime(self.pid)


class LocalPeer:
    """Minimal real AF_UNIX WS endpoint, recording every received client byte."""
    def __init__(self, path, cwd):
        self.path = str(path); self.cwd = cwd; self.bytes = []; self.methods = []; self.errors = []
        self.listener = socket.socket(socket.AF_UNIX); self.listener.bind(self.path)
        self.listener.listen(8); self.listener.settimeout(.1)
        self.stopped = threading.Event(); self.received_done = threading.Event(); self.connections = []
        self.thread = threading.Thread(target=self.run, daemon=True); self.thread.start()

    def run(self):
        while not self.stopped.is_set():
            try: conn, _ = self.listener.accept()
            except socket.timeout: continue
            except OSError: break
            self.connections.append(conn)
            worker = threading.Thread(target=self.handle, args=(conn,), daemon=True); worker.start()

    def handle(self, conn):
        conn.settimeout(2); received = bytearray(); self.bytes.append(received)
        try:
            header = b''
            while b'\r\n\r\n' not in header:
                chunk = conn.recv(4096)
                if not chunk: return
                received.extend(chunk); header += chunk
            conn.sendall(b'HTTP/1.1 101 Switching Protocols\r\n\r\n')
            def exact(count):
                value = b''
                while len(value) < count:
                    chunk = conn.recv(count - len(value))
                    if not chunk: raise EOFError()
                    received.extend(chunk); value += chunk
                return value
            while not self.stopped.is_set():
                first, second = exact(2); size = second & 127
                if size == 126: size = struct.unpack('!H', exact(2))[0]
                if size == 127: size = struct.unpack('!Q', exact(8))[0]
                mask = exact(4) if second & 128 else b''
                payload = exact(size)
                if first & 15 == 8: return
                if mask: payload = bytes(v ^ mask[i % 4] for i, v in enumerate(payload))
                msg = json.loads(payload); self.methods.append(msg['method'])
                if 'id' not in msg: continue
                result = {}
                if msg['method'] in ('thread/start', 'thread/read', 'thread/resume'):
                    result = {'thread': {'id': 'fixture-thread', 'cwd': self.cwd,
                                         'status': {'type': 'idle'}, 'canAcceptDirectInput': True}}
                if msg['method'] == 'thread/turns/list': result = {'data': []}
                if msg['method'] == 'turn/start': result = {'turn': {'id': 'fixture-turn', 'status': 'inProgress'}}
                response = json.dumps({'id': msg['id'], 'result': result}).encode()
                conn.sendall(bytes([0x81, len(response)]) + response if len(response) < 126
                             else b'\x81\x7e' + struct.pack('!H', len(response)) + response)
        except (EOFError, OSError): pass
        except Exception as exc: self.errors.append(type(exc).__name__)
        finally:
            conn.close(); self.received_done.set()

    def close(self):
        self.stopped.set(); self.listener.close()
        for conn in self.connections:
            try: conn.shutdown(socket.SHUT_RDWR)
            except OSError: pass
            conn.close()
        self.thread.join(timeout=2)


class DaemonWiring(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / 'control.sock'
        self.peer = LocalPeer(self.path, self.tmp.name); self.addCleanup(self.peer.close)
        self.driver = CodexProtocolDriver(workdir=self.tmp.name, manage_daemon=False,
            daemon_identity_check='enforce', daemon_expected_exe=sys.executable, store_path=str(Path(self.tmp.name) / 'ledger.db'))
        self.driver._daemon_socket_path = lambda: str(self.path)
        self.addCleanup(self.driver.stop)

    def test_enforce_real_kernel_then_initialize_and_reconnect(self):
        with mock.patch('sessionhelper.codexDriver.verify_peer', wraps=identity.verify_peer) as check:
            self.driver.start(); self.assertTrue(self.driver.health())
            self.driver._rpc.stop(); self.driver.start()
            self.assertEqual(check.call_count, 2, 'RECONNECT_IDENTITY_NOT_RECHECKED')
        events = self.driver._event_log
        good = [e for e in events if e.data.get('change') == 'daemon_identity_verified']
        self.assertEqual(len(good), 2, 'VERIFIED_IDENTITY_EVENT_MISSING')
        self.assertEqual(good[0].data['identity_scope'], 'uid_exe_only')
        self.assertEqual(self.peer.methods.count('initialize'), 2)
        self.assertNotIn('thread/start', self.peer.methods)
        self.assertEqual(self.peer.errors, [])

    def test_wrong_exe_has_zero_handshake_bytes_and_unhealthy_closed_rpc(self):
        self.driver._daemon_expected_exe = '/bin/true'
        with self.assertRaises(identity.DaemonIdentityError, msg='WRONG_EXE_CONNECTED'):
            self.driver.start()
        self.assertFalse(self.driver.health(), 'REJECTED_RPC_HEALTHY')
        self.assertIsNone(self.driver._rpc, 'REJECTED_RPC_RETAINED')
        self.assertFalse(self.driver._initialized)
        self.assertTrue(self.peer.received_done.wait(1), 'REJECTED_PEER_NOT_CLOSED')
        self.assertTrue(all(not b for b in self.peer.bytes), 'UNVERIFIED_PEER_RECEIVED_BYTES')
        bad = [e for e in self.driver._event_log if e.kind == 'error' and e.data.get('change') == 'daemon_identity_rejected']
        self.assertTrue(bad, 'REJECTION_EVENT_MISSING')
        self.assertEqual(bad[-1].data['error_class'], 'daemon_identity_exe_mismatch', 'REJECTION_EVENT_MISSING')

    def test_rejected_delivery_persists_and_never_starts_thread_or_turn(self):
        self.driver._daemon_expected_exe = '/bin/true'
        with mock.patch.object(self.driver, 'start', wraps=self.driver.start) as start:
            first = self.driver.deliver('fixture-envelope', 'PRIVATE BODY')
            persisted = self.driver._store.get_by_envelope('fixture-envelope')
            self.assertIsNotNone(persisted, 'IDENTITY_FAILURE_NOT_DURABLE')
            second = self.driver.deliver('fixture-envelope', 'PRIVATE BODY')
            self.assertEqual(start.call_count, 1, 'IDENTITY_FAILURE_RETRIED')
        self.assertEqual(first.state, 'failed', 'UNVERIFIED_DELIVERY_ADMITTED')
        self.assertTrue(first.detail.startswith('session_unavailable:'), 'IDENTITY_NOT_SESSION_UNAVAILABLE')
        self.assertIn('daemon_identity_exe_mismatch', first.detail)
        self.assertEqual(second.detail, first.detail, 'IDENTITY_FAILURE_RETRIED')
        self.assertEqual(self.driver._thread_id, '')
        self.assertEqual(self.peer.methods, [], 'UNVERIFIED_PEER_RPC_SENT')
        self.assertNotIn('PRIVATE', first.detail)
        record = self.driver._store.get_by_envelope('fixture-envelope')
        self.assertIsNotNone(record, 'IDENTITY_FAILURE_NOT_DURABLE')
        self.assertEqual(record.state, 'failed', 'IDENTITY_FAILURE_NOT_DURABLE')

    def test_owned_binding_wired_and_foreign_instance_rejected(self):
        self.driver._owned_daemon_identity = identity.OwnedDaemonIdentity(os.getpid() + 1, 1)
        with self.assertRaises(identity.DaemonIdentityError, msg='OWNED_BINDING_NOT_WIRED'):
            self.driver.start()
        self.assertEqual(self.peer.methods, [])

    def test_explicit_off_is_visible_and_invalid_setting_refused(self):
        self.driver._daemon_identity_check = 'off'
        self.driver._daemon_expected_exe = '/bin/true'
        with self.assertLogs('sessionhelper.codexDriver', level='WARNING') as logs:
            self.driver.start()
        self.assertTrue(self.driver.health())
        self.assertIn('identity_scope=unchecked', '\n'.join(logs.output), 'DISABLED_GATE_SILENT')
        self.driver.stop(); self.driver._daemon_identity_check = 'invalid'
        self.driver._daemon_expected_exe = sys.executable
        with self.assertRaises(identity.DaemonIdentityError, msg='INVALID_SWITCH_CONNECTED'):
            self.driver.start()

    def test_rejected_fd_is_explicitly_closed_without_gc(self):
        real_socket = socket.socket; retained = []
        def create(*args, **kwargs):
            sock = real_socket(*args, **kwargs); retained.append(sock); return sock
        rpc = UnixWebSocketJsonRpc(str(self.path), identity_check='enforce', expected_exe='/bin/true')
        with mock.patch('sessionhelper.codexDriver.socket.socket', side_effect=create):
            with self.assertRaises(identity.DaemonIdentityError): rpc.start()
        self.addCleanup(rpc.stop)
        for sock in retained: self.addCleanup(sock.close)
        self.assertEqual(retained[0].fileno(), -1, 'REJECTED_FD_OPEN')
        self.assertFalse(rpc.healthy())

    def test_direct_proxy_keep_contract_without_linux_gate(self):
        for mode in ('direct', 'proxy'):
            fake = types.SimpleNamespace(start=lambda: None, stop=lambda: None,
                healthy=lambda: False, request=lambda *a, **k: {}, notify=lambda *a, **k: None,
                notifications=queue.Queue())
            d = CodexProtocolDriver(transport_mode=mode, manage_daemon=False,
                daemon_identity_check='invalid', daemon_expected_exe='/absent',
                store_path=str(Path(self.tmp.name) / (mode + '.db')))
            try:
                with mock.patch('sessionhelper.codexDriver.StdioJsonRpc', return_value=fake) as constructor:
                    with mock.patch('sessionhelper.codexDriver.verify_peer', side_effect=AssertionError('NON_UNIX_GATE_USED')):
                        d.start()
                self.assertTrue(d._initialized, 'NON_UNIX_CONTRACT_CHANGED')
                self.assertTrue(constructor.called)
            finally: d.stop()

    def test_default_observe_wrong_exe_warns_and_connects(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            driver = CodexProtocolDriver(workdir=self.tmp.name, manage_daemon=False,
                daemon_expected_exe='/bin/true', store_path=str(Path(self.tmp.name) / 'observe.db'))
        self.addCleanup(driver.stop)
        driver._daemon_socket_path = lambda: str(self.path)
        with self.assertLogs('sessionhelper.codexDriver', level='WARNING') as logs:
            try:
                driver.start()
            except identity.DaemonIdentityError:
                self.fail('OBSERVE_REJECTED_CONNECTION')
        self.assertTrue(driver.health(), 'OBSERVE_REJECTED_CONNECTION')
        self.assertEqual(self.peer.methods, ['initialize', 'initialized'], 'OBSERVE_REJECTED_CONNECTION')
        self.assertIn('warning=daemon_identity_exe_mismatch', '\n'.join(logs.output), 'OBSERVE_WARNING_MISSING')
        events = [e for e in driver._event_log if e.data.get('change') == 'daemon_identity_observed']
        self.assertTrue(events, 'OBSERVE_EVENT_MISSING')
        self.assertEqual(events[-1].data['identity_scope'], 'unverified')
        self.assertFalse(any(e.data.get('change') == 'daemon_identity_verified' for e in driver._event_log))

    def test_observe_unverifiable_proc_warns_and_rechecks(self):
        self.driver._daemon_identity_check = 'observe'
        with mock.patch('sessionhelper.codexDriver.verify_peer', side_effect=identity.DaemonIdentityError('exe_unverifiable')) as check:
            with self.assertLogs('sessionhelper.codexDriver', level='WARNING'):
                self.driver.start(); self.driver._rpc.stop(); self.driver.start()
        self.assertEqual(check.call_count, 2, 'OBSERVE_RECONNECT_SKIPPED')
        self.assertTrue(self.driver.health(), 'OBSERVE_REJECTED_CONNECTION')
        self.assertEqual(len([e for e in self.driver._event_log if e.data.get('change') == 'daemon_identity_observed']), 2)

    def test_env_three_modes_and_invalid_snapshot(self):
        for raw, expected in [('observe', 'observe'), ('enforce', 'enforce'), ('off', 'off'),
                              ('1', '1'), ('0', '0'), ('true', 'true'), ('', '')]:
            with mock.patch.dict(os.environ, {'SH_CODEX_DAEMON_IDENTITY_CHECK': raw}):
                drv = CodexProtocolDriver(store_path=str(Path(self.tmp.name) / ('ledger-' + str(raw) + '.db')))
                self.assertEqual(drv._daemon_identity_check, expected, 'IDENTITY_SWITCH_PARSE_WRONG')
        with mock.patch.dict(os.environ, {}, clear=True):
            drv = CodexProtocolDriver(store_path=str(Path(self.tmp.name) / 'default.db'))
            self.assertEqual(drv._daemon_identity_check, 'observe', 'IDENTITY_DEFAULT_NOT_OBSERVE')
        for raw in ('1', '0', 'true', '', None, False):
            rpc = UnixWebSocketJsonRpc(str(self.path), identity_check=raw, expected_exe=sys.executable)
            self.addCleanup(rpc.stop)
            with self.assertRaises(identity.DaemonIdentityError, msg='INVALID_SWITCH_CONNECTED'):
                rpc.start()


if __name__ == '__main__':
    unittest.main()
