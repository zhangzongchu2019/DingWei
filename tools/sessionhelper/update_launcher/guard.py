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
        with self.locked():
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
        with self.locked():
            data = self.read()
            pending = data['pending']
            if not trial and pending and pending['starts'] > 0 and now < pending['deadline']:
                try:
                    return self.slot(pending), None
                except UpdateError:
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
                        pending['starts'] += 1
                        atomic_json(self.path, data)  # durable budget before child exec
                        return path, dict(pending)
            for name in ('current', 'previous'):
                if data.get(name):
                    try:
                        return self.slot(data[name]), None
                    except UpdateError:
                        continue
            # Manual bootstrap has no pending job or deadline; use immutable factory.
            if not (self.factory / 'sessionhelper' / '__main__.py').is_file():
                raise UpdateError('factory_missing')
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
    trampoline = 'import sys;sys.path.insert(0,sys.argv.pop(1));import runpy;runpy.run_module("sessionhelper",run_name="__main__",alter_sys=True)'
    child = subprocess.Popen([sys.executable, '-I', '-B', '-c', trampoline, str(path), *args], env=env, start_new_session=True)
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
        return run(factory, state_root, sys.argv[1:])
    except UpdateError as exc:
        print('helper update guard: ' + str(exc), file=sys.stderr)
        return 78
