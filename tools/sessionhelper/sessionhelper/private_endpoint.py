"""Opt-in, local helper-owned Codex endpoint. No discovery or orphan adoption.

A surviving owner holds the actual child Popen and flock. The registry is a
locator, never authority to adopt/kill a PID. Control responses require a live
kernel peer binding and a fresh challenge. No credential/environment persistence.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager, ExitStack
from dataclasses import asdict, dataclass
import hashlib
import importlib.util
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
        'endpoint_binding', 'endpoint_child', 'endpoint_human_confirmation', 'endpoint_retention',
    })


def fail(reason):
    raise PrivateEndpointError('endpoint_' + reason) from None


def implementation_hash():
    return hashlib.sha256(Path(__file__).read_bytes()).hexdigest()


def retention_kernel(spec):
    path = Path(env_get('SH_UPDATE_LAUNCHER', ''))
    try:
        if not path.is_absolute() or path.resolve(strict=True) != path or path.is_symlink():
            fail('retention')
        info = path.stat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid not in (0, os.getuid()) or info.st_mode & 0o022:
            fail('retention')
        loader = importlib.util.spec_from_file_location('_dw_private_retention', path)
        module = importlib.util.module_from_spec(loader); loader.loader.exec_module(module)
        if getattr(module, 'RETENTION_API', None) != 1:
            fail('retention')
        return module, module.Retention(Path(spec.state_dir) / 'update')
    except PrivateEndpointError:
        raise
    except Exception:
        fail('retention')


def package_root():
    return str(Path(__file__).resolve().parent.parent)


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


def thread_identifier(value):
    try:
        if type(value) is not str or str(uuid.UUID(value)) != value:
            fail('binding')
        return value
    except (ValueError, AttributeError):
        fail('binding')


@dataclass(frozen=True)
class AppRef:
    """An authenticated live-owner descriptor, never a configurable path."""
    kind: str
    alias: tuple
    target: tuple
    directory: tuple | None

    def to_dict(self):
        return asdict(self)

    @classmethod
    def from_dict(cls, value):
        if type(value) is not dict or set(value) != {'kind', 'alias', 'target', 'directory'}:
            fail('binding')
        def pair(value):
            if (type(value) not in (list, tuple) or len(value) != 2
                    or type(value[0]) is not int or value[0] < 0 or not positive(value[1])):
                fail('binding')
            return tuple(value)
        if value['kind'] not in ('native_alias', 'direct'):
            fail('binding')
        directory = pair(value['directory']) if value['kind'] == 'native_alias' else None
        if value['kind'] == 'direct' and value['directory'] is not None:
            fail('binding')
        return cls(value['kind'], pair(value['alias']), pair(value['target']), directory)


def inode(info):
    return info.st_dev, info.st_ino


def native_location(spec):
    # Locked native Codex contract: canonical parent + un-resolved filename.
    # It explicitly ignores HOME, TMPDIR and CODEX_HOME. No configurable root.
    requested = spec.root.resolve(strict=True) / APP
    parent = Path('/tmp').resolve(strict=True) / ('codex-daemon-' + str(os.getuid()))
    name = hashlib.sha256(os.fsencode(requested)).hexdigest()
    return parent, name


def native_parent(fd, parent):
    info = os.fstat(fd)
    current = os.stat(parent, follow_symlinks=False)
    for item in (info, current):
        if (not stat.S_ISDIR(item.st_mode) or item.st_uid != os.getuid()
                or stat.S_IMODE(item.st_mode) != 0o700):
            fail('permissions')
    if inode(current) != inode(info):
        fail('path')
    return inode(info)


def app_socket(spec, binding, *, endpoint=None, allow_new_alias=False, retain_anchors=None):
    """Resolve only the self-spawned native alias, then connect its fixed target.

    readlink occurs once at owner startup. Later connections use the live
    owner's immutable inode descriptor and recompute the fixed target; they
    never follow/read the alias again. Anchors prevent inode reuse mid-check.
    """
    sock = None
    retained_start = len(retain_anchors) if retain_anchors is not None else 0
    try:
        with ExitStack() as stack:
            fd = stack.enter_context(root_fd(spec))
            anchor = os.open(APP, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            stack.callback(os.close, anchor)
            alias = os.fstat(anchor)
            if alias.st_uid != os.getuid():
                fail('permissions')
            target_fd = None; parent_fd = None
            if stat.S_ISLNK(alias.st_mode):
                if endpoint is None and not allow_new_alias:
                    fail('binding')
                parent, name = native_location(spec)
                destination = parent / name
                if endpoint is None:
                    value = os.readlink(APP, dir_fd=fd)  # exactly once; no recursion
                    if not os.path.isabs(value) or os.fsencode(value) != os.fsencode(destination):
                        fail('path')
                parent_fd = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                stack.callback(os.close, parent_fd)
                directory = native_parent(parent_fd, parent)
                target_fd = os.open(name, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent_fd)
                stack.callback(os.close, target_fd)
                target = os.fstat(target_fd)
                if (not stat.S_ISSOCK(target.st_mode) or target.st_uid != os.getuid()
                        or stat.S_IMODE(target.st_mode) != 0o600):
                    fail('permissions')
                current = AppRef('native_alias', inode(alias), inode(target), directory)
            elif stat.S_ISSOCK(alias.st_mode) and stat.S_IMODE(alias.st_mode) == 0o600:
                destination = spec.root / APP
                current = AppRef('direct', inode(alias), inode(alias), None)
            else:
                fail('permissions')
            if endpoint is not None and current != endpoint:
                fail('path')
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.settimeout(CONTROL_TIMEOUT)
            sock.connect(str(destination))  # Never connect via a native alias.
            verify_peer(sock, spec.expected_exe, binding)
            unchanged_root(spec, fd)
            if inode(os.stat(APP, dir_fd=fd, follow_symlinks=False)) != inode(alias):
                fail('path')
            if parent_fd is not None:
                if native_parent(parent_fd, parent) != current.directory:
                    fail('path')
                if socket_identity(parent_fd, name) != current.target:
                    fail('path')
            elif socket_identity(fd, APP) != current.target:
                fail('path')
            if retain_anchors is not None:
                # Keep old filesystem inodes allocated for the child's whole
                # lifetime; a recreated path cannot recycle the cached inode.
                for anchor_fd in (anchor, target_fd, parent_fd):
                    if anchor_fd is not None:
                        retain_anchors.append(os.dup(anchor_fd))
            return sock, current
    except DaemonIdentityError:
        if sock is not None:
            sock.close()
        if retain_anchors is not None:
            while len(retain_anchors) > retained_start:
                os.close(retain_anchors.pop())
        raise
    except (OSError, ValueError, RuntimeError):
        if sock is not None:
            sock.close()
        if retain_anchors is not None:
            while len(retain_anchors) > retained_start:
                os.close(retain_anchors.pop())
        fail('path')


def cleanup_app(spec, fd, endpoint):
    """Only the recorded own alias and exact own target inode; no directory scan."""
    if exists(fd, APP):
        info = os.stat(APP, dir_fd=fd, follow_symlinks=False)
        if info.st_uid != os.getuid() or inode(info) != endpoint.alias:
            fail('path')
        os.unlink(APP, dir_fd=fd)
    if endpoint.kind == 'native_alias':
        parent, name = native_location(spec)
        try:
            target_fd = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        except FileNotFoundError:
            return
        try:
            if native_parent(target_fd, parent) != endpoint.directory:
                fail('path')
            if exists(target_fd, name):
                if socket_identity(target_fd, name) != endpoint.target:
                    fail('path')
                os.unlink(name, dir_fd=target_fd)
        finally:
            os.close(target_fd)


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


def native_call(spec, binding, operation, *, endpoint=None):
    """Bounded readonly native calls after same-FD owned verification."""
    from websockets.sync.client import unix_connect
    sock = None
    try:
        sock, _ = app_socket(spec, binding, endpoint=endpoint)
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
        if sock is not None:
            sock.close()


class Client:
    def __init__(self, spec):
        self.spec = spec
        self._launchers = []
        self._writer = None

    def take_writer(self):
        """One helper writer for this endpoint, held until helper stop/crash."""
        if self._writer is not None:
            return
        with root_fd(self.spec) as fd:
            writer = open_file(fd, 'writer.lock', create=True)
            try:
                fcntl.flock(writer, fcntl.LOCK_EX | fcntl.LOCK_NB)
                unchanged_root(self.spec, fd)
            except BlockingIOError:
                os.close(writer)
                fail('busy')
            except Exception:
                os.close(writer)
                raise
            self._writer = writer

    def release_writer(self):
        if self._writer is not None:
            os.close(self._writer); self._writer = None

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
                    if record.get('pin'):
                        if reply.get('pin') != record['pin'] or reply.get('pin_nonce') != record.get('pin_nonce'):
                            fail('retention')
                        deadline = time.monotonic() + CONTROL_TIMEOUT
                        while lock_is_held(fd):
                            if time.monotonic() >= deadline: fail('retention')
                            time.sleep(.01)
                        module, retention = retention_kernel(self.spec)
                        try:
                            retention.finish(record['pin'], record['pin_nonce'],
                                {k: reply[k] for k in ('pin','boot','challenge','state')})
                        except Exception: fail('retention')
                    return reply
                child = reply.get('child')
                if (reply.get('state') != 'ready' or type(child) is not dict
                        or not positive(child.get('pid')) or not positive(child.get('starttime'))
                        or not positive(reply.get('generation'))):
                    fail('child')
                AppRef.from_dict(reply.get('endpoint'))
                return reply
            except (OSError, ValueError, UnicodeError):
                fail('unavailable')
            finally:
                sock.close()

    def status(self):
        return self._query()

    def open_app(self, live):
        fresh = self.status()
        if any(fresh.get(key) != live.get(key) for key in ('boot', 'child', 'generation', 'endpoint')):
            fail('binding')
        binding = OwnedDaemonIdentity(**fresh['child'])
        sock, _ = app_socket(self.spec, binding, endpoint=AppRef.from_dict(fresh['endpoint']))
        return sock

    def native(self, operation):
        live = self.status()
        return native_call(self.spec, OwnedDaemonIdentity(**live['child']), operation,
                           endpoint=AppRef.from_dict(live['endpoint']))

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
                    module, retention = retention_kernel(self.spec)
                    try: pin, handles = retention.register(package_root(), self.spec.fingerprint())
                    except Exception: fail('retention')
                    try:
                        env = env_snapshot()
                        env['CODEX_HOME'] = self.spec.codex_home
                        env['PYTHONPATH'] = package_root()
                        proc = subprocess.Popen([sys.executable, '-B', '-m', 'sessionhelper.private_endpoint', '--owner'],
                            stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                            env=env, cwd=self.spec.workdir, start_new_session=True, close_fds=True,
                            pass_fds=tuple(handles))
                        self._launchers.append(proc)
                        request = {'spec': asdict(self.spec), 'retention': {'id': pin['id'], 'fds': handles}}
                        try:
                            proc.stdin.write(json.dumps(request).encode() + b'\n'); proc.stdin.close()
                        except (BrokenPipeError, OSError): pass
                    finally:
                        # The owner must retain the inherited descriptions. A
                        # failed handoff leaves its durable pin, never frees it.
                        for handle in handles: os.close(handle)
                last = None
                while time.monotonic() < deadline:
                    try:
                        live = self.status()
                        module, retention = retention_kernel(self.spec)
                        try: retention.active(live.get('pin'), package_root(), self.spec.fingerprint())
                        except Exception: fail('retention')
                        return live
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
        if self._writer is None:
            fail('binding')
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

    def bound_thread(self, *, require_writer=True):
        """Hint for current binding only; the driver must still read/resume it.

        Never derive a binding from a historical delivery or a loaded-list.
        """
        if require_writer and self._writer is None:
            fail('binding')
        current = self.status()
        with root_fd(self.spec) as fd:
            if not exists(fd, 'binding.json'):
                return ''
            value = read_json(fd, 'binding.json')
            if (type(value.get('format')) is not int or value['format'] != 1
                    or value.get('config') != self.spec.fingerprint() or value.get('boot') != current['boot']):
                fail('binding')
            return thread_identifier(value.get('thread_id'))

    def save_thread(self, thread_id):
        thread_identifier(thread_id)
        previous = self.bound_thread()
        if previous and previous != thread_id:
            fail('binding')
        current = self.status()
        with root_fd(self.spec) as fd:
            write_json(fd, 'binding.json', {'format': 1, 'config': self.spec.fingerprint(),
                                         'boot': current['boot'], 'thread_id': thread_id})

    def resume_command(self, thread_id):
        try:
            if type(thread_id) is not str or str(uuid.UUID(thread_id)) != thread_id:
                fail('config')
        except (ValueError, AttributeError):
            fail('config')
        if self.bound_thread(require_writer=False) != thread_id:
            fail('binding')
        live = self.status()
        endpoint = AppRef.from_dict(live['endpoint'])
        native_call(self.spec, OwnedDaemonIdentity(**live['child']), lambda request: None, endpoint=endpoint)
        target = native_location(self.spec)[0] / native_location(self.spec)[1] if endpoint.kind == 'native_alias' else self.spec.root / APP
        return [self.spec.command, '--remote', 'unix://' + str(target), 'resume', thread_id]


class Owner:
    def __init__(self, spec, fd, pin=None):
        self.spec = spec; self.fd = fd
        self.pin = pin
        self.boot = secrets.token_hex(16); self.child = None; self.child_start = None
        self.generation = 0; self.restarts = 0; self.state = 'starting'
        self.app_identity = None; self.control_identity = None
        self.app_anchors = []
        self.record = {'format': 1, 'owner_pid': os.getpid(), 'owner_starttime': process_starttime(os.getpid()),
                       'python': executable(sys.executable)[0], 'config': spec.fingerprint(),
                       'implementation': implementation_hash(), 'boot': self.boot}
        if pin is not None:
            self.record.update(pin=pin['id'], pin_nonce=pin['nonce'])

    def publish(self):
        write_json(self.fd, REGISTRY, {**self.record, 'state': self.state, 'generation': self.generation,
                                      'child': self.binding() if self.state == 'ready' else None,
                                      'endpoint': self.app_identity.to_dict() if self.app_identity is not None else None})

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
                binding = OwnedDaemonIdentity(self.child.pid, self.child_start)
                probe, endpoint = app_socket(self.spec, binding, allow_new_alias=True,
                                            retain_anchors=self.app_anchors)
                probe.close()
                self.app_identity = endpoint
                native_call(self.spec, binding, lambda request: None, endpoint=endpoint)
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
            cleanup_app(self.spec, self.fd, self.app_identity); self.app_identity = None
            self.close_app_anchors()
        self.publish()

    def close_app_anchors(self):
        while self.app_anchors:
            os.close(self.app_anchors.pop())

    def refresh(self):
        if self.child is None or self.child.poll() is None:
            return
        self.child.wait()
        if self.app_identity is not None:
            try:
                cleanup_app(self.spec, self.fd, self.app_identity)
            except DaemonIdentityError as exc:
                self.child = None; self.child_start = None; self.state = 'failed'
                self.record['last_error'] = exc.reason; self.publish()
                return
            self.app_identity = None
            self.close_app_anchors()
        self.child = None; self.child_start = None; self.state = 'failed'; self.publish()
        if self.record.get('stop_requested') is True:
            return
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
        return native_call(self.spec, OwnedDaemonIdentity(child['pid'], child['starttime']), check,
                           endpoint=self.app_identity)

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
                self.record['stop_requested'] = True
                self.state = 'stopping'
                # Only the actual still-held Popen; never kill a registry PID.
                self.child.terminate()
                try:
                    self.child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    fail('busy')  # no blind SIGKILL / no fabricated clean stop
            if self.app_identity is not None:
                try:
                    cleanup_app(self.spec, self.fd, self.app_identity)
                except DaemonIdentityError as exc:
                    self.child = None; self.child_start = None; self.state = 'failed'
                    self.record['last_error'] = exc.reason; self.publish()
                    raise
                self.app_identity = None; self.close_app_anchors()
            write_json(self.fd, 'stopped.json', {'format': 1, 'boot': self.boot})
            self.state = 'stopped'
        finally:
            os.close(guard)

    def serve(self):
        listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        listener.bind(str(self.spec.root / CONTROL)); listener.listen(8); listener.settimeout(.1)
        os.chmod(CONTROL, 0o600, dir_fd=self.fd, follow_symlinks=False)
        self.control_identity = socket_identity(self.fd, CONTROL)
        control_anchor = os.open(CONTROL, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=self.fd)
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
                            reply = {'ok': True, 'state': 'stopped',
                                     'pin': self.record.get('pin'), 'pin_nonce': self.record.get('pin_nonce')}
                        elif request['op'] == 'status':
                            if self.state != 'ready':
                                raise PrivateEndpointError(self.record.get('last_error', 'endpoint_child'))
                            reply = {'ok': True, 'state': self.state, 'child': self.binding(), 'generation': self.generation,
                                     'endpoint': self.app_identity.to_dict(), 'pin': self.record.get('pin')}
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
            try:
                if self.state == 'stopped':
                    if self.app_identity is not None:
                        cleanup_app(self.spec, self.fd, self.app_identity)
                    unlink_socket(self.fd, CONTROL, self.control_identity)
                    os.unlink(REGISTRY, dir_fd=self.fd); os.fsync(self.fd)
            finally:
                os.close(control_anchor); self.close_app_anchors()


def run_owner(spec, pin=None, handles=None):
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
            Owner(spec, fd, pin).serve()
            return 0
        finally:
            # Release inherited leases before owner.lock so an authenticated
            # stop observer cannot certify while a dying owner still holds them.
            if handles is not None:
                while handles: os.close(handles.pop())
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
            value = json.loads(raw)
            spec = Spec(**value['spec']); meta = value['retention']; handles = meta['fds']
            if (type(handles) is not list or len(handles) != 3
                    or any(type(f) is not int or f < 3 for f in handles) or len(set(handles)) != len(handles)):
                fail('retention')
            try:
                module, retention = retention_kernel(spec)
                try: pin = retention.accept(meta['id'], handles, package_root(), spec.fingerprint())
                except Exception: fail('retention')
                return run_owner(spec, pin, handles)
            finally:
                for handle in handles: os.close(handle)
        spec = Spec(args.state_dir, args.seat, args.codex_home, args.workdir, args.command, args.expected_exe)
        client = Client(spec)
        if args.action == 'resume':
            module, retention = retention_kernel(spec)
            try: retention.active(client.status().get('pin'), package_root(), spec.fingerprint())
            except Exception: fail('retention')
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
