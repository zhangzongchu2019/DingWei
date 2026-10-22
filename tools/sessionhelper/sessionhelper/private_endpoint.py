"""Opt-in, local helper-owned Codex endpoint. No discovery or orphan adoption.

A surviving owner holds the actual child Popen and flock. The registry is a
locator, never authority to adopt/kill a PID. Control responses require a live
kernel peer binding and a fresh challenge. No credential/environment persistence.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
from dataclasses import asdict, dataclass
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import stat
import subprocess
import sys
import time
import uuid

from .config import env_get, env_snapshot
from .daemon_identity import DaemonIdentityError, OwnedDaemonIdentity, process_starttime, verify_peer

try:
    import fcntl
except ImportError:  # Importing the legacy driver remains portable.
    fcntl = None

LIMIT = 16384
CONTROL_TIMEOUT = 3.0
START_TIMEOUT = 12.0
RESTART_DELAYS = (.1, .5, 1.0)
REGISTRY = 'registry.json'
CONTROL = 'owner.sock'
APP = 'app.sock'


class PrivateEndpointError(DaemonIdentityError):
    REASONS = DaemonIdentityError.REASONS | frozenset({
        'endpoint_config', 'endpoint_path', 'endpoint_permissions', 'endpoint_file',
        'endpoint_registry', 'endpoint_owner', 'endpoint_protocol', 'endpoint_timeout',
        'endpoint_unavailable', 'endpoint_dirty', 'endpoint_stopped', 'endpoint_busy',
        'endpoint_binding', 'endpoint_child', 'endpoint_human_confirmation',
    })


def fail(reason):
    raise PrivateEndpointError('endpoint_' + reason) from None


def implementation_hash():
    return hashlib.sha256(Path(__file__).read_bytes()).hexdigest()


def executable(path):
    try:
        resolved = Path(path).expanduser().resolve(strict=True)
        info = resolved.stat()
        if (not stat.S_ISREG(info.st_mode) or not os.access(resolved, os.X_OK)
                or info.st_mode & 0o022 or info.st_uid not in (0, os.getuid())):
            fail('config')
        return str(resolved), [info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns]
    except (OSError, ValueError, RuntimeError, TypeError):
        fail('config')


@dataclass(frozen=True)
class Spec:
    state_dir: str
    seat: str
    codex_home: str
    workdir: str
    command: str
    expected_exe: str

    def __post_init__(self):
        if sys.platform != 'linux' or fcntl is None:
            fail('config')
        if not all(type(v) is str and v and len(v) <= 4096 for v in asdict(self).values()):
            fail('config')
        if len(self.seat) > 256:
            fail('config')
        for name in ('state_dir', 'codex_home', 'workdir'):
            p = Path(getattr(self, name)).expanduser().absolute()
            if '..' in p.parts:
                fail('path')
            object.__setattr__(self, name, str(p))
        try:
            for name in ('codex_home', 'workdir'):
                p = Path(getattr(self, name)).resolve(strict=True)
                if not p.is_dir():
                    fail('config')
                object.__setattr__(self, name, str(p))
        except (OSError, RuntimeError):
            fail('config')
        for name in ('command', 'expected_exe'):
            object.__setattr__(self, name, executable(getattr(self, name))[0])
        if max(len(os.fsencode(str(self.root / n))) for n in (CONTROL, APP)) > 103:
            fail('path')

    @property
    def root(self):
        key = hashlib.sha256((str(os.getuid()) + '\0' + self.seat + '\0' + self.codex_home).encode()).hexdigest()[:16]
        return Path(self.state_dir) / 'codex-control' / key

    def fingerprint(self):
        fields = {**asdict(self), 'command_identity': executable(self.command)[1],
                  'exe_identity': executable(self.expected_exe)[1]}
        return hashlib.sha256(json.dumps(fields, sort_keys=True).encode()).hexdigest()


@contextmanager
def root_fd(spec, *, create=False):
    """Walk with directory FDs: no hidden symlink traversal or writable parents."""
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    state_depth = len(Path(spec.state_dir).parts) - 1
    try:
        for depth, part in enumerate(spec.root.parts[1:], 1):
            if create:
                try:
                    os.mkdir(part, 0o700, dir_fd=fd)
                except FileExistsError:
                    pass
            next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            os.close(fd); fd = next_fd
            info = os.fstat(fd)
            sticky_root_parent = info.st_uid == 0 and bool(info.st_mode & stat.S_ISVTX)
            if (info.st_uid not in (0, os.getuid())
                    or (info.st_mode & 0o022 and not sticky_root_parent)):
                fail('permissions')
            if depth >= state_depth and info.st_uid != os.getuid():
                fail('permissions')
            if depth > state_depth and stat.S_IMODE(info.st_mode) != 0o700:
                fail('permissions')
        yield fd
    except OSError:
        fail('path')
    finally:
        os.close(fd)


def unchanged_root(spec, fd):
    with root_fd(spec) as fresh:
        a, b = os.fstat(fd), os.fstat(fresh)
        if (a.st_dev, a.st_ino) != (b.st_dev, b.st_ino):
            fail('path')


def open_file(fd, name, *, create=False):
    try:
        f = os.open(name, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC
                    | (os.O_CREAT if create else 0), 0o600, dir_fd=fd)
        info = os.fstat(f)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
            os.close(f); fail('file')
        return f
    except OSError:
        fail('file')


def exists(fd, name):
    try:
        os.stat(name, dir_fd=fd, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False


def read_json(fd, name):
    f = open_file(fd, name)
    try:
        info = os.fstat(f)
        if not 0 < info.st_size <= LIMIT:
            fail('registry')
        raw = os.read(f, LIMIT + 1)
        if len(raw) != info.st_size:
            fail('registry')
        value = json.loads(raw)
        if type(value) is not dict:
            fail('registry')
        return value
    except (ValueError, UnicodeError, OSError):
        fail('registry')
    finally:
        os.close(f)


def write_json(fd, name, value):
    raw = json.dumps(value, sort_keys=True, separators=(',', ':')).encode()
    if len(raw) > LIMIT:
        fail('registry')
    if exists(fd, name):
        previous = open_file(fd, name); os.close(previous)
    temporary = '.' + name + '-' + secrets.token_hex(8)
    f = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=fd)
    try:
        with os.fdopen(f, 'wb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        os.replace(temporary, name, src_dir_fd=fd, dst_dir_fd=fd); os.fsync(fd)
    finally:
        if exists(fd, temporary):
            os.unlink(temporary, dir_fd=fd)


def socket_identity(fd, name):
    try:
        info = os.stat(name, dir_fd=fd, follow_symlinks=False)
        if (not stat.S_ISSOCK(info.st_mode) or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != 0o600):
            fail('permissions')
        return info.st_dev, info.st_ino
    except OSError:
        fail('path')


def unlink_socket(fd, name, identity):
    if not exists(fd, name):
        return
    if socket_identity(fd, name) != identity:
        fail('path')
    os.unlink(name, dir_fd=fd)


def positive(value):
    return type(value) is int and value > 0


def packet(sock):
    raw = bytearray()
    while b'\n' not in raw:
        chunk = sock.recv(min(4096, LIMIT + 1 - len(raw)))
        if not chunk or len(raw) >= LIMIT:
            fail('protocol')
        raw.extend(chunk)
    if len(raw) > LIMIT or raw.count(b'\n') != 1 or not raw.endswith(b'\n'):
        fail('protocol')
    try:
        result = json.loads(raw)
        if type(result) is not dict:
            fail('protocol')
        return result
    except (ValueError, UnicodeError):
        fail('protocol')


def send_packet(sock, value):
    raw = json.dumps(value, separators=(',', ':')).encode() + b'\n'
    if len(raw) > LIMIT:
        fail('protocol')
    sock.sendall(raw)


def lock_is_held(fd):
    f = open_file(fd, 'owner.lock', create=True)
    try:
        try:
            fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return True
        fcntl.flock(f, fcntl.LOCK_UN)
        return False
    finally:
        os.close(f)


def native_call(spec, binding, operation):
    """Bounded readonly native calls after same-FD owned verification."""
    from websockets.sync.client import unix_connect
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.settimeout(CONTROL_TIMEOUT)
    try:
        with root_fd(spec) as fd:
            before = socket_identity(fd, APP)
            sock.connect(str(spec.root / APP))
            verify_peer(sock, spec.expected_exe, binding)
            unchanged_root(spec, fd)
            if socket_identity(fd, APP) != before:
                fail('path')
        with unix_connect(sock=sock, uri='ws://localhost/', open_timeout=CONTROL_TIMEOUT,
                          close_timeout=.2, max_size=65536, max_queue=4, compression=None) as ws:
            counter = 0
            def request(method, params):
                nonlocal counter
                counter += 1; tag = counter
                ws.send(json.dumps({'id': tag, 'method': method, 'params': params}))
                deadline = time.monotonic() + CONTROL_TIMEOUT
                for _ in range(64):
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        fail('timeout')
                    message = json.loads(ws.recv(timeout=remaining))
                    if type(message) is not dict or ('id' in message and 'method' in message):
                        fail('protocol')
                    if type(message.get('id')) is int and message['id'] == tag:
                        if 'error' in message or type(message.get('result')) is not dict:
                            fail('protocol')
                        return message['result']
                    if 'id' in message or type(message.get('method')) is not str:
                        fail('protocol')
                fail('protocol')
            request('initialize', {'clientInfo': {'name': 'dingwei-endpoint-owner', 'version': '1'},
                                   'capabilities': {'experimentalApi': True}})
            ws.send(json.dumps({'method': 'initialized', 'params': {}}))
            return operation(request)
    except DaemonIdentityError:
        raise
    except Exception:
        fail('child')
    finally:
        sock.close()


class Client:
    def __init__(self, spec):
        self.spec = spec
        self._launchers = []

    def _query(self, op='status', *, human_disconnected=False):
        with root_fd(self.spec) as fd:
            if not lock_is_held(fd):
                fail('owner')
            record = read_json(fd, REGISTRY)
            if (record.get('format') != 1 or type(record.get('format')) is not int
                    or record.get('config') != self.spec.fingerprint()
                    or record.get('implementation') != implementation_hash()
                    or not positive(record.get('owner_pid')) or not positive(record.get('owner_starttime'))
                    or type(record.get('boot')) is not str or len(record['boot']) != 32):
                fail('registry')
            python = executable(sys.executable)[0]
            if record.get('python') != python:
                fail('owner')
            before = socket_identity(fd, CONTROL)
            challenge = secrets.token_hex(16)
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.settimeout(CONTROL_TIMEOUT)
            try:
                sock.connect(str(self.spec.root / CONTROL))
                verify_peer(sock, python, OwnedDaemonIdentity(record['owner_pid'], record['owner_starttime']))
                send_packet(sock, {'format': 1, 'op': op, 'boot': record['boot'],
                                   'challenge': challenge, 'human_disconnected': human_disconnected})
                reply = packet(sock)
                unchanged_root(self.spec, fd)
                if ((op != 'stop' and socket_identity(fd, CONTROL) != before)
                        or reply.get('challenge') != challenge or reply.get('boot') != record['boot']):
                    fail('owner')
                if type(reply.get('ok')) is not bool:
                    fail('protocol')
                if reply['ok'] is not True:
                    reason = reply.get('reason')
                    if reason not in PrivateEndpointError.REASONS:
                        fail('protocol')
                    raise PrivateEndpointError(reason)
                if op == 'stop':
                    return reply
                child = reply.get('child')
                if (reply.get('state') != 'ready' or type(child) is not dict
                        or not positive(child.get('pid')) or not positive(child.get('starttime'))
                        or not positive(reply.get('generation'))):
                    fail('child')
                return reply
            except (OSError, ValueError, UnicodeError):
                fail('unavailable')
            finally:
                sock.close()

    def status(self):
        return self._query()

    def ensure(self):
        with root_fd(self.spec, create=True) as fd:
            launch_lock = open_file(fd, 'launch.lock', create=True)
            deadline = time.monotonic() + START_TIMEOUT
            try:
                while True:
                    try:
                        fcntl.flock(launch_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        break
                    except BlockingIOError:
                        if time.monotonic() >= deadline:
                            fail('timeout')
                        time.sleep(.02)
                if exists(fd, 'stopped.json'):
                    read_json(fd, 'stopped.json'); fail('stopped')
                if not lock_is_held(fd):
                    if any(exists(fd, n) for n in (REGISTRY, CONTROL, APP)):
                        fail('dirty')
                    env = env_snapshot()
                    env['CODEX_HOME'] = self.spec.codex_home
                    package_root = str(Path(__file__).resolve().parent.parent)
                    env['PYTHONPATH'] = package_root
                    proc = subprocess.Popen([sys.executable, '-B', '-m', 'sessionhelper.private_endpoint', '--owner'],
                        stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                        env=env, cwd=self.spec.workdir, start_new_session=True, close_fds=True)
                    self._launchers.append(proc)
                    try:
                        proc.stdin.write(json.dumps(asdict(self.spec)).encode() + b'\n'); proc.stdin.close()
                    except (BrokenPipeError, OSError):
                        pass
                last = None
                while time.monotonic() < deadline:
                    try:
                        return self.status()
                    except (PrivateEndpointError, DaemonIdentityError) as exc:
                        last = exc
                        time.sleep(.03)
                if last is not None:
                    raise last
                fail('timeout')
            finally:
                os.close(launch_lock)

    @contextmanager
    def admission(self, peer):
        with root_fd(self.spec) as fd:
            guard = open_file(fd, 'admission.lock', create=True)
            try:
                try:
                    fcntl.flock(guard, fcntl.LOCK_SH | fcntl.LOCK_NB)
                except BlockingIOError:
                    fail('busy')
                current = self.status()['child']
                if (peer is None or peer.scope != 'owned_pid_starttime' or peer.uid != os.getuid()
                        or (peer.pid, peer.starttime) != (current['pid'], current['starttime'])):
                    fail('binding')
                yield
            finally:
                os.close(guard)

    def stop_owner(self, *, human_disconnected=False):
        return self._query('stop', human_disconnected=human_disconnected)

    def resume_command(self, thread_id):
        try:
            if type(thread_id) is not str or str(uuid.UUID(thread_id)) != thread_id:
                fail('config')
        except (ValueError, AttributeError):
            fail('config')
        current = self.status()['child']
        native_call(self.spec, OwnedDaemonIdentity(current['pid'], current['starttime']), lambda request: None)
        return [self.spec.command, '--remote', 'unix://' + str(self.spec.root / APP), 'resume', thread_id]


class Owner:
    def __init__(self, spec, fd):
        self.spec = spec; self.fd = fd
        self.boot = secrets.token_hex(16); self.child = None; self.child_start = None
        self.generation = 0; self.restarts = 0; self.state = 'starting'
        self.app_identity = None; self.control_identity = None
        self.record = {'format': 1, 'owner_pid': os.getpid(), 'owner_starttime': process_starttime(os.getpid()),
                       'python': executable(sys.executable)[0], 'config': spec.fingerprint(),
                       'implementation': implementation_hash(), 'boot': self.boot}

    def publish(self):
        write_json(self.fd, REGISTRY, {**self.record, 'state': self.state, 'generation': self.generation,
                                      'child': self.binding() if self.state == 'ready' else None})

    def binding(self):
        if (self.child is None or self.child.poll() is not None or self.child_start is None
                or process_starttime(self.child.pid) != self.child_start):
            fail('child')
        return {'pid': self.child.pid, 'starttime': self.child_start}

    def spawn(self):
        if exists(self.fd, APP):
            fail('dirty')
        self.state = 'starting'; self.publish()
        env = env_snapshot(); env['CODEX_HOME'] = self.spec.codex_home
        self.child = subprocess.Popen([self.spec.command, 'app-server', '--listen', 'unix://' + str(self.spec.root / APP)],
            cwd=self.spec.workdir, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL, close_fds=True)
        self.child_start = process_starttime(self.child.pid)
        deadline = time.monotonic() + START_TIMEOUT - 1
        while time.monotonic() < deadline:
            if self.child.poll() is not None:
                fail('child')
            if exists(self.fd, APP):
                # Codex may set socket mode itself, independently of umask.
                # Behind the private root, prove our held child on this FD
                # before changing only that newly created socket's mode.
                info = os.stat(APP, dir_fd=self.fd, follow_symlinks=False)
                if not stat.S_ISSOCK(info.st_mode) or info.st_uid != os.getuid():
                    fail('permissions')
                candidate_identity = info.st_dev, info.st_ino
                probe = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                probe.settimeout(CONTROL_TIMEOUT)
                try:
                    probe.connect(str(self.spec.root / APP))
                    verify_peer(probe, self.spec.expected_exe, OwnedDaemonIdentity(self.child.pid, self.child_start))
                    unchanged_root(self.spec, self.fd)
                    current = os.stat(APP, dir_fd=self.fd, follow_symlinks=False)
                    if (current.st_dev, current.st_ino) != candidate_identity:
                        fail('path')
                    os.chmod(APP, 0o600, dir_fd=self.fd, follow_symlinks=False)
                    self.app_identity = socket_identity(self.fd, APP)
                finally:
                    probe.close()
                native_call(self.spec, OwnedDaemonIdentity(self.child.pid, self.child_start), lambda request: None)
                if socket_identity(self.fd, APP) != candidate_identity:
                    fail('path')
                self.app_identity = candidate_identity
                self.generation += 1; self.state = 'ready'; self.publish()
                return
            time.sleep(.02)
        fail('timeout')

    def failed_start(self, reason):
        self.state = 'failed'
        self.record['last_error'] = reason
        if self.child is not None:
            if self.child.poll() is None:
                try:
                    self.child.terminate(); self.child.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    self.publish(); return
            self.child.wait(); self.child = None; self.child_start = None
        if self.app_identity is not None:
            unlink_socket(self.fd, APP, self.app_identity); self.app_identity = None
        self.publish()

    def refresh(self):
        if self.child is None or self.child.poll() is None:
            return
        self.child.wait()
        if self.app_identity is not None:
            unlink_socket(self.fd, APP, self.app_identity)
            self.app_identity = None
        self.child = None; self.child_start = None; self.state = 'failed'; self.publish()
        if self.restarts >= len(RESTART_DELAYS):
            return
        delay = RESTART_DELAYS[self.restarts]; self.restarts += 1
        time.sleep(delay)
        try:
            self.spawn()
        except DaemonIdentityError as exc:
            self.failed_start(exc.reason)

    def idle(self):
        child = self.binding()
        def check(request):
            result = request('thread/loaded/list', {'limit': 4096})
            data = result.get('data')
            if (type(data) is not list or len(data) > 4096 or result.get('nextCursor') is not None
                    or any(type(x) is not str or not 1 <= len(x) <= 256 for x in data)):
                fail('protocol')
            for thread_id in data:
                result = request('thread/read', {'threadId': thread_id, 'includeTurns': False})
                thread = result.get('thread')
                if type(thread) is not dict or thread.get('id') != thread_id or thread.get('status', {}).get('type') != 'idle':
                    fail('busy')
            return True
        return native_call(self.spec, OwnedDaemonIdentity(child['pid'], child['starttime']), check)

    def stop_idle(self, human_disconnected):
        if human_disconnected is not True:
            fail('human_confirmation')
        guard = open_file(self.fd, 'admission.lock', create=True)
        try:
            try:
                fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                fail('busy')
            if self.child is not None and self.child.poll() is None:
                if not self.idle():
                    fail('busy')
                # Only the actual still-held Popen; never kill a registry PID.
                self.child.terminate()
                try:
                    self.child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    fail('busy')  # no blind SIGKILL / no fabricated clean stop
            write_json(self.fd, 'stopped.json', {'format': 1, 'boot': self.boot})
            self.state = 'stopped'
        finally:
            os.close(guard)

    def serve(self):
        listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        listener.bind(str(self.spec.root / CONTROL)); listener.listen(8); listener.settimeout(.1)
        os.chmod(CONTROL, 0o600, dir_fd=self.fd, follow_symlinks=False)
        self.control_identity = socket_identity(self.fd, CONTROL)
        try:
            try:
                self.spawn()
            except DaemonIdentityError as exc:
                self.failed_start(exc.reason)
            while self.state != 'stopped':
                self.refresh()
                try:
                    conn, _ = listener.accept()
                except socket.timeout:
                    continue
                with conn:
                    conn.settimeout(CONTROL_TIMEOUT)
                    request = {}
                    try:
                        raw = conn.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12)
                        import struct
                        _pid, uid, _gid = struct.unpack('iII', raw)
                        if uid != os.getuid():
                            fail('owner')
                        request = packet(conn)
                        if (set(request) != {'format', 'op', 'boot', 'challenge', 'human_disconnected'}
                                or type(request['format']) is not int or request['format'] != 1
                                or request['boot'] != self.boot or type(request['challenge']) is not str
                                or len(request['challenge']) != 32 or type(request['human_disconnected']) is not bool):
                            fail('protocol')
                        if request['op'] == 'stop':
                            self.stop_idle(request['human_disconnected'])
                            reply = {'ok': True, 'state': 'stopped'}
                        elif request['op'] == 'status':
                            if self.state != 'ready':
                                raise PrivateEndpointError(self.record.get('last_error', 'endpoint_child'))
                            reply = {'ok': True, 'state': self.state, 'child': self.binding(), 'generation': self.generation}
                        else:
                            fail('protocol')
                    except DaemonIdentityError as exc:
                        reply = {'ok': False, 'reason': exc.reason}
                    except Exception:
                        reply = {'ok': False, 'reason': 'endpoint_protocol'}
                    try:
                        send_packet(conn, {**reply, 'boot': self.boot, 'challenge': request.get('challenge')})
                    except OSError:
                        pass
        finally:
            listener.close()
            # On unexpected owner failure retain all locator evidence and never
            # pretend to have recovered/killed the possible orphan child.
            if self.state == 'stopped':
                if self.app_identity is not None:
                    unlink_socket(self.fd, APP, self.app_identity)
                unlink_socket(self.fd, CONTROL, self.control_identity)
                os.unlink(REGISTRY, dir_fd=self.fd); os.fsync(self.fd)


def run_owner(spec):
    os.umask(0o077)  # This entry is a dedicated process, never the helper thread.
    with root_fd(spec) as fd:
        lock = open_file(fd, 'owner.lock', create=True)
        try:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                return 73
            if any(exists(fd, n) for n in (REGISTRY, CONTROL, APP, 'stopped.json')):
                fail('dirty')
            Owner(spec, fd).serve()
            return 0
        finally:
            os.close(lock)


def main(argv=None):
    parser = argparse.ArgumentParser(description='显式选择的私有Codex端点；不发现或接管孤儿进程')
    parser.add_argument('--owner', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--state-dir'); parser.add_argument('--seat'); parser.add_argument('--codex-home')
    parser.add_argument('--workdir'); parser.add_argument('--command'); parser.add_argument('--expected-exe')
    parser.add_argument('--action', choices=('status', 'stop', 'resume'), default='status')
    parser.add_argument('--human-disconnected', action='store_true')
    parser.add_argument('--thread-id')
    args = parser.parse_args(argv)
    try:
        if args.owner:
            raw = sys.stdin.buffer.readline(LIMIT + 1)
            if len(raw) > LIMIT or not raw.endswith(b'\n'):
                fail('config')
            return run_owner(Spec(**json.loads(raw)))
        spec = Spec(args.state_dir, args.seat, args.codex_home, args.workdir, args.command, args.expected_exe)
        client = Client(spec)
        if args.action == 'resume':
            command = client.resume_command(args.thread_id)
            env = env_snapshot(); env['CODEX_HOME'] = spec.codex_home
            os.chdir(spec.workdir); os.execve(command[0], command, env)
        result = client.stop_owner(human_disconnected=args.human_disconnected) if args.action == 'stop' else client.status()
        print(json.dumps({'status': result['state'], 'generation': result.get('generation')}))
        return 0
    except Exception as exc:
        reason = exc.reason if isinstance(exc, DaemonIdentityError) else 'endpoint_config'
        print(json.dumps({'status': 'PRIVATE_ENDPOINT_REFUSED', 'reason': reason}))
        return 78


if __name__ == '__main__':
    raise SystemExit(main())
