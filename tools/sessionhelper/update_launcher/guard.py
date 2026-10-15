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
TUPLE = ('job_id', 'release_id', 'agent_id', 'version', 'sha256')


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
            data['pending'].update(starts=0, deadline=now + HEALTH_SECONDS)
            atomic_json(self.path, data)
            return 'pending'

    def _rollback(self, data, reason):
        pending = data['pending']
        data['terminal'][pending['job_id']] = {**{k: pending[k] for k in TUPLE}, 'stage': 'rolled_back'}
        data['last_result'] = {**{k: pending[k] for k in TUPLE}, 'stage': 'rolled_back', 'reason': reason}
        data['pending'] = None
        atomic_json(self.path, data)

    def select(self, trial=True, now=None):
        now = time.time() if now is None else now
        with self.locked():
            data = self.read()
            pending = data['pending']
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
            data['terminal'][pending['job_id']] = {**data['current'], 'stage': 'confirmed'}
            data['last_result'] = {**data['current'], 'stage': 'confirmed', 'reason': ''}
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
    child = subprocess.Popen([sys.executable, '-m', 'sessionhelper', *args], cwd=path, env=env, start_new_session=True)
    old = {}
    def forward(sig, frame):
        if child.poll() is None:
            os.killpg(child.pid, sig)
    try:
        for sig in (signal.SIGTERM, signal.SIGINT):
            old[sig] = signal.signal(sig, forward)
        while child.poll() is None:
            if pending:
                live = state.read()
                if not live['pending']:
                    pending = None  # trusted runtime confirmed or terminalized this job
                elif time.time() >= pending['deadline']:
                    os.killpg(child.pid, signal.SIGTERM)
                    try:
                        child.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(child.pid, signal.SIGKILL); child.wait()
                    state.fail(pending, 'health_timeout')
                    return 75
            time.sleep(0.05)
        code = child.returncode
        if pending:
            state.fail(pending)
            return 75  # supervisor restarts previous/current, not the failed candidate
        return code
    finally:
        for sig, handler in old.items():
            signal.signal(sig, handler)


def main(factory):
    state_root = Path(os.path.expanduser(os.environ.get('SH_STATE_DIR', '~/.dingwei'))) / 'update'
    try:
        return run(factory, state_root, sys.argv[1:])
    except UpdateError as exc:
        print('helper update guard: ' + str(exc), file=sys.stderr)
        return 78
