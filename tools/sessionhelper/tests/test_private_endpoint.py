"""Private endpoint boundaries. Local fixtures only: no CLI discovery or live seats."""
from contextlib import contextmanager
from dataclasses import asdict
import json
import hashlib
import os
from pathlib import Path
import socket
import signal
import sys
import tempfile
import threading
import time
import types
import unittest
from unittest import mock

from sessionhelper import private_endpoint as ep
from sessionhelper.daemon_identity import OwnedDaemonIdentity, process_starttime
from sessionhelper.codexDriver import CodexProtocolDriver, UnixWebSocketJsonRpc
try:  # unittest discovery and canonical path invocation use different roots.
    from test_daemon_identity import LocalPeer
except ImportError:
    from tools.sessionhelper.tests.test_daemon_identity import LocalPeer


@unittest.skipUnless(sys.platform == 'linux' and ep.fcntl is not None, 'private endpoint is Linux-only; legacy remains portable')
class Fixture(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='dwpe-')
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name); self.base.chmod(0o700)
        self.home = self.base / 'ch'; self.home.mkdir(mode=0o700)
        self.spec = ep.Spec(str(self.base / 's'), 'fixture-seat', str(self.home),
                            str(self.base), sys.executable, sys.executable)

    def reject(self, reason, callback):
        with self.assertRaises(ep.PrivateEndpointError, msg='PRIVATE_ACCEPTED_' + reason) as caught:
            callback()
        self.assertEqual(caught.exception.reason, 'endpoint_' + reason,
                         'PRIVATE_WRONG_REASON_' + reason)

    @contextmanager
    def root(self):
        with ep.root_fd(self.spec, create=True) as fd:
            yield fd

    def check_root(self):
        with ep.root_fd(self.spec):
            pass


class Paths(Fixture):
    def test_canonical_config_and_endpoint_identity_are_seat_bound(self):
        other = ep.Spec(self.spec.state_dir, 'other-seat', self.spec.codex_home,
                        self.spec.workdir, sys.executable, sys.executable)
        self.assertNotEqual(self.spec.root, other.root, 'SEAT_ENDPOINT_COLLISION')
        self.assertNotEqual(self.spec.fingerprint(), other.fingerprint(), 'SEAT_CONFIG_COLLISION')
        self.assertEqual(len(self.spec.root.name), 16)
        with self.root() as fd:
            self.assertEqual(os.fstat(fd).st_mode & 0o777, 0o700, 'PRIVATE_ROOT_MODE')
            ep.unchanged_root(self.spec, fd)

    def test_long_path_and_parent_traversal_refused(self):
        for state in (str(self.base / ('a' * 100)), str(self.base / '..' / 'unexpected')):
            self.reject('path', lambda: ep.Spec(state, 'seat', str(self.home), str(self.base),
                                                sys.executable, sys.executable))

    def test_unsafe_directory_modes_and_symlink_refused(self):
        with self.root():
            pass
        self.spec.root.chmod(0o750)
        self.reject('permissions', self.check_root)
        self.spec.root.chmod(0o700)
        parent = self.spec.root.parent
        self.spec.root.rmdir(); self.spec.root.symlink_to(self.home, target_is_directory=True)
        self.reject('path', self.check_root)
        self.spec.root.unlink(); parent.chmod(0o777)
        self.reject('permissions', self.check_root)

    def test_registry_permissions_symlink_hardlink_and_size_refused(self):
        with self.root() as fd:
            ep.write_json(fd, ep.REGISTRY, {'format': 1})
            target = self.spec.root / ep.REGISTRY
            self.assertEqual(ep.read_json(fd, ep.REGISTRY), {'format': 1})
            target.chmod(0o644)
            self.reject('file', lambda: ep.read_json(fd, ep.REGISTRY))
            target.chmod(0o600)
            os.link(target, self.spec.root / 'link')
            self.reject('file', lambda: ep.read_json(fd, ep.REGISTRY))
            (self.spec.root / 'link').unlink()
            # Valid JSON exactly one byte over the limit: parsing must not
            # provide an accidental second reason to reject this mutation.
            padding = ep.LIMIT + 1 - len(json.dumps({'payload': ''}))
            target.write_bytes(json.dumps({'payload': 'x' * padding}).encode())
            self.reject('registry', lambda: ep.read_json(fd, ep.REGISTRY))
            target.unlink(); target.symlink_to(self.home / 'absent')
            self.reject('file', lambda: ep.read_json(fd, ep.REGISTRY))

    def test_socket_alias_and_writable_socket_rejected(self):
        with self.root() as fd:
            listener = socket.socket(socket.AF_UNIX)
            self.addCleanup(listener.close)
            path = self.spec.root / 'test.sock'; listener.bind(str(path)); path.chmod(0o600)
            expected = ep.socket_identity(fd, path.name)
            alias = self.spec.root / ep.CONTROL; alias.symlink_to(path)
            self.reject('permissions', lambda: ep.socket_identity(fd, ep.CONTROL))
            path.chmod(0o666)
            self.reject('permissions', lambda: ep.socket_identity(fd, path.name))
            path.chmod(0o600)
            self.reject('path', lambda: ep.unlink_socket(fd, path.name, (expected[0], expected[1] + 1)))
            self.assertTrue(path.exists(), 'FOREIGN_SOCKET_UNLINKED')
            ep.unlink_socket(fd, path.name, expected)

    def test_orphan_registry_never_spawns_or_adopts(self):
        with self.root() as fd:
            ep.write_json(fd, ep.REGISTRY, {'owner_pid': os.getpid()})
        with mock.patch.object(ep.subprocess, 'Popen', side_effect=AssertionError('ORPHAN_WAS_RESTARTED')) as launch:
            self.reject('dirty', lambda: ep.Client(self.spec).ensure())
        launch.assert_not_called()

    def test_stopped_tombstone_refuses_automatic_start(self):
        with self.root() as fd:
            ep.write_json(fd, 'stopped.json', {'format': 1})
        with mock.patch.object(ep.subprocess, 'Popen', side_effect=AssertionError('STOPPED_OWNER_AUTORESTARTED')) as launch:
            self.reject('stopped', lambda: ep.Client(self.spec).ensure())
        launch.assert_not_called()

    def test_wrong_app_binding_receives_no_handshake_bytes(self):
        from sessionhelper.daemon_identity import DaemonIdentityError
        with self.root():
            pass
        peer = LocalPeer(self.spec.root / ep.APP, str(self.base)); self.addCleanup(peer.close)
        (self.spec.root / ep.APP).chmod(0o600)
        with self.assertRaises(DaemonIdentityError, msg='WRONG_NATIVE_PEER_ACCEPTED') as caught:
            ep.native_call(self.spec, OwnedDaemonIdentity(os.getpid() + 1, process_starttime(os.getpid())),
                           lambda request: self.fail('WRONG_NATIVE_PEER_CALLED'))
        self.assertTrue(peer.received_done.wait(5), 'NATIVE_REJECTION_DID_NOT_CLOSE')
        self.assertTrue(all(not x for x in peer.bytes), 'NATIVE_UNVERIFIED_HANDSHAKE_SENT')
        self.assertEqual(caught.exception.reason, 'owned_pid_mismatch', 'IDENTITY_FAILURE_ERASED')

    def test_wrong_app_executable_receives_no_handshake_bytes(self):
        from sessionhelper.daemon_identity import DaemonIdentityError
        with self.root():
            pass
        peer = LocalPeer(self.spec.root / ep.APP, str(self.base)); self.addCleanup(peer.close)
        (self.spec.root / ep.APP).chmod(0o600)
        spec = ep.Spec(self.spec.state_dir, self.spec.seat, self.spec.codex_home,
                       self.spec.workdir, sys.executable, '/bin/true')
        with self.assertRaises(DaemonIdentityError, msg='WRONG_NATIVE_EXE_ACCEPTED') as caught:
            ep.native_call(spec, OwnedDaemonIdentity(os.getpid(), process_starttime(os.getpid())),
                           lambda request: self.fail('WRONG_NATIVE_EXE_CALLED'))
        self.assertTrue(peer.received_done.wait(5), 'NATIVE_REJECTION_DID_NOT_CLOSE')
        self.assertTrue(all(not x for x in peer.bytes), 'WRONG_NATIVE_EXE_RECEIVED_BYTES')
        self.assertEqual(caught.exception.reason, 'exe_mismatch')


class NativeAlias(Fixture):
    def setUp(self):
        super().setUp()
        self.cm = ep.root_fd(self.spec, create=True); self.fd = self.cm.__enter__()
        self.addCleanup(self.cm.__exit__, None, None, None)
        self.parent = self.base / 'native'; self.parent.mkdir(mode=0o700)
        self.name = '1' * 64; self.target = self.parent / self.name
        self.peer = LocalPeer(self.target, str(self.base)); self.addCleanup(self.peer.close)
        self.target.chmod(0o600)
        self.alias = self.spec.root / ep.APP; self.alias.symlink_to(self.target)
        patch = mock.patch.object(ep, 'native_location', return_value=(self.parent, self.name))
        patch.start(); self.addCleanup(patch.stop)
        self.binding = OwnedDaemonIdentity(os.getpid(), process_starttime(os.getpid()))

    def connect(self, **kwargs):
        sock, ref = ep.app_socket(self.spec, self.binding, allow_new_alias=True, **kwargs)
        sock.close()
        return ref

    def test_native_location_uses_canonical_request_bytes_and_fixed_uid_root(self):
        # Test the unpatched implementation, independently of the safe fake dir.
        with mock.patch.object(ep, 'native_location', wraps=_NATIVE_LOCATION):
            parent, name = ep.native_location(self.spec)
        expected = hashlib.sha256(os.fsencode(self.spec.root.resolve() / ep.APP)).hexdigest()
        self.assertEqual(parent, Path('/tmp').resolve() / ('codex-daemon-' + str(os.getuid())), 'NATIVE_LOCATION_ROOT_DRIFT')
        self.assertEqual(name, expected, 'NATIVE_LOCATION_HASH_DRIFT')

    def test_readlink_once_and_connects_only_calculated_target(self):
        seen, connected = [], []
        original = os.readlink; socket_type = socket.socket
        def readlink(name, *args, **kwargs):
            if name == ep.APP:
                seen.append(name)
            return original(name, *args, **kwargs)
        class Tracked(socket_type):
            def connect(self, path):
                connected.append(path)
                return super().connect(path)
        with mock.patch.object(ep.os, 'readlink', side_effect=readlink), mock.patch.object(ep.socket, 'socket', Tracked):
            ref = self.connect()
            self.connect(endpoint=ref)
        self.assertEqual(seen, [ep.APP], 'NATIVE_ALIAS_READ_AGAIN')
        self.assertEqual(connected, [str(self.target), str(self.target)], 'ALIAS_REUSED_FOR_CONNECT')

    def test_preexisting_alias_stops_before_popen_and_unbound_alias_cannot_connect(self):
        owner = ep.Owner(self.spec, self.fd)
        with mock.patch.object(ep.subprocess, 'Popen', side_effect=AssertionError('PREEXISTING_ALIAS_SPAWNED')) as launch:
            self.reject('dirty', owner.spawn)
        launch.assert_not_called()
        self.reject('binding', lambda: ep.app_socket(self.spec, self.binding))

    def test_outside_relative_and_wrong_digest_alias_are_refused(self):
        for value in (self.base / 'outside', self.parent / ('2' * 64), Path('relative-socket')):
            self.alias.unlink(); self.alias.symlink_to(value)
            self.reject('path', self.connect)
        self.assertEqual(self.peer.bytes, [], 'OUTSIDE_ALIAS_RECEIVED_BYTES')

    def test_target_and_native_directory_permissions_are_hard_gates(self):
        self.target.chmod(0o666)
        self.reject('permissions', self.connect)
        self.target.chmod(0o600)
        self.parent.chmod(0o755)
        self.reject('permissions', self.connect)
        self.parent.chmod(0o700)
        original = os.fstat; target_inode = self.target.stat().st_ino
        def foreign(fd):
            info = original(fd)
            if info.st_ino == target_inode:
                fields = list(info); fields[4] = os.getuid() + 1
                return os.stat_result(fields)
            return info
        with mock.patch.object(ep.os, 'fstat', side_effect=foreign):
            self.reject('permissions', self.connect)
        parent_inode = self.parent.stat().st_ino
        def foreign_parent(fd):
            info = original(fd)
            if info.st_ino == parent_inode:
                fields = list(info); fields[4] = os.getuid() + 1
                return os.stat_result(fields)
            return info
        with mock.patch.object(ep.os, 'fstat', side_effect=foreign_parent):
            self.reject('permissions', self.connect)
        self.assertEqual(self.peer.bytes, [], 'UNSAFE_TARGET_RECEIVED_BYTES')

    def test_native_directory_symlink_is_not_followed(self):
        moved = self.parent.with_name('moved-native')
        self.parent.rename(moved); self.parent.symlink_to(moved, target_is_directory=True)
        self.reject('path', self.connect)

    def test_wrong_peer_pid_or_starttime_is_rejected_before_http(self):
        for owned, reason in ((OwnedDaemonIdentity(os.getpid() + 1, self.binding.starttime), 'owned_pid_mismatch'),
                              (OwnedDaemonIdentity(os.getpid(), self.binding.starttime + 1), 'owned_starttime_mismatch')):
            with self.assertRaises(ep.DaemonIdentityError, msg='ALIAS_WRONG_PEER_ACCEPTED') as caught:
                ep.app_socket(self.spec, owned, allow_new_alias=True)
            self.assertEqual(caught.exception.reason, reason, 'ALIAS_IDENTITY_REASON_LOST')
        self.assertTrue(self.peer.received_done.wait(5), 'ALIAS_REJECTED_FD_NOT_CLOSED')
        self.assertTrue(all(not value for value in self.peer.bytes), 'ALIAS_UNVERIFIED_HTTP_SENT')

    def test_alias_replaced_after_resolution_is_refused(self):
        original = ep.verify_peer
        def replace(sock, exe, owned):
            result = original(sock, exe, owned)
            self.alias.unlink(); self.alias.symlink_to(self.target)
            return result
        with mock.patch.object(ep, 'verify_peer', side_effect=replace):
            self.reject('path', self.connect)

    def test_target_replaced_after_resolution_is_refused_and_not_unlinked(self):
        original = ep.verify_peer; replacement = socket.socket(socket.AF_UNIX)
        self.addCleanup(replacement.close)
        def replace(sock, exe, owned):
            result = original(sock, exe, owned)
            self.target.rename(self.parent / 'old-target')
            replacement.bind(str(self.target)); self.target.chmod(0o600)
            return result
        with mock.patch.object(ep, 'verify_peer', side_effect=replace):
            self.reject('path', self.connect)
        self.assertTrue(self.target.exists(), 'REPLACEMENT_TARGET_WAS_DELETED')

    def test_cleanup_cannot_remove_replaced_alias(self):
        anchors = []
        ref = self.connect(retain_anchors=anchors)
        self.addCleanup(lambda: [os.close(fd) for fd in anchors])
        self.assertEqual(len(anchors), 3, 'ALIAS_ANCHORS_NOT_RETAINED')
        self.assertEqual(ep.inode(os.fstat(anchors[0])), ref.alias, 'ALIAS_ANCHOR_WRONG_OBJECT')
        self.alias.unlink(); self.alias.symlink_to(self.base / 'other')
        self.reject('path', lambda: ep.cleanup_app(self.spec, self.fd, ref))
        self.assertTrue(self.alias.is_symlink(), 'FOREIGN_ALIAS_REMOVED')


_NATIVE_LOCATION = ep.native_location


class ControlFixture:
    """Real control socket and held lock, owned by this local test process."""
    def __init__(self, spec):
        self.spec = spec; self.stopped = threading.Event(); self.calls = []
        self.record = {'format': 1, 'owner_pid': os.getpid(),
                       'owner_starttime': process_starttime(os.getpid()),
                       'python': ep.executable(sys.executable)[0], 'config': spec.fingerprint(),
                       'implementation': ep.implementation_hash(), 'boot': 'a' * 32}
        with ep.root_fd(spec, create=True) as fd:
            self.lock = ep.open_file(fd, 'owner.lock', create=True)
            ep.fcntl.flock(self.lock, ep.fcntl.LOCK_EX | ep.fcntl.LOCK_NB)
            ep.write_json(fd, ep.REGISTRY, self.record)
        self.listener = socket.socket(socket.AF_UNIX)
        self.listener.bind(str(spec.root / ep.CONTROL)); self.listener.listen(8)
        (spec.root / ep.CONTROL).chmod(0o600); self.listener.settimeout(.05)
        self.reply = {'ok': True, 'state': 'ready', 'generation': 1,
                      'child': {'pid': os.getpid(), 'starttime': process_starttime(os.getpid())},
                      'endpoint': {'kind': 'direct', 'alias': [1, 1], 'target': [1, 1], 'directory': None}}
        self.override = {}
        self.thread = threading.Thread(target=self.run, daemon=True); self.thread.start()

    def run(self):
        while not self.stopped.is_set():
            try:
                conn, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            with conn:
                conn.settimeout(2)
                try:
                    request = ep.packet(conn); self.calls.append(request)
                    ep.send_packet(conn, {**self.reply, 'boot': request.get('boot'),
                                          'challenge': request.get('challenge'), **self.override})
                except (OSError, ep.PrivateEndpointError):
                    pass

    def close(self):
        self.stopped.set(); self.listener.close(); self.thread.join(2)
        os.close(self.lock)


class Control(Fixture):
    def setUp(self):
        super().setUp()
        self.owner = ControlFixture(self.spec); self.addCleanup(self.owner.close)
        self.client = ep.Client(self.spec)
        self.client.take_writer(); self.addCleanup(self.client.release_writer)

    def rewrite(self, **fields):
        with self.root() as fd:
            ep.write_json(fd, ep.REGISTRY, {**self.owner.record, **fields})

    def test_live_kernel_binding_challenge_and_reuse_without_launch(self):
        with mock.patch.object(ep.subprocess, 'Popen') as launch:
            first = self.client.ensure(); second = ep.Client(self.spec).ensure()
        launch.assert_not_called()
        self.assertEqual((first['child'], first['generation']), (second['child'], second['generation']),
                         'OWNER_REUSE_CHANGED_CHILD')
        self.assertNotEqual(self.owner.calls[0]['challenge'], self.owner.calls[1]['challenge'],
                            'OWNER_CHALLENGE_REUSED')

    def test_registry_is_locator_not_authority(self):
        from sessionhelper.daemon_identity import DaemonIdentityError
        for fields, reason in (({'owner_pid': os.getpid() + 1}, 'owned_pid_mismatch'),
                               ({'owner_starttime': process_starttime(os.getpid()) + 1}, 'owned_starttime_mismatch')):
            self.rewrite(**fields)
            with self.assertRaises(DaemonIdentityError, msg='REGISTRY_AUTHORIZED_FOREIGN_OWNER') as caught:
                self.client.status()
            self.assertEqual(caught.exception.reason, reason)
        self.assertEqual(self.owner.calls, [], 'WRONG_OWNER_RECEIVED_CONTROL_BYTES')

    def test_config_implementation_and_owner_python_must_match(self):
        for fields, reason in (({'config': '0' * 64}, 'registry'),
                               ({'implementation': '0' * 64}, 'registry'),
                               ({'owner_pid': True}, 'registry'), ({'format': True}, 'registry'),
                               ({'python': '/bin/true'}, 'owner')):
            self.rewrite(**fields)
            self.reject(reason, self.client.status)
        self.assertEqual(self.owner.calls, [], 'INVALID_OWNER_RECEIVED_CONTROL_BYTES')

    def test_listener_without_live_owner_lock_is_not_owner(self):
        ep.fcntl.flock(self.owner.lock, ep.fcntl.LOCK_UN)
        self.reject('owner', self.client.status)
        self.assertEqual(self.owner.calls, [], 'UNLOCKED_OWNER_RECEIVED_BYTES')

    def test_replayed_challenge_wrong_boot_and_nonboolean_ok_refused(self):
        for override, reason in (({'challenge': 'replay'}, 'owner'), ({'boot': 'b' * 32}, 'owner'),
                                  ({'ok': 1}, 'protocol'), ({'generation': True}, 'child'),
                                  ({'child': {'pid': True, 'starttime': 1}}, 'child')):
            self.owner.override = override
            self.reject(reason, self.client.status)

    def test_model_admission_compares_live_child_not_stale_rpc(self):
        peer = types.SimpleNamespace(uid=os.getuid(), pid=os.getpid(),
                                     starttime=process_starttime(os.getpid()), scope='owned_pid_starttime')
        with self.client.admission(peer):
            pass
        peer.starttime += 1
        self.reject('binding', lambda: self.client.admission(peer).__enter__())

    def test_duplicate_helper_cannot_take_writer_but_restart_can(self):
        other = ep.Client(self.spec)
        self.addCleanup(other.release_writer)
        self.reject('busy', other.take_writer)
        self.client.release_writer()
        other.take_writer()
        peer = types.SimpleNamespace(uid=os.getuid(), pid=os.getpid(),
                                     starttime=process_starttime(os.getpid()), scope='owned_pid_starttime')
        self.reject('binding', lambda: self.client.admission(peer).__enter__())
        self.assertEqual(other.status()['child'], self.client.status()['child'], 'WRITER_CHANGED_OWNER')

    def test_stop_exclusion_blocks_new_model_admission(self):
        peer = types.SimpleNamespace(uid=os.getuid(), pid=os.getpid(),
                                     starttime=process_starttime(os.getpid()), scope='owned_pid_starttime')
        with self.root() as fd:
            guard = ep.open_file(fd, 'admission.lock', create=True)
            try:
                ep.fcntl.flock(guard, ep.fcntl.LOCK_EX | ep.fcntl.LOCK_NB)
                self.reject('busy', lambda: self.client.admission(peer).__enter__())
            finally:
                os.close(guard)

    def test_human_resume_is_explicit_id_and_live_owner_only(self):
        self.client.save_thread('12345678-1234-1234-1234-123456789abc')
        with mock.patch.object(ep, 'native_call', return_value=None) as native:
            human = ep.Client(self.spec)  # Reads current hint, never takes helper writer.
            cmd = human.resume_command('12345678-1234-1234-1234-123456789abc')
            self.assertEqual(cmd, [self.spec.command, '--remote', 'unix://' + str(self.spec.root / ep.APP),
                                   'resume', '12345678-1234-1234-1234-123456789abc'])
            native.assert_called_once()
            for invalid in ('--last', '', None, 'NOT-A-THREAD'):
                self.reject('config', lambda: self.client.resume_command(invalid))
            self.reject('binding', lambda: human.resume_command('12345678-1234-1234-1234-123456789abd'))

    def test_current_thread_hint_survives_writer_restart_but_is_not_reassigned(self):
        thread_id = '12345678-1234-1234-1234-123456789abc'
        self.assertEqual(self.client.bound_thread(), '')
        self.client.save_thread(thread_id)
        self.client.release_writer()
        other = ep.Client(self.spec); other.take_writer(); self.addCleanup(other.release_writer)
        self.assertEqual(other.bound_thread(), thread_id, 'HELPER_RESTART_LOST_CURRENT_THREAD')
        self.reject('binding', lambda: other.save_thread('12345678-1234-1234-1234-123456789abd'))
        self.assertEqual(other.bound_thread(), thread_id, 'CURRENT_THREAD_HINT_REASSIGNED')

    def test_old_owner_or_config_cannot_supply_current_thread_hint(self):
        thread_id = '12345678-1234-1234-1234-123456789abc'
        for field, value in (('boot', 'b' * 32), ('config', '0' * 64), ('thread_id', '--last'), ('format', True)):
            with self.root() as fd:
                ep.write_json(fd, 'binding.json', {'format': 1, 'boot': self.owner.record['boot'],
                                                 'config': self.spec.fingerprint(), 'thread_id': thread_id,
                                                 field: value})
            self.reject('binding', self.client.bound_thread)


class Lifecycle(Fixture):
    def setUp(self):
        super().setUp()
        self.cm = ep.root_fd(self.spec, create=True); self.fd = self.cm.__enter__()
        self.addCleanup(self.cm.__exit__, None, None, None)
        self.owner = ep.Owner(self.spec, self.fd)
        self.child = mock.Mock(pid=os.getpid())
        self.child.poll.return_value = None
        self.owner.child = self.child; self.owner.child_start = process_starttime(os.getpid())

    def native(self, loaded=None, status='idle', next_cursor=None):
        def call(spec, binding, operation, **kwargs):
            def request(method, params):
                if method == 'thread/loaded/list':
                    return {'data': [] if loaded is None else loaded, 'nextCursor': next_cursor}
                return {'thread': {'id': params['threadId'], 'status': {'type': status}}}
            return operation(request)
        return mock.patch.object(ep, 'native_call', side_effect=call)

    def test_stop_requires_human_confirmation_and_idle_native_proof(self):
        with self.native():
            self.reject('human_confirmation', lambda: self.owner.stop_idle(False))
        self.child.terminate.assert_not_called()
        for status in ('active', 'notLoaded', 'systemError', 'unknown'):
            with self.native(loaded=['thread-1'], status=status):
                self.reject('busy', lambda: self.owner.stop_idle(True))
        self.child.terminate.assert_not_called()
        with self.native(loaded=['thread-1'], status='idle'):
            self.owner.stop_idle(True)
        self.child.terminate.assert_called_once_with(); self.child.wait.assert_called_once_with(timeout=5)
        self.assertEqual(self.owner.state, 'stopped', 'IDLE_OWNER_NOT_STOPPED')
        self.assertTrue(ep.exists(self.fd, 'stopped.json'), 'STOP_MISSING_TOMBSTONE')

    def test_incomplete_or_unbounded_loaded_list_cannot_prove_idle(self):
        for loaded, cursor in (([], 'next-page'), (['x'] * 4097, None), ([123], None)):
            with self.native(loaded=loaded, next_cursor=cursor):
                self.reject('protocol', self.owner.idle)
        self.child.terminate.assert_not_called()

    def test_helper_admission_lock_prevents_owner_stop(self):
        guard = ep.open_file(self.fd, 'admission.lock', create=True)
        try:
            ep.fcntl.flock(guard, ep.fcntl.LOCK_SH | ep.fcntl.LOCK_NB)
            with self.native():
                self.reject('busy', lambda: self.owner.stop_idle(True))
            self.child.terminate.assert_not_called()
        finally:
            os.close(guard)

    def test_child_exit_reaps_actual_handle_before_one_replacement(self):
        self.child.poll.return_value = 1
        self.owner.state = 'ready'
        with mock.patch.object(self.owner, 'spawn') as spawn, mock.patch.object(ep.time, 'sleep'):
            self.owner.refresh()
        self.child.wait.assert_called_once_with()
        spawn.assert_called_once_with()
        self.assertIsNone(self.owner.child, 'DEAD_CHILD_HANDLE_REUSED')
        self.assertEqual(self.owner.restarts, 1)

    def test_child_binding_rechecks_starttime_and_restart_budget_is_finite(self):
        self.owner.child_start += 1
        self.reject('child', self.owner.binding)
        self.owner.child_start -= 1
        self.child.poll.return_value = 1
        self.owner.restarts = len(ep.RESTART_DELAYS)
        with mock.patch.object(self.owner, 'spawn') as spawn, mock.patch.object(ep.time, 'sleep') as sleep:
            try:
                self.owner.refresh()
            except Exception as exc:
                self.fail('RESTART_BUDGET_RAISED ' + type(exc).__name__)
        spawn.assert_not_called(); sleep.assert_not_called()
        self.assertEqual(ep.read_json(self.fd, ep.REGISTRY)['state'], 'failed')

    def test_start_failure_reaps_owned_child_and_remains_observable(self):
        self.owner.failed_start('endpoint_permissions')
        self.child.terminate.assert_called_once_with(); self.child.wait.assert_any_call(timeout=5)
        record = ep.read_json(self.fd, ep.REGISTRY)
        self.assertEqual(record['state'], 'failed', 'START_FAILURE_HIDDEN')
        self.assertIsNone(record['child'], 'FAILED_CHILD_RETAINED_AS_READY')
        self.assertEqual(record['last_error'], 'endpoint_permissions')

    def test_late_exit_after_stop_timeout_never_auto_restarts(self):
        import subprocess
        self.child.wait.side_effect = subprocess.TimeoutExpired('owned fixture', 5)
        with self.native():
            self.reject('busy', lambda: self.owner.stop_idle(True))
        self.child.poll.return_value = 1; self.child.wait.side_effect = None
        with mock.patch.object(self.owner, 'spawn') as spawn:
            self.owner.refresh()
        self.assertFalse(spawn.called, 'STOP_TIMEOUT_RESTARTED_CHILD')
        self.assertEqual(self.owner.state, 'failed', 'STOP_TIMEOUT_RESTARTED_CHILD')


class DriverWiring(Fixture):
    def test_stop_during_partial_frame_uses_captured_socket_or_oserror(self):
        rpc = UnixWebSocketJsonRpc('unused-fixture')
        for read in (lambda: rpc._recv_exact(1), rpc._recv_text):
            with self.assertRaises(OSError, msg='CLOSED_READER_ASSERTED'):
                read()
        sock = mock.Mock()
        def recv(size):
            rpc._sock = None
            return b'x'
        sock.recv.side_effect = recv; rpc._sock = sock
        self.assertEqual(rpc._recv_exact(2), b'xx', 'RECV_LOST_SOCKET_DURING_STOP')

    def test_private_observe_cannot_reach_transport_even_with_valid_peer(self):
        fake = mock.Mock(); fake.spec.root = self.base
        fake.ensure.return_value = {'child': {'pid': os.getpid(), 'starttime': process_starttime(os.getpid())}}
        rpc = mock.Mock(); rpc.peer_identity = None; rpc.identity_warning = None; rpc.healthy.return_value = False
        with mock.patch('sessionhelper.codexDriver.PrivateEndpointSpec'), \
             mock.patch('sessionhelper.codexDriver.PrivateEndpointClient', return_value=fake), \
             mock.patch('sessionhelper.codexDriver.UnixWebSocketJsonRpc', return_value=rpc):
            driver = CodexProtocolDriver(workdir=str(self.base), endpoint_mode='private',
                daemon_identity_check='observe', daemon_expected_exe=sys.executable,
                store_path=str(self.base / 'observe-ledger.db'))
            self.addCleanup(driver.stop)
            self.reject('config', driver.start)
        rpc.start.assert_not_called()

    def test_private_requires_daemon_ws_and_enforce(self):
        for transport, mode, manage in (('daemon_ws', 'observe', True), ('daemon_ws', 'enforce', False),
                                        ('direct', 'enforce', True), ('proxy', 'enforce', True)):
            driver = CodexProtocolDriver(workdir=str(self.base), transport_mode=transport,
                endpoint_mode='private', endpoint_state_dir=self.spec.state_dir,
                endpoint_seat='fixture-seat', endpoint_codex_home=str(self.home),
                daemon_identity_check=mode, daemon_expected_exe=sys.executable, manage_daemon=manage,
                store_path=str(self.base / 'ledger.db'))
            self.addCleanup(driver.stop)
            self.reject('config', driver.start)

    def test_helper_restart_reuses_owner_and_stop_does_not_stop_it(self):
        path = self.base / 'app.sock'
        peer = LocalPeer(path, str(self.base)); self.addCleanup(peer.close)
        binding = {'pid': os.getpid(), 'starttime': process_starttime(os.getpid())}
        fake = mock.Mock(); fake.ensure.return_value = {'child': binding}
        fake.bound_thread.return_value = ''
        fake.spec.root = self.base
        def connect(_live):
            sock = socket.socket(socket.AF_UNIX)
            sock.connect(str(path))
            return sock
        fake.open_app.side_effect = connect
        @contextmanager
        def admission(identity):
            self.assertEqual(identity.scope, 'owned_pid_starttime', 'PRIVATE_PEER_NOT_OWNED')
            yield
        fake.admission.side_effect = admission
        with mock.patch('sessionhelper.codexDriver.PrivateEndpointClient', return_value=fake), \
             mock.patch('sessionhelper.codexDriver._CODEX_BIN', sys.executable):
            driver = CodexProtocolDriver(workdir=str(self.base), endpoint_mode='private',
                endpoint_state_dir=self.spec.state_dir, endpoint_seat='fixture-seat',
                endpoint_codex_home=str(self.home), daemon_identity_check='enforce',
                daemon_expected_exe=sys.executable, store_path=str(self.base / 'ledger.db'))
            self.addCleanup(driver.stop)
            driver.start(); driver.stop(); driver.start(); driver.stop()
        self.assertEqual(fake.ensure.call_count, 2, 'PRIVATE_RECONNECT_SKIPPED_OWNER')
        fake.stop_owner.assert_not_called()
        self.assertEqual(peer.methods.count('initialize'), 2)
        self.assertNotIn('turn/start', peer.methods)


class RealOwnerProcess(Fixture):
    def setUp(self):
        super().setUp()
        fixture = Path(__file__).with_name('private_endpoint_fake_native.py').resolve()
        wrapper = self.base / 'fake-codex'
        wrapper.write_text('#!' + sys.executable + '\nimport runpy\nrunpy.run_path(' + repr(str(fixture)) + ", run_name='__main__')\n")
        wrapper.chmod(0o700)
        self.spec = ep.Spec(self.spec.state_dir, self.spec.seat, self.spec.codex_home,
                            self.spec.workdir, str(wrapper), sys.executable)
        self.clients = [ep.Client(self.spec), ep.Client(self.spec)]
        self.addCleanup(self.cleanup_processes)

    def cleanup_processes(self):
        for client in self.clients:
            client.release_writer()
        try:
            current = self.clients[0].status()['child']
            ep.native_call(self.spec, OwnedDaemonIdentity(**current), lambda request: request('fixture/idle', {}))
            self.clients[0].stop_owner(human_disconnected=True)
        except Exception:
            pass
        for client in self.clients:
            for proc in client._launchers:
                if proc.poll() is None:
                    try:
                        proc.wait(timeout=3)
                    except __import__('subprocess').TimeoutExpired:
                        # Only the isolated session created by this test's
                        # retained Popen. This is not a product recovery API.
                        os.killpg(proc.pid, signal.SIGKILL)
                        proc.wait(timeout=3)

    def test_double_start_helper_restart_child_exit_and_idle_stop(self):
        rows, errors = [], []
        def launch(client):
            try:
                rows.append(client.ensure())
            except Exception as exc:
                errors.append(type(exc).__name__ + ':' + str(exc))
        workers = [threading.Thread(target=launch, args=(client,)) for client in self.clients]
        for worker in workers:
            worker.start()
        for worker in workers:
            worker.join(15)
        self.assertFalse(errors, 'OWNER_DOUBLE_START_FAILED ' + repr(errors))
        self.assertEqual(len(rows), 2, 'OWNER_DOUBLE_START_INCOMPLETE')
        self.assertEqual(rows[0]['child'], rows[1]['child'], 'DOUBLE_START_CREATED_TWO_CHILDREN')
        initial = rows[0]['child']
        trace = self.spec.root / 'fixture-starts.jsonl'
        self.assertEqual(len(trace.read_text().splitlines()), 1, 'DOUBLE_START_CREATED_TWO_CHILDREN')
        with mock.patch('sessionhelper.codexDriver._CODEX_BIN', self.spec.command):
            driver = CodexProtocolDriver(workdir=self.spec.workdir, endpoint_mode='private',
                endpoint_state_dir=self.spec.state_dir, endpoint_seat=self.spec.seat,
                endpoint_codex_home=self.spec.codex_home, daemon_identity_check='enforce',
                daemon_expected_exe=sys.executable, store_path=str(self.base / 'real-owner-ledger.db'))
            self.addCleanup(driver.stop)
            driver.start(); driver._ensure_thread(); original_thread = driver._thread_id
            self.assertIsNone(driver._rpc._sock.gettimeout(), 'PRIVATE_RPC_IDLE_TIMEOUT')
            time.sleep(ep.CONTROL_TIMEOUT + .1)
            self.assertTrue(driver.health(), 'QUIET_PRIVATE_RPC_DROPPED')
            driver.stop()
            self.assertEqual(self.clients[0].status()['child'], initial, 'HELPER_STOP_KILLED_OWNER_CHILD')
            driver.start(); driver._validate_bound_thread()
            self.assertEqual(driver._thread_id, original_thread, 'HELPER_RESTART_CHANGED_THREAD')
            driver.stop()
            # A fresh instance models a helper process upgrade, rather than
            # relying only on Python object memory surviving stop/start.
            driver = CodexProtocolDriver(workdir=self.spec.workdir, endpoint_mode='private',
                endpoint_state_dir=self.spec.state_dir, endpoint_seat=self.spec.seat,
                endpoint_codex_home=self.spec.codex_home, daemon_identity_check='enforce',
                daemon_expected_exe=sys.executable, store_path=str(self.base / 'real-owner-ledger.db'))
            self.addCleanup(driver.stop)
            driver.start(); driver._validate_bound_thread()
            self.assertEqual(driver._thread_id, original_thread, 'FRESH_HELPER_LOST_CURRENT_BINDING')
            driver.stop()
        # Kill only this test-created child after live control and same-FD
        # owned verification. No PID file alone authorizes this test action.
        ep.native_call(self.spec, OwnedDaemonIdentity(**initial), lambda request: None)
        os.kill(initial['pid'], signal.SIGKILL)
        deadline = time.monotonic() + 15
        while True:
            try:
                replacement = self.clients[0].status()
                if replacement['child'] != initial:
                    break
            except ep.DaemonIdentityError:
                pass
            self.assertLess(time.monotonic(), deadline, 'CHILD_EXIT_DID_NOT_RESTART')
            time.sleep(.03)
        self.assertEqual(replacement['generation'], 2, 'CHILD_GENERATION_NOT_ADVANCED')
        self.assertEqual(replacement['boot'], rows[0]['boot'], 'CHILD_EXIT_REPLACED_OWNER')
        self.assertEqual(len(trace.read_text().splitlines()), 2, 'CHILD_RESTART_COUNT_WRONG')
        driver.start(); driver._validate_bound_thread()
        self.assertEqual(driver._thread_id, original_thread, 'CHILD_RESTART_CHANGED_THREAD')
        driver.stop()
        binding = OwnedDaemonIdentity(**replacement['child'])
        ep.native_call(self.spec, binding, lambda request: request('fixture/busy', {}))
        self.reject('busy', lambda: self.clients[0].stop_owner(human_disconnected=True))
        self.assertEqual(self.clients[0].status()['child'], replacement['child'], 'BUSY_STOP_KILLED_CHILD')
        ep.native_call(self.spec, binding, lambda request: request('fixture/idle', {}))
        self.assertEqual(self.clients[0].stop_owner(human_disconnected=True)['state'], 'stopped')
        for client in self.clients:
            for proc in client._launchers:
                self.assertEqual(proc.wait(timeout=5), 0, 'OWNER_CLEAN_STOP_FAILED')
        self.assertFalse((self.spec.root / ep.REGISTRY).exists(), 'STOP_LEFT_LIVE_REGISTRY')
        self.assertFalse((self.spec.root / ep.CONTROL).exists(), 'STOP_LEFT_CONTROL_SOCKET')
        self.assertFalse((self.spec.root / ep.APP).exists(), 'STOP_LEFT_APP_SOCKET')
        self.reject('stopped', self.clients[0].ensure)


if __name__ == '__main__':
    unittest.main()
