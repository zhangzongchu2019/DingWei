"""Stable stdlib-only supervisor. This file lives outside replaceable code slots."""
from __future__ import annotations

import fcntl
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

MAX_STARTS = 2
HEALTH_SECONDS = 120
TUPLE = ('job_id', 'release_id', 'agent_id', 'owner_id', 'version', 'sha256')


class UpdateError(RuntimeError):
    """A fixed, non-sensitive failure code."""


def atomic_json(path: Path, value: dict) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    # Random exclusive file prevents another process replacing a predictable temp.
    import tempfile
    fd, name = tempfile.mkstemp(prefix='.journal-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as out:
            json.dump(value, out, sort_keys=True)
            out.flush()
            os.fsync(out.fileno())
        os.replace(name, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(name):
            os.unlink(name)


# Retention is part of this immutable stdlib launcher, not a candidate module.
RETENTION_API = 1


def package_fingerprint(root):
    import hashlib
    digest = hashlib.sha256(); total = 0; count = 0
    root = Path(root)
    if root.is_symlink() or root.resolve() != root or not root.is_dir():
        raise UpdateError('retain_invalid')
    for directory, dirs, files in os.walk(root / 'sessionhelper', followlinks=False):
        dirs[:] = sorted(d for d in dirs if d != '__pycache__')
        if any((Path(directory) / d).is_symlink() for d in dirs):
            raise UpdateError('retain_invalid')
        for name in sorted(files):
            if not name.endswith('.py'):
                continue
            path = Path(directory) / name
            if path.is_symlink() or not path.is_file():
                raise UpdateError('retain_invalid')
            raw = path.read_bytes(); total += len(raw); count += 1
            if total > 32 * 1024 * 1024 or count > 2048:
                raise UpdateError('retain_limit')
            digest.update(str(path.relative_to(root)).encode() + b'\0' + raw + b'\0')
    if not count:
        raise UpdateError('retain_invalid')
    return digest.hexdigest()


def _retain_identity(path):
    import stat
    info = Path(path).stat()
    if info.st_uid not in (0, os.getuid()) or info.st_mode & 0o022:
        raise UpdateError('retain_invalid')
    return [info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns]


def runtime_fingerprint():
    import hashlib
    lock = Path(__file__).resolve().parent / 'requirements-runtime.lock'
    if Path(__file__).name == 'guard.py' and lock.parent.name == 'update_launcher':
        lock = lock.parent.parent / lock.name  # source-tree isolated acceptance
    if lock.is_symlink() or not lock.is_file():
        raise UpdateError('retain_runtime')
    paths = [Path(sys.executable).absolute(), Path(sys.prefix), Path(sys.base_prefix)]
    return {'paths': [str(p) for p in paths],
            'identities': [_retain_identity(p) for p in paths],
            'exe': str(paths[0].resolve(strict=True)),
            'lock': hashlib.sha256(lock.read_bytes()).hexdigest(), 'version': sys.version}


class Retention:
    """Durable pins survive missing processes; only authenticated stop releases one.

    Registry/PID fields never authorize reclamation. A lock becoming free is
    insufficient: an orphan native child can still be alive after owner death.
    """
    LIMIT = 128

    def __init__(self, root):
        self.root = Path(root)
        if not self.root.is_absolute() or self.root.resolve() != self.root:
            raise UpdateError('retain_invalid')
        self.path = self.root / 'retain'

    @staticmethod
    def _file(fd, name, *, create=False, exclusive=False):
        import stat
        f = os.open(name, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC |
                    (os.O_CREAT if create else 0) | (os.O_EXCL if exclusive else 0), 0o600, dir_fd=fd)
        try:
            info = os.fstat(f)
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                    or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
                raise UpdateError('retain_invalid')
            return f
        except Exception:
            os.close(f); raise

    @staticmethod
    def _read(fd, name):
        f = Retention._file(fd, name)
        try:
            raw = os.read(f, 1024 * 1024 + 1)
            if len(raw) > 1024 * 1024:
                raise UpdateError('retain_invalid')
            value = json.loads(raw)
            if type(value) is not dict:
                raise UpdateError('retain_invalid')
            return value
        except (ValueError, TypeError):
            raise UpdateError('retain_invalid') from None
        finally:
            os.close(f)

    @staticmethod
    def _write(fd, name, value):
        import secrets
        temp = '.pin-' + secrets.token_hex(16)
        out = Retention._file(fd, temp, create=True, exclusive=True)
        try:
            with os.fdopen(out, 'w', closefd=False) as stream:
                json.dump(value, stream, sort_keys=True); stream.flush(); os.fsync(out)
            os.replace(temp, name, src_dir_fd=fd, dst_dir_fd=fd); os.fsync(fd)
        finally:
            os.close(out)
            try: os.unlink(temp, dir_fd=fd)
            except FileNotFoundError: pass

    def transaction(self, *, create=False, if_present=False):
        from contextlib import contextmanager
        import stat
        @contextmanager
        def locked():
            # Legacy guard calls must not create or validate retention resources.
            # Registration alone creates them; a later state-lock recheck closes
            # a concurrent first registration without reversing the lock order.
            if if_present and not os.path.lexists(self.path):
                yield None
                return
            # Walk all parents without following links. Sticky root /tmp is
            # permitted; the private update and retain leaves must be ours 0700.
            current = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            lock = directory = None
            try:
                for index, part in enumerate(self.root.parts[1:], 1):
                    if create:
                        try: os.mkdir(part, 0o700, dir_fd=current)
                        except FileExistsError: pass
                    child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=current)
                    os.close(current); current = child
                    info = os.fstat(current)
                    sticky = info.st_uid == 0 and bool(info.st_mode & stat.S_ISVTX)
                    if info.st_uid not in (0, os.getuid()) or (info.st_mode & 0o022 and not sticky):
                        raise UpdateError('retain_invalid')
                info = os.fstat(current)
                if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
                    raise UpdateError('retain_invalid')
                lock = self._file(current, 'retain.txn.lock', create=True)
                deadline = time.monotonic() + 3
                while True:
                    try:
                        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        break
                    except BlockingIOError:
                        if time.monotonic() >= deadline: raise UpdateError('retain_busy')
                        time.sleep(.01)
                if create:
                    try: os.mkdir('retain', 0o700, dir_fd=current)
                    except FileExistsError: pass
                directory = os.open('retain', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=current)
                info = os.fstat(directory)
                if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
                    raise UpdateError('retain_invalid')
                yield directory
                after = os.stat('retain', dir_fd=current, follow_symlinks=False)
                if (after.st_dev, after.st_ino) != (info.st_dev, info.st_ino):
                    raise UpdateError('retain_invalid')
            except OSError:
                raise UpdateError('retain_invalid') from None
            finally:
                for f in (directory, lock, current):
                    if f is not None: os.close(f)
        return locked()

    @staticmethod
    def valid_stop(pin):
        import re
        receipt = pin.get('stop')
        if type(receipt) is not dict:
            return False
        if (receipt.get('pin') != pin['id'] or receipt.get('nonce') != pin['nonce']
                or receipt.get('state') != 'stopped'
                or any(type(receipt.get(k)) is not str or not re.fullmatch('[0-9a-f]{32}', receipt[k])
                       for k in ('boot','challenge'))):
            return False
        return True

    def records(self, fd):
        import re
        names = [n for n in os.listdir(fd) if n.endswith('.json')]
        if len(names) > self.LIMIT:
            raise UpdateError('retain_limit')
        result = []
        for name in sorted(names):
            if not re.fullmatch(r'pin-[0-9a-f]{32}\.json', name):
                raise UpdateError('retain_invalid')
            pin = self._read(fd, name)
            import hashlib
            package = pin.get('package'); runtime = pin.get('runtime'); keys = pin.get('locks')
            expected = (['pin-' + pin.get('id',''),
                'package-' + hashlib.sha256(package.encode()).hexdigest(),
                'runtime-' + hashlib.sha256(json.dumps(runtime, sort_keys=True).encode()).hexdigest()]
                if type(package) is str and type(runtime) is dict and type(pin.get('id')) is str else None)
            if (type(pin.get('format')) is not int or pin['format'] != 1 or pin.get('id') != name[4:-5]
                    or pin.get('state') not in ('active', 'unknown', 'stopped', 'reclaimed')
                    or expected is None or keys != expected or not package or len(package) > 4096 or '\0' in package
                    or not Path(package).is_absolute()
                    or '..' in Path(package).parts
                    or type(pin.get('digest')) is not str or not re.fullmatch('[0-9a-f]{64}', pin['digest'])
                    or type(pin.get('nonce')) is not str or not re.fullmatch('[0-9a-f]{32}', pin['nonce'])
                    or type(pin.get('endpoint')) is not str
                    or type(pin.get('package_identity')) is not list or len(pin['package_identity']) != 2
                    or any(type(n) is not int or n < 0 for n in pin['package_identity'])):
                raise UpdateError('retain_invalid')
            if pin['state'] in ('stopped','reclaimed') and not self.valid_stop(pin):
                raise UpdateError('retain_stop')
            result.append(pin)
        return result

    def register(self, package, endpoint):
        import hashlib, secrets
        package = Path(package).resolve(strict=True)
        with self.transaction(create=True) as fd:
            records = self.records(fd)
            self.check_locked(fd, package)
            state = State(self.root, package)
            with state.locked():
                pending = state.read().get('pending')
                if pending and package_fingerprint(state.slot(pending)) != package_fingerprint(package):
                    raise UpdateError('manual_required')
            if len(records) >= self.LIMIT:
                raise UpdateError('retain_limit')
            ident = secrets.token_hex(16)
            pin = {'format': 1, 'id': ident, 'state': 'active', 'package': str(package),
                   'package_identity': _retain_identity(package)[:2],
                   'digest': package_fingerprint(package), 'runtime': runtime_fingerprint(),
                   'endpoint': endpoint, 'nonce': secrets.token_hex(16)}
            keys = ['pin-' + ident, 'package-' + hashlib.sha256(str(package).encode()).hexdigest(),
                    'runtime-' + hashlib.sha256(json.dumps(pin['runtime'], sort_keys=True).encode()).hexdigest()]
            pin['locks'] = keys; handles = []
            try:
                for key in keys:
                    f = self._file(fd, key + '.lock', create=True); handles.append(f)
                    fcntl.flock(f, fcntl.LOCK_SH | fcntl.LOCK_NB)
                self._write(fd, 'pin-' + ident + '.json', pin)
                return pin, handles
            except Exception:
                for f in handles: os.close(f)
                raise

    def accept(self, ident, handles, package, endpoint):
        with self.transaction() as fd:
            pin = next((p for p in self.records(fd) if p['id'] == ident), None)
            if (not pin or pin['state'] != 'active' or pin['package'] != str(Path(package).resolve())
                    or pin['endpoint'] != endpoint or pin['digest'] != package_fingerprint(package)
                    or pin['runtime'] != runtime_fingerprint() or len(handles) != len(pin['locks'])):
                raise UpdateError('retain_invalid')
            for key, handle in zip(pin['locks'], handles):
                st = os.stat(key + '.lock', dir_fd=fd, follow_symlinks=False); actual = os.fstat(handle)
                if (st.st_dev, st.st_ino) != (actual.st_dev, actual.st_ino):
                    raise UpdateError('retain_invalid')
                fcntl.flock(handle, fcntl.LOCK_SH | fcntl.LOCK_NB)
                probe = self._file(fd, key + '.lock')
                try:
                    try: fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    except BlockingIOError: pass
                    else: raise UpdateError('retain_invalid')
                finally: os.close(probe)
            return pin

    def active(self, ident, package, endpoint):
        with self.transaction() as fd:
            pin = next((p for p in self.records(fd) if p['id'] == ident), None)
            if (not pin or pin['state'] != 'active' or pin['endpoint'] != endpoint
                    or pin['package'] != str(Path(package).resolve())
                    or pin['digest'] != package_fingerprint(package)
                    or pin['runtime'] != runtime_fingerprint()):
                raise UpdateError('manual_required')
            return pin

    def check_locked(self, fd, candidate=None, *, replacing=None):
        if fd is None:
            if os.path.lexists(self.path):
                raise UpdateError('retain_busy')
            return []
        records = self.records(fd)
        if replacing is not None and any(p['state'] != 'reclaimed' and p['package'] == str(Path(replacing)) for p in records):
            raise UpdateError('retain_pinned')
        active = [p for p in records if p['state'] in ('active', 'unknown')]
        for pin in active:
            if (_retain_identity(pin['package'])[:2] != pin['package_identity']
                    or package_fingerprint(pin['package']) != pin['digest']):
                raise UpdateError('manual_required')
            if (candidate is not None and
                    (package_fingerprint(candidate) != pin['digest'] or runtime_fingerprint() != pin['runtime'])):
                raise UpdateError('manual_required')
        return active

    def check(self, candidate):
        if not os.path.lexists(self.path):
            return
        with self.transaction() as fd:
            self.check_locked(fd, candidate)

    def finish(self, ident, nonce, proof):
        import re
        # Called only after same-FD authenticated stop response and owner-lock
        # release. No success from a locator, a timeout, or a dead PID alone.
        with self.transaction() as fd:
            pin = next((p for p in self.records(fd) if p['id'] == ident), None)
            if (not pin or pin['state'] != 'active' or pin['nonce'] != nonce
                    or type(proof) is not dict or proof.get('pin') != ident
                    or type(proof.get('challenge')) is not str or not re.fullmatch('[0-9a-f]{32}', proof['challenge'])
                    or type(proof.get('boot')) is not str or not re.fullmatch('[0-9a-f]{32}', proof['boot'])
                    or proof.get('state') != 'stopped'):
                raise UpdateError('retain_stop')
            probe = self._file(fd, pin['locks'][0] + '.lock')
            try:
                try: fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError: raise UpdateError('retain_busy') from None
                pin.update(state='stopped', stop={**proof, 'nonce': nonce})
                self._write(fd, 'pin-' + ident + '.json', pin)
            finally: os.close(probe)

    def reclaim(self, ident, state):
        import re, shutil
        if not re.fullmatch('[0-9a-f]{32}', ident or ''):
            raise UpdateError('retain_invalid')
        with self.transaction() as fd:
            # Global order: retain transaction -> guard state. Never wait for
            # an owner while holding either lock.
            with state.locked():
                records = self.records(fd); pin = next((p for p in records if p['id'] == ident), None)
                if not pin or pin['state'] != 'stopped' or type(pin.get('stop')) is not dict:
                    raise UpdateError('retain_stop')
                if not self.valid_stop(pin):
                    raise UpdateError('retain_stop')
                path = Path(pin['package']); sha = path.name
                if (path.parent != self.root / 'slots' or not re.fullmatch('[0-9a-f]{64}', sha)
                        or path == state.factory or path.is_symlink()
                        or _retain_identity(path)[:2] != pin['package_identity']):
                    raise UpdateError('retain_invalid')
                data = state.read()
                if any(data.get(k) and data[k].get('sha256') == sha for k in ('current','previous','pending')):
                    raise UpdateError('retain_pinned')
                if any(p['id'] != ident and p['state'] in ('active','unknown') and p['package'] == str(path) for p in records):
                    raise UpdateError('retain_pinned')
                lock = self._file(fd, pin['locks'][1] + '.lock')
                try:
                    try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    except BlockingIOError: raise UpdateError('retain_busy') from None
                    parent = os.open(path.parent, os.O_DIRECTORY | os.O_NOFOLLOW | os.O_RDONLY)
                    try:
                        before = os.stat(sha, dir_fd=parent, follow_symlinks=False)
                        if [before.st_dev,before.st_ino] != pin['package_identity']:
                            raise UpdateError('retain_invalid')
                        shutil.rmtree(sha, dir_fd=parent)
                    finally: os.close(parent)
                    pin['state'] = 'reclaimed'; self._write(fd, 'pin-' + ident + '.json', pin)
                finally: os.close(lock)
        return {'state': 'reclaimed', 'pin': ident}


class State:
    def __init__(self, root: Path, factory: Path):
        self.root, self.factory = Path(root), Path(factory).resolve()
        if not self.root.is_absolute() or self.root.is_symlink():
            raise UpdateError('state_invalid')
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.root = self.root.resolve()  # a symlinked HOME parent must match the preflight's real path
        if self.root.stat().st_uid != os.getuid():
            raise UpdateError('state_owner_invalid')
        if self.root.stat().st_mode & 0o077:
            raise UpdateError('state_permissions_invalid')
        self.path = self.root / 'journal.json'

    def read(self):
        if not self.path.exists():
            return {'format': 1, 'current': None, 'previous': None, 'pending': None, 'terminal': {}}
        try:
            data = json.loads(self.path.read_text())
            if data.get('format') != 1 or not isinstance(data.get('terminal'), dict):
                raise ValueError()
            return data
        except (ValueError, OSError, TypeError):
            raise UpdateError('journal_invalid') from None

    def locked(self):
        """Cross-process transaction lock shared by helper and guard."""
        from contextlib import contextmanager
        @contextmanager
        def transaction():
            fd = os.open(self.root / 'state.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
            try:
                fcntl.flock(fd, fcntl.LOCK_EX)
                yield
            finally:
                os.close(fd)
        return transaction()

    def slot(self, item):
        sha = item.get('sha256', '') if isinstance(item, dict) else ''
        if len(sha) != 64 or any(c not in '0123456789abcdef' for c in sha):
            raise UpdateError('slot_invalid')
        path = self.root / 'slots' / sha
        # Never follow named slots/payloads outside the private slot root.
        if path.is_symlink() or path.resolve() != path or not (path / 'sessionhelper' / '__main__.py').is_file():
            raise UpdateError('slot_invalid')
        return path

    def arm(self, item, now=None):
        now = time.time() if now is None else now
        if any(not isinstance(item.get(k), str) or not item[k] for k in TUPLE):
            raise UpdateError('offer_invalid')
        self.slot(item)
        retention = Retention(self.root)
        with retention.transaction(if_present=True) as retain_fd:
            with self.locked():
                retention.check_locked(retain_fd, self.slot(item))
                data = self.read()
                if item['job_id'] in data['terminal']:
                    terminal = data['terminal'][item['job_id']]
                    if not all(terminal.get(k) == item[k] for k in TUPLE):
                        raise UpdateError('offer_mismatch')
                    return terminal['stage']
                if data['pending']:
                    if all(data['pending'].get(k) == item[k] for k in TUPLE):
                        return 'pending'
                    raise UpdateError('update_busy')
                data['pending'] = {k: item[k] for k in TUPLE}
                data['pending'].update(starts=0, deadline=now + HEALTH_SECONDS, from_version=item.get('from_version', ''))
                atomic_json(self.path, data)
                return 'pending'

    def _rollback(self, data, reason):
        pending = data['pending']
        data['terminal'][pending['job_id']] = {**{k: pending[k] for k in TUPLE}, 'stage': 'rolled_back', 'from_version': pending.get('from_version', '')}
        data['last_result'] = {**{k: pending[k] for k in TUPLE}, 'stage': 'rolled_back', 'reason': reason, 'from_version': pending.get('from_version', '')}
        data['pending'] = None
        atomic_json(self.path, data)

    def select(self, trial=True, now=None):
        now = time.time() if now is None else now
        retention = Retention(self.root)
        with retention.transaction(if_present=True) as retain_fd:
            with self.locked():
                data = self.read()
                pending = data['pending']
                if not trial and pending and pending['starts'] > 0 and now < pending['deadline']:
                    try:
                        retention.check_locked(retain_fd, self.slot(pending))
                        return self.slot(pending), None
                    except UpdateError as exc:
                        if str(exc).startswith(('retain_', 'manual_required')): raise
                        pass
                if trial and pending:
                    if pending['starts'] >= MAX_STARTS or now >= pending['deadline']:
                        self._rollback(data, 'health_timeout')
                    else:
                        try:
                            path = self.slot(pending)
                        except UpdateError:
                            self._rollback(data, 'slot_invalid')
                        else:
                            retention.check_locked(retain_fd, path)
                            pending['starts'] += 1
                            atomic_json(self.path, data)  # durable budget before child exec
                            return path, dict(pending)
                for name in ('current', 'previous'):
                    if data.get(name):
                        try:
                            retention.check_locked(retain_fd, self.slot(data[name]))
                            return self.slot(data[name]), None
                        except UpdateError as exc:
                            if str(exc).startswith(('retain_', 'manual_required')): raise
                            continue
                # Manual bootstrap has no pending job or deadline; use immutable factory.
                if not (self.factory / 'sessionhelper' / '__main__.py').is_file():
                    raise UpdateError('factory_missing')
                retention.check_locked(retain_fd, self.factory)
                return self.factory, None

    def confirm(self, expected):
        with self.locked():
            data = self.read()
            pending = data['pending']
            if not pending or not all(expected.get(k) == pending[k] for k in TUPLE):
                raise UpdateError('confirmation_mismatch')
            if time.time() >= pending['deadline']:
                raise UpdateError('health_timeout')
            data['previous'], data['current'] = data['current'], {k: pending[k] for k in TUPLE}
            data['terminal'][pending['job_id']] = {**data['current'], 'stage': 'confirmed', 'from_version': pending.get('from_version', '')}
            data['last_result'] = {**data['current'], 'stage': 'confirmed', 'reason': '', 'from_version': pending.get('from_version', '')}
            data['pending'] = None
            atomic_json(self.path, data)

    def fail(self, expected, reason='candidate_failed'):
        with self.locked():
            data = self.read()
            if data['pending'] and all(expected.get(k) == data['pending'][k] for k in TUPLE):
                self._rollback(data, reason)


def run(factory, root, args=()):
    state = State(Path(root), Path(factory))
    auxiliary = bool(args)  # MCP/setup/version invocations never consume trial budget.
    path, pending = state.select(trial=not auxiliary)
    env = dict(os.environ)
    env['PYTHONPATH'] = str(path)
    env['SH_UPDATE_ROOT'] = str(state.root)
    env['SH_UPDATE_LAUNCHER'] = str(Path(__file__).resolve())
    # Preserve business cwd (notably default driver/A6 workdir), while putting
    # the selected package before the read-only launcher even when cwd is its root.
    trampoline = 'import runpy,sys;sys.path.insert(0,sys.argv.pop(1));runpy.run_module("sessionhelper",run_name="__main__",alter_sys=True)'
    child = subprocess.Popen([sys.executable, '-c', trampoline, str(path), *args], env=env, start_new_session=True)
    old = {}
    stopping = None
    stop_deadline = None

    def terminate():
        # A rejected candidate may ignore TERM. Always reap it before rollback exit.
        if child.poll() is None:
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            child.wait()

    def forward(sig, frame):
        nonlocal stopping, stop_deadline
        if stopping is None:
            stopping, stop_deadline = sig, time.monotonic() + 5
        if child.poll() is None:
            try:
                os.killpg(child.pid, sig)
            except ProcessLookupError:
                pass
    try:
        for sig in (signal.SIGTERM, signal.SIGINT):
            old[sig] = signal.signal(sig, forward)
        while child.poll() is None:
            if stopping is not None:
                # Docker restart is not a failed trial. Selection on the next
                # start still enforces the original persisted deadline/budget.
                if time.monotonic() >= stop_deadline:
                    terminate()
                    break
            elif pending:
                live = state.read()
                terminal = live['terminal'].get(pending['job_id'], {})
                exact = all(terminal.get(k) == pending[k] for k in TUPLE)
                if exact and terminal.get('stage') == 'confirmed':
                    pending = None
                elif not live['pending'] or not all(live['pending'].get(k) == pending[k] for k in TUPLE):
                    terminate()
                    return 75
                elif time.time() >= pending['deadline']:
                    terminate()
                    state.fail(pending, 'health_timeout')
                    return 75
            time.sleep(0.05)
        if stopping is not None:
            return 128 + stopping
        code = child.returncode
        if pending:
            live = state.read()
            terminal = live['terminal'].get(pending['job_id'], {})
            if not (terminal.get('stage') == 'confirmed' and all(terminal.get(k) == pending[k] for k in TUPLE)):
                state.fail(pending)
                return 75  # supervisor restarts current/previous after a real failure
        return code
    finally:
        terminate()
        for sig, handler in old.items():
            signal.signal(sig, handler)


def main(factory):
    state_root = Path(os.path.expanduser(os.environ.get('SH_STATE_DIR', '~/.dingwei'))) / 'update'
    try:
        if len(sys.argv) == 3 and sys.argv[1] == '--retain-reclaim':
            state = State(Path(state_root), Path(factory))
            print(json.dumps(Retention(state.root).reclaim(sys.argv[2], state)))
            return 0
        return run(factory, state_root, sys.argv[1:])
    except UpdateError as exc:
        print('helper update guard: ' + str(exc), file=sys.stderr)
        return 78
